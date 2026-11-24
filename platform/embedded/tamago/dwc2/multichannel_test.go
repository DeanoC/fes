// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"bytes"
	"context"
	"encoding/binary"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

// multiSimDevice models one virtual USB 2.0 device (hub, BT HCI, gamepad, or keyboard)
// attached to the multi-channel DWC2 register simulator.
type multiSimDevice struct {
	addr         uint8
	speed        usb.Speed
	deviceDesc   []byte
	configDesc   []byte
	hubDesc      []byte
	isHub        bool
	multiTTSet   bool
	ports        [4]uint32
	portChildren [4]*multiSimDevice

	// Queued IN payloads per endpoint number (1..15)
	inQueues map[uint8][][]byte
	// Optional fixed repeating IN payload per endpoint (zero-alloc test mode)
	inRepeat [16][]byte
	// Recorded OUT payloads per endpoint number (0 = EP0 Control OUT, 1..15 = Int/Bulk OUT)
	outLog map[uint8][][]byte
	// Inject fatal STALL on a specific endpoint number
	stallEP map[uint8]bool
}

func newSimHub(ports int, multiTT bool) *multiSimDevice {
	proto := byte(1)
	if multiTT {
		proto = 2
	}
	// Config with alt 0 (single-TT) and alt 1 (multi-TT) + EP1 Int-IN status endpoint.
	cfg := []byte{
		9, 2, 41, 0, 1, 1, 0, 0xe0, 50,
		9, 4, 0, 0, 1, 9, 0, 1, 0,
		7, 5, 0x81, 3, 1, 0, 12,
		9, 4, 0, 1, 1, 9, 0, 2, 0,
		7, 5, 0x81, 3, 1, 0, 12,
	}
	return &multiSimDevice{
		speed:      usb.SpeedHigh,
		isHub:      true,
		deviceDesc: []byte{18, 1, 0, 2, 9, 0, proto, 64, 0x42, 0x1a, 0x01, 0x01, 0, 1, 0, 0, 0, 1},
		configDesc: cfg,
		hubDesc:    []byte{9, 0x29, byte(ports), 0, 0, 10, 0, 0, 0xff},
		inQueues:   map[uint8][][]byte{},
		outLog:     map[uint8][][]byte{},
		stallEP:    map[uint8]bool{},
	}
}

func newSimBTDongle() *multiSimDevice {
	cfg := []byte{
		9, 2, 39, 0, 1, 1, 0, 0xe0, 50,
		9, 4, 0, 0, 3, 0xe0, 0x01, 0x01, 0,
		7, 5, 0x81, 3, 16, 0, 1, // EP1 Int-IN (HCI Events)
		7, 5, 0x82, 2, 0x00, 0x02, 0, // EP2 Bulk-IN 512B (ACL RX)
		7, 5, 0x02, 2, 0x00, 0x02, 0, // EP2 Bulk-OUT 512B (ACL TX)
	}
	return &multiSimDevice{
		speed:      usb.SpeedHigh,
		deviceDesc: []byte{18, 1, 0, 2, 0xe0, 0x01, 0x01, 64, 0xda, 0x0b, 0x71, 0x87, 0, 2, 0, 0, 0, 1},
		configDesc: cfg,
		inQueues:   map[uint8][][]byte{},
		outLog:     map[uint8][][]byte{},
		stallEP:    map[uint8]bool{},
	}
}

func newSimGamepad() *multiSimDevice {
	cfg := []byte{
		9, 2, 41, 0, 1, 1, 0, 0xa0, 250,
		9, 4, 0, 0, 2, 3, 0, 0, 0,
		9, 0x21, 0x11, 1, 0, 1, 0x22, 64, 0,
		7, 5, 0x81, 3, 64, 0, 4, // EP1 Int-IN 64B, 4ms (overridable to 1ms)
		7, 5, 0x02, 3, 64, 0, 4, // EP2 Int-OUT 64B (LEDs/rumble/init)
	}
	return &multiSimDevice{
		speed:      usb.SpeedFull,
		deviceDesc: []byte{18, 1, 0, 2, 0, 0, 0, 64, 0x4c, 0x05, 0xe6, 0x0c, 0, 1, 0, 0, 0, 1},
		configDesc: cfg,
		inQueues:   map[uint8][][]byte{},
		outLog:     map[uint8][][]byte{},
		stallEP:    map[uint8]bool{},
	}
}

func newSimKeyboard() *multiSimDevice {
	cfg := []byte{
		9, 2, 34, 0, 1, 1, 0, 0xa0, 50,
		9, 4, 0, 0, 1, 3, 1, 1, 0,
		9, 0x21, 0x11, 1, 0, 1, 0x22, 63, 0,
		7, 5, 0x81, 3, 8, 0, 8,
	}
	return &multiSimDevice{
		speed:      usb.SpeedFull,
		deviceDesc: []byte{18, 1, 0, 2, 0, 0, 0, 8, 0x34, 0x12, 0x78, 0x56, 0, 1, 0, 0, 0, 1},
		configDesc: cfg,
		inQueues:   map[uint8][][]byte{},
		outLog:     map[uint8][][]byte{},
		stallEP:    map[uint8]bool{},
	}
}

type simCtrlState struct {
	dev     *multiSimDevice
	reqType byte
	req     byte
	value   uint16
	index   uint16
	length  int
	resp    []byte
	offset  int
	outBuf  []byte
}

type multiChannelSim struct {
	mu       sync.Mutex
	regs     map[uint32]uint32
	tick     time.Time
	frame    uint16
	rx       []uint32
	rxHead   int
	rxWords  []uint32
	rxWHead  int
	rootDev  *multiSimDevice
	byAddr   map[uint8]*multiSimDevice
	resetDev *multiSimDevice // device currently addressed at 0 during enumeration
	ctrl     *simCtrlState
	txExpect [16]int
	txBuf    [16][]byte
	txDev    [16]*multiSimDevice
	txEP     [16]uint8
	txSplit  [16]bool
	noOutLog bool
}

