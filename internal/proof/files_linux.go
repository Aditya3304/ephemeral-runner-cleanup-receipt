//go:build linux

package proof

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func directoryID(path string) (DirIdentity, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return DirIdentity{}, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return DirIdentity{}, errors.New("expected real directory")
	}
	var st unix.Stat_t
	if err = unix.Lstat(path, &st); err != nil {
		return DirIdentity{}, err
	}
	return DirIdentity{uint64(st.Dev), st.Ino}, nil
}
func PrepareSandbox(base string, s *State) error {
	absolute, err := filepath.Abs(base)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(absolute, 0700); err != nil {
		return err
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return err
	}
	s.Sandbox = filepath.Join(absolute, s.Namespace)
	if err = os.Mkdir(s.Sandbox, 0700); err != nil {
		return err
	}
	s.Directories = map[string]DirIdentity{}
	if err = os.WriteFile(filepath.Join(s.Sandbox, ".proof-owner"), []byte(s.Token), 0600); err != nil {
		return err
	}
	for _, name := range []string{"root", "workspace", "credentials"} {
		path := s.Sandbox
		if name != "root" {
			path = filepath.Join(path, name)
			if err = os.Mkdir(path, 0700); err != nil {
				return err
			}
		}
		id, err := directoryID(path)
		if err != nil {
			return err
		}
		s.Directories[name] = id
	}
	return nil
}
func pinDirectory(fd int, id DirIdentity) error {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return err
	}
	if uint64(st.Dev) != id.Device || st.Ino != id.Inode {
		return errors.New("directory identity changed; refusing deletion")
	}
	return nil
}
func childDirectory(parent int, name string) (int, error) {
	return unix.Openat2(parent, name, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_XDEV | unix.RESOLVE_NO_SYMLINKS})
}

// Wipe uses pinned descriptors. openat2 refuses mount crossings, symlinks and
// path escapes. Symlinks inside the disposable directory are unlinked, not followed.
// No chmod, mount traversal, directory recreation or force-deletion fallback occurs.
func Wipe(ctx context.Context, s *State, name string) error {
	if name != "workspace" && name != "credentials" {
		return errors.New("unknown disposable directory")
	}
	if err := s.Validate(); err != nil {
		return err
	}
	fd, err := unix.Open(s.Sandbox, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if err = pinDirectory(fd, s.Directories["root"]); err != nil {
		return err
	}
	markerFD, err := unix.Openat(fd, ".proof-owner", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	marker := os.NewFile(uintptr(markerFD), "owner")
	data := make([]byte, 33)
	n, readErr := marker.Read(data)
	marker.Close()
	if readErr != nil || n != 32 || string(data[:n]) != s.Token {
		return errors.New("sandbox ownership marker mismatch")
	}
	child, err := childDirectory(fd, name)
	if err != nil {
		return err
	}
	defer unix.Close(child)
	if err = pinDirectory(child, s.Directories[name]); err != nil {
		return err
	}
	budget := 10000
	if err = emptyDirectory(ctx, child, 0, &budget); err != nil {
		return err
	}
	return nil
}
func namesAt(fd int) ([]string, error) {
	// Opening '.' makes an independent directory stream; dup would share offsets.
	readFD, err := unix.Openat(fd, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(readFD), "directory")
	defer f.Close()
	names, err := f.Readdirnames(10001)
	if errors.Is(err, io.EOF) {
		err = nil
	}
	if len(names) > 10000 {
		return nil, errors.New("directory exceeds safe entry limit")
	}
	return names, err
}
func emptyDirectory(ctx context.Context, fd, depth int, budget *int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > 128 {
		return errors.New("directory nesting exceeds safe limit")
	}
	names, err := namesAt(fd)
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		*budget--
		if *budget < 0 {
			return errors.New("workspace exceeds safe entry limit")
		}
		var st unix.Stat_t
		if err = unix.Fstatat(fd, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return err
		}
		flags := 0
		if st.Mode&unix.S_IFMT == unix.S_IFDIR {
			child, e := childDirectory(fd, name)
			if e != nil {
				return fmt.Errorf("refuse traversal %q: %w", name, e)
			}
			e = emptyDirectory(ctx, child, depth+1, budget)
			unix.Close(child)
			if e != nil {
				return e
			}
			flags = unix.AT_REMOVEDIR
		}
		if err = unix.Unlinkat(fd, name, flags); err != nil {
			return fmt.Errorf("delete %q: %w", name, err)
		}
	}
	residual, err := namesAt(fd)
	if err != nil {
		return err
	}
	if len(residual) > 0 {
		return errors.New("residual paths appeared during verification")
	}
	return nil
}
