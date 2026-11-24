// SPDX-License-Identifier: GPL-3.0-or-later
package hci

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	rtlMaxChunkBytes = 252
	rtlLMPSub8761B   = 0x8761

	opcodeRTLReadROMVersion = 0xfc6d
	opcodeRTLDownload       = 0xfc20
)

var (
	rtlEPatchMagic = []byte("Realtech")
	rtlEPatchEnd   = []byte{0x51, 0x04, 0xfd, 0x77}
)

// FirmwareLoader fetches named Realtek firmware/config blobs (for example from
// FAT32 FES/BTFW/ via packagestore.Store.ReadBTFirmware).
type FirmwareLoader interface {
	LoadFirmware(name string) ([]byte, error)
}

// BuildRTLPatchPayload parses a Realtek EPATCH v1 firmware image (such as
// rtl8761bu_fw.bin), selects the patch matching chip_id == romVersion+1, stamps
// the 4-byte firmware version onto the patch tail, and appends the optional
// Realtek config blob (such as rtl8761bu_config.bin).
func BuildRTLPatchPayload(fw, cfg []byte, romVersion uint8) ([]byte, error) {
	if len(fw) < 16 || !bytes.Equal(fw[:8], rtlEPatchMagic) {
		return nil, errors.New("bt/hci: invalid Realtek EPATCH header")
	}
	if !bytes.Equal(fw[len(fw)-4:], rtlEPatchEnd) {
		return nil, errors.New("bt/hci: missing Realtek EPATCH end marker")
	}
	numPatches := int(binary.LittleEndian.Uint16(fw[12:14]))
	if numPatches == 0 || 14+numPatches*8 > len(fw)-4 {
		return nil, fmt.Errorf("bt/hci: invalid Realtek patch table count=%d", numPatches)
	}
	wantChipID := uint16(romVersion) + 1
	for i := 0; i < numPatches; i++ {
		chipID := binary.LittleEndian.Uint16(fw[14+2*i:])
		if chipID != wantChipID {
			continue
		}
		patchLen := int(binary.LittleEndian.Uint16(fw[14+2*numPatches+2*i:]))
		patchOff := int(binary.LittleEndian.Uint32(fw[14+4*numPatches+4*i:]))
		if patchLen < 4 || patchOff < 14 || patchOff+patchLen > len(fw)-4 {
			return nil, fmt.Errorf("bt/hci: corrupt Realtek patch bounds off=%#x len=%d", patchOff, patchLen)
		}
		out := make([]byte, patchLen+len(cfg))
		copy(out[:patchLen], fw[patchOff:patchOff+patchLen])
		// Per Linux btrtl.c, the last 4 bytes of the patch carry fw_version LE.
		copy(out[patchLen-4:patchLen], fw[8:12])
		copy(out[patchLen:], cfg)
		return out, nil
	}
	return nil, fmt.Errorf("bt/hci: no Realtek patch for rom_version=%d (chip_id=%d)", romVersion, wantChipID)
}

// UploadRTLFirmware queries the dongle's ROM version via 0xFC6D, builds the
// chip-matched EPATCH+config image, and streams it in 252-byte chunks over HCI
// vendor command 0xFC20 (matching Linux btrtl rtl_download_firmware).
func UploadRTLFirmware(t *Transport, fw, cfg []byte) error {
	if t == nil {
		return errors.New("bt/hci: nil transport")
	}
	var resp [16]byte
	n, err := t.SendCommand(opcodeRTLReadROMVersion, nil, resp[:])
	if err != nil {
		return fmt.Errorf("bt/hci: RTL Read_ROM_Version: %w", err)
	}
	if n < 2 || resp[0] != 0 {
		return fmt.Errorf("bt/hci: RTL Read_ROM_Version rejected status=%#x n=%d", resp[0], n)
	}
	romVersion := resp[1]
	payload, err := BuildRTLPatchPayload(fw, cfg, romVersion)
	if err != nil {
		return err
	}

	var pkt [1 + rtlMaxChunkBytes]byte
	total := len(payload)
	fragNum := (total + rtlMaxChunkBytes - 1) / rtlMaxChunkBytes
	var idx byte
	for i := 0; i < fragNum; i++ {
		off := i * rtlMaxChunkBytes
		chunkLen := min(rtlMaxChunkBytes, total-off)
		fragIdx := idx
		if i == fragNum-1 {
			fragIdx |= 0x80 // Final chunk marker
		}
		pkt[0] = fragIdx
		copy(pkt[1:1+chunkLen], payload[off:off+chunkLen])
		rn, err := t.SendCommand(opcodeRTLDownload, pkt[:1+chunkLen], resp[:])
		if err != nil {
			return fmt.Errorf("bt/hci: RTL download chunk %d/%d: %w", i+1, fragNum, err)
		}
		if rn < 1 || resp[0] != 0 {
			return fmt.Errorf("bt/hci: RTL download chunk %d/%d status=%#x", i+1, fragNum, resp[0])
		}
		idx++
		if idx >= 0x80 {
			// Realtek bootloader wraps 0x7F -> 0x01 (0x00 is reserved for chunk 0).
			idx = 0x01
		}
	}
	return nil
}
