package localissuer

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestIdentityRequiresVerifiedFinalizerCertificate(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := New("https://issuer:8443", pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	if err != nil {
		t.Fatal(err)
	}
	caKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test client CA"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, _ = x509.ParseCertificate(der)
	makeClient := func(identity string) tls.Certificate {
		u, _ := url.Parse(identity)
		cert := &x509.Certificate{SerialNumber: big.NewInt(2), NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), URIs: []*url.URL{u}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, KeyUsage: x509.KeyUsageDigitalSignature}
		clientDER, err := x509.CreateCertificate(rand.Reader, cert, ca, &caKey.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		return tls.Certificate{Certificate: [][]byte{clientDER, der}, PrivateKey: caKey}
	}
	server := httptest.NewUnstartedServer(issuer.Handler())
	server.TLS, err = TLSConfig(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	if err != nil {
		t.Fatal(err)
	}
	server.StartTLS()
	defer server.Close()
	client := server.Client()
	get := func(path string, want int) {
		r, err := client.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		if r.StatusCode != want {
			t.Fatalf("GET %s status %d", path, r.StatusCode)
		}
	}
	get("/.well-known/openid-configuration", 200)
	get("/jwks", 200)
	post := func(c *http.Client, path, body string, want int) []byte {
		r, err := c.Post(server.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		data, _ := io.ReadAll(r.Body)
		if r.StatusCode != want {
			t.Fatalf("token status %d, want %d: %s", r.StatusCode, want, data)
		}
		return data
	}
	post(client, "/token", "", 401)
	withCert := func(identity string) *http.Client {
		transport := client.Transport.(*http.Transport).Clone()
		transport.TLSClientConfig = transport.TLSClientConfig.Clone()
		transport.TLSClientConfig.Certificates = []tls.Certificate{makeClient(identity)}
		t.Cleanup(transport.CloseIdleConnections)
		return &http.Client{Transport: transport, Timeout: 5 * time.Second}
	}
	post(withCert("spiffe://cleanup-receipt.local/runner"), "/token", "", 403)
	trusted := withCert(ClientIdentity)
	post(trusted, "/token?sub=runner", "", 400)
	post(trusted, "/token", `{"email":"attacker@example.com"}`, 400)
	first := post(trusted, "/token", "", 200)
	second := post(trusted, "/token", "", 200)
	if string(first) == string(second) {
		t.Fatal("token nonce was reused")
	}
	var response struct {
		Token string `json:"id_token"`
	}
	if err = json.Unmarshal(first, &response); err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(response.Token, ".")
	if len(parts) != 3 {
		t.Fatal("invalid JWT")
	}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err = rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], sig); err != nil {
		t.Fatal(err)
	}
	claimsRaw, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims map[string]any
	if err = json.Unmarshal(claimsRaw, &claims); err != nil {
		t.Fatal(err)
	}
	if claims["iss"] != issuer.URL || claims["aud"] != Audience || claims["email"] != Identity || claims["email_verified"] != true || claims["sub"] != "local-finalizer" {
		t.Fatalf("incorrect fixed claims: %v", claims)
	}
	if claims["exp"].(float64)-claims["iat"].(float64) != 120 {
		t.Fatal("incorrect lifetime")
	}
	// Signature verification must reject even a single changed payload byte.
	digest = sha256.Sum256([]byte(parts[0] + "." + parts[1] + "x"))
	if rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], sig) == nil {
		t.Fatal("tampered JWT verified")
	}
}
