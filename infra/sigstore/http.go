// Small standard-library-only HTTP client for private service readiness and mTLS.
// Never prints token responses. No credentials or dependencies are downloaded.
package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	ca := flag.String("ca", "", "server CA PEM")
	cert := flag.String("cert", "", "client certificate")
	key := flag.String("key", "", "client private key")
	method := flag.String("method", "GET", "HTTP method")
	out := flag.String("out", "", "response file, otherwise discard")
	field := flag.String("json-field", "", "extract one JSON string field")
	status := flag.Int("status", 200, "required HTTP status")
	flag.Parse()
	if flag.NArg() != 1 {
		return fmt.Errorf("one URL required")
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if *ca != "" {
		pem, err := os.ReadFile(*ca)
		if err != nil {
			return err
		}
		cfg.RootCAs = x509.NewCertPool()
		if !cfg.RootCAs.AppendCertsFromPEM(pem) {
			return fmt.Errorf("invalid server CA")
		}
	}
	if *cert != "" {
		c, err := tls.LoadX509KeyPair(*cert, *key)
		if err != nil {
			return err
		}
		cfg.Certificates = []tls.Certificate{c}
	}
	client := &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{TLSClientConfig: cfg}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequest(*method, flag.Arg(0), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != *status {
		return fmt.Errorf("unexpected HTTP status %d (want %d)", resp.StatusCode, *status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil {
		return err
	}
	if len(data) > 2<<20 {
		return fmt.Errorf("response exceeds 2 MiB")
	}
	if *field != "" {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(data, &obj); err != nil {
			return err
		}
		var value string
		if err := json.Unmarshal(obj[*field], &value); err != nil {
			return err
		}
		if value == "" {
			return fmt.Errorf("empty response field")
		}
		data = []byte(value)
	}
	if *out != "" {
		return os.WriteFile(*out, data, 0600)
	}
	return nil
}
