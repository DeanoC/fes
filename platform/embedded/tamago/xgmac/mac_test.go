// SPDX-License-Identifier: GPL-3.0-or-later
package xgmac

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
)

type memory struct {
	data      []byte
	published bool
	writes    int
}

func newMemory() *memory                 { return &memory{data: make([]byte, 2<<20)} }
func (m *memory) Base() uint64           { return 0x92000000 }
func (m *memory) Size() uint64           { return uint64(len(m.data)) }
func (m *memory) Read32(o uint32) uint32 { return binary.LittleEndian.Uint32(m.data[o : o+4]) }
func (m *memory) Write32(o, v uint32) {
	if (o < 64 || o >= rxRing && o < rxRing+64) && o%16 == 12 && v&own != 0 && !m.published {
		panic("ownership published before descriptor barrier")
	}
	m.writes++
	binary.LittleEndian.PutUint32(m.data[o:o+4], v)
	m.published = false
}
func (m *memory) Read(o uint32, dst []byte) {
	if !m.published {
		panic("DMA data read before acquire barrier")
	}
	copy(dst, m.data[o:])
}
func (m *memory) Write(o uint32, src []byte) { m.writes++; copy(m.data[o:], src); m.published = false }
func (m *memory) Barrier()                   { m.published = true }
func (m *memory) deviceWrite32(o, v uint32)  { binary.LittleEndian.PutUint32(m.data[o:o+4], v) }

type hardware struct {
	regs             map[uint32]uint32
	phy              map[uint32]uint16
	mem              *memory
	now              time.Time
	frozen           bool
	mode             string
	layout           uint8
	accesses         int
	writes           []uint32
	frames           [][]byte
	rxQueue          [][]byte
	rxIndex, txIndex uint32
	// Optional packet wire for the real Go IP/TCP integration test.
	wireIn  <-chan []byte
	wireOut chan<- []byte
}

