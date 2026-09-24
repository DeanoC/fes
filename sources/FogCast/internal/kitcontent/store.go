// Package kitcontent is the target agent's content store for one node.
// It implements meshcontent.Executor: content-ids live as separate
// objects, Pull copies bytes from a content source onto this kit, and
// LinkExpansion records an expansion slot's own bytes. The store does
// not program the FPGA, does not choose a node, and does not claim or
// release a kit lease.
//
// A slot is Present only after a verified object is committed. A pull
// that is still running is Checking. A canceled or failed pull deletes
// its partial file and leaves the id Missing. Expansion links name the
// slot-bytes content-id. They do not replace primary media and they
// do not record a programmed image.
package kitcontent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/DeanoC/FogCast/internal/meshcontent"
	"golang.org/x/sys/unix"
)

var (
	// meshContentReserve is free space a pull leaves on the card.
	// A pull that would start below this reserve fails closed. Each
	// chunk is also capped at free-reserve, and free space is read
	// again while the copy is running.
	meshContentReserve uint64 = 32 << 20
	// meshContentQuota is the most bytes this store will keep, counting
	// committed objects and in-flight partial files together.
	meshContentQuota int64 = 2 << 30
)

// availableBytes reports free bytes at path. Tests replace it.
var availableBytes = statAvailable

// Source is the content source a kit pull reads. A nil source
// advertises nothing. Open's reader must return when ctx is canceled.
type Source interface {
	Advertises(id meshcontent.ContentID) bool
	Open(ctx context.Context, id meshcontent.ContentID) (io.ReadCloser, error)
}

// Store is one node's content store. It is safe for concurrent Slot
// and Pull calls. LinkExpansion for the same name and content-id is
// idempotent. A later link failure does not remove an earlier link.
type Store struct {
	root     string
	nodeID   string
	source   Source
	abis     []meshcontent.EligibleABI
	mu       sync.Mutex
	inflight map[string]struct{}
	packages []string
}

// Open prepares one node's store. It does not create directories: the
// agent can open the store on boot without writing under the media
// root. The first pull or link creates objects, partial, and links.
// A partial directory left by a crashed pull is swept. nodeID is this
// executor. abis may be empty; an empty list is not eligibility.
// source may be nil.
func Open(root, nodeID string, source Source, abis []meshcontent.EligibleABI) (*Store, error) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return nil, errors.New("mesh content store requires a node id")
	}
	root = filepath.Clean(root)
	if root == "" || root == "." || root == string(filepath.Separator) {
		return nil, errors.New("mesh content store requires a root")
	}
	store := &Store{
		root:     root,
		nodeID:   nodeID,
		source:   source,
		abis:     append([]meshcontent.EligibleABI(nil), abis...),
		inflight: map[string]struct{}{},
	}
	store.sweepPartial()
	return store, nil
}

func (s *Store) ensureDirs() error {
	for _, sub := range []string{"objects", "partial", "links"} {
		if err := os.MkdirAll(filepath.Join(s.root, sub), 0o700); err != nil {
			return err
		}
	}
	return nil
}

// sweepPartial removes files left in partial/ by a pull that did not
// finish. Open runs it once, before any pull on this store is in flight.
func (s *Store) sweepPartial() {
	entries, err := os.ReadDir(filepath.Join(s.root, "partial"))
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		_ = os.Remove(filepath.Join(s.root, "partial", entry.Name()))
	}
}

func (s *Store) NodeID() string {
	if s == nil {
		return ""
	}
	return s.nodeID
}

func (s *Store) EligibleABIs() []meshcontent.EligibleABI {
	if s == nil {
		return nil
	}
	return append([]meshcontent.EligibleABI(nil), s.abis...)
}

// SetPackages records described package ids installed on this node.
// ReadyHere reads them through PackageHolder. Call it before the store
// serves requests. An empty list is not eligibility. Ids that are not
// 64 lowercase hex digits are dropped.
func (s *Store) SetPackages(ids []string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.packages = normalizePackageIDs(ids)
	s.mu.Unlock()
}

// Packages returns the described package ids SetPackages recorded.
func (s *Store) Packages() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.packages...)
}

func (s *Store) SourceAdvertises(id meshcontent.ContentID) bool {
	if s == nil || s.source == nil || id.Validate() != nil {
		return false
	}
	return s.source.Advertises(id)
}

// Slot reports Present only for a committed object. An in-flight pull
// is Checking even when a partial file exists. Anything else is Missing.
func (s *Store) Slot(id meshcontent.ContentID) meshcontent.SlotState {
	if s == nil || id.Validate() != nil {
		return meshcontent.StateMissing
	}
	s.mu.Lock()
	_, pulling := s.inflight[id.String()]
	s.mu.Unlock()
	if pulling {
		return meshcontent.StateChecking
	}
	if s.objectReady(id) {
		return meshcontent.StatePresent
	}
	return meshcontent.StateMissing
}

