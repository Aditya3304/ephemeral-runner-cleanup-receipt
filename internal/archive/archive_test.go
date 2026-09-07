package archive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

func TestReferenceContract(t *testing.T) {
	r := Ref{"local-minio", "receipt-archive", "evidence/sha256/" + strings.Repeat("a", 64), "version", strings.Repeat("a", 64), 7}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(r)
	var obj map[string]any
	_ = json.Unmarshal(b, &obj)
	if len(obj) != 6 {
		t.Fatal("reference contract changed", string(b))
	}
	for _, k := range []string{"store", "bucket", "key", "version_id", "sha256", "size_bytes"} {
		if _, ok := obj[k]; !ok {
			t.Fatal(k)
		}
	}
	for _, mutate := range []func(*Ref){func(r *Ref) { r.VersionID = "null" }, func(r *Ref) { r.Key = "../" + r.Key }, func(r *Ref) { r.SizeBytes = -1 }, func(r *Ref) { r.SHA256 = strings.Repeat("A", 64) }} {
		bad := r
		mutate(&bad)
		if bad.Validate() == nil {
			t.Fatal("invalid reference accepted", bad)
		}
	}
}

func TestAdmissionRenewsExpiredVersion(t *testing.T) {
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	created := now.Add(-10 * 24 * time.Hour)
	historicalUntil := created.Add(7 * 24 * time.Hour)
	if historicalUntil.Before(created.Add(7 * 24 * time.Hour)) {
		t.Fatal("valid historical retention rejected")
	}
	if !needsExtension(historicalUntil, now) {
		t.Fatal("new admission reused expired protection")
	}
	if !needsExtension(now.Add(time.Hour), now) {
		t.Fatal("new admission reused short protection")
	}
	renewed := admissionDeadline(now)
	if renewed.Before(now.Add(7*24*time.Hour)) || needsExtension(renewed, now) {
		t.Fatal("invalid renewal horizon")
	}
	if needsExtension(now.Add(9*24*time.Hour), now) {
		t.Fatal("longer retention would be shortened")
	}
}

func integrationClient(t *testing.T, role string) *Client {
	t.Helper()
	cfg, err := LoadConfig("/secrets/" + role + "/config.json")
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func denied(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("forbidden operation succeeded")
	}
	var response minio.ErrorResponse
	if !errors.As(err, &response) {
		t.Fatalf("not a storage denial: %v", err)
	}
	code := response.Code
	if code != "AccessDenied" && code != "InvalidRequest" && code != "MethodNotAllowed" {
		t.Fatalf("unexpected denial %q: %v", code, err)
	}
	t.Log("rejected:", code)
}