func newHardware() *hardware {
	return &hardware{
		regs: map[uint32]uint32{version: 0x21, features0: 1<<5 | 1<<1, features1: 8<<6 | 7, features2: 7<<18 | 7<<12},
		phy:  map[uint32]uint16{0: 1 << 12, 1: 1<<8 | 1<<5 | 1<<3 | 1<<2, 2: 0x001c, 3: 0xc916, 9: 1 << 9, 10: 1 << 11},
		mem:  newMemory(), now: time.Unix(1, 0), layout: 4}
}
func (h *hardware) Now() time.Time {
	if !h.frozen {
		h.now = h.now.Add(10 * time.Microsecond)
	}
	return h.now
}
func (h *hardware) Read32(reg uint32) uint32 {
	h.accesses++
	if reg == channelStatus && h.regs[rxControl]&1 != 0 {
		h.receive()
	}
	return h.regs[reg]
}
func (h *hardware) Write32(reg, value uint32) {
	h.accesses++
	h.writes = append(h.writes, reg)
	switch reg {
	case dmaMode:
		h.regs[reg] = value
		if h.mode != "reset" {
			h.regs[reg] = 0
			h.rxIndex, h.txIndex = 0, 0
			h.rxQueue = nil
		}
		return
	case mdioData:
		if h.mode == "mdio" {
			h.regs[reg] = value
			return
		}
		if value != 1<<22|1<<18|3<<16 || h.regs[mdioClause] != 1<<4 {
			panic("wrong Clause-22/clock command")
		}
		address := h.regs[mdioAddress]
		if address>>16 != 4 {
			h.regs[reg] = 0xffff
			return
		}
		h.regs[reg] = uint32(h.phy[address&31])
		return
	case channelStatus:
		h.regs[reg] &= ^value
		return
	case rxControl:
		if value&1 != 0 && (value>>h.layout)&((1<<(15-h.layout))-1) != bufferSize {
			panic("incorrect explicit RX buffer register layout")
		}
	case txTail:
		if !h.mem.published {
			panic("TX tail before publication barrier")
		}
		base := uint32(h.mem.Base()) + txRing
		if value < base || value >= base+ringSize*16 || (value-base)%16 != 0 {
			panic("invalid TX tail")
		}
		index := ((value-base)/16 + ringSize - 1) % ringSize
		if index != h.txIndex {
			panic("TX ring ordering violation")
		}
		desc := txRing + index*16
		state := h.mem.Read32(desc + 12)
		if state&(own|first|last|contextDesc) != own|first|last {
			panic("TX descriptor not complete/owned")
		}
		size := h.mem.Read32(desc+8) & 0x3fff
		if size < 14 || size > 1514 || state&0x7fff != size || h.mem.Read32(desc+4) != 0 || h.mem.Read32(desc) != uint32(h.mem.Base())+txBuffers+index*bufferSize {
			panic("bad TX buffer/length")
		}
		offset := txBuffers + index*bufferSize
		frame := bytes.Clone(h.mem.data[offset : offset+size])
		h.frames = append(h.frames, frame)
		if h.mode == "timeout" {
			return
		}
		if h.mode == "bus" {
			h.regs[channelStatus] |= 1 << 12
			return
		}
		if h.mode == "completion" {
			state &^= last
		}
		h.mem.deviceWrite32(desc+12, state&^own)
		h.txIndex = (h.txIndex + 1) % ringSize
		if h.wireOut != nil {
			select {
			case h.wireOut <- frame:
			default:
				panic("model wire full")
			}
		}
	}
	h.regs[reg] = value
}
func (h *hardware) receive() {
	if h.wireIn != nil {
		select {
		case frame := <-h.wireIn:
			h.rxQueue = append(h.rxQueue, frame)
		default:
		}
	}
	if len(h.rxQueue) == 0 {
		return
	}
	desc := rxRing + h.rxIndex*16
	if h.mem.Read32(desc+12)&own == 0 {
		return
	}
	frame := h.rxQueue[0]
	h.rxQueue = h.rxQueue[1:]
	if len(frame) > bufferSize {
		panic("model RX overflow")
	}
	copy(h.mem.data[rxBuffers+h.rxIndex*bufferSize:], frame)
	h.mem.deviceWrite32(desc, 0xfeedface) // RX writeback may replace buffer-address words
	h.mem.deviceWrite32(desc+12, last|uint32(len(frame)))
	h.rxIndex = (h.rxIndex + 1) % ringSize
}
func configuration() Config {
	return Config{MAC: "02:12:34:56:78:90", PHYAddress: 4, PHYID: 0x001cc916, CSRClockHz: 125000000, RXBufferShift: 4}
}
func open(t *testing.T, h *hardware) *Device {
	t.Helper()
	d, err := Open(h, h.mem, configuration())
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func TestDMAFrameRoundTripAndWrap(t *testing.T) {
	for _, shift := range []uint8{1, 4} {
		t.Run(string(rune('0'+shift)), func(t *testing.T) {
			h := newHardware()
			h.layout = shift
			c := configuration()
			c.RXBufferShift = shift
			d, err := Open(h, h.mem, c)
			if err != nil {
				t.Fatal(err)
			}
			if h.regs[packetFilter] != 0 || h.regs[channelInterrupt] != 0 || h.regs[0x3100] != 0 {
				t.Fatal("unexpected promiscuous/IRQ/descriptor mode")
			}
			for n := 0; n < 13; n++ {
				frame := bytes.Repeat([]byte{byte(n + 1)}, 100+n)
				h.rxQueue = append(h.rxQueue, frame)
				dst := make([]byte, 1600)
				size, err := d.Receive(dst)
				if err != nil || !bytes.Equal(dst[:size], frame) {
					t.Fatal("RX data", size, err)
				}
				if err = d.Transmit(frame); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(h.frames[n], frame) {
					t.Fatal("TX data mismatch")
				}
			}
			stats := d.Statistics()
			if stats.Received != 13 || stats.Transmitted != 13 || stats.Dropped != 0 {
				t.Fatal(stats)
			}
			if d.MAC() != configuration().MAC || d.Err() != nil {
				t.Fatal("identity/health changed")
			}
		})
	}
}
func TestRXDropsAndOwnershipFaults(t *testing.T) {
	for _, mode := range []string{"crc", "oversize", "short", "destination", "context", "fragment"} {
		t.Run(mode, func(t *testing.T) {
			h := newHardware()
			d := open(t, h)
			state := last | 100
			switch mode {
			case "crc":
				state |= 1 << 15
			case "oversize":
				state = last | 1515
			case "short":
				state = last | 4
			case "context":
				state |= contextDesc
			case "fragment":
				state &^= last
			}
			h.mem.deviceWrite32(rxRing+12, state)
			dst := bytes.Repeat([]byte{0xaa}, 1600)
			if mode == "destination" {
				dst = dst[:50]
			}
			count, err := d.Receive(dst)
			fatal := mode == "context" || mode == "fragment"
			if count != 0 || (err != nil) != fatal {
				t.Fatal(count, err)
			}
			if !bytes.Equal(dst, bytes.Repeat([]byte{0xaa}, len(dst))) {
				t.Fatal("invalid DMA data exposed")
			}
			if fatal {
				if d.Err() == nil {
					t.Fatal("bad descriptor reused")
				}
			} else if d.Statistics().Dropped != 1 || h.mem.Read32(rxRing+12)&own == 0 {
				t.Fatal("drop not reposted")
			}
		})
	}
}
func TestTXFailurePinsBuffersAndRefusesReuse(t *testing.T) {
	for _, mode := range []string{"timeout", "bus", "completion", "owned", "stopped", "link", "mdio", "frozen"} {
		t.Run(mode, func(t *testing.T) {
			h := newHardware()
			d := open(t, h)
			h.mode = mode
			switch mode {
			case "owned":
				h.mem.deviceWrite32(txRing+12, own)
			case "stopped":
				h.regs[rxControl] = 0
			case "link":
				h.phy[1] &= ^uint16(1 << 2)
				h.now = h.now.Add(time.Second)
			case "mdio":
				h.now = h.now.Add(time.Second)
			case "frozen":
				h.mode = "timeout"
				h.frozen = true
			}
			if err := d.Transmit(bytes.Repeat([]byte{1}, 100)); err == nil {
				t.Fatal("failed TX accepted")
			}
			memoryWrites, accesses := h.mem.writes, h.accesses
			if err := d.Transmit(bytes.Repeat([]byte{2}, 100)); err == nil {
				t.Fatal("failed TX retried")
			}
			if _, err := d.Receive(make([]byte, 1600)); err == nil {
				t.Fatal("failed RX continued")
			}
			if h.mem.writes != memoryWrites || h.accesses != accesses {
				t.Fatal("uncertain DMA region reused")
			}
			if mode == "timeout" && h.mem.Read32(txRing+12)&own == 0 {
				t.Fatal("ambiguous ownership cleared")
			}
		})
	}
}
func TestAdmissionFailures(t *testing.T) {
	for _, mode := range []string{"version", "capability", "active", "macactive", "reset", "mdio", "identity", "negotiation", "half", "speed", "fifo", "channels"} {
		t.Run(mode, func(t *testing.T) {
			h := newHardware()
			switch mode {
			case "version":
				h.regs[version] = 0x22
			case "capability":
				h.regs[features0] = 0
			case "active":
				h.regs[txControl+0x80] = 1
			case "macactive":
				h.regs[rxConfig] = 1
			case "reset":
				h.mode = "reset"
			case "mdio":
				h.mode = "mdio"
			case "identity":
				h.phy[3] = 1
			case "negotiation":
				h.phy[1] &= ^uint16(1 << 5)
			case "half":
				h.phy[10] = 1 << 10
			case "speed":
				h.phy[10] = 0
			case "fifo":
				h.regs[features1] = 0
			case "channels":
				h.regs[features2] = 15 << 12
			}
			if _, err := Open(h, h.mem, configuration()); err == nil {
				t.Fatal("unsupported setup accepted")
			}
			if h.mem.writes != 0 {
				t.Fatal("DMA memory touched before admission")
			}
			if mode == "active" && len(h.writes) != 0 {
				t.Fatal("firmware-owned channel disturbed")
			}
		})
	}
	for _, change := range []func(*Config){func(c *Config) { c.MAC = "0000.0000.0000" }, func(c *Config) { c.MAC = "01:00:00:00:00:01" }, func(c *Config) { c.CSRClockHz = 150000000 }, func(c *Config) { c.RXBufferShift = 0 }, func(c *Config) { c.PHYAddress = 32 }} {
		h := newHardware()
		c := configuration()
		change(&c)
		if _, err := Open(h, h.mem, c); err == nil || h.accesses != 0 {
			t.Fatal("invalid config touched controller")
		}
	}
}
