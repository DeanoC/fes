// SPDX-License-Identifier: GPL-3.0-or-later
package sdcontrol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/dmaops"
)

const (
	fastCapabilities = 0x240
	fastADMAError    = 0x254
	fastADMAAddrLow  = 0x258
	fastADMAAddrHigh = 0x25c

	fastCapADMA2    = uint32(1 << 19)
	fastCap33V      = uint32(1 << 24)
	fastCap64BitBus = uint32(1 << 28)

	fastHost4Bit    = uint32(1 << 1)
	fastHostADMA264 = uint32(3 << 3)
	fastAttrValid   = uint32(1 << 0)
	fastAttrEnd     = uint32(1 << 1)
	fastAttrTran    = uint32(2 << 4)
)

type testSDCoherency struct{}

func (*testSDCoherency) CacheLineSize() uint64               { return 64 }
func (*testSDCoherency) DataBarrier()                        {}
func (*testSDCoherency) SyncBarrier()                        {}
func (*testSDCoherency) CleanRange(uint64, uint64)           {}
func (*testSDCoherency) InvalidateRange(uint64, uint64)      {}
func (*testSDCoherency) CleanInvalidateRange(uint64, uint64) {}

// fastCard wraps the lease-checked *card model with SCR/4-bit, CMD18/CMD25
// multi-block PIO, and ADMA2 descriptor execution against a backing DMA window.
type fastCard struct {
	*card
	mu        sync.Mutex
	dmaBase   uint64
	dmaRAM    []byte
	scr       [8]byte
	acmd      bool
	busWidth  int
	pioBuf    []byte
	pioPos    int
	pioWrLBA  uint32
	pioWrLeft int
	admaFault bool
	sawCMD18  bool
	sawCMD25  bool
	sawADMA2  bool
}

func wrapFastCard(base *card, sectors int, dmaBase uint64, dmaRAM []byte, caps uint32) *fastCard {
	if base == nil {
		base = newCard()
	}
	base.image = make([]byte, sectors*512)
	for i := range base.image {
		base.image[i] = byte((i*17 + i/512*7) & 0xff)
	}
	base.regs[fastCapabilities] = fastCap33V | caps
	return &fastCard{
		card:     base,
		dmaBase:  dmaBase,
		dmaRAM:   dmaRAM,
		scr:      [8]byte{0x02, 0x35}, // SD_BUS_WIDTHS=5 (1-bit + 4-bit)
		busWidth: 1,
	}
}

func (c *fastCard) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.card.Now()
}

func (c *fastCard) Read32(reg uint32) uint32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if reg == dataPort {
		c.access()
		if c.pioPos+4 > len(c.pioBuf) {
			return 0
		}
		v := binary.LittleEndian.Uint32(c.pioBuf[c.pioPos : c.pioPos+4])
		c.pioPos += 4
		if c.pioPos == len(c.pioBuf) {
			c.regs[status] |= 2
		} else if c.pioPos%512 == 0 {
			c.regs[status] |= 1 << 5
		}
		return v
	}
	return c.card.Read32(reg)
}

