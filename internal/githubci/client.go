// Package githubci binds a genuine one-job GitHub runner to trusted coordinator observations.
package githubci

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/coordinator"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
)

type Client struct {
	TokenFile, Repository, Revision, Branch string
	HTTP                                    *http.Client
}
type Run struct {
	ID         int64  `json:"id"`
	Attempt    int64  `json:"run_attempt"`
	SHA        string `json:"head_sha"`
	Branch     string `json:"head_branch"`
	Event      string `json:"event"`
	Path       string `json:"path"`
	Status     string `json:"status"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	HeadRepository struct {
		FullName string `json:"full_name"`
	} `json:"head_repository"`
}
type Job struct {
	ID         int64    `json:"id"`
	RunID      int64    `json:"run_id"`
	Attempt    int64    `json:"run_attempt"`
	Name       string   `json:"name"`
	SHA        string   `json:"head_sha"`
	Status     string   `json:"status"`
	RunnerID   int64    `json:"runner_id"`
	RunnerName string   `json:"runner_name"`
	Labels     []string `json:"labels"`
}
type Runner struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Busy   bool   `json:"busy"`
}
type StatusError int

// Status is separate from the workflow job conclusion: it is published only
// after the finalizer and receipt API have independently verified the artifact.
func (c *Client) Status(ctx context.Context, revision, state, description string) error {
	return c.api(ctx, "POST", "/statuses/"+revision, map[string]string{"state": state, "context": "cleanup/signed-receipt", "description": description}, nil)
}

func (e StatusError) Error() string { return fmt.Sprintf("GitHub API returned HTTP %d", int(e)) }
func (c *Client) api(ctx context.Context, method, path string, input, output any) error {
	st, err := os.Lstat(c.TokenFile)
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 || st.Size() > 16384 {
		return errors.New("GitHub token must be in a private bounded regular file")
	}
	token, err := os.ReadFile(c.TokenFile)
	if err != nil {
		return errors.New("cannot read GitHub operator token")
	}
	t := strings.TrimSpace(string(token))
	if t == "" || strings.ContainsAny(t, "\r\n") {
		return errors.New("invalid GitHub operator token")
	}
	var body io.Reader
	if input != nil {
		data, e := json.Marshal(input)
		if e != nil {
			return e
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://api.github.com/repos/"+c.Repository+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+t)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "cleanup-receipt-coordinator")
	req.Header.Set("Content-Type", "application/json")
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("GitHub API transport unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return StatusError(resp.StatusCode)
	}
	if output == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(output)
}
func Label(run, attempt int64) string { return fmt.Sprintf("proof-gh-%d-%d", run, attempt) }
func (c *Client) validateRun(r Run) error {
	if r.ID <= 0 || r.Attempt < 1 || r.Repository.FullName != c.Repository || r.HeadRepository.FullName != c.Repository || r.SHA != c.Revision || r.Branch != c.Branch || r.Path != ".github/workflows/cleanup-aws.yml" || (r.Event != "push" && r.Event != "workflow_dispatch") {
		return errors.New("run is outside the operator-approved source and workflow")
	}
	return nil
}
func (c *Client) Jobs(ctx context.Context, r Run) ([]Job, error) {
	var result struct {
		Jobs []Job `json:"jobs"`
	}
	err := c.api(ctx, "GET", fmt.Sprintf("/actions/runs/%d/attempts/%d/jobs?per_page=100", r.ID, r.Attempt), nil, &result)
	return result.Jobs, err
}
func (c *Client) Pending(ctx context.Context) (Run, Job, error) {
	var result struct {
		Runs []Run `json:"workflow_runs"`
	}
	err := c.api(ctx, "GET", "/actions/workflows/cleanup-aws.yml/runs?per_page=20", nil, &result)
	if err != nil {
		return Run{}, Job{}, err
	}
	for _, r := range result.Runs {
		if c.validateRun(r) != nil || (r.Status != "queued" && r.Status != "in_progress") {
			continue
		}
		jobs, e := c.Jobs(ctx, r)
		if e != nil {
			return Run{}, Job{}, e
		}
		for _, j := range jobs {
			if j.Name == "guarded" && j.Status == "queued" && j.RunID == r.ID && j.Attempt == r.Attempt && j.SHA == r.SHA && len(j.Labels) == 1 && j.Labels[0] == Label(r.ID, r.Attempt) {
				return r, j, nil
			}
		}
	}
	return Run{}, Job{}, nil
}
func (c *Client) Register(ctx context.Context, r Run, j Job, proxy string) (*coordinator.GitHubRun, string, error) {
	if err := c.validateRun(r); err != nil {
		return nil, "", err
	}
	if j.ID <= 0 || j.Name != "guarded" || j.RunID != r.ID || j.Attempt != r.Attempt || j.SHA != r.SHA {
		return nil, "", errors.New("job identity mismatch")
	}
	var result struct {
		Runner Runner `json:"runner"`
		Config string `json:"encoded_jit_config"`
	}
	name := Label(r.ID, r.Attempt)
	err := c.api(ctx, "POST", "/actions/runners/generate-jitconfig", map[string]any{"name": name, "runner_group_id": 1, "labels": []string{name}, "work_folder": "_work"}, &result)
	if err != nil {
		return nil, "", err
	}
	if result.Runner.ID <= 0 || result.Runner.Name != name || result.Config == "" || len(result.Config) > 65536 {
		return nil, "", errors.New("invalid GitHub JIT registration response")
	}
	return &coordinator.GitHubRun{JobID: j.ID, RunnerID: result.Runner.ID, RunnerName: name, Workflow: r.Path, ProxyIP: proxy}, result.Config, nil
}
func Identity(c *Client, r Run) proof.Identity {
	return proof.Identity{Provider: "github", Repository: c.Repository, Run: strconv.FormatInt(r.ID, 10), Attempt: int(r.Attempt), Job: "guarded", Revision: r.SHA}
}
func (c *Client) Verify(ctx context.Context, l *coordinator.Ledger) error {
	g := l.GitHub
	if g == nil {
		return errors.New("GitHub binding missing")
	}
	var r Run
	if err := c.api(ctx, "GET", "/actions/runs/"+l.Identity.Run+"/attempts/"+strconv.Itoa(l.Identity.Attempt), nil, &r); err != nil {
		return err
	}
	if c.validateRun(r) != nil || strconv.FormatInt(r.ID, 10) != l.Identity.Run || r.Attempt != int64(l.Identity.Attempt) {
		l.GitHubAssignmentMismatch = true
		return errors.New("GitHub run provenance mismatch")
	}
	deadline := time.Now().Add(45 * time.Second)
	for {
		var j Job
		if err := c.api(ctx, "GET", fmt.Sprintf("/actions/jobs/%d", g.JobID), nil, &j); err != nil {
			return err
		}
		if j.RunID != r.ID || j.Attempt != r.Attempt || j.SHA != l.Identity.Revision || j.Name != l.Identity.Job || j.RunnerID != g.RunnerID || j.RunnerName != g.RunnerName {
			l.GitHubAssignmentMismatch = true
			return errors.New("GitHub job assignment mismatch")
		}
		if j.Status == "completed" {
			l.GitHubAssignmentConfirmed = true
			break
		}
		if time.Now().After(deadline) {
			return errors.New("GitHub job termination not confirmed")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	path := fmt.Sprintf("/actions/runners/%d", g.RunnerID)
	var runner Runner
	err := c.api(ctx, "GET", path, nil, &runner)
	if err == StatusError(404) {
		l.GitHubRunnerAbsent = true
		return nil
	}
	if err != nil {
		return err
	}
	if runner.ID != g.RunnerID || runner.Name != g.RunnerName || runner.Busy {
		return errors.New("GitHub runner still busy or identity changed")
	}
	if err = c.api(ctx, "DELETE", path, nil, nil); err != nil && err != StatusError(404) {
		return err
	}
	if err = c.api(ctx, "GET", path, nil, &runner); err != StatusError(404) {
		return errors.New("GitHub runner deletion not confirmed")
	}
	l.GitHubRunnerAbsent = true
	return nil
}
