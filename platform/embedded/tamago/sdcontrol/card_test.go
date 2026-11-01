// SPDX-License-Identifier: GPL-3.0-or-later
package sdcontrol

import "time"

// Independent host-only SDHC register/card model. Any driver access without
// the C firmware's active grant fails. PHY preparation is checked separately.
const (
	blockSize    = 0x204
	argument     = 0x208
	command      = 0x20c
	response     = 0x210
	dataPort     = 0x220
	present      = 0x224
	host         = 0x228
	clock        = 0x22c
	status       = 0x230
	statusEnable = 0x234
	signalEnable = 0x238
)

type card struct {
	regs                             map[uint32]uint32
	tick                             time.Time
	active, prepared, badClock       bool
	state, words, accesses, commands int
	cid                              uint32
}

func newCard() *card {
	return &card{regs: map[uint32]uint32{clock: 7, 0x240: 1 << 24}, tick: time.Unix(1, 0), cid: 0x123456}
}
func (c *card) Now() time.Time { c.tick = c.tick.Add(time.Millisecond); return c.tick }
func (c *card) access() {
	if !c.active {
		panic("PIO before firmware grant or after containment")
	}
	c.accesses++
}
func (c *card) Read32(r uint32) uint32 {
	c.access()
	if r == dataPort {
		c.words++
		if c.words == 128 {
			c.regs[status] |= 2
		}
		return 0x03020100
	}
	return c.regs[r]
}
func (c *card) Write32(r, v uint32) {
	c.access()
	if r == status {
		c.regs[r] &^= v
		return
	}
	if r == clock {
		if v&(7<<24) != 0 {
			c.prepared = false
			c.regs[command] = 0
			c.regs[present] = 0
		}
		if v&4 != 0 && !c.prepared {
			panic("card clock before firmware PHY preparation")
		}
		c.regs[r] = v &^ (7 << 24)
		if v&1 != 0 && !c.badClock {
			c.regs[r] |= 2
		}
		return
	}
	c.regs[r] = v
	if r != command {
		return
	}
	if !c.prepared {
		panic("SD command before firmware PHY preparation")
	}
	c.commands++
	c.words = 0
	c.regs[response] = 0
	switch v >> 24 {
	case 0:
		c.state = 0
	case 8:
		c.regs[response] = 0x1aa
	case 55:
		c.regs[response] = 1 << 5
	case 41:
		c.regs[response] = 0xc0300000
	case 2:
		c.state = 2
		c.regs[response] = 0x11223344
		c.regs[response+4] = 0x55667788
		c.regs[response+8] = 0x99aabbcc
		c.regs[response+12] = c.cid
	case 3:
		if c.state != 2 {
			panic("RCA before CID")
		}
		c.state = 3
		c.regs[response] = 7 << 16
	case 9:
		c.regs[response+12] = 1 << 22
		c.regs[response+4] = 7 << 8
	case 7:
		c.state = 4
		c.regs[status] |= 2
	case 13:
		c.regs[response] = uint32(c.state)<<9 | 1<<8
	case 17:
		if c.state != 4 {
			panic("read before card selection")
		}
		c.regs[status] |= 1 << 5
	default:
		panic("unexpected SD command")
	}
	c.regs[status] |= 1
}
