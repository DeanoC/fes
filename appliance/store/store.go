// Package appliance owns immutable release storage and durable boot selection.
// The card/filesystem must honor fsync and rename. Checksums detect torn state;
// they cannot compensate for a controller that lies about persistence.
package appliance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	release "github.com/DeanoC/FogCast/appliance"
	"golang.org/x/sys/unix"
)

const DefaultRoot = "/media/fat/fogcast/releases"

var ErrCorruptState = errors.New("release selection state is corrupt; factory fallback only")
var ErrBusy = errors.New("release trial or pending activation already exists")

type Store struct {
	root    string
	factory release.Manifest
	// Keep state persistence operations together: a reader may need to finish
	// syncing a confirmation made visible by a rename whose directory sync failed.
	syncFile      func(*os.File) error
	syncDirectory func(string) error
}
type Status struct {
	ConfirmedBootID string `json:"confirmed_boot_id,omitempty"`
	ConfirmedImage  string `json:"confirmed_image,omitempty"`
	Good            string `json:"good"`
	Previous        string `json:"previous,omitempty"`
	Pending         string `json:"pending,omitempty"`
	TrialBootID     string `json:"trial_boot_id,omitempty"`
	TrialImage      string `json:"trial_image,omitempty"`
	Corrupt         bool   `json:"corrupt"`
}
type Selection struct {
	Manifest release.Manifest `json:"manifest"`
	Path     string           `json:"path"`
	Trial    bool             `json:"trial"`
	BootID   string           `json:"boot_id"`
}
type diskState struct {
	Format         int    `json:"format"`
	Good           string `json:"good"`
	Previous       string `json:"previous"`
	Pending        string `json:"pending"`
	BootID         string `json:"boot_id"`
	TrialImage     string `json:"trial_image"`
	ConfirmedImage string `json:"confirmed_image"`
}
type envelope struct {
	Payload json.RawMessage `json:"payload"`
	SHA256  string          `json:"sha256"`
}

func New(root string, factory release.Manifest) (*Store, error) {
	if e := factory.Validate(); e != nil {
		return nil, e
	}
	if !filepath.IsAbs(root) {
		return nil, errors.New("release root must be absolute")
	}
	for _, p := range []string{root, filepath.Join(root, "images"), filepath.Join(root, "manifests")} {
		if e := os.MkdirAll(p, 0700); e != nil {
			return nil, e
		}
		fi, e := os.Lstat(p)
		if e != nil {
			return nil, e
		}
		if !fi.IsDir() {
			return nil, fmt.Errorf("release directory is not a real directory: %s", p)
		}
	}
	return &Store{root: root, factory: factory, syncFile: (*os.File).Sync, syncDirectory: syncDir}, nil
}
func (s *Store) ImagePath(hash string) (string, error) {
	if !release.ValidHash(hash) {
		return "", errors.New("invalid image hash")
	}
	return filepath.Join(s.root, "images", hash+".img"), nil
}
func (s *Store) lock(ctx context.Context) (func(), error) {
	f, e := os.OpenFile(filepath.Join(s.root, ".lock"), os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
	if e != nil {
		return nil, e
	}
	fi, e := f.Stat()
	if e != nil {
		f.Close()
		return nil, e
	}
	if !fi.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("release lock is not a regular file")
	}
	for {
		if e = ctx.Err(); e != nil {
			f.Close()
			return nil, e
		}
		e = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if e == nil {
			return func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN); _ = f.Close() }, nil
		}
		if e != unix.EWOULDBLOCK && e != unix.EAGAIN {
			f.Close()
			return nil, e
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}
func regularOpen(path string) (*os.File, error) {
	f, e := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if e != nil {
		return nil, e
	}
	fi, e := f.Stat()
	if e != nil || !fi.Mode().IsRegular() {
		f.Close()
		if e == nil {
			e = errors.New("release file is not regular")
		}
		return nil, e
	}
	return f, nil
}
func syncDir(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}

// atomicWrite can return an error after rename. In that case persistence is
// indeterminate: callers must inspect status and must not blindly repeat reboot.
func atomicWrite(path string, b []byte) error {
	return atomicWriteWithSync(path, b, (*os.File).Sync, syncDir)
}

