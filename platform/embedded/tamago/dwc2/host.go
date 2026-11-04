// SPDX-License-Identifier: GPL-3.0-or-later

// Package dwc2 is an exploratory polling PIO host for one directly attached
// low/full-speed USB boot keyboard. Firmware owns SoC clocks, ULPI PHY, pinmux
// and VBUS routing. Hubs, split transactions, high speed and DMA are unsupported.
package dwc2

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

// Register identities were checked against Linux v6.12 drivers/usb/dwc2/hw.h.
const (
	gahb           = 0x008
	gusb           = 0x00c
	reset          = 0x010
	gint           = 0x014
	gintmask       = 0x018
	rxpop          = 0x020
	rxsize         = 0x024
	nptxsize       = 0x028
	nptxstatus     = 0x02c
	snpsid         = 0x040
	hwcfg2         = 0x048
	hwcfg3         = 0x04c
	ptxsize        = 0x100
	hcfg           = 0x400
	hfir           = 0x404
	hfnum          = 0x408
	ptxstatus      = 0x410
	hprt           = 0x440
	hcchar         = 0x500
	hcsplit        = 0x504
	hcint          = 0x508
	hcmask         = 0x50c
	hctsiz         = 0x510
	pcgc           = 0xe00
	fifo           = 0x1000
	channelEnable  = uint32(1 << 31)
	channelDisable = uint32(1 << 30)
	fatalChannel   = uint32(1<<2 | 1<<3 | 1<<6 | 1<<7 | 1<<8 | 1<<9 | 1<<10)
)

type Registers interface {
	Read32(uint32) uint32
	Write32(uint32, uint32)
	Now() time.Time
}

// Config is an actual firmware clock declaration. This prototype requires a
// 60 MHz ULPI PHY already configured, reset released and VBUS supply available.
type Config struct{ PHYClockHz uint32 }

type packet struct {
	active, in, done, nak, received, halting bool
	pid                                      uint32
	limit                                    int
	data                                     []byte
	deadline                                 time.Time
	polls                                    int
}
type Host struct {
	io      Registers
	owned   bool
	speed   uint32
	address uint32
	ep0     uint16
	config  usb.Configuration
	pending packet
	fault   error
	next    time.Time
	toggle  uint32
}

func Open(io Registers, config Config) (*Host, error) {
	if io == nil || config.PHYClockHz != 60000000 {
		return nil, fmt.Errorf("dwc2: requires firmware-configured 60 MHz ULPI PHY")
	}
	h := &Host{io: io, ep0: 8}
	if err := h.initialize(); err != nil {
		return h, h.fail(err)
	}
	if err := h.enumerate(); err != nil {
		return h, h.fail(err)
	}
	return h, nil
}
func (h *Host) wait(reg, mask, want uint32) error {
	end := h.io.Now().Add(500 * time.Millisecond)
	for n := 0; n < 1000000; n++ {
		if h.io.Read32(reg)&mask == want {
			return nil
		}
		if !h.io.Now().Before(end) {
			break
		}
		if n&255 == 255 {
			runtime.Gosched()
		}
	}
	return fmt.Errorf("dwc2: timeout at register %#x", reg)
}
func (h *Host) pause(duration time.Duration) error {
	end := h.io.Now().Add(duration)
	for n := 0; n < 1000000; n++ {
		if !h.io.Now().Before(end) {
			return nil
		}
		if n&255 == 255 {
			runtime.Gosched()
		}
	}
	return fmt.Errorf("dwc2: clock stalled")
}

