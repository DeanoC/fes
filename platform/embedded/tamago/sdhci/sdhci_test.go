// SPDX-License-Identifier: GPL-3.0-or-later
package sdhci

import (
	"bytes"
	"encoding/binary"
	"errors"
	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"runtime"
	"sync"
	"testing"
	"time"
)

// Fake card/controller boundary: command issue produces independent controller
// events. It never consults driver wait masks or internal device state.
type card struct {
	regs                                          map[uint32]uint32
	words                                         int
	reads                                         []uint32
	commands                                      []uint32
	tick                                          time.Time
	stuck, frozen, crc, short, rejected, notReady bool
	coldFault                                     string
	state                                         int
	acmd                                          bool
	resets                                        int
	image                                         []byte
	readyAfter, powerPolls                        int
	cid                                           [4]uint32
	accesses                                      int
	quiesceFault                                  string
	assignedRCA                                   uint16
}

func newCard() *card {
	return &card{regs: map[uint32]uint32{host: 1 << 8, clock: 7, 0x240: 1 << 24}, tick: time.Unix(1, 0),
		cid: [4]uint32{0xa1b2c300, 0x34567890, 0x54455354, 0x00123456}, assignedRCA: 7}
}
func (c *card) Now() time.Time {
	if !c.frozen {
		c.tick = c.tick.Add(time.Millisecond)
	}
	return c.tick
}
func (c *card) Read32(reg uint32) uint32 {
	c.accesses++
	if reg == dataPort {
		c.words++
		if c.words == 128 && !c.short {
			c.regs[status] |= 2
		}
		if c.image != nil {
			offset := int(c.regs[argument])*512 + (c.words-1)*4
			return binary.LittleEndian.Uint32(c.image[offset : offset+4])
		}
		return 0x03020100
	}
	return c.regs[reg]
}
func (c *card) Write32(reg, value uint32) {
	c.accesses++
	if (c.quiesceFault == "signal-mask" && reg == signalEnable && value == 0) ||
		(c.quiesceFault == "status-mask" && reg == statusEnable && value == 0) {
		return
	}
	if reg == status {
		c.regs[status] &= ^value
		return
	}
	if reg == clock {
		stopping := c.regs[clock]&4 != 0 && value&4 == 0
		if stopping && c.quiesceFault == "clock-stop" {
			value |= 4
		}
		if stopping && c.quiesceFault == "late-busy" {
			c.regs[present] |= 1 << 9
		}
		if stopping && c.quiesceFault == "late-error" {
			c.regs[status] |= 1 << 15
		}
		if value&(7<<24) != 0 {
			c.resets++
			if c.coldFault == "reset" {
				c.regs[reg] = value
				return
			}
		}
		c.regs[reg] = value &^ (7 << 24)
		if value&1 != 0 && c.coldFault != "clock" {
			c.regs[reg] |= 2
		}
		return
	}
	c.regs[reg] = value
	if reg != command {
		return
	}
	c.commands = append(c.commands, value)
	c.words = 0
	c.regs[response] = 0
	index := value >> 24
	switch index {
	case 0:
		c.state = 0
	case 8:
		c.regs[response] = c.regs[argument] & 0xfff
		if c.coldFault == "echo" {
			c.regs[response] = 0x155
		}
	case 55:
		c.acmd = true
		c.regs[response] = 1 << 5
		if c.coldFault == "app" {
			c.regs[response] = 0
		}
	case 41:
		if !c.acmd {
			panic("ACMD41 without CMD55")
		}
		c.acmd = false
		c.powerPolls++
		if c.regs[argument] != 1<<30|0x00300000 {
			panic("invalid ACMD41 argument")
		}
		c.regs[response] = 0xc0300000
		if c.powerPolls < c.readyAfter {
			c.regs[response] &^= 1 << 31
		}
		if c.coldFault == "powerup" {
			c.regs[response] = 0x40300000
		}
		if c.coldFault == "ocr" {
			c.regs[response] = 0xc0008000
		}
		if c.coldFault == "sdsc" {
			c.regs[response] = 0x80300000
		}
	case 2:
		c.state = 2
		for i, value := range c.cid {
			if c.coldFault == "cid-zero" {
				value = 0
			}
			if c.coldFault == "cid-ones" {
				value = ^uint32(0)
			}
			c.regs[response+uint32(i)*4] = value
		}
	case 3:
		if c.state != 2 {
			panic("CMD3 without identification")
		}
		c.state = 3
		c.regs[response] = uint32(c.assignedRCA) << 16
		if c.coldFault == "r6err" {
			c.regs[response] |= 0x8000
		}
		if c.coldFault == "rca" {
			c.regs[response] = 0
		}
	case 9:
		if c.state != 3 || c.regs[argument] != uint32(c.assignedRCA)<<16 {
			panic("CSD outside standby/address")
		}
		c.regs[response+12] = 1 << 22
		c.regs[response+4] = 7 << 8 // 8192 sectors
		if c.image != nil {
			c.regs[response+4] = uint32(len(c.image)/512/1024-1) << 8
		}
		if c.coldFault == "max" {
			c.regs[response+4] = 0x3fffff << 8
		}
		if c.coldFault == "csd3" {
			c.regs[response+12] = 2 << 22
		}
		if c.coldFault == "csd" {
			c.regs[response+12] = 0
		}
	case 7:
		if c.state != 3 || c.regs[argument] != uint32(c.assignedRCA)<<16 {
			panic("select outside standby")
		}
		c.state = 4
		c.regs[status] |= 2
	case 13:
		if c.regs[argument] != uint32(c.assignedRCA)<<16 {
			panic("status query used stale RCA")
		}
		c.regs[response] = uint32(c.state)<<9 | 1<<8
		if c.quiesceFault == "clock-change" {
			c.regs[clock] &^= 2
		}
		if c.quiesceFault == "dma-change" {
			c.regs[command] |= 1
		}
		if c.quiesceFault == "busy-after-ready" {
			c.regs[present] |= 1 << 9
		}
	case 17:
		if c.state != 4 {
			panic("read outside transfer")
		}
	}
	if c.notReady && index == 13 {
		c.regs[response] = 3 << 9
	}
	if c.rejected {
		c.regs[response] |= 1 << 30
	}
	if c.stuck {
		return
	}
	c.regs[status] |= 1
	if value>>24 == 17 {
		if c.coldFault == "removed" {
			c.regs[status] = 1<<16 | 1<<15
			return
		}
		c.reads = append(c.reads, c.regs[argument])
		c.regs[status] |= 1 << 5
		if c.crc {
			c.regs[status] |= 1<<21 | 1<<15
		}
	}
}
func TestPIOReadAndOwnership(t *testing.T) {
	c := newCard()
	d, err := Open(c, Config{InputClockHz: 100000000})
	if err != nil {
		t.Fatal(err)
	}
	var disk block.Device = d
	if disk.SectorCount() != 8192 {
		t.Fatal("discovered capacity not exposed to filesystem")
	}
	data := make([]byte, 1024)
	if err = d.ReadSectors(12, data); err != nil {
		t.Fatal(err)
	}
	if len(c.reads) != 2 || c.reads[0] != 12 || c.reads[1] != 13 {
		t.Fatalf("wrong block arguments: %v", c.reads)
	}
	if !bytes.Equal(data, bytes.Repeat([]byte{0, 1, 2, 3}, 256)) {
		t.Fatal("PIO data corrupt")
	}
	if c.regs[blockSize] != 1<<16|512 || c.commands[len(c.commands)-1]&(1<<21|1<<4) != 1<<21|1<<4 || c.commands[len(c.commands)-1]&1 != 0 {
		t.Fatal("not single-sector PIO read")
	}
	if c.regs[signalEnable] != 0 {
		t.Fatal("IRQ signals enabled")
	}
	if err = d.WriteSectors(0, make([]byte, 512)); !errors.Is(err, block.ErrReadOnly) {
		t.Fatal(err)
	}
	if err = d.Flush(); err != nil {
		t.Fatal(err)
	}
}
func TestBoundsBeforeMMIO(t *testing.T) {
	c := newCard()
	d, err := Open(c, Config{InputClockHz: 100000000})
	if err != nil {
		t.Fatal(err)
	}
	before := len(c.commands)
	for _, r := range []struct {
		lba  uint64
		n    int
		want error
	}{{8192, 512, block.ErrBounds}, {^uint64(0), 512, block.ErrBounds}, {0, 1, block.ErrAlignment}} {
		if err := d.ReadSectors(r.lba, make([]byte, r.n)); !errors.Is(err, r.want) {
			t.Fatal(err)
		}
	}
	if len(c.commands) != before {
		t.Fatal("invalid range touched card")
	}
}
func TestTransferFailurePoisonsDevice(t *testing.T) {
	for _, mode := range []string{"crc", "timeout", "inhibit", "short", "reject", "frozen", "removed"} {
		t.Run(mode, func(t *testing.T) {
			c := newCard()
			d, err := Open(c, Config{InputClockHz: 100000000})
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "crc":
				c.crc = true
			case "timeout":
				c.stuck = true
			case "inhibit":
				c.regs[present] = 3
			case "short":
				c.short = true
			case "reject":
				c.rejected = true
			case "removed":
				c.coldFault = "removed"
			case "frozen":
				c.stuck = true
				c.frozen = true
			}
			dst := bytes.Repeat([]byte{0xaa}, 512)
			if err = d.ReadSectors(0, dst); err == nil {
				t.Fatal("failed transfer accepted")
			}
			if !bytes.Equal(dst, bytes.Repeat([]byte{0xaa}, 512)) {
				t.Fatal("uncertain sector exposed")
			}
			commands := len(c.commands)
			if err = d.ReadSectors(0, dst); err == nil {
				t.Fatal("failed card reused")
			}
			if err = d.Flush(); err == nil {
				t.Fatal("failed card flushed")
			}
			if len(c.commands) != commands {
				t.Fatal("poisoned driver touched card")
			}
		})
	}
}
func TestColdStartupRejected(t *testing.T) {
	for _, mode := range []string{"reset", "clock", "power", "echo", "app", "powerup", "sdsc", "ocr", "rca", "r6err", "csd", "csd3", "ready", "response", "cid-zero", "cid-ones"} {
		t.Run(mode, func(t *testing.T) {
			c := newCard()
			c.coldFault = mode
			if mode == "power" {
				c.regs[0x240] = 0
			}
			if mode == "ready" {
				c.notReady = true
			}
			if mode == "response" {
				c.rejected = true
			}
			if _, err := Open(c, Config{InputClockHz: 100000000}); err == nil {
				t.Fatal("invalid startup accepted")
			}
		})
	}
	for _, frequency := range []uint32{0, 399999, 200000001} {
		if _, err := Open(newCard(), Config{InputClockHz: frequency}); err == nil {
			t.Fatal("invalid clock accepted")
		}
	}
}
func TestColdReadRecoveryRequiresRemount(t *testing.T) {
	c := newCard()
	d, err := Open(c, Config{InputClockHz: 100000000})
	if err != nil {
		t.Fatal(err)
	}
	c.crc = true
	if err = d.ReadSectors(0, make([]byte, 512)); err == nil {
		t.Fatal("CRC accepted")
	}
	if c.resets < 2 {
		t.Fatal("failed controller not contained")
	}
	c.crc = false
	if err = d.ReadSectors(0, make([]byte, 512)); err == nil {
		t.Fatal("old filesystem allowed after error")
	}
	replacement, err := Open(c, Config{InputClockHz: 100000000})
	if err != nil {
		t.Fatal(err)
	}
	if err = replacement.ReadSectors(0, make([]byte, 512)); err != nil {
		t.Fatal(err)
	}
}
func TestIdentificationClockAndResponseTypes(t *testing.T) {
	c := newCard()
	_, err := Open(c, Config{InputClockHz: 100000001})
	if err != nil {
		t.Fatal(err)
	}
	if (c.regs[clock]>>8)&255 != 126 {
		t.Fatal("clock divider rounded up past 400 kHz")
	}
	want := []uint32{0, 8, 55, 41, 2, 3, 9, 7, 13}
	if len(c.commands) != len(want) {
		t.Fatal(c.commands)
	}
	for i, cmd := range c.commands {
		if cmd>>24 != want[i] {
			t.Fatal("startup order", c.commands)
		}
		if cmd&1 != 0 {
			t.Fatal("DMA enabled")
		}
	}
	if c.commands[3]&(0x1f<<16) != 2<<16 {
		t.Fatal("OCR configured with CRC/index checks")
	}
	if c.commands[4]&(0x1f<<16) != 9<<16 {
		t.Fatal("R2 configured incorrectly")
	}
	if c.regs[host]&((1<<1)|(1<<2)|(1<<5)) != 0 {
		t.Fatal("high speed/wide bus enabled")
	}
}

