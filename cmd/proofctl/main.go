package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := root().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func root() *cobra.Command {
	var statePath, kubeconfig, kubecontext string
	root := &cobra.Command{Use: "proofctl", Short: "Cleanup evidence for a dedicated ephemeral Kubernetes job", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().StringVar(&statePath, "state", "", "Context path outside the disposable workspace")
	root.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", "", "Kubernetes configuration file")
	root.PersistentFlags().StringVar(&kubecontext, "context", "", "Kubernetes context")
	lock := func() (func(), error) {
		if statePath == "" {
			return nil, errors.New("--state is required")
		}
		if err := os.MkdirAll(filepath.Dir(statePath), 0700); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(statePath+".lock", os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return nil, err
		}
		if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
			f.Close()
			return nil, errors.New("another command owns this context")
		}
		return func() { unix.Flock(int(f.Fd()), unix.LOCK_UN); f.Close() }, nil
	}
	var id proof.Identity
	var base string
	begin := &cobra.Command{Use: "begin", Short: "Create a fresh owned namespace and disposable directories", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		release, err := lock()
		if err != nil {
			return err
		}
		defer release()
		if base == "" {
			return errors.New("--sandbox-root is required")
		}
		if err = id.Validate(); err != nil {
			return err
		}
		reserve, err := os.OpenFile(statePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return fmt.Errorf("context must be new: %w", err)
		}
		reserve.Close()
		e, err := proof.Connect(kubeconfig, kubecontext)
		if err != nil {
			os.Remove(statePath)
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()
		s, err := e.Begin(ctx, id, base)
		if err != nil {
			os.Remove(statePath)
			return err
		}
		if err = proof.Save(statePath, s); err != nil {
			return fmt.Errorf("namespace %s created but context save failed: %w", s.Namespace, err)
		}
		data, err := proof.Canonical(s)
		if err != nil {
			return err
		}
		_, err = cmd.OutOrStdout().Write(data)
		return err
	}}
	begin.Flags().StringVar(&base, "sandbox-root", "", "Parent for a NEW proof-<token> sandbox")
	begin.Flags().StringVar(&id.Provider, "provider", "local", "local or github")
	begin.Flags().StringVar(&id.Repository, "repository", "", "Repository identity")
	begin.Flags().StringVar(&id.Run, "run", "", "CI run identity")
	begin.Flags().IntVar(&id.Attempt, "attempt", 1, "CI run attempt")
	begin.Flags().StringVar(&id.Job, "job", "", "Job identity")
	begin.Flags().StringVar(&id.Revision, "revision", "", "Full source commit hash")
	root.AddCommand(begin)
	var timeout time.Duration
	for _, name := range []string{"inventory", "cleanup"} {
		name := name
		command := &cobra.Command{Use: name, Short: map[string]string{"inventory": "Record namespace resources and explicitly owned PVs", "cleanup": "Delete owned resources and verify absence"}[name], Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			release, err := lock()
			if err != nil {
				return err
			}
			defer release()
			s, err := proof.Load(statePath)
			if err != nil {
				return err
			}
			if err = proof.ControlPath(statePath, s); err != nil {
				return err
			}
			e, err := proof.Connect(kubeconfig, kubecontext)
			if err != nil {
				return err
			}
			duration := 30 * time.Second
			if name == "cleanup" {
				duration = timeout
			}
			if duration <= 0 || duration > 10*time.Minute {
				return errors.New("timeout must be positive and at most 10 minutes")
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), duration)
			defer cancel()
			if name == "inventory" {
				err = e.Inventory(ctx, s)
			} else {
				err = e.Cleanup(ctx, s)
			}
			if saveErr := proof.Save(statePath, s); saveErr != nil {
				return errors.Join(err, saveErr)
			}
			data, marshalErr := proof.Canonical(s)
			if marshalErr != nil {
				return errors.Join(err, marshalErr)
			}
			_, writeErr := cmd.OutOrStdout().Write(data)
			return errors.Join(err, writeErr)
		}}
		if name == "cleanup" {
			command.Flags().DurationVar(&timeout, "timeout", 120*time.Second, "Total cleanup deadline")
		}
		root.AddCommand(command)
	}
	var output string
	receipt := &cobra.Command{Use: "receipt", Short: "Emit canonical UNSIGNED preliminary evidence", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		release, err := lock()
		if err != nil {
			return err
		}
		defer release()
		s, err := proof.Load(statePath)
		if err != nil {
			return err
		}
		if err = proof.ControlPath(statePath, s); err != nil {
			return err
		}
		value, err := s.Evidence()
		if err != nil {
			return err
		}
		data, err := proof.Canonical(value)
		if err != nil {
			return err
		}
		if output != "" {
			if err = proof.ControlPath(output, s); err != nil {
				return err
			}
			f, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				return err
			}
			_, err = f.Write(data)
			return errors.Join(err, f.Close())
		}
		_, err = cmd.OutOrStdout().Write(data)
		return err
	}}
	receipt.Flags().StringVar(&output, "out", "", "New output file; never overwrite evidence")
	root.AddCommand(receipt)
	var o proof.VerifyOptions
	v := &cobra.Command{Use: "verify", Short: "Verify digest and Sigstore identity/bundle against operator trust", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()
		if err := proof.Verify(ctx, o); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Digest, certificate identity and Sigstore bundle verified. This does not assert a cleanup verdict.")
		return nil
	}}
	v.Flags().StringVar(&o.Artifact, "artifact", "", "Signed receipt or blob file")
	v.Flags().StringVar(&o.Bundle, "bundle", "", "Sigstore bundle file")
	v.Flags().StringVar(&o.TrustRoot, "trust-root", "", "Independently provisioned trust root")
	v.Flags().StringVar(&o.ExpectedSHA256, "sha256", "", "Expected artifact digest")
	v.Flags().StringVar(&o.Identity, "identity", "", "Exact expected signer identity")
	v.Flags().StringVar(&o.Issuer, "issuer", "", "Exact expected OIDC issuer")
	v.Flags().StringVar(&o.Cosign, "cosign", ".build/cosign", "Pinned Cosign binary")
	root.AddCommand(v)
	return root
}
