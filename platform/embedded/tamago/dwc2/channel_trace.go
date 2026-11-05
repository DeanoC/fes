// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import "fmt"

// channelCommands retains software writes, not controller observations. Keep
// this across packet replacement so a failure can expose the preceding halt
// and the next arm. Recording adds no register reads, clock samples or retries.
type channelCommand struct {
	sequence, packet             uint64
	reason                       string
	value, pid                   uint32
	done, nak, received, halting bool
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
	}
	return s
}
