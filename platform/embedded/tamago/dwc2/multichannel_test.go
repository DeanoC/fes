// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
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
	hubStatus    uint32
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
	// Transiently STALL the first N EP0 device-descriptor or SET_INTERFACE requests
	failDescCount     int
	failSetIfaceCount int
	// Count of CLEAR_FEATURE(ENDPOINT_HALT) requests per endpoint address
	clearHaltCount map[uint8]int
	// Reject SET_PORT_FEATURE(PORT_POWER) or GET_PORT_STATUS transiently
	stallPortPower      bool
	failPortPowerCount  int
	failPortStatusCount int
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
	wedgeCh         int // if >= 0 (when wedgeSet), channel wedgeCh refuses to clear CHENA on halt
	wedgeSet        bool
	failRootEnable  bool
	rootResets      int
	freezeFrame        bool
	nyetCSplitCount    int
	nyetCH0SSplitCount int
	nyetBulkOutCount   int
	nyetCtrlOutCount   int
	nakPingCount       int
	pingCount          int
	npBusyReads        int
	ch0ArmedWhenBusy   bool
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
		if !s.freezeFrame {
			s.frame = (s.frame + 1) & 0x3fff
		}
		return v
	case nptxstatus:
		if s.npBusyReads > 0 {
			s.npBusyReads--
			return 0
		}
		return s.regs[reg]
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
			s.rootResets++
			if !s.failRootEnable {
				cur |= 1<<2 | 1 | uint32(s.rootDev.speed)<<17
				s.resetDev = s.rootDev
				s.rootDev.addr = 0
			}
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
		if s.wedgeSet && ch == s.wedgeCh {
			s.regs[chReg(ch, hcchar)] |= channelEnable
			return
		}
		s.regs[chReg(ch, hcchar)] &^= channelEnable | channelDisable
		s.regs[chReg(ch, hcint)] |= 1 << 1
		s.regs[haint] |= 1 << uint(ch)
		return
	}
	if val&channelEnable == 0 {
		return
	}
	if ch == 0 && s.npBusyReads > 0 {
		s.ch0ArmedWhenBusy = true
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

	// USB 2.0 High-Speed PING token (HCTSIZ.DoPng = bit 31).
	if tsiz&hctsizDoPng != 0 {
		s.pingCount++
		s.regs[chReg(ch, hcchar)] &^= channelEnable
		if s.nakPingCount > 0 {
			s.nakPingCount--
			s.regs[chReg(ch, hcint)] |= 1<<4 | 1<<1 // NAK + CHHLTD
		} else {
			s.regs[chReg(ch, hcint)] |= 1<<5 | 1<<1 // ACK + CHHLTD
		}
		s.regs[haint] |= 1 << uint(ch)
		return
	}

	// Optional CSPLIT NYET injection for testing deferred nextFrame backoff.
	if split && compSplit && ch != 0 && s.nyetCSplitCount > 0 {
		s.nyetCSplitCount--
		s.regs[chReg(ch, hcchar)] &^= channelEnable
		s.regs[chReg(ch, hcint)] |= 1<<6 | 1<<1 // NYET + CHHLTD
		s.regs[haint] |= 1 << uint(ch)
		return
	}

	// Optional CH0 SSPLIT NYET injection for testing SSPLIT retry on transient TT busy.
	if split && !compSplit && ch == 0 && s.nyetCH0SSplitCount > 0 {
		s.nyetCH0SSplitCount--
		s.regs[chReg(ch, hcchar)] &^= channelEnable
		s.regs[chReg(ch, hcint)] |= 1<<6 | 1<<1 // NYET + CHHLTD
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
			if s.ctrl.reqType == 0x23 && s.ctrl.req == 3 && s.ctrl.value == 8 && (s.ctrl.dev.stallPortPower || s.ctrl.dev.failPortPowerCount > 0) {
				if s.ctrl.dev.failPortPowerCount > 0 {
					s.ctrl.dev.failPortPowerCount--
				}
				s.ctrl = nil
				s.regs[chReg(0, hcchar)] &^= channelEnable
				s.regs[chReg(0, hcint)] |= 1<<3 | 1<<1 // STALL
				return
			}
			if s.ctrl.reqType == 0x02 && s.ctrl.req == 1 && s.ctrl.value == 0 { // CLEAR_FEATURE(ENDPOINT_HALT)
				if s.ctrl.dev.clearHaltCount == nil {
					s.ctrl.dev.clearHaltCount = map[uint8]int{}
				}
				s.ctrl.dev.clearHaltCount[uint8(s.ctrl.index)]++
			}
			if s.ctrl.reqType == 0x01 && s.ctrl.req == 11 && s.ctrl.value == 1 { // SET_INTERFACE alt=1
				if s.ctrl.dev.failSetIfaceCount > 0 {
					s.ctrl.dev.failSetIfaceCount--
					s.ctrl = nil
					s.regs[chReg(0, hcchar)] &^= channelEnable
					s.regs[chReg(0, hcint)] |= 1<<3 | 1<<1 // transient STALL
					return
				}
				s.ctrl.dev.multiTTSet = true
			}
			if s.ctrl.length > 0 {
				s.ctrl.dev.outLog[0] = append(s.ctrl.dev.outLog[0], append([]byte(nil), s.ctrl.outBuf...))
			}
			s.ctrl = nil
			s.enqueueRX(0, pid, nil)
			if !(s.wedgeSet && s.wedgeCh == 0) {
				s.regs[chReg(ch, hcchar)] &^= channelEnable
			}
			s.regs[chReg(ch, hcint)] |= 1 | 1<<1
			return
		}
		if s.ctrl.reqType == 0xa3 && s.ctrl.req == 0 && s.ctrl.dev.failPortStatusCount > 0 {
			s.ctrl.dev.failPortStatusCount--
			s.ctrl = nil
			s.regs[chReg(0, hcchar)] &^= channelEnable
			s.regs[chReg(0, hcint)] |= 1<<3 | 1<<1 // transient STALL
			return
		}
		if s.ctrl.reqType&0x80 != 0 && s.ctrl.req == 6 && s.ctrl.value>>8 == 1 && s.ctrl.dev.failDescCount > 0 {
			s.ctrl.dev.failDescCount--
			s.ctrl = nil
			s.regs[chReg(0, hcchar)] &^= channelEnable
			s.regs[chReg(0, hcint)] |= 1<<3 | 1<<1 // transient STALL
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
		// Real USB 2.0 hub status-change endpoint reports bit 0 (C_HUB_*) and live C_PORT_* bits.
		var bitmap byte
		if dev.hubStatus&(0x3<<16) != 0 {
			bitmap |= 1
		}
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
			case reqType == 0xa0 && req == 0: // GET_HUB_STATUS
				var b [4]byte
				binary.LittleEndian.PutUint32(b[:], dev.hubStatus)
				cs.resp = b[:]
			case reqType == 0x20 && req == 1: // CLEAR_HUB_FEATURE
				if val <= 1 {
					dev.hubStatus &^= 1 << (16 + val)
				}
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
		} else if pid != 3 && s.nyetCtrlOutCount > 0 {
			s.nyetCtrlOutCount--
			s.regs[chReg(0, hcint)] |= 1<<6 | 1<<1 // HS Control-OUT NYET
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
	} else if s.nyetBulkOutCount > 0 {
		s.nyetBulkOutCount--
		s.regs[chReg(ch, hcint)] |= 1<<6 | 1<<1 // HS Bulk-OUT NYET
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
	var btDev, padDev, kbdDev usb.BusDevice
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
	var btDev, kbdDev usb.BusDevice
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

func TestMultiHostWedgedChannelFailsPauseAndClose(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	bt := newSimBTDongle()
	sim.attachRoot(bt)
	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatal(err)
	}
	ev, _ := host.NextEvent()
	hciIf, _ := ev.Device.Config.FindInterface(0xe0, 0x01, 0x01)
	intEP, _ := hciIf.FindEndpoint(usb.TransferInterrupt, true)
	pipe, err := host.OpenPipe(ev.Device.Address, intEP)
	if err != nil {
		t.Fatal(err)
	}
	ch := pipe.(*multiPipe).ch

	// Simulate hardware leaving CHENA=1 stuck on ch.
	sim.mu.Lock()
	sim.wedgeSet = true
	sim.wedgeCh = ch
	sim.regs[chReg(ch, hcchar)] |= channelEnable
	sim.mu.Unlock()

	if err := host.PausePolling(context.Background()); err == nil {
		t.Fatal("PausePolling succeeded while hardware left CHENA set")
	}
	if host.paused {
		t.Fatal("host marked paused after channel halt failure")
	}

	// Verify Close also fails closed and refuses to clear/reuse a stuck channel slot.
	sim2 := newMultiChannelSim(1024, 16)
	bt2 := newSimBTDongle()
	sim2.attachRoot(bt2)
	host2, err := OpenMulti(sim2, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatal(err)
	}
	ev2, _ := host2.NextEvent()
	pipe2, err := host2.OpenPipe(ev2.Device.Address, intEP)
	if err != nil {
		t.Fatal(err)
	}
	ch2 := pipe2.(*multiPipe).ch
	sim2.mu.Lock()
	sim2.wedgeSet = true
	sim2.wedgeCh = ch2
	sim2.regs[chReg(ch2, hcchar)] |= channelEnable
	sim2.mu.Unlock()
	if err := pipe2.Close(); err == nil {
		t.Fatal("pipe.Close succeeded while hardware left CHENA set")
	}
	if !host2.slots[ch2].allocated || host2.slots[ch2].fault == nil {
		t.Fatal("stuck channel slot was cleared for reuse")
	}

	// Verify CH0 control-completion halt failure latches controller fault instead of returning success.
	sim3 := newMultiChannelSim(1024, 16)
	bt3 := newSimBTDongle()
	sim3.attachRoot(bt3)
	host3, err := OpenMulti(sim3, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatal(err)
	}
	ev3, _ := host3.NextEvent()
	sim3.mu.Lock()
	sim3.wedgeSet = true
	sim3.wedgeCh = 0
	sim3.mu.Unlock()
	if _, err := host3.ControlTransfer(ev3.Device.Address, 0x20, 0x00, 0, 0, []byte{0x03, 0x0c, 0x00}); err == nil {
		t.Fatal("ControlTransfer returned success while CH0 failed to halt")
	}
	if host3.fault == nil {
		t.Fatal("CH0 halt failure did not latch controller fault")
	}
}

func TestMultiHostRootDeviceRetryOnTransientEnumFailure(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	bt := newSimBTDongle()
	bt.failDescCount = 1 // Transient STALL on first boot descriptor read
	sim.attachRoot(bt)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	if _, ok := host.NextEvent(); ok {
		t.Fatal("unexpected attach event before transient failure cleared")
	}
	var attached bool
	for range 20 {
		sim.advance(25 * time.Millisecond)
		if err := host.Service(); err != nil {
			t.Fatal(err)
		}
		if ev, ok := host.NextEvent(); ok && ev.Kind == usb.DeviceAttached && ev.Device.Descriptor.VendorID == 0x0bda {
			attached = true
			break
		}
	}
	if !attached {
		t.Fatal("directly attached root device was not retried after transient descriptor failure")
	}
}

func TestMultiHostHotplugRetryAndMaxHubDepth(t *testing.T) {
	// 1. Transient first-attempt descriptor STALL on a hot-plugged hub child must retry and succeed.
	sim := newMultiChannelSim(1024, 16)
	hub1 := newSimHub(4, true)
	sim.attachRoot(hub1)
	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatal(err)
	}
	pad := newSimGamepad()
	pad.failDescCount = 1 // Fail first GET_DESCRIPTOR(Device) transiently
	sim.attachHubPort(hub1, 1, pad)
	var attached bool
	for range 40 {
		sim.advance(25 * time.Millisecond)
		if err := host.Service(); err != nil {
			t.Fatal(err)
		}
		if ev, ok := host.NextEvent(); ok && ev.Kind == usb.DeviceAttached && ev.Device.Descriptor.VendorID == 0x054c {
			attached = true
			break
		}
	}
	if !attached {
		t.Fatal("hot-plugged gamepad was not retried after transient descriptor failure")
	}

	// 2. Preserve hub topology depth during dynamic hotplug: hot-plugging a hub beyond
	// maxHubDepth (3 levels) must be rejected instead of bypassing the limit.
	hub2 := newSimHub(2, true)
	sim.attachHubPort(hub1, 2, hub2)
	for range 25 {
		sim.advance(20 * time.Millisecond)
		if err := host.Service(); err != nil {
			t.Fatal(err)
		}
	}
	hub3 := newSimHub(2, true)
	sim.attachHubPort(hub2, 1, hub3)
	for range 25 {
		sim.advance(20 * time.Millisecond)
		if err := host.Service(); err != nil {
			t.Fatal(err)
		}
	}
	if len(host.hubs) != 2 {
		// Depth 0 = hub1, Depth 1 = hub2 -> hub3 is at depth 2 (< maxHubDepth=3), check exact count:
		if len(host.hubs) != 3 {
			t.Fatalf("expected 3 hubs at depths 0,1,2, got %d", len(host.hubs))
		}
	}
	hubsBefore := len(host.hubs)
	hub4 := newSimHub(2, true)
	sim.attachHubPort(hub3, 1, hub4) // Depth 3 == maxHubDepth -> MUST be rejected!
	for range 25 {
		sim.advance(20 * time.Millisecond)
		if err := host.Service(); err != nil {
			t.Fatal(err)
		}
	}
	if len(host.hubs) != hubsBefore {
		t.Fatalf("hub beyond maxHubDepth was admitted during dynamic hotplug: hubs=%d want=%d", len(host.hubs), hubsBefore)
	}
}

