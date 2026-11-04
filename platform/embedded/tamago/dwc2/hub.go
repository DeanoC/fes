// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
	"strings"
	"time"
)

const maxHubPorts = 15
const maxDevices = 31
const maxHubDepth = 3

type target struct {
	address, speed uint32
	ep0            uint16
	route          splitRoute
}
type hubPort struct {
	hub   target
	port  uint16
	speed uint32
}

func (h *Host) target() target { return target{h.address, h.speed, h.ep0, h.route} }
func (h *Host) selectTarget(t target) {
	h.address = t.address
	h.speed = t.speed
	h.ep0 = t.ep0
	h.route = t.route
}

func (h *Host) descriptor(address uint32) ([]byte, error) {
	header, err := h.request(0x80, 6, 0x100, 0, 8)
	if err != nil {
		return nil, err
	}
	if len(header) != 8 || header[0] != 18 || header[1] != 1 {
		return nil, fmt.Errorf("dwc2: invalid device header")
	}
	mps := uint16(header[7])
	if mps != 8 && mps != 16 && mps != 32 && mps != 64 || h.speed == 2 && mps != 8 || h.speed == 0 && mps != 64 {
		return nil, fmt.Errorf("dwc2: invalid EP0 size")
	}
	h.ep0 = mps
	if _, err = h.request(0, 5, uint16(address), 0, 0); err != nil {
		return nil, err
	}
	if err = h.pause(2 * time.Millisecond); err != nil {
		return nil, err
	}
	h.address = address
	data, err := h.request(0x80, 6, 0x100, 0, 18)
	if err != nil {
		return nil, err
	}
	if len(data) != 18 || data[0] != 18 || data[1] != 1 || data[7] != byte(mps) || data[17] != 1 {
		return nil, fmt.Errorf("dwc2: unsupported device descriptor")
	}
	return data, nil
}
func (h *Host) configuration() ([]byte, error) {
	header, err := h.request(0x80, 6, 0x200, 0, 9)
	if err != nil {
		return nil, err
	}
	if len(header) != 9 || header[0] != 9 || header[1] != 2 {
		return nil, fmt.Errorf("dwc2: invalid configuration header")
	}
	size := int(binary.LittleEndian.Uint16(header[2:4]))
	if size < 9 || size > usb.MaxConfigurationBytes {
		return nil, fmt.Errorf("dwc2: invalid configuration length")
	}
	data, err := h.request(0x80, 6, 0x200, 0, size)
	if err != nil {
		return nil, err
	}
	if len(data) != size {
		return nil, fmt.Errorf("dwc2: short configuration")
	}
	return data, nil
}
func hubConfiguration(data []byte) (byte, error) {
	// Reuse the full structural parser, including alternate settings and endpoint
	// counts. A hub must not also advertise an input keyboard interface.
	if _, err := usb.ParseConfiguration(data); !errors.Is(err, usb.ErrNoBootKeyboard) {
		return 0, fmt.Errorf("dwc2: invalid hub configuration: %v", err)
	}
	if data[4] != 1 {
		return 0, fmt.Errorf("dwc2: hub interface count")
	}
	selected, endpoint := false, false
	for off := 9; off < len(data); off += int(data[off]) {
		d := data[off : off+int(data[off])]
		if d[1] == 4 {
			selected = d[3] == 0
			if selected && (d[4] != 1 || d[5] != 9 || d[6] != 0 || d[7] > 2) {
				return 0, fmt.Errorf("dwc2: invalid hub interface")
			}
		}
		if selected && d[1] == 5 {
			if endpoint || len(d) != 7 || d[2]&0x80 == 0 || d[3] != 3 || binary.LittleEndian.Uint16(d[4:6]) > 64 || d[6] == 0 {
				return 0, fmt.Errorf("dwc2: invalid hub interrupt endpoint")
			}
			endpoint = true
		}
	}
	if !endpoint {
		return 0, fmt.Errorf("dwc2: missing hub endpoint")
	}
	return data[5], nil
}
func (h *Host) portStatus(port uint16) (uint32, error) {
	data, err := h.request(0xa3, 0, 0, port, 4)
	if err != nil {
		return 0, err
	}
	if len(data) != 4 {
		return 0, fmt.Errorf("dwc2: short hub port status")
	}
	return binary.LittleEndian.Uint32(data), nil
}
func (h *Host) portFeature(port, feature uint16, set bool) error {
	op := byte(1)
	if set {
		op = 3
	}
	_, err := h.request(0x23, op, feature, port, 0)
	return err
}
func portSpeed(status uint32) (uint32, error) {
	switch status & (3 << 9) {
	case 0:
		return 1, nil
	case 1 << 9:
		return 2, nil
	case 1 << 10:
		return 0, nil
	}
	return 0, fmt.Errorf("dwc2: contradictory hub port speed")
}
func validPort(status uint32, speed uint32) bool {
	actual, err := portSpeed(status)
	return err == nil && actual == speed && status&(1|2|1<<8) == 1|2|1<<8 && status&(1<<2|1<<3|1<<4|0xffff0000) == 0
}
func (h *Host) resetHubPort(port uint16) (uint32, error) {
	before, err := h.portStatus(port)
	if err != nil {
		return 0, err
	}
	if before&1 == 0 || before&(1<<3) != 0 || before&(1<<8) == 0 {
		return 0, fmt.Errorf("dwc2: disconnected/unpowered/overcurrent hub port")
	}
	for feature := uint16(16); feature <= 20; feature++ {
		if before&(1<<feature) != 0 {
			if err = h.portFeature(port, feature, false); err != nil {
				return 0, err
			}
		}
	}
	// Clear initial changes, then require a stable 100ms connection before
	// resetting. A bounce is a new topology, never silently adopted.
	if err = h.pause(100 * time.Millisecond); err != nil {
		return 0, err
	}
	stable, err := h.portStatus(port)
	if err != nil {
		return 0, err
	}
	if stable&1 == 0 || stable&(1<<3|1<<16) != 0 || stable&(1<<8) == 0 {
		return 0, fmt.Errorf("dwc2: hub connection did not debounce")
	}
	if err = h.portFeature(port, 4, true); err != nil {
		return 0, err
	}
	if err = h.pause(50 * time.Millisecond); err != nil {
		return 0, err
	}
	deadline := h.io.Now().Add(time.Second)
	var status uint32
	for count := 0; count < 1000; count++ {
		status, err = h.portStatus(port)
		if err != nil {
			return 0, err
		}
		if status&1 == 0 || status&(1<<3) != 0 {
			return 0, fmt.Errorf("dwc2: hub disconnected/overcurrent during reset")
		}
		if status&(1<<4) == 0 && status&2 != 0 && status&(1<<20) != 0 {
			break
		}
		if !h.io.Now().Before(deadline) || count == 999 {
			return 0, fmt.Errorf("dwc2: hub port reset timeout")
		}
		if err = h.pause(time.Millisecond); err != nil {
			return 0, err
		}
	}
	for feature := uint16(16); feature <= 20; feature++ {
		if status&(1<<feature) != 0 {
			if err = h.portFeature(port, feature, false); err != nil {
				return 0, err
			}
		}
	}
	if err = h.pause(10 * time.Millisecond); err != nil {
		return 0, err
	}
	status, err = h.portStatus(port)
	if err != nil {
		return 0, err
	}
	speed, err := portSpeed(status)
	if err != nil || !validPort(status, speed) {
		return 0, fmt.Errorf("dwc2: unstable hub port after reset")
	}
	return speed, nil
}
func (h *Host) discover() error {
	h.nextAddress = 1
	h.portBudget = 31
	if err := h.scan(nil, 0); err != nil {
		return err
	}
	if !h.keyboardFound {
		return fmt.Errorf("dwc2: %w; discovered %s", usb.ErrNoBootKeyboard, strings.Join(h.devices, ", "))
	}
	h.selectTarget(h.keyboard)
	if _, err := h.request(0, 9, uint16(h.config.Value), 0, 0); err != nil {
		return err
	}
	if _, err := h.request(0x21, 11, 0, uint16(h.config.Interface), 0); err != nil {
		return err
	}
	if _, err := h.request(0x21, 10, 0, uint16(h.config.Interface), 0); err != nil {
		return err
	}
	h.interval = time.Duration(h.config.Interval) * time.Millisecond
	if h.speed == 0 {
		if h.config.Interval > 16 {
			return fmt.Errorf("dwc2: invalid high-speed interval")
		}
		h.interval = (125 * time.Microsecond) << uint(h.config.Interval-1)
	}
	h.healthNext = h.io.Now().Add(100 * time.Millisecond)
	return nil
}
func (h *Host) scan(path []hubPort, depth int) error {
	if h.nextAddress > maxDevices {
		return fmt.Errorf("dwc2: device budget exceeded")
	}
	address := h.nextAddress
	h.nextAddress++
	device, err := h.descriptor(address)
	if err != nil {
		return err
	}
	h.devices = append(h.devices, fmt.Sprintf("addr=%d id=%04x:%04x class=%02x speed=%d", address, binary.LittleEndian.Uint16(device[8:10]), binary.LittleEndian.Uint16(device[10:12]), device[4], h.speed))
	config, err := h.configuration()
	if err != nil {
		return err
	}
	if device[4] != 9 {
		selected, err := usb.ParseConfiguration(config)
		if errors.Is(err, usb.ErrNoBootKeyboard) {
			return nil
		}
		if err != nil {
			return err
		}
		if selected.MaxPacketSize > 64 || h.speed == 2 && (selected.MaxPacketSize > 8 || selected.Interval < 10) {
			return fmt.Errorf("dwc2: unsupported keyboard packet size")
		}
		if h.keyboardFound {
			return fmt.Errorf("dwc2: ambiguous boot keyboards")
		}
		h.keyboardFound = true
		h.keyboard = h.target()
		h.config = selected
		h.path = append([]hubPort(nil), path...)
		return nil
	}
	if depth >= maxHubDepth || h.speed == 2 || device[6] > 2 || h.speed == 1 && device[6] != 0 {
		return fmt.Errorf("dwc2: unsupported hub depth/speed/protocol")
	}
	value, err := hubConfiguration(config)
	if err != nil {
		return err
	}
	if _, err = h.request(0, 9, uint16(value), 0, 0); err != nil {
		return err
	}
	header, err := h.request(0xa0, 6, 0x2900, 0, 7)
	if err != nil {
		return err
	}
	if len(header) != 7 || header[1] != 0x29 || header[2] == 0 || header[2] > maxHubPorts || int(header[2]) > h.portBudget {
		return fmt.Errorf("dwc2: invalid hub descriptor/port budget")
	}
	count := int(header[2])
	length := 7 + 2*((count+1+7)/8)
	if int(header[0]) != length {
		return fmt.Errorf("dwc2: invalid hub bitmap length")
	}
	descriptor, err := h.request(0xa0, 6, 0x2900, 0, length)
	if err != nil {
		return err
	}
	if len(descriptor) != length || string(descriptor[:7]) != string(header) {
		return fmt.Errorf("dwc2: hub descriptor changed")
	}
	h.portBudget -= count
	characteristics := binary.LittleEndian.Uint16(header[3:5])
	for port := 1; port <= count; port++ {
		if characteristics&2 == 0 {
			if err = h.portFeature(uint16(port), 8, true); err != nil {
				return err
			}
		}
	}
	if err = h.pause(time.Duration(max(20, int(header[5])*2)) * time.Millisecond); err != nil {
		return err
	}
	hubStatus, err := h.request(0xa0, 0, 0, 0, 4)
	if err != nil {
		return err
	}
	if len(hubStatus) != 4 || binary.LittleEndian.Uint32(hubStatus)&(2|2<<16) != 0 {
		return fmt.Errorf("dwc2: hub overcurrent/status")
	}
	parent := h.target()
	for port := uint16(1); port <= uint16(count); port++ {
		h.selectTarget(parent)
		status, err := h.portStatus(port)
		if err != nil {
			return err
		}
		if status&(1<<3) != 0 {
			return fmt.Errorf("dwc2: hub port overcurrent")
		}
		if status&1 == 0 {
			continue
		}
		speed, err := h.resetHubPort(port)
		if err != nil {
			return err
		}
		if parent.speed != 0 && speed == 0 {
			return fmt.Errorf("dwc2: high-speed child behind full-speed hub")
		}
		route := parent.route
		if parent.speed == 0 {
			route = splitRoute{}
			if speed != 0 {
				route = splitRoute{parent.address, uint32(port)}
			}
		}
		h.selectTarget(target{0, speed, 8, route})
		if speed == 0 {
			h.ep0 = 64
		}
		childPath := append(append([]hubPort(nil), path...), hubPort{parent, port, speed})
		if err = h.scan(childPath, depth+1); err != nil {
			return err
		}
	}
	h.selectTarget(parent)
	return nil
}
