// Package appliance owns immutable release storage and durable boot selection.
// The card/filesystem must honor fsync and rename. Checksums detect torn state;
// they cannot compensate for a controller that lies about persistence.
package appliance

import (
	"bytes"
	"context"
	"crypto/sha256"
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

// BootFallbackFilename names the optional fallback record beside boot.json.
const BootFallbackFilename = "boot-fallback.json"

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

// ErrRetentionAfterConfirmation means confirmation and matching boot-ticket
// handling succeeded, but release pruning failed. Retrying confirmation is
// safe and retries retention without undoing the durable confirmation.
var ErrRetentionAfterConfirmation = errors.New("release retention failed after durable confirmation")

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
	filesystemType func(string) (uint32, error)
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
	Manifest            release.Manifest `json:"manifest"`
	Path                string           `json:"path"`
	Trial               bool             `json:"trial"`
	BootID              string           `json:"boot_id"`
	FallbackImageSHA256 string           `json:"fallback_image_sha256"`
}

// BootTicket is the stable three-field ticket read by the bootstrap and target
// agent. Recovery metadata lives in a separate, identity-bound sidecar.
type BootTicket struct {
	BootID      string `json:"boot_id"`
	ImageSHA256 string `json:"image_sha256"`
	Trial       bool   `json:"trial"`
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
		filesystemType: statFilesystemType,
		loopBackings:   loopBackingFiles,
	}, nil
}

// Board is the selected board validated when this store was constructed.
func (s *Store) Board() string { return s.factory.Board }

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
