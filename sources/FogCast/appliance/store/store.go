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
	"sort"
	"strings"
	"time"

	release "github.com/DeanoC/FogCast/appliance"
	"github.com/DeanoC/FogCast/appliance/blockprobe"
	"golang.org/x/sys/unix"
)

const DefaultRoot = "/media/fat/fogcast/releases"

var ErrCorruptState = errors.New("release selection state is corrupt; factory fallback only")
var ErrBusy = errors.New("release trial or pending activation already exists")

// ErrInsufficientStorage means the release volume cannot hold the image.
// Staging also returns it, without deleting anything, when the only way to
// make room is to prune and no filesystem outside the release volume can hold
// the upload long enough to verify it.
var ErrInsufficientStorage = errors.New("insufficient release storage")

// ErrBootTicketRewrite means this boot's release confirmation is already
// durable and the matching boot.json trial flag could not be cleared. The
// leftover trial ticket stays acceptable for this boot and image. Callers
// retry the rewrite; the confirmation itself has succeeded.
var ErrBootTicketRewrite = errors.New("boot ticket rewrite failed after durable confirmation")

var errUnreadableBootTicket = errors.New("refusing to prune releases: boot ticket is unreadable")

const releaseHeadroom = 1 << 20

const (
	// MaxStagedSplash is the largest idle core confirmation will copy into the
	// release store. appliancesplash rejects a larger file.
	MaxStagedSplash = 8 << 20
	// MaxSplashSidecar is the staged digest file, including its trailing newline.
	MaxSplashSidecar = 80
	// SplashReserve is the free space Stage leaves after the image so
	// confirmation can write the splash. The same-directory temporary and the
	// destination both exist until rename, and the sidecar is written after.
	SplashReserve = MaxStagedSplash + MaxSplashSidecar + MaxStagedSplash
)

func releaseStorageNeed(size int64) uint64 {
	return uint64(size) + releaseHeadroom + SplashReserve
}

type Store struct {
	root    string
	factory release.Manifest
	// Keep state persistence operations together: a reader may need to finish
	// syncing a confirmation made visible by a rename whose directory sync failed.
	syncFile       func(*os.File) error
	syncDirectory  func(string) error
	availableBytes func(string) (uint64, error)
	loopBackings   func() ([]string, error)
	// spillRoot receives an upload that does not fit on the release volume so
	// it can be verified before any image is deleted. Tests set it. Production
	// leaves it empty and searches a mounted FESDATA3 filesystem instead.
	spillRoot string
	// spillCandidates overrides that search. Nil uses the production paths.
	spillCandidates func() []string
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
	return NewForBoard(root, factory, release.Board)
}

