// SPDX-License-Identifier: GPL-3.0-or-later

package sdhci

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/dmaops"
)

func TestDecodeCardInfoVectors(t *testing.T) {
	// CID[127:8]: MID=0x27, OEM="PH", Product="SD64G", Rev=3.2, Serial=0x12345678, MDT=2025-11 (year-2000=25 -> 0x019, month=11 -> 0xb -> 0x01, 0x9b).
	cid := [15]byte{
		0x27, 'P', 'H', 'S', 'D', '6', '4', 'G',
		0x32, 0x12, 0x34, 0x56, 0x78, 0x01, 0x9b,
	}
	// CSD v2.0[127:8]: CSDStructure=1, TRAN_SPEED=0x32, CCC=0x5b5, C_SIZE=15 (16384 sectors), PermWP|TempWP.
	csd := [15]byte{
		1 << 6, 0x0e, 0x00, 0x32,
		0x5b, 0x59, 0x00, 0x00,
		0x00, 0x0f, 0x7f, 0x80,
		0x0a, 0x40, 0x30,
	}

	// Default Open without SCR leaves HasSCR false.
	noSCR := DecodeCardInfo(cid, csd, false, [8]byte{})
	if noSCR.ManufacturerID != 0x27 || noSCR.OEMID != "PH" || noSCR.ProductName != "SD64G" {
		t.Fatalf("unexpected CID identity: %+v", noSCR)
	}
	if noSCR.RevisionMajor != 3 || noSCR.RevisionMinor != 2 || noSCR.SerialNumber != 0x12345678 {
		t.Fatalf("unexpected CID rev/serial: %+v", noSCR)
	}
	if noSCR.MfgYear != 2025 || noSCR.MfgMonth != 11 {
		t.Fatalf("unexpected Mfg date: %d-%02d", noSCR.MfgYear, noSCR.MfgMonth)
	}
	if noSCR.CSDStructure != 1 || noSCR.TranSpeed != 0x32 || noSCR.CommandClasses != 0x5b5 {
		t.Fatalf("unexpected CSD fields: %+v", noSCR)
	}
	if noSCR.Sectors != 16384 || noSCR.CapacityBytes != 16384*block.SectorSize {
		t.Fatalf("unexpected capacity: sectors=%d bytes=%d", noSCR.Sectors, noSCR.CapacityBytes)
	}
	if !noSCR.PermWriteProtect || !noSCR.TempWriteProtect || noSCR.HasSCR || noSCR.SDSpec != "" || noSCR.SupportsCMD23 {
		t.Fatalf("unexpected WP/SCR state: %+v", noSCR)
	}

	// Verify SD specification decoding table across SCR versions + CMD23 bit.
	specCases := []struct {
		name string
		scr  [8]byte
		want string
	}{
		{name: "1.0x", scr: [8]byte{0x00, 0x05, 0x00, 0x00}, want: "1.0x"},
		{name: "1.10", scr: [8]byte{0x01, 0x05, 0x00, 0x00}, want: "1.10"},
		{name: "2.00", scr: [8]byte{0x02, 0x05, 0x00, 0x00}, want: "2.00"},
		{name: "3.0x", scr: [8]byte{0x02, 0x05, 0x80, 0x02}, want: "3.0x"},
		{name: "4.xx", scr: [8]byte{0x02, 0x05, 0x84, 0x02}, want: "4.xx"},
		{name: "5.xx", scr: [8]byte{0x02, 0x05, 0x84, 0x42}, want: "5.xx"},
		{name: "6.xx", scr: [8]byte{0x02, 0x05, 0x84, 0x82}, want: "6.xx"},
		{name: "7.xx", scr: [8]byte{0x02, 0x05, 0x84, 0xc2}, want: "7.xx"},
		{name: "8.xx", scr: [8]byte{0x02, 0x05, 0x85, 0x02}, want: "8.xx"},
		{name: "9.xx", scr: [8]byte{0x02, 0x05, 0x85, 0x42}, want: "9.xx"},
	}
	for _, tc := range specCases {
		got := DecodeCardInfo(cid, csd, true, tc.scr)
		if !got.HasSCR || got.SDSpec != tc.want || got.BusWidths != 0x05 {
			t.Fatalf("%s: got HasSCR=%v SDSpec=%q BusWidths=%#x", tc.name, got.HasSCR, got.SDSpec, got.BusWidths)
		}
		wantCMD23 := tc.scr[3]&0x02 != 0
		if got.SupportsCMD23 != wantCMD23 {
			t.Fatalf("%s: SupportsCMD23=%v want %v", tc.name, got.SupportsCMD23, wantCMD23)
		}
	}
}

