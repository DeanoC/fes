//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later

package usb

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/usbarmory/tamago/dma"
)

// XHCI is a deliberately bounded polling host for one directly attached USB
// boot keyboard. The caller supplies a mapped, exclusively owned controller.
// It does not initialize a DE25 PHY, clock, reset, PCI bridge or DWC3 core.
// DMA stays reserved until reboot, including after a controller error.
type XHCI struct {
	base, op, doorbell, interrupter uintptr
	memory                          []byte
	memoryAddress                   uint64
	next                            int
	command, control, interrupt     transferRing
	events                          []byte
	eventsAddress                   uint64
	eventIndex                      int
	eventCycle                      uint32
	input, output                   []byte
	inputAddress                    uint64
	controlData                     []byte
	controlAddress                  uint64
	report                          []byte
	reportAddress                   uint64
	pending                         uint64
	slot, port, speed               uint8
	config                          Configuration
	info                            DeviceInfo
	fault                           error
}

// DeviceInfo is observed from the USB device descriptor and xHCI port state.
type DeviceInfo struct {
	Vendor, Product, USBVersion uint16
	Slot, Port, Speed           uint8
}

// OpenXHCI resets an exclusively owned xHCI controller, addresses a directly
// attached USB2 device, retrieves its real descriptors, and selects boot HID.
// This first adapter requires 32-byte contexts, no scratchpads and 4KiB pages.
func OpenXHCI(mmio uintptr) (*XHCI, error) {
	if mmio == 0 || mmio&0xfff != 0 {
		return nil, fmt.Errorf("xHCI: invalid MMIO base")
	}
	region := dma.Default()
	if region == nil {
		return nil, fmt.Errorf("xHCI: initialize board DMA first")
	}
	available := false
	for _, size := range region.FreeBlocks() {
		if size >= (64<<10)+4096 {
			available = true
		}
	}
	if !available {
		return nil, fmt.Errorf("xHCI: insufficient contiguous DMA memory")
	}
	h := &XHCI{base: mmio, eventCycle: 1}
	caps := h.read(0)
	if caps&0xff < 0x20 || caps>>16 < 0x100 {
		return nil, fmt.Errorf("xHCI: unsupported capability header %#x", caps)
	}
	h.op = mmio + uintptr(caps&0xff)
	h.doorbell = mmio + uintptr(h.read(0x14)&^3)
	h.interrupter = mmio + uintptr(h.read(0x18)&^31) + 0x20
	params := h.read(4)
	ports := uint8(params >> 24)
	if params&255 == 0 || ports == 0 || ports > 32 {
		return nil, fmt.Errorf("xHCI: unsupported slots/ports %#x", params)
	}
	if h.read(0x10)&4 != 0 || h.read(8)&0xfbe00000 != 0 {
		return nil, fmt.Errorf("xHCI: 64-byte contexts or scratchpads unsupported")
	}
	if read32(h.op+8)&1 == 0 {
		return nil, fmt.Errorf("xHCI: 4KiB pages unsupported")
	}
	write32(h.op, 0)
	if err := waitRegister(h.op+4, 1, 1); err != nil {
		return nil, err
	}
	write32(h.op, 2)
	if err := waitRegister(h.op, 2, 0); err != nil {
		return nil, err
	}
	if err := waitRegister(h.op+4, 1<<11, 0); err != nil {
		return nil, err
	}
	addr, memory := region.Reserve(64<<10, 4096)
	clear(memory)
	h.memoryAddress, h.memory = uint64(addr), memory
	dcbaaAddress, dcbaa := h.reserve(2048, 64)
	h.command = h.newRing()
	h.control = h.newRing()
	h.interrupt = h.newRing()
	h.eventsAddress, h.events = h.reserve(4096, 64)
	erstAddress, erst := h.reserve(16, 64)
	binary.LittleEndian.PutUint64(erst, h.eventsAddress)
	binary.LittleEndian.PutUint32(erst[8:], 256)
	outputAddress, output := h.reserve(32*32, 64)
	h.output = output
	h.inputAddress, h.input = h.reserve(33*32, 64)
	h.controlAddress, h.controlData = h.reserve(MaxConfigurationBytes, 64)
	h.reportAddress, h.report = h.reserve(8, 64)
	write64(h.op+0x30, dcbaaAddress)
	write64(h.op+0x18, h.command.address|1)
	write32(h.op+0x38, 1)
	write32(h.interrupter, 0) // polling; no interrupt routing assumed
	write32(h.interrupter+4, 0)
	write32(h.interrupter+8, 1)
	write64(h.interrupter+0x10, erstAddress)
	write64(h.interrupter+0x18, h.eventsAddress)
	barrier()
	write32(h.op, 1)
	if err := waitRegister(h.op+4, 1, 0); err != nil {
		return nil, err
	}
	for p := uint8(1); p <= ports; p++ {
		if read32(h.op+0x400+uintptr(p-1)*16)&1 != 0 {
			if h.port != 0 {
				return nil, h.fail(fmt.Errorf("xHCI: multiple attached devices unsupported"))
			}
			h.port = p
		}
	}
	if h.port == 0 {
		return nil, h.fail(fmt.Errorf("xHCI: no attached keyboard"))
	}
	portRegister := h.op + 0x400 + uintptr(h.port-1)*16
	write32(portRegister, 1<<9|1<<4)
	if err := waitRegister(portRegister, 1<<4, 0); err != nil {
		return nil, h.fail(err)
	}
	state := read32(portRegister)
	if state&3 != 3 {
		return nil, h.fail(fmt.Errorf("xHCI: port reset did not enable device %#x", state))
	}
	h.speed = uint8(state>>10) & 15
	if h.speed < 1 || h.speed > 3 {
		return nil, h.fail(fmt.Errorf("xHCI: only directly attached USB2 devices supported, speed=%d", h.speed))
	}
	write32(portRegister, 1<<9|state&0xfe0000)
	event, err := h.runCommand(0, 0, 9<<10)
	if err != nil {
		return nil, h.fail(err)
	}
	h.slot = uint8(event.control >> 24)
	if h.slot != 1 {
		return nil, h.fail(fmt.Errorf("xHCI: unexpected slot %d", h.slot))
	}
	binary.LittleEndian.PutUint64(dcbaa[int(h.slot)*8:], outputAddress)
	binary.LittleEndian.PutUint32(h.input[4:], 3)
	binary.LittleEndian.PutUint32(h.input[32:], uint32(h.speed)<<20|1<<27)
	binary.LittleEndian.PutUint32(h.input[36:], uint32(h.port)<<16)
	maxPacket := uint32(8)
	if h.speed == 3 {
		maxPacket = 64
	}
	h.setupEndpoint(h.input[64:96], h.control.address, maxPacket, 4, 0)
	if _, err = h.runCommand(h.inputAddress, 0, 11<<10|uint32(h.slot)<<24); err != nil {
		return nil, h.fail(err)
	}
	device, err := h.request(0x80, 6, 0x100, 0, 18)
	if err != nil {
		return nil, h.fail(err)
	}
	if len(device) != 18 || device[0] != 18 || device[1] != 1 || device[17] != 1 {
		return nil, h.fail(fmt.Errorf("xHCI: unsupported device descriptor"))
	}
	actualPacket := uint32(device[7])
	if (actualPacket != 8 && actualPacket != 16 && actualPacket != 32 && actualPacket != 64) ||
		(h.speed == 2 && actualPacket != 8) || (h.speed == 3 && actualPacket != 64) {
		return nil, h.fail(fmt.Errorf("xHCI: invalid endpoint0 packet size"))
	}
	h.info = DeviceInfo{Vendor: binary.LittleEndian.Uint16(device[8:]), Product: binary.LittleEndian.Uint16(device[10:]), USBVersion: binary.LittleEndian.Uint16(device[2:]), Slot: h.slot, Port: h.port, Speed: h.speed}
	if actualPacket != maxPacket {
		clear(h.input)
		binary.LittleEndian.PutUint32(h.input[4:], 2)
		copy(h.input[64:96], h.output[32:64])
		binary.LittleEndian.PutUint32(h.input[68:], actualPacket<<16|4<<3|3<<1)
		if _, err = h.runCommand(h.inputAddress, 0, 13<<10|uint32(h.slot)<<24); err != nil {
			return nil, h.fail(err)
		}
	}
	header, err := h.request(0x80, 6, 0x200, 0, 9)
	if err != nil {
		return nil, h.fail(err)
	}
	if len(header) != 9 {
		return nil, h.fail(fmt.Errorf("xHCI: short configuration header"))
	}
	length := int(binary.LittleEndian.Uint16(header[2:]))
	if length < 9 || length > MaxConfigurationBytes {
		return nil, h.fail(fmt.Errorf("xHCI: configuration length %d", length))
	}
	descriptor, err := h.request(0x80, 6, 0x200, 0, length)
	if err != nil {
		return nil, h.fail(err)
	}
	h.config, err = ParseConfiguration(descriptor)
	if err != nil {
		return nil, h.fail(err)
	}
	if h.config.MaxPacketSize > 64 || (h.speed == 2 && h.config.MaxPacketSize > 8) ||
		(h.speed == 3 && h.config.Interval > 16) {
		return nil, h.fail(fmt.Errorf("xHCI: oversized boot keyboard endpoint"))
	}
	if _, err = h.request(0, 9, uint16(h.config.Value), 0, 0); err != nil {
		return nil, h.fail(err)
	}
	if _, err = h.request(0x21, 11, 0, uint16(h.config.Interface), 0); err != nil {
		return nil, h.fail(err)
	}
	if _, err = h.request(0x21, 10, 0, uint16(h.config.Interface), 0); err != nil {
		return nil, h.fail(err)
	}
	endpoint := uint32(h.config.Endpoint&15)*2 + 1
	clear(h.input)
	binary.LittleEndian.PutUint32(h.input[4:], 1|1<<endpoint)
	copy(h.input[32:64], h.output[:32])
	slot0 := binary.LittleEndian.Uint32(h.input[32:])
	binary.LittleEndian.PutUint32(h.input[32:], slot0&^(31<<27)|endpoint<<27)
	interval := uint32(h.config.Interval)
	if h.speed == 3 {
		interval--
	} else {
		n := uint32(0)
		for interval > 1 {
			interval >>= 1
			n++
		}
		interval = n + 3
	}
	h.setupEndpoint(h.input[(endpoint+1)*32:(endpoint+2)*32], h.interrupt.address, uint32(h.config.MaxPacketSize), 7, interval)
	if _, err = h.runCommand(h.inputAddress, 0, 12<<10|uint32(h.slot)<<24); err != nil {
		return nil, h.fail(err)
	}
	h.armKeyboard()
	return h, nil
}

