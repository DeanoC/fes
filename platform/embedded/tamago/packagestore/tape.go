// SPDX-License-Identifier: GPL-3.0-or-later
package packagestore

import (
	"bytes"
	"errors"

	cg "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
)

// MaxRetainedTapeBytes is the shared ZX81 unit-0 media size bound. The selected
// native driver may impose a smaller bound before hardware containment.
const MaxRetainedTapeBytes = 16 << 10

const C64DiskFormat = "c64-d64"

const SpectrumTapeFormat = "spectrum-tape"
const MaxRetainedSpectrumTapeBytes = 64 << 10

// TapeRequest selects immutable game media separately from the core and its
// firmware ROM. It supplies no hardware capability or filesystem access after
// containment. The zero value means launch without initial media.
type TapeRequest struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Format string `json:"format,omitempty"`
}

// InputLimit is the file limit for the selected tape format.
func (r TapeRequest) InputLimit() int {
	switch r.Format {
	case "":
		return MaxRetainedTapeBytes
	case C64DiskFormat:
		return int(cg.FesComputerC64DiskBytes)
	case SpectrumTapeFormat:
		return MaxRetainedSpectrumTapeBytes
	default:
		return 0
	}
}

func (r TapeRequest) Validate() error {
	if r == (TapeRequest{}) {
		return nil
	}
	if !pathOK(r.Path) || !digest(r.SHA256) || r.Size < 1 || r.Size > int64(r.InputLimit()) ||
		r.Format == C64DiskFormat && r.Size != int64(cg.FesComputerC64DiskBytes) {
		return errors.New("package store: complete bounded media selection required")
	}
	return nil
}

// Tape returns a copy of media retained under the same SD lease as the archive.
// The selected driver's preflight must still admit its interface and size.
func (p *Selection) Tape() []byte {
	if p == nil {
		return nil
	}
	return bytes.Clone(p.tape)
}

// TapeFormat reports the retained request's declared format.
func (p *Selection) TapeFormat() string {
	if p == nil {
		return ""
	}
	return p.tapeFormat
}
