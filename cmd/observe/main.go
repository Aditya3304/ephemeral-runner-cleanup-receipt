//go:build linux

// observe is installed only in the project's kind node, never in the job image.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/coordinator/observer"
)

func main() {
	var r observer.Request
	var mode string
	flag.StringVar(&mode, "mode", "collect", "collect, ready, result, or logs")
	flag.StringVar(&r.Run, "run", "", "run ID")
	flag.StringVar(&r.Pod, "pod", "", "pod name")
	flag.StringVar(&r.UID, "uid", "", "pod UID")
	flag.StringVar(&r.Sandbox, "sandbox", "", "sandbox ID")
	flag.StringVar(&r.Binding, "binding", "", "host binding digest")
	flag.IntVar(&r.Seconds, "seconds", 900, "collector lifetime")
	flag.Parse()
	var err error
	if mode == "collect" {
		err = observer.Collect(r)
	} else {
		err = observer.Read(r, mode, os.Stdout)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
