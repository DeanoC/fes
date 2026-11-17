// SPDX-License-Identifier: GPL-3.0-or-later
package sdcontrol

import (
	"bytes"
	"errors"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
)

// Model the card's actual default-speed declaration and clock register faults.
// The underlying card still checks every access against the real C lease.
type clockedCard struct {
	*card
	speed  byte
	fault  string
	armed  bool
	issued []struct{ command, clock uint32 }
}

func (c *clockedCard) Write32(reg, value uint32) {
	if reg == clock && value&(1<<24) != 0 {
		c.armed = false
	}
	if reg == clock && c.armed {
		switch c.fault {
		case "divider":
			if value>>8&255 == 4 && value&1 != 0 {
				value ^= 1 << 8
			}
		case "restore":
			if value>>8&255 == 250 && value&1 != 0 {
				value ^= 1 << 8
			}
		}
	}
	c.card.Write32(reg, value)
	if reg == clock && c.armed && c.fault == "stable" && value>>8&255 == 4 {
		c.regs[clock] &^= 2
	}
	if reg == command {
		index := value >> 24
		c.issued = append(c.issued, struct{ command, clock uint32 }{index, c.regs[clock]})
		if index == 9 {
			c.regs[response+8] = uint32(c.speed) << 24
		}
		if index == 13 {
			c.armed = true
		}
	}
}

func TestOwnedTransferClockReadWriteContainResumeUsesActualFirmware(t *testing.T) {
	for _, writable := range []bool{false, true} {
		t.Run(map[bool]string{false: "readonly", true: "writable"}[writable], func(t *testing.T) {
			fw, base := newRPC(t, "")
			c := &clockedCard{card: base, speed: 0x32}
			cfg := Config{InputClockHz: 200000000, TransferClockHz: 25000000, NoSocketWriteProtect: true}
			if writable {
				cfg.WriteCard = expectedWriteCard(8192)
			}
			s, err := Open(fw, c, cfg)
			if err != nil {
				t.Fatal(err)
			}
			fast, slow := uint32(14<<16|4<<8|7), uint32(14<<16|250<<8|3)
			if c.regs[clock] != fast {
				t.Fatal("fast transfer not selected", c.regs[clock])
			}
			for _, event := range c.issued[:len(c.issued)-1] {
				if event.clock != slow|4 {
					t.Fatal("fast discovery", event)
				}
			}
			data := bytes.Repeat([]byte{0x5a}, 512)
			if writable {
				if err = s.WriteSectors(3, data); err != nil {
					t.Fatal(err)
				}
			} else if err = s.WriteSectors(3, data); !errors.Is(err, block.ErrReadOnly) || len(c.written) != 0 {
				t.Fatal("clock granted write authority", err)
			}
			if err = s.ReadSectors(3, make([]byte, 512)); err != nil {
				t.Fatal(err)
			}
			for cycle := 0; cycle < 2; cycle++ {
				if err = s.Quiesce(); err != nil {
					t.Fatal("actual secure stop rejected restored divider", err)
				}
				if c.regs[clock] != slow {
					t.Fatal("firmware handoff divider differs", c.regs[clock])
				}
				r, ok := s.LastReceipt()
				if !ok || r.Active {
					t.Fatal(r, ok)
				}
				if cycle == 0 {
					if err = s.Resume(); err != nil {
						t.Fatal(err)
					}
					if c.regs[clock] != fast {
						t.Fatal("resume lost rate")
					}
					got := make([]byte, 512)
					if err = s.ReadSectors(3, got); err != nil {
						t.Fatal(err)
					}
					if writable && !bytes.Equal(got, data) {
						t.Fatal("resume changed file bytes")
					}
				}
			}
			if fw.stats[3] != 2 {
				t.Fatal("missing independent secure stops", fw.stats)
			}
		})
	}
}

func TestOwnedTransferClockInvalidSelectionNeverContactsFirmware(t *testing.T) {
	for _, cfg := range []Config{{InputClockHz: 200000000, TransferClockHz: 1}, {InputClockHz: 200000000, TransferClockHz: 50000000}, {InputClockHz: 400000, TransferClockHz: 25000000}} {
		c := newCard()
		cpu := callFunc(func([7]uint64) ([4]uint64, error) {
			t.Fatal("invalid rate contacted firmware")
			return [4]uint64{}, nil
		})
		if s, err := Open(cpu, c, cfg); err == nil || s != nil || c.accesses != 0 {
			t.Fatal(s, err, c.accesses)
		}
	}
}

func TestOwnedTransferClockFailureRetiresWithoutStopRetryOrSlowFallback(t *testing.T) {
	for _, fault := range []string{"speed", "divider", "stable", "restore", "replacement", "resume-divider"} {
		t.Run(fault, func(t *testing.T) {
			fw, base := newRPC(t, "")
			c := &clockedCard{card: base, speed: 0x32}
			if fault == "speed" {
				c.speed = 0x5a
			}
			if fault == "divider" || fault == "stable" {
				c.fault = fault
			}
			s, err := Open(fw, c, Config{InputClockHz: 200000000, TransferClockHz: 25000000, WriteCard: expectedWriteCard(8192)})
			if err == nil {
				if fault == "restore" {
					c.fault = "restore"
				}
				err = s.Quiesce()
				if err == nil {
					if fault == "replacement" {
						c.cid++
					} else if fault == "resume-divider" {
						c.fault = "divider"
					}
					err = s.Resume()
				}
			}
			if s == nil || !errors.Is(err, ErrRetired) {
				t.Fatal("clock failure not retired", s, err)
			}
			if len(c.written) != 0 {
				t.Fatal("failure wrote media")
			}
			_, valid := s.LastReceipt()
			if valid {
				t.Fatal("uncertain lease still valid")
			}
			expectedStops := uint64(0)
			if fault == "replacement" || fault == "resume-divider" {
				expectedStops = 1
			}
			if fw.stats[3] != expectedStops {
				t.Fatal("automatic stop attempted", fw.stats)
			}
			assertRetired(t, s, fw, base)
		})
	}
}
