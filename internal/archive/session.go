package archive

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"time"

	"github.com/minio/minio-go/v7/pkg/credentials"
)

var awsKMSARN = regexp.MustCompile(`^arn:aws:kms:[a-z0-9-]+:[0-9]{12}:key/[a-f0-9-]{36}$`)

// The trusted host rotates this private file atomically. There is deliberately
// no environment, shared AWS profile, metadata endpoint or URL fallback.
// Re-reading each request also permits immediate replacement after revocation.
type sessionProvider struct{ path string }

func (p *sessionProvider) IsExpired() bool { return true }
func (p *sessionProvider) RetrieveWithCredContext(_ *credentials.CredContext) (credentials.Value, error) {
	return p.Retrieve()
}
func (p *sessionProvider) Retrieve() (credentials.Value, error) {
	var empty credentials.Value
	b, err := readFile(p.path, 16<<10)
	if err != nil {
		return empty, errors.New("AWS archive session unavailable")
	}
	var s struct {
		AccessKeyID     string    `json:"AccessKeyId"`
		SecretAccessKey string    `json:"SecretAccessKey"`
		SessionToken    string    `json:"SessionToken"`
		Expiration      time.Time `json:"Expiration"`
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&s) != nil || d.Decode(new(any)) != io.EOF || s.AccessKeyID == "" || s.SecretAccessKey == "" || s.SessionToken == "" || !s.Expiration.After(time.Now().Add(30*time.Second)) {
		return empty, errors.New("AWS archive session invalid or expired")
	}
	return credentials.Value{AccessKeyID: s.AccessKeyID, SecretAccessKey: s.SecretAccessKey, SessionToken: s.SessionToken, SignerType: credentials.SignatureV4}, nil
}
