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
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/DeanoC/FogCast/internal/meshcontent"
)

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
}

// Open creates the object, partial, and link directories under root.
// nodeID is this executor. abis may be empty; an empty list is not
// eligibility. source may be nil.
func Open(root, nodeID string, source Source, abis []meshcontent.EligibleABI) (*Store, error) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return nil, errors.New("mesh content store requires a node id")
	}
	root = filepath.Clean(root)
	if root == "" || root == "." || root == string(filepath.Separator) {
		return nil, errors.New("mesh content store requires a root")
	}
	for _, sub := range []string{"objects", "partial", "links"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o700); err != nil {
			return nil, err
		}
	}
	return &Store{
		root:     root,
		nodeID:   nodeID,
		source:   source,
		abis:     append([]meshcontent.EligibleABI(nil), abis...),
		inflight: map[string]struct{}{},
	}, nil
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
	if _, err := io.Copy(io.MultiWriter(tmp, sum), &ctxReader{ctx: ctx, r: body}); err != nil {
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
