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
	"syscall"

	release "github.com/DeanoC/FogCast/appliance"
	"golang.org/x/sys/unix"
)

// CapacityContext is a non-mutating pre-upload check, not a reservation.
// When an unprotected image exists the estimate may be inaccurate (for
// example because of sparse files or hard links), so it does not reject based
// on the estimate alone. Stage remains authoritative.
func (s *Store) CapacityContext(ctx context.Context, hash string, size int64) (release.Capacity, error) {
	if !release.ValidHash(hash) || size < 2048 || size > release.MaxImageSize {
		return release.Capacity{}, errors.New("invalid image identity or size")
	}
	unlock, err := s.lock(ctx)
	if err != nil {
		return release.Capacity{}, err
	}
	defer unlock()
	result := release.Capacity{Required: releaseStorageNeed(size)}
	path, _ := s.ImagePath(hash)
	if _, err = os.Lstat(path); err == nil {
		// Stage verifies the existing immutable image and manifest before reuse;
		// it does not require space for a second full image.
		result.Existing, result.Possible = true, true
		return result, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return release.Capacity{}, err
	}
	result.Available, err = s.availableBytes(s.root)
	if err != nil {
		return release.Capacity{}, err
	}
	if result.Available >= result.Required {
		result.Possible = true
		return result, nil
	}
	keep, err := s.protectedImages()
	if err != nil {
		return release.Capacity{}, err
	}
	entries, err := os.ReadDir(filepath.Join(s.root, "images"))
	if err != nil {
		return release.Capacity{}, err
	}
	var candidates int
	var fatUpper uint64
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return release.Capacity{}, err
		}
		candidate, ok := publishedImageHash(entry.Name())
		if !ok {
			continue
		}
		if _, protected := keep[candidate]; protected {
			continue
		}
		info, err := os.Lstat(filepath.Join(s.root, "images", entry.Name()))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return release.Capacity{}, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		candidates++
		// Logical length is an optimistic estimate; it can exceed allocated
		// bytes for sparse or hard-linked files. Never use it to refuse Stage.
		if info.Size() > 0 {
			n := uint64(info.Size())
			if ^uint64(0)-result.Reclaimable < n {
				result.Reclaimable = ^uint64(0)
			} else {
				result.Reclaimable += n
			}
		}
		// Stage also deletes the associated manifest and splash sidecars.
		// On FAT the file allocation and directory metadata are bounded by
		// the file's allocated blocks, with extra cluster slack. Count hard
		// links separately and sparse files at logical length to overestimate.
		for _, path := range []string{
			filepath.Join(s.root, "images", entry.Name()),
			filepath.Join(s.root, "manifests", candidate+".json"),
			filepath.Join(s.root, "splashes", candidate+".rbf"),
			filepath.Join(s.root, "splashes", candidate+".sha256"),
		} {
			upper, err := fatFileReclaimUpper(path)
			if err != nil {
				return release.Capacity{}, err
			}
			fatUpper = addCapacity(fatUpper, upper)
		}
	}
	result.SpillChecked = true
	if s.spillRoot != "" {
		info, statErr := os.Stat(s.spillRoot)
		result.Spill = statErr == nil && info.IsDir() && spillInsideRelease(s.root, s.spillRoot) == nil
	} else {
		candidates := releaseSpillCandidates
		if s.spillCandidates != nil {
			candidates = s.spillCandidates
		}
		for _, dir := range candidates() {
			if s.spillUsable(dir, size) {
				result.Spill = true
				break
			}
		}
	}
	result.Possible = result.Spill && candidates > 0
	if result.Possible {
		// Only use the bound to reject on FAT, the release filesystem. Other
		// filesystems can have different allocation/metadata semantics; there
		// the estimate is informational and Stage decides.
		if kind, err := s.filesystemType(s.root); err == nil && kind == uint32(unix.MSDOS_SUPER_MAGIC) {
			result.ReclaimUpper = fatUpper
			result.UpperKnown = true
			result.Possible = addCapacity(result.Available, fatUpper) >= result.Required
		}
	}
	return result, nil
}

// FAT's allocation tables are reserved on the filesystem, not released per
// file. The extra 1 MiB per existing directory entry is deliberately much
// larger than a FAT cluster or entry, so the estimate stays optimistic.
func fatFileReclaimUpper(path string) (uint64, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var size uint64
	if info.Size() > 0 {
		size = uint64(info.Size())
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && st.Blocks > 0 {
		allocated := ^uint64(0)
		if uint64(st.Blocks) <= ^uint64(0)/512 {
			allocated = uint64(st.Blocks) * 512
		}
		if allocated > size {
			size = allocated
		}
	}
	return addCapacity(size, releaseHeadroom), nil
}

func addCapacity(a, b uint64) uint64 {
	if ^uint64(0)-a < b {
		return ^uint64(0)
	}
	return a + b
}

func statFilesystemType(path string) (uint32, error) {
	var fs unix.Statfs_t
	if err := unix.Statfs(path, &fs); err != nil {
		return 0, err
	}
	return uint32(fs.Type), nil
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
// If the release volume lacks room for the image, margin, and splash reserve,
// Stage verifies the upload on a different non-memory filesystem before
// deleting anything. That spill filesystem needs the image plus the 1 MiB
// margin; the splash reserve stays on the release volume. A truncated or
// corrupt upload removes only that temporary file. After publication, Stage
// keeps the current good image, one verified fallback, the new staged image,
// in-progress selections, the boot-ticket image, and loop-backed images. It
// removes other releases oldest first without touching boot files. If no
// qualifying filesystem can hold the upload, Stage returns
// ErrInsufficientStorage without deleting images. After a verified spill,
// unprotected images may have been pruned before a remaining shortage is
// detected; retained images stay protected.
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
			return s.enforceRetention(ctx, m.ImageSHA256)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		b, err := json.Marshal(m)
		if err != nil {
			return err
		}
		if err = atomicWrite(manifestPath, append(b, '\n')); err != nil {
			return err
		}
		return s.enforceRetention(ctx, m.ImageSHA256)
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	free, e := s.availableBytes(s.root)
	if e != nil {
		return e
	}
	if free >= releaseStorageNeed(size) {
		if e = s.publishNewImage(ctx, m, path, r, size); e != nil {
			return e
		}
		return s.enforceRetention(ctx, m.ImageSHA256)
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
	if e = s.publishNewImage(ctx, m, path, in, size); e != nil {
		return e
	}
	return s.enforceRetention(ctx, m.ImageSHA256)
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