func newMultiChannelSim(fifoWords uint32, channels int) *multiChannelSim {
	s := &multiChannelSim{
		regs: map[uint32]uint32{
			snpsid:     0x4f54330a,
			hwcfg2:     1<<19 | 2<<6 | 2<<3 | uint32(channels-1)<<14,
			hwcfg3:     fifoWords << 16,
			gusb:       1 << 4,
			reset:      1 << 31,
			gint:       1,
			hprt:       0, // initially disconnected unless attachRoot called
			nptxstatus: 16<<16 | 256,
			ptxstatus:  16<<16 | 256,
		},
		tick:    time.Unix(10, 0),
		rx:      make([]uint32, 0, 64),
		rxWords: make([]uint32, 0, 512),
		byAddr:  map[uint8]*multiSimDevice{},
	}
	for ch := range s.txBuf {
		s.txBuf[ch] = make([]byte, 0, 512)
	}
	return s
}

func (s *multiChannelSim) attachRoot(dev *multiSimDevice) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rootDev = dev
	s.resetDev = dev
	dev.addr = 0
	s.regs[hprt] = uint32(dev.speed)<<17 | 1<<12 | 1<<1 | 1 // powered, connected, connect-changed
}

func (s *multiChannelSim) detachRoot() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rootDev = nil
	s.resetDev = nil
	s.byAddr = map[uint8]*multiSimDevice{}
	s.regs[hprt] = 1<<12 | 1<<1 | 1<<3 // powered, disconnected, change bits set
	s.regs[gint] |= gintDisconn | gintPRTINT
}

func (s *multiChannelSim) attachHubPort(hub *multiSimDevice, port int, child *multiSimDevice) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hub.portChildren[port-1] = child
	spdBits := uint32(0) // FS
	if child.speed == usb.SpeedLow {
		spdBits = 1 << 9
	} else if child.speed == usb.SpeedHigh {
		spdBits = 1 << 10
	}
	hub.ports[port-1] = 1 | 1<<8 | 1<<16 | spdBits // connected, powered, C_PORT_CONNECTION
}

func (s *multiChannelSim) detachHubPort(hub *multiSimDevice, port int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old := hub.portChildren[port-1]; old != nil && old.addr != 0 {
		delete(s.byAddr, old.addr)
	}
	hub.portChildren[port-1] = nil
	hub.ports[port-1] = 1<<8 | 1<<16 // powered, disconnected, C_PORT_CONNECTION
}

func (s *multiChannelSim) Now() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tick = s.tick.Add(10 * time.Microsecond)
	return s.tick
}

func (s *multiChannelSim) advance(d time.Duration) {
	s.mu.Lock()
	s.tick = s.tick.Add(d)
	s.mu.Unlock()
}

func (s *multiChannelSim) Read32(reg uint32) uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch reg {
	case gint:
		v := s.regs[reg]
		if s.rxHead < len(s.rx) {
			v |= gintRXFLVL
		}
		return v
	case hfnum:
		v := uint32(s.frame)
		s.frame = (s.frame + 1) & 0x3fff
		return v
	case rxpeek:
		if s.rxHead < len(s.rx) {
			return s.rx[s.rxHead]
		}
		return 0
	case rxpop:
		if s.rxHead >= len(s.rx) {
			return 0
		}
		v := s.rx[s.rxHead]
		s.rxHead++
		if s.rxHead >= len(s.rx) {
			s.rx = s.rx[:0]
			s.rxHead = 0
		}
		return v
	case fifo:
		if s.rxWHead >= len(s.rxWords) {
			return 0
		}
		v := s.rxWords[s.rxWHead]
		s.rxWHead++
		if s.rxWHead >= len(s.rxWords) {
			s.rxWords = s.rxWords[:0]
			s.rxWHead = 0
		}
		return v
	default:
		return s.regs[reg]
	}
}

func (s *multiChannelSim) enqueueRX(ch int, pid uint32, payload []byte) {
	s.rx = append(s.rx, 2<<17|pid<<15|uint32(len(payload))<<4|uint32(ch), 3<<17|uint32(ch))
	for i := 0; i < len(payload); i += 4 {
		var w [4]byte
		copy(w[:], payload[i:min(i+4, len(payload))])
		s.rxWords = append(s.rxWords, binary.LittleEndian.Uint32(w[:]))
	}
}

func (s *multiChannelSim) Write32(reg, val uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch reg {
	case gint:
		s.regs[reg] &^= val &^ uint32(1|gintRXFLVL)
		return
	case reset:
		s.regs[reg] = 1 << 31
		return
	case hprt:
		// Clear W1C bits (1,3,5) if written 1; handle port reset (bit 8) -> enable (bit 2).
		cur := s.regs[hprt]
		cur &^= val & uint32(1<<1|1<<3|1<<5)
		cur = (cur &^ uint32(1<<12|1<<8)) | (val & uint32(1<<12|1<<8))
		if val&(1<<8) == 0 && s.regs[hprt]&(1<<8) != 0 && s.rootDev != nil {
			cur |= 1<<2 | 1 | uint32(s.rootDev.speed)<<17
			s.resetDev = s.rootDev
			s.rootDev.addr = 0
		}
		s.regs[hprt] = cur
		return
	}

	// Channel TX FIFO writes (0x1000 + ch*0x1000)
	if reg >= fifo && (reg-fifo)%0x1000 == 0 {
		ch := int((reg - fifo) / 0x1000)
		if ch >= 0 && ch < 16 && s.txExpect[ch] > 0 {
			var w [4]byte
			binary.LittleEndian.PutUint32(w[:], val)
			need := min(4, s.txExpect[ch]-len(s.txBuf[ch]))
			s.txBuf[ch] = append(s.txBuf[ch], w[:need]...)
			if len(s.txBuf[ch]) >= s.txExpect[ch] {
				s.finishTX(ch)
			}
		}
		return
	}

	// Channel register writes (0x500..0x700)
	if reg >= hcchar && reg < hcchar+16*0x20 {
		ch := int((reg - hcchar) / 0x20)
		off := (reg - hcchar) % 0x20
		if off == 8 { // HCINT W1C
			s.regs[reg] &^= val
			if s.regs[reg] == 0 {
				s.regs[haint] &^= 1 << uint(ch)
			}
			return
		}
		s.regs[reg] = val
		if off == 0 { // HCCHAR
			s.handleHCCHAR(ch, val)
		}
		return
	}

	s.regs[reg] = val
}