func TestMultiHostPausePreservesCompletedPacketAndToggle(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	pad := newSimGamepad()
	sim.attachRoot(pad)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	ev, ok := host.NextEvent()
	if !ok || ev.Kind != usb.DeviceAttached {
		t.Fatalf("expected attached gamepad event, got %+v ok=%v", ev, ok)
	}
	iface, ok := ev.Device.Config.FindInterface(3, -1, -1)
	if !ok {
		t.Fatal("missing HID interface")
	}
	ep, ok := iface.FindEndpoint(usb.TransferInterrupt, true)
	if !ok {
		t.Fatal("missing Interrupt-IN endpoint")
	}
	pipe, err := host.OpenPipe(ev.Device.Address, ep)
	if err != nil {
		t.Fatalf("OpenPipe: %v", err)
	}
	mp := pipe.(*multiPipe)

	// Queue first packet so Service() arms the channel, hardware immediately posts
	// the DATA0 packet into RX FIFO + HCINT.XFERCOMPL, and PausePolling runs
	// before a second Service() processes it.
	pkt1 := []byte{0x01, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77}
	pad.inQueues[ep.Number()] = append(pad.inQueues[ep.Number()], pkt1)
	if err := host.Service(); err != nil {
		t.Fatalf("Service (arm): %v", err)
	}
	if !host.slots[mp.ch].armed {
		t.Fatal("expected channel to be armed before PausePolling")
	}

	if err := host.PausePolling(context.Background()); err != nil {
		t.Fatalf("PausePolling: %v", err)
	}
	if got := host.slots[mp.ch].toggle; got != 2 {
		t.Fatalf("PausePolling failed to advance DATA toggle: got %d want 2 (DATA1)", got)
	}

	var buf [64]byte
	n, err := pipe.PollInto(buf[:])
	if err != nil {
		t.Fatalf("PollInto after pause: %v", err)
	}
	if n != len(pkt1) || string(buf[:n]) != string(pkt1) {
		t.Fatalf("lost or corrupted packet across PausePolling: got %x want %x", buf[:n], pkt1)
	}

	if err := host.ResumePolling(context.Background()); err != nil {
		t.Fatalf("ResumePolling: %v", err)
	}
	// Next packet on the wire uses DATA1; if toggle had desynchronized, drainRXIntoSlots
	// would reject it with an invalid dpid fault.
	pkt2 := []byte{0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x00}
	pad.inQueues[ep.Number()] = append(pad.inQueues[ep.Number()], pkt2)
	for range 5 {
		sim.advance(5 * time.Millisecond)
		if err := host.Service(); err != nil {
			t.Fatalf("Service after resume: %v", err)
		}
		n, err = pipe.PollInto(buf[:])
		if err != nil {
			t.Fatalf("PollInto after resume: %v", err)
		}
		if n > 0 {
			break
		}
	}
	if n != len(pkt2) || string(buf[:n]) != string(pkt2) {
		t.Fatalf("post-resume packet mismatch: got %x want %x", buf[:n], pkt2)
	}
}

func TestMultiHostHubStatusPipeRecovery(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	hub := newSimHub(4, true)
	pad := newSimGamepad()
	sim.attachRoot(hub)
	sim.attachHubPort(hub, 1, pad)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	for {
		if _, ok := host.NextEvent(); !ok {
			break
		}
	}
	if len(host.hubs) != 1 || host.hubs[0].statusPipe == nil {
		t.Fatalf("expected 1 hub with active statusPipe, got hubs=%d", len(host.hubs))
	}
	origPipe := host.hubs[0].statusPipe
	origGen := origPipe.gen

	// Inject a fatal error on the hub status pipe channel so it faults on Service().
	host.slots[origPipe.ch].fault = errors.New("injected hub status pipe error")
	if err := host.Service(); err != nil {
		t.Fatalf("Service on faulted hub status pipe: %v", err)
	}
	if host.hubs[0].statusPipe != nil {
		t.Fatal("expected pollHubHotplug to close faulted statusPipe before backoff retry")
	}

	// Advance past hubEnumRetryDelay and verify the status pipe is re-established.
	sim.advance(300 * time.Millisecond)
	if err := host.Service(); err != nil {
		t.Fatalf("Service (reopen): %v", err)
	}
	if host.hubs[0].statusPipe == nil {
		t.Fatal("expected hub statusPipe to be re-opened after retry delay")
	}
	if host.hubs[0].statusPipe.gen <= origGen {
		t.Fatalf("expected fresh pipe generation > %d, got %d", origGen, host.hubs[0].statusPipe.gen)
	}
	if hub.clearHaltCount[0x81] != 1 {
		t.Fatalf("expected CLEAR_FEATURE(ENDPOINT_HALT) on status endpoint 0x81 to resync DATA0 toggle before reopening, got %d", hub.clearHaltCount[0x81])
	}

	// Verify downstream hot-unplug still fires through the recovered status pipe.
	sim.detachHubPort(hub, 1)
	var detached bool
	for range 20 {
		sim.advance(10 * time.Millisecond)
		if err := host.Service(); err != nil {
			t.Fatalf("Service (detach): %v", err)
		}
		if ev, ok := host.NextEvent(); ok && ev.Kind == usb.DeviceDetached {
			detached = true
			break
		}
	}
	if !detached {
		t.Fatal("downstream detach not detected after hub status pipe recovery")
	}
}

func TestMultiHostStalePipeGenerationRejected(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	hub := newSimHub(4, true)
	pad1 := newSimGamepad()
	sim.attachRoot(hub)
	sim.attachHubPort(hub, 1, pad1)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	var padID uint8
	var ep usb.EndpointDescriptor
	for {
		ev, ok := host.NextEvent()
		if !ok {
			break
		}
		if ev.Kind == usb.DeviceAttached && ev.Device.Descriptor.VendorID == 0x054c {
			padID = ev.Device.Address
			iface, _ := ev.Device.Config.FindInterface(3, -1, -1)
			ep, _ = iface.FindEndpoint(usb.TransferInterrupt, true)
		}
	}
	stalePipe, err := host.OpenPipe(padID, ep)
	if err != nil {
		t.Fatalf("OpenPipe(pad1): %v", err)
	}
	staleCh := stalePipe.(*multiPipe).ch

	// Detach pad1 and force address allocation to wrap back to padID so the new
	// device gets the exact same (ch, ownerAddr) tuple with a new slot generation.
	sim.detachHubPort(hub, 1)
	for range 25 {
		sim.advance(25 * time.Millisecond)
		if err := host.Service(); err != nil {
			t.Fatal(err)
		}
		if ev, ok := host.NextEvent(); ok && ev.Kind == usb.DeviceDetached && ev.Device.Address == padID {
			break
		}
	}
	host.nextID = padID

	pad2 := newSimGamepad()
	sim.attachHubPort(hub, 1, pad2)
	var pad2ID uint8
	for range 25 {
		sim.advance(25 * time.Millisecond)
		if err := host.Service(); err != nil {
			t.Fatal(err)
		}
		if ev, ok := host.NextEvent(); ok && ev.Kind == usb.DeviceAttached && ev.Device.Descriptor.VendorID == 0x054c {
			pad2ID = ev.Device.Address
			break
		}
	}
	if pad2ID != padID {
		t.Fatalf("test setup expected wrapped addr %d, got %d", padID, pad2ID)
	}
	livePipe, err := host.OpenPipe(pad2ID, ep)
	if err != nil {
		t.Fatalf("OpenPipe(pad2): %v", err)
	}
	if livePipe.(*multiPipe).ch != staleCh {
		t.Fatalf("test setup expected reused channel %d, got %d", staleCh, livePipe.(*multiPipe).ch)
	}

	var buf [64]byte
	if _, err := stalePipe.PollInto(buf[:]); err == nil {
		t.Fatal("stalePipe.PollInto succeeded against reused channel/address")
	}
	if err := stalePipe.Close(); err != nil {
		t.Fatalf("stalePipe.Close returned unexpected error: %v", err)
	}
	// Closing the stale pipe must NOT have disturbed livePipe.
	if !host.slots[staleCh].allocated || host.slots[staleCh].gen != livePipe.(*multiPipe).gen {
		t.Fatal("stalePipe.Close disturbed live reused channel slot")
	}
	pkt := []byte{0x05, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}
	pad2.inQueues[ep.Number()] = append(pad2.inQueues[ep.Number()], pkt)
	var gotN int
	for range 40 {
		sim.advance(2 * time.Millisecond)
		if err := host.Service(); err != nil {
			t.Fatal(err)
		}
		gotN, err = livePipe.PollInto(buf[:])
		if err != nil {
			t.Fatalf("livePipe.PollInto: %v", err)
		}
		if gotN > 0 {
			break
		}
	}
	if gotN != len(pkt) || string(buf[:gotN]) != string(pkt) {
		t.Fatalf("livePipe did not receive packet after stalePipe.Close: got %x want %x", buf[:gotN], pkt)
	}
}