// fail permanently retires this host. No report or channel is reused after
// ambiguity; PIO holds no Go-heap DMA addresses. A fresh boot is required.
func (h *Host) fail(err error) error {
	if h.fault != nil {
		return h.fault
	}
	h.fault = err
	if !h.owned {
		return err
	}
	if h.io.Read32(gahb)&(1<<5) != 0 {
		// DMA state no longer belongs to this PIO driver. Do not halt or
		// rewrite a channel whose address/ownership is now uncertain.
		return h.fault
	}
	h.io.Write32(gintmask, 0)
	// Do not touch a firmware-owned active DMA/channel on admission failure.
	if h.pending.active {
		h.io.Write32(hcchar, h.io.Read32(hcchar)|channelEnable|channelDisable)
		if halt := h.wait(hcchar, channelEnable, 0); halt != nil {
			h.fault = fmt.Errorf("%w; containment: %v", err, halt)
		}
	}
	return h.fault
}
func (h *Host) portWrite(set, clear uint32) {
	// ENA is write-one-to-disable; connection/enable/overcurrent changes are W1C.
	v := h.io.Read32(hprt) &^ (uint32(1<<1|1<<2|1<<3|1<<5) | clear)
	h.io.Write32(hprt, v|set)
}
func (h *Host) initialize() error {
	id := h.io.Read32(snpsid)
	if id < 0x4f542000 || id >= 0x4f544000 {
		return fmt.Errorf("dwc2: unsupported core ID %#x", id)
	}
	cfg := h.io.Read32(hwcfg2)
	mode := cfg & 7
	if mode == 3 || mode == 4 || mode == 7 || (cfg>>3)&3 == 1 || (cfg>>3)&3 == 3 || cfg&(1<<19) == 0 || h.io.Read32(hwcfg3)>>16 < 256 {
		return fmt.Errorf("dwc2: unsupported host/PIO/FIFO capabilities")
	}
	if h.io.Read32(gusb)&(1<<4|1<<6|1<<3) != 1<<4 {
		return fmt.Errorf("dwc2: firmware ULPI configuration missing")
	}
	if h.io.Read32(gahb)&(1<<5) != 0 {
		return fmt.Errorf("dwc2: firmware DMA still enabled")
	}
	channels := ((cfg >> 14) & 15) + 1
	for n := uint32(0); n < channels; n++ {
		if h.io.Read32(hcchar+n*0x20)&channelEnable != 0 {
			return fmt.Errorf("dwc2: firmware channel still active")
		}
	}
	h.owned = true
	h.io.Write32(gahb, 0) // polling, slave PIO, no global IRQ or DMA
	h.io.Write32(gintmask, 0)
	h.io.Write32(pcgc, 0)
	if err := h.wait(reset, 1<<31, 1<<31); err != nil {
		return err
	}
	h.io.Write32(reset, 1)
	if err := h.wait(reset, 1, 0); err != nil {
		return err
	}
	if err := h.wait(reset, 1<<31, 1<<31); err != nil {
		return err
	}
	if err := h.pause(time.Millisecond); err != nil {
		return err
	}
	h.io.Write32(gusb, (h.io.Read32(gusb)&^uint32(1<<30|1<<15|1<<9|1<<8))|1<<29)
	if err := h.pause(50 * time.Millisecond); err != nil {
		return err
	}
	if err := h.wait(gint, 1, 1); err != nil {
		return err
	}
	h.io.Write32(hcfg, 0) // 30/60 MHz HS PHY; FS/LS operation, no descriptor DMA
	h.io.Write32(rxsize, 128)
	h.io.Write32(nptxsize, 64<<16|128)
	h.io.Write32(ptxsize, 64<<16|192)
	h.io.Write32(reset, 0x10<<6|1<<5|1<<4)
	if err := h.wait(reset, 1<<5|1<<4, 0); err != nil {
		return err
	}
	if h.io.Read32(hprt)&(1<<4) != 0 {
		return fmt.Errorf("dwc2: port overcurrent")
	}
	h.portWrite(1<<12, 0)
	if err := h.pause(100 * time.Millisecond); err != nil {
		return err
	}
	if err := h.wait(hprt, 1, 1); err != nil {
		return err
	}
	h.portWrite(1<<8, 0)
	if err := h.pause(50 * time.Millisecond); err != nil {
		return err
	}
	h.portWrite(0, 1<<8)
	if err := h.wait(hprt, 1|1<<2, 1|1<<2); err != nil {
		return err
	}
	if err := h.pause(10 * time.Millisecond); err != nil {
		return err
	}
	h.speed = (h.io.Read32(hprt) >> 17) & 3
	if h.speed != 1 && h.speed != 2 {
		return fmt.Errorf("dwc2: only direct low/full-speed devices supported")
	}
	h.io.Write32(gint, 0xffffffff) // retire stale disconnect/port status before enumeration
	h.io.Write32(hfir, 59999)      // 60 MHz ULPI, 1 ms FS/LS frames, after port enable
	return nil
}

func (h *Host) begin(ep uint8, mps uint16, kind uint32, in bool, pid uint32, out []byte, limit int, deadline time.Time) error {
	if err := h.pio(); err != nil {
		return err
	}
	if h.pending.active || h.io.Read32(hcchar)&channelEnable != 0 {
		return fmt.Errorf("dwc2: channel not retired")
	}
	if mps == 0 || mps > 64 || limit < 0 || limit > int(mps) || len(out) > int(mps) {
		return fmt.Errorf("dwc2: invalid packet bounds")
	}
	queue := h.io.Read32(nptxstatus)
	if kind == 3 {
		queue = h.io.Read32(ptxstatus)
	}
	if (queue>>16)&255 == 0 || (!in && int(queue&65535) < (len(out)+3)/4) {
		return fmt.Errorf("dwc2: FIFO/request queue unavailable")
	}
	h.pending = packet{active: true, in: in, pid: pid, limit: limit, deadline: deadline}
	h.io.Write32(hcint, 0x7ff)
	h.io.Write32(hcmask, 0x7ff)
	h.io.Write32(hcsplit, 0)
	size := uint32(len(out))
	if in {
		size = uint32(mps)
	}
	h.io.Write32(hctsiz, size|1<<19|pid<<29)
	flags := uint32(mps) | uint32(ep&15)<<11 | kind<<18 | 1<<20 | h.address<<22
	if in {
		flags |= 1 << 15
	}
	if h.speed == 2 {
		flags |= 1 << 17
	}
	if kind == 3 && h.io.Read32(hfnum)&1 == 0 {
		flags |= 1 << 29
	}
	h.io.Write32(hcchar, flags|channelEnable)
	for i := 0; i < len(out); i += 4 {
		var word [4]byte
		copy(word[:], out[i:min(i+4, len(out))])
		h.io.Write32(fifo, binary.LittleEndian.Uint32(word[:]))
	}
	return nil
}

