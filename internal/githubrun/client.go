package githubrun

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
)

// Config is installed by the operator, not read from an artifact or webhook.
// Job keys are GITHUB_JOB; values are exact REST job names. Matrix jobs are not
// supported by this profile; ambiguous matches fail closed.
type Config struct {
	Repository   string            `json:"repository"`
	WorkflowID   int64             `json:"workflow_id"`
	WorkflowPath string            `json:"workflow_path"`
	Jobs         map[string]string `json:"jobs"`
	Since        time.Time         `json:"since"`
	TokenFile    string            `json:"token_file"`
}

func (c Config) Validate() error {
	if !repoPattern.MatchString(c.Repository) || c.WorkflowID <= 0 || c.Since.IsZero() || !filepath.IsAbs(c.TokenFile) || len(c.Jobs) == 0 || len(c.Jobs) > 50 {
		return errors.New("invalid GitHub operator config")
	}
	names := map[string]bool{}
	for key, name := range c.Jobs {
		if !jobPattern.MatchString(key) || name == "" || len(name) > 255 || names[name] {
			return errors.New("invalid or duplicate job mapping")
		}
		names[name] = true
	}
	l := Ledger{Kind: Kind, Identity: proof.Identity{Provider: "github", Repository: c.Repository, Run: "1", Attempt: 1, Job: "job", Revision: strings.Repeat("a", 40)}, WorkflowID: c.WorkflowID, WorkflowPath: c.WorkflowPath, JobName: "job", Conclusion: "success", StartedAt: c.Since, CompletedAt: c.Since, EvidenceStatus: "missing", LogsStatus: "missing"}
	return l.Validate()
}

type Client struct {
	Config Config
	http   *http.Client
	base   string
	token  string
	Now    func() time.Time
}

func New(c Config) (*Client, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	b, err := proof.ReadBounded(c.TokenFile, 16<<10)
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(string(b))
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return nil, errors.New("invalid GitHub token")
	}
	return &Client{Config: c, http: &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, base: "https://api.github.com", token: token}, nil
}
func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

var errMissing = errors.New("GitHub object missing or expired")
var errOversized = errors.New("GitHub response exceeds bound")
var ErrPending = errors.New("GitHub run still active or within collection grace period")

func safeDownload(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.User == nil && u.Port() == "" && (strings.HasSuffix(u.Hostname(), ".blob.core.windows.net") || strings.HasSuffix(u.Hostname(), ".actions.githubusercontent.com"))
}
func (c *Client) get(ctx context.Context, path string, limit int64, download bool) ([]byte, error) {
	address := c.base + "/repos/" + c.Config.Repository + path
	for redirects := 0; redirects < 4; redirects++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			return nil, errors.New("invalid GitHub request")
		}
		if redirects == 0 {
			req.Header.Set("Authorization", "Bearer "+c.token)
			req.Header.Set("Accept", "application/vnd.github+json")
			req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		}
		res, err := c.http.Do(req)
		if err != nil {
			return nil, errors.New("GitHub request unavailable")
		} // Do not leak signed URLs/tokens.
		if res.StatusCode == 301 || res.StatusCode == 302 || res.StatusCode == 307 || res.StatusCode == 308 {
			location := res.Header.Get("Location")
			res.Body.Close()
			if !download || !safeDownload(location) {
				return nil, errors.New("untrusted GitHub download redirect")
			}
			address = location
			continue
		}
		if res.StatusCode != 200 {
			res.Body.Close()
			if res.StatusCode == 404 || res.StatusCode == 410 {
				return nil, errMissing
			}
			return nil, fmt.Errorf("GitHub API status %d", res.StatusCode)
		}
		b, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
		res.Body.Close()
		if err != nil {
			return nil, errors.New("GitHub response interrupted")
		}
		if int64(len(b)) > limit {
			return nil, errOversized
		}
		return b, nil
	}
	return nil, errors.New("too many GitHub download redirects")
}
func (c *Client) json(ctx context.Context, path string, out any) error {
	b, err := c.get(ctx, path, 8<<20, false)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

type run struct {
	ID         int64     `json:"id"`
	Attempt    int       `json:"run_attempt"`
	WorkflowID int64     `json:"workflow_id"`
	Path       string    `json:"path"`
	SHA        string    `json:"head_sha"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	Created    time.Time `json:"created_at"`
	Updated    time.Time `json:"updated_at"`
	Repository struct {
		Name string `json:"full_name"`
	} `json:"repository"`
}
type job struct {
	ID         int64  `json:"id"`
	RunID      int64  `json:"run_id"`
	Attempt    int    `json:"run_attempt"`
	SHA        string `json:"head_sha"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}
type artifact struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Expired bool   `json:"expired"`
	Digest  string `json:"digest"`
	Run     struct {
		ID  int64  `json:"id"`
		SHA string `json:"head_sha"`
	} `json:"workflow_run"`
}

