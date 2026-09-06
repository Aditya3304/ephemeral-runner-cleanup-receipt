package proof

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	jcs "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"golang.org/x/sys/unix"
)

const Label = "cleanup-receipt.local/run"
const MaxEvidence = 1 << 20

type Identity struct {
	Provider   string `json:"provider"`
	Repository string `json:"repository"`
	Run        string `json:"run_id"`
	Attempt    int    `json:"run_attempt"`
	Job        string `json:"job_id"`
	Revision   string `json:"source_revision"`
}
type Observation struct {
	Status   string `json:"status"`
	Observer string `json:"observer"`
	Reason   string `json:"reason"`
}
type Ref struct {
	Group     string `json:"group"`
	Version   string `json:"version"`
	Resource  string `json:"resource"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	UID       string `json:"uid"`
}
type DirIdentity struct {
	Device uint64 `json:"device,string"`
	Inode  uint64 `json:"inode,string"`
}
type State struct {
	Kind              string                 `json:"kind"`
	Identity          Identity               `json:"identity"`
	Token             string                 `json:"token"`
	Namespace         string                 `json:"namespace"`
	NamespaceUID      string                 `json:"namespace_uid"`
	ClusterUID        string                 `json:"cluster_uid"`
	Sandbox           string                 `json:"sandbox"`
	Directories       map[string]DirIdentity `json:"directories"`
	StartedAt         time.Time              `json:"started_at"`
	CompletedAt       *time.Time             `json:"completed_at"`
	InventoryComplete bool                   `json:"inventory_complete"`
	Resources         []Ref                  `json:"resources"`
	Coverage          map[string]Observation `json:"coverage"`
	CleanupAttempts   int                    `json:"cleanup_attempts"`
}

func NewState(id Identity) (*State, error) {
	if err := id.Validate(); err != nil {
		return nil, err
	}
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return nil, err
	}
	s := &State{Kind: "cleanup-context/v1", Identity: id, Token: hex.EncodeToString(token), StartedAt: time.Now().UTC(), Resources: []Ref{}, Coverage: map[string]Observation{}}
	s.Namespace = "proof-" + s.Token
	for _, key := range []string{"workspace", "credentials", "resources", "logs", "runner_disposal"} {
		s.Coverage[key] = Observation{"unobservable", "none", "Not observed by this command"}
	}
	return s, nil
}
func (id Identity) Validate() error {
	if id.Provider != "local" && id.Provider != "github" {
		return errors.New("provider must be local or github")
	}
	for _, v := range []string{id.Repository, id.Run, id.Job} {
		if len(v) == 0 || len(v) > 255 {
			return errors.New("repository, run and job must be 1–255 bytes")
		}
	}
	if id.Attempt < 1 || id.Attempt > 2147483647 {
		return errors.New("run attempt must fit a positive PostgreSQL integer")
	}
	if !regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`).MatchString(id.Revision) {
		return errors.New("source revision must be a full lowercase Git commit hash")
	}
	return nil
}
func (s *State) Validate() error {
	if s.Kind != "cleanup-context/v1" {
		return errors.New("unsupported context kind")
	}
	if err := s.Identity.Validate(); err != nil {
		return err
	}
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(s.Token) || s.Namespace != "proof-"+s.Token {
		return errors.New("invalid ownership token or namespace")
	}
	if s.ClusterUID == "" || s.NamespaceUID == "" {
		return errors.New("missing cluster/namespace UID")
	}
	if !filepath.IsAbs(s.Sandbox) || filepath.Base(s.Sandbox) != s.Namespace {
		return errors.New("invalid disposable sandbox path")
	}
	for _, key := range []string{"root", "workspace", "credentials"} {
		if s.Directories[key].Inode == 0 {
			return errors.New("missing pinned directory identity")
		}
	}
	for _, key := range []string{"workspace", "credentials", "resources", "logs", "runner_disposal"} {
		if _, ok := s.Coverage[key]; !ok {
			return errors.New("missing coverage component")
		}
		o := s.Coverage[key]
		if !contains([]string{"verified", "failed", "unsupported", "unobservable"}, o.Status) || !contains([]string{"job", "trusted", "none"}, o.Observer) || len(o.Reason) == 0 || len(o.Reason) > 1024 {
			return errors.New("invalid coverage observation")
		}
	}
	if len(s.Coverage) != 5 || len(s.Directories) != 3 {
		return errors.New("unexpected context components")
	}
	if s.StartedAt.IsZero() || s.CleanupAttempts < 0 || (s.CompletedAt != nil && s.CompletedAt.Before(s.StartedAt)) {
		return errors.New("invalid context chronology")
	}
	for _, r := range s.Resources {
		if r.UID == "" || r.Name == "" || r.Version == "" || r.Resource == "" {
			return errors.New("invalid resource reference")
		}
		if r.Namespace != s.Namespace && !(r.Namespace == "" && r.Group == "" && r.Version == "v1" && r.Resource == "persistentvolumes") {
			return errors.New("resource reference escapes allowed scope")
		}
	}
	return nil
}
func Canonical(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return jcs.Transform(data)
}
func Digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func ReadBounded(path string, limit int64) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("input must be a bounded regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("input exceeds size limit")
	}
	return data, nil
}
func Load(path string) (*State, error) {
	data, err := ReadBounded(path, MaxEvidence)
	if err != nil {
		return nil, err
	}
	if _, err = jcs.Transform(data); err != nil {
		return nil, fmt.Errorf("invalid/duplicate-key JSON: %w", err)
	}
	var s State
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err = d.Decode(&s); err != nil {
		return nil, err
	}
	if err = s.Validate(); err != nil {
		return nil, err
	}
	return &s, nil
}
func Save(path string, s *State) error {
	if err := s.Validate(); err != nil {
		return err
	}
	data, err := Canonical(s)
	if err != nil {
		return err
	}
	if len(data) > MaxEvidence {
		return errors.New("context exceeds 1 MiB limit")
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".proof-context-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, path)
}

// Control files must never be recreated inside a workspace just declared empty.
func ControlPath(path string, s *State) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(s.Sandbox, filepath.Join(parent, filepath.Base(abs)))
	if err != nil {
		return err
	}
	if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("state and evidence files must be outside the disposable sandbox")
	}
	return nil
}
func (s *State) Record(component, status, reason string) {
	// A previous cleanup failure remains visible even if a later retry removes it.
	if old := s.Coverage[component]; old.Status == "failed" && status != "failed" {
		return
	}
	if len(reason) > 1024 {
		reason = reason[:1024]
	}
	s.Coverage[component] = Observation{status, "job", reason}
}
func (s *State) Evidence() (map[string]any, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	verdict := "partial"
	for _, o := range s.Coverage {
		if o.Status == "failed" {
			verdict = "fail"
		}
	}
	return map[string]any{"kind": "cleanup-evidence/v1", "identity": s.Identity, "namespace": s.Namespace, "namespace_uid": s.NamespaceUID, "cluster_uid": s.ClusterUID, "started_at": s.StartedAt, "completed_at": s.CompletedAt, "cleanup_attempts": s.CleanupAttempts, "inventory_complete": s.InventoryComplete, "resources": s.Resources, "coverage": s.Coverage, "verdict": verdict, "signature_state": "unsigned"}, nil
}