func (h *XHCI) reserve(length, align int) (uint64, []byte) {
	h.next = (h.next + align - 1) &^ (align - 1)
	start := h.next
	h.next += length
	if h.next > len(h.memory) {
		panic("xHCI internal DMA budget")
	}
	return h.memoryAddress + uint64(start), h.memory[start:h.next]
}
func (h *XHCI) newRing() transferRing {
	address, data := h.reserve(4096, 64)
	return transferRing{address: address, data: data, cycle: 1}
}
func (h *XHCI) read(offset uintptr) uint32 { return read32(h.base + offset) }
func (h *XHCI) fail(err error) error       { h.fault = err; return err }
func (h *XHCI) setupEndpoint(data []byte, address uint64, packet, kind, interval uint32) {
	binary.LittleEndian.PutUint32(data, interval<<16)
	binary.LittleEndian.PutUint32(data[4:], packet<<16|kind<<3|3<<1)
	binary.LittleEndian.PutUint64(data[8:], address|1)
	payload := uint32(8) // Average TRB Length
	if kind == 7 {
		// USB2 periodic bandwidth, xHCI 4.14.2: MPS * (MaxBurst+1).
		payload |= packet << 16
	}
	binary.LittleEndian.PutUint32(data[16:], payload)
}

func waitRegister(address uintptr, mask, want uint32) error {
	deadline := time.Now().Add(time.Second)
	for read32(address)&mask != want {
		if time.Now().After(deadline) {
			return fmt.Errorf("xHCI register timeout at %#x value=%#x mask=%#x", address, read32(address), mask)
		}
		runtime.Gosched()
	}
	return nil
}

