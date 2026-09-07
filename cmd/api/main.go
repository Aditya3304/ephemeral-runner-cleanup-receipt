package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/api"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/archive"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/database"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/finalizer"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if e := run(); e != nil {
		log.Print("API startup or server failure; inspect local configuration and service health")
		os.Exit(1)
	}
}
func run() error {
	path := flag.String("config", "/config/api.json", "operator configuration")
	flag.Parse()
	c, e := api.LoadConfig(*path)
	if e != nil {
		return e
	}
	token, e := proof.ReadBounded(c.TokenFile, 256)
	if e != nil {
		return e
	}
	secret := strings.TrimSpace(string(token))
	if len(secret) != 64 {
		return errors.New("invalid token")
	}
	ac, e := archive.LoadConfig(c.ArchiveConfig)
	if e != nil {
		return e
	}
	reader, e := archive.New(ac)
	if e != nil {
		return e
	}
	// Verify immutable operator inputs at startup; Cosign rechecks them per call.
	for p, sha := range map[string]string{c.CosignBinary: c.Policy.CosignSHA256, c.TrustRoot: c.Policy.TrustRootSHA256, c.SigningConfig: c.Policy.SigningConfigSHA256} {
		b, e := proof.ReadBounded(p, 256<<20)
		if e != nil || proof.Digest(b) != sha {
			return errors.New("operator digest mismatch")
		}
	}
	db, e := database.Config("proof", false)
	if e != nil {
		return e
	}
	db.RuntimeParams["application_name"] = "cleanup-receipt-api"
	pc, e := pgxpool.ParseConfig("")
	if e != nil {
		return e
	}
	pc.ConnConfig = db
	pc.MaxConns = 4
	pc.MinConns = 0
	pc.MaxConnLifetime = 5 * time.Minute
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, e := pgxpool.NewWithConfig(ctx, pc)
	if e != nil {
		return e
	}
	defer pool.Close()
	signer := &finalizer.Cosign{Binary: c.CosignBinary, SigningConfig: c.SigningConfig, TrustRoot: c.TrustRoot, Policy: c.Policy}
	app := &api.Server{Store: &api.Store{Pool: pool}, Verifier: &api.Verifier{Archive: reader, Signature: signer, Policy: c.Policy, Repository: c.Repository}, Token: secret}
	server := &http.Server{Addr: ":8080", Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 50 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	log.Print("Local receipt API listening; database readiness is reported separately")
	select {
	case e = <-done:
		if errors.Is(e, http.ErrServerClosed) {
			return nil
		}
		return e
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}
