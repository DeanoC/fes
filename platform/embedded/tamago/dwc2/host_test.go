// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/input"
)

type control struct {
	pid                  uint32
	typ, request         byte
	value, index, length uint16
	data                 []byte
	offset               int
}
type transaction struct {
	in                  bool
	ep, pid, addr, size uint32
	setup               []byte
}

// controller models USB device requests and independent FIFO/channel events.
// The fake enforces device address, endpoint PID and configuration ordering.
type controller struct {
	controlNAKs           int
	regs                  map[uint32]uint32
	tick                  time.Time
	rx, words             []uint32
	active                transaction
	request               *control
	address               uint32
	configured, protocol  bool
	device, configuration []byte
	reports               [][]byte
	reportPID             uint32
	mode                  string
	frozen                bool
	accesses              int
	starts                []transaction
	requests              []control
}

func newController() *controller {
	return &controller{regs: map[uint32]uint32{
		snpsid: 0x4f54330a, hwcfg2: 1<<19 | 2<<6 | 2<<3 | 3<<14, hwcfg3: 1024 << 16, gusb: 1 << 4,
		reset: 1 << 31, gint: 1, hprt: 2<<17 | 5, nptxstatus: 8<<16 | 64, ptxstatus: 8<<16 | 64,
	}, tick: time.Unix(1, 0), device: []byte{18, 1, 0, 2, 0, 0, 0, 8, 0x34, 0x12, 0x78, 0x56, 0, 1, 0, 0, 0, 1},
		configuration: []byte{9, 2, 34, 0, 1, 1, 0, 0x80, 50, 9, 4, 0, 0, 1, 3, 1, 1, 0, 9, 0x21, 0x11, 1, 0, 1, 0x22, 63, 0, 7, 5, 0x81, 3, 8, 0, 10}}
}
func (c *controller) Now() time.Time {
	if !c.frozen {
		c.tick = c.tick.Add(100 * time.Microsecond)
	}
	return c.tick
}
func (c *controller) Read32(reg uint32) uint32 {
	c.accesses++
	switch reg {
	case gint:
		v := c.regs[reg]
		if len(c.rx) > 0 {
			v |= 1 << 4
		}
		return v
	case rxpop:
		if len(c.rx) == 0 {
			panic("empty RX queue")
		}
		v := c.rx[0]
		c.rx = c.rx[1:]
		return v
	case fifo:
		if len(c.words) == 0 {
			panic("empty RX FIFO")
		}
		v := c.words[0]
		c.words = c.words[1:]
		return v
	}
	return c.regs[reg]
}
func (c *controller) Write32(reg, value uint32) {
	c.accesses++
	switch reg {
	case gint:
		c.regs[reg] &^= value &^ uint32(1|1<<4)
		return
	case reset:
		c.regs[reg] = value
		if c.mode != "reset" {
			c.regs[reg] = 1 << 31
		}
		return
	case hcint:
		c.regs[reg] &= ^value
		return
	case hprt:
		old := c.regs[reg]
		ro := old & uint32(3<<17|1|1<<2|1<<4)
		if value&(1<<2) != 0 {
			ro &^= 1 << 2
		}
		c.regs[reg] = ro | value&uint32(1<<12|1<<8)
		if old&(1<<8) != 0 && value&(1<<8) == 0 {
			c.regs[reg] |= 1 << 2
		}
		return
	case hcchar:
		c.regs[reg] = value
		if value&channelDisable != 0 {
			if c.mode != "halt" {
				c.regs[reg] &= ^channelEnable
				c.regs[hcint] |= 2
			}
			return
		}
		if value&channelEnable == 0 {
			return
		}
		c.active = transaction{in: value&(1<<15) != 0, ep: (value >> 11) & 15, pid: c.regs[hctsiz] >> 29 & 3, addr: value >> 22 & 127, size: c.regs[hctsiz] & 0x7ffff}
		c.starts = append(c.starts, c.active)
		if c.active.addr != c.address {
			panic("USB transaction at wrong device address")
		}
		if c.mode == "stuck" || c.mode == "halt" {
			return
		}
		if c.mode == "stall" {
			c.regs[hcint] = 1 << 3
			return
		}
		if c.mode == "nak" {
			c.regs[hcint] = 1 << 4
			return
		}
		if c.active.in {
			c.receive()
			return
		}
		if c.active.size == 0 {
			if c.active.pid != 2 {
				panic("control OUT status not DATA1")
			}
			c.finishControl()
			c.complete(nil, false)
			return
		}
		if c.active.pid != 3 || c.active.size != 8 {
			panic("unsupported OUT packet in model")
		}
		return
	case fifo:
		var word [4]byte
		binary.LittleEndian.PutUint32(word[:], value)
		c.active.setup = append(c.active.setup, word[:]...)
		if len(c.active.setup) == 8 {
			c.setup(c.active.setup)
			c.complete(nil, false)
		}
		return
	}
	c.regs[reg] = value
}
func (c *controller) setup(data []byte) {
	c.request = &control{pid: 2, typ: data[0], request: data[1], value: binary.LittleEndian.Uint16(data[2:]), index: binary.LittleEndian.Uint16(data[4:]), length: binary.LittleEndian.Uint16(data[6:])}
	r := c.request
	switch r.request {
	case 6:
		if r.value == 0x100 {
			r.data = bytes.Clone(c.device)
		} else if r.value == 0x200 {
			r.data = bytes.Clone(c.configuration)
		} else {
			panic("unknown descriptor")
		}
	case 5:
		if c.address != 0 {
			panic("address assigned twice")
		}
	case 9:
		if c.address != 1 || r.value != 1 {
			panic("invalid configuration")
		}
	case 11, 10:
		if !c.configured || r.typ != 0x21 || r.index != 0 || r.value != 0 {
			panic("invalid boot HID request")
		}
	default:
		panic("unknown control request")
	}
	c.requests = append(c.requests, *r)
}
func (c *controller) finishControl() {
	if c.request == nil {
		panic("status without request")
	}
	r := c.request
	switch r.request {
	case 5:
		c.address = uint32(r.value)
	case 9:
		c.configured = true
		c.reportPID = 0
	case 11:
		c.protocol = true
	}
	c.request = nil
}
func (c *controller) receive() {
	if c.active.ep != 0 {
		if !c.configured || !c.protocol || c.active.pid != c.reportPID {
			panic("invalid interrupt state/toggle")
		}
		if len(c.reports) == 0 {
			c.regs[hcint] = 1 << 4
			return
		}
		data := c.reports[0]
		c.reports = c.reports[1:]
		c.reportPID ^= 2
		c.complete(data, true)
		return
	}
	r := c.request
	if r == nil {
		panic("IN without control setup")
	}
	if r.length == 0 {
		if c.active.pid != 2 {
			panic("control IN status not DATA1")
		}
		c.finishControl()
		c.complete(nil, true)
		return
	}
	if c.active.pid != r.pid {
		panic("control IN data toggle mismatch")
	}
	if c.controlNAKs > 0 {
		c.controlNAKs--
		c.regs[hcint] = 1 << 4
		return
	}
	r.pid ^= 2
	maxPacket := int(c.device[7])
	n := min(maxPacket, int(r.length)-r.offset, len(r.data)-r.offset)
	data := r.data[r.offset : r.offset+n]
	r.offset += n
	c.complete(data, true)
}
func (c *controller) complete(data []byte, in bool) {
	c.regs[hcint] = 1 // Deliberately require software to retire CHEN afterwards.
	if !in {
		return
	}
	pid := c.active.pid
	if c.mode == "pid" {
		pid ^= 2
	}
	size := len(data)
	if c.mode == "overflow" {
		size = 65
	}
	if c.mode == "missing" {
		return
	}
	c.rx = append(c.rx, uint32(size)<<4|pid<<15|2<<17, 3<<17, 7<<17)
	for i := 0; i < len(data); i += 4 {
		var word [4]byte
		copy(word[:], data[i:min(i+4, len(data))])
		c.words = append(c.words, binary.LittleEndian.Uint32(word[:]))
	}
}
func open(t *testing.T, c *controller) *Host {
	t.Helper()
	h, err := Open(c, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatal(err)
	}
	return h
}
func report(t *testing.T, h *Host) []byte {
	t.Helper()
	for i := 0; i < 1000; i++ {
		data, err := h.PollKeyboard()
		if err != nil {
			t.Fatal(err)
		}
		if data != nil {
			return data
		}
	}
	t.Fatal("no report")
	return nil
}
func TestEnumerationAndKeyboard(t *testing.T) {
	for _, speed := range []uint32{1, 2} {
		t.Run(string(rune('0'+speed)), func(t *testing.T) {
			c := newController()
			c.regs[hprt] = speed<<17 | 5
			h := open(t, c)
			if c.address != 1 || !c.configured || !c.protocol || len(c.requests) != 8 {
				t.Fatal("incomplete USB enumeration", c.requests)
			}
			if c.regs[gahb]&(1<<5|1) != 0 || c.regs[hcsplit] != 0 {
				t.Fatal("DMA/IRQ/splits enabled")
			}
			if c.regs[hfir] != 59999 || c.regs[hprt]&4 == 0 {
				t.Fatal("frame clock/port enable lost")
			}
			c.reports = [][]byte{{0, 0, 4, 0, 0, 0, 0, 0}, make([]byte, 8)}
			if got := report(t, h); got[2] != 4 {
				t.Fatal(got)
			}
			if got := report(t, h); !bytes.Equal(got, make([]byte, 8)) {
				t.Fatal(got)
			}
			if h.toggle != 0 {
				t.Fatal("toggle did not advance twice")
			}
		})
	}
}
func TestSharedInputDecoder(t *testing.T) {
	c := newController()
	h := open(t, c)
	keyboard, err := input.NewUSBKeyboard(h)
	if err != nil {
		t.Fatal(err)
	}
	c.reports = [][]byte{{0, 0, 4, 0, 0, 0, 0, 0}, make([]byte, 8)}
	for i := 0; i < 1000; i++ {
		events, err := keyboard.Poll()
		if err != nil {
			t.Fatal(err)
		}
		if len(events) > 0 {
			if events[0].Key != "a" {
				t.Fatal(events)
			}
			return
		}
	}
	t.Fatal("HID report did not reach normalized input")
}
func TestNAKIsBoundedAndPreservesToggle(t *testing.T) {
	c := newController()
	h := open(t, c)
	for i := 0; i < 500; i++ {
		before := c.accesses
		if data, err := h.PollKeyboard(); err != nil || data != nil {
			t.Fatal(data, err)
		}
		if c.accesses-before > 100 {
			t.Fatal("unbounded input poll")
		}
	}
	if h.toggle != 0 || c.reportPID != 0 {
		t.Fatal("NAK advanced toggle")
	}
	c.reports = [][]byte{{0, 0, 4, 0, 0, 0, 0, 0}}
	if report(t, h)[2] != 4 {
		t.Fatal("lost report after NAK")
	}
}
func TestTransferFailuresRetireHost(t *testing.T) {
	for _, mode := range []string{"stall", "pid", "overflow", "missing", "stuck", "halt", "disconnect", "short"} {
		t.Run(mode, func(t *testing.T) {
			c := newController()
			h := open(t, c)
			c.mode = mode
			c.reports = [][]byte{{0, 0, 4, 0, 0, 0, 0, 0}}
			if mode == "short" {
				c.reports = [][]byte{{0, 0, 4}}
			}
			if mode == "disconnect" {
				c.regs[hprt] &= ^uint32(5)
			}
			var err error
			for n := 0; n < 10000; n++ {
				_, err = h.PollKeyboard()
				if err != nil {
					break
				}
			}
			if err == nil {
				t.Fatal("bad transfer accepted")
			}
			before := c.accesses
			if _, again := h.PollKeyboard(); again == nil {
				t.Fatal("failed host reused")
			}
			if c.accesses != before {
				t.Fatal("failed host accessed hardware")
			}
		})
	}
}
func TestAdmissionAndStartupFailures(t *testing.T) {
	for _, mode := range []string{"id", "dma", "active", "fifo", "phy", "deviceonly", "clock", "reset", "overcurrent", "absent", "highspeed", "descriptor", "hub", "config", "interval", "frozen"} {
		t.Run(mode, func(t *testing.T) {
			c := newController()
			switch mode {
			case "id":
				c.regs[snpsid] = 0
			case "dma":
				c.regs[gahb] = 1 << 5
			case "active":
				c.regs[hcchar+0x20] = channelEnable
			case "fifo":
				c.regs[hwcfg3] = 128 << 16
			case "phy":
				c.regs[gusb] = 0
			case "deviceonly":
				c.regs[hwcfg2] = c.regs[hwcfg2]&^uint32(7) | 4
			case "clock":
				c.regs[reset] = 0
			case "reset":
				c.mode = "reset"
			case "overcurrent":
				c.regs[hprt] |= 1 << 4
			case "absent":
				c.regs[hprt] = 0
			case "highspeed":
				c.regs[hprt] = 5
			case "descriptor":
				c.device[7] = 9
			case "hub":
				c.device[4] = 9
			case "interval":
				c.configuration[len(c.configuration)-1] = 1
			case "config":
				c.configuration[2] = 0xff
				c.configuration[3] = 0xff
			case "frozen":
				c.frozen = true
			}
			if _, err := Open(c, Config{PHYClockHz: 60000000}); err == nil {
				t.Fatal("invalid controller/device accepted")
			}
			if mode == "dma" && c.regs[gahb] != 1<<5 {
				t.Fatal("firmware DMA modified before ownership admission")
			}
		})
	}
}

func TestControlNAKsAndFullSpeed64BytePackets(t *testing.T) {
	c := newController()
	c.controlNAKs = 3
	c.regs[hprt] = 1<<17 | 5
	c.device[7] = 64
	h := open(t, c)
	if c.controlNAKs != 0 || h.ep0 != 64 {
		t.Fatal("control retries/EP0 discovery incomplete")
	}
	c.reports = [][]byte{{0, 0, 4, 0, 0, 0, 0, 0}}
	if report(t, h)[2] != 4 {
		t.Fatal("lost input after control retries")
	}
}
