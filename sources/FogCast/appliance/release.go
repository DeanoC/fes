// Package appliance defines the closed, raw-image appliance release format.
package appliance

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

const (
	Format                    = 1
	ManifestFormatMajor       = 1
	ManifestFormatMinor       = 0
	Board                     = "de10-nano"
	BoardDE25                 = "de25-nano"
	BootABI                   = "fes-bootstrap-v1"
	MaxImageSize        int64 = (4 << 30) - 1
	MaxManifestSize           = 8192
)

type Manifest struct {
	Format          int    `json:"format"`
	Board           string `json:"board"`
	BootABI         string `json:"boot_abi"`
	Version         string `json:"version"`
	KernelSHA256    string `json:"kernel_sha256"`
	ImageSHA256     string `json:"image_sha256"`
	ImageSize       int64  `json:"image_size"`
	FESRevision     string `json:"fes_revision"`
	FogCastRevision string `json:"fogcast_revision"`
	RuntimeRevision string `json:"runtime_revision"`
	// ManifestFormat is optional "major.minor". Empty means 1.0. Producers
	// omit it so a deployed agent that does not know the name can still stage.
	ManifestFormat string `json:"manifest_format,omitempty"`
	ImagePasses    int64  `json:"image_passes,omitempty"`
}

var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

func ValidHash(s string) bool { return hashPattern.MatchString(s) }
func (m Manifest) Validate() error {
	return m.ValidateForBoard(Board)
}

// ValidateForBoard requires the caller's selected board, independently of the
// manifest. The default Validate entry point remains DE10-only.
func (m Manifest) ValidateForBoard(board string) error {
	if (board != Board && board != BoardDE25) || m.Format != Format || m.Board != board || m.BootABI != BootABI {
		return errors.New("unsupported release format, board, or boot ABI")
	}
	if m.ManifestFormat != "" {
		major, _, err := parseManifestFormat(m.ManifestFormat)
		if err != nil || major != ManifestFormatMajor {
			return errors.New("unsupported release format, board, or boot ABI")
		}
	}
	if len(m.Version) == 0 || len(m.Version) > 128 || strings.TrimSpace(m.Version) != m.Version || strings.IndexFunc(m.Version, unicode.IsControl) >= 0 {
		return errors.New("invalid release version")
	}
	if !ValidHash(m.KernelSHA256) || !ValidHash(m.ImageSHA256) {
		return errors.New("invalid release SHA-256")
	}
	if m.ImageSize < 2048 || m.ImageSize > MaxImageSize {
		return errors.New("image size outside supported bounds")
	}
	for _, v := range []string{m.FESRevision, m.FogCastRevision, m.RuntimeRevision} {
		if !revisionPattern.MatchString(v) {
			return errors.New("release revisions must be full lowercase Git SHA-1 IDs")
		}
	}
	if m.ImagePasses < 0 {
		return errors.New("image_passes must be a positive integer")
	}
	return nil
}
func (m Manifest) Compatible(kernelSHA string) error {
	return m.CompatibleForBoard(kernelSHA, Board)
}

// CompatibleForBoard binds both the selected board and the stable kernel.
func (m Manifest) CompatibleForBoard(kernelSHA, board string) error {
	if e := m.ValidateForBoard(board); e != nil {
		return e
	}
	if m.KernelSHA256 != kernelSHA {
		return errors.New("release kernel differs from stable bootstrap")
	}
	return nil
}

// DecodeManifest reads one release manifest. manifest_format is optional
// "major.minor"; a missing value means 1.0. Field names match exactly. The
// same major with a higher minor may carry unknown optional fields, including
// a case variant of a recognized name. Those names are returned so the update
// path can log them, and they are not kept on Manifest. An unknown major, a
// duplicate, or a missing or malformed required field is rejected.
// image_passes stays optional. When present it must be a positive integer and
// remains on Manifest so remarshaling keeps those bytes. Producers omit both
// optional names so a deployed agent can still stage release.json.
func DecodeManifest(r io.Reader) (Manifest, []string, error) {
	return DecodeManifestForBoard(r, Board)
}

