package kitlauncher

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/host/tenfoot"
)

const (
	// DefaultCacheRoot is the kit FAT tree for last-good catalog and covers.
	// Replacing the system image does not wipe it.
	DefaultCacheRoot = "/media/fat/fogcast/launcher-cache"

	// OfflineMessage is the kit footer when the host is absent. Launch still
	// needs the host; the shelf may still show the last-good local catalog.
	OfflineMessage = "Offline - local library"

	connectingMessage = "Connecting to FogCast"

	catalogFileName  = "catalog.json"
	coversDirName    = "covers"
	catalogFormat    = 1
	maxCatalogBytes  = 32 << 20
	maxCoverBytes    = 8 << 20
	artworkHandleLen = 64
)

// CatalogSnapshot is the last-good kit browse list written after a successful
// host catalog fetch.
type CatalogSnapshot struct {
	Games      []tenfoot.Game
	Strip      []tenfoot.Game
	StripLabel string
	Recents    []tenfoot.Game
}

type catalogFile struct {
	Format     int            `json:"format"`
	SavedUnix  int64          `json:"saved_unix,omitempty"`
	Games      []tenfoot.Game `json:"games"`
	Strip      []tenfoot.Game `json:"strip,omitempty"`
	StripLabel string         `json:"strip_label,omitempty"`
	Recents    []tenfoot.Game `json:"recents,omitempty"`
}

// DiskStore is the durable launcher-cache under FAT. Catalog replace is
// atomic; cover blobs are keyed by 64-hex artwork handle. It is not the ROM
// cache and never evicts `/media/fat/fogcast/cache`.
type DiskStore struct {
	root       string
	mu         sync.Mutex
	catalogKey string
}

// OpenDiskStore creates root/covers at 0700. root must be absolute.
func OpenDiskStore(root string) (*DiskStore, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if !filepath.IsAbs(root) {
		return nil, errors.New("launcher cache root must be absolute")
	}
	covers := filepath.Join(root, coversDirName)
	if err := os.MkdirAll(covers, 0700); err != nil {
		return nil, err
	}
	return &DiskStore{root: root}, nil
}

func cacheRoot(c Config) string {
	if strings.TrimSpace(c.path) == "" {
		return ""
	}
	dir, err := filepath.Abs(filepath.Dir(c.path))
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "launcher-cache")
}

// LoadCatalog returns the last-good snapshot. Missing or unreadable files are
// a miss, not a hang.
func (s *DiskStore) LoadCatalog() (CatalogSnapshot, bool) {
	if s == nil {
		return CatalogSnapshot{}, false
	}
	path := filepath.Join(s.root, catalogFileName)
	s.mu.Lock()
	defer s.mu.Unlock()
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() <= 0 || fi.Size() > maxCatalogBytes {
		return CatalogSnapshot{}, false
	}
	f, err := os.Open(path)
	if err != nil {
		return CatalogSnapshot{}, false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxCatalogBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxCatalogBytes {
		return CatalogSnapshot{}, false
	}
	var file catalogFile
	if json.Unmarshal(data, &file) != nil || file.Format != catalogFormat {
		return CatalogSnapshot{}, false
	}
	if file.Games == nil {
		file.Games = []tenfoot.Game{}
	}
	snap := CatalogSnapshot{
		Games:      file.Games,
		Strip:      file.Strip,
		StripLabel: file.StripLabel,
		Recents:    file.Recents,
	}
	if len(snap.Games) == 0 && len(snap.Strip) == 0 {
		return CatalogSnapshot{}, false
	}
	s.catalogKey = snapshotKey(snap)
	return snap, true
}

// SaveCatalog atomically replaces catalog.json. Unchanged payloads are not
// rewritten.
func (s *DiskStore) SaveCatalog(snap CatalogSnapshot) error {
	if s == nil {
		return errors.New("launcher cache unavailable")
	}
	if snap.Games == nil {
		snap.Games = []tenfoot.Game{}
	}
	key := snapshotKey(snap)
	file := catalogFile{
		Format:     catalogFormat,
		SavedUnix:  time.Now().Unix(),
		Games:      snap.Games,
		Strip:      snap.Strip,
		StripLabel: snap.StripLabel,
		Recents:    snap.Recents,
	}
	data, err := json.Marshal(file)
	if err != nil {
		return err
	}
	if len(data) > maxCatalogBytes {
		return errors.New("launcher catalog is too large")
	}
	path := filepath.Join(s.root, catalogFileName)
	s.mu.Lock()
	defer s.mu.Unlock()
	if key != "" && key == s.catalogKey {
		return nil
	}
	if err := writeAtomic(path, data); err != nil {
		return err
	}
	s.catalogKey = key
	return nil
}

// LoadArtwork returns the raw cover bytes for a 64-hex handle.
func (s *DiskStore) LoadArtwork(handle string) ([]byte, bool) {
	if s == nil {
		return nil, false
	}
	path, err := s.coverPath(handle)
	if err != nil {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() <= 0 || fi.Size() > maxCoverBytes {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 || len(data) > maxCoverBytes {
		return nil, false
	}
	return data, true
}

// SaveArtwork writes cover bytes under covers/<handle>. Request only calls
// this after a network fetch, so boot disk hits do not rewrite FAT.
func (s *DiskStore) SaveArtwork(handle string, data []byte) error {
	if s == nil {
		return errors.New("launcher cache unavailable")
	}
	if len(data) == 0 || len(data) > maxCoverBytes {
		return errors.New("artwork is invalid")
	}
	path, err := s.coverPath(handle)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if prev, err := os.ReadFile(path); err == nil && bytes.Equal(prev, data) {
		return nil
	}
	return writeAtomic(path, data)
}

func (s *DiskStore) coverPath(handle string) (string, error) {
	handle = normalizeArtworkHandle(handle)
	if handle == "" {
		return "", errors.New("artwork handle is invalid")
	}
	dir := filepath.Join(s.root, coversDirName)
	path := filepath.Join(dir, handle)
	if filepath.Dir(path) != dir {
		return "", errors.New("artwork handle is invalid")
	}
	return path, nil
}

func snapshotKey(snap CatalogSnapshot) string {
	type key struct {
		Games      []tenfoot.Game `json:"games"`
		Strip      []tenfoot.Game `json:"strip"`
		StripLabel string         `json:"strip_label"`
		Recents    []tenfoot.Game `json:"recents"`
	}
	data, err := json.Marshal(key{
		Games:      snap.Games,
		Strip:      snap.Strip,
		StripLabel: snap.StripLabel,
		Recents:    snap.Recents,
	})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func normalizeArtworkHandle(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if len(value) != artworkHandleLen {
		return ""
	}
	for _, r := range value {
		if r < '0' || r > '9' && (r < 'a' || r > 'f') {
			return ""
		}
	}
	return value
}

func applyLocalSnapshot(m *Model, c *Client) bool {
	if m == nil || c == nil || c.Cache == nil {
		return false
	}
	snap, ok := c.Cache.LoadCatalog()
	if !ok {
		return false
	}
	m.SetCatalog(snap.Games)
	m.SetStrip(snap.Strip, snap.StripLabel)
	m.Recents = append([]tenfoot.Game(nil), snap.Recents...)
	return true
}

func persistSnapshot(c *Client, snap CatalogSnapshot) {
	if c == nil || c.Cache == nil {
		return
	}
	_ = c.Cache.SaveCatalog(snap)
}

func isTransientStatus(message string) bool {
	switch message {
	case connectingMessage, OfflineMessage, "Kit in use", "Kit unavailable", "Kit not ready":
		return true
	default:
		return false
	}
}
