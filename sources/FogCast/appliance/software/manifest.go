// Package software defines immutable kit userspace releases. Content identity
// covers the complete manifest, including every payload digest and contract.
package software

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
)

const Format = 1
const MaxManifestSize = 1 << 20
const MaxBundleSize int64 = 512 << 20
const MaxFiles = 4096
const DefaultRoot = "/run/fogcast/fesdata3/software-releases"

var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var namePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._+-]{0,127}$`)
var RequiredExecutables = []string{"bin/fogcast-kit", "bin/fogcast-tenfoot", "bin/mister-agent", "bin/mister-runtime"}

type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Mode   uint32 `json:"mode"`
}
type Manifest struct {
	Format       int               `json:"format"`
	Board        string            `json:"board"`
	Architecture string            `json:"architecture"`
	BaseContract string            `json:"base_contract"`
	Version      string            `json:"version"`
	Interfaces   map[string]string `json:"interfaces"`
	Provenance   map[string]string `json:"provenance"`
	Files        []File            `json:"files"`
}
type Compatibility struct {
	Board        string            `json:"board"`
	Architecture string            `json:"architecture"`
	BaseContract string            `json:"base_contract"`
	Interfaces   map[string]string `json:"interfaces"`
}
type Release struct {
	ID       string   `json:"id"`
	Manifest Manifest `json:"manifest"`
}

func ValidID(id string) bool { return hashPattern.MatchString(id) }
func validPath(p string) bool {
	return len(p) <= 240 && fs.ValidPath(p) && p != "." && path.Clean(p) == p && !strings.ContainsAny(p, "\\\x00\r\n") && (strings.HasPrefix(p, "bin/") || strings.HasPrefix(p, "share/"))
}
func (m Manifest) Validate() error {
	if m.Format != Format || !namePattern.MatchString(m.Board) || !namePattern.MatchString(m.Architecture) || !namePattern.MatchString(m.BaseContract) || !namePattern.MatchString(m.Version) {
		return errors.New("invalid software release identity or format")
	}
	if len(m.Files) < len(RequiredExecutables) || len(m.Files) > MaxFiles || len(m.Interfaces) == 0 || len(m.Interfaces) > 64 || len(m.Provenance) > 64 {
		return errors.New("invalid release file or interface count")
	}
	for k, v := range m.Interfaces {
		if !namePattern.MatchString(k) || !namePattern.MatchString(v) {
			return errors.New("invalid interface contract")
		}
	}
	for k, v := range m.Provenance {
		if !namePattern.MatchString(k) || len(v) == 0 || len(v) > 256 || strings.ContainsAny(v, "\x00\r\n") {
			return errors.New("invalid provenance")
		}
	}
	seen := map[string]File{}
	var size int64
	for _, f := range m.Files {
		if !validPath(f.Path) || !ValidID(f.SHA256) || f.Size < 0 || f.Size > MaxBundleSize || (f.Mode != 0444 && f.Mode != 0555) {
			return fmt.Errorf("invalid file %q", f.Path)
		}
		if _, ok := seen[f.Path]; ok {
			return fmt.Errorf("duplicate file %q", f.Path)
		}
		seen[f.Path] = f
		size += f.Size
		if size > MaxBundleSize-MaxManifestSize-int64(MaxFiles)*1024 {
			return errors.New("release too large")
		}
	}
	for p := range seen {
		for d := path.Dir(p); d != "."; d = path.Dir(d) {
			if _, ok := seen[d]; ok {
				return errors.New("file/directory collision")
			}
		}
	}
	for _, p := range RequiredExecutables {
		if f, ok := seen[p]; !ok || f.Mode != 0555 || f.Size == 0 {
			return fmt.Errorf("missing executable %s", p)
		}
	}
	return nil
}
func (m Manifest) Canonical() ([]byte, error) {
	if e := m.Validate(); e != nil {
		return nil, e
	}
	m.Files = append([]File(nil), m.Files...)
	sort.Slice(m.Files, func(i, j int) bool { return m.Files[i].Path < m.Files[j].Path })
	b, e := json.Marshal(m)
	if e != nil {
		return nil, e
	}
	// Validate allows the maximum file count and path length. Those records can
	// still encode past the size every reader accepts, so packing stops here.
	if len(b) > MaxManifestSize {
		return nil, errors.New("manifest too large")
	}
	return b, nil
}
func (m Manifest) ID() (string, error) {
	b, e := m.Canonical()
	if e != nil {
		return "", e
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
func (m Manifest) Compatible(c Compatibility) error {
	if e := m.Validate(); e != nil {
		return e
	}
	if m.Board != c.Board || m.Architecture != c.Architecture || m.BaseContract != c.BaseContract {
		return errors.New("incompatible software base contract")
	}
	for k, v := range c.Interfaces {
		if m.Interfaces[k] != v {
			return fmt.Errorf("incompatible interface %s", k)
		}
	}
	return nil
}
func DecodeManifest(r io.Reader) (Manifest, error) {
	var m Manifest
	b, e := io.ReadAll(io.LimitReader(r, MaxManifestSize+1))
	if e != nil {
		return m, e
	}
	if len(b) > MaxManifestSize {
		return m, errors.New("manifest too large")
	}
	// Reject duplicate names at every nesting depth; encoding/json alone accepts them.
	d := json.NewDecoder(bytes.NewReader(b))
	if e = uniqueJSON(d); e != nil {
		return m, e
	}
	if _, e = d.Token(); e != io.EOF {
		return m, errors.New("trailing manifest data")
	}
	d = json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e = d.Decode(&m); e != nil {
		return m, e
	}
	if e = m.Validate(); e != nil {
		return m, e
	}
	// Identity is the digest of the canonical bytes. Whitespace, reordered
	// fields and reordered file records must not decode as that release.
	return m, sameCanonical(b, m)
}
func sameCanonical(b []byte, m Manifest) error {
	c, e := m.Canonical()
	if e != nil {
		return e
	}
	if !bytes.Equal(b, c) {
		return errors.New("manifest is not canonical")
	}
	return nil
}
func uniqueJSON(d *json.Decoder) error {
	t, e := d.Token()
	if e != nil {
		return e
	}
	switch t {
	case json.Delim('{'):
		seen := map[string]bool{}
		for d.More() {
			k, e := d.Token()
			if e != nil {
				return e
			}
			s, ok := k.(string)
			if !ok || seen[s] {
				return errors.New("duplicate JSON field")
			}
			seen[s] = true
			if e = uniqueJSON(d); e != nil {
				return e
			}
		}
		_, e = d.Token()
		return e
	case json.Delim('['):
		for d.More() {
			if e = uniqueJSON(d); e != nil {
				return e
			}
		}
		_, e = d.Token()
		return e
	}
	return nil
}