// NewForBoard pins the store to a separately selected board. Its factory and
// every staged or retained manifest must match that board and kernel.
func NewForBoard(root string, factory release.Manifest, board string) (*Store, error) {
	if e := factory.ValidateForBoard(board); e != nil {
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
	return &Store{
		root:           root,
		factory:        factory,
		syncFile:       (*os.File).Sync,
		syncDirectory:  syncDir,
		availableBytes: statAvailable,
		loopBackings:   loopBackingFiles,
	}, nil
}
func (s *Store) ImagePath(hash string) (string, error) {
	if !release.ValidHash(hash) {
		return "", errors.New("invalid image hash")
	}
	return filepath.Join(s.root, "images", hash+".img"), nil
}

// SplashPaths returns the staged splash for hash and its sha256 sidecar.
// Confirmation publishes the splash onto FAT /idle.rbf. Prune deletes both
// files when it deletes the image. Names are not prefixes of each other, so
// the kit exFAT lookup cannot alias one to the other.
func SplashPaths(root, hash string) (rbf, digest string, err error) {
	if !release.ValidHash(hash) {
		return "", "", errors.New("invalid image hash")
	}
	if !filepath.IsAbs(root) {
		return "", "", errors.New("release root must be absolute")
	}
	dir := filepath.Join(root, "splashes")
	return filepath.Join(dir, hash+".rbf"), filepath.Join(dir, hash+".sha256"), nil
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
//
// When confirmation also publishes a FAT splash, ConfirmedImage is recorded
// only after that publish. Store acceptance alone must not disarm the watchdog.
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

// SplashPendingContext reports that this boot's image is known-good and the
// watchdog has not been shown confirmation yet. That is the recoverable gap
// between accepting the trial and publishing its FAT splash.
func (s *Store) SplashPendingContext(ctx context.Context, bootID, imageSHA string) (bool, error) {
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
	if corrupt || st.BootID != bootID || st.Good != imageSHA || st.TrialImage != "" || st.ConfirmedImage != "" {
		return false, nil
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
		m, _, e = release.DecodeManifestForBoard(f, s.factory.Board)
		f.Close()
		if e != nil {
			return m, e
		}
	}
	if e = m.CompatibleForBoard(s.factory.KernelSHA256, s.factory.Board); e != nil {
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
// When the release volume has room for the image, a 1 MiB margin, and the
// splash confirmation will store, Stage writes and verifies it there and does
// not delete anything. Otherwise it verifies the upload on a different,
// non-memory filesystem before deleting anything. That spill filesystem needs
// the image plus the 1 MiB margin; the splash reserve stays on the release
// volume. A truncated or corrupt upload removes only that temporary file.
// After verification, Stage deletes other release images oldest first until
// the image, the margin, and the splash reserve fit, then copies the verified
// file into this store.
// It keeps factory, known-good, previous, pending, trial, the boot.json image,
// and every image in this store that backs a loop device. It does not remove
// boot files. If no qualifying filesystem can hold the upload, or the retained
// images still leave too little room, Stage returns ErrInsufficientStorage and
// deletes nothing.
func (s *Store) Stage(ctx context.Context, m release.Manifest, size int64, r io.Reader) error {
	if e := m.CompatibleForBoard(s.factory.KernelSHA256, s.factory.Board); e != nil {
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
	free, e := s.availableBytes(s.root)
	if e != nil {
		return e
	}
	if free >= releaseStorageNeed(size) {
		return s.publishNewImage(ctx, m, path, r, size)
	}
	// Retention is resolved before the upload is admitted so an unreadable
	// boot ticket, corrupt selection, or failed loop scan still refuses
	// without reading or deleting.
	if _, e = s.protectedImages(); e != nil {
		return e
	}
	spilled, e := s.admitUpload(ctx, m, size, r)
	if e != nil {
		return e
	}
	defer os.Remove(spilled)
	if e = s.ensureSpace(ctx, size); e != nil {
		return e
	}
	in, e := regularOpen(spilled)
	if e != nil {
		return e
	}
	defer in.Close()
	return s.publishNewImage(ctx, m, path, in, size)
}

func (s *Store) publishNewImage(ctx context.Context, m release.Manifest, path string, r io.Reader, size int64) error {
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

// admitUpload writes the upload to a filesystem other than the release volume
// and verifies it there. The returned file is complete. The caller removes it.
func (s *Store) admitUpload(ctx context.Context, m release.Manifest, size int64, r io.Reader) (string, error) {
	f, e := s.openSpill(size)
	if e != nil {
		return "", e
	}
	tmp := f.Name()
	defer f.Close()
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(tmp)
		}
	}()
	n, e := io.Copy(f, io.LimitReader(contextReader{ctx, r}, size+1))
	if e != nil {
		return "", e
	}
	if n != size {
		return "", errors.New("upload size mismatch")
	}
	if e = verifyImage(ctx, f, m); e != nil {
		return "", e
	}
	if e = f.Sync(); e != nil {
		return "", e
	}
	if e = f.Close(); e != nil {
		return "", e
	}
	if e = ctx.Err(); e != nil {
		return "", e
	}
	remove = false
	return tmp, nil
}

func (s *Store) openSpill(size int64) (*os.File, error) {
	if s.spillRoot != "" {
		if e := spillInsideRelease(s.root, s.spillRoot); e != nil {
			return nil, e
		}
		if e := removeStaleSpillFiles(s.spillRoot); e != nil {
			return nil, e
		}
		return os.CreateTemp(s.spillRoot, spillFilePrefix)
	}
	candidates := releaseSpillCandidates
	if s.spillCandidates != nil {
		candidates = s.spillCandidates
	}
	for _, dir := range candidates() {
		if !s.spillUsable(dir, size) {
			continue
		}
		// One directory per release root. Stage holds this store's lock, so a
		// file left here belongs to a process that exited before removing it.
		// Another store's in-progress upload lives in its own directory.
		staging := filepath.Join(dir, ".release-stage", spillOwner(s.root))
		if e := os.MkdirAll(staging, 0700); e != nil {
			continue
		}
		if e := removeStaleSpillFiles(staging); e != nil {
			continue
		}
		if !s.spillUsable(staging, size) {
			continue
		}
		f, e := os.CreateTemp(staging, spillFilePrefix)
		if e != nil {
			continue
		}
		return f, nil
	}
	return nil, ErrInsufficientStorage
}

const spillFilePrefix = ".stage-spill-"

func spillOwner(root string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(root)))
	return fmt.Sprintf("%x", sum[:8])
}

// removeStaleSpillFiles deletes regular spill files left by a crashed Stage.
// The caller holds the store lock. Symlinks and other names are left in place.
func removeStaleSpillFiles(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	removed := false
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), spillFilePrefix) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if err = os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		removed = true
	}
	if !removed {
		return nil
	}
	return syncDir(dir)
}

