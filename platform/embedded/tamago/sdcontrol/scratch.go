// SPDX-License-Identifier: GPL-3.0-or-later
package sdcontrol

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/DeanoC/fes/platform/embedded/tamago/sdhci"
)

const scratchSectors = 8

// ScratchPlan is trusted build input for the bounded native write diagnostic.
// It authorizes only the final eight sectors, outside all MBR partitions.
// Normal filesystem and package code cannot select this mode.
type ScratchPlan struct {
	Format         int    `json:"format"`
	Scope          string `json:"scope"`
	CID            string `json:"cid"`
	Sectors        uint64 `json:"sectors"`
	MBRSHA256      string `json:"mbr_sha256"`
	OriginalSHA256 string `json:"original_sha256"`
}

func ParseScratchPlan(data []byte) (ScratchPlan, error) {
	var p ScratchPlan
	if len(data) == 0 || len(data) > 4096 {
		return p, fmt.Errorf("sd scratch: invalid plan length")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return p, err
	}
	if d.Decode(new(any)) != io.EOF {
		return p, fmt.Errorf("sd scratch: trailing plan data")
	}
	return p, p.Validate()
}

func (p ScratchPlan) Identity() sdhci.Identity {
	var id sdhci.Identity
	b, _ := hex.DecodeString(p.CID)
	copy(id.CID[:], b)
	id.Sectors = p.Sectors
	return id
}

func (p ScratchPlan) Validate() error {
	validHex := func(s string, size int) bool {
		b, err := hex.DecodeString(s)
		return err == nil && len(b) == size && s == strings.ToLower(s)
	}
	if p.Format != 1 || p.Scope != "nano-sd-scratch-write-v1" || !validHex(p.CID, 15) ||
		p.CID == strings.Repeat("0", 30) || p.CID == strings.Repeat("f", 30) ||
		p.Sectors < 4096 || p.Sectors > 1<<32 || p.Sectors%1024 != 0 ||
		!validHex(p.MBRSHA256, 32) || !validHex(p.OriginalSHA256, 32) ||
		p.OriginalSHA256 == fmt.Sprintf("%x", sha256.Sum256(scratchPattern())) {
		return fmt.Errorf("sd scratch: invalid card, geometry or content expectation")
	}
	return nil
}

func scratchPattern() []byte {
	b := make([]byte, scratchSectors*512)
	for i := range b {
		b[i] = byte(i*37 + (i/512)*17 + 0x5a)
	}
	return b
}

// InspectScratch obtains candidate plan data through an existing owned session.
// It performs only reads; it neither enables writes nor grants a later test.
func InspectScratch(s *Session) (ScratchPlan, error) {
	id := s.Identity()
	p := ScratchPlan{Format: 1, Scope: "nano-sd-scratch-write-v1", CID: fmt.Sprintf("%x", id.CID), Sectors: id.Sectors,
		MBRSHA256: strings.Repeat("0", 64), OriginalSHA256: strings.Repeat("0", 64)}
	if err := p.Validate(); err != nil {
		return ScratchPlan{}, err
	}
	r, valid := s.LastReceipt()
	if !valid || !r.Active || !r.Prepared {
		return ScratchPlan{}, fmt.Errorf("sd scratch: inspection requires active ownership")
	}
	mbr, tail := make([]byte, 512), make([]byte, scratchSectors*512)
	if err := s.ReadSectors(0, mbr); err != nil {
		return ScratchPlan{}, err
	}
	p.MBRSHA256 = fmt.Sprintf("%x", sha256.Sum256(mbr))
	if err := p.checkMBR(mbr); err != nil {
		return ScratchPlan{}, err
	}
	if err := s.ReadSectors(id.Sectors-scratchSectors, tail); err != nil {
		return ScratchPlan{}, err
	}
	p.OriginalSHA256 = fmt.Sprintf("%x", sha256.Sum256(tail))
	return p, p.Validate()
}

