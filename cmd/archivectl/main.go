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
	"path/filepath"
	"time"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/archive"
	"golang.org/x/sys/unix"
)

func bounded(path string, limit int64) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() > limit {
		return nil, errors.New("input must be a bounded regular file")
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(b)) > limit {
		return nil, errors.New("input exceeds limit")
	}
	return b, err
}
func run() error {
	if len(os.Args) < 2 {
		return errors.New("usage: archivectl put|verify|read --config FILE --file FILE (put) | --reference FILE (verify/read)")
	}
	command := os.Args[1]
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	cfg := fs.String("config", "", "operator config JSON")
	file := fs.String("file", "", "bounded input file for put")
	name := fs.String("name", "", "evidence metadata name (defaults to input basename)")
	reference := fs.String("reference", "", "reference JSON for verify/read")
	if err := fs.Parse(os.Args[2:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	c, err := archive.LoadConfig(*cfg)
	if err != nil {
		return err
	}
	client, err := archive.New(c)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	switch command {
	case "put":
		b, err := bounded(*file, c.MaxObjectBytes)
		if err != nil {
			return err
		}
		if *name == "" {
			*name = filepath.Base(*file)
		}
		ref, err := client.Put(ctx, *name, b)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(ref)
	case "verify", "read":
		b, err := bounded(*reference, 64<<10)
		if err != nil {
			return err
		}
		var ref archive.Ref
		d := json.NewDecoder(bytes.NewReader(b))
		d.DisallowUnknownFields()
		if err = d.Decode(&ref); err != nil {
			return err
		}
		if d.Decode(new(any)) != io.EOF {
			return errors.New("trailing reference JSON")
		}
		b, err = client.Read(ctx, ref)
		if err != nil {
			return err
		}
		if command == "read" {
			_, err = os.Stdout.Write(b)
			return err
		}
		fmt.Println("Archive exact version, SHA-256, size, SSE-KMS and retention verified.")
		return nil
	default:
		return errors.New("unknown archive command")
	}
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
