// Package appliance defines the closed, raw-image appliance release format.
package appliance

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode"
)

const (
	Format                = 1
	Board                 = "de10-nano"
	BootABI               = "fes-bootstrap-v1"
	MaxImageSize    int64 = (4 << 30) - 1
	MaxManifestSize       = 8192
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
}

var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

func ValidHash(s string) bool { return hashPattern.MatchString(s) }
func (m Manifest) Validate() error {
	if m.Format != Format || m.Board != Board || m.BootABI != BootABI {
		return errors.New("unsupported release format, board, or boot ABI")
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
	return nil
}
func (m Manifest) Compatible(kernelSHA string) error {
	if e := m.Validate(); e != nil {
		return e
	}
	if m.KernelSHA256 != kernelSHA {
		return errors.New("release kernel differs from stable bootstrap")
	}
	return nil
}

// DecodeManifest rejects duplicate, unknown, case-variant and missing fields.
func DecodeManifest(r io.Reader) (Manifest, error) {
	var m Manifest
	b, e := io.ReadAll(io.LimitReader(r, MaxManifestSize+1))
	if e != nil {
		return m, e
	}
	if len(b) > MaxManifestSize {
		return m, errors.New("manifest too large")
	}
	names := []string{"format", "board", "boot_abi", "version", "kernel_sha256", "image_sha256", "image_size", "fes_revision", "fogcast_revision", "runtime_revision"}
	fields := make(map[string]bool, len(names))
	for _, n := range names {
		fields[n] = false
	}
	d := json.NewDecoder(bytes.NewReader(b))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return m, errors.New("manifest must be a JSON object")
	}
	for d.More() {
		t, e = d.Token()
		if e != nil {
			return m, e
		}
		n, ok := t.(string)
		if !ok {
			return m, errors.New("invalid field name")
		}
		seen, known := fields[n]
		if !known || seen {
			return m, fmt.Errorf("unknown or duplicate manifest field %q", n)
		}
		fields[n] = true
		var raw json.RawMessage
		if e = d.Decode(&raw); e != nil {
			return m, e
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return m, fmt.Errorf("null manifest field %q", n)
		}
	}
	if _, e = d.Token(); e != nil {
		return m, e
	}
	if _, e = d.Token(); e != io.EOF {
		return m, errors.New("trailing manifest data")
	}
	for n, seen := range fields {
		if !seen {
			return m, fmt.Errorf("missing manifest field %q", n)
		}
	}
	if e = json.Unmarshal(b, &m); e != nil {
		return m, e
	}
	return m, m.Validate()
}
