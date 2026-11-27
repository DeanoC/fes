// SPDX-License-Identifier: GPL-3.0-or-later

package adv7513_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/adv7513"
	"github.com/DeanoC/fes/platform/embedded/tamago/i2c"
)

type simSlave interface {
	handleWrite(reg uint8, data []byte) error
	handleRead(reg uint8, count int) ([]byte, error)
}

type advSimSlave struct {
	mu           sync.Mutex
	regs         [256]byte
	history      []adv7513.RegisterWrite
	edidPollNeed int
	edidPolls    int
}

func newADVSimSlave(edidPollNeed int) *advSimSlave {
	s := &advSimSlave{edidPollNeed: edidPollNeed}
	s.regs[adv7513.RegChipRevision] = adv7513.ExpectedChipRevision
	s.regs[adv7513.RegStatus] = adv7513.StatusLinkReady
	s.regs[adv7513.RegPllStatus] = adv7513.PllLocked
	s.regs[adv7513.RegVicDetected] = adv7513.Vic720p60 << adv7513.VicShift
	return s
}

func (s *advSimSlave) handleWrite(reg uint8, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, b := range data {
		addr := reg + uint8(i)
		s.regs[addr] = b
		s.history = append(s.history, adv7513.RegisterWrite{Address: addr, Value: b})
		// Writing 1 to INT0 clears W1C bits; pulsing EDID_READ_CTRL triggers ready.
		if addr == adv7513.RegInt0 {
			s.regs[adv7513.RegInt0] &^= b
			s.edidPolls = 0
		}
		if addr == adv7513.RegEdidReadCtrl && b == adv7513.EdidReadTrigger {
			s.edidPolls = 0
		}
	}
	return nil
}

func (s *advSimSlave) handleRead(reg uint8, count int) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]byte, count)
	for i := 0; i < count; i++ {
		addr := reg + uint8(i)
		if addr == adv7513.RegInt0 {
			s.edidPolls++
			if s.edidPolls >= s.edidPollNeed {
				s.regs[adv7513.RegInt0] |= adv7513.Int0EDIDReady
			}
		}
		out[i] = s.regs[addr]
	}
	return out, nil
}

type edidMemSlave struct {
	mem [256]byte
}

func (e *edidMemSlave) handleWrite(reg uint8, data []byte) error { return nil }

func (e *edidMemSlave) handleRead(reg uint8, count int) ([]byte, error) {
	out := make([]byte, count)
	for i := 0; i < count; i++ {
		out[i] = e.mem[int(reg)+i]
	}
	return out, nil
}

// Local minimal DW_apb_i2c simulator wiring i2c.Controller -> ADV7513 + EDID slaves.
type localDWBus struct {
	mu           sync.Mutex
	con          uint32
	tar          uint8
	enabled      bool
	rawIntr      uint32
	abrtSrc      uint32
	slaves       map[uint8]simSlave
	rxFIFO       []byte
	txHasReg     bool
	txReg        uint8
	txData       []byte
	pendingReads int
}

func newLocalDWBus(adv *advSimSlave, edid *edidMemSlave) (*i2c.Controller, error) {
	b := &localDWBus{
		slaves: map[uint8]simSlave{
			adv7513.MainMapAddr7Bit:    adv,
			adv7513.DefaultEDIDMapAddr: edid,
		},
	}
	return i2c.Open(b, i2c.Config{Speed: i2c.SpeedFast})
}

func (b *localDWBus) Read32(offset uint32) uint32 {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch offset {
	case i2c.RegCompType:
		return i2c.DWCompType
	case i2c.RegCompParam1:
		return (15 << 16) | (15 << 8)
	case i2c.RegEnableStatus:
		if b.enabled {
			return 1
		}
		return 0
	case i2c.RegRawIntrStat, i2c.RegIntrStat:
		return b.rawIntr
	case i2c.RegTxAbrtSource:
		return b.abrtSrc
	case i2c.RegStatus:
		st := uint32((1 << 1) | (1 << 2))
		if len(b.rxFIFO) > 0 {
			st |= 1 << 3
		}
		return st
	case i2c.RegDataCmd:
		v := b.rxFIFO[0]
		b.rxFIFO = b.rxFIFO[1:]
		return uint32(v)
	case i2c.RegClrIntr, i2c.RegClrTxAbrt:
		b.rawIntr, b.abrtSrc = 0, 0
		return 0
	case i2c.RegClrStopDet:
		b.rawIntr &^= 1 << 9
		return 0
	default:
		return 0
	}
}