func (s *multiChannelSim) findDevice(addr uint8) *multiSimDevice {
	if addr == 0 {
		return s.resetDev
	}
	return s.byAddr[addr]
}

func (s *multiChannelSim) handleHCCHAR(ch int, val uint32) {
	if val&channelDisable != 0 {
		s.regs[chReg(ch, hcchar)] &^= channelEnable | channelDisable
		s.regs[chReg(ch, hcint)] |= 1 << 1
		s.regs[haint] |= 1 << uint(ch)
		return
	}
	if val&channelEnable == 0 {
		return
	}
	addr := uint8((val >> 22) & 0x7f)
	ep := uint8((val >> 11) & 0x0f)
	in := val&(1<<15) != 0
	tsiz := s.regs[chReg(ch, hctsiz)]
	size := int(tsiz & 0x7ffff)
	pid := (tsiz >> 29) & 3
	splt := s.regs[chReg(ch, hcsplit)]
	split := splt&(1<<31) != 0
	compSplit := splt&(1<<16) != 0

	dev := s.findDevice(addr)
	if dev == nil {
		s.regs[chReg(ch, hcchar)] &^= channelEnable
		s.regs[chReg(ch, hcint)] |= 1<<7 | 1<<1 // XACTERR + CHHLTD
		s.regs[haint] |= 1 << uint(ch)
		return
	}

	// Start-split on IN or zero-length OUT immediately ACKs so host issues CSPLIT.
	if split && !compSplit && (in || size == 0) {
		s.regs[chReg(ch, hcchar)] &^= channelEnable
		s.regs[chReg(ch, hcint)] |= 1<<5 | 1<<1 // ACK + CHHLTD
		s.regs[haint] |= 1 << uint(ch)
		return
	}

	if !in {
		if split && compSplit {
			// CSPLIT OUT completes the earlier SSPLIT OUT payload.
			s.regs[chReg(ch, hcchar)] &^= channelEnable
			s.regs[chReg(ch, hcint)] |= 1 | 1<<1
			s.regs[haint] |= 1 << uint(ch)
			return
		}
		if size == 0 {
			// Zero-length OUT (control status stage).
			if ch == 0 && s.ctrl != nil {
				s.ctrl = nil
			}
			s.regs[chReg(ch, hcchar)] &^= channelEnable
			s.regs[chReg(ch, hcint)] |= 1 | 1<<1
			s.regs[haint] |= 1 << uint(ch)
			return
		}
		s.txExpect[ch] = size
		s.txBuf[ch] = s.txBuf[ch][:0]
		s.txDev[ch] = dev
		s.txEP[ch] = ep
		s.txSplit[ch] = split
		return
	}

	// IN direction
	if ch == 0 {
		// Control DATA IN or STATUS IN
		if s.ctrl == nil {
			s.regs[chReg(ch, hcchar)] &^= channelEnable
			s.regs[chReg(ch, hcint)] |= 1<<3 | 1<<1
			return
		}
		if s.ctrl.reqType&0x80 == 0 {
			// Status IN for OUT/no-data control request
			if s.ctrl.reqType == 0x00 && s.ctrl.req == 5 { // SET_ADDRESS
				newAddr := uint8(s.ctrl.value)
				s.ctrl.dev.addr = newAddr
				s.byAddr[newAddr] = s.ctrl.dev
			}
			if s.ctrl.reqType == 0x01 && s.ctrl.req == 11 && s.ctrl.value == 1 { // SET_INTERFACE alt=1
				s.ctrl.dev.multiTTSet = true
			}
			if s.ctrl.length > 0 {
				s.ctrl.dev.outLog[0] = append(s.ctrl.dev.outLog[0], append([]byte(nil), s.ctrl.outBuf...))
			}
			s.ctrl = nil
			s.enqueueRX(0, pid, nil)
			s.regs[chReg(ch, hcchar)] &^= channelEnable
			s.regs[chReg(ch, hcint)] |= 1 | 1<<1
			return
		}
		rem := len(s.ctrl.resp) - s.ctrl.offset
		n := min(size, rem)
		chunk := s.ctrl.resp[s.ctrl.offset : s.ctrl.offset+n]
		s.ctrl.offset += n
		s.enqueueRX(0, pid, chunk)
		s.regs[chReg(ch, hcchar)] &^= channelEnable
		s.regs[chReg(ch, hcint)] |= 1 | 1<<1
		return
	}

	// Non-EP0 IN pipe (Interrupt-IN or Bulk-IN)
	if dev.stallEP[ep] {
		s.regs[chReg(ch, hcchar)] &^= channelEnable
		s.regs[chReg(ch, hcint)] |= 1<<3 | 1<<1 // STALL + CHHLTD
		s.regs[haint] |= 1 << uint(ch)
		return
	}
	if dev.isHub && ep == 1 {
		// Real USB 2.0 hub status-change endpoint reports live C_PORT_* bits.
		var bitmap byte
		for p := 0; p < len(dev.ports); p++ {
			if dev.ports[p]&(0x1f<<16) != 0 {
				bitmap |= 1 << uint(p+1)
			}
		}
		if bitmap == 0 {
			s.regs[chReg(ch, hcchar)] &^= channelEnable
			s.regs[chReg(ch, hcint)] |= 1<<4 | 1<<1 // NAK + CHHLTD
			s.regs[haint] |= 1 << uint(ch)
			return
		}
		s.enqueueRX(ch, pid, []byte{bitmap})
		s.regs[chReg(ch, hcchar)] &^= channelEnable
		s.regs[chReg(ch, hcint)] |= 1 | 1<<1 // XFERCOMPL + CHHLTD
		s.regs[haint] |= 1 << uint(ch)
		return
	}
	var pkt []byte
	if int(ep) < len(dev.inRepeat) && dev.inRepeat[ep] != nil {
		pkt = dev.inRepeat[ep]
	} else {
		q := dev.inQueues[ep]
		if len(q) == 0 {
			s.regs[chReg(ch, hcchar)] &^= channelEnable
			s.regs[chReg(ch, hcint)] |= 1<<4 | 1<<1 // NAK + CHHLTD
			s.regs[haint] |= 1 << uint(ch)
			return
		}
		pkt = q[0]
		dev.inQueues[ep] = q[1:]
	}
	s.enqueueRX(ch, pid, pkt)
	s.regs[chReg(ch, hcchar)] &^= channelEnable
	s.regs[chReg(ch, hcint)] |= 1 | 1<<1 // XFERCOMPL + CHHLTD
	s.regs[haint] |= 1 << uint(ch)
}