func TestMaximumSDXCCapacity(t *testing.T) {
	c := newCard()
	c.coldFault = "max"
	d, err := Open(c, Config{InputClockHz: 100000000})
	if err != nil {
		t.Fatal(err)
	}
	if d.SectorCount() != 1<<32 {
		t.Fatal("SDXC capacity overflow")
	}
	if err = d.ReadSectors(1<<32-1, make([]byte, 512)); err != nil {
		t.Fatal(err)
	}
	if len(c.reads) != 1 || c.reads[0] != 0xffffffff {
		t.Fatal("last SDXC sector truncated")
	}
	if err = d.ReadSectors(1<<32, make([]byte, 512)); !errors.Is(err, block.ErrBounds) {
		t.Fatal(err)
	}
}

func TestDelayedCardPowerUp(t *testing.T) {
	c := newCard()
	c.readyAfter = 4
	d, err := Open(c, Config{InputClockHz: 100000000})
	if err != nil {
		t.Fatal(err)
	}
	if c.powerPolls != 4 || d.SectorCount() != 8192 {
		t.Fatal("power-up loop did not finish discovery")
	}
}

func assertRetiredNoMMIO(t *testing.T, d *Device, c *card) {
	t.Helper()
	before := c.accesses
	for _, operation := range []func() error{func() error { return d.ReadSectors(0, make([]byte, 512)) }, d.Flush, d.Quiesce, d.Resume} {
		if err := operation(); !errors.Is(err, ErrRetired) {
			t.Fatalf("retired operation: %v", err)
		}
	}
	if c.accesses != before {
		t.Fatal("retired device touched MMIO")
	}
}