func (b *localDWBus) Write32(offset uint32, val uint32) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch offset {
	case i2c.RegCon:
		b.con = val
	case i2c.RegTar:
		b.tar = uint8(val & 0x7f)
	case i2c.RegEnable:
		b.enabled = val&1 != 0
	case i2c.RegDataCmd:
		sl := b.slaves[b.tar]
		if sl == nil {
			b.rawIntr |= 1 << 6
			b.abrtSrc = 1 << 0
			return
		}
		if val&(1<<8) != 0 {
			b.pendingReads++
			out, _ := sl.handleRead(b.txReg, 1)
			b.txReg++
			b.rxFIFO = append(b.rxFIFO, out[0])
		} else if !b.txHasReg {
			b.txHasReg = true
			b.txReg = byte(val)
		} else {
			b.txData = append(b.txData, byte(val))
		}
		if val&(1<<9) != 0 {
			if b.txHasReg && b.pendingReads == 0 {
				_ = sl.handleWrite(b.txReg, b.txData)
			}
			b.rawIntr |= 1 << 9
			b.txHasReg, b.txReg, b.txData, b.pendingReads = false, 0, nil, 0
		}
	}
}

func makeSample256EDID() []byte {
	raw := make([]byte, 256)
	// Base EDID header.
	copy(raw[0:8], []byte{0x00, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x00})
	// Manufacturer "FES": F=6, E=5, S=19 -> (6<<10)|(5<<5)|19 = 0x18b3.
	binary.BigEndian.PutUint16(raw[8:10], (6<<10)|(5<<5)|19)
	binary.LittleEndian.PutUint16(raw[10:12], 0x2501)
	binary.LittleEndian.PutUint32(raw[12:16], 0x12345678)
	raw[16] = 12
	raw[17] = 36 // 2026
	raw[18] = 1
	raw[19] = 4
	raw[20] = 0x80 // digital input

	// Descriptor 0: 1280x720@60 (74.25 MHz -> 7425 * 10kHz).
	d0 := raw[54:72]
	binary.LittleEndian.PutUint16(d0[0:2], 7425)
	d0[2] = byte(1280 & 0xff)
	d0[3] = byte(370 & 0xff)
	d0[4] = byte(((1280 >> 8) << 4) | (370 >> 8))
	d0[5] = byte(720 & 0xff)
	d0[6] = 30
	d0[7] = byte(((720 >> 8) << 4) | 0)
	d0[8] = 110
	d0[9] = 40
	d0[10] = (5 << 4) | 5

	// Descriptor 1: Monitor Name "FES-HDMI".
	d1 := raw[72:90]
	d1[3] = 0xfc
	copy(d1[5:18], "FES-HDMI\n    ")

	raw[126] = 1 // 1 CEA-861 extension block
	var sum0 byte
	for i := 0; i < 127; i++ {
		sum0 += raw[i]
	}
	raw[127] = byte(256 - int(sum0))

	// Extension block 1: CEA-861.
	ext := raw[128:256]
	ext[0] = 0x02 // CEA tag
	ext[1] = 0x03 // rev 3
	// Data blocks: Video (tag 2, len 2: VIC 4 native, VIC 16), Audio (tag 1, len 3), HDMI VSDB (tag 3, len 5).
	blocks := []byte{
		(2 << 5) | 2, 0x80 | 4, 16,
		(1 << 5) | 3, (1 << 3) | 1, 1 << 2, 0x07, // LPCM 2ch, 48 kHz
		(3 << 5) | 5, 0x03, 0x0c, 0x00, 0x10, 0x00, // HDMI OUI 00:0C:03, addr 1.0.0.0
	}
	copy(ext[4:], blocks)
	ext[2] = byte(4 + len(blocks)) // dtdStart
	ext[3] = 0x70                  // BasicAudio + YCbCr444 + YCbCr422
	var sum1 byte
	for i := 0; i < 127; i++ {
		sum1 += ext[i]
	}
	ext[127] = byte(256 - int(sum1))
	return raw
}