func (s *Store) spillUsable(dir string, size int64) bool {
	if spillInsideRelease(s.root, dir) != nil {
		return false
	}
	rootDev, e := deviceID(s.root)
	if e != nil {
		return false
	}
	dev, e := deviceID(dir)
	if e != nil {
		return false
	}
	var fs unix.Statfs_t
	if unix.Statfs(dir, &fs) != nil {
		return false
	}
	return admissionFilesystem(fs, dev, rootDev, uint64(size)+releaseHeadroom)
}

func admissionFilesystem(fs unix.Statfs_t, dev, rootDev, need uint64) bool {
	if dev == 0 || dev == rootDev {
		return false
	}
	// Statfs_t.Type is int32 on ARM. RAMFS_MAGIC does not fit in int32, so
	// compare the 32-bit kind. fes-boot is built for that architecture.
	if memoryFilesystem(uint32(fs.Type)) {
		return false
	}
	if fs.Flags&unix.ST_RDONLY != 0 {
		return false
	}
	if fs.Bsize <= 0 {
		return false
	}
	return fs.Bavail*uint64(fs.Bsize) >= need
}

func memoryFilesystem(kind uint32) bool {
	return kind == uint32(unix.TMPFS_MAGIC) || kind == uint32(unix.RAMFS_MAGIC)
}

func spillInsideRelease(root, dir string) error {
	rel, e := filepath.Rel(filepath.Clean(root), filepath.Clean(dir))
	if e != nil {
		return e
	}
	if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("release admission spill is inside the release root")
	}
	return nil
}

func deviceID(path string) (uint64, error) {
	var st unix.Stat_t
	if e := unix.Stat(path, &st); e != nil {
		return 0, e
	}
	return uint64(st.Dev), nil
}

func releaseSpillCandidates() []string {
	return []string{
		"/run/fogcast/fesdata3",
		"/media/fat/fogcast/evidence",
		"/media/fat/fogcast/cache",
		"/media/fat/fogcast/launcher-cache",
		"/media/fat/fogcast/saves",
		"/media/fat/fogcast/core-data",
	}
}

func statAvailable(root string) (uint64, error) {
	var fs unix.Statfs_t
	if err := unix.Statfs(root, &fs); err != nil {
		return 0, err
	}
	return uint64(fs.Bavail) * uint64(fs.Bsize), nil
}

// ensureSpace frees unreferenced images until size, the staging margin, and
// the splash reserve fit. Protected identities are resolved before the first
// deletion. Callers verify the incoming image before this runs, so a failed
// upload never reaches it.
func (s *Store) ensureSpace(ctx context.Context, size int64) error {
	if size < 0 {
		return errors.New("invalid image size")
	}
	need := releaseStorageNeed(size)
	free, err := s.availableBytes(s.root)
	if err != nil {
		return err
	}
	if free >= need {
		return nil
	}
	keep, err := s.protectedImages()
	if err != nil {
		return err
	}
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		removed, err := s.removeOldestUnreferenced(keep)
		if err != nil {
			return err
		}
		if !removed {
			return ErrInsufficientStorage
		}
		free, err = s.availableBytes(s.root)
		if err != nil {
			return err
		}
		if free >= need {
			return nil
		}
	}
}

