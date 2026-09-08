package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/coordinator"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/finalizer"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/githubci"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	var c githubci.Client
	var kube, ledger, proxy, image string
	var once bool
	flag.StringVar(&c.Broker, "broker", "http://127.0.0.1:8123/api", "Fixed local SSM bridge; keeps administrative GitHub credentials on the operator laptop")
	flag.StringVar(&c.TokenFile, "token-file", "/opt/cleanup/operator/github-token", "Private operator token (Administration write, Actions read)")
	flag.StringVar(&c.Repository, "repository", "Aditya3304/ephemeral-runner-cleanup-receipt", "Approved repository")
	flag.StringVar(&c.Revision, "revision", "", "Exact approved workflow/source SHA")
	flag.StringVar(&c.Branch, "branch", "aws-github-finals", "Approved branch; no pull requests")
	flag.StringVar(&kube, "kubeconfig", ".build/kubeconfig", "Cluster config")
	flag.StringVar(&ledger, "ledger", ".build/coordinator", "Private ledger")
	flag.StringVar(&proxy, "proxy-ip", "172.17.0.1", "Trusted GitHub CONNECT proxy")
	flag.StringVar(&image, "image-file", ".build/github-runner-image", "Pinned preloaded image")
	flag.BoolVar(&once, "once", false, "Process at most one queued job")
	flag.Parse()
	if len(c.Revision) != 40 {
		return fmt.Errorf("exact 40-character approved source SHA required")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := os.MkdirAll(ledger, 0700); err != nil {
		return err
	}
	co, err := coordinator.Connect(kube, ledger)
	if err != nil {
		return err
	}
	co.VerifyGitHub = c.Verify
	release, err := co.Lock()
	if err != nil {
		return err
	}
	defer release()
	// Recover coordinator-owned runs before admitting any new GitHub registration.
	entries, err := os.ReadDir(ledger)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() || len(e.Name()) != 32 {
			continue
		}
		l, e2 := co.Load(e.Name())
		if e2 != nil {
			return e2
		}
		if l.Phase != "complete" {
			if _, e2 = co.Collect(ctx, e.Name()); e2 != nil {
				return e2
			}
		}
		if e2 = complete(ctx, &c, l); e2 != nil {
			return e2
		}
	}
	for ctx.Err() == nil {
		r, j, e := c.Pending(ctx)
		if e != nil {
			return e
		}
		if r.ID == 0 {
			if once {
				return nil
			}
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(10 * time.Second):
			}
			continue
		}
		g, jit, e := c.Register(ctx, r, j, proxy)
		if e != nil {
			return e
		}
		co.GitHub = g
		co.RunnerConfig = jit
		if e = c.Status(ctx, r.SHA, "pending", "Waiting for runner termination and independent signed cleanup evidence"); e != nil {
			return e
		}
		img, e := os.ReadFile(image)
		if e != nil {
			return e
		}
		l, e := co.Start(ctx, githubci.Identity(&c, r), strings.TrimSpace(string(img)), []string{"github-actions"}, 10*time.Minute)
		co.RunnerConfig = ""
		if l != nil {
			fmt.Println(string(coordinator.JSONSummary(l)))
			if l.Phase == "complete" {
				if fe := complete(context.WithoutCancel(ctx), &c, l); fe != nil {
					return fe
				}
			}
		}
		if e != nil {
			return e
		}
		if once {
			return nil
		}
	}
	return nil
}
func complete(ctx context.Context, c *githubci.Client, l *coordinator.Ledger) error {
	marker := filepath.Join(".build/github-delivered", l.Token)
	if _, err := os.Stat(marker); err == nil {
		return nil
	}
	if err := finalize(ctx, l.Token); err != nil {
		return err
	}
	files, err := filepath.Glob(".build/finalized/*/receipt.json")
	if err != nil {
		return err
	}
	for _, file := range files {
		data, e := os.ReadFile(file)
		if e != nil {
			return e
		}
		receipt, e := finalizer.ParseReceipt(data)
		if e != nil {
			return e
		}
		if receipt.Identity != l.Identity {
			continue
		}
		var result finalizer.Result
		b, e := os.ReadFile(filepath.Join(filepath.Dir(file), "result.json"))
		if e != nil {
			return e
		}
		if e = json.Unmarshal(b, &result); e != nil {
			return e
		}
		if l.GitHub != nil {
			state := "error"
			if receipt.Verdict == "pass" {
				state = "success"
			} else if receipt.Verdict == "fail" {
				state = "failure"
			}
			if e = c.Status(ctx, l.Identity.Revision, state, fmt.Sprintf("Signed cleanup %s; run %s; receipt %.16s", receipt.Verdict, l.Identity.Run, result.Receipt.SHA256)); e != nil {
				return e
			}
		}
		if e = os.MkdirAll(filepath.Dir(marker), 0700); e != nil {
			return e
		}
		return os.WriteFile(marker, []byte(result.Receipt.SHA256+"\n"), 0600)
	}
	return fmt.Errorf("matching verified publication missing")
}
func finalize(ctx context.Context, token string) error {
	// Only a fixed operator script is executable, and the argument is a validated local token.
	if len(token) != 32 || filepath.Base(token) != token {
		return fmt.Errorf("invalid finalizer token")
	}
	cmd := exec.CommandContext(ctx, "bash", "scripts/aws/finalize.sh", token)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
