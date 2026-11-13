// SPDX-License-Identifier: GPL-3.0-or-later
package sdhci

import (
	"bytes"
	"encoding/binary"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
)

// The card accepts exactly one FIFO sector, then programs independently of the
// host. Its image can change even when the host never gets transfer completion.
type writeCard struct {
	*card
	active        bool
	buffer        [512]byte
	lba           uint32
	written       []uint32
	busyQueries   int
	fault         string
	onWord        func(int)
	statusQueries int
}

func newWriteCard() *writeCard {
	c := newCard()
	c.image = make([]byte, 8192*512)
	c.regs[present] = 1 << 19 // No mechanical write protection.
	return &writeCard{card: c}
}

func (c *writeCard) Write32(reg, value uint32) {
	if reg == dataPort {
		c.accesses++
		if !c.active || c.regs[present]&(1<<10) == 0 || c.words >= 128 {
			panic("write outside writable FIFO")
		}
		binary.LittleEndian.PutUint32(c.buffer[c.words*4:], value)
		c.words++
		if c.onWord != nil {
			c.onWord(c.words)
		}
		if c.words == 128 {
			copy(c.image[int(c.lba)*512:], c.buffer[:])
			c.written = append(c.written, c.lba)
			c.active = false
			c.state = 7
			c.busyQueries = 3
			c.regs[present] &^= 1<<10 | 1<<8 | 2
			if c.fault != "transfer-timeout" {
				c.regs[status] |= 2
			}
			if c.fault == "data-crc" {
				c.regs[status] |= 1<<21 | 1<<15
			}
		}
		return
	}
	if reg == clock && value&(6<<24) != 0 {
		c.active = false // Host reset cannot undo already committed image bytes.
	}
	if reg == command && value>>24 == 24 {
		if c.state != 4 || c.regs[blockSize] != 1<<16|512 || value != 24<<24|0x3a0000 {
			panic("CMD24 outside ready state or wrong PIO direction/geometry")
		}
		c.lba, c.active = c.regs[argument], true
		c.card.Write32(reg, value)
		c.regs[present] |= 1<<10 | 1<<8 | 2
		if c.fault != "fifo-timeout" {
			c.regs[status] |= 1 << 4
		}
		if c.fault == "fifo-disabled" {
			c.regs[present] &^= 1 << 10
		}
		if c.fault == "premature-complete" {
			c.regs[status] |= 2
		}
		return
	}
	if reg == command && value>>24 == 13 && c.state == 7 {
		c.statusQueries++
		c.busyQueries--
		if c.busyQueries == 0 && c.fault != "programming-timeout" {
			c.state = 4
		}
		if c.fault == "wrong-state" {
			c.state = 3
		}
	}
	c.card.Write32(reg, value)
}

func TestPIOWriteReadbackAndReadyBeforeNextSector(t *testing.T) {
	c := newWriteCard()
	d, err := Open(c, Config{InputClockHz: 100000000, AllowWrites: true})
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 1024)
	for i := range data {
		data[i] = byte(i*37 + i/512)
	}
	if err := d.WriteSectors(3, data); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(c.image[3*512:5*512], data) || len(c.written) != 2 || c.written[0] != 3 || c.written[1] != 4 || c.statusQueries != 6 {
		t.Fatal("sector bytes/order or independent card-ready observations differ")
	}
	got := make([]byte, 1024)
	if err := d.ReadSectors(3, got); err != nil || !bytes.Equal(got, data) {
		t.Fatal("readback", err)
	}
	if err := d.Flush(); err != nil {
		t.Fatal(err)
	}
	before := c.accesses
	if err := d.WriteSectors(8192, nil); err != nil || c.accesses != before {
		t.Fatal("empty write touched controller", err)
	}
	for _, request := range []struct {
		lba  uint64
		size int
	}{{8192, 512}, {8191, 1024}, {0, 511}} {
		if err := d.WriteSectors(request.lba, make([]byte, request.size)); err == nil || c.accesses != before {
			t.Fatal("invalid range touched controller", request, err)
		}
	}
}

