// SPDX-License-Identifier: GPL-3.0-or-later

package i2c_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/i2c"
)

type simSlave interface {
	handleWrite(reg uint8, data []byte) error
	handleRead(reg uint8, count int) ([]byte, error)
}

type regMapSlave struct {
	mu   sync.Mutex
	regs map[uint8]byte
}

func newRegMapSlave() *regMapSlave {
	return &regMapSlave{regs: make(map[uint8]byte)}
}

func (s *regMapSlave) handleWrite(reg uint8, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, b := range data {
		s.regs[reg+uint8(i)] = b
	}
	return nil
}

func (s *regMapSlave) handleRead(reg uint8, count int) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]byte, count)
	for i := 0; i < count; i++ {
		out[i] = s.regs[reg+uint8(i)]
	}
	return out, nil
}

// fakeDWRegs simulates a Synopsys DW_apb_i2c register block with 16-entry FIFOs.
type fakeDWRegs struct {
	mu           sync.Mutex
	compType     uint32
	compParam1   uint32
	con          uint32
	tar          uint8
	ssHCnt       uint32
	ssLCnt       uint32
	fsHCnt       uint32
	fsLCnt       uint32
	sdaHold      uint32
	enabled      bool
	rawIntr      uint32
	abrtSrc      uint32
	slaves       map[uint8]simSlave
	rxFIFO       []byte
	txHasReg     bool
	txReg        uint8
	txData       []byte
	pendingReads int
	forceArbLost bool
	forceAbort   uint32
	stuckBusy    bool
}

func newFakeDWRegs() *fakeDWRegs {
	return &fakeDWRegs{
		compType:   i2c.DWCompType,
		compParam1: (15 << 16) | (15 << 8), // 16 TX / 16 RX entries
		slaves:     make(map[uint8]simSlave),
	}
}

func (f *fakeDWRegs) Read32(offset uint32) uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch offset {
	case i2c.RegCompType:
		return f.compType
	case i2c.RegCompParam1:
		return f.compParam1
	case i2c.RegCompVersion:
		return 0x3230312a
	case i2c.RegCon:
		return f.con
	case i2c.RegSSHCNT:
		return f.ssHCnt
	case i2c.RegSSLCNT:
		return f.ssLCnt
	case i2c.RegFSHCNT:
		return f.fsHCnt
	case i2c.RegFSLCNT:
		return f.fsLCnt
	case i2c.RegSDAHold:
		return f.sdaHold
	case i2c.RegEnableStatus:
		if f.enabled {
			return 1
		}
		return 0
	case i2c.RegRawIntrStat, i2c.RegIntrStat:
		return f.rawIntr
	case i2c.RegTxAbrtSource:
		return f.abrtSrc
	case i2c.RegStatus:
		var st uint32
		if f.stuckBusy {
			st |= (1 << 5) | (1 << 0) // MST_ACTIVITY without TFNF -> forces wait timeout
			return st
		}
		st |= (1 << 1) | (1 << 2) // TFNF | TFE (immediate execution model)
		if len(f.rxFIFO) > 0 {
			st |= 1 << 3 // RFNE
		}
		return st
	case i2c.RegDataCmd:
		if len(f.rxFIFO) == 0 {
			return 0
		}
		b := f.rxFIFO[0]
		f.rxFIFO = f.rxFIFO[1:]
		return uint32(b)
	case i2c.RegClrIntr:
		f.rawIntr = 0
		f.abrtSrc = 0
		return 0
	case i2c.RegClrTxAbrt:
		f.rawIntr &^= 1 << 6
		f.abrtSrc = 0
		return 0
	case i2c.RegClrStopDet:
		f.rawIntr &^= 1 << 9
		return 0
	case i2c.RegTxFLR:
		return 0
	case i2c.RegRxFLR:
		return uint32(len(f.rxFIFO))
	default:
		return 0
	}
}

