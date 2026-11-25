// SPDX-License-Identifier: GPL-3.0-or-later

package xgmac

import (
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"math/bits"
	"net"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/network"
)

func TestMulticastHashIndexAndValidation(t *testing.T) {
	mdns := net.HardwareAddr{0x01, 0x00, 0x5e, 0x00, 0x00, 0xfb}
	allHosts := net.HardwareAddr{0x01, 0x00, 0x5e, 0x00, 0x00, 0x01}

	for _, tc := range []struct {
		bits  int
		shift uint
	}{
		{64, 26},
		{128, 25},
		{256, 24},
	} {
		idx, err := MulticastHashIndex(mdns, tc.bits)
		want := bits.Reverse32(crc32.ChecksumIEEE(mdns)) >> tc.shift
		if err != nil || idx != want || idx >= uint32(tc.bits) {
			t.Fatalf("MulticastHashIndex(mdns, %d)=(%d,%v), want %d", tc.bits, idx, err, want)
		}
		if _, err := MulticastHashIndex(allHosts, tc.bits); err != nil {
			t.Fatalf("allHosts %d bits err=%v", tc.bits, err)
		}
	}

	for _, bad := range []net.HardwareAddr{
		{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}, // unicast
		{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, // broadcast
		{0x01, 0x00, 0x5e, 0x00},             // short
	} {
		if _, err := MulticastHashIndex(bad, 64); !errors.Is(err, ErrInvalidMulticast) {
			t.Fatalf("MulticastHashIndex(%v) err=%v, want ErrInvalidMulticast", bad, err)
		}
	}
	if _, err := MulticastHashIndex(mdns, 32); err == nil {
		t.Fatal("unsupported hash table width 32 accepted")
	}
}

type filterMMCModel struct {
	*hardware
	mismatchHash    bool
	resetMMCOnRead  bool
	readClearFields []uint32
}

func (m *filterMMCModel) Read32(reg uint32) uint32 {
	v := m.hardware.Read32(reg)
	if m.resetMMCOnRead && reg >= mmcTXOctetsGB && reg <= mmcRXFIFOOverflows+4 {
		m.regs[reg] = 0
	}
	return v
}

func (m *filterMMCModel) Write32(reg, value uint32) {
	if reg >= hashTableBase && reg < hashTableBase+32 && m.mismatchHash {
		m.regs[reg] = value ^ 0x55aa55aa
		return
	}
	if reg == mmcControl && value&mmcCtrlResetOnRead != 0 {
		m.resetMMCOnRead = true
	}
	if reg == mmcControl && value&mmcCtrlCounterReset != 0 {
		for _, r := range []uint32{
			mmcTXOctetsGB, mmcTXOctetsGB + 4,
			mmcTXPacketsGB, mmcTXPacketsGB + 4,
			mmcRXPacketsGB, mmcRXPacketsGB + 4,
			mmcRXOctetsGB, mmcRXOctetsGB + 4,
			mmcRXCRCErrors, mmcRXCRCErrors + 4,
			mmcRXRuntErrors, mmcRXJabberErrors, mmcRXUndersizeGood,
			mmcRXLengthErrors, mmcRXLengthErrors + 4,
			mmcRXFIFOOverflows, mmcRXFIFOOverflows + 4,
		} {
			m.regs[r] = 0
		}
	}
	m.hardware.Write32(reg, value)
}

func newFilterConfig() Config {
	return Config{
		MAC:           "02:11:22:33:44:55",
		PHYAddress:    4,
		PHYID:         0x001cc916,
		CSRClockHz:    125000000,
		RXBufferShift: 4,
	}
}

func TestSetMulticastMACsLifecycleAndFailClosed(t *testing.T) {
	// features1 bits 25:24 == 0 => unsupported hash table rejects non-empty lists.
	hNoHash := newHardware()
	dNoHash, err := Open(hNoHash, hNoHash.mem, newFilterConfig())
	if err != nil {
		t.Fatal(err)
	}
	mdns := net.HardwareAddr{0x01, 0x00, 0x5e, 0x00, 0x00, 0xfb}
	if dNoHash.MulticastHashBits() != 0 {
		t.Fatalf("hashBits=%d want 0", dNoHash.MulticastHashBits())
	}
	if err := dNoHash.SetMulticastMACs([]net.HardwareAddr{mdns}); !errors.Is(err, ErrMulticastUnsupported) {
		t.Fatalf("unsupported SetMulticastMACs err=%v, want ErrMulticastUnsupported", err)
	}
	if err := dNoHash.SetMulticastMACs(nil); err != nil {
		t.Fatalf("empty SetMulticastMACs on 0-bit hash table failed: %v", err)
	}

	// features1 bits 25:24 == 2 => 128-bit (4 uint32 hash registers).
	h := newHardware()
	h.regs[features1] = (2 << 24) | (8 << 6) | 7
	m := &filterMMCModel{hardware: h}
	d, err := Open(m, h.mem, newFilterConfig())
	if err != nil {
		t.Fatal(err)
	}
	if d.MulticastHashBits() != 128 {
		t.Fatalf("hashBits=%d want 128", d.MulticastHashBits())
	}

	allHosts := net.HardwareAddr{0x01, 0x00, 0x5e, 0x00, 0x00, 0x01}
	groups := []net.HardwareAddr{mdns, allHosts}
	if err := d.SetMulticastMACs(groups); err != nil {
		t.Fatalf("SetMulticastMACs: %v", err)
	}
	if h.regs[packetFilter] != filterHMC {
		t.Fatalf("packetFilter=%#x want filterHMC (%#x)", h.regs[packetFilter], filterHMC)
	}
	var wantHash [4]uint32
	for _, g := range groups {
		idx, _ := MulticastHashIndex(g, 128)
		wantHash[idx/32] |= uint32(1) << (idx % 32)
	}
	for i := 0; i < 4; i++ {
		if h.regs[hashTableBase+uint32(i)*4] != wantHash[i] {
			t.Fatalf("hash reg %d=%#x want %#x", i, h.regs[hashTableBase+uint32(i)*4], wantHash[i])
		}
	}

	// Verify persistence across BindControl.Stop/Start reset.
	ctrl, err := d.BindControl(network.DMAControl{
		Stop: func(context.Context) error {
			// Clear MAC filter registers as a hardware reset during Stop would.
			h.regs[packetFilter] = 0
			for i := 0; i < 4; i++ {
				h.regs[hashTableBase+uint32(i)*4] = 0
			}
			return nil
		},
		Start: func(context.Context) error {
			h.regs[dmaMode] = 0
			h.regs[txConfig] = 0
			h.regs[rxConfig] = 0
			h.regs[txControl] = 0
			h.regs[rxControl] = 0
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := d.SetMulticastMACs(groups); !errors.Is(err, network.ErrPaused) {
		t.Fatalf("paused SetMulticastMACs err=%v, want ErrPaused", err)
	}
	if err := ctrl.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.regs[packetFilter] != filterHMC {
		t.Fatalf("Start did not restore filterHMC: %#x", h.regs[packetFilter])
	}
	for i := 0; i < 4; i++ {
		if h.regs[hashTableBase+uint32(i)*4] != wantHash[i] {
			t.Fatalf("Start did not restore hash reg %d: got %#x want %#x", i, h.regs[hashTableBase+uint32(i)*4], wantHash[i])
		}
	}

	// Clearing clears HMC and zeros all hash registers.
	if err := d.SetMulticastMACs(nil); err != nil {
		t.Fatalf("clear SetMulticastMACs: %v", err)
	}
	if h.regs[packetFilter] != 0 {
		t.Fatalf("packetFilter after clear=%#x want 0", h.regs[packetFilter])
	}

	// Hardware readback mismatch retires the device fail-closed.
	m.mismatchHash = true
	if err := d.SetMulticastMACs(groups); err == nil {
		t.Fatal("hash readback mismatch succeeded unexpectedly")
	}
	if d.Err() == nil {
		t.Fatal("hash readback mismatch did not retire device")
	}
	// Ensure frame encoding reference test uses binary safely.
	_ = binary.LittleEndian
}

func TestHardwareMMCCountersSnapshotAndResetOnReadAccumulation(t *testing.T) {
	// 1. Default snapshot mode (ResetMMCOnRead=false).
	hSnap := newHardware()
	mSnap := &filterMMCModel{hardware: hSnap}
	dSnap, err := Open(mSnap, hSnap.mem, newFilterConfig())
	if err != nil {
		t.Fatal(err)
	}
	hSnap.regs[mmcTXOctetsGB] = 0x1000
	hSnap.regs[mmcTXOctetsGB+4] = 0x0002
	hSnap.regs[mmcRXCRCErrors] = 7
	hSnap.regs[mmcRXRuntErrors] = 3
	hSnap.regs[mmcRXFIFOOverflows] = 11

	st, err := dSnap.HardwareStats()
	if err != nil {
		t.Fatal(err)
	}
	if st.TXOctetsGB != (2<<32)|0x1000 || st.RXCRCErrors != 7 || st.RXRuntErrors != 3 || st.RXFIFOOverflows != 11 {
		t.Fatalf("snapshot HardwareStats=%+v", st)
	}
	if hSnap.regs[mmcControl] != 0 {
		t.Fatalf("ResetMMCOnRead=false touched mmcControl=%#x", hSnap.regs[mmcControl])
	}

	// 2. Opt-in ResetMMCOnRead=true accumulates clear-on-read deltas across polls and Stop/Start.
	h := newHardware()
	h.regs[mmcRXIntMask] = 0x1234
	h.regs[mmcTXIntMask] = 0x5678
	h.regs[mmcRXCRCErrors] = 99 // stale value before Open must be cleared by initMMCLocked
	m := &filterMMCModel{hardware: h}
	cfg := newFilterConfig()
	cfg.ResetMMCOnRead = true
	d, err := Open(m, h.mem, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if h.regs[mmcRXIntMask] != 0 || h.regs[mmcTXIntMask] != 0 {
		t.Fatalf("MMC interrupt masks not zeroed: rx=%#x tx=%#x", h.regs[mmcRXIntMask], h.regs[mmcTXIntMask])
	}
	if h.regs[mmcControl]&mmcCtrlResetOnRead == 0 {
		t.Fatalf("mmcControl=%#x missing ROR bit", h.regs[mmcControl])
	}
	if got, err := d.HardwareStats(); err != nil || got.RXCRCErrors != 0 {
		t.Fatalf("post-reset initial stats=%+v err=%v", got, err)
	}

	h.regs[mmcTXPacketsGB] = 5
	h.regs[mmcRXPacketsGB] = 8
	h.regs[mmcRXCRCErrors] = 2
	h.regs[mmcRXLengthErrors] = 1
	st1, err := d.HardwareStats()
	if err != nil {
		t.Fatal(err)
	}
	if st1.TXPacketsGB != 5 || st1.RXPacketsGB != 8 || st1.RXCRCErrors != 2 || st1.RXLengthErrors != 1 {
		t.Fatalf("poll 1=%+v", st1)
	}
	// Model cleared registers on read; add second delta before BindControl.Stop.
	h.regs[mmcTXPacketsGB] = 4
	h.regs[mmcRXJabberErrors] = 6
	h.regs[mmcRXUndersizeGood] = 1
	h.regs[mmcRXFIFOOverflows] = 3

	ctrl, err := d.BindControl(network.DMAControl{
		Stop: func(context.Context) error { return nil },
		Start: func(context.Context) error {
			h.regs[dmaMode] = 0
			h.regs[txConfig] = 0
			h.regs[rxConfig] = 0
			h.regs[txControl] = 0
			h.regs[rxControl] = 0
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := d.HardwareStats(); !errors.Is(err, network.ErrPaused) {
		t.Fatalf("paused HardwareStats err=%v, want ErrPaused", err)
	}
	if err := ctrl.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	st2, err := d.HardwareStats()
	if err != nil {
		t.Fatal(err)
	}
	if st2.TXPacketsGB != 9 || st2.RXPacketsGB != 8 || st2.RXCRCErrors != 2 || st2.RXJabberErrors != 6 || st2.RXUndersizeGood != 1 || st2.RXFIFOOverflows != 3 {
		t.Fatalf("cumulative stats across Stop/Start=%+v", st2)
	}
}