func atomicWriteWithSync(path string, b []byte, syncFile func(*os.File) error, syncDirectory func(string) error) error {
	dir := filepath.Dir(path)
	f, e := os.CreateTemp(dir, ".write-")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	defer f.Close()
	if _, e = f.Write(b); e != nil {
		return e
	}
	if e = syncFile(f); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = os.Rename(tmp, path); e != nil {
		return e
	}
	return syncDirectory(dir)
}
func (s *Store) fallbackState() diskState { return diskState{Format: 1, Good: s.factory.ImageSHA256} }
func (s *Store) readState() (diskState, bool, error) {
	fallback := s.fallbackState()
	f, e := regularOpen(filepath.Join(s.root, "state.json"))
	if errors.Is(e, os.ErrNotExist) {
		return fallback, false, nil
	}
	if e != nil {
		return fallback, false, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 8193))
	if e != nil {
		return fallback, false, e
	}
	if len(b) > 8192 {
		return fallback, true, nil
	}
	var env envelope
	if e = json.Unmarshal(b, &env); e != nil {
		return fallback, true, nil
	}
	sum := fmt.Sprintf("%x", sha256.Sum256(env.Payload))
	if sum != env.SHA256 {
		return fallback, true, nil
	}
	var st diskState
	if e = json.Unmarshal(env.Payload, &st); e != nil {
		return fallback, true, nil
	}
	// State is private to this version of Store. Requiring our canonical
	// encoding also rejects unknown, duplicate, omitted and case-variant keys.
	canonical, e := json.Marshal(st)
	if e != nil || !bytes.Equal(canonical, env.Payload) {
		return fallback, true, nil
	}
	canonical, e = json.Marshal(env)
	if e != nil || !bytes.Equal(canonical, bytes.TrimSpace(b)) {
		return fallback, true, nil
	}
	if st.Format != 1 || !release.ValidHash(st.Good) {
		return fallback, true, nil
	}
	for _, h := range []string{st.Previous, st.Pending, st.TrialImage, st.ConfirmedImage} {
		if h != "" && !release.ValidHash(h) {
			return fallback, true, nil
		}
	}
	if st.BootID != "" && !validBootID(st.BootID) || st.TrialImage != "" && (st.BootID == "" || st.Pending != "" || st.ConfirmedImage != "") || st.ConfirmedImage != "" && (st.BootID == "" || st.ConfirmedImage != st.Good) {
		return fallback, true, nil
	}
	return st, false, nil
}
func (s *Store) writeState(st diskState) error {
	b, e := json.Marshal(st)
	if e != nil {
		return e
	}
	out, e := json.Marshal(envelope{Payload: b, SHA256: fmt.Sprintf("%x", sha256.Sum256(b))})
	if e != nil {
		return e
	}
	return atomicWriteWithSync(filepath.Join(s.root, "state.json"), append(out, '\n'), s.syncFile, s.syncDirectory)
}

// syncState completes persistence while the caller holds the store lock. The
// state file was opened without following a symlink, and no Store writer can
// replace its name between file and directory sync. A visible checksum alone
// does not establish that a preceding confirmation's final fsync succeeded.
func (s *Store) syncState(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := regularOpen(filepath.Join(s.root, "state.json"))
	if err != nil {
		return err
	}
	defer f.Close()
	if err = s.syncFile(f); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = s.syncDirectory(s.root); err != nil {
		return err
	}
	return ctx.Err()
}

