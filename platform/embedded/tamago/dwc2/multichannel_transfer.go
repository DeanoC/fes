// SPDX-License-Identifier: GPL-3.0-or-later

package dwc2

import (
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

// ControlTransfer executes a synchronous bounded EP0 control transfer on CH0,
// supporting both IN (reqType&0x80 != 0) and OUT (reqType&0x80 == 0) data stages.
func (h *MultiHost) ControlTransfer(addr uint8, reqType, req byte, value, index uint16, data []byte) (int, error) {
	if h.fault != nil {
		return 0, h.fault
	}
	if h.paused {
		return 0, fmt.Errorf("dwc2: host paused")
	}
	if err := h.pioCheck(); err != nil {
		return 0, h.failController(err)
	}
	if len(data) > usb.MaxConfigurationBytes {
		return 0, fmt.Errorf("dwc2: control transfer length %d exceeds limit", len(data))
	}
	speed := h.rootSpeed
	ep0 := uint16(8)
	var route splitRoute
	if addr == 0 {
		if speed == usb.SpeedHigh {
			ep0 = 64
		}
	} else {
		if int(addr) >= len(h.devices) || h.devices[addr] == nil {
			return 0, fmt.Errorf("dwc2: unknown device address %d", addr)
		}
		dev := h.devices[addr]
		speed = dev.info.Speed
		ep0 = dev.info.Descriptor.MaxPacketSize0
		route = dev.route
	}
	return h.controlTransferTarget(addr, speed, ep0, route, reqType, req, value, index, data)
}

func (h *MultiHost) controlTransferTarget(addr uint8, speed usb.Speed, ep0 uint16, route splitRoute, reqType, req byte, value, index uint16, data []byte) (int, error) {
	inDir := reqType&0x80 != 0
	length := len(data)
	var setup [8]byte
	setup[0] = reqType
	setup[1] = req
	binary.LittleEndian.PutUint16(setup[2:4], value)
	binary.LittleEndian.PutUint16(setup[4:6], index)
	binary.LittleEndian.PutUint16(setup[6:8], uint16(length))

	deadline := h.io.Now().Add(2 * time.Second)
	// SETUP stage (PID = SETUP = 3)
	if _, _, err := h.ch0Transfer(addr, speed, ep0, route, false, 3, setup[:], 0, false, deadline); err != nil {
		return 0, err
	}
	// DATA stage (starts DATA1 = 2, alternates DATA0/DATA1)
	pid := uint32(2)
	doneBytes := 0
	if length > 0 {
		if inDir {
			for doneBytes < length {
				want := min(int(ep0), length-doneBytes)
				n, _, err := h.ch0Transfer(addr, speed, ep0, route, true, pid, nil, want, false, deadline)
				if err != nil {
					return doneBytes, err
				}
				copy(data[doneBytes:doneBytes+n], h.slots[0].inBuf[:n])
				doneBytes += n
				pid ^= 2
				if n < int(ep0) {
					break
				}
			}
		} else {
			outDoPing := false
			for doneBytes < length {
				chunk := min(int(ep0), length-doneBytes)
				_, nyet, err := h.ch0Transfer(addr, speed, ep0, route, false, pid, data[doneBytes:doneBytes+chunk], 0, outDoPing, deadline)
				if err != nil {
					return doneBytes, err
				}
				outDoPing = nyet
				doneBytes += chunk
				pid ^= 2
			}
		}
	}
	// STATUS stage (opposite direction of DATA, or IN when length==0; always DATA1=2)
	statusIn := !inDir || length == 0
	if _, _, err := h.ch0Transfer(addr, speed, ep0, route, statusIn, 2, nil, 0, false, deadline); err != nil {
		return doneBytes, err
	}
	return doneBytes, nil
}

func (h *MultiHost) ch0Transfer(addr uint8, speed usb.Speed, ep0 uint16, route splitRoute, in bool, pid uint32, out []byte, limit int, doPing bool, deadline time.Time) (int, bool, error) {
	split := route.hub != 0
	hsOut := !in && !split && speed == usb.SpeedHigh && pid != 3
	if split && h.isSingleTTHubBusy(route.hub, 0) {
		ttDeadline := h.io.Now().Add(4 * time.Millisecond)
		for h.isSingleTTHubBusy(route.hub, 0) && h.io.Now().Before(ttDeadline) {
			if err := h.drainRXIntoSlots(64); err != nil {
				return 0, false, err
			}
			if h.io.Read32(gint)&gintRXFLVL != 0 {
				continue
			}
			now := h.io.Now()
			for ch := 1; ch < h.channels; ch++ {
				s := &h.slots[ch]
				if !s.allocated || s.fault != nil || s.route.hub != route.hub {
					continue
				}
				if s.armed {
					h.stepChannel(ch, now)
					if h.fault != nil {
						return 0, false, h.fault
					}
				}
				if s.fault == nil && !s.armed && s.waitingSplit && s.completeSplit {
					if err := h.armSlot(ch, true); err != nil {
						s.fault = err
					}
				}
			}
			if h.fault != nil {
				return 0, false, h.fault
			}
		}
		if h.isSingleTTHubBusy(route.hub, 0) {
			return 0, false, fmt.Errorf("dwc2: single-TT hub %d busy; refusing CH0 split addr=%d", route.hub, addr)
		}
	}
	for retries := 0; retries < 1000; retries++ {
		if !h.io.Now().Before(deadline) {
			return 0, false, fmt.Errorf("dwc2: CH0 control timeout addr=%d", addr)
		}
		if err := h.pioCheck(); err != nil {
			return 0, false, h.failController(err)
		}
		if h.io.Read32(chReg(0, hcchar))&channelEnable != 0 {
			if err := h.haltChannel(0); err != nil {
				return 0, false, h.failController(err)
			}
		}
		completeSplit := false
		var startFrame, nextFrame uint16
	splitLoop:
		for {
			if !h.io.Now().Before(deadline) {
				return 0, false, fmt.Errorf("dwc2: CH0 split timeout addr=%d", addr)
			}
			ping := hsOut && doPing
			// Wait for both a non-periodic request queue entry and enough TX FIFO
			// words before programming CH0 so concurrent Bulk-OUT traffic cannot
			// overrun or stall the shared non-periodic FIFO.
			outWords := 0
			if !in && !completeSplit && !ping {
				outWords = (len(out) + 3) / 4
			}
			if q := h.io.Read32(nptxstatus); (q>>16)&255 == 0 || int(q&65535) < outWords {
				runtime.Gosched()
				continue
			}
			if split {
				frame := uint16(h.io.Read32(hfnum)) & 0x3fff
				if !completeSplit {
					if frame&7 > 5 {
						runtime.Gosched()
						continue
					}
					startFrame = frame
				} else {
					if (frame>>3)&0x7ff != (startFrame>>3)&0x7ff {
						completeSplit = false
						continue
					}
					if frameBefore(frame, nextFrame) {
						runtime.Gosched()
						continue
					}
				}
			}
			h.io.Write32(chReg(0, hcint), 0x7ff)
			h.io.Write32(chReg(0, hcmask), 0x7ff)
			var splt uint32
			if split {
				splt = 1<<31 | 3<<14 | route.hub<<7 | route.port
				if completeSplit {
					splt |= 1 << 16
				}
			}
			h.io.Write32(chReg(0, hcsplit), splt)
			size := uint32(len(out))
			if in {
				size = uint32(ep0)
			} else if completeSplit || ping {
				size = 0
			}
			tsiz := size | 1<<19 | pid<<29
			if ping {
				tsiz = hctsizDoPng | 1<<19 | pid<<29
			}
			h.io.Write32(chReg(0, hctsiz), tsiz)
			flags := uint32(ep0) | 1<<20 | uint32(addr)<<22
			if in {
				flags |= 1 << 15
			}
			if speed == usb.SpeedLow {
				flags |= 1 << 17
			}
			h.io.Write32(chReg(0, hcchar), flags|channelEnable)
			if !in && !completeSplit && !ping && len(out) > 0 {
				for i := 0; i < len(out); i += 4 {
					var word [4]byte
					copy(word[:], out[i:min(i+4, len(out))])
					h.io.Write32(chFIFO(0), binary.LittleEndian.Uint32(word[:]))
				}
			}

			received := false
			gotLen := 0
			for polls := 0; polls < 1000000; polls++ {
				if !h.io.Now().Before(deadline) {
					if err := h.haltChannel(0); err != nil {
						return 0, false, h.failController(err)
					}
					return 0, false, fmt.Errorf("dwc2: CH0 transfer timeout addr=%d", addr)
				}
				// Drain RX FIFO entries; route non-CH0 entries through the same
				// ownership/armed/direction/size/PID validator used by Service().
				for h.io.Read32(gint)&gintRXFLVL != 0 {
					v := h.io.Read32(rxpop)
					rch := int(v & 0x0f)
					cnt := int((v >> 4) & 0x7ff)
					dpid := (v >> 15) & 3
					kind := (v >> 17) & 0x0f
					if rch == 0 {
						if kind != 2 {
							continue
						}
						if cnt > limit || cnt > maxMultiMPS {
							for i := 0; i < cnt; i += 4 {
								_ = h.io.Read32(fifo)
							}
							if err := h.haltChannel(0); err != nil {
								return 0, false, h.failController(err)
							}
							return 0, false, fmt.Errorf("dwc2: CH0 RX overflow %d > %d", cnt, limit)
						}
						received = true
						gotLen = cnt
						for i := 0; i < cnt; i += 4 {
							var word [4]byte
							binary.LittleEndian.PutUint32(word[:], h.io.Read32(fifo))
							copy(h.slots[0].inBuf[i:cnt], word[:min(4, cnt-i)])
						}
					} else {
						if err := h.consumeSlotRX(rch, cnt, dpid, kind); err != nil {
							return 0, false, err
						}
					}
				}
				ints := h.io.Read32(chReg(0, hcint))
				fatal := fatalChannel
				if split || hsOut {
					fatal &^= 1 << 6
				}
				if ints&fatal != 0 {
					if err := h.haltChannel(0); err != nil {
						return 0, false, h.failController(err)
					}
					return 0, false, fmt.Errorf("dwc2: CH0 fatal hcint=%#x addr=%d", ints, addr)
				}
				if in && !split && ints&(1<<4) != 0 {
					h.io.Write32(chReg(0, hcint), 1<<4)
					ints &^= 1 << 4
				}
				if in && !split && ints&(1<<5) != 0 {
					h.io.Write32(chReg(0, hcint), 1<<5)
					ints &^= 1 << 5
				}
				if ping {
					if ints&(1<<5) != 0 {
						if err := h.haltChannel(0); err != nil {
							return 0, false, h.failController(err)
						}
						doPing = false
						continue splitLoop // PING ACKed; send DATA OUT immediately
					}
					if ints&(1<<4) != 0 {
						if err := h.haltChannel(0); err != nil {
							return 0, false, h.failController(err)
						}
						break splitLoop // PING NAKed; retry PING
					}
				}
				if hsOut && !ping && ints&(1<<6) != 0 {
					if err := h.haltChannel(0); err != nil {
						return 0, false, h.failController(err)
					}
					return gotLen, true, nil // HS Control-OUT accepted with NYET
				}
				if ints&1 != 0 {
					if err := h.haltChannel(0); err != nil {
						return 0, false, h.failController(err)
					}
					if in && limit > 0 && !received {
						return 0, false, fmt.Errorf("dwc2: CH0 completed without RX")
					}
					return gotLen, false, nil
				}
				if split && ints&(1<<5) != 0 && !completeSplit {
					if err := h.haltChannel(0); err != nil {
						return 0, false, h.failController(err)
					}
					completeSplit = true
					nextFrame = (startFrame + 2) & 0x3fff
					continue splitLoop
				}
				if split && ints&(1<<6) != 0 {
					if err := h.haltChannel(0); err != nil {
						return 0, false, h.failController(err)
					}
					if completeSplit {
						nextFrame = (uint16(h.io.Read32(hfnum)) + 1) & 0x3fff
						continue splitLoop
					}
					break splitLoop // Retry SSPLIT on transient TT NYET
				}
				if ints&(1<<4) != 0 {
					if err := h.haltChannel(0); err != nil {
						return 0, false, h.failController(err)
					}
					break splitLoop // Retry on NAK
				}
				if polls&255 == 255 {
					runtime.Gosched()
				}
			}
			if err := h.haltChannel(0); err != nil {
				return 0, false, h.failController(err)
			}
			return 0, false, fmt.Errorf("dwc2: CH0 poll budget exhausted addr=%d", addr)
		}
	}
	return 0, false, fmt.Errorf("dwc2: CH0 NAK budget exhausted addr=%d", addr)
}

// OpenPipe allocates a dedicated hardware host channel (1..channels-1) for ep
// on the enumerated device addr.
func (h *MultiHost) OpenPipe(addr uint8, ep usb.EndpointDescriptor) (usb.Pipe, error) {
	if h.fault != nil {
		return nil, h.fault
	}
	if h.paused {
		return nil, fmt.Errorf("dwc2: host paused")
	}
	if err := h.pioCheck(); err != nil {
		return nil, h.failController(err)
	}
	if int(addr) == 0 || int(addr) >= len(h.devices) || h.devices[addr] == nil {
		return nil, fmt.Errorf("dwc2: unknown device address %d", addr)
	}
	if ep.TransferType != usb.TransferInterrupt && ep.TransferType != usb.TransferBulk {
		return nil, fmt.Errorf("dwc2: unsupported endpoint transfer type %d", ep.TransferType)
	}
	if ep.MaxPacketSize == 0 || ep.MaxPacketSize > maxMultiMPS {
		return nil, fmt.Errorf("dwc2: unsupported endpoint MPS %d", ep.MaxPacketSize)
	}
	reqWords := uint32((int(ep.MaxPacketSize) + 3) / 4)
	if ep.In() {
		if reqWords+2 > h.rxWords {
			return nil, fmt.Errorf("dwc2: IN endpoint MPS %d exceeds RX FIFO capacity %d words", ep.MaxPacketSize, h.rxWords)
		}
	} else if ep.TransferType == usb.TransferInterrupt {
		if reqWords > h.pTxWords {
			return nil, fmt.Errorf("dwc2: Interrupt-OUT endpoint MPS %d exceeds periodic TX FIFO capacity %d words", ep.MaxPacketSize, h.pTxWords)
		}
	} else {
		if reqWords > h.npTxWords {
			return nil, fmt.Errorf("dwc2: Bulk-OUT endpoint MPS %d exceeds non-periodic TX FIFO capacity %d words", ep.MaxPacketSize, h.npTxWords)
		}
	}
	ch := -1
	for i := 1; i < h.channels; i++ {
		s := &h.slots[i]
		if s.allocated {
			if s.ownerAddr == addr && s.ep.Address == ep.Address {
				return nil, fmt.Errorf("dwc2: endpoint %#x on device %d already open", ep.Address, addr)
			}
			continue
		}
		if ch < 0 {
			ch = i
		}
	}
	if ch < 0 {
		return nil, fmt.Errorf("dwc2: all %d hardware channels in use", h.channels-1)
	}
	dev := h.devices[addr]
	now := h.io.Now()
	h.nextPipeGen++
	gen := h.nextPipeGen
	h.slots[ch] = multiChannel{
		allocated: true,
		ownerAddr: addr,
		gen:       gen,
		speed:     dev.info.Speed,
		route:     dev.route,
		ep:        ep,
		toggle:    0, // DATA0
		interval:  endpointInterval(dev.info.Speed, ep),
		nextDue:   now,
	}
	p := &multiPipe{host: h, ch: ch, addr: addr, gen: gen, ep: ep}
	if h.irqMode.Load() && h.irqActive.Load() {
		h.armMultiIRQ(now)
		if ep.In() {
			h.signalReady()
		}
	}
	return p, nil
}

type multiPipe struct {
	host   *MultiHost
	ch     int
	addr   uint8
	gen    uint64
	ep     usb.EndpointDescriptor
	closed bool
}

func (p *multiPipe) DeviceAddress() uint8 { return p.addr }

func (p *multiPipe) Endpoint() usb.EndpointDescriptor { return p.ep }

func (p *multiPipe) PollInto(dst []byte) (int, error) {
	if p.closed || p.host == nil {
		return 0, errors.New("dwc2: pipe closed")
	}
	if p.host.fault != nil {
		return 0, p.host.fault
	}
	s := &p.host.slots[p.ch]
	if !s.allocated || s.ownerAddr != p.addr || s.gen != p.gen {
		return 0, errors.New("dwc2: device detached")
	}
	if s.fault != nil {
		return 0, s.fault
	}
	if !s.inReady {
		return 0, nil
	}
	n := s.inReadyLen
	if n > len(dst) {
		return 0, fmt.Errorf("dwc2: buffer too small (%d < %d)", len(dst), n)
	}
	copy(dst[:n], s.inBuf[:n])
	s.inReady = false
	s.inReadyLen = 0
	if p.host.irqMode.Load() && p.host.irqActive.Load() && !p.host.paused && !s.armed {
		p.host.signalReady()
	}
	return n, nil
}

func (p *multiPipe) Write(data []byte) error {
	if p.closed || p.host == nil {
		return errors.New("dwc2: pipe closed")
	}
	if p.host.fault != nil {
		return p.host.fault
	}
	if p.ep.In() {
		return errors.New("dwc2: cannot write to IN pipe")
	}
	if len(data) > int(p.ep.MaxPacketSize) || len(data) > maxMultiMPS {
		return fmt.Errorf("dwc2: OUT write %d exceeds MPS %d", len(data), p.ep.MaxPacketSize)
	}
	s := &p.host.slots[p.ch]
	if !s.allocated || s.ownerAddr != p.addr || s.gen != p.gen {
		return errors.New("dwc2: device detached")
	}
	if s.fault != nil {
		return s.fault
	}
	if s.outPending {
		return errors.New("dwc2: OUT pipe busy")
	}
	copy(s.outBuf[:], data)
	s.outLen = len(data)
	s.outPending = true
	s.outDone = false
	s.outErr = nil
	now := p.host.io.Now()
	if s.ep.TransferType != usb.TransferInterrupt || s.nextDue.Before(now) {
		s.nextDue = now
	}
	if p.host.irqMode.Load() && p.host.irqActive.Load() && !p.host.paused && !s.armed {
		p.host.signalReady()
	}
	return nil
}

func (p *multiPipe) WriteDone() (bool, error) {
	if p.closed || p.host == nil {
		return false, errors.New("dwc2: pipe closed")
	}
	if p.host.fault != nil {
		return false, p.host.fault
	}
	s := &p.host.slots[p.ch]
	if !s.allocated || s.ownerAddr != p.addr || s.gen != p.gen {
		return false, errors.New("dwc2: device detached")
	}
	if s.outErr != nil {
		return false, s.outErr
	}
	if s.fault != nil {
		return false, s.fault
	}
	return s.outDone, nil
}

func (p *multiPipe) SetIntervalOverride(interval time.Duration) {
	if p.closed || p.host == nil {
		return
	}
	s := &p.host.slots[p.ch]
	if s.allocated && s.ownerAddr == p.addr && s.gen == p.gen {
		s.overrideInterval = interval
	}
}

func (p *multiPipe) Close() error {
	if p.closed || p.host == nil {
		return nil
	}
	if p.host.paused {
		return fmt.Errorf("dwc2: host paused")
	}
	p.closed = true
	if p.host.fault != nil {
		return p.host.fault
	}
	s := &p.host.slots[p.ch]
	if s.allocated && s.ownerAddr == p.addr && s.gen == p.gen {
		if err := p.host.pioCheck(); err != nil {
			s.fault = err
			return p.host.failController(err)
		}
		if err := p.host.haltChannel(p.ch); err != nil {
			// Retain slot as allocated/faulted so an active hardware channel is never reused.
			s.fault = err
			return p.host.failController(err)
		}
		p.host.slots[p.ch] = multiChannel{}
		if p.host.irqMode.Load() && p.host.irqActive.Load() {
			p.host.io.Write32(haintmsk, p.host.activeChannelMask())
		}
	}
	return nil
}
