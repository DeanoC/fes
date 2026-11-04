// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

type rxObserved struct {
	*controller
	peeks, pops, fifos int
}

func (c *rxObserved) Read32(reg uint32) uint32 {
	switch reg {
	case rxpeek:
		c.peeks++
	case rxpop:
		c.pops++
	case fifo:
		c.fifos++
	}
	return c.controller.Read32(reg)
}

func TestRXFaultPreservesFirstSnapshotAndRetiresWithoutAnotherPop(t *testing.T) {
	for _, status := range []uint32{0, 1, 5, 15} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			c := newController()
			h := open(t, c)
			observed := &rxObserved{controller: c}
			h.io = observed
			// Reproduce the recorded channel state, with an independently
			// supplied invalid FIFO record. It must never become input.
			c.rx = []uint32{status << 17}
			c.regs[hcchar] = 0xc0128008
			c.regs[hctsiz] = 0x40080008
			c.mode = "halt"
			h.pending = packet{active: true, in: true, pid: 2, limit: 8, deadline: c.Now().Add(time.Second)}
			done, data, nak, err := h.step()
			if err == nil || done || nak || len(data) != 0 {
				t.Fatalf("invalid RX yielded a report: %v %x %v %v", done, data, nak, err)
			}
			for _, field := range []string{fmt.Sprintf("unsupported RX status %d", status), "irq=0x11",
				fmt.Sprintf("peek=%#x pop=%#x", status<<17, status<<17), "after=0x1",
				"channel=0xc0128008", "transfer=0x40080008"} {
				if !strings.Contains(err.Error(), field) {
					t.Fatalf("missing first observation %q: %v", field, err)
				}
			}
			retired := h.fail(err)
			if !strings.Contains(retired.Error(), "containment: dwc2: register wait exhausted") {
				t.Fatalf("uncertain halt was hidden: %v", retired)
			}
			before := c.accesses
			if _, again := h.PollKeyboard(); again != retired || c.accesses != before {
				t.Fatal("failed host resumed hardware access")
			}
			if observed.peeks != 1 || observed.pops != 1 || observed.fifos != 0 {
				t.Fatalf("fault consumed another queue/data entry: %+v", observed)
			}
		})
	}
}