func TestMultiHostPauseFinishesAckedSplit(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	hub := newSimHub(4, true)
	pad := newSimGamepad()
	sim.attachRoot(hub)
	sim.attachHubPort(hub, 1, pad)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	var padAddr uint8
	var ep usb.EndpointDescriptor
	for {
		ev, ok := host.NextEvent()
		if !ok {
			break
		}
		if ev.Kind == usb.DeviceAttached && ev.Device.Descriptor.VendorID == 0x054c {
			padAddr = ev.Device.Address
			iface, _ := ev.Device.Config.FindInterface(3, -1, -1)
			ep, _ = iface.FindEndpoint(usb.TransferInterrupt, true)
		}
	}
	pipe, err := host.OpenPipe(padAddr, ep)
	if err != nil {
		t.Fatalf("OpenPipe: %v", err)
	}
	mp := pipe.(*multiPipe)

	// Queue a report on the hub-attached FS gamepad, align the frame so SSPLIT
	// starts immediately, and run one Service() so SSPLIT is ACKed on the wire
	// while s.armed is still true (or promoted to CSPLIT) right before PausePolling.
	pkt1 := []byte{0x01, 0xde, 0xad, 0xbe, 0xef, 0x01, 0x02, 0x03}
	pad.inQueues[ep.Number()] = append(pad.inQueues[ep.Number()], pkt1)
	sim.mu.Lock()
	sim.frame = 0 // microframe 0 -> wire 1 <= 5, SSPLIT arms immediately
	sim.mu.Unlock()
	if err := host.Service(); err != nil {
		t.Fatalf("Service (arm SSPLIT): %v", err)
	}
	if !host.slots[mp.ch].armed || host.slots[mp.ch].completeSplit {
		t.Fatalf("expected channel armed on SSPLIT before pause: armed=%v completeSplit=%v", host.slots[mp.ch].armed, host.slots[mp.ch].completeSplit)
	}

	// PausePolling must finish the ACKed split's CSPLIT, preserve the packet,
	// advance DATA toggle, and leave all channels halted.
	if err := host.PausePolling(context.Background()); err != nil {
		t.Fatalf("PausePolling: %v", err)
	}
	if host.slots[mp.ch].completeSplit || host.slots[mp.ch].waitingSplit {
		t.Fatalf("expected split phase finished before pause completed: completeSplit=%v waitingSplit=%v", host.slots[mp.ch].completeSplit, host.slots[mp.ch].waitingSplit)
	}
	if got := host.slots[mp.ch].toggle; got != 2 {
		t.Fatalf("expected DATA1 toggle after split pause completion, got %d", got)
	}

	var buf [64]byte
	n, err := pipe.PollInto(buf[:])
	if err != nil {
		t.Fatalf("PollInto after split pause: %v", err)
	}
	if n != len(pkt1) || string(buf[:n]) != string(pkt1) {
		t.Fatalf("split packet lost across PausePolling: got %x want %x", buf[:n], pkt1)
	}

	// Resume and verify the next split transfer completes cleanly on DATA1.
	if err := host.ResumePolling(context.Background()); err != nil {
		t.Fatalf("ResumePolling: %v", err)
	}
	pkt2 := []byte{0x02, 0xca, 0xfe, 0xba, 0xbe, 0x04, 0x05, 0x06}
	pad.inQueues[ep.Number()] = append(pad.inQueues[ep.Number()], pkt2)
	for range 40 {
		sim.advance(2 * time.Millisecond)
		if err := host.Service(); err != nil {
			t.Fatalf("Service after resume: %v", err)
		}
		n, err = pipe.PollInto(buf[:])
		if err != nil {
			t.Fatalf("PollInto after resume: %v", err)
		}
		if n > 0 {
			break
		}
	}
	if n != len(pkt2) || string(buf[:n]) != string(pkt2) {
		t.Fatalf("post-resume split packet mismatch: got %x want %x", buf[:n], pkt2)
	}
}

func TestMultiHostControlTransferValidatesNonCH0RX(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	bt := newSimBTDongle()
	sim.attachRoot(bt)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	ev, ok := host.NextEvent()
	if !ok || ev.Kind != usb.DeviceAttached {
		t.Fatalf("expected BT attach event, got %+v ok=%v", ev, ok)
	}
	hciIf, _ := ev.Device.Config.FindInterface(0xe0, 0x01, 0x01)
	epEv, _ := hciIf.FindEndpoint(usb.TransferInterrupt, true)
	pipe, err := host.OpenPipe(ev.Device.Address, epEv)
	if err != nil {
		t.Fatalf("OpenPipe: %v", err)
	}
	mp := pipe.(*multiPipe)

	// Inject a stale/mismatched RX FIFO entry for mp.ch (which is NOT armed and
	// has wrong DATA1 PID when slot expects DATA0) before a CH0 ControlTransfer.
	sim.mu.Lock()
	sim.enqueueRX(mp.ch, 2, []byte{0xde, 0xad, 0xbe, 0xef})
	sim.mu.Unlock()

	hciReset := []byte{0x03, 0x0c, 0x00}
	if _, err := host.ControlTransfer(ev.Device.Address, 0x20, 0x00, 0, 0, hciReset); err != nil {
		t.Fatalf("ControlTransfer: %v", err)
	}
	if host.slots[mp.ch].received {
		t.Fatal("ch0Transfer accepted unvalidated non-CH0 RX packet into unarmed/wrong-PID slot")
	}
	if host.slots[mp.ch].fault == nil {
		t.Fatal("expected ch0Transfer to fault slot on invalid non-CH0 RX packet")
	}
}

func TestMultiHostIRQKeepsSOFWhenQueueBusy(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	pad := newSimGamepad()
	sim.attachRoot(pad)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	ev, ok := host.NextEvent()
	if !ok || ev.Kind != usb.DeviceAttached {
		t.Fatalf("expected attach event, got %+v ok=%v", ev, ok)
	}
	iface, _ := ev.Device.Config.FindInterface(3, -1, -1)
	ep, _ := iface.FindEndpoint(usb.TransferInterrupt, true)
	pipe, err := host.OpenPipe(ev.Device.Address, ep)
	if err != nil {
		t.Fatalf("OpenPipe: %v", err)
	}
	mp := pipe.(*multiPipe)

	ctrl, _ := newMockGIC(t)
	if err := host.EnableIRQ(ctrl, testUSBIRQ); err != nil {
		t.Fatalf("EnableIRQ: %v", err)
	}
	defer host.DisableIRQ()

	// Make the periodic TX request queue full (0 free slots) while the pipe is
	// due (nextDue <= now): armSlot must leave the slot unarmed, and armMultiIRQ
	// MUST keep gintSOF enabled so the next SOF retries arming!
	sim.mu.Lock()
	savedPTX := sim.regs[ptxstatus]
	sim.regs[ptxstatus] = 0
	sim.mu.Unlock()
	sim.advance(10 * time.Millisecond)

	if err := host.Service(); err != nil {
		t.Fatalf("Service (queue full): %v", err)
	}
	if host.slots[mp.ch].armed {
		t.Fatal("expected slot to remain unarmed while ptxstatus queue is full")
	}
	sim.mu.Lock()
	mask := sim.regs[gintmask]
	sim.mu.Unlock()
	if mask&gintSOF == 0 {
		t.Fatalf("armMultiIRQ failed to keep gintSOF enabled for due-but-unarmed channel: gintmask=%#x", mask)
	}

	// Restore queue capacity and verify the channel arms and delivers a packet.
	pkt := []byte{0x01, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x01}
	sim.mu.Lock()
	sim.regs[ptxstatus] = savedPTX
	pad.inQueues[ep.Number()] = append(pad.inQueues[ep.Number()], pkt)
	sim.mu.Unlock()

	var buf [64]byte
	var gotN int
	for range 5 {
		if err := host.Service(); err != nil {
			t.Fatalf("Service (after queue freed): %v", err)
		}
		gotN, err = pipe.PollInto(buf[:])
		if err != nil {
			t.Fatalf("PollInto: %v", err)
		}
		if gotN > 0 {
			break
		}
	}
	if gotN != len(pkt) || string(buf[:gotN]) != string(pkt) {
		t.Fatalf("expected packet after queue freed: got %x want %x", buf[:gotN], pkt)
	}
}

func TestMultiHostRootPortEnableLossRecovery(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	pad := newSimGamepad()
	sim.attachRoot(pad)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	ev, ok := host.NextEvent()
	if !ok || ev.Kind != usb.DeviceAttached {
		t.Fatalf("expected initial attach event, got %+v ok=%v", ev, ok)
	}
	origAddr := ev.Device.Address

	// Simulate a babble event that disables the root port (PRTENA=0, PRTENCHNG=1)
	// while keeping PRTCONNSTS=1 and PRTCONNDET=0.
	sim.mu.Lock()
	sim.regs[hprt] = (sim.regs[hprt] &^ (1 << 2)) | (1 << 3)
	sim.mu.Unlock()

	if err := host.Service(); err != nil {
		t.Fatalf("Service (enable loss): %v", err)
	}

	// Verify PRTENCHNG was cleared, stale device was detached, and root port was
	// reset and re-enumerated.
	sim.mu.Lock()
	hprtVal := sim.regs[hprt]
	sim.mu.Unlock()
	if hprtVal&(1<<3) != 0 {
		t.Fatalf("PRTENCHNG was not cleared: hprt=%#x", hprtVal)
	}
	if hprtVal&(1<<2) == 0 {
		t.Fatalf("root port was not re-enabled after enable-loss recovery: hprt=%#x", hprtVal)
	}
	var sawDetach, sawReattach bool
	for {
		ev, ok := host.NextEvent()
		if !ok {
			break
		}
		if ev.Kind == usb.DeviceDetached && ev.Device.Address == origAddr {
			sawDetach = true
		}
		if ev.Kind == usb.DeviceAttached && ev.Device.Descriptor.VendorID == 0x054c {
			sawReattach = true
		}
	}
	if !sawDetach || !sawReattach {
		t.Fatalf("expected detach+reattach after PRTENA loss: sawDetach=%v sawReattach=%v", sawDetach, sawReattach)
	}
}

func TestMultiHostPollIntoPreservesPacketWhenBufferTooSmall(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	pad := newSimGamepad()
	sim.attachRoot(pad)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	ev, ok := host.NextEvent()
	if !ok || ev.Kind != usb.DeviceAttached {
		t.Fatalf("expected attach event, got %+v ok=%v", ev, ok)
	}
	iface, _ := ev.Device.Config.FindInterface(3, -1, -1)
	ep, _ := iface.FindEndpoint(usb.TransferInterrupt, true)
	pipe, err := host.OpenPipe(ev.Device.Address, ep)
	if err != nil {
		t.Fatalf("OpenPipe: %v", err)
	}

	pkt := []byte{0x01, 0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70}
	sim.mu.Lock()
	pad.inQueues[ep.Number()] = append(pad.inQueues[ep.Number()], pkt)
	sim.mu.Unlock()
	for range 3 {
		if err := host.Service(); err != nil {
			t.Fatalf("Service: %v", err)
		}
	}

	var small [4]byte
	if n, err := pipe.PollInto(small[:]); err == nil || n != 0 {
		t.Fatalf("expected buffer-too-small error and n=0, got n=%d err=%v", n, err)
	}
	var full [64]byte
	n, err := pipe.PollInto(full[:])
	if err != nil {
		t.Fatalf("PollInto retry with full buffer: %v", err)
	}
	if n != len(pkt) || !bytes.Equal(full[:n], pkt) {
		t.Fatalf("packet not preserved after short-buffer error: got %x want %x", full[:n], pkt)
	}
}