func TestDeviceCardInfoBeforeAndAfterSCR(t *testing.T) {
	c := newCard()
	d, err := Open(c, Config{InputClockHz: 100_000_000})
	if err != nil {
		t.Fatal(err)
	}
	info := d.CardInfo()
	if info.HasSCR || info.Sectors != 8192 || info.CapacityBytes != 8192*block.SectorSize || d.CMD23Active() {
		t.Fatalf("unexpected default CardInfo: %+v cmd23=%v", info, d.CMD23Active())
	}

	hw := newAdvancedCard(2048, 0, nil, cap33V)
	hw.scr = [8]byte{0x02, 0x35, 0x84, 0x02} // SD 4.xx, 1+4 bit, CMD23 supported
	dev, err := Open(hw, Config{
		InputClockHz: 100_000_000,
		BusWidth4Bit: true,
		MultiBlock:   true,
		UseCMD23:     true,
	})
	if err != nil {
		t.Fatalf("Open 4-bit CMD23: %v", err)
	}
	adv := dev.CardInfo()
	if !adv.HasSCR || adv.SDSpec != "4.xx" || !adv.SupportsCMD23 || !dev.CMD23Active() {
		t.Fatalf("unexpected advanced CardInfo: %+v cmd23=%v", adv, dev.CMD23Active())
	}
	if adv.Sectors != 2048 || adv.CapacityBytes != 2048*block.SectorSize || adv.TranSpeed != 0x32 {
		t.Fatalf("unexpected advanced geometry: %+v", adv)
	}
}

func TestCMD23ConfigAndSCRRejection(t *testing.T) {
	hw := newAdvancedCard(256, 0, nil, cap33V)
	before := len(hw.commands)
	if _, err := Open(hw, Config{InputClockHz: 100_000_000, UseCMD23: true}); err == nil || len(hw.commands) != before {
		t.Fatalf("UseCMD23 without MultiBlock accepted or touched card: err=%v cmds=%d", err, len(hw.commands))
	}

	// Card SCR without SCR[33] must refuse UseCMD23 cleanly.
	hw.scr = [8]byte{0x02, 0x35, 0x80, 0x00}
	if _, err := Open(hw, Config{
		InputClockHz: 100_000_000,
		MultiBlock:   true,
		UseCMD23:     true,
	}); err == nil || !strings.Contains(err.Error(), "CMD23") {
		t.Fatalf("expected CMD23 SCR rejection, got %v", err)
	}
}

