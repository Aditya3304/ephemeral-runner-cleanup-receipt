package githubrun

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
	"golang.org/x/sys/unix"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func reply(code int, b []byte) *http.Response {
	return &http.Response{StatusCode: code, Body: io.NopCloser(bytes.NewReader(b)), Header: make(http.Header)}
}
func jreply(v any) *http.Response { b, _ := json.Marshal(v); return reply(200, b) }
func packed(t *testing.T, names []string, data []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, name := range names {
		w, e := z.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		w.Write(data)
	}
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func fixture(t *testing.T) (*Client, proof.Identity, *run, *job, []byte) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	id := proof.Identity{Provider: "github", Repository: "owner/repo", Run: "123", Attempt: 2, Job: "cleanup", Revision: strings.Repeat("a", 40)}
	r := &run{ID: 123, Attempt: 2, WorkflowID: 7, Path: ".github/workflows/cleanup.yml", SHA: id.Revision, Status: "completed", Conclusion: "success", Created: now.Add(-10 * time.Minute), Updated: now.Add(-3 * time.Minute)}
	r.Repository.Name = id.Repository
	// GitHub's documented example omits run_attempt on jobs.
	j := &job{ID: 456, RunID: 123, SHA: id.Revision, Name: "cleanup", Status: "completed", Conclusion: "success"}
	s, _ := proof.NewState(id)
	s.NamespaceUID = "namespace-uid"
	s.Sandbox = "/data/" + s.Namespace
	s.Directories = map[string]proof.DirIdentity{"root": {Inode: 1}, "workspace": {Inode: 2}, "credentials": {Inode: 3}}
	s.ClusterUID = "cluster-uid"
	s.StartedAt = r.Created.Add(time.Minute)
	done := r.Updated.Add(-time.Minute)
	s.CompletedAt = &done
	s.CleanupAttempts = 1
	s.InventoryComplete = true
	s.Resources = []proof.Ref{}
	for _, k := range []string{"workspace", "credentials", "resources"} {
		s.Coverage[k] = proof.Observation{Status: "verified", Observer: "job", Reason: "Job cleanup completed"}
	}
	value, err := s.Evidence()
	if err != nil {
		t.Fatal(err)
	}
	evidence, _ := proof.Canonical(value)
	c := &Client{Config: Config{Repository: id.Repository, WorkflowID: 7, WorkflowPath: r.Path, Jobs: map[string]string{"cleanup": "cleanup"}, Since: r.Created.Add(-time.Hour), TokenFile: "/operator/token"}, base: "https://api.github.com", token: "TEST-ONLY", Now: func() time.Time { return now }}
	return c, id, r, j, evidence
}
func serve(t *testing.T, c *Client, r *run, jobs []job, evidence []byte, mutate func(*http.Request) *http.Response) {
	t.Helper()
	blob := packed(t, []string{"evidence.json"}, evidence)
	c.http = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: transport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "api.github.com" {
			if req.Header.Get("Authorization") != "Bearer TEST-ONLY" {
				t.Error("missing API token")
			}
		} else if req.Header.Get("Authorization") != "" {
			t.Error("token leaked to signed download")
		}
		if mutate != nil {
			if response := mutate(req); response != nil {
				return response, nil
			}
		}
		switch req.URL.Path {
		case "/repos/owner/repo/actions/workflows/7/runs":
			return jreply(map[string]any{"workflow_runs": []run{*r}}), nil
		case "/repos/owner/repo/actions/runs/123/attempts/2":
			return jreply(r), nil
		case "/repos/owner/repo/actions/runs/123/attempts/2/jobs":
			return jreply(map[string]any{"jobs": jobs}), nil
		case "/repos/owner/repo/actions/jobs/456/logs":
			response := reply(302, nil)
			response.Header.Set("Location", "https://test.blob.core.windows.net/log")
			return response, nil
		case "/log":
			return reply(200, []byte("all completed job logs\n")), nil
		case "/repos/owner/repo/actions/runs/123/artifacts":
			items := []artifact{}
			if evidence != nil {
				a := artifact{ID: 9, Name: "cleanup-preliminary-123-2-cleanup", Digest: "sha256:" + proof.Digest(blob)}
				a.Run.ID = 123
				a.Run.SHA = r.SHA
				items = append(items, a)
			}
			return jreply(map[string]any{"artifacts": items}), nil
		case "/repos/owner/repo/actions/artifacts/9/zip":
			return reply(200, blob), nil
		}
		t.Errorf("unexpected request %s", req.URL.Path)
		return reply(404, nil), nil
	})}
}
func TestCollectGitHubAndAtomicPublication(t *testing.T) {
	c, id, r, j, evidence := fixture(t)
	serve(t, c, r, []job{*j}, evidence, nil)
	l, files, err := c.Collect(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if l.EvidenceStatus != "accepted-untrusted" || l.LogsStatus != "collected" || l.JobID != 456 {
		t.Fatalf("unexpected ledger %+v", l)
	}
	root := filepath.Join(t.TempDir(), "input")
	previousMask := unix.Umask(0077)
	defer unix.Umask(previousMask)
	if err = Publish(root, l, files); err != nil {
		t.Fatal(err)
	}
	for name := range files {
		info, err := os.Stat(filepath.Join(root, Directory(id), name))
		if err != nil || info.Mode().Perm() != 0644 {
			t.Fatalf("finalizer cannot read service publication %s: %v", name, err)
		}
	}
	if _, err = Load(root, id); err != nil {
		t.Fatal(err)
	}
	if err = Publish(root, l, files); err == nil {
		t.Fatal("overwrote frozen snapshot")
	}
	other := id
	other.Revision = strings.Repeat("b", 40)
	if _, err = Load(root, other); err == nil {
		t.Fatal("accepted revision conflict")
	}
	marker, _ := os.ReadFile(filepath.Join(root, Directory(id), "finalization-ready.json"))
	if !bytes.Equal(marker, Ready(files)) {
		t.Fatal("wrong publication hash")
	}
	files["job.log"] = []byte("tampered")
	if ValidateFiles(l, files) == nil {
		t.Fatal("accepted altered logs")
	}
}
func TestMissingPostAndInvalidEvidenceStillCollected(t *testing.T) {
	for _, scenario := range []string{"missing", "bad-json", "forged-trusted", "wrong-attempt", "unallocated", "expired", "missing-logs", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			c, id, r, j, evidence := fixture(t)
			jobs := []job{*j}
			var mutate func(*http.Request) *http.Response
			want := "invalid"
			switch scenario {
			case "missing":
				evidence = nil
				want = "missing"
			case "bad-json":
				evidence = []byte("not JSON")
			case "forged-trusted":
				evidence = bytes.ReplaceAll(evidence, []byte(`"observer":"job"`), []byte(`"observer":"trusted"`))
			case "wrong-attempt":
				evidence = bytes.ReplaceAll(evidence, []byte(`"run_attempt":2`), []byte(`"run_attempt":1`))
			case "unallocated":
				jobs = nil
				want = "missing"
			case "cancelled":
				r.Conclusion = "cancelled"
				jobs[0].Conclusion = "cancelled"
				evidence = nil
				want = "missing"
			case "expired":
				want = "missing"
				mutate = func(req *http.Request) *http.Response {
					if strings.HasSuffix(req.URL.Path, "/zip") {
						return reply(410, nil)
					}
					return nil
				}
			case "missing-logs":
				want = "accepted-untrusted"
				mutate = func(req *http.Request) *http.Response {
					if strings.HasSuffix(req.URL.Path, "/logs") {
						return reply(404, nil)
					}
					return nil
				}
			}
			serve(t, c, r, jobs, evidence, mutate)
			l, _, err := c.Collect(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			if l.EvidenceStatus != want {
				t.Fatalf("got %s want %s", l.EvidenceStatus, want)
			}
			if (scenario == "missing-logs" || scenario == "unallocated") && l.LogsStatus != "missing" {
				t.Fatal("missing logs claimed collected")
			}
		})
	}
}
func TestCollectorRejectsWrongControlPlaneAndTransientFailures(t *testing.T) {
	for _, scenario := range []string{"run", "attempt", "revision", "workflow", "repository", "job-sha", "ambiguous-job", "job-attempt", "rate-limit", "server-error", "redirect", "pending"} {
		t.Run(scenario, func(t *testing.T) {
			c, id, r, j, evidence := fixture(t)
			jobs := []job{*j}
			var mutate func(*http.Request) *http.Response
			switch scenario {
			case "run":
				r.ID++
			case "attempt":
				r.Attempt++
			case "revision":
				r.SHA = strings.Repeat("b", 40)
			case "workflow":
				r.WorkflowID++
			case "repository":
				r.Repository.Name = "evil/repo"
			case "job-sha":
				jobs[0].SHA = strings.Repeat("b", 40)
			case "ambiguous-job":
				jobs = append(jobs, *j)
			case "job-attempt":
				jobs[0].Attempt = 1
			case "pending":
				r.Status = "in_progress"
			case "rate-limit", "server-error":
				mutate = func(req *http.Request) *http.Response {
					if strings.HasSuffix(req.URL.Path, "/logs") {
						code := 429
						if scenario == "server-error" {
							code = 503
						}
						return reply(code, nil)
					}
					return nil
				}
			case "redirect":
				mutate = func(req *http.Request) *http.Response {
					if strings.HasSuffix(req.URL.Path, "/logs") {
						res := reply(302, nil)
						res.Header.Set("Location", "http://127.0.0.1/private")
						return res
					}
					return nil
				}
			}
			serve(t, c, r, jobs, evidence, mutate)
			if _, _, err := c.Collect(context.Background(), id); err == nil {
				t.Fatal("unsafe snapshot published")
			}
		})
	}
}
func TestDiscoveryRerunsAndPagination(t *testing.T) {
	c, _, r, j, evidence := fixture(t)
	pages := 0
	serve(t, c, r, []job{*j}, evidence, func(req *http.Request) *http.Response {
		if strings.HasSuffix(req.URL.Path, "/runs") {
			pages++
			if req.URL.Query().Get("created") == "" {
				t.Error("missing discovery boundary")
			}
			runs := []run{*r}
			if pages == 1 {
				runs = make([]run, 100)
				for i := range runs {
					runs[i] = *r
					runs[i].ID = int64(i + 1000)
				}
			}
			return jreply(map[string]any{"workflow_runs": runs})
		}
		return nil
	})
	ids, err := c.Discover(context.Background())
	if err != nil || len(ids) != 202 || pages != 2 {
		t.Fatalf("pagination/attempts %d %d %v", len(ids), pages, err)
	}
	if Directory(ids[0]) == Directory(ids[1]) {
		t.Fatal("attempts collapsed")
	}
}
func TestUnsafeZipAndDownloadBounds(t *testing.T) {
	for _, names := range [][]string{{"../evidence.json"}, {"evidence.json", "evidence.json"}, {"nested/evidence.json"}, {"a", "b", "c"}} {
		if _, err := unpack(packed(t, names, []byte("{}"))); err == nil {
			t.Fatal("unsafe ZIP accepted")
		}
	}
	if _, err := unpack(packed(t, []string{"evidence.json"}, bytes.Repeat([]byte("x"), MaxEvidence+1))); err == nil {
		t.Fatal("zip bomb accepted")
	}
	for _, address := range []string{"https://evil.invalid/x", "http://a.blob.core.windows.net/x", "https://a.blob.core.windows.net:8443/x", "https://user@a.blob.core.windows.net/x", "https://a.blob.core.windows.net.evil.invalid/x"} {
		if safeDownload(address) {
			t.Fatal("unsafe host accepted")
		}
	}
	c, _, _, _, _ := fixture(t)
	c.http = &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) { return reply(200, []byte("12345")), nil })}
	if _, err := c.get(context.Background(), "/bounded", 4, false); !errors.Is(err, errOversized) {
		t.Fatal("response limit not enforced")
	}
}