func TestMultiHostRootResetRespectsBackoffAndRetryBound(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	pad := newSimGamepad()
	sim.failRootEnable = true
	sim.attachRoot(pad)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	// OpenMulti's initial enumerateRoot attempted 1 reset and scheduled retry #1.
	if sim.rootResets != 1 {
		t.Fatalf("expected 1 initial reset, got %d", sim.rootResets)
	}
	// Immediate Service() calls before hubEnumRetryDelay must NOT re-reset the port.
	for range 5 {
		sim.advance(5 * time.Millisecond)
		if err := host.Service(); err != nil {
			t.Fatalf("Service (during backoff): %v", err)
		}
	}
	if sim.rootResets != 1 {
		t.Fatalf("root port was reset during backoff window: resets=%d want 1", sim.rootResets)
	}

	// Advancing past backoff retries up to maxHubEnumRetries (3 retries = 4 total attempts),
	// then marks rootExhausted and stops resetting.
	for range 8 {
		sim.advance(150 * time.Millisecond)
		if err := host.Service(); err != nil {
			t.Fatalf("Service (retry): %v", err)
		}
	}
	wantTotal := 1 + maxHubEnumRetries
	if sim.rootResets != wantTotal {
		t.Fatalf("expected bounded %d total reset attempts, got %d", wantTotal, sim.rootResets)
	}
	if !host.rootExhausted {
		t.Fatal("expected rootExhausted=true after retry bound exhausted")
	}
}

func TestMultiHostHubStatusBitZeroClearsAndDetaches(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	hub := newSimHub(4, true)
	pad := newSimGamepad()
	sim.attachRoot(hub)
	sim.attachHubPort(hub, 1, pad)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	var padAddr uint8
	for {
		ev, ok := host.NextEvent()
		if !ok {
			break
		}
		if ev.Kind == usb.DeviceAttached && ev.Device.Descriptor.VendorID == 0x054c {
			padAddr = ev.Device.Address
		}
	}
	if padAddr == 0 {
		t.Fatal("expected gamepad attached behind hub")
	}

	// Assert hub-level global overcurrent (HUB_OVER_CURRENT=bit 1, C_HUB_OVER_CURRENT=bit 17).
	sim.mu.Lock()
	hub.hubStatus = (1 << 1) | (1 << 17)
	sim.mu.Unlock()

	var detached bool
	for range 25 {
		sim.advance(25 * time.Millisecond)
		if err := host.Service(); err != nil {
			t.Fatalf("Service (hub overcurrent): %v", err)
		}
		if ev, ok := host.NextEvent(); ok && ev.Kind == usb.DeviceDetached && ev.Device.Address == padAddr {
			detached = true
			break
		}
	}
	if !detached {
		t.Fatal("downstream gamepad was not detached on hub-level overcurrent (bitmap bit 0)")
	}
	sim.mu.Lock()
	st := hub.hubStatus
	sim.mu.Unlock()
	if st&(1<<17) != 0 {
		t.Fatalf("C_HUB_OVER_CURRENT was not cleared: hubStatus=%#x", st)
	}
}

func TestMultiHostPauseChecksPIOBeforeDraining(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	pad := newSimGamepad()
	sim.attachRoot(pad)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	ev, _ := host.NextEvent()
	iface, _ := ev.Device.Config.FindInterface(3, -1, -1)
	ep, _ := iface.FindEndpoint(usb.TransferInterrupt, true)
	pipe, err := host.OpenPipe(ev.Device.Address, ep)
	if err != nil {
		t.Fatalf("OpenPipe: %v", err)
	}
	mp := pipe.(*multiPipe)

	// Queue an RX entry and mark the channel armed, then simulate a foreign owner
	// enabling DMA before PausePolling runs.
	sim.mu.Lock()
	sim.enqueueRX(mp.ch, 0, []byte{1, 2, 3, 4})
	rxCountBefore := len(sim.rx)
	sim.regs[gahb] |= 1 << 5 // foreign DMAEN
	sim.mu.Unlock()

	if err := host.PausePolling(context.Background()); err == nil {
		t.Fatal("PausePolling succeeded despite foreign DMA enabled")
	}
	sim.mu.Lock()
	rxCountAfter := len(sim.rx)
	sim.mu.Unlock()
	if rxCountAfter != rxCountBefore {
		t.Fatalf("PausePolling drained RX FIFO before pioCheck: rx before=%d after=%d", rxCountBefore, rxCountAfter)
	}
}

func TestMultiHostOpenPipeRejectsMPSExceedingFIFO(t *testing.T) {
	// 256-word minimum profile partitions to rx=128, nptx=64, ptx=64 words.
	sim := newMultiChannelSim(256, 8)
	bt := newSimBTDongle()
	sim.attachRoot(bt)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti(256 words): %v", err)
	}
	ev, ok := host.NextEvent()
	if !ok || ev.Kind != usb.DeviceAttached {
		t.Fatalf("expected BT attach event, got %+v ok=%v", ev, ok)
	}
	hciIf, _ := ev.Device.Config.FindInterface(0xe0, 0x01, 0x01)
	epEv, _ := hciIf.FindEndpoint(usb.TransferInterrupt, true)    // 16B -> fits 128-word RX
	epAclIn, _ := hciIf.FindEndpoint(usb.TransferBulk, true)      // 512B (128+2 > 128) -> must fail
	epAclOut, _ := hciIf.FindEndpoint(usb.TransferBulk, false)    // 512B (128 > 64) -> must fail

	if _, err := host.OpenPipe(ev.Device.Address, epAclOut); err == nil {
		t.Fatal("OpenPipe accepted 512B Bulk-OUT endpoint on 64-word non-periodic TX FIFO")
	}
	if _, err := host.OpenPipe(ev.Device.Address, epAclIn); err == nil {
		t.Fatal("OpenPipe accepted 512B Bulk-IN endpoint exceeding 128-word RX FIFO status+data budget")
	}
	if _, err := host.OpenPipe(ev.Device.Address, epEv); err != nil {
		t.Fatalf("OpenPipe rejected valid 16B Interrupt-IN endpoint: %v", err)
	}
}

func TestMultiHostInitHubAbortsOnMultiTTSelectFailure(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	hub := newSimHub(4, true)
	hub.failSetIfaceCount = 1 // Fail initial SET_INTERFACE(alt=1) transiently
	pad := newSimGamepad()
	sim.attachRoot(hub)
	sim.attachHubPort(hub, 1, pad)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	if len(host.hubs) != 0 {
		t.Fatalf("expected hub setup to abort when Multi-TT SET_INTERFACE fails, got %d hubs", len(host.hubs))
	}
	if _, ok := host.NextEvent(); ok {
		t.Fatal("unexpected child attach event before Multi-TT hub setup succeeded")
	}

	// Retry after backoff should succeed in selecting Multi-TT and enumerating the child.
	var attached bool
	for range 25 {
		sim.advance(25 * time.Millisecond)
		if err := host.Service(); err != nil {
			t.Fatalf("Service (hub retry): %v", err)
		}
		if ev, ok := host.NextEvent(); ok && ev.Kind == usb.DeviceAttached && ev.Device.Descriptor.VendorID == 0x054c {
			attached = true
			break
		}
	}
	if !attached || !hub.multiTTSet {
		t.Fatalf("expected Multi-TT hub retry to succeed: attached=%v multiTTSet=%v", attached, hub.multiTTSet)
	}
}

func TestMultiHostCSPLITNYETHonorsDeferredNextFrame(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	hub := newSimHub(4, true)
	pad := newSimGamepad()
	sim.attachRoot(hub)
	sim.attachHubPort(hub, 1, pad)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	var padAddr uint8
	var ep usb.EndpointDescriptor
	for {
		ev, ok := host.NextEvent()
		if !ok {
			break
		}
		if ev.Kind == usb.DeviceAttached && ev.Device.Descriptor.VendorID == 0x054c {
			padAddr = ev.Device.Address
			iface, _ := ev.Device.Config.FindInterface(3, -1, -1)
			ep, _ = iface.FindEndpoint(usb.TransferInterrupt, true)
		}
	}
	pipe, err := host.OpenPipe(padAddr, ep)
	if err != nil {
		t.Fatalf("OpenPipe: %v", err)
	}
	mp := pipe.(*multiPipe)

	pkt := []byte{0x01, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd}
	sim.mu.Lock()
	pad.inQueues[ep.Number()] = append(pad.inQueues[ep.Number()], pkt)
	// Freeze hfnum at microframe 0 (wire=1 for Interrupt) to arm SSPLIT.
	sim.frame = 0
	sim.freezeFrame = true
	sim.nyetCSplitCount = 1
	sim.mu.Unlock()

	if err := host.Service(); err != nil {
		t.Fatalf("Service (SSPLIT): %v", err)
	}
	// Advance to microframe 2 (wire=3 == startFrame+2) so CSPLIT fires and hits NYET!
	// Because freezeFrame=true, armSlot in the same Service() sees the SAME microframe
	// and MUST NOT immediately re-arm CSPLIT before nextFrame = wire+1.
	sim.mu.Lock()
	sim.frame = 2
	sim.mu.Unlock()
	if err := host.Service(); err != nil {
		t.Fatalf("Service (CSPLIT arm): %v", err)
	}
	if !host.slots[mp.ch].armed || !host.slots[mp.ch].completeSplit {
		t.Fatalf("expected CSPLIT armed on microframe 2: armed=%v completeSplit=%v", host.slots[mp.ch].armed, host.slots[mp.ch].completeSplit)
	}
	if err := host.Service(); err != nil {
		t.Fatalf("Service (CSPLIT NYET): %v", err)
	}
	if host.slots[mp.ch].armed {
		t.Fatal("armSlot re-armed CSPLIT in the same microframe immediately after NYET instead of waiting for nextFrame")
	}
	if !host.slots[mp.ch].waitingSplit || !host.slots[mp.ch].completeSplit {
		t.Fatalf("expected channel waiting for deferred CSPLIT: waitingSplit=%v completeSplit=%v", host.slots[mp.ch].waitingSplit, host.slots[mp.ch].completeSplit)
	}

	// Advance by 1 microframe (frame=3 -> wire=4 == nextFrame): CSPLIT now arms and completes!
	sim.mu.Lock()
	sim.frame = 3
	sim.mu.Unlock()
	if err := host.Service(); err != nil {
		t.Fatalf("Service (CSPLIT retry arm): %v", err)
	}
	if !host.slots[mp.ch].armed {
		t.Fatal("expected CSPLIT to arm once microframe reached nextFrame")
	}
	if err := host.Service(); err != nil {
		t.Fatalf("Service (CSPLIT complete): %v", err)
	}
	var buf [64]byte
	n, err := pipe.PollInto(buf[:])
	if err != nil {
		t.Fatalf("PollInto: %v", err)
	}
	if n != len(pkt) || !bytes.Equal(buf[:n], pkt) {
		t.Fatalf("packet mismatch after CSPLIT NYET backoff: got %x want %x", buf[:n], pkt)
	}
}