func TestParityWithLibmisterRuntimeHeader(t *testing.T) {
	// Matches TestDeGeneratorPacks720pFields in adv7513_test.cpp.
	encoded := adv7513.EncodeDEGenerator(adv7513.Menu720pDE)
	want := [6]byte{0x40, 0xd9, 0x0a, 0x00, 0x2d, 0x00}
	if encoded != want {
		t.Fatalf("EncodeDEGenerator = %x, want %x", encoded, want)
	}
	decoded := adv7513.DecodeDEGenerator(encoded)
	if decoded != adv7513.Menu720pDE {
		t.Fatalf("DecodeDEGenerator = %+v, want %+v", decoded, adv7513.Menu720pDE)
	}

	// Matches TestNamedFieldsMatchProgrammingGuideBytes in adv7513_test.cpp.
	if adv7513.MainMapAddr7Bit != 0x39 ||
		adv7513.Rgb4448bitStyle1 != 0x38 ||
		adv7513.Sync720p16x9 != 0x62 ||
		adv7513.HdmiNoHdcp != 0x06 ||
		adv7513.PowerUp != 0x10 ||
		adv7513.StatusLinkReady != 0x60 ||
		adv7513.PixelRepeatManual != 0x40 ||
		adv7513.Vic720p60 != 4 ||
		adv7513.I2cFreqId48kRgb444 != 0x20 ||
		adv7513.CscRgbIdentityUpper != 0xa8 {
		t.Fatal("ADV7513 register constant mismatch with adv7513.hpp")
	}
	if hi, mid, lo := adv7513.Encode20Bit(adv7513.AudioN48k); hi != 0x00 || mid != 0x18 || lo != 0x00 {
		t.Fatalf("N48k = (%x,%x,%x)", hi, mid, lo)
	}
	if hi, mid, lo := adv7513.Encode20Bit(adv7513.AudioCts74250kHz); hi != 0x01 || mid != 0x22 || lo != 0x0a {
		t.Fatalf("Cts74250kHz = (%x,%x,%x)", hi, mid, lo)
	}

	// Matches TestApplicationAudioProgrammingGuideValues in adv7513_test.cpp.
	regs := make(map[uint8]uint8)
	var updates []uint8
	for _, w := range adv7513.ApplicationAudio48k() {
		regs[w.Address] = w.Value
		if w.Address == 0x4a {
			updates = append(updates, w.Value)
		}
	}
	if regs[0x0a] != 0x01 || regs[0x0b] != 0x2e || regs[0x0c] != 0x84 || regs[0x0e] != 0x01 ||
		regs[0x12] != 0x00 || regs[0x14] != 0x02 || regs[0x15] != 0x20 ||
		regs[0x01] != 0x00 || regs[0x02] != 0x18 || regs[0x03] != 0x00 ||
		regs[0x47] != 0x00 || regs[0x73] != 0x01 ||
		!bytes.Equal(updates, []byte{0xa0, 0x80}) {
		t.Fatalf("ApplicationAudio48k mismatch: regs=%v updates=%x", regs, updates)
	}

	// Matches TestMenuInitializationPowersUpFirstAndSetsRequiredF9 in adv7513_test.cpp.
	initSeq := adv7513.Menu720p60Initialization()
	if len(initSeq) < 4 || initSeq[0] != (adv7513.RegisterWrite{Address: 0xd6, Value: 0xc0}) ||
		initSeq[1] != (adv7513.RegisterWrite{Address: 0x41, Value: 0x10}) {
		t.Fatalf("unexpected Menu720p60Initialization prefix: %+v", initSeq[:4])
	}
	sawF9 := false
	for _, w := range initSeq {
		if w.Address == 0xf9 && w.Value == 0x00 {
			sawF9 = true
		}
	}
	if !sawF9 {
		t.Fatal("missing adi_required 0xf9 = 0x00 write")
	}
}