// Configuration returns the descriptor selected during real USB enumeration.
func (h *XHCI) Configuration() Configuration { return h.config }
func (h *XHCI) DeviceInfo() DeviceInfo       { return h.info }

func (h *XHCI) event() (eventTRB, bool, error) {
	if h.fault != nil {
		return eventTRB{}, false, h.fault
	}
	if read32(h.op+4)&(1<<2|1<<12) != 0 {
		return eventTRB{}, false, h.fail(fmt.Errorf("xHCI host controller error"))
	}
	offset := h.eventIndex * 16
	control := atomic.LoadUint32((*uint32)(unsafe.Pointer(&h.events[offset+12])))
	if control&1 != h.eventCycle {
		return eventTRB{}, false, nil
	}
	barrier()
	event := eventTRB{parameter: binary.LittleEndian.Uint64(h.events[offset:]), status: binary.LittleEndian.Uint32(h.events[offset+8:]), control: control}
	h.eventIndex++
	if h.eventIndex == 256 {
		h.eventIndex = 0
		h.eventCycle ^= 1
	}
	write64(h.interrupter+0x18, h.eventsAddress+uint64(h.eventIndex*16)|8)
	write32(h.interrupter, 1)
	if event.kind() == 34 {
		port := uint8(event.parameter >> 24)
		if port == h.port && read32(h.op+0x400+uintptr(port-1)*16)&1 == 0 {
			return eventTRB{}, false, h.fail(fmt.Errorf("xHCI: keyboard disconnected; reboot required"))
		}
	}
	return event, true, nil
}
func (h *XHCI) waitEvent(kind uint32, address uint64) (eventTRB, error) {
	deadline := time.Now().Add(2 * time.Second)
	for {
		event, ok, err := h.event()
		if err != nil {
			return eventTRB{}, err
		}
		if ok {
			if event.kind() == kind {
				if event.parameter&^15 != address {
					return eventTRB{}, fmt.Errorf("xHCI: unexpected completion address %#x expected %#x", event.parameter, address)
				}
				if event.code() != 1 {
					return eventTRB{}, fmt.Errorf("xHCI completion type=%d code=%d", kind, event.code())
				}
				return event, nil
			}
			if event.kind() != 34 {
				return eventTRB{}, fmt.Errorf("xHCI: unexpected event type=%d", event.kind())
			}
		}
		if time.Now().After(deadline) {
			return eventTRB{}, fmt.Errorf("xHCI timeout waiting event type=%d address=%#x", kind, address)
		}
		runtime.Gosched()
	}
}
func (h *XHCI) runCommand(parameter uint64, status, control uint32) (eventTRB, error) {
	address := h.command.push(parameter, status, control)
	barrier()
	write32(h.doorbell, 0)
	return h.waitEvent(33, address)
}

