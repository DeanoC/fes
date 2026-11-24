// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

func (h *MultiHost) allocAddress() (uint8, error) {
	for n := 0; n < maxMultiDevices; n++ {
		cand := h.nextID
		h.nextID++
		if h.nextID > maxMultiDevices {
			h.nextID = 1
		}
		if h.devices[cand] == nil {
			return cand, nil
		}
	}
	return 0, fmt.Errorf("dwc2: USB address space exhausted")
}

// detachDevice cleanly halts all channels owned by addr (and recursively any
// downstream hub children), removes it from the device table, and emits a
// DeviceDetached event without disturbing sibling devices.
func (h *MultiHost) detachDevice(addr uint8) {
	if int(addr) == 0 || int(addr) >= len(h.devices) || h.devices[addr] == nil {
		return
	}
	dev := h.devices[addr]
	// First detach any children behind this device if it is a hub.
	if dev.isHub {
		for a := uint8(1); a <= maxMultiDevices; a++ {
			if child := h.devices[a]; child != nil && child.info.ParentHub == addr {
				h.detachDevice(a)
			}
		}
		for i := 0; i < len(h.hubs); i++ {
			if h.hubs[i].addr == addr {
				if h.hubs[i].statusPipe != nil {
					_ = h.hubs[i].statusPipe.Close()
				}
				h.hubs = append(h.hubs[:i], h.hubs[i+1:]...)
				break
			}
		}
	}
	// Halt and free every hardware channel belonging to addr.
	for ch := 1; ch < h.channels; ch++ {
		if h.slots[ch].allocated && h.slots[ch].ownerAddr == addr {
			if err := h.haltChannel(ch); err != nil {
				// Never clear or reuse a channel that hardware failed to halt.
				h.slots[ch].fault = err
				_ = h.failController(err)
				continue
			}
			h.slots[ch] = multiChannel{}
		}
	}
	h.devices[addr] = nil
	if !dev.isHub {
		h.pushEvent(usb.HostEvent{Kind: usb.DeviceDetached, Device: dev.info})
	}
	if h.irqMode.Load() && h.irqActive.Load() {
		h.io.Write32(haintmsk, h.activeChannelMask())
	}
}

func (h *MultiHost) detachAllDevices() {
	for a := uint8(1); a <= maxMultiDevices; a++ {
		if h.devices[a] != nil {
			h.detachDevice(a)
		}
	}
	h.rootConn = false
	h.rootRetryCount = 0
}

func (h *MultiHost) scheduleRootRetry() {
	if h.rootRetryCount < maxHubEnumRetries {
		h.rootRetryCount++
		h.rootRetryAt = h.io.Now().Add(hubEnumRetryDelay)
	} else {
		h.rootRetryCount = 0
	}
}

func (h *MultiHost) pollRootHotplug() {
	port := h.io.Read32(hprt)
	gintVal := h.io.Read32(gint)
	conn := port&1 != 0
	connChanged := port&(1<<1) != 0
	enabled := port&(1<<2) != 0
	enableChanged := port&(1<<3) != 0
	overcurr := port&(1<<4) != 0
	overcurrChanged := port&(1<<5) != 0
	disconnIRQ := gintVal&gintDisconn != 0

	var w1c uint32
	if connChanged {
		w1c |= 1 << 1
	}
	if enableChanged {
		w1c |= 1 << 3
	}
	if overcurrChanged {
		w1c |= 1 << 5
	}
	if w1c != 0 {
		// Clear W1C connect/enable/overcurrent change bits without disabling PRTENA.
		h.portWrite(w1c, 0)
	}
	if disconnIRQ {
		h.io.Write32(gint, gintDisconn)
	}
	portLostEnable := h.rootConn && (!enabled || overcurr)
	if !conn || disconnIRQ || portLostEnable {
		h.rootRetryCount = 0
		if h.rootConn {
			h.detachAllDevices()
		}
	}
	if !h.rootConn && conn && !overcurr {
		h.rootRetryCount = 0
		h.enumerateRoot()
		return
	}
	if h.rootConn && conn && !overcurr && h.rootRetryCount > 0 && !h.io.Now().Before(h.rootRetryAt) {
		h.enumerateRoot()
	}
}