func TestQuiesceAndFreshControllerResume(t *testing.T) {
	c := newCard()
	d, err := Open(c, Config{InputClockHz: 200000000})
	if err != nil {
		t.Fatal(err)
	}
	before := c.accesses
	if err = d.Resume(); err == nil || c.accesses != before {
		t.Fatal("active device reinitialized")
	}
	id := d.Identity()
	wantCID := [15]byte{0x12, 0x34, 0x56, 0x54, 0x45, 0x53, 0x54, 0x34, 0x56, 0x78, 0x90, 0xa1, 0xb2, 0xc3, 0}
	if id.CID != wantCID || id.Sectors != 8192 {
		t.Fatalf("R2 CID/geometry: %+v", id)
	}
	id.CID[0] ^= 0xff
	if d.Identity().CID != wantCID {
		t.Fatal("identity getter aliases stored identity")
	}
	if err = d.Quiesce(); err != nil {
		t.Fatal(err)
	}
	if c.regs[clock]&7 != 3 || c.regs[signalEnable] != 0 || c.regs[statusEnable] != 0 {
		t.Fatal("card clock or interrupts remain active")
	}
	before = c.accesses
	for _, operation := range []func() error{func() error { return d.ReadSectors(0, make([]byte, 512)) }, d.Flush, d.Quiesce} {
		if err = operation(); !errors.Is(err, ErrQuiesced) {
			t.Fatalf("quiesced operation: %v", err)
		}
	}
	if c.accesses != before {
		t.Fatal("quiesced device touched MMIO")
	}
	// Model a fresh controller after the platform reset/regrant. Card identity
	// survives; no SDHCI configuration is carried over from the earlier run.
	c.regs = map[uint32]uint32{0x240: 1 << 24}
	c.assignedRCA = 11
	c.cid[3] |= 0xa5000000 // SDHCI's reserved R2 high byte is not CID identity.
	if err = d.Resume(); err != nil {
		t.Fatal(err)
	}
	if d.Identity().CID != wantCID || d.Identity().Sectors != 8192 {
		t.Fatal("identity changed on resume")
	}
	if err = d.ReadSectors(7, make([]byte, 512)); err != nil {
		t.Fatal(err)
	}
	if d.rca != 11 {
		t.Fatal("new card address was not adopted")
	}
	if err = d.Flush(); err != nil {
		t.Fatal(err)
	}
}