func TestDriverEndToEndOverDesignWareI2CAndEDID(t *testing.T) {
	ctx := context.Background()
	adv := newADVSimSlave(2)
	edidMem := &edidMemSlave{}
	sampleEDID := makeSample256EDID()
	copy(edidMem.mem[:], sampleEDID)

	bus, err := newLocalDWBus(adv, edidMem)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adv7513.New(nil, adv7513.Config{}); !errors.Is(err, adv7513.ErrInvalidConfig) {
		t.Fatalf("nil bus got %v", err)
	}
	if _, err := adv7513.New(bus, adv7513.Config{MaxEDIDPolls: -1}); !errors.Is(err, adv7513.ErrInvalidConfig) {
		t.Fatalf("bad MaxEDIDPolls got %v", err)
	}

	dev, err := adv7513.New(bus, adv7513.Config{})
	if err != nil {
		t.Fatal(err)
	}
	rev, err := dev.ChipRevision(ctx)
	if err != nil || rev != adv7513.ExpectedChipRevision {
		t.Fatalf("ChipRevision = (%#x, %v)", rev, err)
	}

	if err := dev.InitMenu720p60(ctx); err != nil {
		t.Fatalf("InitMenu720p60: %v", err)
	}
	st, err := dev.Status(ctx)
	if err != nil || !st.HPD || !st.MonitorSense || !st.PoweredUp || !st.PLLLocked || st.DetectedVIC != adv7513.Vic720p60 {
		t.Fatalf("Status after init = %+v, err=%v", st, err)
	}

	if err := dev.ConfigureAudio48k(ctx, true); err != nil {
		t.Fatalf("ConfigureAudio48k: %v", err)
	}
	if adv.regs[adv7513.RegPacketEnable1] != adv7513.PacketsVideoAudio {
		t.Fatalf("PacketEnable1 = %#x, want %#x", adv.regs[adv7513.RegPacketEnable1], adv7513.PacketsVideoAudio)
	}

	// Custom ApplyWrites + PowerDown/PowerUp.
	if err := dev.ApplyWrites(ctx, []adv7513.RegisterWrite{{Address: adv7513.RegClockDelay, Value: 0x60}}); err != nil {
		t.Fatal(err)
	}
	if adv.regs[adv7513.RegClockDelay] != 0x60 {
		t.Fatalf("ClockDelay = %#x", adv.regs[adv7513.RegClockDelay])
	}
	if err := dev.PowerDown(ctx); err != nil {
		t.Fatal(err)
	}
	if st, err := dev.Status(ctx); err != nil || st.PoweredUp {
		t.Fatalf("expected PoweredUp=false after PowerDown: %+v (%v)", st, err)
	}
	if err := dev.PowerUp(ctx, true); err != nil {
		t.Fatal(err)
	}
	if st, err := dev.Status(ctx); err != nil || !st.PoweredUp {
		t.Fatalf("expected PoweredUp=true after PowerUp: %+v (%v)", st, err)
	}

	// Full 256-byte EDID read & CEA-861 parsing.
	edid, raw, err := dev.ReadEDID(ctx)
	if err != nil {
		t.Fatalf("ReadEDID: %v", err)
	}
	if !bytes.Equal(raw, sampleEDID) {
		t.Fatalf("raw EDID bytes mismatch")
	}
	if edid.Manufacturer != "FES" || edid.MonitorName != "FES-HDMI" || !edid.DigitalInput || !edid.IsHDMI ||
		edid.HDMIAddr != ([4]uint8{1, 0, 0, 0}) || !edid.SupportsVIC(4) || !edid.SupportsVIC(16) ||
		edid.SupportsVIC(99) || !edid.SupportsAudio48kLPCM() {
		t.Fatalf("unexpected parsed EDID: %+v", edid)
	}
	pref, ok := edid.PreferredTiming()
	if !ok || pref.HActive != 1280 || pref.VActive != 720 || pref.PixelClockKHz != 74250 {
		t.Fatalf("unexpected preferred timing: %+v (ok=%v)", pref, ok)
	}
	if hz := pref.RefreshMHz(); hz < 59900 || hz > 60100 {
		t.Fatalf("unexpected RefreshMHz: %d", hz)
	}

	// Poll timeout when monitor never asserts INT0 EDID_READY.
	neverReady := newADVSimSlave(1000)
	busNever, err := newLocalDWBus(neverReady, edidMem)
	if err != nil {
		t.Fatal(err)
	}
	devTimeout, _ := adv7513.New(busNever, adv7513.Config{MaxEDIDPolls: 3})
	if _, _, err := devTimeout.ReadEDID(ctx); !errors.Is(err, i2c.ErrTimeout) {
		t.Fatalf("EDID timeout got %v, want ErrTimeout", err)
	}
}

func TestParseEDIDRejectsMalformedInputs(t *testing.T) {
	valid := makeSample256EDID()
	if _, err := adv7513.ParseEDID(valid[:64]); !errors.Is(err, adv7513.ErrInvalidEDID) {
		t.Fatalf("short EDID got %v", err)
	}
	badHdr := append([]byte(nil), valid...)
	badHdr[0] = 0x12
	if _, err := adv7513.ParseEDID(badHdr); !errors.Is(err, adv7513.ErrInvalidEDID) {
		t.Fatalf("bad header got %v", err)
	}
	badSum0 := append([]byte(nil), valid...)
	badSum0[20] ^= 0x01
	if _, err := adv7513.ParseEDID(badSum0); !errors.Is(err, adv7513.ErrInvalidEDID) {
		t.Fatalf("bad base checksum got %v", err)
	}
	badSum1 := append([]byte(nil), valid...)
	badSum1[135] ^= 0x01
	if _, err := adv7513.ParseEDID(badSum1); !errors.Is(err, adv7513.ErrInvalidEDID) {
		t.Fatalf("bad ext checksum got %v", err)
	}
}