func (h *MultiHost) enumerateRoot() {
	// Reset root port and wait for enable.
	h.portWrite(1<<8, 0)
	if err := h.delay(50 * time.Millisecond); err != nil {
		h.scheduleRootRetry()
		return
	}
	h.portWrite(0, 1<<8)
	if err := h.waitReg(hprt, 1|1<<2, 1|1<<2, 500*time.Millisecond); err != nil {
		h.scheduleRootRetry()
		return
	}
	if err := h.delay(10 * time.Millisecond); err != nil {
		h.scheduleRootRetry()
		return
	}
	spd := (h.io.Read32(hprt) >> 17) & 3
	if spd > 2 {
		return
	}
	h.rootSpeed = usb.Speed(spd)
	h.rootConn = true
	interval := uint32(59999)
	if h.rootSpeed == usb.SpeedHigh {
		interval = 7499
	}
	h.io.Write32(hfir, interval)
	h.io.Write32(gint, gintDisconn)
	if err := h.enumerateDevice(h.rootSpeed, 0, 0, splitRoute{}, 0); err != nil {
		h.scheduleRootRetry()
		return
	}
	h.rootRetryCount = 0
}

func (h *MultiHost) enumerateDevice(speed usb.Speed, parentHub, parentPort uint8, route splitRoute, depth int) error {
	addr, err := h.allocAddress()
	if err != nil {
		return err
	}
	ep0 := uint16(8)
	if speed == usb.SpeedHigh {
		ep0 = 64
	}
	// 1. Read first 8 bytes of device descriptor at address 0 to learn bMaxPacketSize0.
	var hdr [8]byte
	n, err := h.controlTransferTarget(0, speed, ep0, route, 0x80, 6, 0x0100, 0, hdr[:])
	if err != nil || n != 8 || hdr[0] != 18 || hdr[1] != 1 {
		return fmt.Errorf("dwc2: invalid device header err=%v n=%d", err, n)
	}
	mps0 := uint16(hdr[7])
	if mps0 != 8 && mps0 != 16 && mps0 != 32 && mps0 != 64 {
		return fmt.Errorf("dwc2: invalid EP0 MPS %d", mps0)
	}
	ep0 = mps0

	// 2. SET_ADDRESS(addr)
	if _, err := h.controlTransferTarget(0, speed, ep0, route, 0x00, 5, uint16(addr), 0, nil); err != nil {
		return err
	}
	if err := h.delay(2 * time.Millisecond); err != nil {
		return err
	}

	// 3. Read full 18-byte device descriptor at new address.
	var devRaw [18]byte
	n, err = h.controlTransferTarget(addr, speed, ep0, route, 0x80, 6, 0x0100, 0, devRaw[:])
	if err != nil || n != 18 {
		return fmt.Errorf("dwc2: full device descriptor failed err=%v n=%d", err, n)
	}
	desc, err := usb.ParseDeviceDescriptor(devRaw[:])
	if err != nil {
		return err
	}

	// 4. Read configuration descriptor header, then full tree.
	var cfgHdr [9]byte
	n, err = h.controlTransferTarget(addr, speed, ep0, route, 0x80, 6, 0x0200, 0, cfgHdr[:])
	if err != nil || n != 9 || cfgHdr[0] != 9 || cfgHdr[1] != 2 {
		return fmt.Errorf("dwc2: invalid config header err=%v n=%d", err, n)
	}
	totalLen := int(binary.LittleEndian.Uint16(cfgHdr[2:4]))
	if totalLen < 9 || totalLen > usb.MaxConfigurationBytes {
		return fmt.Errorf("dwc2: invalid config length %d", totalLen)
	}
	cfgBuf := make([]byte, totalLen)
	n, err = h.controlTransferTarget(addr, speed, ep0, route, 0x80, 6, 0x0200, 0, cfgBuf)
	if err != nil || n != totalLen {
		return fmt.Errorf("dwc2: short config descriptor err=%v n=%d/%d", err, n, totalLen)
	}
	cfg, err := usb.ParseFullConfiguration(cfgBuf)
	if err != nil {
		return err
	}

	// 5. SET_CONFIGURATION(cfg.Value)
	if _, err := h.controlTransferTarget(addr, speed, ep0, route, 0x00, 9, uint16(cfg.Value), 0, nil); err != nil {
		return err
	}

	isHub := desc.Class == 9
	dev := &multiDevice{
		info: usb.BusDevice{
			Address:    addr,
			Speed:      speed,
			ParentHub:  parentHub,
			ParentPort: parentPort,
			Descriptor: desc,
			Config:     cfg,
		},
		route: route,
		isHub: isHub,
	}
	h.devices[addr] = dev

	if !isHub {
		h.pushEvent(usb.HostEvent{Kind: usb.DeviceAttached, Device: dev.info})
		return nil
	}
	return h.initHub(dev, depth)
}

