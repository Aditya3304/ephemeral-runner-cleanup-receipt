package coordinator

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func ledgerFixture() *Ledger {
	token := strings.Repeat("a", 32)
	return &Ledger{Kind: "local-ci-run/v1", Identity: proof.Identity{Provider: "local", Repository: "test/repo", Run: token, Attempt: 1, Job: "test", Revision: strings.Repeat("b", 40)}, Token: token, ClusterUID: "cluster-uid", ResourceNamespace: "proof-" + token, ResourceUID: "resource-uid", RunnerNamespace: "proof-runner-" + token, RunnerUID: "runner-uid", StageUID: "stage-uid", CreatedAt: time.Now().UTC(), Phase: "running", Command: []string{"node", "-e", "process.exit(0)"}, TimeoutSeconds: 30}
}

func evidenceFixture(l *Ledger) preliminary {
	now := time.Now().UTC()
	p := preliminary{Kind: "cleanup-evidence/v1", Identity: l.Identity, Namespace: l.ResourceNamespace, NamespaceUID: l.ResourceUID, ClusterUID: l.ClusterUID, StartedAt: l.CreatedAt, CompletedAt: &now, CleanupAttempts: 1, InventoryComplete: true, Resources: []proof.Ref{}, Coverage: map[string]proof.Observation{}, Verdict: "partial", SignatureState: "unsigned"}
	for _, name := range []string{"workspace", "credentials", "resources"} {
		p.Coverage[name] = proof.Observation{Status: "verified", Observer: "job", Reason: "test observation"}
	}
	for _, name := range []string{"logs", "runner_disposal"} {
		p.Coverage[name] = proof.Observation{Status: "unobservable", Observer: "none", Reason: "not observed"}
	}
	return p
}

func TestUntrustedEvidenceCannotCrossTrustBoundary(t *testing.T) {
	l := ledgerFixture()
	p := evidenceFixture(l)
	data, _ := proof.Canonical(p)
	if err := validateEvidence(data, l); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*preliminary)
	}{
		{"wrong run", func(p *preliminary) { p.Identity.Run = "other" }},
		{"namespace replacement", func(p *preliminary) { p.NamespaceUID = "replacement" }},
		{"trusted observer forgery", func(p *preliminary) {
			p.Coverage["workspace"] = proof.Observation{Status: "verified", Observer: "trusted", Reason: "forged"}
		}},
		{"self disposal", func(p *preliminary) {
			p.Coverage["runner_disposal"] = proof.Observation{Status: "verified", Observer: "job", Reason: "forged"}
		}},
		{"passing unsigned receipt", func(p *preliminary) { p.Verdict = "pass" }},
		{"foreign resource", func(p *preliminary) {
			p.Resources = append(p.Resources, proof.Ref{Namespace: "default", UID: "u", Name: "n", Resource: "secrets", Version: "v1"})
		}},
		{"bad chronology", func(p *preliminary) { p.StartedAt = p.StartedAt.Add(time.Hour) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := evidenceFixture(l)
			test.change(&p)
			data, _ := proof.Canonical(p)
			if validateEvidence(data, l) == nil {
				t.Fatal("forgery accepted")
			}
		})
	}
	if validateEvidence(append(data, ' '), l) == nil {
		t.Fatal("noncanonical evidence accepted")
	}
	duplicate := bytes.Replace(data, []byte(`"kind":"cleanup-evidence/v1"`), []byte(`"kind":"cleanup-evidence/v1","kind":"cleanup-evidence/v1"`), 1)
	if validateEvidence(duplicate, l) == nil {
		t.Fatal("duplicate JSON field accepted")
	}
	if validateEvidence(bytes.Repeat([]byte(" "), 901<<10), l) == nil {
		t.Fatal("oversized evidence accepted")
	}
}

func TestLedgerPersistsIntentAndLocks(t *testing.T) {
	c := &Coordinator{Root: t.TempDir()}
	release, err := c.Lock()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if release2, err := c.Lock(); err == nil {
		release2()
		t.Fatal("second coordinator lock accepted")
	}
	l := ledgerFixture()
	dir, _ := c.runDir(l.Identity.Run)
	if err = os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = c.save(l); err != nil {
		t.Fatal(err)
	}
	loaded, err := c.Load(l.Identity.Run)
	if err != nil || loaded.ResourceUID != l.ResourceUID {
		t.Fatalf("ledger recovery: %v", err)
	}
	if _, err = c.Load("../escape"); err == nil {
		t.Fatal("path escape accepted")
	}
	if err = c.RequestCancel(l.Identity.Run); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dir, "cancel.requested")); err != nil {
		t.Fatal(err)
	}
}

func TestStagingRejectsReplayAndReplacement(t *testing.T) {
	l := ledgerFixture()
	p := evidenceFixture(l)
	data, _ := proof.Canonical(p)
	stage := &core.ConfigMap{ObjectMeta: meta.ObjectMeta{Name: "guard-stage", Namespace: l.RunnerNamespace, UID: types.UID(l.StageUID)}, Data: map[string]string{"evidence.json": string(data)}}
	c := &Coordinator{Root: t.TempDir(), K: fake.NewClientset(stage)}
	dir, _ := c.runDir(l.Identity.Run)
	_ = os.Mkdir(dir, 0700)
	if err := c.collectStage(context.Background(), l); err != nil {
		t.Fatal(err)
	}
	original := l.EvidenceDigest
	if err := c.collectStage(context.Background(), l); err != nil {
		t.Fatal(err)
	}
	p.CleanupAttempts = 2
	data, _ = proof.Canonical(p)
	stage.Data["evidence.json"] = string(data)
	_, _ = c.K.CoreV1().ConfigMaps(l.RunnerNamespace).Update(context.Background(), stage, meta.UpdateOptions{})
	if c.collectStage(context.Background(), l) == nil {
		t.Fatal("conflicting replay accepted")
	}
	archived, _ := os.ReadFile(filepath.Join(dir, "cleanup-evidence.json"))
	if proof.Digest(archived) != original {
		t.Fatal("original evidence overwritten")
	}
	stage.UID = "other"
	_, _ = c.K.CoreV1().ConfigMaps(l.RunnerNamespace).Update(context.Background(), stage, meta.UpdateOptions{})
	if c.collectStage(context.Background(), l) == nil {
		t.Fatal("stage replacement accepted")
	}
}

func TestDisposalPreservesNamespaceReplacement(t *testing.T) {
	l := ledgerFixture()
	c := &Coordinator{K: fake.NewClientset(&core.Namespace{ObjectMeta: meta.ObjectMeta{Name: l.ResourceNamespace, UID: "replacement", Labels: labels(l)}})}
	if c.deleteNamespace(context.Background(), l.ResourceNamespace, l.ResourceUID, l.Token) == nil {
		t.Fatal("replacement accepted")
	}
	for _, a := range c.K.(*fake.Clientset).Actions() {
		if a.GetVerb() == "delete" {
			t.Fatal("replacement was deleted")
		}
	}
}
