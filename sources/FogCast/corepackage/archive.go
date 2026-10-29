package corepackage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
)

// Archive retains a validated canonical archive in private memory. Inspection
// is not hardware admission: the runtime still owns target/ABI compatibility,
// linked ROM/composition receipts, programming and live identity verification.
type Archive struct {
	inspection                Inspection
	manifest, payload, romMap []byte
}

// RetainArchive reads exactly length bytes, validates the existing format-2/3/4
// package rules and retains the same bytes independently of the reader. It
// requires neither an OS path nor private POSIX staging. It publishes no files
// and makes no persistence or supported-hardware claim.
// The reader must return promptly or enforce its own I/O deadlines; cancellation
// is observed between reads and validation phases, not inside a blocked Read.
func RetainArchive(ctx context.Context, length int64, reader io.Reader) (*Archive, error) {
	if ctx == nil || reader == nil {
		return nil, errors.New("core package: missing archive input")
	}
	if length < 1 || length > MaxArchiveSize {
		return nil, fmt.Errorf("core package: archive size must be 1 through %d bytes", MaxArchiveSize)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// One bounded allocation, including an extra byte to reject understated
	// lengths. Limit individual reads and reject non-progressing readers.
	data := make([]byte, int(length)+1)
	offset, stalled := 0, 0
	for offset < len(data) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		chunk := data[offset:min(offset+(32<<10), len(data))]
		n, err := reader.Read(chunk)
		if n < 0 || n > len(chunk) {
			return nil, errors.New("core package: invalid reader count")
		}
		offset += n
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		if n == 0 {
			stalled++
			if stalled >= 100 {
				return nil, io.ErrNoProgress
			}
		} else {
			stalled = 0
		}
		if runtime.GOOS == "tamago" {
			runtime.Gosched()
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if int64(offset) != length {
		return nil, errors.New("core package: upload length does not match the declared length")
	}
	manifest, payload, romMap, err := readArchiveMembers(data[:offset], false)
	if err != nil {
		return nil, err
	}
	descriptor, err := decodeContext(ctx, manifest, payload, romMap)
	if err != nil {
		return nil, err
	}
	id := packageIdentity(manifest, payload, romMap)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &Archive{inspection: Inspection{PackageID: id, Descriptor: descriptor}, manifest: manifest, payload: payload, romMap: romMap}, nil
}

// Inspection returns an independent descriptor so edits cannot change retained
// identity or later inspections of this archive.
func (a *Archive) Inspection() Inspection { return cloneInspection(a.inspection) }

// Payload returns an independent cursor over the validated payload. Reads copy
// into caller buffers; no mutable archive slice or WriteTo shortcut escapes.
// The reader keeps the retained bytes alive independently of the Archive.
func (a *Archive) Payload() io.Reader {
	return &contextReader{ctx: context.Background(), reader: bytes.NewReader(a.payload)}
}