func TestPIOWriteRequiresSelectedAccessAndWriteProtectInput(t *testing.T) {
	c := newWriteCard()
	d, err := Open(c, Config{InputClockHz: 100000000})
	if err != nil {
		t.Fatal(err)
	}
	before := c.accesses
	if err := d.WriteSectors(0, make([]byte, 512)); !errors.Is(err, block.ErrReadOnly) || c.accesses != before {
		t.Fatal("default read-only access changed", err)
	}
	c = newWriteCard()
	d, err = Open(c, Config{InputClockHz: 100000000, AllowWrites: true})
	if err != nil {
		t.Fatal(err)
	}
	c.regs[present] &^= 1 << 19
	commands := len(c.commands)
	if err := d.WriteSectors(0, make([]byte, 512)); !errors.Is(err, block.ErrReadOnly) || len(c.commands) != commands {
		t.Fatal("protected socket accepted a write command", err)
	}
	if err := d.ReadSectors(0, make([]byte, 512)); err != nil {
		t.Fatal("protection retired healthy reads", err)
	}
}

func TestSocketWithoutWriteProtectStillRequiresExplicitWrites(t *testing.T) {
	for _, allow := range []bool{false, true} {
		c := newWriteCard()
		c.regs[present] &^= 1 << 19
		d, err := Open(c, Config{InputClockHz: 100000000, AllowWrites: allow, NoSocketWriteProtect: true})
		if err != nil {
			t.Fatal(err)
		}
		before := c.accesses
		err = d.WriteSectors(3, bytes.Repeat([]byte{0xad}, 512))
		if !allow {
			if !errors.Is(err, block.ErrReadOnly) || c.accesses != before || len(c.written) != 0 {
				t.Fatal("socket wiring granted write permission", err)
			}
		} else if err != nil || len(c.written) != 1 || c.written[0] != 3 || c.image[3*512] != 0xad {
			t.Fatal("explicit writes on unwired socket failed", err, c.written)
		}
	}
}

func TestCardCSDProtectionBlocksBothSocketPoliciesAndIsRefreshedOnResume(t *testing.T) {
	for _, protection := range []uint32{1 << 13, 1 << 12, 1<<13 | 1<<12} {
		for _, absent := range []bool{false, true} {
			c := newWriteCard()
			c.csdWriteProtect = protection
			d, err := Open(c, Config{InputClockHz: 100000000, AllowWrites: true, NoSocketWriteProtect: absent})
			if err != nil {
				t.Fatal(err)
			}
			commands, accesses := len(c.commands), c.accesses
			if err = d.WriteSectors(3, make([]byte, 512)); !errors.Is(err, block.ErrReadOnly) || len(c.commands) != commands || c.accesses != accesses || len(c.written) != 0 {
				t.Fatal("protected CSD issued a write", protection, absent, err)
			}
			if err = d.ReadSectors(3, make([]byte, 512)); err != nil {
				t.Fatal("card write protection blocked reads", err)
			}
			if err = d.Quiesce(); err != nil {
				t.Fatal(err)
			}
			if protection&(1<<13) != 0 {
				// Permanent protection cannot be cleared on a real card.
				if err = d.Resume(); err != nil {
					t.Fatal(err)
				}
				commands = len(c.commands)
				if err = d.WriteSectors(3, make([]byte, 512)); !errors.Is(err, block.ErrReadOnly) || len(c.commands) != commands {
					t.Fatal("permanent card protection lost on resume", err)
				}
				continue
			}
			c.csdWriteProtect = 0
			if err = d.Resume(); err != nil {
				t.Fatal(err)
			}
			if err = d.WriteSectors(3, make([]byte, 512)); err != nil {
				t.Fatal("removed card write protection was not rediscovered", err)
			}
			if err = d.Quiesce(); err != nil {
				t.Fatal(err)
			}
			c.csdWriteProtect = protection
			if err = d.Resume(); err != nil {
				t.Fatal(err)
			}
			commands = len(c.commands)
			if err = d.WriteSectors(4, make([]byte, 512)); !errors.Is(err, block.ErrReadOnly) || len(c.commands) != commands || len(c.written) != 1 {
				t.Fatal("new card write protection was not rediscovered", err)
			}
		}
	}
}