func (c *fastCard) Write32(reg, val uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if reg == dataPort && c.pioWrLeft > 0 {
		c.access()
		offset := int(c.pioWrLBA)*512 + c.pioPos
		binary.LittleEndian.PutUint32(c.image[offset:offset+4], val)
		c.pioPos += 4
		if c.pioPos == c.pioWrLeft {
			c.pioWrLeft = 0
			c.regs[present] &^= 1 << 10
			c.regs[status] |= 2
			c.written = append(c.written, c.pioWrLBA)
		} else if c.pioPos%512 == 0 {
			c.regs[status] |= 1 << 4
		}
		return
	}
	if reg == clock {
		c.card.Write32(reg, val)
		return
	}
	if reg != command {
		c.card.Write32(reg, val)
		return
	}
	c.access()
	if !c.prepared {
		panic("SD command before firmware PHY preparation")
	}
	c.commands++
	c.words = 0
	c.regs[reg] = val
	c.regs[response] = 0
	index := val >> 24
	switch index {
	case 0:
		c.acmd = false
		c.state = 0
	case 8:
		c.acmd = false
		c.regs[response] = 0x1aa
	case 55:
		c.acmd = true
		c.regs[response] = 1 << 5
	case 41:
		c.acmd = false
		c.regs[response] = 0xc0300000
	case 2:
		c.acmd = false
		c.state = 2
		c.regs[response] = 0x11223344
		c.regs[response+4] = 0x55667788
		c.regs[response+8] = 0x99aabbcc
		c.regs[response+12] = c.cid
	case 3:
		c.acmd = false
		c.state = 3
		c.regs[response] = 7 << 16
	case 9:
		c.acmd = false
		c.regs[response+12] = 1 << 22
		c.regs[response+8] = 0x32 << 24
		c.regs[response+4] = uint32(len(c.image)/512/1024-1) << 8
	case 7:
		c.acmd = false
		c.state = 4
		c.regs[status] |= 2
	case 13:
		c.acmd = false
		c.regs[response] = uint32(c.state)<<9 | 1<<8
	case 51:
		if !c.acmd {
			panic("CMD51 without CMD55")
		}
		c.acmd = false
		c.pioBuf = append([]byte(nil), c.scr[:]...)
		c.pioPos = 0
		c.regs[status] |= 1 << 5
	case 6:
		if !c.acmd {
			panic("unexpected CMD6 without CMD55")
		}
		c.acmd = false
		if c.regs[argument] == 2 {
			c.busWidth = 4
		}
		c.regs[response] = uint32(c.state)<<9 | 1<<8
	case 17, 18:
		c.acmd = false
		if index == 18 {
			c.sawCMD18 = true
		}
		lba := c.regs[argument]
		blocks := c.regs[blockSize] >> 16
		total := int(blocks) * 512
		if val&1 != 0 {
			c.sawADMA2 = true
			c.runADMA2(lba, total, true)
		} else {
			c.pioBuf = append([]byte(nil), c.image[int(lba)*512:int(lba)*512+total]...)
			c.pioPos = 0
			c.regs[status] |= 1 << 5
		}
	case 24, 25:
		c.acmd = false
		if index == 25 {
			c.sawCMD25 = true
		}
		lba := c.regs[argument]
		blocks := c.regs[blockSize] >> 16
		total := int(blocks) * 512
		if val&1 != 0 {
			c.sawADMA2 = true
			c.runADMA2(lba, total, false)
		} else {
			c.pioWrLBA = lba
			c.pioPos = 0
			c.pioWrLeft = total
			c.regs[present] |= 1 << 10
			c.regs[status] |= 1 << 4
		}
	default:
		panic("unexpected SD command")
	}
	c.regs[status] |= 1
}

func (c *fastCard) runADMA2(lba uint32, total int, isRead bool) {
	if c.admaFault {
		c.regs[fastADMAError] = 0x5
		c.regs[status] |= 1 << 25
		return
	}
	use64 := (c.regs[host] & (3 << 3)) == fastHostADMA264
	descAddr := uint64(c.regs[fastADMAAddrLow]) | uint64(c.regs[fastADMAAddrHigh])<<32
	descOff := descAddr - c.dmaBase
	copied := 0
	imgOff := int(lba) * 512
	for {
		hdr := binary.LittleEndian.Uint32(c.dmaRAM[descOff : descOff+4])
		if hdr&fastAttrValid == 0 || hdr&fastAttrTran == 0 {
			c.regs[fastADMAError] = 0x1
			c.regs[status] |= 1 << 25
			return
		}
		chunk := int(hdr >> 16)
		if chunk == 0 {
			chunk = 65536
		}
		var bufAddr uint64
		if use64 {
			lo := uint64(binary.LittleEndian.Uint32(c.dmaRAM[descOff+4 : descOff+8]))
			hi := uint64(binary.LittleEndian.Uint32(c.dmaRAM[descOff+8 : descOff+12]))
			bufAddr = lo | (hi << 32)
			descOff += 12
		} else {
			bufAddr = uint64(binary.LittleEndian.Uint32(c.dmaRAM[descOff+4 : descOff+8]))
			descOff += 8
		}
		bufOff := bufAddr - c.dmaBase
		if isRead {
			copy(c.dmaRAM[bufOff:bufOff+uint64(chunk)], c.image[imgOff+copied:imgOff+copied+chunk])
		} else {
			copy(c.image[imgOff+copied:imgOff+copied+chunk], c.dmaRAM[bufOff:bufOff+uint64(chunk)])
		}
		copied += chunk
		if hdr&fastAttrEnd != 0 {
			break
		}
	}
	if copied != total {
		c.regs[fastADMAError] = 0x4
		c.regs[status] |= 1 << 25
		return
	}
	c.regs[status] |= 2
}