func (h *XHCI) request(requestType, request uint8, value, index uint16, length int) ([]byte, error) {
	if length < 0 || length > len(h.controlData) {
		return nil, fmt.Errorf("xHCI invalid control length")
	}
	clear(h.controlData)
	parameter := uint64(requestType) | uint64(request)<<8 | uint64(value)<<16 | uint64(index)<<32 | uint64(length)<<48
	direction := uint32(0)
	transferType := uint32(0)
	if length > 0 {
		transferType = 2
		if requestType&0x80 != 0 {
			direction = 1 << 16
			transferType = 3
		}
	}
	h.control.push(parameter, 8, 2<<10|1<<6|transferType<<16)
	if length > 0 {
		h.control.push(h.controlAddress, uint32(length), 3<<10|direction|1<<4)
	}
	statusDirection := uint32(1 << 16)
	if length > 0 && direction != 0 {
		statusDirection = 0
	}
	address := h.control.push(0, 0, 4<<10|1<<5|statusDirection)
	barrier()
	write32(h.doorbell+uintptr(h.slot)*4, 1)
	if _, err := h.waitEvent(32, address); err != nil {
		return nil, err
	}
	result := make([]byte, length)
	copy(result, h.controlData[:length])
	return result, nil
}
func (h *XHCI) armKeyboard() {
	clear(h.report)
	h.pending = h.interrupt.push(h.reportAddress, 8, 1<<10|1<<5)
	barrier()
	write32(h.doorbell+uintptr(h.slot)*4, uint32(h.config.Endpoint&15)*2+1)
}

// PollKeyboard consumes at most 16 event entries and never waits for a key.
// A non-nil result is one eight-byte boot report. The controller owns the next
// interrupt transfer before this method returns. Errors are sticky.
func (h *XHCI) PollKeyboard() ([]byte, error) {
	for n := 0; n < 16; n++ {
		event, ok, err := h.event()
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, nil
		}
		if event.kind() == 34 {
			continue
		}
		if event.kind() != 32 || event.parameter&^15 != h.pending || uint8(event.control>>24) != h.slot {
			return nil, h.fail(fmt.Errorf("xHCI: unexpected keyboard completion"))
		}
		if event.code() != 1 || event.status&0xffffff != 0 {
			return nil, h.fail(fmt.Errorf("xHCI: invalid keyboard transfer code=%d remainder=%d", event.code(), event.status&0xffffff))
		}
		report := append([]byte(nil), h.report...)
		h.armKeyboard()
		return report, nil
	}
	return nil, nil
}

type transferRing struct {
	address uint64
	data    []byte
	index   int
	cycle   uint32
}

func (r *transferRing) push(parameter uint64, status, control uint32) uint64 {
	if r.index == 255 {
		putTRB(r.data[255*16:], r.address, 0, 6<<10|1<<1|r.cycle)
		r.index = 0
		r.cycle ^= 1
	}
	offset := r.index * 16
	address := r.address + uint64(offset)
	putTRB(r.data[offset:], parameter, status, control|r.cycle)
	r.index++
	return address
}
func putTRB(data []byte, parameter uint64, status, control uint32) {
	binary.LittleEndian.PutUint64(data, parameter)
	binary.LittleEndian.PutUint32(data[8:], status)
	barrier()
	atomic.StoreUint32((*uint32)(unsafe.Pointer(&data[12])), control)
}

type eventTRB struct {
	parameter       uint64
	status, control uint32
}

func (e eventTRB) kind() uint32 { return e.control >> 10 & 63 }
func (e eventTRB) code() uint32 { return e.status >> 24 }

func read32(uintptr) uint32
func write32(uintptr, uint32)
func barrier()
func write64(address uintptr, value uint64) {
	write32(address, uint32(value))
	write32(address+4, uint32(value>>32))
}
