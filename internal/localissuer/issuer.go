// Package localissuer issues short-lived identities only to the locally trusted
// finalizer. It is deliberately not a general login or caller-selected identity API.
package localissuer

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const Identity = "finalizer@cleanup-receipt.local"
const ClientIdentity = "spiffe://cleanup-receipt.local/finalizer"
const Audience = "sigstore"

type Issuer struct {
	URL string
	key *rsa.PrivateKey
	kid string
	now func() time.Time
}

func New(issuerURL string, keyPEM []byte) (*Issuer, error) {
	u, err := url.Parse(issuerURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return nil, errors.New("issuer must be an HTTPS origin without a path or credentials")
	}
	block, rest := pem.Decode(keyPEM)
	if block == nil || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, errors.New("expected one PEM RSA key")
	}
	var key *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		var parsed any
		parsed, err = x509.ParsePKCS8PrivateKey(block.Bytes)
		if err == nil {
			var ok bool
			key, ok = parsed.(*rsa.PrivateKey)
			if !ok {
				err = errors.New("issuer key must be RSA")
			}
		}
	default:
		err = errors.New("unsupported issuer key format")
	}
	if err != nil {
		return nil, err
	}
	if key.N.BitLen() < 3072 {
		return nil, errors.New("issuer RSA key must have at least 3072 bits")
	}
	if err = key.Validate(); err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(der)
	return &Issuer{URL: issuerURL, key: key, kid: base64.RawURLEncoding.EncodeToString(sum[:]), now: time.Now}, nil
}

// TLSConfig permits public discovery while requiring a verified client chain for
// any supplied certificate. The token handler separately requires that chain.
func TLSConfig(clientCA []byte) (*tls.Config, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(clientCA) {
		return nil, errors.New("invalid finalizer client CA")
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.VerifyClientCertIfGiven, ClientCAs: pool}, nil
}

func (i *Issuer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		i.json(w, map[string]any{"issuer": i.URL, "jwks_uri": i.URL + "/jwks", "token_endpoint": i.URL + "/token", "response_types_supported": []string{"id_token"}, "subject_types_supported": []string{"public"}, "id_token_signing_alg_values_supported": []string{"RS256"}, "token_endpoint_auth_methods_supported": []string{"tls_client_auth"}, "scopes_supported": []string{"openid", "email"}, "claims_supported": []string{"iss", "sub", "aud", "iat", "nbf", "exp", "jti", "email", "email_verified"}})
	})
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, r *http.Request) {
		i.json(w, map[string]any{"keys": []any{map[string]any{"kty": "RSA", "use": "sig", "alg": "RS256", "kid": i.kid, "n": base64.RawURLEncoding.EncodeToString(i.key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(i.key.E)).Bytes())}}})
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready\n"))
	})
	mux.HandleFunc("POST /token", i.token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		mux.ServeHTTP(w, r)
	})
}

func (i *Issuer) json(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func (i *Issuer) token(w http.ResponseWriter, r *http.Request) {
	if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.PeerCertificates) == 0 {
		http.Error(w, "verified finalizer client certificate required", http.StatusUnauthorized)
		return
	}
	cert := r.TLS.PeerCertificates[0]
	if len(cert.URIs) != 1 || cert.URIs[0].String() != ClientIdentity {
		http.Error(w, "client identity is not authorized", http.StatusForbidden)
		return
	}
	// There is no caller-controlled subject, audience, duration or claim override.
	if r.URL.RawQuery != "" || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		http.Error(w, "token requests must have an empty body and no query", http.StatusBadRequest)
		return
	}
	now := i.now().UTC()
	if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
		http.Error(w, "client certificate expired", http.StatusUnauthorized)
		return
	}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		http.Error(w, "identity unavailable", http.StatusServiceUnavailable)
		return
	}
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": i.kid})
	claims, _ := json.Marshal(map[string]any{"iss": i.URL, "sub": "local-finalizer", "aud": Audience, "iat": now.Unix(), "nbf": now.Add(-5 * time.Second).Unix(), "exp": now.Add(120 * time.Second).Unix(), "jti": base64.RawURLEncoding.EncodeToString(nonce), "email": Identity, "email_verified": true})
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, i.key, crypto.SHA256, digest[:])
	if err != nil {
		http.Error(w, "identity unavailable", http.StatusServiceUnavailable)
		return
	}
	i.json(w, map[string]any{"id_token": fmt.Sprintf("%s.%s", unsigned, base64.RawURLEncoding.EncodeToString(signature)), "token_type": "Bearer", "expires_in": 120})
}