func TestOwnedFastModesInvalidConfigNeverContactsFirmware(t *testing.T) {
	c := wrapFastCard(nil, 8192, 0, nil, fastCapADMA2)
	cpu := callFunc(func([7]uint64) ([4]uint64, error) {
		t.Fatal("invalid ADMA2 config contacted firmware")
		return [4]uint64{}, nil
	})
	s, err := Open(cpu, c, Config{
		InputClockHz: 200_000_000,
		UseADMA2:     true,
		DMAPool:      nil,
	})
	if err == nil || s != nil || c.accesses != 0 {
		t.Fatalf("got s=%v err=%v accesses=%d, want nil error and 0 MMIO", s, err, c.accesses)
	}
}

func TestOwnedDefaultConfigKeeps1BitSingleSectorPIO(t *testing.T) {
	fw, base := newRPC(t, "")
	s, err := Open(fw, base, Config{InputClockHz: 200_000_000})
	if err != nil {
		t.Fatal(err)
	}
	if s.BusWidth() != 1 || s.ADMA2Active() {
		t.Fatalf("default config width=%d adma=%t want 1/false", s.BusWidth(), s.ADMA2Active())
	}
	var nilSession *Session
	if nilSession.BusWidth() != 0 || nilSession.ADMA2Active() {
		t.Fatal("nil Session returned active transport")
	}
}