// Discover uses the configured workflow, not user-supplied run URLs. Every
// attempt is enumerated, including attempts preceding a rerun while offline.
// A saturated API window is an explicit error, never a silent first-page scan.
func (c *Client) Discover(ctx context.Context) ([]proof.Identity, error) {
	ids := []proof.Identity{}
	for page := 1; page <= 10; page++ {
		var response struct {
			Runs []run `json:"workflow_runs"`
		}
		path := fmt.Sprintf("/actions/workflows/%d/runs?per_page=100&page=%d&created=%s", c.Config.WorkflowID, page, url.QueryEscape(">="+c.Config.Since.Format(time.RFC3339)))
		if err := c.json(ctx, path, &response); err != nil {
			return nil, err
		}
		for _, r := range response.Runs {
			if r.Status != "completed" {
				continue
			}
			if r.ID <= 0 || r.Attempt < 1 || r.Attempt > 100 || r.WorkflowID != c.Config.WorkflowID || r.Path != c.Config.WorkflowPath || r.Repository.Name != c.Config.Repository {
				return nil, errors.New("discovery identity mismatch")
			}
			if c.now().Before(r.Updated.Add(2 * time.Minute)) {
				continue
			}
			for attempt := 1; attempt <= r.Attempt; attempt++ {
				for key := range c.Config.Jobs {
					id := proof.Identity{Provider: "github", Repository: c.Config.Repository, Run: strconv.FormatInt(r.ID, 10), Attempt: attempt, Job: key, Revision: r.SHA}
					if !ValidIdentity(id) {
						return nil, errors.New("invalid discovered identity")
					}
					ids = append(ids, id)
				}
			}
		}
		if len(response.Runs) < 100 {
			sort.Slice(ids, func(i, j int) bool { return Directory(ids[i]) < Directory(ids[j]) })
			return ids, nil
		}
	}
	return ids, errors.New("GitHub discovery window saturated; older runs require explicit backfill before narrowing since")
}

func unpack(data []byte) ([]byte, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	if len(z.File) < 1 || len(z.File) > 2 {
		return nil, errors.New("unexpected artifact members")
	}
	seen := map[string]bool{}
	var evidence []byte
	for _, f := range z.File {
		limit := int64(MaxEvidence)
		if f.Name == "guard.json" {
			limit = 16 << 10
		} else if f.Name != "evidence.json" {
			return nil, errors.New("unexpected artifact path")
		}
		if seen[f.Name] || f.Mode()&os.ModeType != 0 || f.UncompressedSize64 > uint64(limit) {
			return nil, errors.New("unsafe artifact member")
		}
		seen[f.Name] = true
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(r, limit+1))
		r.Close()
		if err != nil || int64(len(b)) > limit {
			return nil, errors.New("invalid artifact contents")
		}
		if f.Name == "evidence.json" {
			evidence = b
		}
	}
	return evidence, nil
}