func (f *fakeDWRegs) Write32(offset uint32, val uint32) {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch offset {
	case i2c.RegCon:
		f.con = val
	case i2c.RegTar:
		f.tar = uint8(val & 0x7f)
	case i2c.RegSSHCNT:
		f.ssHCnt = val & 0xffff
	case i2c.RegSSLCNT:
		f.ssLCnt = val & 0xffff
	case i2c.RegFSHCNT:
		f.fsHCnt = val & 0xffff
	case i2c.RegFSLCNT:
		f.fsLCnt = val & 0xffff
	case i2c.RegSDAHold:
		f.sdaHold = val
	case i2c.RegEnable:
		f.enabled = val&1 != 0
		if !f.enabled {
			f.rxFIFO = nil
			f.resetTxnLocked()
		}
	case i2c.RegDataCmd:
		if !f.enabled {
			return
		}
		if f.forceArbLost {
			f.rawIntr |= 1 << 6
			f.abrtSrc = 1 << 12
			f.resetTxnLocked()
			return
		}
		if f.forceAbort != 0 {
			f.rawIntr |= 1 << 6
			f.abrtSrc = f.forceAbort
			f.resetTxnLocked()
			return
		}
		sl := f.slaves[f.tar]
		if sl == nil {
			f.rawIntr |= 1 << 6
			f.abrtSrc = 1 << 0 // 7B_ADDR_NOACK
			f.resetTxnLocked()
			return
		}
		isRead := val&(1<<8) != 0
		isStop := val&(1<<9) != 0
		if isRead {
			f.pendingReads++
			if !f.txHasReg {
				// Probe transaction (1-byte read without prior register byte).
				f.rxFIFO = append(f.rxFIFO, 0x00)
			} else {
				// Pipeline each requested byte cleanly into rxFIFO respecting bounded FIFO.
				b, err := sl.handleRead(f.txReg, 1)
				if err != nil {
					f.rawIntr |= 1 << 6
					f.abrtSrc = 1 << 3
					f.resetTxnLocked()
					return
				}
				f.txReg++
				f.rxFIFO = append(f.rxFIFO, b[0])
			}
		} else {
			b := byte(val & 0xff)
			if !f.txHasReg {
				f.txHasReg = true
				f.txReg = b
			} else {
				f.txData = append(f.txData, b)
			}
		}
		if isStop {
			if f.txHasReg && f.pendingReads == 0 {
				if err := sl.handleWrite(f.txReg, f.txData); err != nil {
					f.rawIntr |= 1 << 6
					f.abrtSrc = 1 << 3
					f.resetTxnLocked()
					return
				}
			}
			f.rawIntr |= 1 << 9 // STOP_DET
			f.resetTxnLocked()
		}
	}
}

func (f *fakeDWRegs) resetTxnLocked() {
	f.txHasReg = false
	f.txReg = 0
	f.txData = nil
	f.pendingReads = 0
}

func TestOpenValidationAndTimingSetup(t *testing.T) {
	if _, err := i2c.Open(nil, i2c.Config{}); !errors.Is(err, i2c.ErrNilAccess) {
		t.Fatalf("nil regs got %v, want ErrNilAccess", err)
	}
	badID := newFakeDWRegs()
	badID.compType = 0x12345678
	if _, err := i2c.Open(badID, i2c.Config{}); !errors.Is(err, i2c.ErrNotFound) {
		t.Fatalf("bad compType got %v, want ErrNotFound", err)
	}
	for _, badCfg := range []i2c.Config{
		{InputClockHz: 1_000_000},
		{Speed: 123_456},
		{MaxPollIterations: -1},
	} {
		if _, err := i2c.Open(newFakeDWRegs(), badCfg); !errors.Is(err, i2c.ErrInvalidConfig) {
			t.Fatalf("cfg %+v got %v, want ErrInvalidConfig", badCfg, err)
		}
	}

	stdRegs := newFakeDWRegs()
	if _, err := i2c.Open(stdRegs, i2c.Config{InputClockHz: 100_000_000, Speed: i2c.SpeedStandard, SDAHoldCycles: 12}); err != nil {
		t.Fatal(err)
	}
	if stdRegs.ssHCnt != 400 || stdRegs.ssLCnt != 600 || stdRegs.sdaHold != 12 {
		t.Fatalf("unexpected std timing: H=%d L=%d hold=%d", stdRegs.ssHCnt, stdRegs.ssLCnt, stdRegs.sdaHold)
	}

	fastRegs := newFakeDWRegs()
	if _, err := i2c.Open(fastRegs, i2c.Config{InputClockHz: 100_000_000, Speed: i2c.SpeedFast}); err != nil {
		t.Fatal(err)
	}
	if fastRegs.fsHCnt != 90 || fastRegs.fsLCnt != 160 {
		t.Fatalf("unexpected fast timing: H=%d L=%d", fastRegs.fsHCnt, fastRegs.fsLCnt)
	}
}

