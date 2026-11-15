package sdhci

import (
	"errors"
	"reflect"
	"testing"
)

type clockCard struct {
	*card
	armed  bool
	fault  string
	speed  byte
	issued []struct{ command, clock uint32 }
}

func newClockCard() *clockCard { return &clockCard{card: newCard(), speed: 0x32} }
func (c *clockCard) Write32(reg, value uint32) {
	if reg == clock && value&(7<<24) != 0 {
		c.armed = false
	}
	if reg == clock && c.armed {
		old := c.regs[clock]
		switch c.fault {
		case "card-stop":
			if old&4 != 0 && value&4 == 0 {
				value |= 4
			}
		case "internal-stop":
			if value&7 == 0 {
				value |= 1
			}
		case "divider":
			if value&1 != 0 && value>>8&255 == 4 {
				value ^= 1 << 8
			}
		case "enable":
			if value>>8&255 == 4 && value&4 != 0 {
				value &^= 4
			}
		case "restore-divider":
			if value>>8&255 == 250 && value&1 != 0 {
				value ^= 1 << 8
			}
		}
	}
	c.card.Write32(reg, value)
	if reg == clock && c.armed && c.fault == "stable" && value>>8&255 == 4 {
		c.regs[clock] &^= 2
	}
	if reg != command {
		return
	}
	index := value >> 24
	c.issued = append(c.issued, struct{ command, clock uint32 }{index, c.regs[clock]})
	if index == 9 {
		c.regs[response+8] = uint32(c.speed) << 24
	}
	if index == 13 {
		c.armed = true
		if c.fault == "frozen-after-discovery" {
			c.frozen = true
		}
	}
}

func TestSelectedClockUsesSlowDiscoveryAndRestoresFirmwareHandoff(t *testing.T) {
	c := newClockCard()
	prepared := 0
	config := Config{InputClockHz: 200000000, TransferClockHz: 25000000, PrepareClock: func() error {
		prepared++
		if c.regs[clock]&clockModeMask != 14<<16|250<<8|3 {
			return errors.New("firmware preparation not at identification clock")
		}
		return nil
	}}
	d, err := Open(c, config)
	if err != nil {
		t.Fatal(err)
	}
	if d.dataClockHz != 25000000 || c.regs[clock]&clockModeMask != 14<<16|4<<8|7 {
		t.Fatal("data clock not selected", d.dataClockHz, c.regs[clock])
	}
	for _, event := range c.issued[:len(c.issued)-1] {
		if event.clock&clockModeMask != 14<<16|250<<8|7 {
			t.Fatalf("fast discovery command: %#v", event)
		}
	}
	if event := c.issued[len(c.issued)-1]; event.command != 13 || event.clock&clockModeMask != 14<<16|4<<8|7 {
		t.Fatalf("no fast readiness proof: %#v", event)
	}
	if err = d.ReadSectors(0, make([]byte, 512)); err != nil {
		t.Fatal(err)
	}
	if err = d.Quiesce(); err != nil {
		t.Fatal(err)
	}
	// This is the unchanged secure firmware's exact disabled-clock mask.
	if c.regs[clock]&clockModeMask != 14<<16|250<<8|3 {
		t.Fatal("firmware stop would reject clock", c.regs[clock])
	}
	before := c.accesses
	if !errors.Is(d.ReadSectors(0, make([]byte, 512)), ErrQuiesced) || c.accesses != before {
		t.Fatal("quiesced read touched registers")
	}
	if err = d.Resume(); err != nil {
		t.Fatal(err)
	}
	if prepared != 2 || c.regs[clock]&clockModeMask != 14<<16|4<<8|7 {
		t.Fatal("resume omitted slow preparation/fast selection", prepared, c.regs[clock])
	}
	if err = d.ReadSectors(1, make([]byte, 512)); err != nil {
		t.Fatal(err)
	}
	if err = d.Quiesce(); err != nil {
		t.Fatal(err)
	}
}