func (s *multiChannelSim) finishTX(ch int) {
	dev := s.txDev[ch]
	ep := s.txEP[ch]
	split := s.txSplit[ch]
	s.txExpect[ch] = 0
	var payload []byte
	if !s.noOutLog || ch == 0 {
		payload = append([]byte(nil), s.txBuf[ch]...)
	}
	s.txBuf[ch] = s.txBuf[ch][:0]

	if ch == 0 {
		pid := (s.regs[chReg(0, hctsiz)] >> 29) & 3
		if pid == 3 { // SETUP packet
			reqType, req := payload[0], payload[1]
			val := binary.LittleEndian.Uint16(payload[2:4])
			idx := binary.LittleEndian.Uint16(payload[4:6])
			length := int(binary.LittleEndian.Uint16(payload[6:8]))
			cs := &simCtrlState{dev: dev, reqType: reqType, req: req, value: val, index: idx, length: length}
			switch {
			case reqType == 0x80 && req == 6 && val>>8 == 1: // GET_DESCRIPTOR Device
				cs.resp = dev.deviceDesc[:min(length, len(dev.deviceDesc))]
			case reqType == 0x80 && req == 6 && val>>8 == 2: // GET_DESCRIPTOR Config
				cs.resp = dev.configDesc[:min(length, len(dev.configDesc))]
			case reqType == 0xa0 && req == 6 && val>>8 == 0x29: // GET_DESCRIPTOR Hub
				cs.resp = dev.hubDesc[:min(length, len(dev.hubDesc))]
			case reqType == 0xa3 && req == 0: // GET_PORT_STATUS
				var b [4]byte
				if idx >= 1 && int(idx) <= len(dev.ports) {
					binary.LittleEndian.PutUint32(b[:], dev.ports[idx-1])
				}
				cs.resp = b[:]
			case reqType == 0x23 && req == 3: // SET_PORT_FEATURE
				if idx >= 1 && int(idx) <= len(dev.ports) {
					switch val {
					case 8: // PORT_POWER
						dev.ports[idx-1] |= 1 << 8
					case 4: // PORT_RESET -> enable child at address 0
						dev.ports[idx-1] |= 1<<1 | 1<<20 // PORT_ENABLE + C_PORT_RESET
						if child := dev.portChildren[idx-1]; child != nil {
							child.addr = 0
							s.resetDev = child
						}
					}
				}
			case reqType == 0x23 && req == 1: // CLEAR_PORT_FEATURE
				if idx >= 1 && int(idx) <= len(dev.ports) && val >= 16 && val <= 20 {
					dev.ports[idx-1] &^= 1 << val
				}
			}
			s.ctrl = cs
		} else if s.ctrl != nil {
			s.ctrl.outBuf = append(s.ctrl.outBuf, payload...)
		}
		s.regs[chReg(0, hcchar)] &^= channelEnable
		if split {
			s.regs[chReg(0, hcint)] |= 1<<5 | 1<<1 // ACK for SSPLIT
		} else {
			s.regs[chReg(0, hcint)] |= 1 | 1<<1
		}
		return
	}

	// Non-EP0 OUT transfer (Interrupt OUT or Bulk OUT)
	if dev.stallEP[ep] {
		s.regs[chReg(ch, hcchar)] &^= channelEnable
		s.regs[chReg(ch, hcint)] |= 1<<3 | 1<<1
		s.regs[haint] |= 1 << uint(ch)
		return
	}
	if !s.noOutLog {
		dev.outLog[ep] = append(dev.outLog[ep], payload)
	}
	s.regs[chReg(ch, hcchar)] &^= channelEnable
	if split {
		s.regs[chReg(ch, hcint)] |= 1<<5 | 1<<1 // SSPLIT ACK -> CSPLIT completes
	} else {
		s.regs[chReg(ch, hcint)] |= 1 | 1<<1
	}
	s.regs[haint] |= 1 << uint(ch)
}