func (h *MultiHost) initHub(dev *multiDevice, depth int) error {
	addr := dev.info.Address
	if depth >= maxHubDepth || dev.info.Speed == usb.SpeedLow {
		h.detachDevice(addr)
		return fmt.Errorf("dwc2: unsupported hub depth/speed")
	}

	// Enable Multi-TT (SET_INTERFACE alt=1) when high-speed hub advertises
	// protocol 2 (Multi-TT) and an alternate setting 1 exists.
	if dev.info.Speed == usb.SpeedHigh && dev.info.Descriptor.Protocol == 2 {
		for _, iface := range dev.info.Config.Interfaces {
			if iface.Class == 9 && iface.AlternateSetting == 1 {
				_, _ = h.ControlTransfer(addr, 0x01, 11, 1, uint16(iface.Number), nil)
				break
			}
		}
	}

	// Read 7-byte hub descriptor header to learn bNbrPorts and bPwrOn2PwrGood.
	var hdr [7]byte
	if n, err := h.ControlTransfer(addr, 0xa0, 6, 0x2900, 0, hdr[:]); err != nil || n != 7 || hdr[1] != 0x29 || hdr[2] == 0 || hdr[2] > maxHubPorts {
		h.detachDevice(addr)
		return fmt.Errorf("dwc2: invalid hub descriptor err=%v n=%d", err, n)
	}
	numPorts := hdr[2]
	pwrDelay := time.Duration(max(20, int(hdr[5])*2)) * time.Millisecond

	// Power on all downstream ports.
	for port := uint16(1); port <= uint16(numPorts); port++ {
		if _, err := h.ControlTransfer(addr, 0x23, 3, 8, port, nil); err != nil {
			h.detachDevice(addr)
			return err
		}
	}
	if err := h.delay(pwrDelay); err != nil {
		h.detachDevice(addr)
		return err
	}

	// Open the hub's Interrupt-IN status-change endpoint for zero-stall hotplug.
	hub := &multiHub{
		addr:     addr,
		speed:    dev.info.Speed,
		route:    dev.route,
		depth:    depth,
		numPorts: numPorts,
	}
	h.openHubStatusPipe(hub)
	h.hubs = append(h.hubs, hub)

	// Enumerate initially connected downstream ports (isolated per port so one
	// broken port does not abort sibling devices on the same hub).
	for port := uint8(1); port <= numPorts; port++ {
		h.handleHubPortChange(hub, port, depth+1)
	}
	return nil
}

func (h *MultiHost) openHubStatusPipe(hub *multiHub) {
	if hub == nil || hub.statusPipe != nil {
		return
	}
	dev := h.devices[hub.addr]
	if dev == nil {
		return
	}
	if iface, ok := dev.info.Config.FindInterface(9, 0, -1); ok {
		if ep, ok := iface.FindEndpoint(usb.TransferInterrupt, true); ok {
			if p, err := h.OpenPipe(hub.addr, ep); err == nil {
				hub.statusPipe = p.(*multiPipe)
				hub.statusRetryAt = time.Time{}
				return
			}
		}
	}
	hub.statusRetryAt = h.io.Now().Add(hubEnumRetryDelay)
}

func (h *MultiHost) pollHubHotplug() {
	var statusBuf [4]byte
	now := h.io.Now()
	// Iterate over a snapshot index since detaching a nested hub mutates h.hubs.
	for i := 0; i < len(h.hubs); i++ {
		hub := h.hubs[i]
		if hub.statusPipe == nil && !now.Before(hub.statusRetryAt) {
			h.openHubStatusPipe(hub)
		}
		if hub.statusPipe != nil {
			n, err := hub.statusPipe.PollInto(statusBuf[:])
			if err != nil {
				// Close the faulted status pipe and retry after hubEnumRetryDelay so
				// transient errors recover without churning channels on permanent faults.
				_ = hub.statusPipe.Close()
				hub.statusPipe = nil
				hub.statusRetryAt = now.Add(hubEnumRetryDelay)
				if h.fault != nil {
					return
				}
			} else if n > 0 {
				// Bit p (1..numPorts) in the status bitmap indicates a change on port p.
				for port := uint8(1); port <= hub.numPorts; port++ {
					byteIdx := int(port) / 8
					bit := uint(port % 8)
					if byteIdx < n && statusBuf[byteIdx]&(1<<bit) != 0 {
						hub.retryCount[port] = 0
						h.handleHubPortChange(hub, port, hub.depth+1)
					}
				}
			}
		}
		// Retry any port whose initial hotplug enumeration failed transiently.
		for port := uint8(1); port <= hub.numPorts; port++ {
			if hub.retryCount[port] > 0 && !now.Before(hub.retryAt[port]) {
				h.handleHubPortChange(hub, port, hub.depth+1)
			}
		}
	}
}