func (h *Host) pio() error {
	if h.io.Read32(gahb)&(1<<5|1) != 0 || h.io.Read32(gintmask) != 0 || h.io.Read32(hcfg)&(1<<23) != 0 {
		return fmt.Errorf("dwc2: PIO/interrupt ownership changed")
	}
	return nil
}

// step consumes a fixed number of RX entries and register reads. Completion
// includes channel retirement; it never hands a still-owned buffer to callers.
func (h *Host) step() (bool, []byte, bool, error) {
	if err := h.pio(); err != nil {
		return false, nil, false, err
	}
	p := &h.pending
	if !p.active {
		return false, nil, false, fmt.Errorf("dwc2: no active packet")
	}
	p.polls++
	if p.polls > 1000000 || !h.io.Now().Before(p.deadline) {
		return false, nil, false, fmt.Errorf("dwc2: packet timeout")
	}
	port := h.io.Read32(hprt)
	if port&5 != 5 || port&(1<<4) != 0 || h.io.Read32(gint)&(1<<29) != 0 {
		return false, nil, false, fmt.Errorf("dwc2: port disconnected/disabled/overcurrent")
	}
	for n := 0; n < 8 && h.io.Read32(gint)&(1<<4) != 0; n++ {
		v := h.io.Read32(rxpop)
		count := int((v >> 4) & 2047)
		kind := (v >> 17) & 15
		if v&15 != 0 {
			return false, nil, false, fmt.Errorf("dwc2: unexpected RX channel")
		}
		switch kind {
		case 2:
			if !p.in || p.received || count > p.limit || (v>>15)&3 != p.pid {
				return false, nil, false, fmt.Errorf("dwc2: invalid RX size/PID/ownership")
			}
			p.received = true
			for i := 0; i < count; i += 4 {
				var word [4]byte
				binary.LittleEndian.PutUint32(word[:], h.io.Read32(fifo))
				p.data = append(p.data, word[:min(4, count-i)]...)
			}
		case 3, 7:
			if count != 0 {
				return false, nil, false, fmt.Errorf("dwc2: payload on RX status")
			}
		default:
			return false, nil, false, fmt.Errorf("dwc2: unsupported RX status %d", kind)
		}
	}
	if h.io.Read32(gint)&(1<<4) != 0 {
		return false, nil, false, nil
	}
	ints := h.io.Read32(hcint)
	if ints&fatalChannel != 0 {
		return false, nil, false, fmt.Errorf("dwc2: channel error %#x", ints)
	}
	if ints&1 != 0 {
		p.done = true
	}
	if ints&(1<<4) != 0 {
		p.nak = true
	}
	if (p.done || p.received) && p.nak {
		return false, nil, false, fmt.Errorf("dwc2: contradictory completion")
	}
	if p.done || p.nak {
		if h.io.Read32(hcchar)&channelEnable != 0 {
			if !p.halting {
				h.io.Write32(hcchar, h.io.Read32(hcchar)|channelEnable|channelDisable)
				p.halting = true
			}
			return false, nil, false, nil
		}
		if p.done && p.in && !p.received {
			return false, nil, false, fmt.Errorf("dwc2: completion without RX packet")
		}
		h.io.Write32(hcint, 0x7ff)
		p.active = false
		return true, p.data, p.nak, nil
	}
	if ints&(1<<1) != 0 {
		return false, nil, false, fmt.Errorf("dwc2: unexplained channel halt")
	}
	return false, nil, false, nil
}