func TestMultiHostDynamicFIFOsAndEmptyBootHotplug(t *testing.T) {
	// Verify Agilex 5 1024-word FIFO partitioning and zero-device boot!
	sim := newMultiChannelSim(1024, 16)
	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti empty boot failed: %v", err)
	}
	if host.Channels() != 16 || host.FIFOWords() != 1024 {
		t.Fatalf("channels=%d fifo=%d", host.Channels(), host.FIFOWords())
	}
	if sim.regs[rxsize] != 512 || sim.regs[nptxsize] != (256<<16|512) || sim.regs[ptxsize] != (256<<16|768) {
		t.Fatalf("unexpected FIFO partition rx=%#x nptx=%#x ptx=%#x", sim.regs[rxsize], sim.regs[nptxsize], sim.regs[ptxsize])
	}
	if devs := host.Devices(); len(devs) != 0 {
		t.Fatalf("expected 0 devices at empty boot, got %d", len(devs))
	}

	// Now hotplug a keyboard directly into the root port!
	kbd := newSimKeyboard()
	sim.attachRoot(kbd)
	if err := host.Service(); err != nil {
		t.Fatalf("Service on root attach: %v", err)
	}
	ev, ok := host.NextEvent()
	if !ok || ev.Kind != usb.DeviceAttached || ev.Device.Descriptor.VendorID != 0x1234 {
		t.Fatalf("expected root DeviceAttached, got ok=%t ev=%+v", ok, ev)
	}

	// Unplug from root port -> clean DeviceDetached, host stays alive!
	sim.detachRoot()
	if err := host.Service(); err != nil {
		t.Fatalf("Service on root detach: %v", err)
	}
	ev, ok = host.NextEvent()
	if !ok || ev.Kind != usb.DeviceDetached || len(host.Devices()) != 0 {
		t.Fatalf("expected DeviceDetached, got ok=%t ev=%+v devs=%d", ok, ev, len(host.Devices()))
	}
}

