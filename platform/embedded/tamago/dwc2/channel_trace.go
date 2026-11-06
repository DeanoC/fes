// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"fmt"
	"time"
)

// channelCommands retains software writes across packet replacement so a
// failure can expose the preceding halt and next arm. The basic write record
// adds no reads or clocks. Direct periodic arm/continuation records separately
// carry the sequential observations made by writePeriodicChannel.
type channelCommand struct {
	sequence, packet             uint64
	reason                       string
	value, pid                   uint32
	done, nak, received, halting bool
	periodic                     periodicCommandTiming
}

// These sequential observations bracket a command, not its exact bus commit
// or selected wire frame. The clocks include the frame reads and bookkeeping.
// No observation changes the command or authorizes data, retirement or a retry.
type periodicCommandTiming struct {
	sampled                    bool
	selected, before, after    uint32
	selectionToPreNS, windowNS int64
}

// writePeriodicChannel records only direct periodic arm/continuation writes.
// Selection used the existing frame read. Two extra non-destructive HFNUM reads
// and three clock samples bound the surrounding software/MMIO window. Neither
// RX nor channel status is consumed here. Split and halt paths do not use it.
func (h *Host) writePeriodicChannel(reason string, value, selected uint32, selectionStart time.Time) {
	pre := h.io.Now()
	before := h.io.Read32(hfnum)
	h.writeChannel(reason, value)
	after := h.io.Read32(hfnum)
	end := h.io.Now()
	c := &h.channelCommands
	c.entries[(c.count-1)%uint64(len(c.entries))].periodic = periodicCommandTiming{
		sampled: true, selected: selected, before: before, after: after,
		selectionToPreNS: pre.Sub(selectionStart).Nanoseconds(), windowNS: end.Sub(pre).Nanoseconds(),
	}
}

type channelCommands struct {
	entries [8]channelCommand
	count   uint64
}

func (h *Host) writeChannel(reason string, value uint32) {
	p := &h.pending
	c := &h.channelCommands
	c.entries[c.count%uint64(len(c.entries))] = channelCommand{
		sequence: c.count + 1, packet: h.packetSequence, reason: reason,
		value: value, pid: p.pid, done: p.done, nak: p.nak,
		received: p.received, halting: p.halting,
	}
	c.count++
	h.io.Write32(hcchar, value)
}

func (c channelCommands) String() string {
	start := uint64(0)
	if c.count > uint64(len(c.entries)) {
		start = c.count - uint64(len(c.entries))
	}
	s := ""
	for n := start; n < c.count; n++ {
		v := c.entries[n%uint64(len(c.entries))]
		if s != "" {
			s += "; "
		}
		s += fmt.Sprintf("seq=%d packet=%d reason=%s value=%#x pid=%d done=%t nak=%t received=%t halting=%t",
			v.sequence, v.packet, v.reason, v.value, v.pid, v.done, v.nak, v.received, v.halting)
		if v.periodic.sampled {
			p := v.periodic
			s += fmt.Sprintf(" periodic_timing={sampled=true selected=%#x before=%#x after=%#x select_to_pre_ns=%d window_ns=%d}",
				p.selected, p.before, p.after, p.selectionToPreNS, p.windowNS)
		}
	}
	return s
}
