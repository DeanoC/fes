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
	for _, rate := range []uint32{1, 400000, 75000000} {
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

type highSpeedCard struct {
	*clockCard
	scr          [8]byte
	busWidth     int
	acmd         bool
	pioBuf       []byte
	pioPos       int
	cmd6Func1OK  bool
	cmd6SwitchOK bool
	cmd6MaxCurr  uint16
	rejectHSHost bool
	acmd51Count  int
	cmd6Count    int
}

func newHighSpeedCard() *highSpeedCard {
	base := newClockCard()
	base.regs[capabilities] = cap33V | capHighSpeed
	return &highSpeedCard{
		clockCard:    base,
		scr:          [8]byte{0x02, 0x35}, // SD_SPEC=2 (>=1.10) + SD_BUS_WIDTHS=5 (1-bit + 4-bit)
		busWidth:     1,
		cmd6Func1OK:  true,
		cmd6SwitchOK: true,
		cmd6MaxCurr:  200,
	}
}

func (c *highSpeedCard) Read32(reg uint32) uint32 {
	if reg == dataPort && len(c.pioBuf) > 0 {
		c.accesses++
		v := uint32(0)
		if c.pioPos+4 <= len(c.pioBuf) {
			v = uint32(c.pioBuf[c.pioPos]) | uint32(c.pioBuf[c.pioPos+1])<<8 |
				uint32(c.pioBuf[c.pioPos+2])<<16 | uint32(c.pioBuf[c.pioPos+3])<<24
		}
		c.pioPos += 4
		if c.pioPos == len(c.pioBuf) {
			c.regs[status] |= 2
			c.pioBuf = nil
			c.pioPos = 0
		} else if c.pioPos%512 == 0 {
			c.regs[status] |= 1 << 5
		}
		return v
	}
	return c.clockCard.Read32(reg)
}

func (c *highSpeedCard) Write32(reg, value uint32) {
	if reg == host && c.rejectHSHost && value&hostHighSpeed != 0 {
		c.regs[host] = value &^ hostHighSpeed
		return
	}
	if reg == command {
		index := value >> 24
		if index == 51 {
			if !c.acmd {
				panic("CMD51 without CMD55")
			}
			c.acmd = false
			c.acmd51Count++
			c.issued = append(c.issued, struct{ command, clock uint32 }{index, c.regs[clock]})
			c.commands = append(c.commands, value)
			c.pioBuf = append([]byte(nil), c.scr[:]...)
			c.pioPos = 0
			c.regs[status] |= 1<<5 | 1
			return
		}
		if index == 6 {
			if c.acmd {
				c.acmd = false
				if c.regs[argument] == 2 {
					c.busWidth = 4
				}
				c.issued = append(c.issued, struct{ command, clock uint32 }{index, c.regs[clock]})
				c.commands = append(c.commands, value)
				c.regs[response] = 4<<9 | 1<<8
				c.regs[status] |= 1
				return
			}
			c.cmd6Count++
			c.issued = append(c.issued, struct{ command, clock uint32 }{index, c.regs[clock]})
			c.commands = append(c.commands, value)
			var st [64]byte
			arg := c.regs[argument]
			st[0] = byte(c.cmd6MaxCurr >> 8)
			st[1] = byte(c.cmd6MaxCurr)
			if c.cmd6Func1OK {
				st[13] = 1 << 1 // Group 1 Function 1 supported
			} else {
				st[13] = 1 << 0
			}
			if arg>>31 == 0 {
				if c.cmd6Func1OK {
					st[16] = 1
				} else {
					st[16] = 0x0f
				}
			} else {
				if c.cmd6SwitchOK {
					st[16] = 1
				} else {
					st[16] = 0x0f
				}
			}
			c.pioBuf = append([]byte(nil), st[:]...)
			c.pioPos = 0
			c.regs[response] = 4<<9 | 1<<8
			c.regs[status] |= 1<<5 | 1
			return
		}
		if index == 55 {
			c.acmd = true
		} else {
			c.acmd = false
		}
	}
	c.clockCard.Write32(reg, value)
}

func TestHighSpeed50MHzSwitchContainAndResume(t *testing.T) {
	c := newHighSpeedCard()
	d, err := Open(c, Config{
		InputClockHz:    200000000,
		TransferClockHz: 50000000,
		BusWidth4Bit:    true,
		MultiBlock:      true,
	})
	if err != nil {
		t.Fatalf("Open HighSpeed 50MHz: %v", err)
	}
	if !d.HighSpeed() || d.TransferClockHz() != 50000000 || d.BusWidth() != 4 {
		t.Fatalf("mode=(hs=%t hz=%d width=%d), want (true, 50000000, 4)", d.HighSpeed(), d.TransferClockHz(), d.BusWidth())
	}
	if got := c.regs[clock] & clockModeMask; got != 14<<16|2<<8|7 {
		t.Fatalf("clock=%#x want %#x", got, uint32(14<<16|2<<8|7))
	}
	if c.regs[host]&(host4BitWidth|hostHighSpeed) != host4BitWidth|hostHighSpeed {
		t.Fatalf("host=%#x missing 4-bit or High-Speed bit", c.regs[host])
	}
	if c.acmd51Count != 1 || c.cmd6Count != 2 {
		t.Fatalf("expected 1 ACMD51 (cached across 4-bit + HS) and 2 CMD6 (check+switch), got %d/%d", c.acmd51Count, c.cmd6Count)
	}

	if err := d.ReadSectors(0, make([]byte, 512)); err != nil {
		t.Fatal(err)
	}
	if err := d.Quiesce(); err != nil {
		t.Fatal(err)
	}
	if c.regs[clock]&clockModeMask != 14<<16|250<<8|3 {
		t.Fatalf("quiesce did not restore ident clock: %#x", c.regs[clock])
	}
	if err := d.Resume(); err != nil {
		t.Fatal(err)
	}
	if !d.HighSpeed() || d.TransferClockHz() != 50000000 || c.regs[clock]&clockModeMask != 14<<16|2<<8|7 {
		t.Fatalf("resume did not restore 50MHz High-Speed: hs=%t hz=%d clock=%#x", d.HighSpeed(), d.TransferClockHz(), c.regs[clock])
	}
	if err := d.ReadSectors(1, make([]byte, 512)); err != nil {
		t.Fatal(err)
	}
	if err := d.Quiesce(); err != nil {
		t.Fatal(err)
	}
}

func TestHighSpeed50MHzFailurePathsRejectWithoutFallback(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*highSpeedCard)
	}{
		{name: "no-host-cap", setup: func(c *highSpeedCard) { c.regs[capabilities] = cap33V }},
		{name: "sd-v1.0-scr", setup: func(c *highSpeedCard) { c.scr[0] = 0x00 }},
		{name: "cmd6-check-unsupported", setup: func(c *highSpeedCard) { c.cmd6Func1OK = false }},
		{name: "cmd6-zero-current", setup: func(c *highSpeedCard) { c.cmd6MaxCurr = 0 }},
		{name: "cmd6-exceeds-current-limit", setup: func(c *highSpeedCard) { c.cmd6MaxCurr = 400 }},
		{name: "cmd6-switch-rejected", setup: func(c *highSpeedCard) { c.cmd6SwitchOK = false }},
		{name: "host-readback-mismatch", setup: func(c *highSpeedCard) { c.rejectHSHost = true }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newHighSpeedCard()
			tc.setup(c)
			if d, err := Open(c, Config{InputClockHz: 200000000, TransferClockHz: 50000000}); err == nil || d != nil {
				t.Fatalf("%s unexpectedly succeeded: d=%v err=%v", tc.name, d, err)
			}
		})
	}
	// Explicit higher board current budget admits a 400 mA High-Speed card.
	cHigh := newHighSpeedCard()
	cHigh.cmd6MaxCurr = 400
	dHigh, err := Open(cHigh, Config{InputClockHz: 200000000, TransferClockHz: 50000000, HighSpeedMaxCurrentMA: 400})
	if err != nil || !dHigh.HighSpeed() {
		t.Fatalf("explicit 400mA budget failed: d=%v err=%v", dHigh, err)
	}
	_ = dHigh.Quiesce()
	c := newHighSpeedCard()
	if d, err := Open(c, Config{InputClockHz: 25000000, TransferClockHz: 50000000}); err == nil || d != nil || c.accesses != 0 {
		t.Fatalf("input<transfer touched hardware: d=%v err=%v accesses=%d", d, err, c.accesses)
	}
}
