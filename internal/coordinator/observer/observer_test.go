//go:build linux

package observer

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func event(wd int, mask uint32, name string) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.NativeEndian, int32(wd))
	_ = binary.Write(&b, binary.NativeEndian, mask)
	_ = binary.Write(&b, binary.NativeEndian, uint32(0))
	suffix := []byte(name + "\x00")
	_ = binary.Write(&b, binary.NativeEndian, uint32(len(suffix)))
	b.Write(suffix)
	return b.Bytes()
}
func TestWatchCoverageRequiresOriginalFileAndWriterClose(t *testing.T) {
	w := logWatch{guardWD: 2}
	w.events(event(2, unix.IN_CREATE, "0.log"))
	w.events(event(2, unix.IN_MODIFY, "0.log"))
	if !w.created || w.closed || w.problem != "" {
		t.Fatal(w)
	}
	w.events(event(2, unix.IN_CLOSE_WRITE, "0.log"))
	if !w.closed || w.problem != "" {
		t.Fatal(w)
	}
	w.events(event(2, unix.IN_MODIFY, "0.log"))
	if w.problem == "" {
		t.Fatal("rewrite after terminal writer close accepted")
	}
}
func TestEveryDiscontinuityIsIrreversible(t *testing.T) {
	for _, tc := range []struct {
		name string
		mask uint32
		file string
		wd   int
	}{
		{"rotation", unix.IN_MOVED_FROM, "0.log", 2}, {"rotated file", unix.IN_CREATE, "0.log.1", 2}, {"unlink", unix.IN_DELETE, "0.log", 2},
		{"replacement", unix.IN_MOVED_TO, "0.log", 2}, {"watch overflow", unix.IN_Q_OVERFLOW, "", -1}, {"unmount", unix.IN_UNMOUNT, "", 2},
		{"watch lost", unix.IN_IGNORED, "", 2}, {"directory removed", unix.IN_DELETE_SELF, "", 2}, {"directory replaced", unix.IN_MOVED_FROM, "guard", 1},
		{"duplicate creation", unix.IN_CREATE, "0.log", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := logWatch{guardWD: 2, created: true}
			w.events(event(tc.wd, tc.mask, tc.file))
			if w.problem == "" {
				t.Fatal("discontinuity accepted")
			}
			w.events(event(2, unix.IN_CLOSE_WRITE, "0.log"))
			if w.problem == "" {
				t.Fatal("later close repaired gap")
			}
		})
	}
}
func TestRealInotifyCapturesCreateCloseAndRotation(t *testing.T) {
	dir := t.TempDir()
	guardPath := filepath.Join(dir, "guard")
	if e := os.Mkdir(guardPath, 0700); e != nil {
		t.Fatal(e)
	}
	parent, e := openDirectory(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer parent.Close()
	guard, e := openDirectory(guardPath)
	if e != nil {
		t.Fatal(e)
	}
	defer guard.Close()
	w, e := newLogWatch(parent, guard)
	if e != nil {
		t.Fatal(e)
	}
	defer unix.Close(w.fd)
	path := filepath.Join(guardPath, "0.log")
	if e = os.WriteFile(path, []byte("raw stderr\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = w.drain(); e != nil {
		t.Fatal(e)
	}
	if !w.created || !w.closed || w.problem != "" {
		t.Fatalf("missed real events: %+v", w)
	}
	if e = os.Rename(path, path+".1"); e != nil {
		t.Fatal(e)
	}
	if e = w.drain(); e != nil {
		t.Fatal(e)
	}
	if w.problem == "" {
		t.Fatal("actual rotation accepted")
	}
}
func TestScopedAbsenceRejectsHiddenFilesSymlinksAndMissingParents(t *testing.T) {
	dir := t.TempDir()
	parent, e := openDirectory(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer parent.Close()
	if got := absentDirectory(parent, "workspace"); got.Status != "verified" {
		t.Fatal(got)
	}
	path := filepath.Join(dir, "workspace")
	if e = os.Mkdir(path, 0700); e != nil {
		t.Fatal(e)
	}
	if got := absentDirectory(parent, "workspace"); got.Status != "verified" {
		t.Fatal(got)
	}
	if e = os.WriteFile(filepath.Join(path, ".hidden"), []byte("secret"), 0600); e != nil {
		t.Fatal(e)
	}
	if got := absentDirectory(parent, "workspace"); got.Status != "failed" {
		t.Fatal(got)
	}
	outside := t.TempDir()
	if e = os.Symlink(outside, filepath.Join(dir, "credentials")); e != nil {
		t.Fatal(e)
	}
	if got := absentDirectory(parent, "credentials"); got.Status != "unobservable" {
		t.Fatal("followed symlink", got)
	}
	if _, e = openDirectory(filepath.Join(dir, "credentials")); e == nil {
		t.Fatal("followed path component symlink")
	}
	if _, e = openChild(parent, "../outside", unix.O_RDONLY|unix.O_DIRECTORY); e == nil {
		t.Fatal("escaped pinned directory")
	}
	r := requestFixture()
	a, b := inspectWorkspace(r)
	if a.Status != "unobservable" || b.Status != "unobservable" {
		t.Fatal("missing volume reported clean")
	}
}
func requestFixture() Request {
	return Request{Run: strings.Repeat("a", 32), Pod: "job-abc12", UID: "3a8615a9-4b3f-4945-87dc-4470f1e70978", Sandbox: strings.Repeat("b", 64), Binding: strings.Repeat("c", 64), Seconds: 345}
}
func TestRequestScopesAndRuntimeTime(t *testing.T) {
	r := requestFixture()
	if e := r.Validate(); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*Request){func(r *Request) { r.Run = "../escape" }, func(r *Request) { r.Pod = "../../etc" }, func(r *Request) { r.UID = "other" }, func(r *Request) { r.Seconds = 901 }, func(r *Request) { r.Sandbox = "short" }, func(r *Request) { r.Binding = "" }} {
		bad := r
		change(&bad)
		if e := bad.Validate(); e == nil {
			t.Fatal("invalid request accepted")
		}
	}
	stamp := time.Date(2026, 9, 7, 0, 0, 0, 123456789, time.UTC)
	for _, value := range []string{`"2026-09-07T00:00:00.123456789Z"`, `1788739200123456789`, `"1788739200123456789"`} {
		n, e := runtimeTime(json.RawMessage(value))
		if e != nil || n != stamp.UnixNano() {
			t.Fatal(value, n, e)
		}
	}
	for _, value := range []string{`null`, `"bad"`, `1.5`, `"1800-01-01T00:00:00Z"`} {
		if _, e := runtimeTime(json.RawMessage(value)); e == nil {
			t.Fatal("bad timestamp accepted", value)
		}
	}
}
func TestResultNeverPromotesMissingStartOrTermination(t *testing.T) {
	now := time.Now().UTC()
	r := Result{Kind: "kind-node-observation/v1", Request: requestFixture(), ArmedAt: now.Add(-time.Minute), CompletedAt: now, LogsDigest: strings.Repeat("a", 64), Logs: Finding{"unobservable", "gap"}, Workspace: Finding{"unobservable", "not seen"}, Credentials: Finding{"unobservable", "not seen"}}
	if e := r.Validate(); e != nil {
		t.Fatal(e)
	}
	r.Logs.Status = "verified"
	if e := r.Validate(); e == nil {
		t.Fatal("gap promoted")
	}
	r.Logs.Status = "unobservable"
	r.Workspace.Status = "verified"
	if e := r.Validate(); e == nil {
		t.Fatal("filesystem observation without stopped guard")
	}
}

func TestMissingResultDirectoryIsIrreversible(t *testing.T) {
	r := requestFixture()
	r.Run = strings.Repeat("d", 32)
	if err := os.RemoveAll(r.Directory()); err != nil {
		t.Fatal(err)
	}
	err := Read(r, "result", io.Discard)
	if !errors.Is(err, ErrResultLost) {
		t.Fatalf("missing result directory remained retryable: %v", err)
	}
}