func TestResumeRejectsChangedCardAndCapacity(t *testing.T) {
	for _, change := range []string{"cid", "capacity"} {
		t.Run(change, func(t *testing.T) {
			c := newCard()
			d, err := Open(c, Config{InputClockHz: 100000000})
			if err != nil {
				t.Fatal(err)
			}
			original := d.Identity()
			if err = d.Quiesce(); err != nil {
				t.Fatal(err)
			}
			if change == "cid" {
				c.cid[0] ^= 0x100
			} else {
				c.coldFault = "max"
			}
			if err = d.Resume(); !errors.Is(err, ErrCardChanged) {
				t.Fatalf("changed media accepted: %v", err)
			}
			if d.Identity() != original || len(c.reads) != 0 {
				t.Fatal("replacement published or read as the old card")
			}
			assertRetiredNoMMIO(t, d, c)
		})
	}
}

func TestResumeFailureRetiresDevice(t *testing.T) {
	for _, fault := range []string{"reset", "clock", "echo", "cid-zero", "cid-ones", "r6err", "csd3"} {
		t.Run(fault, func(t *testing.T) {
			c := newCard()
			d, err := Open(c, Config{InputClockHz: 100000000})
			if err != nil {
				t.Fatal(err)
			}
			if err = d.Quiesce(); err != nil {
				t.Fatal(err)
			}
			c.coldFault = fault
			if err = d.Resume(); err == nil {
				t.Fatal("failed initialization resumed")
			}
			assertRetiredNoMMIO(t, d, c)
		})
	}
}

