// Package archive stores bounded evidence in a configured, TLS-only MinIO archive.
// References never select a network endpoint or credentials.
package archive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/encrypt"
	"golang.org/x/sys/unix"
)

const MaxObjectBytes int64 = 100 << 20

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var componentPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

// Ref is the frozen database/proof evidence storage reference. It has no URL.
type Ref struct {
	Store     string `json:"store"`
	Bucket    string `json:"bucket"`
	Key       string `json:"key"`
	VersionID string `json:"version_id"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
}

func (r Ref) Validate() error {
	if !componentPattern.MatchString(r.Store) || !componentPattern.MatchString(r.Bucket) || !digestPattern.MatchString(r.SHA256) || r.VersionID == "" || r.VersionID == "null" || len(r.VersionID) > 256 || strings.ContainsAny(r.VersionID, "\r\n\x00") || r.SizeBytes < 0 || r.SizeBytes > MaxObjectBytes {
		return errors.New("invalid archive reference")
	}
	if len(r.Key) > 1024 || !strings.HasSuffix(r.Key, "/sha256/"+r.SHA256) || strings.Contains(r.Key, "..") || strings.ContainsAny(r.Key, "\\\r\n\x00") {
		return errors.New("invalid digest-addressed archive key")
	}
	return nil
}

type Config struct {
	Store          string `json:"store"`
	Endpoint       string `json:"endpoint"`
	Bucket         string `json:"bucket"`
	Prefix         string `json:"prefix"`
	Region         string `json:"region"`
	CAFile         string `json:"ca_file"`
	AccessKeyFile  string `json:"access_key_file"`
	SecretKeyFile  string `json:"secret_key_file"`
	KMSKey         string `json:"kms_key"`
	MaxObjectBytes int64  `json:"max_object_bytes"`
}

func readFile(path string, limit int64) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() > limit {
		return nil, errors.New("archive input must be a bounded regular file")
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(b)) > limit {
		return nil, errors.New("archive input too large")
	}
	return b, err
}

func LoadConfig(path string) (Config, error) {
	var c Config
	b, err := readFile(path, 64<<10)
	if err != nil {
		return c, err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err = d.Decode(&c); err != nil {
		return c, err
	}
	if d.Decode(new(any)) != io.EOF {
		return c, errors.New("trailing archive config JSON")
	}
	for _, p := range []*string{&c.CAFile, &c.AccessKeyFile, &c.SecretKeyFile} {
		if *p != "" && !filepath.IsAbs(*p) {
			*p = filepath.Join(filepath.Dir(path), *p)
		}
	}
	return c, nil
}

type Client struct {
	s3        *minio.Client
	config    Config
	transport *http.Transport
}

func New(c Config) (*Client, error) {
	u, err := url.Parse(c.Endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("archive endpoint must be an operator-configured HTTPS origin")
	}
	if !componentPattern.MatchString(c.Store) || !componentPattern.MatchString(c.Bucket) || !componentPattern.MatchString(c.Prefix) || c.Region == "" || c.KMSKey == "" || c.MaxObjectBytes <= 0 || c.MaxObjectBytes > MaxObjectBytes {
		return nil, errors.New("invalid archive configuration or size limit")
	}
	ca, err := readFile(c.CAFile, 1<<20)
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("invalid archive CA")
	}
	access, err := readFile(c.AccessKeyFile, 4096)
	if err != nil {
		return nil, err
	}
	secret, err := readFile(c.SecretKeyFile, 4096)
	if err != nil {
		return nil, err
	}
	a, s := strings.TrimSpace(string(access)), strings.TrimSpace(string(secret))
	if a == "" || s == "" {
		return nil, errors.New("empty archive credentials")
	}
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext, TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 30 * time.Second, IdleConnTimeout: 30 * time.Second, MaxIdleConns: 4, MaxConnsPerHost: 4, DisableCompression: true}
	client, err := minio.New(u.Host, &minio.Options{Creds: credentials.NewStaticV4(a, s, ""), Secure: true, Region: c.Region, Transport: transport, BucketLookup: minio.BucketLookupPath, MaxRetries: 2})
	if err != nil {
		return nil, err
	}
	return &Client{client, c, transport}, nil
}

func (c *Client) validate(r Ref) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if r.Store != c.config.Store || r.Bucket != c.config.Bucket || r.Key != c.config.Prefix+"/sha256/"+r.SHA256 || r.SizeBytes > c.config.MaxObjectBytes {
		return errors.New("reference outside configured archive")
	}
	return nil
}

// Put uses a conditional single-part write. Retrying identical bytes recovers
// the existing version, and returns only after reading and checking that version.
// name selects metadata only; it never contributes a path or endpoint.
func (c *Client) Put(ctx context.Context, name string, data []byte) (Ref, error) {
	var r Ref
	if int64(len(data)) > c.config.MaxObjectBytes {
		return r, errors.New("archive object exceeds configured limit")
	}
	if !componentPattern.MatchString(name) {
		return r, errors.New("invalid evidence name")
	}
	contentType := "application/octet-stream"
	if strings.HasSuffix(name, ".json") {
		contentType = "application/json"
	} else if strings.HasSuffix(name, ".log") {
		contentType = "text/plain"
	}
	sum := sha256.Sum256(data)
	r = Ref{c.config.Store, c.config.Bucket, c.config.Prefix + "/sha256/" + hex.EncodeToString(sum[:]), "", hex.EncodeToString(sum[:]), int64(len(data))}
	encryption, err := encrypt.NewSSEKMS(c.config.KMSKey, nil)
	if err != nil {
		return Ref{}, err
	}
	opts := minio.PutObjectOptions{ContentType: contentType, ServerSideEncryption: encryption, DisableMultipart: true}
	opts.SetMatchETagExcept("*")
	result, err := c.s3.PutObject(ctx, r.Bucket, r.Key, bytes.NewReader(data), int64(len(data)), opts)
	if err != nil {
		code := minio.ToErrorResponse(err).Code
		if code != "PreconditionFailed" && code != "ConditionalRequestConflict" {
			return Ref{}, fmt.Errorf("archive upload: %w", err)
		}
		st, e := c.s3.StatObject(ctx, r.Bucket, r.Key, minio.StatObjectOptions{})
		if e != nil {
			return Ref{}, e
		}
		r.VersionID = st.VersionID
	} else {
		r.VersionID = result.VersionID
	}
	if err = c.Protect(ctx, r); err != nil {
		return Ref{}, fmt.Errorf("archive admission retention: %w", err)
	}
	return r, nil
}

// Protect admits an exact existing version for a new or resumed finalization.
// Historical verification uses Verify instead and never needs write credentials.
func (c *Client) Protect(ctx context.Context, r Ref) error {
	if err := c.Verify(ctx, r); err != nil {
		return err
	}
	return c.protectAdmission(ctx, r, time.Now())
}

// An old reference can still be read after its retention expires. Reusing it
// for a new receipt is a new admission and must renew its protection horizon.
func admissionDeadline(now time.Time) time.Time {
	return now.UTC().Add(7*24*time.Hour + time.Minute).Truncate(time.Second)
}
func needsExtension(until, now time.Time) bool { return until.Before(admissionDeadline(now)) }
func (c *Client) protectAdmission(ctx context.Context, r Ref, now time.Time) error {
	mode, until, err := c.s3.GetObjectRetention(ctx, r.Bucket, r.Key, r.VersionID)
	if err != nil {
		return err
	}
	if mode == nil || *mode != minio.Compliance || until == nil {
		return errors.New("COMPLIANCE retention required")
	}
	if needsExtension(*until, now) {
		deadline := admissionDeadline(now)
		err = c.s3.PutObjectRetention(ctx, r.Bucket, r.Key, minio.PutObjectRetentionOptions{VersionID: r.VersionID, Mode: mode, RetainUntilDate: &deadline})
		if err != nil {
			return err
		}
	}
	mode, until, err = c.s3.GetObjectRetention(ctx, r.Bucket, r.Key, r.VersionID)
	if err != nil {
		return err
	}
	if mode == nil || *mode != minio.Compliance || until == nil || until.Before(now.Add(7*24*time.Hour)) {
		return errors.New("new archive admission lacks seven days remaining retention")
	}
	return nil
}

func (c *Client) checkMetadata(ctx context.Context, r Ref) error {
	st, err := c.s3.StatObject(ctx, r.Bucket, r.Key, minio.StatObjectOptions{VersionID: r.VersionID})
	if err != nil {
		return err
	}
	if st.VersionID != r.VersionID || st.Size != r.SizeBytes {
		return errors.New("archive size/version mismatch")
	}
	keyID := st.Metadata.Get("X-Amz-Server-Side-Encryption-Aws-Kms-Key-Id")
	if st.Metadata.Get("X-Amz-Server-Side-Encryption") != "aws:kms" || (keyID != c.config.KMSKey && keyID != "arn:aws:kms:"+c.config.KMSKey) {
		return fmt.Errorf("archive SSE-KMS metadata mismatch: algorithm=%q key=%q", st.Metadata.Get("X-Amz-Server-Side-Encryption"), st.Metadata.Get("X-Amz-Server-Side-Encryption-Aws-Kms-Key-Id"))
	}
	if _, _, err = mime.ParseMediaType(st.ContentType); err != nil {
		return errors.New("invalid archive content type")
	}
	mode, until, err := c.s3.GetObjectRetention(ctx, r.Bucket, r.Key, r.VersionID)
	if err != nil {
		return err
	}
	// Retention may have naturally elapsed when an old receipt is verified.
	// Compare to creation time, not verification time.
	if mode == nil || *mode != minio.Compliance || until == nil || until.Before(st.LastModified.Add(7*24*time.Hour)) {
		return errors.New("archive seven-day COMPLIANCE retention missing")
	}
	return nil
}

// Read verifies the configured location, exact version, retention, encryption,
// size and SHA-256 before releasing bytes to the caller.
func (c *Client) Read(ctx context.Context, r Ref) ([]byte, error) {
	if err := c.validate(r); err != nil {
		return nil, err
	}
	if err := c.checkMetadata(ctx, r); err != nil {
		return nil, err
	}
	object, err := c.s3.GetObject(ctx, r.Bucket, r.Key, minio.GetObjectOptions{VersionID: r.VersionID})
	if err != nil {
		return nil, err
	}
	defer object.Close()
	b, err := io.ReadAll(io.LimitReader(object, r.SizeBytes+1))
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(b)
	if int64(len(b)) != r.SizeBytes || hex.EncodeToString(sum[:]) != r.SHA256 {
		return nil, errors.New("archive digest/size mismatch")
	}
	return b, nil
}
func (c *Client) Verify(ctx context.Context, r Ref) error { _, err := c.Read(ctx, r); return err }
