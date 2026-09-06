package proof

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	jcs "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const MaxAssignment = 16 << 10

// Assignment binds a job to exactly one coordinator-provisioned resource
// namespace. The runner itself must live in a separate namespace.
type Assignment struct {
	Kind         string   `json:"kind"`
	Identity     Identity `json:"identity"`
	Token        string   `json:"token"`
	Namespace    string   `json:"namespace"`
	NamespaceUID string   `json:"namespace_uid"`
	ClusterUID   string   `json:"cluster_uid"`
}

func (a Assignment) Validate() error {
	if a.Kind != "cleanup-assignment/v1" {
		return errors.New("unsupported assignment kind")
	}
	if err := a.Identity.Validate(); err != nil {
		return err
	}
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(a.Token) || a.Namespace != "proof-"+a.Token {
		return errors.New("invalid assignment token or namespace")
	}
	for _, uid := range []string{a.NamespaceUID, a.ClusterUID} {
		if strings.TrimSpace(uid) == "" || len(uid) > 255 {
			return errors.New("assignment cluster/namespace UID must be 1–255 bytes")
		}
	}
	return nil
}

// UnmarshalJSON also protects callers that decode assignments without using
// LoadAssignment. Keys must be present with their exact spelling; null, unknown
// and duplicate keys (including inside identity) are rejected.
func (a *Assignment) UnmarshalJSON(data []byte) error {
	if len(data) > MaxAssignment {
		return errors.New("assignment exceeds 16 KiB limit")
	}
	if _, err := jcs.Transform(data); err != nil {
		return fmt.Errorf("invalid/duplicate-key assignment JSON: %w", err)
	}
	fields, err := assignmentFields(data, []string{"kind", "identity", "token", "namespace", "namespace_uid", "cluster_uid"})
	if err != nil {
		return err
	}
	if _, err = assignmentFields(fields["identity"], []string{"provider", "repository", "run_id", "run_attempt", "job_id", "source_revision"}); err != nil {
		return fmt.Errorf("assignment identity: %w", err)
	}
	type plain Assignment
	var decoded plain
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err = d.Decode(&decoded); err != nil {
		return err
	}
	result := Assignment(decoded)
	if err = result.Validate(); err != nil {
		return err
	}
	*a = result
	return nil
}

func assignmentFields(data []byte, names []string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	if len(fields) != len(names) {
		return nil, errors.New("assignment has missing or unknown fields")
	}
	for _, name := range names {
		value, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, fmt.Errorf("assignment requires non-null %s", name)
		}
	}
	return fields, nil
}

func LoadAssignment(path string) (*Assignment, error) {
	data, err := ReadBounded(path, MaxAssignment)
	if err != nil {
		return nil, err
	}
	var a Assignment
	if err = json.Unmarshal(data, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// BeginAssigned performs only Kubernetes reads. All assignment and remote
// identity checks precede filesystem creation; an existing sandbox is refused.
func (e *Engine) BeginAssigned(ctx context.Context, a Assignment, sandboxBase string) (*State, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}
	if sandboxBase == "" {
		return nil, errors.New("sandbox base is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cluster, err := e.K.CoreV1().Namespaces().Get(ctx, "kube-system", meta.GetOptions{})
	if err != nil {
		return nil, err
	}
	if string(cluster.UID) != a.ClusterUID {
		return nil, errors.New("assignment cluster UID mismatch")
	}
	ns, err := e.K.CoreV1().Namespaces().Get(ctx, a.Namespace, meta.GetOptions{})
	if err != nil {
		return nil, err
	}
	if ns.Name != a.Namespace || string(ns.UID) != a.NamespaceUID || ns.Labels[Label] != a.Token {
		return nil, errors.New("assignment namespace ownership or UID mismatch")
	}
	if ns.DeletionTimestamp != nil || ns.Status.Phase == "Terminating" {
		return nil, errors.New("assigned namespace is terminating")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	// Reuse the ordinary begin defaults, especially the coverage semantics.
	s, err := NewState(a.Identity)
	if err != nil {
		return nil, err
	}
	s.Token, s.Namespace = a.Token, a.Namespace
	s.NamespaceUID, s.ClusterUID = a.NamespaceUID, a.ClusterUID
	s.NamespaceScoped = true
	if err = PrepareSandbox(sandboxBase, s); err != nil {
		return nil, err
	}
	return s, s.Validate()
}
