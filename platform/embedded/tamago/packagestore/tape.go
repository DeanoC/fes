// SPDX-License-Identifier: GPL-3.0-or-later
package packagestore

import (
	"bytes"
	"errors"
)

// MaxRetainedTapeBytes is the shared ZX81 unit-0 media size bound. The selected
// native driver may impose a smaller bound before hardware containment.
const MaxRetainedTapeBytes = 16 << 10

// TapeRequest selects immutable game media separately from the core and its
// firmware ROM. It supplies no hardware capability or filesystem access after
// containment. The zero value means launch without a tape.
type TapeRequest struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func (r TapeRequest) Validate() error {
	if r == (TapeRequest{}) {
		return nil
	}
	if !pathOK(r.Path) || !digest(r.SHA256) || r.Size < 1 || r.Size > MaxRetainedTapeBytes {
		return errors.New("package store: complete bounded tape selection required")
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
