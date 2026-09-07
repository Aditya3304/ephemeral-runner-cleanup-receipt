// Finalizer is built into its own approved image; it never loads job code.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"syscall"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/archive"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/finalizer"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
	jcs "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

// Set only by the approved image build: -ldflags '-X main.buildRevision=<commit>'.
// A missing build revision always fails closed, including development invocations.
var buildRevision string

type config struct {
	InputDir      string                 `json:"input_dir"`
	StateDir      string                 `json:"state_dir"`
	OutputDir     string                 `json:"output_dir"`
	ArchiveConfig string                 `json:"archive_config"`
	CosignBinary  string                 `json:"cosign_binary"`
	SigningConfig string                 `json:"signing_config"`
	TrustRoot     string                 `json:"trust_root"`
	TLSCAFile     string                 `json:"tls_ca_file"`
	Policy        finalizer.SignerPolicy `json:"policy"`
	Token         finalizer.TokenConfig  `json:"token"`
}

func loadConfig(path string) (config, error) {
	var c config
	b, err := proof.ReadBounded(path, 1<<20)
	if err != nil {
		return c, err
	}
	if _, err = jcs.Transform(b); err != nil {
		return c, err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err = d.Decode(&c); err != nil {
		return c, err
	}
	if d.Decode(new(any)) != io.EOF {
		return c, errors.New("trailing operator configuration")
	}
	if !regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`).MatchString(buildRevision) || c.Policy.Revision != buildRevision {
		return c, errors.New("operator finalizer revision does not match embedded approved build revision")
	}
	if c.Policy.Issuer != "https://issuer:8443" || c.Policy.Identity != "finalizer@cleanup-receipt.local" || c.Token.Endpoint != "https://issuer:8443/token" {
		return c, errors.New("unsupported local finalizer identity profile")
	}
	return c, nil
}
func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("finalizer", flag.ContinueOnError)
	cfg := flags.String("config", "/run/finalizer/config.json", "operator-mounted configuration")
	runID := flags.String("run", "", "coordinator allocated run ID")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	c, err := loadConfig(*cfg)
	if err != nil {
		return err
	}
	ac, err := archive.LoadConfig(c.ArchiveConfig)
	if err != nil {
		return errors.New("cannot load operator archive configuration")
	}
	ar, err := archive.New(ac)
	if err != nil {
		return errors.New("cannot initialize approved archive")
	}
	signer := &finalizer.Cosign{Binary: c.CosignBinary, SigningConfig: c.SigningConfig, TrustRoot: c.TrustRoot, TLSCAFile: c.TLSCAFile, Policy: c.Policy, Tokens: finalizer.MTLSProvider{Config: c.Token}}
	engine := &finalizer.Engine{InputDir: c.InputDir, StateDir: c.StateDir, OutputDir: c.OutputDir, Policy: c.Policy, Archive: ar, Signer: signer}
	result, err := engine.Run(ctx, *runID)
	if err != nil {
		return err
	}
	b, err := proof.Canonical(result)
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "finalizer:", err)
		os.Exit(1)
	}
}
