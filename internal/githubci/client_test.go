package githubci

import (
	"context"
	"fmt"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/coordinator"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestVerifyNeverAcceptsWrongAssignmentOrMissingAPI(t *testing.T) {
	for _, test := range []struct {
		name                       string
		runner                     int
		status                     int
		wantMismatch, wantVerified bool
	}{
		{"matching", 27, 200, false, true}, {"different runner", 28, 200, true, false}, {"API unavailable", 27, 503, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			token := filepath.Join(t.TempDir(), "token")
			if err := os.WriteFile(token, []byte("test-token"), 0600); err != nil {
				t.Fatal(err)
			}
			sha := strings.Repeat("a", 40)
			c := Client{TokenFile: token, Repository: "owner/repo", Revision: sha, Branch: "aws-github-finals"}
			c.HTTP = &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
				status := test.status
				body := "{}"
				if strings.Contains(req.URL.Path, "/actions/runs/") {
					body = fmt.Sprintf(`{"id":123,"run_attempt":1,"head_sha":%q,"head_branch":"aws-github-finals","event":"push","path":".github/workflows/cleanup-aws.yml","repository":{"full_name":"owner/repo"},"head_repository":{"full_name":"owner/repo"}}`, sha)
				}
				if strings.Contains(req.URL.Path, "/actions/jobs/") {
					body = fmt.Sprintf(`{"id":99,"run_id":123,"run_attempt":1,"head_sha":%q,"name":"guarded","status":"completed","runner_id":%d,"runner_name":"proof-gh-123-1"}`, sha, test.runner)
				}
				if strings.Contains(req.URL.Path, "/actions/runners/") {
					status = 404
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
			})}
			l := &coordinator.Ledger{Identity: Identity(&c, Run{ID: 123, Attempt: 1, SHA: sha}), GitHub: &coordinator.GitHubRun{JobID: 99, RunnerID: 27, RunnerName: "proof-gh-123-1"}}
			err := c.Verify(context.Background(), l)
			if (err == nil) != test.wantVerified || l.GitHubAssignmentMismatch != test.wantMismatch || l.GitHubRunnerAbsent != test.wantVerified || l.GitHubAssignmentConfirmed != test.wantVerified {
				t.Fatalf("wrong verification outcome: err=%v ledger=%+v", err, l)
			}
		})
	}
}
