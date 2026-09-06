//go:build linux

package proof

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// A subprocess bounds this regression even if opening the FIFO blocks before
// Wipe can observe its context deadline. No writer ever opens the FIFO.
func TestWipeOwnerFIFO(t *testing.T) {
	if os.Getenv("PROOF_TEST_OWNER_FIFO") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWipeOwnerFIFO$", "-test.count=1")
		cmd.Env = append(os.Environ(), "PROOF_TEST_OWNER_FIFO=1")
		output, err := cmd.CombinedOutput()
		if ctx.Err() != nil {
			t.Fatalf("Wipe blocked on owner FIFO past deadline: %s", output)
		}
		if err != nil {
			t.Fatalf("FIFO regression: %v\n%s", err, output)
		}
		return
	}
	s := localState(t)
	marker := filepath.Join(s.Sandbox, ".proof-owner")
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(marker, 0600); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(s.Sandbox, "workspace", "keep")
	mustWrite(t, keep, "keep")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	if err := Wipe(ctx, s, "workspace"); err == nil {
		t.Fatal("FIFO owner accepted")
	}
	if time.Since(started) > time.Second {
		t.Fatal("FIFO rejection was not prompt")
	}
	if data, err := os.ReadFile(keep); err != nil || string(data) != "keep" {
		t.Fatalf("workspace modified: %q, %v", data, err)
	}
}

func TestWipeRejectsNonRegularAndUnboundedOwner(t *testing.T) {
	for _, mode := range []string{"directory", "symlink", "short", "long", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			s := localState(t)
			marker := filepath.Join(s.Sandbox, ".proof-owner")
			if err := os.Remove(marker); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "directory":
				if err := os.Mkdir(marker, 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				outside := filepath.Join(t.TempDir(), "owner")
				mustWrite(t, outside, s.Token)
				if err := os.Symlink(outside, marker); err != nil {
					t.Fatal(err)
				}
			case "short":
				mustWrite(t, marker, s.Token[:31])
			case "long":
				mustWrite(t, marker, s.Token+"x")
			case "oversized":
				mustWrite(t, marker, s.Token+strings.Repeat("x", 1<<20))
			}
			keep := filepath.Join(s.Sandbox, "credentials", "keep")
			mustWrite(t, keep, "keep")
			if err := Wipe(context.Background(), s, "credentials"); err == nil {
				t.Fatal("unsafe owner accepted")
			}
			if data, err := os.ReadFile(keep); err != nil || string(data) != "keep" {
				t.Fatalf("credentials modified: %q, %v", data, err)
			}
		})
	}
}