func (h *Host) transfer(in bool, pid uint32, out []byte, limit int, deadline time.Time) ([]byte, error) {
	for retries := 0; retries < 1000; retries++ {
		if err := h.begin(0, h.ep0, 0, in, pid, out, limit, deadline); err != nil {
			return nil, err
		}
		for polls := 0; polls < 1000000; polls++ {
			done, data, nak, err := h.step()
			if err != nil {
				return nil, err
			}
			if done {
				if !nak {
					return data, nil
				}
				break
			}
			if polls&255 == 255 {
				runtime.Gosched()
			}
		}
		if h.pending.active {
			return nil, fmt.Errorf("dwc2: control poll budget exhausted")
		}
	}
	return nil, fmt.Errorf("dwc2: control NAK budget exhausted")
}
func (h *Host) request(requestType, request byte, value, index uint16, length int) ([]byte, error) {
	if length < 0 || length > usb.MaxConfigurationBytes || (length != 0 && requestType&0x80 == 0) {
		return nil, fmt.Errorf("dwc2: unsupported control request")
	}
	setup := []byte{requestType, request, byte(value), byte(value >> 8), byte(index), byte(index >> 8), byte(length), byte(length >> 8)}
	deadline := h.io.Now().Add(2 * time.Second)
	if _, err := h.transfer(false, 3, setup, 0, deadline); err != nil {
		return nil, err
	}
	var data []byte
	pid := uint32(2)
	for len(data) < length {
		packet, err := h.transfer(true, pid, nil, min(int(h.ep0), length-len(data)), deadline)
		if err != nil {
			return nil, err
		}
		data = append(data, packet...)
		pid ^= 2
		if len(packet) < int(h.ep0) {
			break
		}
	}
	// No-data requests use IN status; control reads use OUT status. DATA1.
	if _, err := h.transfer(length == 0, 2, nil, 0, deadline); err != nil {
		return nil, err
	}
	return data, nil
}
func (h *Host) enumerate() error {
	header, err := h.request(0x80, 6, 0x100, 0, 8)
	if err != nil {
		return err
	}
	if len(header) != 8 || header[0] != 18 || header[1] != 1 {
		return fmt.Errorf("dwc2: invalid device header")
	}
	mps := uint16(header[7])
	if (mps != 8 && mps != 16 && mps != 32 && mps != 64) || (h.speed == 2 && mps != 8) {
		return fmt.Errorf("dwc2: invalid EP0 size")
	}
	h.ep0 = mps
	if _, err = h.request(0, 5, 1, 0, 0); err != nil {
		return err
	}
	if err = h.pause(2 * time.Millisecond); err != nil {
		return err
	}
	h.address = 1
	device, err := h.request(0x80, 6, 0x100, 0, 18)
	if err != nil {
		return err
	}
	if len(device) != 18 || device[0] != 18 || device[1] != 1 || device[4] == 9 || device[7] != byte(mps) || device[17] != 1 {
		return fmt.Errorf("dwc2: unsupported device descriptor")
	}
	config, err := h.request(0x80, 6, 0x200, 0, 9)
	if err != nil {
		return err
	}
	if len(config) != 9 || config[0] != 9 || config[1] != 2 {
		return fmt.Errorf("dwc2: invalid configuration header")
	}
	size := int(binary.LittleEndian.Uint16(config[2:4]))
	if size < 9 || size > usb.MaxConfigurationBytes {
		return fmt.Errorf("dwc2: invalid configuration length")
	}
	data, err := h.request(0x80, 6, 0x200, 0, size)
	if err != nil {
		return err
	}
	h.config, err = usb.ParseConfiguration(data)
	if err != nil {
		return err
	}
	if h.config.MaxPacketSize > 64 || (h.speed == 2 && (h.config.MaxPacketSize > 8 || h.config.Interval < 10)) {
		return fmt.Errorf("dwc2: unsupported keyboard packet size")
	}
	if _, err = h.request(0, 9, uint16(h.config.Value), 0, 0); err != nil {
		return err
	}
	if _, err = h.request(0x21, 11, 0, uint16(h.config.Interface), 0); err != nil {
		return err
	}
	if _, err = h.request(0x21, 10, 0, uint16(h.config.Interface), 0); err != nil {
		return err
	}
	return nil
}

// PollKeyboard never spins waiting for device input. NAK retires the current
// attempt and preserves the interrupt data toggle. Call regularly to progress.
func (h *Host) PollKeyboard() ([]byte, error) {
	if h.fault != nil {
		return nil, h.fault
	}
	if !h.pending.active {
		if h.io.Now().Before(h.next) {
			return nil, nil
		}
		if err := h.begin(h.config.Endpoint, h.config.MaxPacketSize, 3, true, h.toggle, nil, 8, h.io.Now().Add(500*time.Millisecond)); err != nil {
			return nil, h.fail(err)
		}
		return nil, nil
	}
	done, data, nak, err := h.step()
	if err != nil {
		return nil, h.fail(err)
	}
	if !done {
		return nil, nil
	}
	h.next = h.io.Now().Add(time.Duration(h.config.Interval) * time.Millisecond)
	if nak {
		return nil, nil
	}
	if len(data) != 8 {
		return nil, h.fail(fmt.Errorf("dwc2: short boot keyboard report"))
	}
	h.toggle ^= 2
	return data, nil
}
