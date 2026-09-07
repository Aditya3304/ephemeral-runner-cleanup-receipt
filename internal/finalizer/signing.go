package finalizer

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
	jcs "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

// TokenProvider is an operator-supplied identity source, never job configuration.
type TokenProvider interface {
	Token(context.Context) ([]byte, error)
}
type TokenConfig struct {
	Endpoint string `json:"endpoint"`
	CAFile   string `json:"ca_file"`
	CertFile string `json:"cert_file"`
	KeyFile  string `json:"key_file"`
}
type MTLSProvider struct{ Config TokenConfig }

func (p MTLSProvider) Token(ctx context.Context) ([]byte, error) {
	u, err := url.Parse(p.Config.Endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/token" {
		return nil, errors.New("invalid operator token endpoint")
	}
	ca, err := proof.ReadBounded(p.Config.CAFile, MaxReceipt)
	if err != nil {
		return nil, errors.New("cannot load token CA")
	}
	certPEM, err := proof.ReadBounded(p.Config.CertFile, MaxReceipt)
	if err != nil {
		return nil, errors.New("cannot load identity certificate")
	}
	key, err := proof.ReadBounded(p.Config.KeyFile, MaxReceipt)
	if err != nil {
		return nil, errors.New("cannot load identity key")
	}
	cert, err := tls.X509KeyPair(certPEM, key)
	if err != nil {
		return nil, errors.New("invalid identity keypair")
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, errors.New("invalid identity certificate")
	}
	if len(leaf.URIs) != 1 || leaf.URIs[0].String() != "spiffe://cleanup-receipt.local/finalizer" {
		return nil, errors.New("incorrect finalizer client identity")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("invalid token CA")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, Certificates: []tls.Certificate{cert}}, Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("token redirects forbidden") }}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), nil)
	if err != nil {
		return nil, errors.New("cannot construct token request")
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, errors.New("token endpoint unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("token endpoint rejected identity")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 16385))
	if err != nil || len(data) > 16384 {
		return nil, errors.New("invalid token response size")
	}
	var result struct {
		IDToken   string `json:"id_token"`
		ExpiresIn int    `json:"expires_in"`
		TokenType string `json:"token_type"`
	}
	if _, err = jcs.Transform(data); err != nil {
		return nil, errors.New("invalid token response JSON")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&result) != nil || d.Decode(new(any)) != io.EOF || result.ExpiresIn != 120 || result.TokenType != "Bearer" || len(result.IDToken) < 16 || len(result.IDToken) > 12000 || len(strings.Split(result.IDToken, ".")) != 3 || strings.ContainsAny(result.IDToken, "\r\n \t") {
		return nil, errors.New("invalid token response")
	}
	return []byte(result.IDToken), nil
}

// Signer must perform genuine signing; test doubles are only used by unit tests.
type Signer interface {
	Sign(context.Context, []byte) ([]byte, error)
	Verify(context.Context, []byte, []byte) error
}
type Cosign struct {
	Binary        string
	SigningConfig string
	TrustRoot     string
	TLSCAFile     string
	Policy        SignerPolicy
	Tokens        TokenProvider
}

func (s *Cosign) inputs() ([]byte, []byte, error) {
	if !filepath.IsAbs(s.Binary) || !filepath.IsAbs(s.SigningConfig) || !filepath.IsAbs(s.TrustRoot) {
		return nil, nil, errors.New("operator signing paths must be absolute")
	}
	binary, err := proof.ReadBounded(s.Binary, 256<<20)
	if err != nil || proof.Digest(binary) != s.Policy.CosignSHA256 {
		return nil, nil, errors.New("approved Cosign binary digest mismatch")
	}
	config, err := proof.ReadBounded(s.SigningConfig, MaxReceipt)
	if err != nil || proof.Digest(config) != s.Policy.SigningConfigSHA256 {
		return nil, nil, errors.New("operator signing configuration digest mismatch")
	}
	trust, err := proof.ReadBounded(s.TrustRoot, MaxReceipt)
	if err != nil || proof.Digest(trust) != s.Policy.TrustRootSHA256 {
		return nil, nil, errors.New("operator trust root digest mismatch")
	}
	return config, trust, nil
}
func (s *Cosign) privateInputs(data []byte) (string, error) {
	if len(data) > MaxReceipt {
		return "", errors.New("signing artifact exceeds bound")
	}
	config, trust, err := s.inputs()
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "finalizer-cosign-")
	if err != nil {
		return "", err
	}
	good := false
	defer func() {
		if !good {
			os.RemoveAll(dir)
		}
	}()
	for name, b := range map[string][]byte{"receipt.json": data, "signing.json": config, "trust.json": trust} {
		if err = os.WriteFile(filepath.Join(dir, name), b, 0600); err != nil {
			return "", err
		}
	}
	if s.TLSCAFile != "" {
		ca, e := proof.ReadBounded(s.TLSCAFile, MaxReceipt)
		if e != nil {
			return "", e
		}
		if e = os.WriteFile(filepath.Join(dir, "ca.pem"), ca, 0600); e != nil {
			return "", e
		}
	}
	good = true
	return dir, nil
}
func (s *Cosign) command(ctx context.Context, dir string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, s.Binary, args...)
	command.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + dir, "TMPDIR=" + dir}
	if s.TLSCAFile != "" {
		command.Env = append(command.Env, "SSL_CERT_FILE="+filepath.Join(dir, "ca.pem"))
	}
	// Never echo subprocess output: it may contain OIDC tokens or service secrets.
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	command.Stdin = nil
	if err := command.Run(); err != nil {
		return errors.New("Cosign operation failed (diagnostics withheld to protect credentials)")
	}
	return nil
}
func (s *Cosign) Sign(ctx context.Context, data []byte) ([]byte, error) {
	if s.Tokens == nil {
		return nil, errors.New("operator token provider required")
	}
	r, err := ParseReceipt(data)
	if err != nil {
		return nil, err
	}
	if r.Finalizer != s.Policy {
		return nil, errors.New("receipt does not match pinned finalizer policy")
	}
	dir, err := s.privateInputs(data)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	token, err := s.Tokens.Token(ctx)
	if err != nil {
		return nil, err
	}
	if len(token) < 16 || len(token) > 12000 {
		return nil, errors.New("token exceeds bounds")
	}
	tokenFile := filepath.Join(dir, "identity.token")
	if err = os.WriteFile(tokenFile, token, 0600); err != nil {
		return nil, err
	}
	err = s.command(ctx, dir, "sign-blob", "--yes", "--oidc-disable-ambient-providers", "--identity-token", tokenFile, "--signing-config", filepath.Join(dir, "signing.json"), "--trusted-root", filepath.Join(dir, "trust.json"), "--bundle", filepath.Join(dir, "receipt.bundle.json"), filepath.Join(dir, "receipt.json"))
	if err != nil {
		return nil, err
	}
	return proof.ReadBounded(filepath.Join(dir, "receipt.bundle.json"), MaxBundle)
}
func (s *Cosign) Verify(ctx context.Context, data, bundle []byte) error {
	receipt, err := ParseReceipt(data)
	if err != nil {
		return err
	}
	if receipt.Finalizer != s.Policy {
		return errors.New("receipt does not match pinned finalizer policy")
	}
	if len(bundle) == 0 || len(bundle) > MaxBundle {
		return errors.New("bundle exceeds bounds")
	}
	dir, err := s.privateInputs(data)
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if err = os.WriteFile(filepath.Join(dir, "receipt.bundle.json"), bundle, 0600); err != nil {
		return err
	}
	return s.command(ctx, dir, "verify-blob", "--offline", "--trusted-root", filepath.Join(dir, "trust.json"), "--bundle", filepath.Join(dir, "receipt.bundle.json"), "--certificate-identity", s.Policy.Identity, "--certificate-oidc-issuer", s.Policy.Issuer, filepath.Join(dir, "receipt.json"))
}