func (h *MultiHost) scheduleHubPortRetry(hub *multiHub, port uint8) {
	if int(port) >= len(hub.retryCount) {
		return
	}
	if hub.retryCount[port] < maxHubEnumRetries {
		hub.retryCount[port]++
		hub.retryAt[port] = h.io.Now().Add(hubEnumRetryDelay)
	} else {
		hub.retryCount[port] = 0
	}
}

func (h *MultiHost) handleHubPortChange(hub *multiHub, port uint8, depth int) {
	var raw [4]byte
	if n, err := h.ControlTransfer(hub.addr, 0xa3, 0, 0, uint16(port), raw[:]); err != nil || n != 4 {
		return
	}
	status := binary.LittleEndian.Uint32(raw[:])
	// Clear any latched C_PORT_* change bits (16..20).
	for feat := uint16(16); feat <= 20; feat++ {
		if status&(1<<feat) != 0 {
			_, _ = h.ControlTransfer(hub.addr, 0x23, 1, feat, uint16(port), nil)
		}
	}

	// Find any existing device attached to (hub.addr, port).
	var existing uint8
	for a := uint8(1); a <= maxMultiDevices; a++ {
		if d := h.devices[a]; d != nil && d.info.ParentHub == hub.addr && d.info.ParentPort == port {
			existing = a
			break
		}
	}

	connected := status&1 != 0
	overcurrent := status&(1<<3) != 0
	if !connected || overcurrent {
		hub.retryCount[port] = 0
		if existing != 0 {
			h.detachDevice(existing)
		}
		return
	}
	// If already enumerated and still enabled with no connect bounce, keep alive.
	if existing != 0 && status&(1<<16) == 0 && status&2 != 0 {
		hub.retryCount[port] = 0
		return
	}
	if existing != 0 {
		h.detachDevice(existing)
	}

	// Reset downstream port and wait for enable.
	if _, err := h.ControlTransfer(hub.addr, 0x23, 3, 4, uint16(port), nil); err != nil {
		h.scheduleHubPortRetry(hub, port)
		return
	}
	if err := h.delay(50 * time.Millisecond); err != nil {
		h.scheduleHubPortRetry(hub, port)
		return
	}
	deadline := h.io.Now().Add(time.Second)
	for {
		if n, err := h.ControlTransfer(hub.addr, 0xa3, 0, 0, uint16(port), raw[:]); err != nil || n != 4 {
			h.scheduleHubPortRetry(hub, port)
			return
		}
		status = binary.LittleEndian.Uint32(raw[:])
		if status&1 == 0 || status&(1<<3) != 0 {
			hub.retryCount[port] = 0
			return
		}
		if status&(1<<4) == 0 && status&2 != 0 {
			break
		}
		if !h.io.Now().Before(deadline) {
			h.scheduleHubPortRetry(hub, port)
			return
		}
		_ = h.delay(time.Millisecond)
	}
	for feat := uint16(16); feat <= 20; feat++ {
		if status&(1<<feat) != 0 {
			_, _ = h.ControlTransfer(hub.addr, 0x23, 1, feat, uint16(port), nil)
		}
	}
	_ = h.delay(10 * time.Millisecond)

	spd, err := portSpeed(status)
	if err != nil {
		return
	}
	childSpeed := usb.Speed(spd)
	route := hub.route
	if hub.speed == usb.SpeedHigh {
		route = splitRoute{}
		if childSpeed != usb.SpeedHigh {
			route = splitRoute{hub: uint32(hub.addr), port: uint32(port)}
		}
	}
	if err := h.enumerateDevice(childSpeed, hub.addr, port, route, depth); err != nil {
		h.scheduleHubPortRetry(hub, port)
		return
	}
	hub.retryCount[port] = 0
}