func TestQuiesceFailureRetiresDevice(t *testing.T) {
	for _, fault := range []string{"dma", "clock-off", "pending-error", "busy-before", "signal-mask", "status-mask", "clock-stop", "late-busy", "late-error", "clock-change", "dma-change", "busy-after-ready", "card-not-ready"} {
		t.Run(fault, func(t *testing.T) {
			c := newCard()
			d, err := Open(c, Config{InputClockHz: 100000000})
			if err != nil {
				t.Fatal(err)
			}
			c.quiesceFault = fault
			switch fault {
			case "dma":
				c.regs[command] |= 1
			case "clock-off":
				c.regs[clock] &^= 4
			case "pending-error":
				c.regs[status] |= 1 << 15
			case "busy-before":
				c.regs[present] |= 1 << 9
			case "signal-mask":
				c.regs[signalEnable] = 1
			case "card-not-ready":
				c.notReady = true
			}
			if err = d.Quiesce(); err == nil {
				t.Fatal("uncertain stop accepted")
			}
			if fault == "dma-change" && c.regs[clock]&4 == 0 {
				t.Fatal("card clock stopped after DMA ownership changed")
			}
			assertRetiredNoMMIO(t, d, c)
		})
	}
}

type heldSector struct {
	Registers
	once             sync.Once
	entered, release chan struct{}
}

func (h *heldSector) Read32(reg uint32) uint32 {
	if reg == dataPort {
		h.once.Do(func() { close(h.entered); <-h.release })
	}
	return h.Registers.Read32(reg)
}

func TestQuiesceFencesQueuedReadsAndStopsBatchAtSectorBoundary(t *testing.T) {
	c := newCard()
	held := &heldSector{Registers: c, entered: make(chan struct{}), release: make(chan struct{})}
	d, err := Open(held, Config{InputClockHz: 100000000})
	if err != nil {
		t.Fatal(err)
	}
	dst := bytes.Repeat([]byte{0xaa}, 1024)
	readDone := make(chan error, 1)
	go func() { readDone <- d.ReadSectors(0, dst) }()
	<-held.entered
	quiesceDone := make(chan error, 1)
	go func() { quiesceDone <- d.Quiesce() }()
	deadline := time.Now().Add(time.Second)
	for !d.stopRequested.Load() {
		if time.Now().After(deadline) {
			close(held.release)
			t.Fatal("quiesce did not fence admission")
		}
		runtime.Gosched()
	}
	queuedDone := make(chan error, 1)
	go func() { queuedDone <- d.ReadSectors(2, make([]byte, 512)) }()
	select {
	case err := <-quiesceDone:
		close(held.release)
		t.Fatalf("stopped before current sector completed: %v", err)
	default:
	}
	close(held.release)
	if err = <-readDone; !errors.Is(err, ErrQuiesced) {
		t.Fatalf("batch did not stop: %v", err)
	}
	if err = <-queuedDone; !errors.Is(err, ErrQuiesced) {
		t.Fatalf("queued read admitted: %v", err)
	}
	if err = <-quiesceDone; err != nil {
		t.Fatal(err)
	}
	if len(c.reads) != 1 || c.reads[0] != 0 {
		t.Fatal("another sector issued after stop request", c.reads)
	}
	if !bytes.Equal(dst[:512], bytes.Repeat([]byte{0, 1, 2, 3}, 128)) || !bytes.Equal(dst[512:], bytes.Repeat([]byte{0xaa}, 512)) {
		t.Fatal("sector boundary or unfinished output changed")
	}
}