func TestMultiHostSingleTTHubSerializesSplits(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	hub := newSimHub(4, false) // Single-TT high-speed hub (bDeviceProtocol == 1)
	pad := newSimGamepad()
	kbd := newSimKeyboard()
	sim.attachRoot(hub)
	sim.attachHubPort(hub, 1, pad)
	sim.attachHubPort(hub, 2, kbd)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	if len(host.hubs) != 1 || !host.hubs[0].singleTT {
		t.Fatalf("expected 1 single-TT hub, got hubs=%d singleTT=%v", len(host.hubs), len(host.hubs) > 0 && host.hubs[0].singleTT)
	}
	var padDev, kbdDev usb.BusDevice
	for {
		ev, ok := host.NextEvent()
		if !ok {
			break
		}
		if ev.Kind == usb.DeviceAttached {
			switch ev.Device.Descriptor.VendorID {
			case 0x054c:
				padDev = ev.Device
			case 0x1234:
				kbdDev = ev.Device
			}
		}
	}
	if padDev.Address == 0 || kbdDev.Address == 0 {
		t.Fatalf("expected both children behind single-TT hub, got pad=%d kbd=%d", padDev.Address, kbdDev.Address)
	}
	padEp, _ := padDev.Config.Interfaces[0].FindEndpoint(usb.TransferInterrupt, true)
	kbdEp, _ := kbdDev.Config.Interfaces[0].FindEndpoint(usb.TransferInterrupt, true)
	padPipe, err := host.OpenPipe(padDev.Address, padEp)
	if err != nil {
		t.Fatalf("OpenPipe(pad): %v", err)
	}
	kbdPipe, err := host.OpenPipe(kbdDev.Address, kbdEp)
	if err != nil {
		t.Fatalf("OpenPipe(kbd): %v", err)
	}

	padPkt := []byte{0x01, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77}
	kbdPkt := []byte{0x00, 0x00, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00}
	sim.mu.Lock()
	pad.inQueues[padEp.Number()] = append(pad.inQueues[padEp.Number()], padPkt)
	kbd.inQueues[kbdEp.Number()] = append(kbd.inQueues[kbdEp.Number()], kbdPkt)
	sim.mu.Unlock()

	var gotPad, gotKbd []byte
	var buf [64]byte
	for range 60 {
		sim.advance(2 * time.Millisecond)
		if err := host.Service(); err != nil {
			t.Fatalf("Service: %v", err)
		}
		// Verify invariant: at most ONE split channel targeting the single-TT hub
		// is ever simultaneously armed or waiting for CSPLIT.
		active := 0
		for ch := 1; ch < host.channels; ch++ {
			s := &host.slots[ch]
			if s.allocated && s.route.hub == uint32(hub.addr) && (s.armed || (s.waitingSplit && s.completeSplit)) {
				active++
			}
		}
		if active > 1 {
			t.Fatalf("single-TT hub had %d concurrent active split channels (must serialize to <= 1)", active)
		}
		if gotPad == nil {
			if n, err := padPipe.PollInto(buf[:]); err != nil {
				t.Fatal(err)
			} else if n > 0 {
				gotPad = append([]byte(nil), buf[:n]...)
			}
		}
		if gotKbd == nil {
			if n, err := kbdPipe.PollInto(buf[:]); err != nil {
				t.Fatal(err)
			} else if n > 0 {
				gotKbd = append([]byte(nil), buf[:n]...)
			}
		}
		if gotPad != nil && gotKbd != nil {
			break
		}
	}
	if !bytes.Equal(gotPad, padPkt) || !bytes.Equal(gotKbd, kbdPkt) {
		t.Fatalf("single-TT serialized delivery failed: pad=%x kbd=%x", gotPad, gotKbd)
	}
}

func TestMultiHostRefusesCH0WhenSingleTTStaysBusy(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	hub := newSimHub(4, false) // Single-TT HS hub
	pad := newSimGamepad()
	sim.attachRoot(hub)
	sim.attachHubPort(hub, 1, pad)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	var padAddr uint8
	var ep usb.EndpointDescriptor
	for {
		ev, ok := host.NextEvent()
		if !ok {
			break
		}
		if ev.Kind == usb.DeviceAttached && ev.Device.Descriptor.VendorID == 0x054c {
			padAddr = ev.Device.Address
			iface, _ := ev.Device.Config.FindInterface(3, -1, -1)
			ep, _ = iface.FindEndpoint(usb.TransferInterrupt, true)
		}
	}
	pipe, err := host.OpenPipe(padAddr, ep)
	if err != nil {
		t.Fatalf("OpenPipe: %v", err)
	}
	mp := pipe.(*multiPipe)

	// Hold the single-TT hub busy with a channel stuck in CSPLIT NYET/frozen frame
	// for the entire 4 ms CH0 drain window so CH0 must refuse rather than collide.
	sim.mu.Lock()
	sim.frame = 0
	sim.freezeFrame = true
	sim.nyetCSplitCount = 100
	sim.mu.Unlock()
	if err := host.Service(); err != nil {
		t.Fatalf("Service (SSPLIT): %v", err)
	}
	sim.mu.Lock()
	sim.frame = 2
	sim.mu.Unlock()
	if err := host.Service(); err != nil {
		t.Fatalf("Service (CSPLIT): %v", err)
	}
	if !host.slots[mp.ch].armed || !host.slots[mp.ch].completeSplit {
		t.Fatalf("expected active CSPLIT holding single TT busy")
	}

	var desc [18]byte
	if _, err := host.ControlTransfer(padAddr, 0x80, 6, 0x0100, 0, desc[:]); err == nil {
		t.Fatal("ControlTransfer proceeded on CH0 while single-TT hub remained busy past 4 ms")
	}
}

func TestMultiHostOpenPipeRejectsDuplicateEndpoint(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	pad := newSimGamepad()
	sim.attachRoot(pad)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	ev, ok := host.NextEvent()
	if !ok || ev.Kind != usb.DeviceAttached {
		t.Fatalf("expected attach event, got %+v ok=%v", ev, ok)
	}
	iface, _ := ev.Device.Config.FindInterface(3, -1, -1)
	ep, _ := iface.FindEndpoint(usb.TransferInterrupt, true)

	p1, err := host.OpenPipe(ev.Device.Address, ep)
	if err != nil {
		t.Fatalf("OpenPipe #1: %v", err)
	}
	if _, err := host.OpenPipe(ev.Device.Address, ep); err == nil {
		t.Fatal("OpenPipe allowed duplicate pipe for already-open (addr, ep.Address)")
	}
	if err := p1.Close(); err != nil {
		t.Fatalf("p1.Close: %v", err)
	}
	p2, err := host.OpenPipe(ev.Device.Address, ep)
	if err != nil {
		t.Fatalf("OpenPipe after Close: %v", err)
	}
	_ = p2.Close()
}

func TestMultiHostCH0WaitsForNonPeriodicQueueSpace(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	bt := newSimBTDongle()
	sim.attachRoot(bt)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	ev, ok := host.NextEvent()
	if !ok || ev.Kind != usb.DeviceAttached {
		t.Fatalf("expected attach event, got %+v ok=%v", ev, ok)
	}

	// Simulate non-periodic request queue/FIFO temporarily full for 3 reads.
	sim.mu.Lock()
	sim.npBusyReads = 3
	sim.ch0ArmedWhenBusy = false
	sim.mu.Unlock()

	hciReset := []byte{0x03, 0x0c, 0x00}
	if n, err := host.ControlTransfer(ev.Device.Address, 0x20, 0x00, 0, 0, hciReset); err != nil || n != 3 {
		t.Fatalf("ControlTransfer after transient nptxstatus busy: n=%d err=%v", n, err)
	}
	sim.mu.Lock()
	armedBusy := sim.ch0ArmedWhenBusy
	rem := sim.npBusyReads
	sim.mu.Unlock()
	if armedBusy {
		t.Fatal("CH0 was armed while GNPTXSTS reported 0 free queue/FIFO space")
	}
	if rem != 0 {
		t.Fatalf("expected all busy reads to be waited out, remaining=%d", rem)
	}
}

func TestMultiHostHubUsesSelectedMultiTTAlternateStatusEndpoint(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	hub := newSimHub(4, true)
	// Give alt 1 (Multi-TT) a distinct Interrupt-IN status endpoint (0x82, interval 8)
	// from alt 0 (0x81, interval 12).
	hub.configDesc = []byte{
		9, 2, 41, 0, 1, 1, 0, 0xe0, 50,
		9, 4, 0, 0, 1, 9, 0, 1, 0,
		7, 5, 0x81, 3, 1, 0, 12,
		9, 4, 0, 1, 1, 9, 0, 2, 0,
		7, 5, 0x82, 3, 1, 0, 8,
	}
	sim.attachRoot(hub)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	if len(host.hubs) != 1 || host.hubs[0].statusPipe == nil {
		t.Fatalf("expected 1 hub with active statusPipe, got hubs=%d", len(host.hubs))
	}
	gotEP := host.hubs[0].statusPipe.Endpoint()
	if gotEP.Address != 0x82 || gotEP.Interval != 8 {
		t.Fatalf("hub status pipe opened alt 0 instead of selected Multi-TT alt 1 endpoint: got addr=%#x interval=%d want addr=0x82 interval=8", gotEP.Address, gotEP.Interval)
	}
}

func TestMultiHostRejectsHubWithoutStatusEndpoint(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	hub := newSimHub(4, false)
	// Hub interface with 0 endpoints (missing Interrupt-IN status-change endpoint).
	hub.configDesc = []byte{
		9, 2, 18, 0, 1, 1, 0, 0xe0, 50,
		9, 4, 0, 0, 0, 9, 0, 1, 0,
	}
	sim.attachRoot(hub)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	if len(host.hubs) != 0 {
		t.Fatalf("expected hub without Interrupt-IN status endpoint to be rejected, got %d hubs", len(host.hubs))
	}
}

func TestMultiHostAlwaysPoweredHubSkipsPortPower(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	hub := newSimHub(4, true)
	// wHubCharacteristics bits 1:0 = 2 (no power switching / always-powered hub)
	hub.hubDesc[3] = 0x02
	hub.stallPortPower = true
	pad := newSimGamepad()
	sim.attachRoot(hub)
	sim.attachHubPort(hub, 1, pad)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	if len(host.hubs) != 1 || host.hubs[0].powerSwitched {
		t.Fatalf("expected 1 always-powered hub (powerSwitched=false), got hubs=%d powerSwitched=%v", len(host.hubs), len(host.hubs) > 0 && host.hubs[0].powerSwitched)
	}
	ev, ok := host.NextEvent()
	if !ok || ev.Kind != usb.DeviceAttached || ev.Device.Descriptor.VendorID != 0x054c {
		t.Fatalf("expected child gamepad attached behind always-powered hub, got ok=%v ev=%+v", ok, ev)
	}
}

func TestMultiHostOpenMultiReturnsFatalFaultDuringInitialEnumeration(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	pad := newSimGamepad()
	sim.attachRoot(pad)
	// Wedge CH0 so initial enumerateRoot hits a fatal controller fault.
	sim.wedgeSet = true
	sim.wedgeCh = 0

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err == nil {
		t.Fatal("OpenMulti succeeded despite fatal controller fault during initial enumeration")
	}
	if host == nil {
		t.Fatal("OpenMulti did not retain partial host when returning fatal enumeration fault")
	}
}

func TestMultiHostCH0RetriesOnSSPLITNYET(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	hub := newSimHub(4, true)
	pad := newSimGamepad()
	sim.attachRoot(hub)
	sim.attachHubPort(hub, 1, pad)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	var padAddr uint8
	for {
		ev, ok := host.NextEvent()
		if !ok {
			break
		}
		if ev.Kind == usb.DeviceAttached && ev.Device.Descriptor.VendorID == 0x054c {
			padAddr = ev.Device.Address
		}
	}
	if padAddr == 0 {
		t.Fatal("expected gamepad attached behind hub")
	}

	// Inject a transient NYET on CH0's SSPLIT; ControlTransfer must retry and succeed.
	sim.mu.Lock()
	sim.nyetCH0SSplitCount = 2
	sim.mu.Unlock()

	var desc [18]byte
	n, err := host.ControlTransfer(padAddr, 0x80, 6, 0x0100, 0, desc[:])
	if err != nil || n != 18 {
		t.Fatalf("ControlTransfer failed after transient CH0 SSPLIT NYET: n=%d err=%v", n, err)
	}
	sim.mu.Lock()
	rem := sim.nyetCH0SSplitCount
	sim.mu.Unlock()
	if rem != 0 {
		t.Fatalf("expected all injected CH0 SSPLIT NYETs to be consumed, remaining=%d", rem)
	}
}

