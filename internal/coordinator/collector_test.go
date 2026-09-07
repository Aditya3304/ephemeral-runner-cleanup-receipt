package coordinator

import (
	"os/exec"
	"testing"
)

func exitFailure(t *testing.T, code string) error {
	t.Helper()
	err := exec.Command("sh", "-c", "exit "+code).Run()
	if err == nil {
		t.Fatal("expected command failure")
	}
	return err
}

func TestOnlyLostResultExitIsTerminal(t *testing.T) {
	if retryableCollection(classifyCollectorFailure("result", exitFailure(t, "66"), "result lost")) {
		t.Fatal("irreversibly lost collector result remained retryable")
	}
	if !retryableCollection(classifyCollectorFailure("result", exitFailure(t, "1"), "temporary failure")) {
		t.Fatal("ordinary result failure became terminal")
	}
	if !retryableCollection(classifyCollectorFailure("ready", exitFailure(t, "66"), "not armed")) {
		t.Fatal("pre-start readiness failure became terminal")
	}
}
