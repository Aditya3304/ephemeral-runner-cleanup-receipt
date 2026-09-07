package coordinator

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/coordinator/observer"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
)

func trustedFixture(t *testing.T) (*Coordinator, *Ledger, *observer.Result) {
	t.Helper()
	c := &Coordinator{Root: t.TempDir()}
	l := ledgerFixture()
	l.Phase = "complete"
	l.JobUID = "job-uid"
	l.PodUID = "3a8615a9-4b3f-4945-87dc-4470f1e70978"
	l.PodName = "job-abc12"
	l.RoleUID = "role-uid"
	l.BindingUID = "binding-uid"
	l.Image = "cleanup-receipt/runner@sha256:" + strings.Repeat("e", 64)
	l.ActualImage = "docker.io/" + l.Image
	l.ContainerID = "containerd://" + strings.Repeat("c", 64)
	exit := int32(0)
	l.PodExitCode = &exit
	l.RunnerAbsent = true
	l.ResourceAbsent = true
	l.RoleAbsent = true
	l.BindingAbsent = true
	r := &observer.Result{Kind: "kind-node-observation/v1", Request: observer.Request{Run: l.Token, UID: l.PodUID, Pod: l.PodName, Sandbox: strings.Repeat("d", 64), Binding: collectorBinding(l), Seconds: 330}, ArmedAt: time.Now().UTC().Add(-time.Minute), CompletedAt: time.Now().UTC(), StartProven: true, TerminationProven: true, ContainerID: strings.Repeat("c", 64), ImageID: l.ActualImage, StartedAt: time.Now().Add(-50 * time.Second).UnixNano(), FinishedAt: time.Now().Add(-time.Second).UnixNano(), LogInode: 123, LogDevice: 1, Logs: observer.Finding{Status: "verified", Reason: "watched start to stopped writer"}, Workspace: observer.Finding{Status: "verified", Reason: "empty pinned directory"}, Credentials: observer.Finding{Status: "verified", Reason: "empty pinned directory"}}
	logs := []byte("2026-09-07T00:00:00Z stdout F test\n")
	r.LogsDigest = proof.Digest(logs)
	r.LogsBytes = int64(len(logs))
	l.CollectorRequest = &r.Request
	l.LogsDigest = r.LogsDigest
	l.LogsBytes = r.LogsBytes
	l.LogsStatus = "complete"
	raw, _ := proof.Canonical(r)
	l.CollectorDigest = proof.Digest(raw)
	if e := c.publish(l); e != nil {
		t.Fatal(e)
	}
	dir, _ := c.runDir(l.Token)
	if e := durable(filepath.Join(dir, "collector.json"), raw); e != nil {
		t.Fatal(e)
	}
	if e := durable(filepath.Join(dir, "job.log"), logs); e != nil {
		t.Fatal(e)
	}
	return c, l, r
}
func TestFinalizationPublicationCrashWindowRepairsWithoutLedgerMutation(t *testing.T) {
	c, l, _ := trustedFixture(t)
	dir, _ := c.runDir(l.Token)
	before, _ := os.ReadFile(filepath.Join(dir, "run.json"))
	if _, e := c.LoadObservations(l.Token); e == nil {
		t.Fatal("complete ledger alone became ready")
	}
	if _, e := c.Collect(context.Background(), l.Token); e != nil {
		t.Fatal(e)
	}
	o, e := c.LoadObservations(l.Token)
	if e != nil {
		t.Fatal(e)
	}
	for name, v := range o.Coverage {
		if v.Status != "verified" || v.Observer != "trusted" {
			t.Fatalf("%s: %+v", name, v)
		}
	}
	after, _ := os.ReadFile(filepath.Join(dir, "run.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("recovery changed ledger digest")
	}
	marker, _ := os.ReadFile(filepath.Join(dir, "finalization-ready.json"))
	if e = os.Remove(filepath.Join(dir, "finalization-ready.json")); e != nil {
		t.Fatal(e)
	}
	if _, e = c.LoadObservations(l.Token); e == nil {
		t.Fatal("observation without ready marker accepted")
	}
	if _, e = c.Collect(context.Background(), l.Token); e != nil {
		t.Fatal(e)
	}
	repaired, _ := os.ReadFile(filepath.Join(dir, "finalization-ready.json"))
	if !bytes.Equal(marker, repaired) {
		t.Fatal("repaired marker changed")
	}
}
func TestObservationAndReadyRejectTampering(t *testing.T) {
	for _, name := range []string{"run.json", "observations.json", "finalization-ready.json", "job.log"} {
		t.Run(name, func(t *testing.T) {
			c, l, _ := trustedFixture(t)
			if e := c.publishObservations(l); e != nil {
				t.Fatal(e)
			}
			dir, _ := c.runDir(l.Token)
			path := filepath.Join(dir, name)
			raw, _ := os.ReadFile(path)
			raw = append(raw, ' ')
			if e := os.WriteFile(path, raw, 0600); e != nil {
				t.Fatal(e)
			}
			if _, e := c.LoadObservations(l.Token); e == nil {
				t.Fatal("tampering accepted")
			}
		})
	}
}
func TestTrustedCoverageCannotBeForgedFromSnapshotOrJobClaims(t *testing.T) {
	c, l, r := trustedFixture(t)
	dir, _ := c.runDir(l.Token)
	ledgerData, _ := os.ReadFile(filepath.Join(dir, "run.json"))
	o, e := observations(l, ledgerData, r)
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"workspace", "credentials", "logs", "resources", "runner_disposal"} {
		copyO := *o
		copyO.Coverage = map[string]proof.Observation{}
		for k, v := range o.Coverage {
			copyO.Coverage[k] = v
		}
		copyO.Coverage[name] = proof.Observation{Status: "verified", Observer: "job", Reason: "job forged authority"}
		raw, _ := proof.Canonical(copyO)
		if _, e := ValidateObservations(raw, l, ledgerData); e == nil {
			t.Fatal("forged coverage accepted", name)
		}
	}
	l.CollectorRequest = nil
	l.CollectorDigest = ""
	l.LogsStatus = "snapshot-unattested"
	if e = c.save(l); e != nil {
		t.Fatal(e)
	}
	ledgerData, _ = os.ReadFile(filepath.Join(dir, "run.json"))
	o, e = observations(l, ledgerData, nil)
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"logs", "workspace", "credentials"} {
		if o.Coverage[name].Status == "verified" {
			t.Fatal("snapshot elevated", name)
		}
	}
}
func TestCollectorIdentityAndCoverageFailuresPreventPass(t *testing.T) {
	for _, name := range []string{"source", "image", "pod", "container", "size", "start", "termination", "rbac", "residual", "gap"} {
		t.Run(name, func(t *testing.T) {
			c, l, r := trustedFixture(t)
			switch name {
			case "source":
				l.Identity.Revision = strings.Repeat("f", 40)
			case "image":
				l.ActualImage = "wrong"
			case "pod":
				l.PodUID = "replacement"
			case "container":
				l.ContainerID = "containerd://" + strings.Repeat("f", 64)
			case "size":
				l.LogsBytes++
			case "start":
				r.StartProven = false
			case "termination":
				r.TerminationProven = false
			case "rbac":
				l.BindingAbsent = false
			case "residual":
				r.Workspace = observer.Finding{Status: "failed", Reason: "hidden file remains"}
			case "gap":
				r.Logs = observer.Finding{Status: "unobservable", Reason: "rotation observed"}
				l.LogsStatus = "gap"
			}
			raw, _ := proof.Canonical(r)
			l.CollectorDigest = proof.Digest(raw)
			if e := c.save(l); e != nil {
				t.Fatal(e)
			}
			dir, _ := c.runDir(l.Token)
			ledgerData, _ := os.ReadFile(filepath.Join(dir, "run.json"))
			o, e := observations(l, ledgerData, r)
			if name == "rbac" || name == "residual" || name == "gap" {
				if e != nil {
					t.Fatal(e)
				}
				all := true
				for _, v := range o.Coverage {
					all = all && v.Status == "verified"
				}
				if all {
					t.Fatal("incomplete/failure became full coverage")
				}
			} else if e == nil {
				t.Fatal("invalid collector accepted")
			}
		})
	}
}
func TestBoundedArtifactVerificationAndTransport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log")
	data := []byte("exact bytes\x00\xff\n")
	if e := os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	if e := verifyFile(path, proof.Digest(data), int64(len(data)), int64(len(data))); e != nil {
		t.Fatal(e)
	}
	if e := verifyFile(path, proof.Digest(data), int64(len(data)), int64(len(data)-1)); e == nil {
		t.Fatal("limit bypass")
	}
	link := filepath.Join(dir, "link")
	if e := os.Symlink(path, link); e != nil {
		t.Fatal(e)
	}
	if e := verifyFile(link, proof.Digest(data), int64(len(data)), 100); e == nil {
		t.Fatal("symlink artifact accepted")
	}
	var b bytes.Buffer
	w := cappedWriter{W: &b, Remaining: 4}
	if n, e := w.Write([]byte("1234")); e != nil || n != 4 {
		t.Fatal(n, e)
	}
	if _, e := w.Write([]byte("5")); e == nil || b.String() != "1234" {
		t.Fatal("transport limit not enforced")
	}
}