// DecodeManifestForBoard retains the exact field/version rules, accepting only
// the explicitly selected supported board. It never selects from upload data.
func DecodeManifestForBoard(r io.Reader, board string) (Manifest, []string, error) {
	var m Manifest
	if board != Board && board != BoardDE25 {
		return m, nil, errors.New("unsupported selected release board")
	}
	b, e := io.ReadAll(io.LimitReader(r, MaxManifestSize+1))
	if e != nil {
		return m, nil, e
	}
	if len(b) > MaxManifestSize {
		return m, nil, errors.New("manifest too large")
	}
	names := []string{"format", "board", "boot_abi", "version", "kernel_sha256", "image_sha256", "image_size", "fes_revision", "fogcast_revision", "runtime_revision", "image_passes", "manifest_format"}
	optional := map[string]bool{"image_passes": true, "manifest_format": true}
	fields := make(map[string]bool, len(names))
	for _, n := range names {
		fields[n] = false
	}
	var ignored []string
	var formatRaw json.RawMessage
	recognized := make(map[string]json.RawMessage, len(names))
	seenName := map[string]bool{}
	d := json.NewDecoder(bytes.NewReader(b))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return m, nil, errors.New("manifest must be a JSON object")
	}
	for d.More() {
		t, e = d.Token()
		if e != nil {
			return m, nil, e
		}
		n, ok := t.(string)
		if !ok {
			return m, nil, errors.New("invalid field name")
		}
		if seenName[n] {
			return m, nil, fmt.Errorf("unknown or duplicate manifest field %q", n)
		}
		seenName[n] = true
		_, known := fields[n]
		var raw json.RawMessage
		if e = d.Decode(&raw); e != nil {
			return m, nil, e
		}
		if known && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return m, nil, fmt.Errorf("null manifest field %q", n)
		}
		if !known {
			ignored = append(ignored, n)
			continue
		}
		fields[n] = true
		recognized[n] = append(json.RawMessage(nil), raw...)
		if n == "image_passes" {
			if e = validateImagePasses(raw); e != nil {
				return m, nil, e
			}
		}
		if n == "manifest_format" {
			formatRaw = append(json.RawMessage(nil), raw...)
		}
	}
	if _, e = d.Token(); e != nil {
		return m, nil, e
	}
	if _, e = d.Token(); e != io.EOF {
		return m, nil, errors.New("trailing manifest data")
	}
	major, minor := ManifestFormatMajor, ManifestFormatMinor
	if formatRaw != nil {
		text, err := manifestFormatText(formatRaw)
		if err != nil {
			return m, nil, err
		}
		major, minor, err = parseManifestFormat(text)
		if err != nil {
			return m, nil, err
		}
		if major != ManifestFormatMajor {
			return m, nil, fmt.Errorf("unknown manifest format major %d", major)
		}
	}
	if minor <= ManifestFormatMinor && len(ignored) > 0 {
		return m, nil, fmt.Errorf("unknown or duplicate manifest field %q", ignored[0])
	}
	for n, seen := range fields {
		if !seen && !optional[n] {
			return m, nil, fmt.Errorf("missing manifest field %q", n)
		}
	}
	// encoding/json matches struct fields case-insensitively and keeps the last
	// match. Unmarshal only the exact recognized values so a trailing case
	// variant cannot replace a field that already passed validation.
	filtered, e := json.Marshal(recognized)
	if e != nil {
		return m, nil, e
	}
	if e = json.Unmarshal(filtered, &m); e != nil {
		return m, nil, e
	}
	if minor <= ManifestFormatMinor {
		ignored = nil
	}
	return m, ignored, m.ValidateForBoard(board)
}

func manifestFormatText(raw json.RawMessage) (string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) < 2 || raw[0] != '"' {
		return "", errors.New("manifest_format must be major.minor")
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return "", errors.New("manifest_format must be major.minor")
	}
	return text, nil
}

func parseManifestFormat(text string) (int, int, error) {
	majorText, minorText, ok := strings.Cut(text, ".")
	if !ok || strings.Contains(minorText, ".") || !plainDecimal(majorText) || !plainDecimal(minorText) {
		return 0, 0, errors.New("manifest_format must be major.minor")
	}
	major, err1 := strconv.ParseInt(majorText, 10, 32)
	minor, err2 := strconv.ParseInt(minorText, 10, 32)
	if err1 != nil || err2 != nil {
		return 0, 0, errors.New("manifest_format must be major.minor")
	}
	return int(major), int(minor), nil
}

func plainDecimal(s string) bool {
	if s == "" || (len(s) > 1 && s[0] == '0') {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func validateImagePasses(raw json.RawMessage) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] < '1' || raw[0] > '9' {
		return errors.New("image_passes must be a positive integer")
	}
	for _, c := range raw[1:] {
		if c < '0' || c > '9' {
			return errors.New("image_passes must be a positive integer")
		}
	}
	n, e := strconv.ParseInt(string(raw), 10, 64)
	if e != nil || n < 1 {
		return errors.New("image_passes must be a positive integer")
	}
	return nil
}
