package main

import (
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/finalizer"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmbeddedRevisionRequired(t *testing.T) {
	old := buildRevision
	defer func() { buildRevision = old }()
	revision := strings.Repeat("a", 40)
	c := config{Policy: finalizer.SignerPolicy{Revision: revision, Issuer: "https://issuer:8443", Identity: "finalizer@cleanup-receipt.local"}, Token: finalizer.TokenConfig{Endpoint: "https://issuer:8443/token"}}
	b, err := proof.Canonical(c)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	for _, build := range []string{"", "devel", strings.Repeat("b", 40)} {
		buildRevision = build
		if _, err = loadConfig(path); err == nil {
			t.Fatalf("accepted build %q", build)
		}
	}
	buildRevision = revision
	if _, err = loadConfig(path); err != nil {
		t.Fatal(err)
	}
}