func TestRealArchive(t *testing.T) {
	if os.Getenv("ARCHIVE_INTEGRATION") != "1" {
		t.Skip("run scripts/archive-test.sh for real isolated storage")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	finalizer, verifier, admin := integrationClient(t, "finalizer"), integrationClient(t, "verifier"), integrationClient(t, "admin")
	data := []byte(fmt.Sprintf("archive real storage probe %d\n", time.Now().UnixNano()))
	ref, err := finalizer.Put(ctx, "job.log", data)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("uploaded exact reference: %+v", ref)
	t.Run("digest-size-version-encryption-retention", func(t *testing.T) {
		b, err := verifier.Read(ctx, ref)
		if err != nil || !bytes.Equal(b, data) {
			t.Fatal("readback", err)
		}
		t.Log("SHA-256, byte size, version, aws:kms/archive-key and >=7 day COMPLIANCE verified")
	})
	t.Run("idempotent-conditional-put", func(t *testing.T) {
		again, err := finalizer.Put(ctx, "job.log", data)
		if err != nil || again != ref {
			t.Fatal("retry changed reference", again, err)
		}
	})
	t.Run("tampered-references", func(t *testing.T) {
		for _, mutate := range []func(*Ref){func(r *Ref) { r.Store = "other" }, func(r *Ref) { r.Bucket = "other" }, func(r *Ref) { r.VersionID = "00000000-0000-0000-0000-000000000001" }, func(r *Ref) { r.SizeBytes++ }, func(r *Ref) { r.SHA256 = strings.Repeat("0", 64); r.Key = "evidence/sha256/" + r.SHA256 }} {
			bad := ref
			mutate(&bad)
			if verifier.Verify(ctx, bad) == nil {
				t.Fatal("tampered ref accepted", bad)
			}
		}
	})
	t.Run("expired-version-readmitted-with-simulated-clock", func(t *testing.T) {
		future := time.Now().Add(8 * 24 * time.Hour)
		_, oldUntil, err := finalizer.s3.GetObjectRetention(ctx, ref.Bucket, ref.Key, ref.VersionID)
		if err != nil || oldUntil == nil || !oldUntil.Before(future) {
			t.Fatal("probe must be expired at simulated admission", err)
		}
		if err = finalizer.protectAdmission(ctx, ref, future); err != nil {
			t.Fatal(err)
		}
		mode, until, err := verifier.s3.GetObjectRetention(ctx, ref.Bucket, ref.Key, ref.VersionID)
		if err != nil || mode == nil || *mode != minio.Compliance || until == nil || until.Before(future.Add(7*24*time.Hour)) {
			t.Fatal("expired version not renewed", err)
		}
		if err = verifier.Verify(ctx, ref); err != nil {
			t.Fatal("same original version no longer verifies", err)
		}
		t.Log("same exact version renewed >=7d after simulated admission 8d later")
	})
	t.Run("compliance-cannot-be-shortened", func(t *testing.T) {
		until := time.Now().Add(7 * 24 * time.Hour)
		mode := minio.Compliance
		denied(t, finalizer.s3.PutObjectRetention(ctx, ref.Bucket, ref.Key, minio.PutObjectRetentionOptions{VersionID: ref.VersionID, Mode: &mode, RetainUntilDate: &until}))
	})
	t.Run("verifier-cannot-upload", func(t *testing.T) { _, err := verifier.Put(ctx, "job.log", []byte("forbidden")); denied(t, err) })
	t.Run("verifier-cannot-read-unversioned-bytes", func(t *testing.T) {
		object, err := verifier.s3.GetObject(ctx, ref.Bucket, ref.Key, minio.GetObjectOptions{})
		if err == nil {
			defer object.Close()
			_, err = io.ReadAll(io.LimitReader(object, MaxObjectBytes+1))
		}
		denied(t, err)
	})
	t.Run("verifier-wire-get-without-version-denied",func(t *testing.T){
		u,err:=verifier.s3.Presign(ctx,http.MethodGet,ref.Bucket,ref.Key,time.Minute,nil)
		if err!=nil{t.Fatal(err)}
		if _,ok:=u.Query()["versionId"];ok{t.Fatal("probe unexpectedly includes versionId")}
		req,err:=http.NewRequestWithContext(ctx,http.MethodGet,u.String(),nil);if err!=nil{t.Fatal(err)}
		hc:=&http.Client{Transport:verifier.transport,Timeout:10*time.Second,CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}}
		resp,err:=hc.Do(req);if err!=nil{t.Fatal(err)};defer resp.Body.Close()
		body,err:=io.ReadAll(io.LimitReader(resp.Body,4096));if err!=nil{t.Fatal(err)}
		if resp.StatusCode!=http.StatusForbidden || !bytes.Contains(body,[]byte("<Code>AccessDenied</Code>")){t.Fatalf("unversioned wire GET not denied: %d %s",resp.StatusCode,body)}
		t.Log("direct signed HTTP GET without versionId: 403 AccessDenied (no SDK lazy reader)")
	})
	t.Run("verifier-cannot-extend-retention", func(t *testing.T) {
		until := time.Now().Add(20 * 24 * time.Hour)
		mode := minio.Compliance
		denied(t, verifier.s3.PutObjectRetention(ctx, ref.Bucket, ref.Key, minio.PutObjectRetentionOptions{VersionID: ref.VersionID, Mode: &mode, RetainUntilDate: &until}))
	})
	for name, c := range map[string]*Client{"finalizer": finalizer, "verifier": verifier, "admin-compliance": admin} {
		t.Run(name+"-cannot-delete-retained-version", func(t *testing.T) {
			denied(t, c.s3.RemoveObject(ctx, ref.Bucket, ref.Key, minio.RemoveObjectOptions{VersionID: ref.VersionID, GovernanceBypass: true}))
		})
	}
	for name, c := range map[string]*Client{"finalizer": finalizer, "verifier": verifier} {
		t.Run(name+"-cannot-delete-marker", func(t *testing.T) {
			denied(t, c.s3.RemoveObject(ctx, ref.Bucket, ref.Key, minio.RemoveObjectOptions{}))
		})
		t.Run(name+"-cannot-list-buckets", func(t *testing.T) { _, err := c.s3.ListBuckets(ctx); denied(t, err) })
		t.Run(name+"-cannot-change-retention", func(t *testing.T) {
			until := time.Now().Add(time.Hour)
			mode := minio.Governance
			denied(t, c.s3.PutObjectRetention(ctx, ref.Bucket, ref.Key, minio.PutObjectRetentionOptions{VersionID: ref.VersionID, Mode: &mode, RetainUntilDate: &until, GovernanceBypass: true}))
		})
	}
	t.Run("outside-prefix-denied", func(t *testing.T) {
		_, err := finalizer.s3.PutObject(ctx, ref.Bucket, "outside/probe", bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{})
		denied(t, err)
	})
	t.Run("exact-version-overwrite-forbidden", func(t *testing.T) {
		// Sign the versionId query, not an unsigned edit of a presigned URL.
		u, err := finalizer.s3.Presign(ctx, http.MethodPut, ref.Bucket, ref.Key, time.Minute, url.Values{"versionId": []string{ref.VersionID}})
		if err != nil {
			t.Fatal(err)
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), strings.NewReader("changed"))
		// Use this client's pinned-CA transport; never an insecure HTTP client.
		hc := &http.Client{Transport: finalizer.transport, Timeout: 10 * time.Second}
		resp, err := hc.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		if resp.StatusCode < 400 {
			t.Fatalf("version-targeted overwrite accepted: %d %s", resp.StatusCode, b)
		}
		t.Log("version-targeted PUT rejected", resp.StatusCode, string(b))
		if err = verifier.Verify(ctx, ref); err != nil {
			t.Fatal("original version changed", err)
		}
	})
	t.Run("new-version-preserves-original", func(t *testing.T) {
		// A privileged unconditioned PUT creates a distinct version. It must not
		// change the bytes/version referenced by an already signed receipt.
		info, err := admin.s3.PutObject(ctx, ref.Bucket, ref.Key, strings.NewReader("replacement version"), 19, minio.PutObjectOptions{DisableMultipart: true})
		if err != nil {
			t.Fatal(err)
		}
		if info.VersionID == "" || info.VersionID == ref.VersionID {
			t.Fatal("version did not change")
		}
		if err = verifier.Verify(ctx, ref); err != nil {
			t.Fatal("original version changed", err)
		}
		if _, err = finalizer.Put(ctx, "job.log", data); err == nil {
			t.Fatal("corrupted latest version silently trusted")
		}
	})
	t.Run("untrusted-ca-rejected", func(t *testing.T) {
		hc := &http.Client{Timeout: 5 * time.Second}
		resp, err := hc.Get(finalizer.config.Endpoint + "/minio/health/live")
		if resp != nil {
			resp.Body.Close()
		}
		if err == nil {
			t.Fatal("system CA unexpectedly trusts private archive")
		}
	})
	t.Run("runtime-internet-blocked", func(t *testing.T) {
		conn, err := net.DialTimeout("tcp", "1.1.1.1:443", 2*time.Second)
		if err == nil {
			conn.Close()
			t.Fatal("archive runtime has internet egress")
		}
		t.Log("external TCP connection blocked")
	})
}