func TestMultiHostConcurrentBTDongleGamepadKeyboardAndHubHotplug(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	hub := newSimHub(3, true)
	bt := newSimBTDongle()
	pad := newSimGamepad()
	kbd := newSimKeyboard()
	sim.attachRoot(hub)
	sim.attachHubPort(hub, 1, bt)
	sim.attachHubPort(hub, 2, pad)
	sim.attachHubPort(hub, 3, kbd)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	if !hub.multiTTSet {
		t.Fatal("expected Multi-TT SET_INTERFACE alt=1 on HS protocol-2 hub")
	}
	if len(host.Devices()) != 3 {
		t.Fatalf("expected 3 non-hub devices, got %d", len(host.Devices()))
	}

	// Drain initial attach events and locate addresses.
	var btDev, padDev, kbdDev usb.DeviceInfo
	for range 3 {
		ev, ok := host.NextEvent()
		if !ok || ev.Kind != usb.DeviceAttached {
			t.Fatalf("missing attach event: ok=%t ev=%+v", ok, ev)
		}
		switch ev.Device.Descriptor.VendorID {
		case 0x0bda:
			btDev = ev.Device
		case 0x054c:
			padDev = ev.Device
		case 0x1234:
			kbdDev = ev.Device
		}
	}
	if btDev.Address == 0 || padDev.Address == 0 || kbdDev.Address == 0 {
		t.Fatalf("missing device addresses bt=%d pad=%d kbd=%d", btDev.Address, padDev.Address, kbdDev.Address)
	}

	// 1. Open BT HCI pipes: EP1 Int-IN (Events), EP2 Bulk-IN (512B ACL RX), EP2 Bulk-OUT (512B ACL TX).
	hciIf, _ := btDev.Config.FindInterface(0xe0, 0x01, 0x01)
	epEv, _ := hciIf.FindEndpoint(usb.TransferInterrupt, true)
	epAclIn, _ := hciIf.FindEndpoint(usb.TransferBulk, true)
	epAclOut, _ := hciIf.FindEndpoint(usb.TransferBulk, false)

	btEvPipe, err := host.OpenPipe(btDev.Address, epEv)
	if err != nil {
		t.Fatalf("OpenPipe BT Event: %v", err)
	}
	btAclInPipe, err := host.OpenPipe(btDev.Address, epAclIn)
	if err != nil {
		t.Fatalf("OpenPipe BT ACL IN: %v", err)
	}
	btAclOutPipe, err := host.OpenPipe(btDev.Address, epAclOut)
	if err != nil {
		t.Fatalf("OpenPipe BT ACL OUT: %v", err)
	}

	// Send HCI_Reset command over EP0 Control OUT (bmRequestType=0x20, DATA OUT)!
	hciReset := []byte{0x03, 0x0c, 0x00}
	if n, err := host.ControlTransfer(btDev.Address, 0x20, 0x00, 0, 0, hciReset); err != nil || n != 3 {
		t.Fatalf("HCI Control OUT failed: n=%d err=%v", n, err)
	}
	if len(bt.outLog[0]) != 1 || !bytes.Equal(bt.outLog[0][0], hciReset) {
		t.Fatalf("BT dongle did not receive HCI command: %x", bt.outLog[0])
	}

	// 2. Open Gamepad (1000 Hz override!) Int-IN + Int-OUT and Keyboard Int-IN simultaneously!
	padIf := padDev.Config.Interfaces[0]
	padEpIn, _ := padIf.FindEndpoint(usb.TransferInterrupt, true)
	padEpOut, _ := padIf.FindEndpoint(usb.TransferInterrupt, false)
	padInPipe, err := host.OpenPipe(padDev.Address, padEpIn)
	if err != nil {
		t.Fatalf("OpenPipe Pad IN: %v", err)
	}
	padInPipe.SetIntervalOverride(time.Millisecond)
	padOutPipe, err := host.OpenPipe(padDev.Address, padEpOut)
	if err != nil {
		t.Fatalf("OpenPipe Pad OUT: %v", err)
	}
	kbdEpIn, _ := kbdDev.Config.Interfaces[0].FindEndpoint(usb.TransferInterrupt, true)
	kbdInPipe, err := host.OpenPipe(kbdDev.Address, kbdEpIn)
	if err != nil {
		t.Fatalf("OpenPipe Kbd IN: %v", err)
	}

	// Queue simultaneous traffic on ALL pipes: 512-byte HS Bulk ACL IN/OUT + BT Event + Gamepad IN/OUT + Keyboard IN!
	aclRxWant := make([]byte, 512)
	aclTxWant := make([]byte, 512)
	for i := range aclRxWant {
		aclRxWant[i] = byte(i ^ 0x5a)
		aclTxWant[i] = byte(i ^ 0xa5)
	}
	btEvWant := []byte{0x0e, 0x04, 0x01, 0x03, 0x0c, 0x00}
	padReportWant := []byte{0x01, 0x80, 0x80, 0x80, 0x80, 0x08, 0x10, 0x00}
	padOutWant := []byte{0x02, 0xff, 0x01, 0x04}
	kbdReportWant := []byte{0, 0, 4, 0, 0, 0, 0, 0}

	sim.mu.Lock()
	bt.inQueues[1] = append(bt.inQueues[1], btEvWant)
	bt.inQueues[2] = append(bt.inQueues[2], aclRxWant)
	pad.inQueues[1] = append(pad.inQueues[1], padReportWant)
	kbd.inQueues[1] = append(kbd.inQueues[1], kbdReportWant)
	sim.mu.Unlock()

	if err := btAclOutPipe.Write(aclTxWant); err != nil {
		t.Fatalf("btAclOutPipe.Write: %v", err)
	}
	if err := padOutPipe.Write(padOutWant); err != nil {
		t.Fatalf("padOutPipe.Write: %v", err)
	}

	// Service until all concurrent pipes complete (including Multi-TT splits for FS pad & kbd!).
	var gotEv, gotAcl, gotPad, gotKbd []byte
	var buf [512]byte
	for range 32 {
		if err := host.Service(); err != nil {
			t.Fatalf("Service: %v", err)
		}
		if gotEv == nil {
			if n, err := btEvPipe.PollInto(buf[:]); err != nil {
				t.Fatal(err)
			} else if n > 0 {
				gotEv = append([]byte(nil), buf[:n]...)
			}
		}
		if gotAcl == nil {
			if n, err := btAclInPipe.PollInto(buf[:]); err != nil {
				t.Fatal(err)
			} else if n > 0 {
				gotAcl = append([]byte(nil), buf[:n]...)
			}
		}
		if gotPad == nil {
			if n, err := padInPipe.PollInto(buf[:]); err != nil {
				t.Fatal(err)
			} else if n > 0 {
				gotPad = append([]byte(nil), buf[:n]...)
			}
		}
		if gotKbd == nil {
			if n, err := kbdInPipe.PollInto(buf[:]); err != nil {
				t.Fatal(err)
			} else if n > 0 {
				gotKbd = append([]byte(nil), buf[:n]...)
			}
		}
		sim.advance(time.Millisecond)
	}
	if !bytes.Equal(gotEv, btEvWant) || !bytes.Equal(gotAcl, aclRxWant) || !bytes.Equal(gotPad, padReportWant) || !bytes.Equal(gotKbd, kbdReportWant) {
		t.Fatalf("concurrent RX mismatch ev=%d acl=%d pad=%d kbd=%d", len(gotEv), len(gotAcl), len(gotPad), len(gotKbd))
	}
	if ok, err := btAclOutPipe.WriteDone(); !ok || err != nil || len(bt.outLog[2]) != 1 || !bytes.Equal(bt.outLog[2][0], aclTxWant) {
		t.Fatalf("512B BT Bulk OUT failed ok=%t err=%v", ok, err)
	}
	if ok, err := padOutPipe.WriteDone(); !ok || err != nil || len(pad.outLog[2]) != 1 || !bytes.Equal(pad.outLog[2][0], padOutWant) {
		t.Fatalf("Gamepad Int OUT failed ok=%t err=%v", ok, err)
	}

	// 3. Hot-unplug Keyboard on Hub Port 3 via Interrupt-IN hub bitmap!
	// HS hub bInterval=12 is 256ms; advance 330ms across Service ticks.
	// BT Dongle (Port 1) and Gamepad (Port 2) must keep working uninterrupted!
	sim.detachHubPort(hub, 3)
	for range 22 {
		sim.advance(15 * time.Millisecond)
		if err := host.Service(); err != nil {
			t.Fatalf("Service during hub detach: %v", err)
		}
	}
	ev, ok := host.NextEvent()
	if !ok || ev.Kind != usb.DeviceDetached || ev.Device.Address != kbdDev.Address {
		t.Fatalf("expected keyboard DeviceDetached, got ok=%t ev=%+v", ok, ev)
	}
	if _, err := kbdInPipe.PollInto(buf[:]); err == nil {
		t.Fatal("detached keyboard pipe did not return error")
	}

	// Verify BT ACL RX STILL works right after keyboard unplug!
	sim.mu.Lock()
	bt.inQueues[2] = append(bt.inQueues[2], aclRxWant)
	sim.mu.Unlock()
	gotAcl = nil
	for range 8 {
		sim.advance(time.Millisecond)
		if err := host.Service(); err != nil {
			t.Fatal(err)
		}
		if n, err := btAclInPipe.PollInto(buf[:]); err != nil {
			t.Fatal(err)
		} else if n > 0 {
			gotAcl = append([]byte(nil), buf[:n]...)
			break
		}
	}
	if !bytes.Equal(gotAcl, aclRxWant) {
		t.Fatal("BT ACL pipe disrupted by sibling keyboard detach")
	}

	// 4. Per-device fault isolation: STALL on Gamepad EP1 faults ONLY padInPipe,
	// while BT Dongle keeps running cleanly!
	sim.mu.Lock()
	pad.stallEP[1] = true
	bt.inQueues[1] = append(bt.inQueues[1], btEvWant)
	sim.mu.Unlock()
	for range 12 {
		sim.advance(time.Millisecond)
		if err := host.Service(); err != nil {
			t.Fatalf("per-device STALL retired entire host: %v", err)
		}
	}
	if _, err := padInPipe.PollInto(buf[:]); err == nil {
		t.Fatal("expected STALL fault on padInPipe")
	}
	if n, err := btEvPipe.PollInto(buf[:]); err != nil || n != len(btEvWant) {
		t.Fatalf("BT Event pipe affected by gamepad STALL: n=%d err=%v", n, err)
	}

	// 5. Pipe introspection/Close, Devices(), GICv3 IRQ + Pause/Resume + Guarded/Retire!
	if devs := host.Devices(); len(devs) != 2 {
		t.Fatalf("expected 2 attached non-hub devices after keyboard unplug, got %d", len(devs))
	}
	if btEvPipe.DeviceAddress() != btDev.Address || btEvPipe.Endpoint().Address != 0x81 {
		t.Fatalf("unexpected pipe metadata addr=%d ep=%#x", btEvPipe.DeviceAddress(), btEvPipe.Endpoint().Address)
	}
	if err := padOutPipe.Close(); err != nil {
		t.Fatalf("padOutPipe.Close: %v", err)
	}

	ctrl, ghw := newMockGIC(t)
	if err := host.EnableIRQ(ctrl, testUSBIRQ); err != nil {
		t.Fatalf("EnableIRQ: %v", err)
	}
	if err := host.Service(); err != nil {
		t.Fatalf("Service with IRQ: %v", err)
	}
	if sim.regs[haintmsk] == 0 || sim.regs[gahb] != gahbGlobalIRQ {
		t.Fatalf("unexpected IRQ regs haintmsk=%#x gahb=%#x", sim.regs[haintmsk], sim.regs[gahb])
	}
	ghw.fire(ctrl, testUSBIRQ)
	select {
	case <-host.Ready():
	default:
		t.Fatal("MultiHost IRQ did not signal Ready()")
	}
	if host.IRQCount() != 1 {
		t.Fatalf("IRQCount=%d, want 1", host.IRQCount())
	}
	// Deliver one packet after IRQ to record latency telemetry and verify ResetLatency.
	sim.mu.Lock()
	bt.inQueues[1] = append(bt.inQueues[1], btEvWant)
	sim.mu.Unlock()
	for range 6 {
		sim.advance(time.Millisecond)
		if err := host.Service(); err != nil {
			t.Fatal(err)
		}
		if n, _ := btEvPipe.PollInto(buf[:]); n > 0 {
			break
		}
	}
	if st := host.Latency(); st.Samples == 0 {
		t.Fatalf("expected >=1 latency sample, got %+v", st)
	}
	host.ResetLatency()
	if st := host.Latency(); st.Samples != 0 {
		t.Fatalf("ResetLatency left samples=%d", st.Samples)
	}
	if err := host.PausePolling(context.Background()); err != nil {
		t.Fatalf("PausePolling: %v", err)
	}
	if sim.regs[gahb] != 0 || sim.regs[gintmask] != 0 || sim.regs[haintmsk] != 0 {
		t.Fatalf("PausePolling left IRQ regs set: gahb=%#x gintmask=%#x haintmsk=%#x",
			sim.regs[gahb], sim.regs[gintmask], sim.regs[haintmsk])
	}
	if err := host.ResumePolling(context.Background()); err != nil {
		t.Fatalf("ResumePolling: %v", err)
	}
	if err := host.Service(); err != nil {
		t.Fatalf("Service after ResumePolling: %v", err)
	}
	if sim.regs[gahb] != gahbGlobalIRQ || sim.regs[haintmsk] == 0 {
		t.Fatalf("Service after ResumePolling did not re-enable IRQ: gahb=%#x haintmsk=%#x",
			sim.regs[gahb], sim.regs[haintmsk])
	}
	host.DisableIRQ()

	// Verify EnableIRQGuarded + RetireIRQ.
	allowed := true
	if err := host.EnableIRQGuarded(ctrl, testUSBIRQ, func() bool { return allowed }); err != nil {
		t.Fatalf("EnableIRQGuarded: %v", err)
	}
	allowed = false
	host.RetireIRQ()
	if host.Ready() != nil {
		t.Fatal("Ready() still non-nil after RetireIRQ")
	}
}

