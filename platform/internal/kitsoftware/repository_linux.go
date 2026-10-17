package kitsoftware

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/DeanoC/FogCast/appliance/software"
	"golang.org/x/sys/unix"
)

const maxStateSize = 16 << 20
const initializedMarker = "initialized.v1"

// FileRepository uses independent flock files for transactions and the worker.
// Root must first be verified as persistent by the production caller. Root is
// pinned by descriptor; every operation additionally rejects a replaced path.
type FileRepository struct {
	root     string
	dir      *os.File
	identity os.FileInfo
	mu       sync.Mutex
	// writeCheckpoint is an instance-local test observer; production leaves it nil.
	writeCheckpoint func(string, State)
}

func OpenRepository(root, factory string) (*FileRepository, error) {
	if !software.ValidID(factory) {
		return nil, errors.New("invalid factory software identity")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	// Persist the control directory itself before publishing anything inside it.
	parent, err := os.Open(filepath.Dir(root))
	if err != nil {
		return nil, err
	}
	err = errors.Join(parent.Sync(), parent.Close())
	if err != nil {
		return nil, err
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	dir := os.NewFile(uintptr(fd), root)
	info, err := dir.Stat()
	if err != nil {
		dir.Close()
		return nil, err
	}
	r := &FileRepository{root: root, dir: dir, identity: info}
	err = r.withLock("state.lock", false, func() error {
		initialized, err := r.initialized()
		if err != nil {
			return err
		}
		s, err := r.read()
		if initialized {
			if errors.Is(err, os.ErrNotExist) {
				return errors.New("initialized software state missing")
			}
			return err
		}
		// Open never returns before its marker is durable. Without that marker,
		// only an interrupted first initialization may be completed. Admission
		// credentials or worker artifacts prove this is an established repository.
		entries, listErr := r.entries()
		if listErr != nil {
			return listErr
		}
		for _, entry := range entries {
			switch entry.Name() {
			case "state.lock", "state.json", "state.tmp":
			default:
				return errors.New("software initialization marker missing from established repository")
			}
		}
		if errors.Is(err, os.ErrNotExist) {
			if err := r.write(State{Version: 1, Selected: factory, Receipts: map[string]Receipt{}}); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if s.Generation != 0 || s.Previous != "" || s.Pending != nil || s.Unreleased != nil || len(s.Receipts) != 0 {
			return errors.New("software initialization marker missing from established state")
		}
		return r.markInitialized()
	})
	if err != nil {
		dir.Close()
		return nil, err
	}
	return r, nil
}

func (r *FileRepository) initialized() (bool, error) {
	fd, err := unix.Openat(int(r.dir.Fd()), initializedMarker, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	f := os.NewFile(uintptr(fd), initializedMarker)
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Size() != 0 {
		return false, errors.New("invalid software initialization marker")
	}
	return true, nil
}

func (r *FileRepository) markInitialized() error {
	fd, err := unix.Openat(int(r.dir.Fd()), initializedMarker, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), initializedMarker)
	if err := errors.Join(f.Sync(), f.Close()); err != nil {
		return err
	}
	return r.dir.Sync()
}
func (r *FileRepository) Close() error { return r.dir.Close() }
func (r *FileRepository) entries() ([]os.DirEntry, error) {
	fd, err := unix.Openat(int(r.dir.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), r.root)
	defer f.Close()
	return f.ReadDir(-1)
}
func (r *FileRepository) check() error {
	info, err := os.Lstat(r.root)
	if err != nil {
		return err
	}
	if !info.IsDir() || !os.SameFile(info, r.identity) {
		return errors.New("software state storage replaced")
	}
	return nil
}
func (r *FileRepository) lock(name string, nonblock bool) (func(), error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	fd, err := unix.Openat(int(r.dir.Fd()), name, unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	mode := unix.LOCK_EX
	if nonblock {
		mode |= unix.LOCK_NB
	}
	if err := unix.Flock(fd, mode); err != nil {
		unix.Close(fd)
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrBusy
		}
		return nil, err
	}
	// Reject replaced lock inodes, including a replacement during acquisition.
	var opened, linked unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil {
		unix.Close(fd)
		return nil, err
	}
	if err := unix.Fstatat(int(r.dir.Fd()), name, &linked, unix.AT_SYMLINK_NOFOLLOW); err != nil || opened.Ino != linked.Ino || opened.Dev != linked.Dev || linked.Mode&unix.S_IFMT != unix.S_IFREG {
		unix.Close(fd)
		return nil, errors.New("software state lock replaced")
	}
	return func() { unix.Flock(fd, unix.LOCK_UN); unix.Close(fd) }, nil
}
func (r *FileRepository) withLock(name string, nonblock bool, fn func() error) error {
	unlock, err := r.lock(name, nonblock)
	if err != nil {
		return err
	}
	defer unlock()
	return fn()
}
func (r *FileRepository) WorkerLock() (func(), error) { return r.lock("worker.lock", true) }
func (r *FileRepository) read() (State, error) {
	var s State
	fd, err := unix.Openat(int(r.dir.Fd()), "state.json", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return s, err
	}
	f := os.NewFile(uintptr(fd), "state.json")
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return s, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxStateSize {
		return s, errors.New("invalid software state file")
	}
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return s, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return s, errors.New("trailing software state data")
	}
	return s, validateState(s)
}
func (r *FileRepository) checkpoint(stage string, s State) {
	if r.writeCheckpoint != nil {
		r.writeCheckpoint(stage, s)
	}
}

func (r *FileRepository) write(s State) error {
	if err := validateState(s); err != nil {
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if len(b) > maxStateSize {
		return errors.New("software state exceeds size limit")
	}
	r.checkpoint("before-create", s)
	fd, err := unix.Openat(int(r.dir.Fd()), "state.tmp", unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if errors.Is(err, unix.EEXIST) {
		if err = unix.Unlinkat(int(r.dir.Fd()), "state.tmp", 0); err != nil {
			return err
		}
		fd, err = unix.Openat(int(r.dir.Fd()), "state.tmp", unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	}
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), "state.tmp")
	r.checkpoint("after-create", s)
	_, writeErr := io.Copy(f, bytes.NewReader(b))
	r.checkpoint("after-write", s)
	syncErr := f.Sync()
	r.checkpoint("after-file-sync", s)
	closeErr := f.Close()
	r.checkpoint("after-close", s)
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	r.checkpoint("before-rename", s)
	if err := unix.Renameat(int(r.dir.Fd()), "state.tmp", int(r.dir.Fd()), "state.json"); err != nil {
		return err
	}
	r.checkpoint("after-rename", s)
	if err := r.dir.Sync(); err != nil {
		return fmt.Errorf("sync software state directory: %w", err)
	}
	r.checkpoint("after-directory-sync", s)
	return nil
}
func (r *FileRepository) Read() (s State, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	err = r.withLock("state.lock", false, func() error { s, err = r.read(); return err })
	return
}
func (r *FileRepository) Update(fn func(*State) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.withLock("state.lock", false, func() error {
		s, err := r.read()
		if err != nil {
			return err
		}
		if err := fn(&s); err != nil {
			return err
		}
		return r.write(s)
	})
}
