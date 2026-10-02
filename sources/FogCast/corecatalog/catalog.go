// Package corecatalog reads a local published core catalog, without compiling or executing cores.
package corecatalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/DeanoC/FogCast/corepackage"
)

type Entry struct {
	CoreID        string `json:"core_id"`
	Label         string `json:"label"`
	System        string `json:"system"`
	Standing      string `json:"standing"`
	PackageID     string `json:"package_id,omitempty"`
	ArchivePath   string `json:"archive_path,omitempty"`
	ArchiveSHA256 string `json:"archive_sha256,omitempty"`
	ArchiveSize   int64  `json:"archive_size,omitempty"`
}
type Catalog struct {
	Version  int     `json:"version"`
	SourceID string  `json:"source_id"`
	SHA256   string  `json:"catalog_sha256"`
	Entries  []Entry `json:"entries"`
	root     string
}

var digest = regexp.MustCompile(`^[0-9a-f]{64}$`)
var name = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,127}$`)
var core = regexp.MustCompile(`^fes\.[a-z0-9][a-z0-9.-]{0,63}$`)

func Load(path string) (Catalog, error) {
	var c Catalog
	f, err := os.Open(path)
	if err != nil {
		return c, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 1<<20+1))
	if err != nil || len(data) > 1<<20 {
		return c, errors.New("catalog exceeds size limit")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err = d.Decode(&c); err != nil {
		return Catalog{}, err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return Catalog{}, errors.New("invalid trailing catalog data")
	}
	if c.Version != 1 || !name.MatchString(c.SourceID) || !digest.MatchString(c.SHA256) || len(c.Entries) == 0 || len(c.Entries) > 256 {
		return Catalog{}, errors.New("invalid catalog metadata")
	}
	seen := map[string]bool{}
	for _, e := range c.Entries {
		if !core.MatchString(e.CoreID) || seen[e.CoreID] || !name.MatchString(e.System) || strings.TrimSpace(e.Label) == "" || len(e.Label) > 512 || strings.ContainsAny(e.Label, "\x00\n\r") || (e.Standing != "supported" && e.Standing != "demo" && e.Standing != "experimental") {
			return Catalog{}, errors.New("invalid catalog entry")
		}
		seen[e.CoreID] = true
		if e.PackageID == "" {
			if e.ArchivePath != "" || e.ArchiveSHA256 != "" || e.ArchiveSize != 0 {
				return Catalog{}, errors.New("incomplete artifact identity")
			}
			continue
		}
		if !digest.MatchString(e.PackageID) || !digest.MatchString(e.ArchiveSHA256) || e.ArchiveSize < 1 || e.ArchiveSize > corepackage.MaxArchiveSize || !validPath(e.ArchivePath) {
			return Catalog{}, errors.New("invalid artifact identity")
		}
	}
	var body map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&body); err != nil {
		return Catalog{}, err
	}
	delete(body, "catalog_sha256")
	var canonical bytes.Buffer
	if err := writeCanonical(&canonical, body); err != nil {
		return Catalog{}, err
	}
	sum := sha256.Sum256(canonical.Bytes())
	if hex.EncodeToString(sum[:]) != c.SHA256 {
		return Catalog{}, errors.New("catalog digest mismatch")
	}
	// colecovision is the slug already published catalogs use. New
	// publications emit coleco. The digest above is over the file bytes,
	// so the alias is applied only after that check.
	for i := range c.Entries {
		if c.Entries[i].System == "colecovision" {
			c.Entries[i].System = "coleco"
		}
	}
	c.root = filepath.Dir(path)
	return c, nil
}
func validPath(s string) bool {
	return s != "" && !strings.Contains(s, "\\") && !strings.HasPrefix(s, "/") && filepath.ToSlash(filepath.Clean(s)) == s && s != "." && !strings.HasPrefix(s, "../") && s != ".."
}
func (c Catalog) OpenPackage(coreID string) (io.ReadCloser, Entry, error) {
	for _, e := range c.Entries {
		if e.CoreID != coreID {
			continue
		}
		if e.PackageID == "" {
			return nil, e, errors.New("core has not been published")
		}
		root, err := os.OpenRoot(c.root)
		if err != nil {
			return nil, e, err
		}
		defer root.Close()
		f, err := root.Open(e.ArchivePath)
		if err != nil {
			return nil, e, err
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil || !st.Mode().IsRegular() || st.Size() != e.ArchiveSize {
			return nil, e, errors.New("invalid package file")
		}
		b, err := io.ReadAll(io.LimitReader(f, e.ArchiveSize+1))
		if err != nil {
			return nil, e, err
		}
		sum := sha256.Sum256(b)
		if int64(len(b)) != e.ArchiveSize || hex.EncodeToString(sum[:]) != e.ArchiveSHA256 {
			return nil, e, errors.New("package digest mismatch")
		}
		return io.NopCloser(bytes.NewReader(b)), e, nil
	}
	return nil, Entry{}, errors.New("core not found in catalog")
}

// Canonical JSON matches the publisher: sorted keys, compact UTF-8 strings,
// integer numbers, without HTML escaping.
func writeCanonical(out *bytes.Buffer, value any) error {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			writeCanonical(out, k)
			out.WriteByte(':')
			if err := writeCanonical(out, v[k]); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	case []any:
		out.WriteByte('[')
		for i, e := range v {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := writeCanonical(out, e); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case string:
		out.WriteByte('"')
		for _, r := range v {
			switch r {
			case '"', '\\':
				out.WriteByte('\\')
				out.WriteRune(r)
			case '\b':
				out.WriteString(`\b`)
			case '\f':
				out.WriteString(`\f`)
			case '\n':
				out.WriteString(`\n`)
			case '\r':
				out.WriteString(`\r`)
			case '\t':
				out.WriteString(`\t`)
			default:
				if r < 32 {
					fmt.Fprintf(out, `\u%04x`, r)
				} else {
					out.WriteRune(r)
				}
			}
		}
		out.WriteByte('"')
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return errors.New("catalog numbers must be integers")
		}
		fmt.Fprint(out, n)
	default:
		return errors.New("invalid catalog value")
	}
	return nil
}