func (c *Client) Collect(ctx context.Context, id proof.Identity) (*Ledger, map[string][]byte, error) {
	if !ValidIdentity(id) || id.Repository != c.Config.Repository || c.Config.Jobs[id.Job] == "" {
		return nil, nil, errors.New("identity outside operator policy")
	}
	var r run
	path := fmt.Sprintf("/actions/runs/%s/attempts/%d", id.Run, id.Attempt)
	if err := c.json(ctx, path, &r); err != nil {
		return nil, nil, err
	}
	if strconv.FormatInt(r.ID, 10) != id.Run || r.Attempt != id.Attempt || r.SHA != id.Revision || r.Repository.Name != id.Repository || r.WorkflowID != c.Config.WorkflowID || r.Path != c.Config.WorkflowPath {
		return nil, nil, errors.New("run identity mismatch")
	}
	if r.Status != "completed" || c.now().Before(r.Updated.Add(2*time.Minute)) {
		return nil, nil, ErrPending
	}
	l := &Ledger{Kind: Kind, Identity: id, WorkflowID: r.WorkflowID, WorkflowPath: r.Path, JobName: c.Config.Jobs[id.Job], Conclusion: r.Conclusion, StartedAt: r.Created, CompletedAt: r.Updated, EvidenceStatus: "missing", LogsStatus: "missing"}
	jobs := []job{}
	for page := 1; ; page++ {
		var response struct {
			Jobs []job `json:"jobs"`
		}
		if err := c.json(ctx, fmt.Sprintf("%s/jobs?per_page=100&page=%d", path, page), &response); err != nil {
			return nil, nil, err
		}
		jobs = append(jobs, response.Jobs...)
		if len(response.Jobs) < 100 {
			break
		}
		if page == 10 {
			return nil, nil, errors.New("too many jobs")
		}
	}
	for _, j := range jobs {
		if j.Name == l.JobName {
			// run_attempt is optional in GitHub's job response. The attempt-
			// scoped endpoint above is authoritative; check the field if sent.
			if l.JobID != 0 || j.ID <= 0 || j.RunID != r.ID || j.Attempt != 0 && j.Attempt != id.Attempt || j.SHA != id.Revision || j.Status != "completed" {
				return nil, nil, errors.New("ambiguous or mismatched job")
			}
			l.JobID = j.ID
			l.Conclusion = j.Conclusion
		}
	}
	files := map[string][]byte{}
	// Skipped/unallocated configured jobs still receive an explicit partial receipt.
	if l.JobID != 0 {
		logs, err := c.get(ctx, fmt.Sprintf("/actions/jobs/%d/logs", l.JobID), MaxLogs, true)
		if err == nil && len(logs) > 0 {
			files["job.log"] = logs
			l.LogsStatus = "collected"
			l.LogsDigest = proof.Digest(logs)
			l.LogsBytes = int64(len(logs))
		} else if errors.Is(err, errOversized) {
			l.LogsStatus = "oversized"
		} else if err != nil && !errors.Is(err, errMissing) {
			return nil, nil, err
		}
	}
	expected := fmt.Sprintf("cleanup-preliminary-%s-%d-%s", id.Run, id.Attempt, id.Job)
	if len(expected) > 120 {
		expected = expected[:120]
	}
	var found *artifact
	for page := 1; ; page++ {
		var response struct {
			Artifacts []artifact `json:"artifacts"`
		}
		if err := c.json(ctx, fmt.Sprintf("/actions/runs/%s/artifacts?per_page=100&page=%d&name=%s", id.Run, page, url.QueryEscape(expected)), &response); err != nil {
			return nil, nil, err
		}
		for _, a := range response.Artifacts {
			if a.Name != expected {
				continue
			}
			if found != nil {
				return nil, nil, errors.New("ambiguous preliminary artifact")
			}
			copy := a
			found = &copy
		}
		if len(response.Artifacts) < 100 {
			break
		}
		if page == 10 {
			return nil, nil, errors.New("too many artifacts")
		}
	}
	if found != nil && !found.Expired && l.JobID != 0 {
		if found.ID <= 0 || found.Run.ID != r.ID || found.Run.SHA != id.Revision {
			return nil, nil, errors.New("artifact run binding mismatch")
		}
		b, err := c.get(ctx, fmt.Sprintf("/actions/artifacts/%d/zip", found.ID), 2<<20, true)
		if err != nil && !errors.Is(err, errMissing) && !errors.Is(err, errOversized) {
			return nil, nil, err
		}
		if errors.Is(err, errOversized) {
			l.EvidenceStatus = "invalid"
		} else if err == nil {
			l.EvidenceStatus = "invalid"
			if found.Digest != "" && found.Digest != "sha256:"+proof.Digest(b) {
				return nil, nil, errors.New("artifact transport digest mismatch")
			}
			evidence, unpackErr := unpack(b)
			if unpackErr == nil && len(evidence) > 0 {
				files["cleanup-evidence.json"] = evidence
				l.EvidenceDigest = proof.Digest(evidence)
				if ValidateEvidence(evidence, l) == nil {
					l.EvidenceStatus = "accepted-untrusted"
				}
			}
		}
	}
	if err := l.Validate(); err != nil {
		return nil, nil, err
	}
	data, err := proof.Canonical(l)
	if err != nil {
		return nil, nil, err
	}
	files["run.json"] = data
	return l, files, ValidateFiles(l, files)
}

// Publish atomically commits a whole snapshot. A retry reads the old snapshot;
// it must not recollect changed/expired artifacts after signing has begun.
func Publish(root string, l *Ledger, files map[string][]byte) error {
	if !filepath.IsAbs(root) {
		return errors.New("absolute GitHub input root required")
	}
	if err := ValidateFiles(l, files); err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil || resolved != filepath.Clean(root) {
		return errors.New("GitHub root traverses symlink")
	}
	if err = os.Chmod(root, 0755); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(root, ".collect-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	for name, data := range files {
		if err = writeSync(filepath.Join(dir, name), data); err != nil {
			return err
		}
	}
	if err = writeSync(filepath.Join(dir, "finalization-ready.json"), Ready(files)); err != nil {
		return err
	}
	// Finalizer containers run as uid 65532; inputs are readable, never writable.
	if err = os.Chmod(dir, 0755); err != nil {
		return err
	}
	if err = syncDir(dir); err != nil {
		return err
	}
	if err = os.Rename(dir, filepath.Join(root, Directory(l.Identity))); err != nil {
		return err
	}
	return syncDir(root)
}
func writeSync(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	// The installed service uses UMask=0077. These public input files must
	// still be readable by the separate uid-65532 finalizer's read-only mount.
	if err == nil {
		err = f.Chmod(0644)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	return errors.Join(err, closeErr)
}
func syncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func Load(root string, id proof.Identity) (*Ledger, error) {
	dir := filepath.Join(root, Directory(id))
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	if resolved != dir {
		return nil, errors.New("GitHub input symlink")
	}
	b, err := proof.ReadBounded(filepath.Join(dir, "run.json"), 1<<20)
	if err != nil {
		return nil, err
	}
	l, err := Parse(b)
	if err != nil {
		return nil, err
	}
	if l.Identity != id {
		return nil, errors.New("GitHub snapshot identity conflict")
	}
	return l, nil
}