func TestCMD23MultiBlockPIOAndTailSingleSector(t *testing.T) {
	hw := newAdvancedCard(256, 0, nil, cap33V)
	hw.scr = [8]byte{0x02, 0x35, 0x80, 0x02} // SD 3.0x + CMD23

	dev, err := Open(hw, Config{
		InputClockHz: 100_000_000,
		AllowWrites:  true,
		BusWidth4Bit: true,
		MultiBlock:   true,
		UseCMD23:     true,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !dev.CMD23Active() {
		t.Fatal("CMD23Active = false")
	}

	// Read 129 sectors: chunk 1 = 128 sectors (CMD23 + CMD18), chunk 2 = 1 sector (CMD17, no CMD23).
	hw.commands = nil
	dst := make([]byte, 129*512)
	if err := dev.ReadSectors(10, dst); err != nil {
		t.Fatalf("ReadSectors(129): %v", err)
	}
	if !bytes.Equal(dst, hw.image[10*512:139*512]) {
		t.Fatal("CMD23 PIO read mismatch")
	}
	var opcodes []uint32
	var cmd18Flags uint32
	for _, cmd := range hw.commands {
		op := cmd >> 24
		opcodes = append(opcodes, op)
		if op == 18 {
			cmd18Flags = cmd
		}
	}
	wantReadOps := []uint32{23, 18, 17}
	if len(opcodes) != len(wantReadOps) || opcodes[0] != 23 || opcodes[1] != 18 || opcodes[2] != 17 {
		t.Fatalf("unexpected read opcode sequence: %v, want %v", opcodes, wantReadOps)
	}
	if cmd18Flags&(1<<2) != 0 {
		t.Fatalf("CMD18 unexpectedly set Auto-CMD12 bit: %#x", cmd18Flags)
	}
	if cmd18Flags&((1<<5)|(1<<1)) != ((1 << 5) | (1 << 1)) {
		t.Fatalf("CMD18 missing multi-block / block-count bits: %#x", cmd18Flags)
	}

	// Write 129 sectors: CMD23(128) + CMD25(128) + CMD13, then CMD24(1) + CMD13.
	hw.commands = nil
	payload := make([]byte, 129*512)
	for i := range payload {
		payload[i] = byte((i*7 + 3) & 0xff)
	}
	if err := dev.WriteSectors(20, payload); err != nil {
		t.Fatalf("WriteSectors(129): %v", err)
	}
	if !bytes.Equal(hw.image[20*512:149*512], payload) {
		t.Fatal("CMD23 PIO write mismatch")
	}
	opcodes = nil
	var cmd25Flags uint32
	for _, cmd := range hw.commands {
		op := cmd >> 24
		opcodes = append(opcodes, op)
		if op == 25 {
			cmd25Flags = cmd
		}
	}
	wantWriteOps := []uint32{23, 25, 13, 24, 13}
	if len(opcodes) != len(wantWriteOps) {
		t.Fatalf("unexpected write opcodes: %v, want %v", opcodes, wantWriteOps)
	}
	for i := range wantWriteOps {
		if opcodes[i] != wantWriteOps[i] {
			t.Fatalf("write opcode[%d]=%d want %d (all=%v)", i, opcodes[i], wantWriteOps[i], opcodes)
		}
	}
	if cmd25Flags&(1<<2) != 0 {
		t.Fatalf("CMD25 unexpectedly set Auto-CMD12 bit: %#x", cmd25Flags)
	}
}

func TestCMD23ADMA2AndResumeEnforcement(t *testing.T) {
	dmaBase := uint64(0x80000000)
	dmaRAM := make([]byte, 256*1024)
	pool, err := dmaops.NewPool(dmaBase, dmaRAM, dmaops.Cached, &testCoherency{line: 64})
	if err != nil {
		t.Fatalf("NewFixedPool: %v", err)
	}

	hw := newAdvancedCard(256, dmaBase, dmaRAM, cap33V|capADMA2)
	hw.scr = [8]byte{0x02, 0x35, 0x84, 0x02}
	dev, err := Open(hw, Config{
		InputClockHz: 100_000_000,
		AllowWrites:  true,
		BusWidth4Bit: true,
		MultiBlock:   true,
		UseCMD23:     true,
		UseADMA2:     true,
		DMAPool:      pool,
	})
	if err != nil {
		t.Fatalf("Open ADMA2+CMD23: %v", err)
	}

	hw.commands = nil
	wbuf := make([]byte, 16*512)
	for i := range wbuf {
		wbuf[i] = byte((i*19 + 5) & 0xff)
	}
	if err := dev.WriteSectors(8, wbuf); err != nil {
		t.Fatalf("WriteSectors ADMA2+CMD23: %v", err)
	}
	rbuf := make([]byte, 16*512)
	if err := dev.ReadSectors(8, rbuf); err != nil {
		t.Fatalf("ReadSectors ADMA2+CMD23: %v", err)
	}
	if !bytes.Equal(rbuf, wbuf) {
		t.Fatal("ADMA2+CMD23 roundtrip mismatch")
	}
	var cmd23Count int
	for _, cmd := range hw.commands {
		if cmd>>24 == 23 {
			cmd23Count++
		}
		if (cmd>>24 == 18 || cmd>>24 == 25) && cmd&(1<<2) != 0 {
			t.Fatalf("ADMA2 CMD%d set Auto-CMD12 bit: %#x", cmd>>24, cmd)
		}
	}
	if cmd23Count != 2 {
		t.Fatalf("expected 2 CMD23 commands (write + read), got %d (%v)", cmd23Count, hw.commands)
	}

	// Quiesce/Resume with matching SCR succeeds and keeps CMD23 active.
	if err := dev.Quiesce(); err != nil {
		t.Fatalf("Quiesce: %v", err)
	}
	if err := dev.Resume(); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if !dev.CMD23Active() {
		t.Fatal("CMD23 not preserved after Resume")
	}

	// Losing SCR CMD23 bit across Resume fails closed and retires device + poisons ADMA2.
	if err := dev.Quiesce(); err != nil {
		t.Fatalf("Quiesce 2: %v", err)
	}
	hw.mu.Lock()
	hw.scr[3] &^= 0x02
	hw.mu.Unlock()
	if err := dev.Resume(); err == nil {
		t.Fatal("expected Resume failure when card loses CMD23 support")
	}
	if err := dev.Close(); !errors.Is(err, ErrRetired) {
		t.Fatalf("Close after retired Resume = %v, want ErrRetired", err)
	}
}

func TestCMD23RejectionPoisonsADMA2(t *testing.T) {
	dmaBase := uint64(0x80000000)
	dmaRAM := make([]byte, 256*1024)
	pool, err := dmaops.NewPool(dmaBase, dmaRAM, dmaops.Cached, &testCoherency{line: 64})
	if err != nil {
		t.Fatal(err)
	}

	hw := newAdvancedCard(128, dmaBase, dmaRAM, cap33V|capADMA2)
	hw.scr = [8]byte{0x02, 0x35, 0x84, 0x02}
	dev, err := Open(hw, Config{
		InputClockHz: 100_000_000,
		BusWidth4Bit: true,
		MultiBlock:   true,
		UseCMD23:     true,
		UseADMA2:     true,
		DMAPool:      pool,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Force command-inhibit before CMD23 so CMD23 times out and poisons ADMA2.
	hw.mu.Lock()
	hw.regs[present] = 3
	hw.mu.Unlock()
	buf := make([]byte, 4*512)
	if err := dev.ReadSectors(0, buf); err == nil {
		t.Fatal("expected ReadSectors failure on CMD23 inhibit")
	}
	if !errors.Is(dev.ReadSectors(0, buf), ErrRetired) {
		t.Fatal("device not retired after CMD23 failure")
	}
	if err := dev.Close(); !errors.Is(err, ErrRetired) {
		t.Fatalf("Close after failed CMD23 = %v, want ErrRetired", err)
	}
}
