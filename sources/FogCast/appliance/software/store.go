package software

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"
)

type Store struct {
	root          string
	compatibility Compatibility
	pinned        map[string]fs.FileInfo
	persistent    bool
}

// New opens a POSIX store for tools and tests. Production must use OpenPersistent.
func New(root string, c Compatibility) (*Store, error) {
	root, e := filepath.Abs(root)
	if e != nil {
		return nil, e
	}
	if e = realDirectories(root, true); e != nil {
		return nil, e
	}
	s := &Store{root: root, compatibility: c, pinned: map[string]fs.FileInfo{}}
	for _, p := range []string{root, filepath.Join(root, "releases"), filepath.Join(root, "incoming")} {
		if e = realDirectories(p, true); e != nil {
			return nil, e
		}
		s.pinned[p], e = os.Lstat(p)
		if e != nil {
			return nil, e
		}
	}
	p := filepath.Join(root, ".lock")
	f, e := os.OpenFile(p, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return nil, e
	}
	info, e := f.Stat()
	f.Close()
	if e != nil || !info.Mode().IsRegular() {
		return nil, errors.New("invalid store lock")
	}
	s.pinned[p] = info
	if e = syncDir(filepath.Dir(root)); e != nil {
		return nil, e
	}
	if e = syncDir(root); e != nil {
		return nil, e
	}
	return s, nil
}
func (s *Store) Root() string { return s.root }
func (s *Store) check() error {
	if s.persistent {
		if e := checkPersistent(s.root); e != nil {
			return e
		}
	}
	for p, info := range s.pinned {
		now, e := os.Lstat(p)
		if e != nil {
			return e
		}
		if !os.SameFile(info, now) || now.Mode()&os.ModeSymlink != 0 {
			return errors.New("software store identity changed")
		}
	}
	return nil
}
func (s *Store) locked(ctx context.Context, fn func() error) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if e := s.check(); e != nil {
		return e
	}
	f, e := os.OpenFile(filepath.Join(s.root, ".lock"), os.O_RDWR|syscall.O_NOFOLLOW, 0)
	if e != nil {
		return e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !os.SameFile(info, s.pinned[filepath.Join(s.root, ".lock")]) {
		return errors.New("store lock changed")
	}
	for {
		e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if e == nil {
			break
		}
		if e != syscall.EWOULDBLOCK && e != syscall.EAGAIN {
			return e
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	if e = s.check(); e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	return fn()
}

// Install validates completely before atomic publication. It never selects or
// restarts a release. Concurrent identical installs are idempotent.
func (s *Store) Install(ctx context.Context, r io.Reader) (release Release, created bool, err error) {
	err = s.locked(ctx, func() error {
		dir, e := os.MkdirTemp(filepath.Join(s.root, "incoming"), "bundle-")
		if e != nil {
			return e
		}
		defer os.RemoveAll(dir)
		release, e = extract(ctx, r, dir)
		if e != nil {
			return e
		}
		if e = release.Manifest.Compatible(s.compatibility); e != nil {
			return e
		}
		dest := filepath.Join(s.root, "releases", release.ID)
		if _, e = os.Lstat(dest); e == nil {
			_, e = s.read(ctx, release.ID)
			return e
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		b, e := release.Manifest.Canonical()
		if e != nil {
			return e
		}
		f, e := os.OpenFile(filepath.Join(dir, "manifest.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0444)
		if e != nil {
			return e
		}
		_, e = f.Write(b)
		if e == nil {
			e = f.Chmod(0444)
		}
		if e == nil {
			e = f.Sync()
		}
		ce := f.Close()
		if e == nil {
			e = ce
		}
		if e != nil {
			return e
		}
		e = filepath.WalkDir(dir, func(p string, d fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.IsDir() {
				return syncDir(p)
			}
			return nil
		})
		if e != nil {
			return e
		}
		if e = os.Rename(dir, dest); e != nil {
			return e
		}
		if e = syncDir(filepath.Join(s.root, "releases")); e != nil {
			return e
		}
		if e = syncDir(filepath.Join(s.root, "incoming")); e != nil {
			return e
		}
		created = true
		return nil
	})
	return
}

// Read verifies every installed byte, mode and member; corruption is an error,
// never an implicit fallback to another release.
func (s *Store) Read(ctx context.Context, id string) (release Release, err error) {
	err = s.locked(ctx, func() error { var e error; release, e = s.read(ctx, id); return e })
	return
}
func (s *Store) Path(ctx context.Context, id string) (string, error) {
	if _, e := s.Read(ctx, id); e != nil {
		return "", e
	}
	return filepath.Join(s.root, "releases", id), nil
}
func (s *Store) List(ctx context.Context) (out []Release, err error) {
	out = []Release{}
	err = s.locked(ctx, func() error {
		entries, e := os.ReadDir(filepath.Join(s.root, "releases"))
		if e != nil {
			return e
		}
		for _, entry := range entries {
			r, e := s.read(ctx, entry.Name())
			if e != nil {
				return e
			}
			out = append(out, r)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return nil
	})
	return
}
func (s *Store) read(ctx context.Context, id string) (Release, error) {
	if !ValidID(id) {
		return Release{}, errors.New("invalid release ID")
	}
	r, err := VerifyDirectory(ctx, filepath.Join(s.root, "releases", id))
	if err != nil {
		return Release{}, err
	}
	if r.ID != id {
		return Release{}, errors.New("stored manifest identity mismatch")
	}
	if err = r.Manifest.Compatible(s.compatibility); err != nil {
		return Release{}, err
	}
	return r, nil
}

// VerifyDirectory authenticates a complete unpacked release, including factory
// bundles in the read-only base image. Callers must separately check compatibility.
func VerifyDirectory(ctx context.Context, dir string) (Release, error) {
	var out Release
	dir, e := filepath.Abs(dir)
	if e != nil {
		return out, e
	}
	if e = realDirectories(dir, false); e != nil {
		return out, e
	}

	p := filepath.Join(dir, "manifest.json")
	info, e := os.Lstat(p)
	if e != nil {
		return out, e
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0444 {
		return out, errors.New("invalid stored manifest")
	}
	f, e := os.Open(p)
	if e != nil {
		return out, e
	}
	m, e := DecodeManifest(f)
	f.Close()
	if e != nil {
		return out, e
	}
	id, e := m.ID()
	if e != nil {
		return out, e
	}
	expected := map[string]File{}
	dirs := map[string]bool{".": true}
	for _, file := range m.Files {
		expected[file.Path] = file
		for p := filepath.Dir(file.Path); p != "."; p = filepath.Dir(p) {
			dirs[p] = true
		}
	}
	seen := 0
	e = filepath.WalkDir(dir, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		relative, e := filepath.Rel(dir, p)
		if e != nil {
			return e
		}
		if d.IsDir() {
			if !dirs[relative] {
				return errors.New("unlisted release directory")
			}
			return nil
		}
		if relative == "manifest.json" {
			return nil
		}
		file, ok := expected[filepath.ToSlash(relative)]
		if !ok {
			return errors.New("unlisted release file")
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != fs.FileMode(file.Mode) || info.Size() != file.Size {
			return fmt.Errorf("invalid stored file %s", relative)
		}
		f, e := os.Open(p)
		if e != nil {
			return e
		}
		h := sha256.New()
		n, e := io.Copy(h, io.LimitReader(contextReader{ctx, f}, file.Size+1))
		f.Close()
		if e != nil {
			return e
		}
		if n != file.Size || hex.EncodeToString(h.Sum(nil)) != file.SHA256 {
			return fmt.Errorf("corrupt release file %s", relative)
		}
		seen++
		return nil
	})
	if e != nil {
		return out, e
	}
	if seen != len(expected) {
		return out, errors.New("missing installed file")
	}
	return Release{id, m}, nil
}
func realDirectories(p string, create bool) error {
	p = filepath.Clean(p)
	if p == "/" {
		return nil
	}
	if e := realDirectories(filepath.Dir(p), create); e != nil {
		return e
	}
	info, e := os.Lstat(p)
	if errors.Is(e, os.ErrNotExist) && create {
		if e = os.Mkdir(p, 0755); e != nil && !errors.Is(e, os.ErrExist) {
			return e
		}
		info, e = os.Lstat(p)
	}
	if e != nil {
		return e
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("not a real directory: %s", p)
	}
	return nil
}
func syncDir(p string) error {
	f, e := os.Open(p)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
