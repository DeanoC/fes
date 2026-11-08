// SPDX-License-Identifier: GPL-3.0-or-later
package computer

import (
	"bytes"
	"context"
	"hash/crc32"

	g "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
)

// InitialTapeRequest supplies one private source copy and selected identity.
// The trusted platform owns fixed publication memory, complete live framing,
// per-CSR CPU/reset guards, execution-held checks and the secure lifecycle lock.
// It must finish synchronously within the operation's bound and retain any
// ambiguous publication. No physical address or timeout is supplied here.
type InitialTapeRequest struct {
	Image      []byte
	Identity   Identity
	GPO, CRC32 uint32
}

type InitialTapeReceipt struct{ GPO, Bytes, CRC32 uint32 }
type InitialTapeLoader func(context.Context, InitialTapeRequest) (InitialTapeReceipt, error)

// LoadInitialTape uses only an explicitly selected secure loader, or the
// ordinary protocol when no loader was selected at construction. It requires
// the acknowledged Hold established by the caller; it never issues Hold/Run.
func (c *Computer) LoadInitialTape(ctx context.Context, data []byte) error {
	return c.loadTape(ctx, data, true)
}

func (o *operation) tapeState() (uint16, error) {
	var info [6]uint16
	for i := range info {
		v, err := o.exchange(g.FesComputerOpcodeMediaInfo, uint32(i), 0)
		if err != nil {
			return 0, err
		}
		info[i] = v
	}
	if uint32(info[0])|uint32(info[1])<<16 != g.FesComputerZx81TapeMinBytes ||
		uint32(info[2])|uint32(info[3])<<16 != g.FesDe25ComputerZx81TapeMaxBytes ||
		uint32(info[4]) != g.FesComputerMediaChunkMaxBytes ||
		(info[5] != uint16(g.FesComputerMediaStateEmpty) && info[5] != uint16(g.FesComputerMediaStateReady)) {
		return 0, ErrProtocol
	}
	return info[5], nil
}

// LoadTape replaces the diagnostic board's unit-0 ZX81 tape. The board limit
// (16375 bytes) is intentionally stricter than the generic ABI's 16384 bytes.
// CRC, offset, ordinal and odd-byte padding use the shared media protocol. This
// method does not implement persistent media writes or package ROM linking.
func (c *Computer) LoadTape(ctx context.Context, data []byte) (err error) {
	return c.loadTape(ctx, data, false)
}

func (c *Computer) loadTape(ctx context.Context, data []byte, initial bool) (err error) {
	if c.identity.Capabilities&g.FesComputerCapabilityMediaZx81Tape == 0 ||
		len(data) < int(g.FesComputerZx81TapeMinBytes) || len(data) > int(g.FesDe25ComputerZx81TapeMaxBytes) {
		return ErrArgument
	}
	o, err := c.begin(ctx, "load-tape")
	if err != nil {
		return err
	}
	defer func() { o.finish(err) }()
	if initial && c.gpo & ^g.FesComputerRequestMask != g.FesComputerOpcodeExecution<<24|g.FesComputerControlIndex<<16|g.FesComputerExecutionHoldReset {
		return ErrProtocol
	}
	if initial && c.initialTape != nil {
		image := bytes.Clone(data)
		request := InitialTapeRequest{Image: bytes.Clone(image), Identity: c.identity, GPO: c.gpo, CRC32: crc32.ChecksumIEEE(image)}
		o.phase = "before-initial-tape"
		if err = o.check(); err != nil {
			return err
		}
		var receipt InitialTapeReceipt
		receipt, err = c.initialTape(ctx, request)
		if err != nil {
			return err
		}
		o.phase = "after-initial-tape"
		if err = o.check(); err != nil {
			return err
		}
		// The secure engine queries 16 identity words at both ends, six media
		// words at both ends, four begin words, three per chunk, data and commit.
		commands := 49 + 3*((len(image)+int(g.FesComputerMediaChunkMaxBytes)-1)/int(g.FesComputerMediaChunkMaxBytes)) + (len(image)+1)/2
		requestBit := c.gpo & g.FesComputerRequestMask
		if commands%2 != 0 {
			requestBit ^= g.FesComputerRequestMask
		}
		want := requestBit | g.FesComputerOpcodeIdentity<<24 | (g.FesComputerIdentityWordCount-1)<<16
		if receipt.GPO != want || receipt.Bytes != uint32(len(image)) || receipt.CRC32 != request.CRC32 || !bytes.Equal(request.Image, image) {
			return ErrProtocol
		}
		c.gpo = receipt.GPO
		return nil
	}
	if _, err = o.tapeState(); err != nil {
		return err
	}
	// Keep a caller mutation from changing bytes after their CRC is computed.
	image := append([]byte(nil), data...)
	crc := crc32.ChecksumIEEE(image)
	for i, word := range [...]uint16{uint16(len(image)), 0, uint16(crc), uint16(crc >> 16)} {
		if err = o.mutate(g.FesComputerOpcodeMediaBegin, uint32(i), word); err != nil {
			return err
		}
	}
	for offset := 0; offset < len(image); {
		length := min(int(g.FesComputerMediaChunkMaxBytes), len(image)-offset)
		for i, word := range [...]uint16{uint16(offset), uint16(offset >> 16), uint16(length)} {
			if err = o.mutate(g.FesComputerOpcodeMediaChunk, uint32(i), word); err != nil {
				return err
			}
		}
		for i := 0; i < length; i += 2 {
			word := uint16(image[offset+i])
			if i+1 < length {
				word |= uint16(image[offset+i+1]) << 8
			}
			if err = o.mutate(g.FesComputerOpcodeMediaData, uint32(i/2), word); err != nil {
				return err
			}
		}
		offset += length
	}
	if err = o.mutate(g.FesComputerOpcodeMediaCommit, g.FesComputerZx81TapeUnit, 0); err != nil {
		return err
	}
	state, err := o.tapeState()
	if err != nil {
		return err
	}
	if uint32(state) != g.FesComputerMediaStateReady {
		return ErrProtocol
	}
	return nil
}

func (c *Computer) EjectTape(ctx context.Context) (err error) {
	if c.identity.Capabilities&g.FesComputerCapabilityMediaZx81Tape == 0 {
		return ErrArgument
	}
	o, err := c.begin(ctx, "eject-tape")
	if err != nil {
		return err
	}
	defer func() { o.finish(err) }()
	if _, err = o.tapeState(); err != nil {
		return err
	}
	if err = o.mutate(g.FesComputerOpcodeMediaEject, g.FesComputerZx81TapeUnit, 0); err != nil {
		return err
	}
	state, err := o.tapeState()
	if err != nil {
		return err
	}
	if uint32(state) != g.FesComputerMediaStateEmpty {
		return ErrProtocol
	}
	return nil
}