func TestMultiHostHubHotReplugReusesChannels(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	hub := newSimHub(4, true)
	bt := newSimBTDongle()
	kbd := newSimKeyboard()
	sim.attachRoot(hub)
	sim.attachHubPort(hub, 1, bt)
	sim.attachHubPort(hub, 3, kbd)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	var btDev, kbdDev usb.DeviceInfo
	for {
		ev, ok := host.NextEvent()
		if !ok {
			break
		}
		if ev.Kind == usb.DeviceAttached {
			switch ev.Device.Descriptor.VendorID {
			case 0x0bda:
				btDev = ev.Device
			case 0x1234:
				kbdDev = ev.Device
			}
		}
	}
	if btDev.Address == 0 || kbdDev.Address == 0 {
		t.Fatalf("missing initial devices bt=%d kbd=%d", btDev.Address, kbdDev.Address)
	}
	hciIf, _ := btDev.Config.FindInterface(0xe0, 0x01, 0x01)
	btEvEP, _ := hciIf.FindEndpoint(usb.TransferInterrupt, true)
	btPipe, err := host.OpenPipe(btDev.Address, btEvEP)
	if err != nil {
		t.Fatalf("OpenPipe(BT): %v", err)
	}
	kbdIf, _ := kbdDev.Config.FindInterface(3, 1, 1)
	kbdEP, _ := kbdIf.FindEndpoint(usb.TransferInterrupt, true)
	kbdPipe, err := host.OpenPipe(kbdDev.Address, kbdEP)
	if err != nil {
		t.Fatalf("OpenPipe(KBD): %v", err)
	}
	kbdCh := kbdPipe.(*multiPipe).ch

	// Unplug keyboard on port 3.
	sim.detachHubPort(hub, 3)
	for range 22 {
		sim.advance(15 * time.Millisecond)
		if err := host.Service(); err != nil {
			t.Fatal(err)
		}
	}
	ev, ok := host.NextEvent()
	if !ok || ev.Kind != usb.DeviceDetached || ev.Device.Address != kbdDev.Address {
		t.Fatalf("expected keyboard detach, got ok=%t ev=%+v", ok, ev)
	}

	// Hot-replug a DualSense gamepad onto the same hub port 3!
	pad := newSimGamepad()
	sim.attachHubPort(hub, 3, pad)
	for range 22 {
		sim.advance(15 * time.Millisecond)
		if err := host.Service(); err != nil {
			t.Fatal(err)
		}
	}
	ev, ok = host.NextEvent()
	if !ok || ev.Kind != usb.DeviceAttached || ev.Device.Descriptor.VendorID != 0x054c {
		t.Fatalf("expected gamepad hot-replug DeviceAttached, got ok=%t ev=%+v", ok, ev)
	}
	padIf, _ := ev.Device.Config.FindInterface(3, -1, -1)
	padEP, _ := padIf.FindEndpoint(usb.TransferInterrupt, true)
	padPipe, err := host.OpenPipe(ev.Device.Address, padEP)
	if err != nil {
		t.Fatalf("OpenPipe(replugged gamepad): %v", err)
	}
	if padPipe.(*multiPipe).ch != kbdCh {
		t.Fatalf("replugged pipe got ch %d, expected reused freed ch %d", padPipe.(*multiPipe).ch, kbdCh)
	}

	// Both the newly replugged gamepad and the original BT dongle deliver reports concurrently.
	padWant := []byte{1, 2, 3, 4}
	btWant := []byte{0x0e, 0x04, 0x01, 0x03, 0x0c, 0x00}
	sim.mu.Lock()
	pad.inQueues[1] = append(pad.inQueues[1], padWant)
	bt.inQueues[1] = append(bt.inQueues[1], btWant)
	sim.mu.Unlock()
	var buf [64]byte
	var gotPad, gotBT []byte
	for range 12 {
		sim.advance(time.Millisecond)
		if err := host.Service(); err != nil {
			t.Fatal(err)
		}
		if gotPad == nil {
			if n, err := padPipe.PollInto(buf[:]); err != nil {
				t.Fatal(err)
			} else if n > 0 {
				gotPad = append([]byte(nil), buf[:n]...)
			}
		}
		if gotBT == nil {
			if n, err := btPipe.PollInto(buf[:]); err != nil {
				t.Fatal(err)
			} else if n > 0 {
				gotBT = append([]byte(nil), buf[:n]...)
			}
		}
	}
	if !bytes.Equal(gotPad, padWant) || !bytes.Equal(gotBT, btWant) {
		t.Fatalf("post-replug RX mismatch pad=%x bt=%x", gotPad, gotBT)
	}
}

