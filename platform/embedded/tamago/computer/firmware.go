// SPDX-License-Identifier: GPL-3.0-or-later
package computer

import (
	"bytes"
	"context"
	"fmt"
	"hash/crc32"

	g "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
)

// FirmwareDataBatchWords caps one held-ROM data burst. The individual
// mailbox transactions and the public operation deadline still apply.
const FirmwareDataBatchWords = 64

// C64DataBatchWords is one complete 512-byte disk chunk. The 36 KiB C64
// firmware uses the same bound to amortize guarded register mappings.
const C64DataBatchWords = 256

// FirmwareDataBatcher optionally supplies an owned, synchronous IO scope for
// a single burst of firmware Data commands. The callback must run once or
// return an admission error, and must never be retried after partial IO.
// The supplied port is valid only for the duration of the callback.
type FirmwareDataBatcher interface {
	WithFirmwareDataBatch(func(IO) error) error
}

// ValidFirmwareUploadSize reports the size envelope of the DE25 mailbox ROM map.
func ValidFirmwareUploadSize(size int64) bool {
	return size >= 1024 && size <= 256<<10 && size%1024 == 0
}

// LoadInitialFirmware uploads an already admitted format-3 firmware source
// over the declared firmware-upload 1.0 interface. The caller must bind the
// bytes to its retained package receipt and select a fresh, owned FPGA epoch.
// The immediately preceding command must be an acknowledged Hold. This method
// leaves execution held and never retries a command or trusts a ready image
// from an earlier owner. Limits bound the whole upload and each acknowledgement.
func (c *Computer) LoadInitialFirmware(ctx context.Context, image []byte) (err error) {
	if !ValidFirmwareUploadSize(int64(len(image))) || c.identity.Minor != 0 {
		return ErrArgument
	}
	o, err := c.begin(ctx, "initial-firmware")
	if err != nil {
		return err
	}
	defer func() { o.finish(err) }()
	if c.gpo & ^g.FesComputerRequestMask != g.FesComputerOpcodeExecution<<24|g.FesComputerControlIndex<<16|g.FesComputerExecutionHoldReset {
		return fmt.Errorf("%w: firmware requires acknowledged Hold", ErrProtocol)
	}
	owned := bytes.Clone(image)
	size := uint32(len(owned))
	if err := o.checkFirmwareInfo(size, g.FesFirmwareUploadStateEmpty, 0); err != nil {
		return err
	}
	crc := crc32.ChecksumIEEE(owned)
	for i, value := range [...]uint16{uint16(size), uint16(size >> 16), uint16(crc), uint16(crc >> 16)} {
		if err := o.mutate(g.FesFirmwareUploadOpcodeBegin, uint32(i), value); err != nil {
			return err
		}
	}
	batch, accelerated := c.io.(FirmwareDataBatcher)
	words := FirmwareDataBatchWords
	if c.identity.Capabilities&g.FesComputerCapabilityMediaC64Disk != 0 {
		words = C64DataBatchWords
	}
	for base := 0; base < len(owned); base += 2 * words {
		end := min(base+2*words, len(owned))
		send := func(io IO) error {
			o.io = io
			defer func() { o.io = c.io }()
			for i := base; i < end; i += 2 {
				word := uint16(owned[i]) | uint16(owned[i+1])<<8
				if err := o.mutate(g.FesFirmwareUploadOpcodeData, uint32(i/2)&0xff, word); err != nil {
					return err
				}
			}
			return nil
		}
		if err := o.check(); err != nil {
			return err
		}
		if accelerated {
			err = batch.WithFirmwareDataBatch(send)
		} else {
			err = send(c.io)
		}
		if err != nil {
			return err
		}
	}
	if err := o.mutate(g.FesFirmwareUploadOpcodeCommit, 0, 0); err != nil {
		return err
	}
	return o.checkFirmwareInfo(size, g.FesFirmwareUploadStateReady, size)
}

func (o *operation) checkFirmwareInfo(size, state, received uint32) error {
	expected := [...]uint16{
		g.FesFirmwareUploadInfoBytesLow:     uint16(size),
		g.FesFirmwareUploadInfoBytesHigh:    uint16(size >> 16),
		g.FesFirmwareUploadInfoState:        uint16(state),
		g.FesFirmwareUploadInfoReceivedLow:  uint16(received),
		g.FesFirmwareUploadInfoReceivedHigh: uint16(received >> 16),
	}
	for i, want := range expected {
		got, err := o.exchange(g.FesFirmwareUploadOpcodeInfo, uint32(i), 0)
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("%w: firmware info field %d got %#x want %#x", ErrProtocol, i, got, want)
		}
	}
	return nil
}