// ConfirmedContext is the watchdog's confirmation boundary. True means this
// exact boot/image is confirmed AND both state and its directory have synced.
// It can finish an earlier indeterminate post-rename confirmation safely.
// Unconfirmed, different or corrupt state returns false; I/O failures return
// an error and must never cause the watchdog to disarm.
func (s *Store) ConfirmedContext(ctx context.Context, bootID, imageSHA string) (bool, error) {
	if !validBootID(bootID) || !release.ValidHash(imageSHA) {
		return false, errors.New("invalid confirmation identity")
	}
	unlock, err := s.lock(ctx)
	if err != nil {
		return false, err
	}
	defer unlock()
	st, corrupt, err := s.readState()
	if err != nil {
		return false, err
	}
	if corrupt || st.BootID != bootID || st.ConfirmedImage != imageSHA || st.Good != imageSHA {
		return false, nil
	}
	if err = s.syncState(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// RejectTrialContext records an exact consumed trial rejected before watchdog
// startup or root switching. It preserves Good/Previous and the consumed boot
// ID, clears the failed trial, and never creates Pending or confirmation state.
// Failure is fatal to this boot attempt because persistence may be uncertain.
func (s *Store) RejectTrialContext(ctx context.Context, bootID, imageSHA string) error {
	if !validBootID(bootID) || !release.ValidHash(imageSHA) {
		return errors.New("invalid rejected trial identity")
	}
	unlock, err := s.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	st, corrupt, err := s.readState()
	if err != nil {
		return err
	}
	if corrupt {
		return ErrCorruptState
	}
	if st.BootID != bootID || st.TrialImage != imageSHA {
		return errors.New("rejected image is not this boot's consumed trial")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	st.TrialImage = ""
	return s.writeState(st)
}

// RecordFallbackContext binds Good to the verified nontrial image that the
// bootstrap has successfully prepared for this boot. Only the recorded Good
// or stable factory may be selected. A consumed trial must first be rejected
// explicitly; this method cannot discard or accept a trial. When factory
// replaces an unusable Good, it becomes the truthful predecessor of the next
// update, and the unusable image is not retained as a rollback target.
// Corrupt-state factory boot preserves the corrupt record as recovery evidence.
func (s *Store) RecordFallbackContext(ctx context.Context, bootID, imageSHA string) error {
	if !validBootID(bootID) || !release.ValidHash(imageSHA) {
		return errors.New("invalid fallback identity")
	}
	unlock, err := s.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	st, corrupt, err := s.readState()
	if err != nil {
		return err
	}
	if corrupt {
		if imageSHA != s.factory.ImageSHA256 {
			return ErrCorruptState
		}
		return ctx.Err()
	}
	if st.BootID != bootID {
		return errors.New("fallback boot does not match consumed boot")
	}
	if imageSHA != st.Good && imageSHA != s.factory.ImageSHA256 {
		return errors.New("fallback is neither known good nor factory")
	}
	if st.TrialImage != "" || st.Pending != "" {
		return errors.New("fallback cannot discard trial or pending selection")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if st.Good == imageSHA {
		return s.syncState(ctx)
	}
	st.Good = imageSHA
	st.Previous = ""
	st.ConfirmedImage = ""
	return s.writeState(st)
}
func (s *Store) Status() (Status, error) { return s.StatusContext(context.Background()) }

// StatusContext lets the independent watchdog bound lock waiting by its deadline.
func (s *Store) StatusContext(ctx context.Context) (Status, error) {
	unlock, e := s.lock(ctx)
	if e != nil {
		return Status{}, e
	}
	defer unlock()
	st, corrupt, e := s.readState()
	confirmedBootID := ""
	if st.ConfirmedImage != "" {
		confirmedBootID = st.BootID
	}
	return Status{ConfirmedBootID: confirmedBootID, ConfirmedImage: st.ConfirmedImage, Good: st.Good, Previous: st.Previous, Pending: st.Pending, TrialBootID: trialBootID(st), TrialImage: st.TrialImage, Corrupt: corrupt}, e
}
func trialBootID(st diskState) string {
	if st.TrialImage != "" {
		return st.BootID
	}
	return ""
}
func validBootID(id string) bool {
	return len(id) > 0 && len(id) <= 128 && strings.IndexFunc(id, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-')
	}) < 0
}
func (s *Store) verify(hash string) (release.Manifest, error) {
	return s.verifyContext(context.Background(), hash)
}
func (s *Store) verifyContext(ctx context.Context, hash string) (release.Manifest, error) {
	var m release.Manifest
	path, e := s.ImagePath(hash)
	if e != nil {
		return m, e
	}
	if hash == s.factory.ImageSHA256 {
		m = s.factory
	} else {
		f, e := regularOpen(filepath.Join(s.root, "manifests", hash+".json"))
		if e != nil {
			return m, e
		}
		m, e = release.DecodeManifest(f)
		f.Close()
		if e != nil {
			return m, e
		}
	}
	if e = m.Compatible(s.factory.KernelSHA256); e != nil {
		return m, e
	}
	if m.ImageSHA256 != hash {
		return m, errors.New("manifest image identity mismatch")
	}
	f, e := regularOpen(path)
	if e != nil {
		return m, e
	}
	defer f.Close()
	if e = verifyImage(ctx, f, m); e != nil {
		return m, e
	}
	return m, nil
}
func (s *Store) Verify(hash string) (release.Manifest, error) {
	unlock, e := s.lock(context.Background())
	if e != nil {
		return release.Manifest{}, e
	}
	defer unlock()
	return s.verify(hash)
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.r.Read(b)
}
func verifyImage(ctx context.Context, f *os.File, m release.Manifest) error {
	fi, e := f.Stat()
	if e != nil {
		return e
	}
	if fi.Size() != m.ImageSize {
		return errors.New("image size mismatch")
	}
	magic := make([]byte, 2)
	if _, e = f.ReadAt(magic, 1080); e != nil {
		return e
	}
	if !bytes.Equal(magic, []byte{0x53, 0xef}) {
		return errors.New("image lacks ext filesystem superblock")
	}
	features := make([]byte, 4)
	if _, e = f.ReadAt(features, 1120); e != nil {
		return e
	}
	if binary.LittleEndian.Uint32(features)&0x40 == 0 {
		return errors.New("image must be ext4 with extents")
	}
	if _, e = f.Seek(0, io.SeekStart); e != nil {
		return e
	}
	h := sha256.New()
	if _, e = io.Copy(h, contextReader{ctx, f}); e != nil {
		return e
	}
	if fmt.Sprintf("%x", h.Sum(nil)) != m.ImageSHA256 {
		return errors.New("image SHA-256 mismatch")
	}
	return ctx.Err()
}

// Stage publishes only complete verified raw images. It never changes selection
// and never replaces an existing content-addressed image, including corrupt ones.
func (s *Store) Stage(ctx context.Context, m release.Manifest, size int64, r io.Reader) error {
	if e := m.Compatible(s.factory.KernelSHA256); e != nil {
		return e
	}
	if size != m.ImageSize {
		return errors.New("upload size differs from manifest")
	}
	unlock, e := s.lock(ctx)
	if e != nil {
		return e
	}
	defer unlock()
	path, _ := s.ImagePath(m.ImageSHA256)
	if _, e = os.Lstat(path); e == nil {
		// A previous publication may have synced the image and failed before
		// writing its manifest. Verify those exact existing bytes and repair
		// only the absent manifest; never replace the image.
		f, err := regularOpen(path)
		if err != nil {
			return err
		}
		err = verifyImage(ctx, f, m)
		f.Close()
		if err != nil {
			return err
		}
		manifestPath := filepath.Join(s.root, "manifests", m.ImageSHA256+".json")
		if _, err = os.Lstat(manifestPath); err == nil {
			existing, err := s.verify(m.ImageSHA256)
			if err != nil {
				return err
			}
			if existing != m {
				return errors.New("immutable image already has a different manifest")
			}
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		b, err := json.Marshal(m)
		if err != nil {
			return err
		}
		return atomicWrite(manifestPath, append(b, '\n'))
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	var fs unix.Statfs_t
	if e = unix.Statfs(s.root, &fs); e != nil {
		return e
	}
	if uint64(size)+(1<<20) > uint64(fs.Bavail)*uint64(fs.Bsize) {
		return errors.New("insufficient release storage")
	}
	f, e := os.CreateTemp(filepath.Join(s.root, "images"), ".stage-")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	defer f.Close()
	n, e := io.Copy(f, io.LimitReader(contextReader{ctx, r}, size+1))
	if e != nil {
		return e
	}
	if n != size {
		return errors.New("upload size mismatch")
	}
	if e = verifyImage(ctx, f, m); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	// A process lock protects the no-overwrite check and rename on FAT (hard links
	// are unavailable). All writers to this dedicated directory must use Store.
	if _, e = os.Lstat(path); !errors.Is(e, os.ErrNotExist) {
		if e == nil {
			return errors.New("immutable image appeared during staging")
		}
		return e
	}
	if e = os.Rename(tmp, path); e != nil {
		return e
	}
	if e = syncDir(filepath.Dir(path)); e != nil {
		return e
	}
	b, e := json.Marshal(m)
	if e != nil {
		return e
	}
	return atomicWrite(filepath.Join(s.root, "manifests", m.ImageSHA256+".json"), append(b, '\n'))
}
func (s *Store) activate(ctx context.Context, st diskState, hash string) error {
	if st.Pending == hash {
		return nil
	}
	if st.Pending != "" || st.TrialImage != "" {
		return ErrBusy
	}
	if hash == st.Good {
		return errors.New("image is already known good")
	}
	if _, e := s.verifyContext(ctx, hash); e != nil {
		return e
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	st.Pending = hash
	return s.writeState(st)
}
func (s *Store) Activate(hash string) error {
	return s.ActivateContext(context.Background(), hash)
}
func (s *Store) ActivateContext(ctx context.Context, hash string) error {
	unlock, e := s.lock(ctx)
	if e != nil {
		return e
	}
	defer unlock()
	st, c, e := s.readState()
	if e != nil {
		return e
	}
	if c {
		return ErrCorruptState
	}
	return s.activate(ctx, st, hash)
}
func (s *Store) Rollback() error {
	return s.RollbackContext(context.Background())
}
func (s *Store) RollbackContext(ctx context.Context) error {
	unlock, e := s.lock(ctx)
	if e != nil {
		return e
	}
	defer unlock()
	st, c, e := s.readState()
	if e != nil {
		return e
	}
	if c {
		return ErrCorruptState
	}
	if st.Previous == "" {
		return errors.New("no previous known-good release")
	}
	return s.activate(ctx, st, st.Previous)
}
func (s *Store) selection(hash, bootID string, trial bool) (Selection, error) {
	m, e := s.verify(hash)
	if e != nil {
		return Selection{}, e
	}
	path, _ := s.ImagePath(hash)
	return Selection{Manifest: m, Path: path, Trial: trial, BootID: bootID}, nil
}
func (s *Store) goodSelection(st diskState, bootID string) (Selection, error) {
	sel, e := s.selection(st.Good, bootID, false)
	if e == nil {
		return sel, nil
	}
	return s.selection(s.factory.ImageSHA256, bootID, false)
}

// BeginBoot durably consumes a trial before returning it. Calling twice for one
// boot is an error. A subsequent boot clears an unconfirmed consumed trial.
// Mount/init failures after selection must try Good then factory in the caller.
func (s *Store) BeginBoot(bootID string) (Selection, error) {
	if !validBootID(bootID) {
		return Selection{}, errors.New("invalid boot ID")
	}
	unlock, e := s.lock(context.Background())
	if e != nil {
		return Selection{}, e
	}
	defer unlock()
	st, c, e := s.readState()
	if e != nil {
		return Selection{}, e
	}
	if c {
		return s.goodSelection(s.fallbackState(), bootID)
	}
	if st.BootID == bootID {
		return Selection{}, errors.New("boot selection already consumed for this boot")
	}
	candidate := st.Pending
	st.Pending = ""
	st.TrialImage = ""
	st.ConfirmedImage = ""
	st.BootID = bootID
	if candidate != "" {
		sel, e := s.selection(candidate, bootID, true)
		if e == nil {
			st.TrialImage = candidate
			if e = s.writeState(st); e != nil {
				return Selection{}, e
			}
			return sel, nil
		}
	}
	if e = s.writeState(st); e != nil {
		return Selection{}, e
	}
	return s.goodSelection(st, bootID)
}
func (s *Store) Confirm(bootID, imageSHA string) error {
	return s.ConfirmContext(context.Background(), bootID, imageSHA)
}
func (s *Store) ConfirmContext(ctx context.Context, bootID, imageSHA string) error {
	if !validBootID(bootID) || !release.ValidHash(imageSHA) {
		return errors.New("invalid confirmation identity")
	}
	unlock, e := s.lock(ctx)
	if e != nil {
		return e
	}
	defer unlock()
	st, c, e := s.readState()
	if e != nil {
		return e
	}
	if c {
		return ErrCorruptState
	}
	if st.BootID != bootID {
		return errors.New("confirmation boot does not match")
	}
	if st.ConfirmedImage == imageSHA && st.Good == imageSHA {
		return s.syncState(ctx)
	}
	if st.TrialImage != imageSHA {
		return errors.New("confirmation image is not this boot's trial")
	}
	if _, e = s.verifyContext(ctx, imageSHA); e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	st.Previous = st.Good
	st.Good = imageSHA
	st.TrialImage = ""
	st.ConfirmedImage = imageSHA
	return s.writeState(st)
}
