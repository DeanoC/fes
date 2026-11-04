// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestNonDataRXRetainedFieldsDoNotReadAnotherPayload(t *testing.T) {
	for _, fields := range []uint32{8<<4 | 2<<15, 65<<4 | 3<<15, 2047<<4 | 1<<15} {
		c := newController()
		h := open(t, c)
		c.completionFields, c.haltFields = fields, fields
		want := []byte{0, 0, 4, 0, 0, 0, 0, 0}
		c.reports = [][]byte{want}
		if got := report(t, h); !bytes.Equal(got, want) {
			t.Fatalf("non-data metadata changed payload: %x", got)
		}
		if len(c.words) != 0 || h.pending.active || c.regs[hcchar]&channelEnable != 0 {
			t.Fatal("payload escaped before FIFO/channel retirement")
		}
	}
}

func TestNonDataRXByteCountCannotSubstituteForActualData(t *testing.T) {
	c := newController()
	h := open(t, c)
	// The physical completion word was0x70080. Even with DONE and a retired
	// channel, its retained count/PID must not manufacture an IN-data packet.
	c.rx = []uint32{0x70080, 7<<17 | 8<<4 | 2<<15}
	c.regs[hcint], c.regs[hcchar] = 1, 0
	h.pending = packet{active: true, in: true, limit: 8, pid: 2, deadline: c.Now().Add(time.Second)}
	done, data, nak, err := h.step()
	if err == nil || !strings.Contains(err.Error(), "completion without RX packet") || done || nak || len(data) != 0 {
		t.Fatalf("non-data entry invented input: %v %x %v %v", done, data, nak, err)
	}
}