func (s *Store) protectedImages() (map[string]struct{}, error) {
	st, corrupt, err := s.readState()
	if err != nil {
		return nil, err
	}
	if corrupt {
		return nil, ErrCorruptState
	}
	keep := map[string]struct{}{}
	add := func(hash string) {
		if release.ValidHash(hash) {
			keep[hash] = struct{}{}
		}
	}
	add(s.factory.ImageSHA256)
	add(st.Good)
	add(st.Previous)
	add(st.Pending)
	add(st.TrialImage)
	add(st.ConfirmedImage)
	boot, err := s.bootImage()
	if err != nil {
		return nil, err
	}
	add(boot)
	paths, err := s.loopBackings()
	if err != nil {
		return nil, fmt.Errorf("refusing to prune releases: %w", err)
	}
	imagesDir := filepath.Join(s.root, "images")
	for _, path := range paths {
		if hash, ok := imageHashFromBacking(imagesDir, path); ok {
			add(hash)
		}
	}
	return keep, nil
}

type bootTicket struct {
	BootID      string `json:"boot_id"`
	ImageSHA256 string `json:"image_sha256"`
	Trial       bool   `json:"trial"`
}

func (s *Store) bootImage() (string, error) {
	ticket, err := s.readBootTicket()
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return ticket.ImageSHA256, nil
}

