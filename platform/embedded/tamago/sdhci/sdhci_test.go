// SPDX-License-Identifier: GPL-3.0-or-later
package sdhci

import (
	"bytes"
	"errors"
	"github.com/DeanoC/fes/platform/embedded/tamago/block"
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
}

func newCard() *card {
	return &card{regs: map[uint32]uint32{host: 1 << 8, clock: 7}, tick: time.Unix(1, 0)}
}
func (c *card) Now() time.Time {
	if !c.frozen {
		c.tick = c.tick.Add(time.Millisecond)
	}
	return c.tick
}
func (c *card) Read32(reg uint32) uint32 {
	if reg == dataPort {
		c.words++
		if c.words == 128 && !c.short {
			c.regs[status] |= 2
		}
		return 0x03020100
	}
	return c.regs[reg]
}
func (c *card) Write32(reg, value uint32) {
	if reg == status {
		c.regs[status] &= ^value
		return
	}
	c.regs[reg] = value
	if reg != command {
		return
	}
	c.commands = append(c.commands, value)
	c.words = 0
	c.regs[response] = 4<<9 | 1<<8
	if c.notReady {
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
		c.reads = append(c.reads, c.regs[argument])
		c.regs[status] |= 1 << 5
		if c.crc {
			c.regs[status] |= 1<<21 | 1<<15
		}
	}
}
func TestPIOReadAndOwnership(t *testing.T) {
	c := newCard()
	d, err := Open(c, 8192, 7)
	if err != nil {
		t.Fatal(err)
	}
	var disk block.Device = d
	if disk.SectorCount() != 8192 {
		t.Fatal("firmware capacity not exposed to filesystem")
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
	if c.regs[blockSize] != 1<<16|512 || c.commands[1]&(1<<21|1<<4) != 1<<21|1<<4 || c.commands[1]&1 != 0 {
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
	d, err := Open(c, 16, 7)
	if err != nil {
		t.Fatal(err)
	}
	before := len(c.commands)
	for _, r := range []struct {
		lba  uint64
		n    int
		want error
	}{{16, 512, block.ErrBounds}, {^uint64(0), 512, block.ErrBounds}, {0, 1, block.ErrAlignment}} {
		if err := d.ReadSectors(r.lba, make([]byte, r.n)); !errors.Is(err, r.want) {
			t.Fatal(err)
		}
	}
	if len(c.commands) != before {
		t.Fatal("invalid range touched card")
	}
}
func TestTransferFailurePoisonsDevice(t *testing.T) {
	for _, mode := range []string{"crc", "timeout", "inhibit", "short", "reject", "frozen"} {
		t.Run(mode, func(t *testing.T) {
			c := newCard()
			d, err := Open(c, 8192, 7)
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
func TestHandoffRejected(t *testing.T) {
	for _, mode := range []string{"power", "clock", "ready", "rca", "capacity", "response"} {
		t.Run(mode, func(t *testing.T) {
			c := newCard()
			sectors := uint64(8192)
			rca := uint16(7)
			switch mode {
			case "power":
				c.regs[host] = 0
			case "clock":
				c.regs[clock] = 3
			case "ready":
				c.notReady = true
			case "rca":
				rca = 0
			case "capacity":
				sectors = 1<<32 + 1
			case "response":
				c.rejected = true
			}
			if _, err := Open(c, sectors, rca); err == nil {
				t.Fatal("invalid handoff accepted")
			}
		})
	}
}
