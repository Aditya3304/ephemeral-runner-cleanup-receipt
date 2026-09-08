package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/delivery"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
)

func main() {
	retryRejected := flag.Bool("retry-rejected", false, "Operator retry after repairing a rejection; retains previous failures and the six-attempt budget")
	flag.Parse()
	if flag.NArg() != 1 {
		os.Stderr.WriteString("Usage: deliver RESULT_PATH (or all)\n")
		os.Exit(2)
	}
	token, e := proof.ReadBounded("/run/auth/token", 256)
	if e != nil {
		os.Exit(1)
	}
	q := &delivery.Queue{Dir: "/delivery", BaseURL: "http://api:8080", Token: strings.TrimSpace(string(token)), Client: &http.Client{Timeout: 50 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect forbidden") }}}
	paths := []string{flag.Arg(0)}
	q.RetryRejected = *retryRejected
	if paths[0] == "all" {
		paths, e = filepath.Glob("/input/*/result.json")
		if e != nil {
			os.Exit(1)
		}
	}
	failed := false
	for _, path := range paths {
		b, e := proof.ReadBounded(path, 16<<10)
		if e != nil {
			failed = true
			continue
		}
		s, e := q.Deliver(context.Background(), b)
		if e != nil {
			failed = true
		}
		if s != nil {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"run_id": s.Result.Identity.Run, "status": s.Status, "attempts": s.Tries, "receipt_id": s.ReceiptID, "next_retry_at": s.NextRetryAt})
		}
	}
	if failed {
		os.Exit(1)
	}
}
