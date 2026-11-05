// SPDX-License-Identifier: GPL-3.0-or-later
package computer

import (
	"context"
	"hash/crc32"

	g "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
)

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
	if c.identity.Capabilities&g.FesComputerCapabilityMediaZx81Tape == 0 ||
		len(data) < int(g.FesComputerZx81TapeMinBytes) || len(data) > int(g.FesDe25ComputerZx81TapeMaxBytes) {
		return ErrArgument
	}
	o, err := c.begin(ctx, "load-tape")
	if err != nil {
		return err
	}
	defer func() { o.finish(err) }()
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