func (s *Store) readBootTicket() (bootTicket, error) {
	var ticket bootTicket
	f, err := regularOpen(filepath.Join(s.root, "boot.json"))
	if err != nil {
		return ticket, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil {
		return ticket, err
	}
	if len(b) > 4096 {
		return ticket, errUnreadableBootTicket
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if dec.Decode(&ticket) != nil || !release.ValidHash(ticket.ImageSHA256) {
		return ticket, errUnreadableBootTicket
	}
	if err = dec.Decode(&struct{}{}); err != io.EOF {
		return ticket, errUnreadableBootTicket
	}
	return ticket, nil
}

// clearBootTrial rewrites a matching trial ticket to trial:false. The boot id
// and image stay as the bootstrap recorded them. A missing ticket, or a ticket
// for another boot or image, is left untouched. The caller holds the store lock.
func (s *Store) clearBootTrial(bootID, imageSHA string) error {
	ticket, err := s.readBootTicket()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if ticket.BootID != bootID || ticket.ImageSHA256 != imageSHA || !ticket.Trial {
		return nil
	}
	ticket.Trial = false
	body, err := json.Marshal(ticket)
	if err != nil {
		return err
	}
	return atomicWriteWithSync(filepath.Join(s.root, "boot.json"), append(body, '\n'), s.syncFile, s.syncDirectory)
}

// clearConfirmedBootTicket clears a durable confirmation's trial ticket.
// It is only called after the release record and its directory are synced.
// A rewrite error is ErrBootTicketRewrite and does not undo that record.
func (s *Store) clearConfirmedBootTicket(bootID, imageSHA string) error {
	if err := s.clearBootTrial(bootID, imageSHA); err != nil {
		return fmt.Errorf("%w: %w", ErrBootTicketRewrite, err)
	}
	return nil
}

func (s *Store) removeOldestUnreferenced(keep map[string]struct{}) (bool, error) {
	dir := filepath.Join(s.root, "images")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	type candidate struct {
		hash string
		mod  time.Time
	}
	var stale []candidate
	for _, entry := range entries {
		hash, ok := publishedImageHash(entry.Name())
		if !ok {
			continue
		}
		if _, protected := keep[hash]; protected {
			continue
		}
		info, err := os.Lstat(filepath.Join(dir, entry.Name()))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		stale = append(stale, candidate{hash: hash, mod: info.ModTime()})
	}
	if len(stale) == 0 {
		return false, nil
	}
	sort.Slice(stale, func(i, j int) bool {
		if !stale[i].mod.Equal(stale[j].mod) {
			return stale[i].mod.Before(stale[j].mod)
		}
		return stale[i].hash < stale[j].hash
	})
	hash := stale[0].hash
	// Drop the staged splash first. A failure leaves the image in place so the
	// next prune retries; removing the image first would leak the splash.
	if err = s.removeImageSplash(hash); err != nil {
		return false, err
	}
	if err = os.Remove(filepath.Join(dir, hash+".img")); err != nil {
		return false, err
	}
	if err = s.syncDirectory(dir); err != nil {
		return false, err
	}
	manifestDir := filepath.Join(s.root, "manifests")
	manifest := filepath.Join(manifestDir, hash+".json")
	info, err := os.Lstat(manifest)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, errors.New("release manifest is not a regular file")
	}
	if err = os.Remove(manifest); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err = s.syncDirectory(manifestDir); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) removeImageSplash(hash string) error {
	rbf, digest, err := SplashPaths(s.root, hash)
	if err != nil {
		return err
	}
	removed := false
	for _, path := range []string{rbf, digest} {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.IsDir() {
			return errors.New("release splash is not a regular file")
		}
		if err = os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		removed = true
	}
	if !removed {
		return nil
	}
	return s.syncDirectory(filepath.Dir(rbf))
}

func publishedImageHash(name string) (string, bool) {
	hash, ok := strings.CutSuffix(name, ".img")
	if !ok || !release.ValidHash(hash) {
		return "", false
	}
	return hash, true
}

func imageHashFromBacking(imagesDir, backing string) (string, bool) {
	backing = strings.TrimSpace(backing)
	backing = strings.TrimSuffix(backing, " (deleted)")
	if backing == "" {
		return "", false
	}
	cleaned := filepath.Clean(backing)
	hash, ok := publishedImageHash(filepath.Base(cleaned))
	if !ok || filepath.Dir(cleaned) != filepath.Clean(imagesDir) {
		return "", false
	}
	return hash, true
}

func loopBackingFiles() ([]string, error) {
	return loopBackingFilesFrom("/sys/block")
}

func loopBackingFilesFrom(blockDir string) ([]string, error) {
	entries, err := os.ReadDir(blockDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var paths []string
	for _, entry := range entries {
		// Optical devices and disks with no usable media are ignored before any
		// further sysfs read. Loop backing files are read only for the rest.
		if decision, ok := blockprobe.Decide(blockDir, entry.Name()); ok && !decision.Allow {
			continue
		}
		if !loopDeviceName(entry.Name()) {
			continue
		}
		backing, err := os.ReadFile(filepath.Join(blockDir, entry.Name(), "loop", "backing_file"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		path := strings.TrimSpace(string(backing))
		if path != "" {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

func loopDeviceName(name string) bool {
	rest, ok := strings.CutPrefix(name, "loop")
	if !ok || rest == "" {
		return false
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
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
		if e = s.syncState(ctx); e != nil {
			return e
		}
		// The release is already durable. Still clear a trial ticket left by an
		// interrupted confirm or an older writer, so this boot does not keep
		// reporting trial until the next reboot. A rewrite failure leaves the
		// durable record in place.
		return s.clearConfirmedBootTicket(bootID, imageSHA)
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
	if e = s.writeState(st); e != nil {
		return e
	}
	return s.clearConfirmedBootTicket(bootID, imageSHA)
}

// AcceptTrialContext promotes this boot's trial to known-good without recording
// the watchdog boundary. ConfirmedImage stays empty until CompleteSplashContext.
// A retry after promotion only finishes the state sync.
func (s *Store) AcceptTrialContext(ctx context.Context, bootID, imageSHA string) error {
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
	if st.Good == imageSHA && st.TrialImage == "" && (st.ConfirmedImage == "" || st.ConfirmedImage == imageSHA) {
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
	if st.Good != imageSHA {
		st.Previous = st.Good
	}
	st.Good = imageSHA
	st.TrialImage = ""
	st.ConfirmedImage = ""
	return s.writeState(st)
}

// CompleteSplashContext records the watchdog confirmation for an image whose
// splash is already on FAT. It does not publish the splash itself.
func (s *Store) CompleteSplashContext(ctx context.Context, bootID, imageSHA string) error {
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
	if st.BootID != bootID || st.Good != imageSHA || st.TrialImage != "" {
		return errors.New("splash confirmation is not this boot's accepted image")
	}
	if st.ConfirmedImage == imageSHA {
		return s.syncState(ctx)
	}
	if st.ConfirmedImage != "" {
		return errors.New("splash confirmation does not match")
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	st.ConfirmedImage = imageSHA
	return s.writeState(st)
}