func TestPIOWriteFailureNeverReplaysOrClaimsRollback(t *testing.T) {
	for _, fault := range []string{"fifo-timeout", "fifo-disabled", "premature-complete", "data-crc", "transfer-timeout", "programming-timeout", "wrong-state", "card-reject", "frozen-clock", "dma-change"} {
		t.Run(fault, func(t *testing.T) {
			c := newWriteCard()
			d, err := Open(c, Config{InputClockHz: 100000000, AllowWrites: true})
			if err != nil {
				t.Fatal(err)
			}
			c.fault = fault
			if fault == "card-reject" {
				c.rejected = true
			}
			if fault == "frozen-clock" {
				c.frozen = true
				c.fault = "programming-timeout"
			}
			if fault == "dma-change" {
				c.regs[command] |= 1
			}
			if err := d.WriteSectors(3, bytes.Repeat([]byte{0xab}, 1024)); err == nil {
				t.Fatal("failed write accepted")
			}
			if len(c.written) > 1 {
				t.Fatal("batch continued after uncertainty")
			}
			if fault == "transfer-timeout" && !bytes.Equal(c.image[3*512:4*512], bytes.Repeat([]byte{0xab}, 512)) {
				t.Fatal("model must preserve possible commit despite lost completion")
			}
			before := c.accesses
			for _, call := range []func() error{
				func() error { return d.WriteSectors(3, make([]byte, 512)) },
				func() error { return d.ReadSectors(3, make([]byte, 512)) },
				d.Flush, d.Quiesce, d.Resume,
			} {
				if err := call(); !errors.Is(err, ErrRetired) || c.accesses != before {
					t.Fatal("retired transport reused", err)
				}
			}
		})
	}
}

func TestPIOWriteQuiesceFinishesCurrentSectorAndFencesRemainder(t *testing.T) {
	c := newWriteCard()
	d, err := Open(c, Config{InputClockHz: 100000000, AllowWrites: true})
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	c.onWord = func(n int) {
		if n == 64 {
			once.Do(func() { close(entered); <-release })
		}
	}
	writeDone, stopDone := make(chan error, 1), make(chan error, 1)
	go func() { writeDone <- d.WriteSectors(3, bytes.Repeat([]byte{0xab}, 1024)) }()
	<-entered
	go func() { stopDone <- d.Quiesce() }()
	deadline := time.Now().Add(time.Second)
	for !d.stopRequested.Load() {
		if time.Now().After(deadline) {
			close(release)
			t.Fatal("quiesce did not fence new sectors")
		}
		runtime.Gosched()
	}
	close(release)
	if err := <-writeDone; !errors.Is(err, ErrQuiesced) {
		t.Fatal(err)
	}
	if err := <-stopDone; err != nil {
		t.Fatal(err)
	}
	if len(c.written) != 1 || c.written[0] != 3 || !bytes.Equal(c.image[4*512:5*512], make([]byte, 512)) {
		t.Fatal("quiesce admitted another sector")
	}
	before := c.accesses
	if err := d.WriteSectors(3, make([]byte, 512)); !errors.Is(err, ErrQuiesced) || c.accesses != before {
		t.Fatal("quiesced write reached controller", err)
	}
	c.onWord = nil
	if err := d.Resume(); err != nil {
		t.Fatal(err)
	}
	if err := d.WriteSectors(4, bytes.Repeat([]byte{0xcd}, 512)); err != nil {
		t.Fatal(err)
	}
}