func TestTransfersMultiByteEDIDSizeAndErrors(t *testing.T) {
	ctx := context.Background()
	hw := newFakeDWRegs()
	slave := newRegMapSlave()
	hw.slaves[0x39] = slave

	ctrl, err := i2c.Open(hw, i2c.Config{MaxPollIterations: 100})
	if err != nil {
		t.Fatal(err)
	}

	// Single-byte write/read + probe.
	if err := ctrl.Probe(ctx, 0x39); err != nil {
		t.Fatalf("Probe(0x39): %v", err)
	}
	if err := ctrl.Probe(ctx, 0x3d); !errors.Is(err, i2c.ErrNACK) {
		t.Fatalf("Probe(0x3d) got %v, want ErrNACK", err)
	}
	if err := ctrl.WriteRegByte(ctx, 0x39, 0x41, 0x10); err != nil {
		t.Fatal(err)
	}
	got, err := ctrl.ReadRegByte(ctx, 0x39, 0x41)
	if err != nil || got != 0x10 {
		t.Fatalf("ReadRegByte = (%#x, %v), want 0x10", got, err)
	}

	// 128-byte multi-byte write and pipelined read exceeding the 16-entry RX FIFO.
	payload := make([]byte, 128)
	for i := range payload {
		payload[i] = byte(i ^ 0x5a)
	}
	if err := ctrl.WriteReg(ctx, 0x39, 0x00, payload); err != nil {
		t.Fatalf("WriteReg 128B: %v", err)
	}
	readBack := make([]byte, 128)
	if err := ctrl.ReadReg(ctx, 0x39, 0x00, readBack); err != nil {
		t.Fatalf("ReadReg 128B: %v", err)
	}
	if !bytes.Equal(readBack, payload) {
		t.Fatalf("128B readback mismatch")
	}

	// Address & buffer validation.
	for _, badAddr := range []uint8{0x00, 0x07, 0x78, 0x7f} {
		if err := ctrl.Probe(ctx, badAddr); !errors.Is(err, i2c.ErrInvalidAddr) {
			t.Fatalf("addr %#x got %v, want ErrInvalidAddr", badAddr, err)
		}
	}
	if err := ctrl.ReadReg(ctx, 0x39, 0x00, nil); !errors.Is(err, i2c.ErrInvalidConfig) {
		t.Fatalf("empty dst got %v, want ErrInvalidConfig", err)
	}

	// Arbitration lost and generic TX abort.
	hw.mu.Lock()
	hw.forceArbLost = true
	hw.mu.Unlock()
	if err := ctrl.WriteRegByte(ctx, 0x39, 0x41, 0x10); !errors.Is(err, i2c.ErrArbitrationLost) {
		t.Fatalf("arb lost got %v, want ErrArbitrationLost", err)
	}
	hw.mu.Lock()
	hw.forceArbLost = false
	hw.forceAbort = 1 << 1 // non-NACK/non-arb abort
	hw.mu.Unlock()
	if err := ctrl.WriteRegByte(ctx, 0x39, 0x41, 0x10); !errors.Is(err, i2c.ErrAbort) {
		t.Fatalf("abort got %v, want ErrAbort", err)
	}
	hw.mu.Lock()
	hw.forceAbort = 0
	hw.stuckBusy = true
	hw.mu.Unlock()
	if err := ctrl.WriteRegByte(ctx, 0x39, 0x41, 0x10); !errors.Is(err, i2c.ErrTimeout) {
		t.Fatalf("timeout got %v, want ErrTimeout", err)
	}
	hw.mu.Lock()
	hw.stuckBusy = false
	hw.mu.Unlock()

	st := ctrl.Stats()
	if st.Writes != 2 || st.BytesWritten != 129 || st.Reads != 2 || st.BytesRead != 129 ||
		st.NACKs != 1 || st.ArbLost != 1 || st.Aborts != 1 || st.Timeouts != 1 {
		t.Fatalf("unexpected stats: %+v", st)
	}

	// Concurrent callers under -race.
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(id uint8) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				if err := ctrl.WriteRegByte(ctx, 0x39, 0x10+id, byte(i)); err != nil {
					t.Errorf("WriteRegByte: %v", err)
					return
				}
				if _, err := ctrl.ReadRegByte(ctx, 0x39, 0x10+id); err != nil {
					t.Errorf("ReadRegByte: %v", err)
					return
				}
			}
		}(uint8(g))
	}
	wg.Wait()

	// Verify that after an interrupted transfer (and even if stale RX bytes exist),
	// a subsequent transfer to the same address resets/flushes the controller cleanly.
	if err := ctrl.WriteRegByte(ctx, 0x39, 0x55, 0xab); err != nil {
		t.Fatal(err)
	}
	hw.mu.Lock()
	hw.rxFIFO = append(hw.rxFIFO, 0xde, 0xad)
	hw.stuckBusy = true
	hw.mu.Unlock()
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ctrl.ReadRegByte(cancelCtx, 0x39, 0x55); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled ReadRegByte got %v", err)
	}
	hw.mu.Lock()
	hw.stuckBusy = false
	// Also inject stale RX FIFO bytes before the retry to confirm setTargetLocked drains them.
	hw.rxFIFO = append(hw.rxFIFO, 0x99)
	hw.mu.Unlock()
	fresh, err := ctrl.ReadRegByte(ctx, 0x39, 0x55)
	if err != nil || fresh != 0xab {
		t.Fatalf("retry after interrupted transfer = (%#x, %v), want 0xab", fresh, err)
	}
}
