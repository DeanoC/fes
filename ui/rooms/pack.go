package rooms

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const (
	// ManifestName is the file every room pack directory must contain.
	ManifestName = "room.toml"

	maxManifestBytes = 64 << 10
	maxSourceBytes   = 2 << 20
	maxAssetBytes    = 16 << 20
)

var roomIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// Manifest is the decoded room.toml.
type Manifest struct {
	ID          string        `toml:"id"`
	Title       string        `toml:"title"`
	Author      string        `toml:"author"`
	Description string        `toml:"description"`
	Version     int           `toml:"version"`
	Main        string        `toml:"main"`
	Icon        string        `toml:"icon"`
	Theme       ManifestTheme `toml:"theme"`
}

// ManifestTheme carries optional colour overrides exposed to the script as
// room.theme.background / room.theme.accent.
type ManifestTheme struct {
	Background string `toml:"background"`
	Accent     string `toml:"accent"`
}

// Pack is one discovered room directory. FS is rooted at the pack directory
// so scripts can only read their own files. Err is set for packs that were
// found but cannot run; they are still listed so creators see the failure.
type Pack struct {
	Manifest
	Source string
	FS     fs.FS
	Err    error
}

// Valid reports whether the pack loaded without error.
func (p Pack) Valid() bool { return p.Err == nil }

// DecodeManifest parses room.toml bytes with unknown fields rejected.
func DecodeManifest(data []byte) (Manifest, error) {
	if len(data) > maxManifestBytes {
		return Manifest{}, errors.New("room.toml too large")
	}
	var m Manifest
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("invalid room.toml: %w", err)
	}
	m.ID = strings.TrimSpace(m.ID)
	if !roomIDPattern.MatchString(m.ID) {
		return Manifest{}, fmt.Errorf("room id %q must be lowercase letters, digits, '.', '_' or '-'", m.ID)
	}
	if strings.TrimSpace(m.Title) == "" {
		m.Title = m.ID
	}
	if strings.TrimSpace(m.Main) == "" {
		m.Main = "main.lua"
	}
	if _, err := cleanPackPath(m.Main); err != nil {
		return Manifest{}, fmt.Errorf("main: %w", err)
	}
	if m.Icon != "" {
		if _, err := cleanPackPath(m.Icon); err != nil {
			return Manifest{}, fmt.Errorf("icon: %w", err)
		}
	}
	if m.Theme.Background != "" {
		if _, err := ParseHexColor(m.Theme.Background); err != nil {
			return Manifest{}, fmt.Errorf("theme.background: %w", err)
		}
	}
	if m.Theme.Accent != "" {
		if _, err := ParseHexColor(m.Theme.Accent); err != nil {
			return Manifest{}, fmt.Errorf("theme.accent: %w", err)
		}
	}
	return m, nil
}

// LoadPackFS reads a pack from an fs.FS rooted at the pack directory.
func LoadPackFS(fsys fs.FS, source string) Pack {
	p := Pack{Source: source, FS: fsys}
	data, err := fs.ReadFile(fsys, ManifestName)
	if err != nil {
		p.Err = fmt.Errorf("%s: %w", ManifestName, err)
		p.ID = fallbackID(source)
		p.Title = p.ID
		return p
	}
	m, err := DecodeManifest(data)
	if err != nil {
		p.Err = err
		p.ID = fallbackID(source)
		p.Title = p.ID
		return p
	}
	p.Manifest = m
	if _, err := fs.Stat(fsys, m.Main); err != nil {
		p.Err = fmt.Errorf("main script %s: %w", m.Main, err)
	}
	return p
}

// LoadDir discovers packs in every immediate subdirectory of root. A missing
// root is not an error: it simply has no packs.
func LoadDir(root string) ([]Pack, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("rooms: read %s: %w", root, err)
	}
	var packs []Pack
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		if _, err := os.Stat(filepath.Join(dir, ManifestName)); err != nil {
			continue
		}
		packs = append(packs, LoadPackFS(os.DirFS(dir), dir))
	}
	sortPacks(packs)
	return packs, nil
}

// LoadEmbedded discovers packs in every immediate subdirectory of an
// embedded tree. ids are prefixed so they never collide with user packs.
func LoadEmbedded(fsys fs.FS, prefix string) []Pack {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil
	}
	var packs []Pack
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sub, err := fs.Sub(fsys, entry.Name())
		if err != nil {
			continue
		}
		p := LoadPackFS(sub, "embedded:"+entry.Name())
		if p.Err == nil && prefix != "" && !strings.HasPrefix(p.ID, prefix) {
			p.ID = prefix + p.ID
		}
		packs = append(packs, p)
	}
	sortPacks(packs)
	return packs
}

func sortPacks(packs []Pack) {
	sort.SliceStable(packs, func(i, j int) bool {
		return strings.ToLower(packs[i].Title) < strings.ToLower(packs[j].Title)
	})
}

func fallbackID(source string) string {
	base := strings.ToLower(filepath.Base(strings.TrimPrefix(source, "embedded:")))
	if roomIDPattern.MatchString(base) {
		return base
	}
	return "invalid-pack"
}

// Index is the set of rooms a launcher can open.
type Index struct {
	Packs []Pack
}

// NewIndex merges packs; a later pack with the same id replaces an earlier one.
func NewIndex(groups ...[]Pack) *Index {
	idx := &Index{}
	seen := map[string]int{}
	for _, group := range groups {
		for _, p := range group {
			if i, ok := seen[p.ID]; ok {
				idx.Packs[i] = p
				continue
			}
			seen[p.ID] = len(idx.Packs)
			idx.Packs = append(idx.Packs, p)
		}
	}
	return idx
}

// Find returns the pack with id.
func (idx *Index) Find(id string) (Pack, bool) {
	if idx == nil {
		return Pack{}, false
	}
	for _, p := range idx.Packs {
		if p.ID == id {
			return p, true
		}
	}
	return Pack{}, false
}

// ValidCount is the number of runnable packs.
func (idx *Index) ValidCount() int {
	if idx == nil {
		return 0
	}
	n := 0
	for _, p := range idx.Packs {
		if p.Valid() {
			n++
		}
	}
	return n
}

// cleanPackPath validates a script-supplied relative path and returns it in
// fs.FS form. Absolute paths and any ".." segment are rejected.
func cleanPackPath(rel string) (string, error) {
	rel = strings.TrimSpace(strings.ReplaceAll(rel, "\\", "/"))
	if rel == "" {
		return "", errors.New("empty path")
	}
	if strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("path %q must be relative to the room", rel)
	}
	clean := path.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || !fs.ValidPath(clean) {
		return "", fmt.Errorf("path %q escapes the room", rel)
	}
	return clean, nil
}

func readPackFile(fsys fs.FS, rel string, limit int) ([]byte, error) {
	clean, err := cleanPackPath(rel)
	if err != nil {
		return nil, err
	}
	f, err := fsys.Open(clean)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, 0, 4096)
	chunk := make([]byte, 32<<10)
	for {
		n, err := f.Read(chunk)
		buf = append(buf, chunk[:n]...)
		if len(buf) > limit {
			return nil, fmt.Errorf("%s exceeds %d bytes", clean, limit)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return buf, nil
			}
			return nil, err
		}
	}
}
