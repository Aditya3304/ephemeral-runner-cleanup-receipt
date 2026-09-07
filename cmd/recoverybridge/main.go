// recoverybridge runs in an isolated container. Auth remains in its Docker volume.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/archive"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/finalizer"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/watchdog"
)

func main() {
	if err := run(); err != nil {
		os.Stderr.WriteString("Recovery bridge unavailable or invalid input\n")
		os.Exit(1)
	}
}
func client() *http.Client {
	return &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect forbidden") }}
}
func run() error {
	if len(os.Args) < 2 {
		return errors.New("mode required")
	}
	token, e := proof.ReadBounded("/run/auth/token", 256)
	if e != nil {
		return e
	}
	if os.Args[1] == "report" {
		b, e := io.ReadAll(io.LimitReader(os.Stdin, 16<<10+1))
		if e != nil || len(b) > 16<<10 {
			return errors.New("invalid report")
		}
		req, e := http.NewRequestWithContext(context.Background(), "POST", "http://api:8080/v1/recovery", bytes.NewReader(b))
		if e != nil {
			return e
		}
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
		req.Header.Set("Content-Type", "application/json")
		resp, e := client().Do(req)
		if e != nil {
			return e
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return errors.New("report rejected")
		}
		return nil
	}
	if len(os.Args) != 3 || os.Args[1] != "inspect" || !regexp.MustCompile("^[a-f0-9]{64}$").MatchString(os.Args[2]) {
		return errors.New("invalid inspection key")
	}
	key := os.Args[2]
	out := watchdog.Inspection{}
	var state struct {
		Status   string     `json:"status"`
		Stage    string     `json:"stage"`
		Attempts int        `json:"attempts"`
		Next     *time.Time `json:"next_retry_at"`
	}
	b, e := proof.ReadBounded(filepath.Join("/finalizer", key, "state.json"), 64<<10)
	if e == nil {
		if json.Unmarshal(b, &state) != nil {
			return errors.New("invalid finalizer state")
		}
		out.FinalizerStatus = state.Status
		out.FinalizerStage = state.Stage
		out.FinalizerTries = state.Attempts
		out.FinalizerDue = state.Next
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	var d struct {
		Status string     `json:"status"`
		Next   *time.Time `json:"next_retry_at"`
	}
	b, e = proof.ReadBounded(filepath.Join("/delivery", key+".json"), 64<<10)
	if e == nil {
		if json.Unmarshal(b, &d) != nil {
			return errors.New("invalid delivery state")
		}
		out.DeliveryStatus = d.Status
		out.DeliveryDue = d.Next
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	b, e = proof.ReadBounded(filepath.Join("/input", key, "result.json"), 16<<10)
	if e == nil {
		result, e := finalizer.ParseResult(b)
		if e != nil || watchdog.Key(result.Identity) != key {
			return errors.New("invalid published identity")
		}
		out.Published = true
		resp, e := client().Get("http://api:8080/v1/receipts?run_id=" + result.Identity.Run + "&limit=100")
		if e == nil {
			defer resp.Body.Close()
			if resp.StatusCode == 200 {
				var page struct {
					Items []struct {
						ID      string      `json:"id"`
						Receipt archive.Ref `json:"receipt_object"`
						Bundle  archive.Ref `json:"bundle_object"`
					} `json:"items"`
				}
				if json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&page) == nil {
					for _, r := range page.Items {
						if r.Receipt == result.Receipt && r.Bundle == result.Bundle {
							out.ReceiptID = r.ID
						}
					}
				}
			}
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(out)
}