// Pull copies id from the content source. It returns Present only
// after the digest matches and the object is renamed into place.
// Cancel and every other failure remove the partial file.
func (s *Store) Pull(ctx context.Context, id meshcontent.ContentID) (meshcontent.SlotState, error) {
	if s == nil {
		return "", meshcontent.ErrContentPullFailed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := id.Validate(); err != nil {
		return "", err
	}
	key := id.String()
	s.mu.Lock()
	if _, pulling := s.inflight[key]; pulling {
		s.mu.Unlock()
		return meshcontent.StateChecking, nil
	}
	if s.objectReady(id) {
		s.mu.Unlock()
		return meshcontent.StatePresent, nil
	}
	if s.source == nil || !s.source.Advertises(id) {
		s.mu.Unlock()
		return "", meshcontent.ErrContentPullFailed
	}
	s.inflight[key] = struct{}{}
	s.mu.Unlock()

	if err := s.ensureDirs(); err != nil {
		s.mu.Lock()
		delete(s.inflight, key)
		s.mu.Unlock()
		return "", meshcontent.ErrContentPullFailed
	}
	state, err := s.copy(ctx, id)
	s.mu.Lock()
	delete(s.inflight, key)
	s.mu.Unlock()
	return state, err
}

func (s *Store) copy(ctx context.Context, id meshcontent.ContentID) (meshcontent.SlotState, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	// Refuse before creating a partial when the card or the quota already
	// has no room. The writer applies the same cap to every chunk and
	// re-reads free space, so a pull cannot run the card down to ENOSPC
	// and two in-flight pulls cannot each spend the same remainder.
	if !s.roomFor(1) {
		return "", meshcontent.ErrContentPullFailed
	}
	body, err := s.source.Open(ctx, id)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", meshcontent.ErrContentPullFailed
	}
	defer body.Close()

	partial := filepath.Join(s.root, "partial")
	tmp, err := os.CreateTemp(partial, id.Digest+".")
	if err != nil {
		return "", meshcontent.ErrContentPullFailed
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	sum := sha256.New()
	limited := &spaceWriter{store: s, w: io.MultiWriter(tmp, sum)}
	if _, err := io.Copy(limited, &ctxReader{ctx: ctx, r: body}); err != nil {
		cleanup()
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", meshcontent.ErrContentPullFailed
	}
	if hex.EncodeToString(sum.Sum(nil)) != id.Digest {
		cleanup()
		return "", meshcontent.ErrContentPullFailed
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return "", meshcontent.ErrContentPullFailed
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", meshcontent.ErrContentPullFailed
	}
	dest := filepath.Join(s.root, "objects", id.Digest)
	if err := os.Rename(tmpName, dest); err != nil {
		_ = os.Remove(tmpName)
		return "", meshcontent.ErrContentPullFailed
	}
	return meshcontent.StatePresent, nil
}

// LinkExpansion records name → id. The object bytes stay where Pull
// put them. The same pair is a no-op success. A different id for the
// same name replaces that one link and leaves every other name in place.
func (s *Store) LinkExpansion(name string, id meshcontent.ContentID) error {
	if s == nil || !validLinkName(name) || id.Validate() != nil {
		return errors.New("mesh content link is invalid")
	}
	if s.Slot(id) != meshcontent.StatePresent {
		return errors.New("mesh content link: slot is not present")
	}
	if err := s.ensureDirs(); err != nil {
		return err
	}
	body := []byte(id.String() + "\n")
	path := filepath.Join(s.root, "links", name)
	existing, err := os.ReadFile(path)
	if err == nil && string(existing) == string(body) {
		return nil
	}
	return writeAtomic(filepath.Join(s.root, "links"), name, body)
}

func (s *Store) objectReady(id meshcontent.ContentID) bool {
	info, err := os.Stat(filepath.Join(s.root, "objects", id.Digest))
	return err == nil && info.Mode().IsRegular()
}

func validLinkName(name string) bool {
	if name == "" || len(name) > 64 || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '_', c == '-', c == '.':
		default:
			return false
		}
	}
	return true
}

func writeAtomic(dir, name string, body []byte) error {
	tmp, err := os.CreateTemp(dir, ".link-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, filepath.Join(dir, name)); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// usedBytes is committed objects plus in-flight partial files. Callers
// that decide whether another byte fits hold s.mu across this read and
// the write, so two pulls cannot both observe the same remainder.
func (s *Store) usedBytes() int64 {
	return dirBytes(filepath.Join(s.root, "objects")) + dirBytes(filepath.Join(s.root, "partial"))
}

func dirBytes(dir string) int64 {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var total int64
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		total += info.Size()
	}
	return total
}

// roomFor reports whether at least n more bytes fit under the quota and
// the free-space reserve. n is at least 1 for the pre-copy check.
func (s *Store) roomFor(n int64) bool {
	if n < 1 {
		n = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.roomLocked() >= n
}

// roomLocked is min(quota-used, free-reserve). Caller holds s.mu.
// used includes partial/, so an in-flight pull of another id counts.
func (s *Store) roomLocked() int64 {
	used := s.usedBytes()
	if used >= meshContentQuota {
		return 0
	}
	quotaRoom := meshContentQuota - used
	free, err := availableBytes(s.root)
	if err != nil || free < meshContentReserve {
		return 0
	}
	space := free - meshContentReserve
	if space > uint64(math.MaxInt64) {
		return quotaRoom
	}
	spaceRoom := int64(space)
	if spaceRoom < quotaRoom {
		return spaceRoom
	}
	return quotaRoom
}

func statAvailable(path string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bsize) * uint64(st.Bavail), nil
}

// spaceWriter caps each chunk at min(quota-used, free-reserve) and
// re-reads free space while the copy is running. A chunk that does not
// fit is not written past the cap. The pull then fails and drops the
// partial file.
type spaceWriter struct {
	store *Store
	w     io.Writer
}

func (q *spaceWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	q.store.mu.Lock()
	defer q.store.mu.Unlock()
	room := q.store.roomLocked()
	if room <= 0 {
		return 0, meshcontent.ErrContentPullFailed
	}
	capped := false
	if int64(len(p)) > room {
		p = p[:room]
		capped = true
	}
	n, err := q.w.Write(p)
	if capped && err == nil {
		err = meshcontent.ErrContentPullFailed
	}
	return n, err
}
