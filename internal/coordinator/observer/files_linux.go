//go:build linux

package observer

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// openDirectory refuses symlinks at every component, including the kubelet root.
func openDirectory(path string) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("noncanonical directory")
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if e != nil {
			return nil, e
		}
		fd = next
	}
	return os.NewFile(uintptr(fd), path), nil
}
func openChild(parent *os.File, name string, flags int) (*os.File, error) {
	fd, err := unix.Openat2(int(parent.Fd()), name, &unix.OpenHow{Flags: uint64(flags | unix.O_NOFOLLOW | unix.O_CLOEXEC | unix.O_NONBLOCK), Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}
func absentDirectory(parent *os.File, name string) Finding {
	f, err := openChild(parent, name, unix.O_RDONLY|unix.O_DIRECTORY)
	if errors.Is(err, unix.ENOENT) {
		return Finding{"verified", "Pinned stopped-runner path is absent"}
	}
	if err != nil {
		return Finding{"unobservable", "Scoped path cannot be safely inspected"}
	}
	defer f.Close()
	entries, err := f.Readdirnames(1)
	if len(entries) > 0 {
		return Finding{"failed", "Residual directory entry remains after guard termination"}
	}
	if err != io.EOF {
		return Finding{"unobservable", "Directory enumeration did not prove absence"}
	}
	return Finding{"verified", "Pinned stopped-runner directory is empty, including hidden entries"}
}
func inspectWorkspace(r Request) (Finding, Finding) {
	missing := Finding{"unobservable", "Pinned kubelet emptyDir unavailable; disappearance is not cleanup proof"}
	volume, err := openDirectory("/var/lib/kubelet/pods/" + r.UID + "/volumes/kubernetes.io~empty-dir/work")
	if err != nil {
		return missing, missing
	}
	defer volume.Close()
	if r.GitHub {
		// Includes the Actions work tree, diagnostics and registration credentials.
		// Inspect only the fixed root in the UID-pinned volume, never a job path.
		runtime := absentDirectory(volume, "github-runtime")
		if runtime.Status != "verified" {
			return runtime, runtime
		}
	}
	root, err := openChild(volume, "proof-"+r.Run, unix.O_RDONLY|unix.O_DIRECTORY)
	if errors.Is(err, unix.ENOENT) {
		f := Finding{"verified", "Run directory absent within the existing pinned emptyDir after termination"}
		return f, f
	}
	if err != nil {
		return missing, missing
	}
	defer root.Close()
	return absentDirectory(root, "workspace"), absentDirectory(root, "credentials")
}
func digestReader(r io.Reader) (string, int64, error) {
	h := sha256.New()
	n, e := io.Copy(h, r)
	return hex.EncodeToString(h.Sum(nil)), n, e
}
func writeJSON(dir, name string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".pending-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), filepath.Join(dir, name)); err != nil {
		return err
	}
	d, err := openDirectory(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// The directory is pinned and watched BEFORE runtime creation of 0.log. A
// rotation is conservatively a gap even if every rotated byte might survive.
type logWatch struct {
	fd      int
	guardWD int
	created bool
	closed  bool
	problem string
}

func newLogWatch(parent, guard *os.File) (*logWatch, error) {
	fd, e := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if e != nil {
		return nil, e
	}
	w := &logWatch{fd: fd}
	for i, d := range []*os.File{parent, guard} {
		mask := uint32(unix.IN_CREATE | unix.IN_MODIFY | unix.IN_CLOSE_WRITE | unix.IN_MOVED_FROM | unix.IN_MOVED_TO | unix.IN_DELETE | unix.IN_DELETE_SELF | unix.IN_MOVE_SELF | unix.IN_UNMOUNT)
		wd, err := unix.InotifyAddWatch(fd, fmt.Sprintf("/proc/self/fd/%d", d.Fd()), mask)
		if err != nil {
			unix.Close(fd)
			return nil, err
		}
		if i == 1 {
			w.guardWD = wd
		}
	}
	return w, nil
}
func (w *logWatch) events(data []byte) {
	for len(data) >= unix.SizeofInotifyEvent {
		wd := int(int32(binary.NativeEndian.Uint32(data)))
		mask := binary.NativeEndian.Uint32(data[4:])
		n := int(binary.NativeEndian.Uint32(data[12:]))
		if n > len(data)-unix.SizeofInotifyEvent {
			w.problem = "malformed watch event"
			return
		}
		name := strings.TrimRight(string(data[unix.SizeofInotifyEvent:unix.SizeofInotifyEvent+n]), "\x00")
		data = data[unix.SizeofInotifyEvent+n:]
		if mask&(unix.IN_Q_OVERFLOW|unix.IN_IGNORED|unix.IN_UNMOUNT|unix.IN_DELETE_SELF|unix.IN_MOVE_SELF) != 0 {
			w.problem = "collector watch lost continuity"
			continue
		}
		if wd == w.guardWD {
			if name == "0.log" && mask == unix.IN_CREATE && !w.created {
				w.created = true
			} else if name == "0.log" && mask == unix.IN_MODIFY && w.created && !w.closed {
			} else if name == "0.log" && mask == unix.IN_CLOSE_WRITE && w.created && !w.closed {
				w.closed = true
			} else {
				w.problem = "CRI log rotation, replacement, or unexpected file"
			}
		} else if name == "guard" {
			w.problem = "guard log directory replaced"
		}
	}
	if len(data) != 0 {
		w.problem = "partial watch event"
	}
}
func (w *logWatch) drain() error {
	b := make([]byte, 64<<10)
	for {
		n, e := unix.Read(w.fd, b)
		if errors.Is(e, unix.EAGAIN) {
			return nil
		}
		if e != nil {
			return e
		}
		if n == 0 {
			return errors.New("watch closed")
		}
		w.events(b[:n])
	}
}