func TestSelectedClockFailurePathsNeverFallBackOrReturnSuccess(t *testing.T) {
	for _, fault := range []string{"card-stop", "internal-stop", "divider", "enable", "stable", "frozen-after-discovery"} {
		t.Run(fault, func(t *testing.T) {
			c := newClockCard()
			c.fault = fault
			if d, err := Open(c, Config{InputClockHz: 200000000, TransferClockHz: 25000000}); err == nil || d != nil {
				t.Fatal("failed selected clock accepted", err)
			}
		})
	}
	c := newClockCard()
	c.speed = 0x5a
	if _, err := Open(c, Config{InputClockHz: 200000000, TransferClockHz: 25000000}); err == nil {
		t.Fatal("non-default card speed accepted")
	}
	for _, input := range []uint32{0, 399999, 200000001} {
		c := newClockCard()
		if _, err := Open(c, Config{InputClockHz: input, TransferClockHz: 25000000}); err == nil || c.accesses != 0 {
			t.Fatal("invalid input mutated controller", input)
		}
	}
	for _, rate := range []uint32{1, 400000, 50000000} {
		c := newClockCard()
		if _, err := Open(c, Config{InputClockHz: 200000000, TransferClockHz: rate}); err == nil || c.accesses != 0 {
			t.Fatal("unsupported mode mutated controller", rate)
		}
	}
}

func TestSelectedClockRestorationFailureRetiresAndMediaChangeDoesNotPromote(t *testing.T) {
	for _, fault := range []string{"restore-divider", "card-stop"} {
		t.Run(fault, func(t *testing.T) {
			c := newClockCard()
			d, err := Open(c, Config{InputClockHz: 200000000, TransferClockHz: 25000000})
			if err != nil {
				t.Fatal(err)
			}
			c.fault = fault
			if err = d.Quiesce(); err == nil {
				t.Fatal("failed restore accepted")
			}
			before := c.accesses
			if !errors.Is(d.Resume(), ErrRetired) || !errors.Is(d.ReadSectors(0, make([]byte, 512)), ErrRetired) || c.accesses != before {
				t.Fatal("retired device issued I/O")
			}
		})
	}
	c := newClockCard()
	d, err := Open(c, Config{InputClockHz: 200000000, TransferClockHz: 25000000})
	if err != nil {
		t.Fatal(err)
	}
	if err = d.Quiesce(); err != nil {
		t.Fatal(err)
	}
	c.cid[0] ^= 1
	c.issued = nil
	if !errors.Is(d.Resume(), ErrCardChanged) {
		t.Fatal("changed card adopted")
	}
	for _, event := range c.issued {
		if event.clock>>8&255 != 250 {
			t.Fatal("changed card reached fast clock", event)
		}
	}
}

func TestSelectedClockReadModeMutationIsTerminalAndDefaultIsUnchanged(t *testing.T) {
	for _, reg := range []uint32{clock, host, signalEnable, command} {
		c := newClockCard()
		d, err := Open(c, Config{InputClockHz: 200000000, TransferClockHz: 25000000})
		if err != nil {
			t.Fatal(err)
		}
		if reg == clock {
			c.regs[reg] ^= 1 << 8
		} else {
			c.regs[reg] ^= 1
		}
		if err = d.ReadSectors(0, make([]byte, 512)); err == nil {
			t.Fatal("mutated mode accepted", reg)
		}
		before := c.accesses
		if !errors.Is(d.ReadSectors(0, make([]byte, 512)), ErrRetired) || c.accesses != before {
			t.Fatal("mutated mode retried")
		}
	}
	a, b := newCard(), newClockCard()
	x, e1 := Open(a, Config{InputClockHz: 200000000})
	y, e2 := Open(b, Config{InputClockHz: 200000000})
	if e1 != nil || e2 != nil || x.dataClockHz != 400000 || y.dataClockHz != 400000 || !reflect.DeepEqual(a.commands, b.commands) {
		t.Fatal("default transport changed", e1, e2)
	}
}