func TestMultiHostServiceDefersCompletionsWhileRXFIFOStillNonEmpty(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	pad := newSimGamepad()
	sim.attachRoot(pad)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	ev, ok := host.NextEvent()
	if !ok || ev.Kind != usb.DeviceAttached {
		t.Fatalf("expected attach event, got %+v ok=%v", ev, ok)
	}
	iface, _ := ev.Device.Config.FindInterface(3, -1, -1)
	ep, _ := iface.FindEndpoint(usb.TransferInterrupt, true)
	pipe, err := host.OpenPipe(ev.Device.Address, ep)
	if err != nil {
		t.Fatalf("OpenPipe: %v", err)
	}
	mp := pipe.(*multiPipe)

	// Arm the channel so hardware queues pkt in RX FIFO and sets HCINT.XFERCOMPL,
	// then prepend 260 non-data RX status markers ahead of pkt so the next
	// Service() hits the 256-entry drain bound while gintRXFLVL stays asserted.
	pkt := []byte{0x01, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88}
	sim.mu.Lock()
	pad.inQueues[ep.Number()] = append(pad.inQueues[ep.Number()], pkt)
	sim.mu.Unlock()
	if err := host.Service(); err != nil {
		t.Fatalf("Service (arm): %v", err)
	}
	if !host.slots[mp.ch].armed {
		t.Fatal("expected channel armed with completed packet in RX FIFO")
	}

	sim.mu.Lock()
	markers := make([]uint32, 260)
	for i := range markers {
		markers[i] = 7<<17 | uint32(mp.ch) // CHHLTD status marker (no payload)
	}
	sim.rx = append(markers, sim.rx[sim.rxHead:]...)
	sim.rxHead = 0
	sim.mu.Unlock()

	// This Service() drains 256 markers and sees gintRXFLVL still set, so it MUST
	// defer channel completion instead of retiring HCINT.XFERCOMPL without payload!
	if err := host.Service(); err != nil {
		t.Fatalf("Service #1 (drain markers): %v", err)
	}
	if !host.slots[mp.ch].armed || host.slots[mp.ch].toggle != 0 {
		t.Fatalf("Service prematurely retired channel before RX FIFO drained: armed=%v toggle=%d", host.slots[mp.ch].armed, host.slots[mp.ch].toggle)
	}
	if host.slots[mp.ch].fault != nil {
		t.Fatalf("unexpected channel fault: %v", host.slots[mp.ch].fault)
	}

	// Next Service() drains the remaining 4 markers + pkt and delivers pkt cleanly.
	if err := host.Service(); err != nil {
		t.Fatalf("Service #2 (finish drain): %v", err)
	}
	var buf [64]byte
	gotN, err := pipe.PollInto(buf[:])
	if err != nil {
		t.Fatalf("PollInto: %v", err)
	}
	if gotN != len(pkt) || !bytes.Equal(buf[:gotN], pkt) {
		t.Fatalf("packet lost or corrupted when RX FIFO exceeded per-call drain bound: got %x want %x", buf[:gotN], pkt)
	}
}

func TestMultiHostHubRecoveryWaitsPowerGoodDelay(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	hub := newSimHub(4, true)
	hub.hubDesc[5] = 60 // bPwrOn2PwrGood = 60 * 2 ms = 120 ms
	pad := newSimGamepad()
	sim.attachRoot(hub)
	sim.attachHubPort(hub, 1, pad)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	for {
		if _, ok := host.NextEvent(); !ok {
			break
		}
	}
	if host.hubs[0].pwrGoodDelay != 120*time.Millisecond {
		t.Fatalf("expected pwrGoodDelay=120ms, got %s", host.hubs[0].pwrGoodDelay)
	}

	// Trigger hub overcurrent to detach child, then recover (C_HUB_OVER_CURRENT=1, HUB_OVER_CURRENT=0).
	sim.mu.Lock()
	hub.hubStatus = (1 << 1) | (1 << 17)
	sim.mu.Unlock()
	for range 25 {
		sim.advance(25 * time.Millisecond)
		if err := host.Service(); err != nil {
			t.Fatal(err)
		}
		if ev, ok := host.NextEvent(); ok && ev.Kind == usb.DeviceDetached {
			break
		}
	}

	// Signal overcurrent cleared; record time before the status bitmap is processed.
	sim.mu.Lock()
	hub.hubStatus = 1 << 17 // C_HUB_OVER_CURRENT set, HUB_OVER_CURRENT cleared
	sim.mu.Unlock()
	var reattached bool
	var maxStep time.Duration
	for range 25 {
		sim.advance(25 * time.Millisecond)
		before := sim.Now()
		if err := host.Service(); err != nil {
			t.Fatal(err)
		}
		if step := sim.Now().Sub(before); step > maxStep {
			maxStep = step
		}
		if ev, ok := host.NextEvent(); ok && ev.Kind == usb.DeviceAttached && ev.Device.Descriptor.VendorID == 0x054c {
			reattached = true
			break
		}
	}
	if !reattached {
		t.Fatal("downstream gamepad did not re-enumerate after hub overcurrent recovery")
	}
	if maxStep < 120*time.Millisecond {
		t.Fatalf("hub recovery did not wait bPwrOn2PwrGood (120ms) after PORT_POWER: maxServiceStep=%s", maxStep)
	}
}

func TestMultiHostHubRecoveryRetriesFailedPortPower(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	hub := newSimHub(4, true)
	pad := newSimGamepad()
	sim.attachRoot(hub)
	sim.attachHubPort(hub, 1, pad)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	for {
		if _, ok := host.NextEvent(); !ok {
			break
		}
	}

	// Trigger hub overcurrent to detach child.
	sim.mu.Lock()
	hub.hubStatus = (1 << 1) | (1 << 17)
	sim.mu.Unlock()
	for range 25 {
		sim.advance(25 * time.Millisecond)
		if err := host.Service(); err != nil {
			t.Fatal(err)
		}
		if ev, ok := host.NextEvent(); ok && ev.Kind == usb.DeviceDetached {
			break
		}
	}

	// Fail the first recovery PORT_POWER transiently; the hub must schedule a
	// bounded repower retry instead of dropping the downstream child.
	sim.mu.Lock()
	hub.failPortPowerCount = 1
	hub.hubStatus = 1 << 17 // C_HUB_OVER_CURRENT set, HUB_OVER_CURRENT cleared
	sim.mu.Unlock()

	var reattached bool
	for range 40 {
		sim.advance(25 * time.Millisecond)
		if err := host.Service(); err != nil {
			t.Fatal(err)
		}
		if ev, ok := host.NextEvent(); ok && ev.Kind == usb.DeviceAttached && ev.Device.Descriptor.VendorID == 0x054c {
			reattached = true
			break
		}
	}
	if !reattached {
		t.Fatal("downstream gamepad was not recovered after transient PORT_POWER failure")
	}
	if host.hubs[0].repowerRetries != 0 {
		t.Fatalf("expected repowerRetries cleared after success, got %d", host.hubs[0].repowerRetries)
	}
}

func TestMultiHostServiceStopsArmingAfterControllerFault(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	bt := newSimBTDongle()
	sim.attachRoot(bt)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	ev, ok := host.NextEvent()
	if !ok || ev.Kind != usb.DeviceAttached {
		t.Fatalf("expected attach event, got %+v ok=%v", ev, ok)
	}
	hciIf, _ := ev.Device.Config.FindInterface(0xe0, 0x01, 0x01)
	ep1, _ := hciIf.FindEndpoint(usb.TransferInterrupt, true)
	ep2, _ := hciIf.FindEndpoint(usb.TransferBulk, true)

	p1, err := host.OpenPipe(ev.Device.Address, ep1)
	if err != nil {
		t.Fatalf("OpenPipe(ch1): %v", err)
	}
	p2, err := host.OpenPipe(ev.Device.Address, ep2)
	if err != nil {
		t.Fatalf("OpenPipe(ch2): %v", err)
	}
	mp1, mp2 := p1.(*multiPipe), p2.(*multiPipe)

	// Make ch1 appear armed with a fatal STALL while wedged so stepChannel(ch1)
	// fails haltChannel and latches h.fault. ch2 is unarmed and due; Service()
	// MUST return h.fault immediately and NEVER arm ch2.
	sim.mu.Lock()
	sim.wedgeSet = true
	sim.wedgeCh = mp1.ch
	sim.regs[chReg(mp1.ch, hcchar)] = channelEnable
	sim.regs[chReg(mp1.ch, hcint)] = 1 << 3 // STALL
	sim.regs[chReg(mp2.ch, hcchar)] = 0
	sim.mu.Unlock()
	host.slots[mp1.ch].armed = true
	host.slots[mp2.ch].armed = false
	host.slots[mp2.ch].nextDue = sim.Now().Add(-time.Millisecond)

	if err := host.Service(); err == nil {
		t.Fatal("Service succeeded despite wedged halt controller fault on ch1")
	}
	if host.slots[mp2.ch].armed {
		t.Fatal("Service armed ch2 after stepChannel(ch1) latched a controller fault")
	}
	sim.mu.Lock()
	ch2HCCHAR := sim.regs[chReg(mp2.ch, hcchar)]
	sim.mu.Unlock()
	if ch2HCCHAR&channelEnable != 0 {
		t.Fatalf("ch2 hardware channel was enabled after controller fault: hcchar=%#x", ch2HCCHAR)
	}
}

func TestMultiHostPausedRejectsPipeCloseAndOpen(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	pad := newSimGamepad()
	sim.attachRoot(pad)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	ev, ok := host.NextEvent()
	if !ok || ev.Kind != usb.DeviceAttached {
		t.Fatalf("expected attach event, got %+v ok=%v", ev, ok)
	}
	iface, _ := ev.Device.Config.FindInterface(3, -1, -1)
	epIn, _ := iface.FindEndpoint(usb.TransferInterrupt, true)
	epOut, _ := iface.FindEndpoint(usb.TransferInterrupt, false)

	pipe, err := host.OpenPipe(ev.Device.Address, epIn)
	if err != nil {
		t.Fatalf("OpenPipe: %v", err)
	}
	mp := pipe.(*multiPipe)

	if err := host.PausePolling(context.Background()); err != nil {
		t.Fatalf("PausePolling: %v", err)
	}
	// While paused, raw Close and OpenPipe MUST fail without touching MMIO or
	// freeing the slot.
	if err := pipe.Close(); err == nil {
		t.Fatal("pipe.Close succeeded while MultiHost polling was paused")
	}
	if !host.slots[mp.ch].allocated {
		t.Fatal("pipe.Close freed channel slot while MultiHost was paused")
	}
	if _, err := host.OpenPipe(ev.Device.Address, epOut); err == nil {
		t.Fatal("OpenPipe succeeded while MultiHost polling was paused")
	}

	// After ResumePolling, closing the pipe succeeds normally.
	if err := host.ResumePolling(context.Background()); err != nil {
		t.Fatalf("ResumePolling: %v", err)
	}
	if err := pipe.Close(); err != nil {
		t.Fatalf("pipe.Close after resume: %v", err)
	}
}

