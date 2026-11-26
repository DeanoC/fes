// SPDX-License-Identifier: GPL-3.0-or-later

package sdhci

import (
	"encoding/binary"
	"strings"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
)

// CardInfo summarizes immutable SD card identification, geometry, and
// capability fields decoded from CID, CSD (v2.0), and optional SCR registers.
type CardInfo struct {
	ManufacturerID   uint8
	OEMID            string
	ProductName      string
	RevisionMajor    uint8
	RevisionMinor    uint8
	SerialNumber     uint32
	MfgYear          uint16
	MfgMonth         uint8
	CSDStructure     uint8
	TranSpeed        uint8
	CommandClasses   uint16
	Sectors          uint64
	CapacityBytes    uint64
	PermWriteProtect bool
	TempWriteProtect bool
	HasSCR           bool
	SDSpec           string
	BusWidths        uint8
	SupportsCMD23    bool
}

// DecodeCardInfo parses SD card identification and capability fields from
// SDHCI-shifted CID[127:8], CSD[127:8], and optional big-endian wire SCR[63:0].
func DecodeCardInfo(cid [15]byte, csd [15]byte, hasSCR bool, scr [8]byte) CardInfo {
	csize := uint32(csd[7]&0x3f)<<16 | uint32(csd[8])<<8 | uint32(csd[9])
	sectors := (uint64(csize) + 1) * 1024
	mdt := uint16(cid[13]&0x0f)<<8 | uint16(cid[14])

	info := CardInfo{
		ManufacturerID:   cid[0],
		OEMID:            string(cid[1:3]),
		ProductName:      strings.TrimRight(string(cid[3:8]), " \x00"),
		RevisionMajor:    cid[8] >> 4,
		RevisionMinor:    cid[8] & 0x0f,
		SerialNumber:     binary.BigEndian.Uint32(cid[9:13]),
		MfgYear:          2000 + (mdt >> 4),
		MfgMonth:         uint8(mdt & 0x0f),
		CSDStructure:     csd[0] >> 6,
		TranSpeed:        csd[3],
		CommandClasses:   uint16(csd[4])<<4 | uint16(csd[5]>>4),
		Sectors:          sectors,
		CapacityBytes:    sectors * block.SectorSize,
		PermWriteProtect: csd[14]&(1<<5) != 0,
		TempWriteProtect: csd[14]&(1<<4) != 0,
		HasSCR:           hasSCR,
	}
	if hasSCR {
		info.BusWidths = scr[1] & 0x0f
		info.SupportsCMD23 = scr[3]&0x02 != 0
		info.SDSpec = decodeSDSpec(scr)
	}
	return info
}

func decodeSDSpec(scr [8]byte) string {
	sdSpec := scr[0] & 0x0f
	sdSpec3 := (scr[2] >> 7) & 1
	sdSpec4 := (scr[2] >> 2) & 1
	sdSpecX := ((scr[2] & 0x03) << 2) | ((scr[3] >> 6) & 0x03)

	switch {
	case sdSpec == 0:
		return "1.0x"
	case sdSpec == 1:
		return "1.10"
	case sdSpec == 2 && sdSpec3 == 0:
		return "2.00"
	case sdSpec == 2 && sdSpec3 == 1 && sdSpec4 == 0 && sdSpecX == 0:
		return "3.0x"
	case sdSpec == 2 && sdSpec3 == 1 && sdSpec4 == 1 && sdSpecX == 0:
		return "4.xx"
	case sdSpec == 2 && sdSpec3 == 1 && sdSpecX == 1:
		return "5.xx"
	case sdSpec == 2 && sdSpec3 == 1 && sdSpecX == 2:
		return "6.xx"
	case sdSpec == 2 && sdSpec3 == 1 && sdSpecX == 3:
		return "7.xx"
	case sdSpec == 2 && sdSpec3 == 1 && sdSpecX == 4:
		return "8.xx"
	case sdSpec == 2 && sdSpec3 == 1 && sdSpecX == 5:
		return "9.xx"
	default:
		return "unknown"
	}
}

// CardInfo returns decoded CID, CSD, and (when queried) SCR fields for the
// bound card. SCR fields are populated whenever ACMD51 has been read (such as
// 4-bit mode, 50 MHz High-Speed, or UseCMD23).
func (d *Device) CardInfo() CardInfo {
	d.mu.Lock()
	defer d.mu.Unlock()
	info := DecodeCardInfo(d.cid, d.csd, d.hasSCR, d.scr)
	info.Sectors = d.sectors
	info.CapacityBytes = d.sectors * block.SectorSize
	return info
}

// CMD23Active reports whether pre-defined CMD23 (SET_BLOCK_COUNT) multi-block
// transfers are enabled on this device.
func (d *Device) CMD23Active() bool { return d.cmd23 }
