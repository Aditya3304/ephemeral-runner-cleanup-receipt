package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/localissuer"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	addr := flag.String("listen", ":8443", "Local internal TLS listener")
	origin := flag.String("issuer", "https://issuer:8443", "Fixed issuer origin")
	keyFile := flag.String("signing-key", "/run/issuer/signing.key", "Issuer RSA key")
	certFile := flag.String("tls-cert", "/run/issuer/server.crt", "Server TLS certificate")
	tlsKey := flag.String("tls-key", "/run/issuer/server.key", "Server TLS key")
	caFile := flag.String("client-ca", "/run/issuer/client-ca.crt", "Trusted finalizer client CA")
	flag.Parse()
	key, err := proof.ReadBounded(*keyFile, 32<<10)
	if err != nil {
		return err
	}
	i, err := localissuer.New(*origin, key)
	if err != nil {
		return err
	}
	ca, err := proof.ReadBounded(*caFile, 32<<10)
	if err != nil {
		return err
	}
	tlsConfig, err := localissuer.TLSConfig(ca)
	if err != nil {
		return err
	}
	server := &http.Server{Addr: *addr, Handler: i.Handler(), TLSConfig: tlsConfig, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServeTLS(*certFile, *tlsKey) }()
	select {
	case err = <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}