func TestMultiHostRetriesInitialHubPortGetStatusFailure(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	hub := newSimHub(4, true)
	hub.failPortStatusCount = 1 // Fail initial GET_PORT_STATUS on port 1 transiently
	pad := newSimGamepad()
	sim.attachRoot(hub)
	sim.attachHubPort(hub, 1, pad)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	if _, ok := host.NextEvent(); ok {
		t.Fatal("unexpected attach event before transient GET_PORT_STATUS retry")
	}
	var attached bool
	for range 25 {
		sim.advance(25 * time.Millisecond)
		if err := host.Service(); err != nil {
			t.Fatal(err)
		}
		if ev, ok := host.NextEvent(); ok && ev.Kind == usb.DeviceAttached && ev.Device.Descriptor.VendorID == 0x054c {
			attached = true
			break
		}
	}
	if !attached {
		t.Fatal("initially connected hub child was not retried after transient GET_PORT_STATUS failure")
	}
}

func TestMultiHostInterruptOUTPreservesInterval(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	pad := newSimGamepad()
	sim.attachRoot(pad)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	ev, ok := host.NextEvent()
	if !ok || ev.Kind != usb.DeviceAttached {
		t.Fatalf("expected attach event, got %+v ok=%v", ev, ok)
	}
	iface, _ := ev.Device.Config.FindInterface(3, -1, -1)
	epOut, _ := iface.FindEndpoint(usb.TransferInterrupt, false)
	outPipe, err := host.OpenPipe(ev.Device.Address, epOut)
	if err != nil {
		t.Fatalf("OpenPipe(OUT): %v", err)
	}
	mp := outPipe.(*multiPipe)

	// Complete first Interrupt-OUT write.
	if err := outPipe.Write([]byte{0x02, 0x01, 0x02, 0x03}); err != nil {
		t.Fatalf("Write #1: %v", err)
	}
	for range 3 {
		if err := host.Service(); err != nil {
			t.Fatal(err)
		}
		if done, err := outPipe.WriteDone(); err != nil {
			t.Fatal(err)
		} else if done {
			break
		}
	}

	// Immediately submit a second Interrupt-OUT write; it must NOT arm before
	// epOut.Interval (4 ms for FS gamepad) has elapsed!
	if err := outPipe.Write([]byte{0x02, 0x04, 0x05, 0x06}); err != nil {
		t.Fatalf("Write #2: %v", err)
	}
	if err := host.Service(); err != nil {
		t.Fatal(err)
	}
	if host.slots[mp.ch].armed || host.slots[mp.ch].outDone {
		t.Fatalf("Interrupt-OUT armed immediately without honoring bInterval: armed=%v outDone=%v", host.slots[mp.ch].armed, host.slots[mp.ch].outDone)
	}

	// Advance past bInterval and verify Write #2 completes.
	sim.advance(host.slots[mp.ch].effectiveInterval() + time.Millisecond)
	var done2 bool
	for range 3 {
		if err := host.Service(); err != nil {
			t.Fatal(err)
		}
		var err error
		done2, err = outPipe.WriteDone()
		if err != nil {
			t.Fatal(err)
		}
		if done2 {
			break
		}
	}
	if !done2 {
		t.Fatal("Interrupt-OUT Write #2 did not complete after bInterval elapsed")
	}
}

func TestMultiHostHSBulkOUTAndControlOUTHandleNYETWithPing(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	bt := newSimBTDongle() // High-Speed USB 2.0 BT dongle
	sim.attachRoot(bt)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	ev, ok := host.NextEvent()
	if !ok || ev.Kind != usb.DeviceAttached {
		t.Fatalf("expected BT attach event, got %+v ok=%v", ev, ok)
	}
	hciIf, _ := ev.Device.Config.FindInterface(0xe0, 0x01, 0x01)
	epAclOut, _ := hciIf.FindEndpoint(usb.TransferBulk, false)
	outPipe, err := host.OpenPipe(ev.Device.Address, epAclOut)
	if err != nil {
		t.Fatalf("OpenPipe(ACL OUT): %v", err)
	}
	mp := outPipe.(*multiPipe)

	// 1. Bulk-OUT packet #1 receives HS NYET (accepted, triggers doPing=true).
	sim.mu.Lock()
	sim.nyetBulkOutCount = 1
	sim.nakPingCount = 1 // First PING returns NAK, second PING returns ACK
	sim.mu.Unlock()

	pkt1 := []byte{0x01, 0x02, 0x03, 0x04}
	if err := outPipe.Write(pkt1); err != nil {
		t.Fatalf("Write #1: %v", err)
	}
	var done1 bool
	for range 5 {
		if err := host.Service(); err != nil {
			t.Fatalf("Service #1: %v", err)
		}
		done1, err = outPipe.WriteDone()
		if err != nil {
			t.Fatalf("WriteDone #1 (NYET should not fault HS Bulk-OUT): %v", err)
		}
		if done1 {
			break
		}
	}
	if !done1 || !host.slots[mp.ch].doPing {
		t.Fatalf("expected Write #1 done with doPing=true: done=%v doPing=%v", done1, host.slots[mp.ch].doPing)
	}

	// 2. Bulk-OUT packet #2 must PING first (NAK -> retry -> ACK), then send DATA.
	pkt2 := []byte{0x05, 0x06, 0x07, 0x08}
	if err := outPipe.Write(pkt2); err != nil {
		t.Fatalf("Write #2: %v", err)
	}
	var done2 bool
	for range 12 {
		sim.advance(200 * time.Microsecond)
		if err := host.Service(); err != nil {
			t.Fatalf("Service #2: %v", err)
		}
		done2, err = outPipe.WriteDone()
		if err != nil {
			t.Fatalf("WriteDone #2: %v", err)
		}
		if done2 {
			break
		}
	}
	if !done2 || host.slots[mp.ch].doPing {
		t.Fatalf("expected Write #2 done with doPing cleared: done=%v doPing=%v", done2, host.slots[mp.ch].doPing)
	}
	sim.mu.Lock()
	pingsAfterBulk := sim.pingCount
	sim.mu.Unlock()
	if pingsAfterBulk < 2 {
		t.Fatalf("expected >=2 PING tokens (NAK + ACK), got %d", pingsAfterBulk)
	}
	if len(bt.outLog[2]) != 2 || !bytes.Equal(bt.outLog[2][0], pkt1) || !bytes.Equal(bt.outLog[2][1], pkt2) {
		t.Fatalf("unexpected Bulk-OUT log: %x", bt.outLog[2])
	}

	// 3. Multi-packet HS Control-OUT where chunk 1 returns NYET must PING before chunk 2.
	ctrlPayload := make([]byte, 128)
	for i := range ctrlPayload {
		ctrlPayload[i] = byte(i)
	}
	sim.mu.Lock()
	sim.nyetCtrlOutCount = 1
	sim.mu.Unlock()
	n, err := host.ControlTransfer(ev.Device.Address, 0x20, 0x00, 0, 0, ctrlPayload)
	if err != nil || n != len(ctrlPayload) {
		t.Fatalf("HS Control-OUT with NYET failed: n=%d err=%v", n, err)
	}
	sim.mu.Lock()
	pingsAfterCtrl := sim.pingCount
	sim.mu.Unlock()
	if pingsAfterCtrl <= pingsAfterBulk {
		t.Fatalf("expected CH0 to issue PING after HS Control-OUT NYET: before=%d after=%d", pingsAfterBulk, pingsAfterCtrl)
	}
}

func TestMultiHostPausePreservesCSPLITNYET(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	hub := newSimHub(4, true)
	pad := newSimGamepad()
	sim.attachRoot(hub)
	sim.attachHubPort(hub, 1, pad)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	var padAddr uint8
	var ep usb.EndpointDescriptor
	for {
		ev, ok := host.NextEvent()
		if !ok {
			break
		}
		if ev.Kind == usb.DeviceAttached && ev.Device.Descriptor.VendorID == 0x054c {
			padAddr = ev.Device.Address
			iface, _ := ev.Device.Config.FindInterface(3, -1, -1)
			ep, _ = iface.FindEndpoint(usb.TransferInterrupt, true)
		}
	}
	pipe, err := host.OpenPipe(padAddr, ep)
	if err != nil {
		t.Fatalf("OpenPipe: %v", err)
	}
	mp := pipe.(*multiPipe)

	// Arm SSPLIT -> advance to CSPLIT which hits NYET while CHENA is still set
	// so PausePolling Step 2 observes the CSPLIT NYET during haltChannelCapture!
	pkt := []byte{0x01, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x42}
	sim.mu.Lock()
	pad.inQueues[ep.Number()] = append(pad.inQueues[ep.Number()], pkt)
	sim.frame = 0
	sim.freezeFrame = true
	sim.nyetCSplitCount = 1
	sim.mu.Unlock()

	if err := host.Service(); err != nil {
		t.Fatalf("Service (SSPLIT): %v", err)
	}
	sim.mu.Lock()
	sim.frame = 2
	sim.mu.Unlock()
	if err := host.Service(); err != nil {
		t.Fatalf("Service (arm CSPLIT -> NYET): %v", err)
	}
	// Keep CHENA=1 with HCINT=NYET so Step 1 leaves it halting and Step 2's
	// haltChannelCapture observes the concurrent CSPLIT NYET!
	sim.mu.Lock()
	sim.regs[chReg(mp.ch, hcchar)] |= channelEnable
	sim.freezeFrame = false
	sim.mu.Unlock()

	if err := host.PausePolling(context.Background()); err != nil {
		t.Fatalf("PausePolling: %v", err)
	}
	var buf [64]byte
	n, err := pipe.PollInto(buf[:])
	if err != nil {
		t.Fatalf("PollInto after CSPLIT NYET pause: %v", err)
	}
	if n != len(pkt) || !bytes.Equal(buf[:n], pkt) {
		t.Fatalf("PausePolling dropped CSPLIT after concurrent NYET: got %x want %x", buf[:n], pkt)
	}
}

func TestMultiHostOpenPipeChecksPIOBeforeHAINTMSK(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	pad := newSimGamepad()
	sim.attachRoot(pad)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	ev, _ := host.NextEvent()
	iface, _ := ev.Device.Config.FindInterface(3, -1, -1)
	epIn, _ := iface.FindEndpoint(usb.TransferInterrupt, true)

	ctrl, _ := newMockGIC(t)
	if err := host.EnableIRQ(ctrl, testUSBIRQ); err != nil {
		t.Fatalf("EnableIRQ: %v", err)
	}
	defer host.DisableIRQ()

	// Inject foreign DMA before OpenPipe; OpenPipe must fail without writing HAINTMSK.
	sim.mu.Lock()
	haintBefore := sim.regs[haintmsk]
	sim.regs[gahb] |= 1 << 5
	sim.mu.Unlock()

	if _, err := host.OpenPipe(ev.Device.Address, epIn); err == nil {
		t.Fatal("OpenPipe succeeded despite foreign DMA enabled")
	}
	sim.mu.Lock()
	haintAfter := sim.regs[haintmsk]
	sim.mu.Unlock()
	if haintAfter != haintBefore {
		t.Fatalf("OpenPipe wrote HAINTMSK after foreign DMA: before=%#x after=%#x", haintBefore, haintAfter)
	}
}