func TestMultiHostServiceAndPipesZeroAllocs(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	sim.noOutLog = true
	bt := newSimBTDongle()
	sim.attachRoot(bt)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	ev, ok := host.NextEvent()
	if !ok || ev.Kind != usb.DeviceAttached {
		t.Fatalf("missing BT attach event")
	}
	hciIf, _ := ev.Device.Config.FindInterface(0xe0, 0x01, 0x01)
	intInEP, _ := hciIf.FindEndpoint(usb.TransferInterrupt, true)
	bulkInEP, _ := hciIf.FindEndpoint(usb.TransferBulk, true)
	bulkOutEP, _ := hciIf.FindEndpoint(usb.TransferBulk, false)

	intPipe, err := host.OpenPipe(ev.Device.Address, intInEP)
	if err != nil {
		t.Fatal(err)
	}
	bulkInPipe, err := host.OpenPipe(ev.Device.Address, bulkInEP)
	if err != nil {
		t.Fatal(err)
	}
	bulkOutPipe, err := host.OpenPipe(ev.Device.Address, bulkOutEP)
	if err != nil {
		t.Fatal(err)
	}

	// Preallocate repeating packets so the simulator does not allocate either.
	evPkt := []byte{0x0e, 0x04, 0x01, 0x03, 0x0c, 0x00}
	aclPkt := make([]byte, 512)
	for i := range aclPkt {
		aclPkt[i] = byte(i)
	}
	bt.inRepeat[1] = evPkt
	bt.inRepeat[2] = aclPkt

	var rxBuf [512]byte
	// Warm up one cycle so channels settle.
	for range 4 {
		sim.advance(time.Millisecond)
		if err := host.Service(); err != nil {
			t.Fatal(err)
		}
		_, _ = intPipe.PollInto(rxBuf[:])
		_, _ = bulkInPipe.PollInto(rxBuf[:])
	}

	allocs := testing.AllocsPerRun(50, func() {
		if err := bulkOutPipe.Write(aclPkt); err != nil {
			t.Fatal(err)
		}
		for range 3 {
			sim.advance(time.Millisecond)
			if err := host.Service(); err != nil {
				t.Fatal(err)
			}
			_, _ = intPipe.PollInto(rxBuf[:])
			_, _ = bulkInPipe.PollInto(rxBuf[:])
			if done, err := bulkOutPipe.WriteDone(); err != nil {
				t.Fatal(err)
			} else if done {
				break
			}
		}
	})
	if allocs > 0 {
		t.Fatalf("MultiHost steady-state Service+PollInto+Write allocated %.2f allocs/run, want 0", allocs)
	}
}