func TestOwnedFastModes4BitMultiBlockContainResume(t *testing.T) {
	for _, writable := range []bool{false, true} {
		name := "readonly"
		if writable {
			name = "writable"
		}
		t.Run(name, func(t *testing.T) {
			fw, base := newRPC(t, "")
			c := wrapFastCard(base, 8192, 0, nil, 0)
			cfg := Config{
				InputClockHz:         200_000_000,
				TransferClockHz:      25_000_000,
				BusWidth4Bit:         true,
				MultiBlock:           true,
				NoSocketWriteProtect: true,
			}
			if writable {
				cfg.WriteCard = expectedWriteCard(8192)
			}
			s, err := Open(fw, c, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if s.BusWidth() != 4 || s.ADMA2Active() || c.busWidth != 4 || c.regs[host]&fastHost4Bit == 0 {
				t.Fatalf("unexpected 4-bit mode: session=%d/%t card=%d host=%#x", s.BusWidth(), s.ADMA2Active(), c.busWidth, c.regs[host])
			}
			got := make([]byte, 8*512)
			if err := s.ReadSectors(4, got); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, c.image[4*512:12*512]) || !c.sawCMD18 {
				t.Fatal("4-bit multi-block read mismatch or CMD18 unused")
			}
			wbuf := bytes.Repeat([]byte{0x6b}, 6*512)
			if writable {
				if err := s.WriteSectors(20, wbuf); err != nil {
					t.Fatal(err)
				}
				if !c.sawCMD25 || !bytes.Equal(c.image[20*512:26*512], wbuf) {
					t.Fatal("multi-block write mismatch or CMD25 unused")
				}
			} else if err := s.WriteSectors(20, wbuf); !errors.Is(err, block.ErrReadOnly) {
				t.Fatalf("readonly multi-block write err=%v want ErrReadOnly", err)
			}

			if err := s.Quiesce(); err != nil {
				t.Fatal(err)
			}
			if err := s.Resume(); err != nil {
				t.Fatal(err)
			}
			if s.BusWidth() != 4 || c.busWidth != 4 || c.regs[host]&fastHost4Bit == 0 {
				t.Fatalf("resume lost 4-bit width: session=%d card=%d host=%#x", s.BusWidth(), c.busWidth, c.regs[host])
			}
			gotPost := make([]byte, 6*512)
			if writable {
				if err := s.ReadSectors(20, gotPost); err != nil || !bytes.Equal(gotPost, wbuf) {
					t.Fatalf("post-resume readback failed: err=%v", err)
				}
			} else {
				if err := s.ReadSectors(4, gotPost); err != nil || !bytes.Equal(gotPost, c.image[4*512:10*512]) {
					t.Fatalf("post-resume readback failed: err=%v", err)
				}
			}
			if err := s.Quiesce(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOwnedFastModesADMA2ReadWriteContainResumeAndRetire(t *testing.T) {
	const dmaBase = uint64(0x94800000)
	ram := make([]byte, 128<<10)
	pool, err := dmaops.NewPool(dmaBase, ram, dmaops.Cached, &testSDCoherency{})
	if err != nil {
		t.Fatal(err)
	}
	fw, base := newRPC(t, "")
	c := wrapFastCard(base, 8192, dmaBase, ram, fastCapADMA2)
	s, err := Open(fw, c, Config{
		InputClockHz:         200_000_000,
		TransferClockHz:      25_000_000,
		BusWidth4Bit:         true,
		MultiBlock:           true,
		UseADMA2:             true,
		DMAPool:              pool,
		WriteCard:            expectedWriteCard(8192),
		NoSocketWriteProtect: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.BusWidth() != 4 || !s.ADMA2Active() {
		t.Fatalf("unexpected ADMA2 mode: width=%d adma=%t", s.BusWidth(), s.ADMA2Active())
	}

	got := make([]byte, 16*512)
	if err := s.ReadSectors(2, got); err != nil {
		t.Fatal(err)
	}
	if !c.sawADMA2 || !bytes.Equal(got, c.image[2*512:18*512]) {
		t.Fatal("ADMA2 read mismatch")
	}

	payload := bytes.Repeat([]byte{0x3c}, 12*512)
	if err := s.WriteSectors(30, payload); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(c.image[30*512:42*512], payload) {
		t.Fatal("ADMA2 write mismatch")
	}

	if err := s.Quiesce(); err != nil {
		t.Fatal(err)
	}
	if err := s.Resume(); err != nil {
		t.Fatal(err)
	}
	if s.BusWidth() != 4 || !s.ADMA2Active() {
		t.Fatalf("resume lost ADMA2/4-bit state: width=%d adma=%t", s.BusWidth(), s.ADMA2Active())
	}
	got2 := make([]byte, 12*512)
	if err := s.ReadSectors(30, got2); err != nil || !bytes.Equal(got2, payload) {
		t.Fatalf("post-resume ADMA2 readback err=%v", err)
	}

	c.mu.Lock()
	c.admaFault = true
	c.mu.Unlock()
	if err := s.ReadSectors(0, make([]byte, 4*512)); !errors.Is(err, ErrRetired) {
		t.Fatalf("ADMA fault err=%v want ErrRetired", err)
	}
	if !s.Retired() {
		t.Fatal("expected retired session after ADMA fault")
	}
	if err := s.ReadSectors(0, make([]byte, 512)); !errors.Is(err, ErrRetired) {
		t.Fatalf("post-fault read err=%v want ErrRetired", err)
	}
}