func TestMultiHostPipeRejectsOperationsAfterControllerFault(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	pad := newSimGamepad()
	sim.attachRoot(pad)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	ev, _ := host.NextEvent()
	iface, _ := ev.Device.Config.FindInterface(3, -1, -1)
	epIn, _ := iface.FindEndpoint(usb.TransferInterrupt, true)
	epOut, _ := iface.FindEndpoint(usb.TransferInterrupt, false)

	inPipe, err := host.OpenPipe(ev.Device.Address, epIn)
	if err != nil {
		t.Fatalf("OpenPipe(IN): %v", err)
	}
	outPipe, err := host.OpenPipe(ev.Device.Address, epOut)
	if err != nil {
		t.Fatalf("OpenPipe(OUT): %v", err)
	}

	// Trigger a controller-level foreign DMA fault via Service().
	sim.mu.Lock()
	sim.regs[gahb] |= 1 << 5
	sim.mu.Unlock()
	if err := host.Service(); err == nil {
		t.Fatal("Service did not report foreign DMA fault")
	}

	var buf [64]byte
	if _, err := inPipe.PollInto(buf[:]); err == nil {
		t.Fatal("inPipe.PollInto succeeded after controller retirement")
	}
	if err := outPipe.Write([]byte{0x02, 0x01}); err == nil {
		t.Fatal("outPipe.Write succeeded after controller retirement")
	}
	if _, err := outPipe.WriteDone(); err == nil {
		t.Fatal("outPipe.WriteDone succeeded after controller retirement")
	}
}

func TestMultiHostOpenPipeAndWriteWakeIRQMode(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	pad := newSimGamepad()
	sim.attachRoot(pad)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	ev, ok := host.NextEvent()
	if !ok || ev.Kind != usb.DeviceAttached {
		t.Fatalf("expected attach event, got %+v ok=%v", ev, ok)
	}
	iface, _ := ev.Device.Config.FindInterface(3, -1, -1)
	epIn, _ := iface.FindEndpoint(usb.TransferInterrupt, true)
	epOut, _ := iface.FindEndpoint(usb.TransferInterrupt, false)

	ctrl, _ := newMockGIC(t)
	if err := host.EnableIRQ(ctrl, testUSBIRQ); err != nil {
		t.Fatalf("EnableIRQ: %v", err)
	}
	defer host.DisableIRQ()

	// Run Service before any pipes are opened: GINTMSK should NOT have gintSOF set.
	if err := host.Service(); err != nil {
		t.Fatalf("Service (no pipes): %v", err)
	}
	sim.mu.Lock()
	maskBefore := sim.regs[gintmask]
	sim.mu.Unlock()
	if maskBefore&gintSOF != 0 {
		t.Fatalf("expected gintSOF clear before pipes opened, got gintmask=%#x", maskBefore)
	}
	drainReady := func() {
		for {
			select {
			case <-host.Ready():
			default:
				return
			}
		}
	}
	drainReady()

	// Opening an IN pipe in IRQ mode after Service() must arm SOF / HAINTMSK and
	// immediately signal Ready() so the IRQ loop wakes up to arm it.
	inPipe, err := host.OpenPipe(ev.Device.Address, epIn)
	if err != nil {
		t.Fatalf("OpenPipe(IN): %v", err)
	}
	defer inPipe.Close()

	sim.mu.Lock()
	maskAfter := sim.regs[gintmask]
	haintAfter := sim.regs[haintmsk]
	sim.mu.Unlock()
	if maskAfter&gintSOF == 0 || haintAfter == 0 {
		t.Fatalf("OpenPipe in IRQ mode did not re-evaluate IRQ masks: gintmask=%#x haintmsk=%#x", maskAfter, haintAfter)
	}
	select {
	case <-host.Ready():
	default:
		t.Fatal("OpenPipe(IN) in IRQ mode did not signal Ready() wakeup")
	}

	// Open an OUT pipe, run Service(), drain Ready(), and verify Write() wakes Ready().
	outPipe, err := host.OpenPipe(ev.Device.Address, epOut)
	if err != nil {
		t.Fatalf("OpenPipe(OUT): %v", err)
	}
	defer outPipe.Close()

	if err := host.Service(); err != nil {
		t.Fatalf("Service (after opening OUT pipe): %v", err)
	}
	drainReady()

	if err := outPipe.Write([]byte{0x02, 0x01, 0x02, 0x03}); err != nil {
		t.Fatalf("outPipe.Write: %v", err)
	}
	select {
	case <-host.Ready():
	default:
		t.Fatal("Write() on unarmed OUT pipe in IRQ mode did not signal Ready() wakeup")
	}
}

func TestMultiHostDisableIRQSkipsMMIOOnDescDMA(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	pad := newSimGamepad()
	sim.attachRoot(pad)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	ctrl, _ := newMockGIC(t)
	if err := host.EnableIRQ(ctrl, testUSBIRQ); err != nil {
		t.Fatalf("EnableIRQ: %v", err)
	}

	// Simulate another owner enabling HCFG.DESCDMA (bit 23) while keeping GAHBCFG.DMAEN=0;
	// DisableIRQ MUST unregister from GIC without writing GINTMSK/HAINTMSK/GAHBCFG.
	sim.mu.Lock()
	sim.regs[hcfg] |= 1 << 23
	sim.regs[gintmask] = 0x12345678
	sim.regs[haintmsk] = 0x000000ff
	sim.regs[gahb] = gahbGlobalIRQ
	sim.mu.Unlock()

	host.DisableIRQ()

	sim.mu.Lock()
	gotMask := sim.regs[gintmask]
	gotHaint := sim.regs[haintmsk]
	gotAHB := sim.regs[gahb]
	sim.mu.Unlock()
	if gotMask != 0x12345678 || gotHaint != 0x000000ff || gotAHB != gahbGlobalIRQ {
		t.Fatalf("DisableIRQ performed MMIO despite HCFG.DESCDMA active: gintmask=%#x haintmsk=%#x gahb=%#x", gotMask, gotHaint, gotAHB)
	}
}

func TestMultiHostDisableIRQRefusesDescriptorDMAMMIO(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	pad := newSimGamepad()
	sim.attachRoot(pad)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	ctrl, _ := newMockGIC(t)
	if err := host.EnableIRQ(ctrl, testUSBIRQ); err != nil {
		t.Fatalf("EnableIRQ: %v", err)
	}
	if err := host.Service(); err != nil {
		t.Fatalf("Service: %v", err)
	}

	sim.mu.Lock()
	gintBefore := sim.regs[gintmask]
	haintBefore := sim.regs[haintmsk]
	gahbBefore := sim.regs[gahb]
	// External owner enables HCFG.DESCDMA (bit 23) before DisableIRQ runs.
	sim.regs[hcfg] |= 1 << 23
	sim.mu.Unlock()

	host.DisableIRQ()

	if host.Ready() != nil {
		t.Fatal("DisableIRQ did not unregister GIC IRQ handler when HCFG.DESCDMA was set")
	}
	sim.mu.Lock()
	gintAfter := sim.regs[gintmask]
	haintAfter := sim.regs[haintmsk]
	gahbAfter := sim.regs[gahb]
	sim.mu.Unlock()
	if gintAfter != gintBefore || haintAfter != haintBefore || gahbAfter != gahbBefore {
		t.Fatalf("DisableIRQ touched DWC2 registers with HCFG.DESCDMA active: gintmask %#x->%#x haintmsk %#x->%#x gahb %#x->%#x",
			gintBefore, gintAfter, haintBefore, haintAfter, gahbBefore, gahbAfter)
	}

	// OpenMulti must also reject a controller where HCFG.DESCDMA is already set at init.
	sim2 := newMultiChannelSim(1024, 16)
	sim2.regs[hcfg] = 1 << 23
	if _, err := OpenMulti(sim2, Config{PHYClockHz: 60000000}); err == nil {
		t.Fatal("OpenMulti succeeded despite HCFG.DESCDMA already active at init")
	}
}

func TestMultiHostPauseFailsWhenIRQSuspendLosesOwnership(t *testing.T) {
	sim := newMultiChannelSim(1024, 16)
	pad := newSimGamepad()
	sim.attachRoot(pad)

	host, err := OpenMulti(sim, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti: %v", err)
	}
	ctrl, _ := newMockGIC(t)
	allowed := true
	if err := host.EnableIRQGuarded(ctrl, testUSBIRQ, func() bool { return allowed }); err != nil {
		t.Fatalf("EnableIRQGuarded: %v", err)
	}
	if err := host.Service(); err != nil {
		t.Fatalf("Service: %v", err)
	}

	// Simulate lease guard revoking ownership after PausePolling's pioCheck passes
	// (while GAHB/GINTMSK still look intact so pioCheck itself returns nil).
	allowed = false
	sim.mu.Lock()
	gintBefore := sim.regs[gintmask]
	haintBefore := sim.regs[haintmsk]
	gahbBefore := sim.regs[gahb]
	sim.mu.Unlock()

	if err := host.PausePolling(context.Background()); err == nil {
		t.Fatal("PausePolling succeeded when suspendIRQ lost lease ownership")
	}
	if host.paused {
		t.Fatal("PausePolling left host.paused=true after suspendIRQ ownership loss")
	}
	if host.fault == nil {
		t.Fatal("PausePolling did not latch host.fault on suspendIRQ ownership loss")
	}
	if host.Ready() != nil {
		t.Fatal("PausePolling did not unregister GIC handler on suspendIRQ ownership loss")
	}
	sim.mu.Lock()
	gintAfter := sim.regs[gintmask]
	haintAfter := sim.regs[haintmsk]
	gahbAfter := sim.regs[gahb]
	sim.mu.Unlock()
	if gintAfter != gintBefore || haintAfter != haintBefore || gahbAfter != gahbBefore {
		t.Fatalf("PausePolling touched DWC2 IRQ registers after guard became inactive: gintmask %#x->%#x haintmsk %#x->%#x gahb %#x->%#x",
			gintBefore, gintAfter, haintBefore, haintAfter, gahbBefore, gahbAfter)
	}

	// Also verify suspendIRQ directly refuses HCFG.DESCDMA without DWC2 MMIO.
	sim2 := newMultiChannelSim(1024, 16)
	sim2.attachRoot(newSimGamepad())
	host2, err := OpenMulti(sim2, Config{PHYClockHz: 60000000})
	if err != nil {
		t.Fatalf("OpenMulti #2: %v", err)
	}
	ctrl2, _ := newMockGIC(t)
	if err := host2.EnableIRQ(ctrl2, testUSBIRQ); err != nil {
		t.Fatalf("EnableIRQ #2: %v", err)
	}
	if err := host2.Service(); err != nil {
		t.Fatalf("Service #2: %v", err)
	}
	sim2.mu.Lock()
	g2Before := sim2.regs[gintmask]
	h2Before := sim2.regs[haintmsk]
	a2Before := sim2.regs[gahb]
	sim2.regs[hcfg] |= 1 << 23
	sim2.mu.Unlock()
	if err := host2.suspendIRQ(); err == nil {
		t.Fatal("suspendIRQ succeeded despite HCFG.DESCDMA active")
	}
	sim2.mu.Lock()
	g2After := sim2.regs[gintmask]
	h2After := sim2.regs[haintmsk]
	a2After := sim2.regs[gahb]
	sim2.mu.Unlock()
	if g2After != g2Before || h2After != h2Before || a2After != a2Before {
		t.Fatalf("suspendIRQ wrote DWC2 registers with HCFG.DESCDMA active: gintmask %#x->%#x haintmsk %#x->%#x gahb %#x->%#x",
			g2Before, g2After, h2Before, h2After, a2Before, a2After)
	}
}