func (p ScratchPlan) checkMBR(b []byte) error {
	if len(b) != 512 || b[510] != 0x55 || b[511] != 0xaa || fmt.Sprintf("%x", sha256.Sum256(b)) != p.MBRSHA256 {
		return fmt.Errorf("sd scratch: MBR hash or signature mismatch")
	}
	type extent struct{ start, end uint64 }
	var extents []extent
	fats := 0
	for i := 0; i < 4; i++ {
		e := b[446+i*16 : 462+i*16]
		if bytes.Equal(e, make([]byte, 16)) {
			continue
		}
		if (e[0] != 0 && e[0] != 0x80) || (e[4] != 0xa2 && e[4] != 0x0b && e[4] != 0x0c) {
			return fmt.Errorf("sd scratch: unsupported MBR partition")
		}
		if e[4] == 0x0b || e[4] == 0x0c {
			fats++
		}
		start, count := uint64(binary.LittleEndian.Uint32(e[8:])), uint64(binary.LittleEndian.Uint32(e[12:]))
		if start == 0 || count == 0 || start >= p.Sectors || count > p.Sectors-start || start+count > p.Sectors-scratchSectors {
			return fmt.Errorf("sd scratch: partition bounds overlap scratch or media edge")
		}
		for _, old := range extents {
			if start < old.end && old.start < start+count {
				return fmt.Errorf("sd scratch: overlapping partitions")
			}
		}
		extents = append(extents, extent{start, start + count})
	}
	if fats != 1 {
		return fmt.Errorf("sd scratch: expected one FAT partition")
	}
	return nil
}

// RunScratch writes once, reads back, contains, resumes the same card, reads
// back again and ends contained. Every failure is terminal to this invocation:
// no retry, rollback or automatic stop is attempted. Keep the Session alive.
// A success is not power-loss, FAT-write or normal appliance qualification.
func RunScratch(s *Session, p ScratchPlan, log io.Writer) error {
	if err := p.Validate(); err != nil {
		return err
	}
	initial, valid := s.LastReceipt()
	if !valid || !initial.Active || !initial.Prepared || initial.Generation > ^uint64(0)-4 || s.Identity() != p.Identity() || log == nil {
		return fmt.Errorf("sd scratch: invalid initial owned card")
	}
	lba := p.Sectors - scratchSectors
	emit := func(stage string, delta uint64, active bool) error {
		r, valid := s.LastReceipt()
		if !valid || r.Owner != initial.Owner || r.Generation != initial.Generation+delta || r.Active != active || active && !r.Prepared {
			return fmt.Errorf("sd scratch: unexpected %s receipt", stage)
		}
		_, err := fmt.Fprintf(log, "FES_NANO_SDWRITE %s cid=%s sectors=%d lba=%d count=8 owner=%016x generation=%d sha256=%x\n", stage, p.CID, p.Sectors, lba, r.Owner, r.Generation, sha256.Sum256(scratchPattern()))
		return err
	}
	mbr := make([]byte, 512)
	if err := s.ReadSectors(0, mbr); err != nil {
		return err
	}
	if err := p.checkMBR(mbr); err != nil {
		return err
	}
	original := make([]byte, scratchSectors*512)
	if err := s.ReadSectors(lba, original); err != nil {
		return err
	}
	if fmt.Sprintf("%x", sha256.Sum256(original)) != p.OriginalSHA256 {
		return fmt.Errorf("sd scratch: original contents changed")
	}
	if err := emit("VERIFIED", 0, true); err != nil {
		return err
	}
	pattern := scratchPattern()
	if err := s.WriteSectors(lba, pattern); err != nil {
		return err
	}
	if err := s.Flush(); err != nil {
		return err
	}
	verify := func() error {
		b := make([]byte, len(pattern))
		if err := s.ReadSectors(lba, b); err != nil {
			return err
		}
		if !bytes.Equal(b, pattern) {
			return fmt.Errorf("sd scratch: data readback mismatch")
		}
		b = make([]byte, 512)
		if err := s.ReadSectors(0, b); err != nil {
			return err
		}
		return p.checkMBR(b)
	}
	if err := verify(); err != nil {
		return err
	}
	if err := emit("WRITTEN", 0, true); err != nil {
		return err
	}
	if err := s.Quiesce(); err != nil {
		return err
	}
	if err := emit("CONTAINED", 1, false); err != nil {
		return err
	}
	if err := s.Resume(); err != nil {
		return err
	}
	if err := verify(); err != nil {
		return err
	}
	if err := emit("RESUMED", 3, true); err != nil {
		return err
	}
	if err := s.Quiesce(); err != nil {
		return err
	}
	return emit("PASS", 4, false)
}
