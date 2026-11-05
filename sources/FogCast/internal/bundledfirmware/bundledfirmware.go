// Package bundledfirmware reads the firmware that ships beside FES as
// separate, replaceable files. Each object is a directory holding
// manifest.json, the unmodified firmware image and, when its licence
// requires it, the licence text. The bytes are never part of a sealed core
// package or of this source tree; scripts/bundled_firmware.py fetches them
// by pinned URL and SHA-256 when a bundle is built.
package bundledfirmware

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Format is the only manifest format this host reads.
const Format = 1

// ManifestName is the manifest file in each firmware directory.
const ManifestName = "manifest.json"

const (
	maxManifestBytes = 64 << 10
	maxImageBytes    = 1 << 20
	maxLicenceBytes  = 64 << 10
)

var (
	idRE     = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,63}$`)
	fileRE   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	digestRE = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Location is a pinned download: where the bytes or their source came from.
type Location struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

// Manifest describes one bundled firmware image for one core's named ROM.
type Manifest struct {
	Format  int    `json:"format"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	CoreID  string `json:"core_id"`
	ROMID   string `json:"rom_id"`
	File    string `json:"file"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
	// Licence is an SPDX identifier, or LicenseRef-* for a permission.
	Licence     string `json:"licence"`
	LicenceName string `json:"licence_name"`
	// LicenceFile is the licence text beside the image, when it ships one.
	LicenceFile string `json:"licence_file,omitempty"`
	// Notice is shown with the firmware: copyright lines and any wording
	// the rights holder asks for, verbatim.
	Notice string `json:"notice"`
	// Origin is the archive the image was taken from.
	Origin Location `json:"origin"`
	// Source is the corresponding source code, for a free software image.
	Source *Location `json:"source,omitempty"`
}

// Firmware is a validated object: its manifest, image and licence text.
type Firmware struct {
	Manifest
	Image       []byte
	LicenceText string
}

// Load reads every firmware directory in dir. A directory that fails
// validation is skipped and reported; the others still load. A missing dir
// holds no firmware. Two objects for the same core and ROM are refused: the
// first by name wins.
func Load(dir string) ([]Firmware, []error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, []error{fmt.Errorf("read bundled firmware: %w", err)}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var out []Firmware
	var problems []error
	seen := map[[2]string]string{}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || !entry.IsDir() {
			continue
		}
		fw, err := loadOne(filepath.Join(dir, name), name)
		if err != nil {
			problems = append(problems, fmt.Errorf("bundled firmware %s: %w", name, err))
			continue
		}
		key := [2]string{fw.CoreID, fw.ROMID}
		if first, ok := seen[key]; ok {
			problems = append(problems, fmt.Errorf("bundled firmware %s: %s %s is already bundled by %s", name, fw.CoreID, fw.ROMID, first))
			continue
		}
		seen[key] = name
		out = append(out, fw)
	}
	return out, problems
}

func loadOne(dir, name string) (Firmware, error) {
	raw, err := readRegular(filepath.Join(dir, ManifestName), maxManifestBytes)
	if err != nil {
		return Firmware{}, err
	}
	var m Manifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&m); err != nil {
		return Firmware{}, fmt.Errorf("decode manifest: %w", err)
	}
	if decoder.More() {
		return Firmware{}, errors.New("manifest has trailing data")
	}
	if m.ID != name {
		return Firmware{}, fmt.Errorf("manifest id %q differs from its directory", m.ID)
	}
	if err := m.Validate(); err != nil {
		return Firmware{}, err
	}
	image, err := readRegular(filepath.Join(dir, m.File), maxImageBytes)
	if err != nil {
		return Firmware{}, err
	}
	if int64(len(image)) != m.Size {
		return Firmware{}, fmt.Errorf("%s is %d bytes, manifest says %d", m.File, len(image), m.Size)
	}
	if fmt.Sprintf("%x", sha256.Sum256(image)) != m.SHA256 {
		return Firmware{}, fmt.Errorf("%s failed its SHA-256 check", m.File)
	}
	fw := Firmware{Manifest: m, Image: image}
	if m.LicenceFile != "" {
		text, err := readRegular(filepath.Join(dir, m.LicenceFile), maxLicenceBytes)
		if err != nil {
			return Firmware{}, err
		}
		fw.LicenceText = string(text)
	}
	return fw, nil
}

// Validate checks every field a host needs to import, select and credit the
// image. It does not read the image.
func (m Manifest) Validate() error {
	var missing []string
	for _, field := range []struct{ name, value string }{
		{"name", m.Name}, {"version", m.Version}, {"core_id", m.CoreID}, {"rom_id", m.ROMID},
		{"licence", m.Licence}, {"licence_name", m.LicenceName}, {"notice", m.Notice},
	} {
		if strings.TrimSpace(field.value) == "" {
			missing = append(missing, field.name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("manifest is missing %s", strings.Join(missing, ", "))
	}
	switch {
	case m.Format != Format:
		return fmt.Errorf("manifest format %d is not %d", m.Format, Format)
	case !idRE.MatchString(m.ID):
		return fmt.Errorf("manifest id %q is invalid", m.ID)
	case !fileRE.MatchString(m.File) || m.File == ManifestName:
		return fmt.Errorf("image file %q is invalid", m.File)
	case m.LicenceFile != "" && (!fileRE.MatchString(m.LicenceFile) || m.LicenceFile == ManifestName || m.LicenceFile == m.File):
		return fmt.Errorf("licence file %q is invalid", m.LicenceFile)
	case m.Size < 1 || m.Size > maxImageBytes:
		return fmt.Errorf("image size %d is out of range", m.Size)
	case !digestRE.MatchString(m.SHA256):
		return errors.New("image sha256 is invalid")
	}
	if err := m.Origin.validate("origin"); err != nil {
		return err
	}
	if m.Source != nil {
		return m.Source.validate("source")
	}
	return nil
}

func (l Location) validate(name string) error {
	if !strings.HasPrefix(l.URL, "https://") || strings.ContainsAny(l.URL, " \r\n") {
		return fmt.Errorf("%s url must be https", name)
	}
	if !digestRE.MatchString(l.SHA256) {
		return fmt.Errorf("%s sha256 is invalid", name)
	}
	return nil
}

func readRegular(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", filepath.Base(path))
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("%s is larger than %d bytes", filepath.Base(path), limit)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is larger than %d bytes", filepath.Base(path), limit)
	}
	return data, nil
}
