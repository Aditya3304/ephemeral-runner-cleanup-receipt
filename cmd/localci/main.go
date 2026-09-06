package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/coordinator"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
	"github.com/spf13/cobra"
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
	var kubeconfig, ledger string
	root := &cobra.Command{Use: "localci", Short: "Local CI jobs with durable cleanup evidence staging", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", ".build/kubeconfig", "Dedicated local cluster configuration")
	root.PersistentFlags().StringVar(&ledger, "ledger", ".build/coordinator", "Coordinator-owned ledger, never mounted into jobs")
	connect := func() (*coordinator.Coordinator, error) { return coordinator.Connect(kubeconfig, ledger) }
	var repository, revision, job, imageFile string
	var timeout time.Duration
	run := &cobra.Command{Use: "run -- COMMAND [ARG...]", Args: cobra.MinimumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.ArgsLenAtDash() < 0 {
			return errors.New("separate the command with --")
		}
		c, err := connect()
		if err != nil {
			return err
		}
		release, err := c.Lock()
		if err != nil {
			return err
		}
		defer release()
		data, err := proof.ReadBounded(imageFile, 512)
		if err != nil {
			return err
		}
		l, err := c.Start(cmd.Context(), proof.Identity{Provider: "local", Repository: repository, Run: "pending", Attempt: 1, Job: job, Revision: revision}, strings.TrimSpace(string(data)), args, timeout)
		if l != nil {
			fmt.Fprintln(cmd.OutOrStdout(), string(coordinator.JSONSummary(l)))
		}
		if err != nil {
			return err
		}
		if l.PodExitCode == nil || *l.PodExitCode != 0 {
			return errors.New("job did not succeed; cleanup outcome is recorded separately")
		}
		if l.EvidenceStatus != "accepted-untrusted" {
			return errors.New("job evidence missing or rejected")
		}
		return nil
	}}
	run.Flags().StringVar(&repository, "repository", "Aditya3304/ephemeral-runner-cleanup-receipt", "Source repository identity")
	run.Flags().StringVar(&revision, "revision", "", "Exact source commit")
	run.Flags().StringVar(&job, "job", "test", "Job display identity")
	run.Flags().StringVar(&imageFile, "image-file", ".build/runner-image", "Preloaded immutable runner image reference")
	run.Flags().DurationVar(&timeout, "timeout", 90*time.Second, "Maximum test-command duration, followed by cleanup")
	root.AddCommand(run)
	for _, name := range []string{"inspect", "collect", "cancel"} {
		name := name
		cmd := &cobra.Command{Use: name + " RUN_ID", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			c, err := connect()
			if err != nil {
				return err
			}
			if name == "cancel" {
				if err = c.RequestCancel(args[0]); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "Cancellation recorded; the coordinator will stop the command and attempt post cleanup.")
				return nil
			}
			var l *coordinator.Ledger
			if name == "collect" {
				release, e := c.Lock()
				if e != nil {
					return e
				}
				defer release()
				l, err = c.Collect(cmd.Context(), args[0])
			} else {
				l, err = c.Load(args[0])
			}
			if l != nil {
				fmt.Fprintln(cmd.OutOrStdout(), string(coordinator.JSONSummary(l)))
			}
			return err
		}}
		root.AddCommand(cmd)
	}
	return root
}
