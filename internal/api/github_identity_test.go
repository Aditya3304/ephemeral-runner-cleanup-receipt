package api

import (
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
	"strings"
	"testing"
)

func TestGitHubIdentityUsesRunNumberNotFilesystemToken(t *testing.T) {
	id := proof.Identity{Provider: "github", Repository: "owner/repo", Run: "34197128621", Attempt: 1, Job: "guarded", Revision: strings.Repeat("a", 40)}
	if !validIdentity(id) {
		t.Fatal("numeric GitHub run rejected")
	}
	for _, run := range []string{"../run", "0", "1e6", strings.Repeat("a", 32)} {
		id.Run = run
		if validIdentity(id) {
			t.Fatal("invalid GitHub run accepted", run)
		}
	}
	id.Provider = "local"
	id.Run = strings.Repeat("b", 32)
	if !validIdentity(id) {
		t.Fatal("local identity regressed")
	}
}
