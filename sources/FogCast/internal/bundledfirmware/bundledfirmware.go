// Package bundledfirmware reads the firmware and disks that ship beside FES
// as separate, replaceable files. Each object is a directory holding
// manifest.json, the image and, when its licence requires it, the licence
// text. The bytes are never part of a sealed core package;
// scripts/bundled_firmware.py stages them, by pinned URL and SHA-256, when a
// bundle is built.
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
const Format = 2

// Kinds of bundled object.
const (
	// KindFirmware is a core's named ROM, selected by default for its titles.
	KindFirmware = "firmware"
	// KindDisk is a disk image for a core: the PC/XT's FreeDOS boot disk.
	KindDisk = "disk"
)

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

// Manifest describes one bundled image: firmware for one core's named ROM,
// or a disk for one core.
type Manifest struct {
	Format  int    `json:"format"`
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	CoreID  string `json:"core_id"`
	// ROMID names a firmware object's ROM. A disk has none.
	ROMID  string `json:"rom_id,omitempty"`
	File   string `json:"file"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	// Licence is an SPDX identifier, or LicenseRef-* for a permission.
	Licence     string `json:"licence"`
	LicenceName string `json:"licence_name"`
	// LicenceFile is the licence text beside the image, when it ships one.
	LicenceFile string `json:"licence_file,omitempty"`
	// Notice is shown with the firmware: copyright lines and any wording
	// the rights holder asks for, verbatim.
	Notice string `json:"notice"`
	// Origins are the archives the image's bytes were taken from.
	Origins []Location `json:"origins"`
	// Sources are the corresponding source code of a free software image.
	Sources []Location `json:"sources,omitempty"`
}

// Firmware is a validated object of either kind: its manifest, image and
// licence text.
type Firmware struct {
	Manifest
	Image       []byte
	LicenceText string
}

// Load reads every firmware directory in dir. A directory that fails
// validation is skipped and reported; the others still load. A missing dir
// holds no objects. Two firmware objects for the same core and ROM, or two
// disks for the same core, are refused: the first by name wins.
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
	seen := map[[3]string]string{}
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
		key := [3]string{fw.Kind, fw.CoreID, fw.ROMID}
		if first, ok := seen[key]; ok {
			what := fw.CoreID + " " + fw.ROMID
			if fw.Kind == KindDisk {
				what = "a disk for " + fw.CoreID
			}
			problems = append(problems, fmt.Errorf("bundled firmware %s: %s is already bundled by %s", name, what, first))
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
	fields := []struct{ name, value string }{
		{"name", m.Name}, {"version", m.Version}, {"core_id", m.CoreID},
		{"licence", m.Licence}, {"licence_name", m.LicenceName}, {"notice", m.Notice},
	}
	if m.Kind == KindFirmware {
		fields = append(fields, struct{ name, value string }{"rom_id", m.ROMID})
	}
	for _, field := range fields {
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
	case m.Kind != KindFirmware && m.Kind != KindDisk:
		return fmt.Errorf("manifest kind %q is not firmware or disk", m.Kind)
	case m.Kind == KindDisk && m.ROMID != "":
		return errors.New("a disk names no rom_id")
	case len(m.Origins) == 0:
		return errors.New("manifest is missing origins")
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
	for _, origin := range m.Origins {
		if err := origin.validate("origin"); err != nil {
			return err
		}
	}
	for _, source := range m.Sources {
		if err := source.validate("source"); err != nil {
			return err
		}
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
