// SPDX-License-Identifier: GPL-3.0-or-later

package usbhub

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
	"unicode/utf16"

	"github.com/DeanoC/fes/platform/embedded/tamago/input/controllers"
	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
	"github.com/DeanoC/fes/platform/embedded/tamago/usbcontrol"
)

func (h *Hub) attachDevice(dev usb.BusDevice) {
	if dev.Address == 0 {
		return
	}
	for _, iface := range dev.Config.Interfaces {
		if iface.AlternateSetting != 0 {
			continue
		}
		if iface.Class == 0xff && iface.SubClass == 0x5d && iface.Protocol == 0x01 {
			h.attachWiredNativeController(dev, iface, controllers.KindXInput)
			continue
		}
		if iface.Number == 0 && iface.Class == 0xff && iface.SubClass == 0x47 && iface.Protocol == 0xd0 {
			h.attachWiredNativeController(dev, iface, controllers.KindXboxGIP)
			continue
		}
		if iface.Class != 3 {
			continue
		}
		if iface.SubClass == 1 && iface.Protocol == 1 {
			h.attachWiredKeyboard(dev.Address, iface)
			continue
		}
		if iface.SubClass == 1 && iface.Protocol == 2 {
			h.attachWiredMouse(dev.Address, iface)
			continue
		}
		if kind := controllers.IdentifyByVIDPID(dev.Descriptor.VendorID, dev.Descriptor.ProductID); kind != controllers.KindUnknown {
			h.attachWiredNativeController(dev, iface, kind)
			continue
		}
		h.attachWiredGamepad(dev, iface)
	}
}

func (h *Hub) detachDevice(addr uint8) {
	if addr == 0 {
		return
	}
	for i := range h.wiredKeyboards {
		if h.wiredKeyboards[i].active && h.wiredKeyboards[i].addr == addr {
			h.closeWiredKeyboard(i)
		}
	}
	for i := range h.wiredMice {
		if h.wiredMice[i].active && h.wiredMice[i].addr == addr {
			h.closeWiredMouse(i)
		}
	}
	for i := range h.wiredGamepads {
		if h.wiredGamepads[i].active && h.wiredGamepads[i].addr == addr {
			h.closeWiredGamepad(i)
		}
	}
}

func (h *Hub) attachWiredKeyboard(addr uint8, iface usb.InterfaceDescriptor) {
	ep, ok := iface.FindEndpoint(usb.TransferInterrupt, true)
	if !ok || ep.MaxPacketSize < 8 || ep.MaxPacketSize > usb.MaxHIDReportBytes {
		return
	}
	free := -1
	for i := range h.wiredKeyboards {
		slot := &h.wiredKeyboards[i]
		if slot.active {
			if slot.addr == addr && slot.iface == iface.Number {
				return
			}
		} else if free < 0 {
			free = i
		}
	}
	if free < 0 {
		return
	}
	if _, err := h.bus.ControlTransfer(addr, 0x21, 0x0b, 0, uint16(iface.Number), nil); err != nil {
		return
	}
	// Request 100 ms HID idle snapshots where supported; devices that STALL
	// SET_IDLE still operate on state-change interrupt reports.
	_, _ = h.bus.ControlTransfer(addr, 0x21, 0x0a, 25<<8, uint16(iface.Number), nil)
	pipe, err := h.bus.OpenPipe(addr, ep)
	if err != nil {
		return
	}
	h.wiredKeyboards[free] = wiredKeyboardSlot{
		active: true,
		addr:   addr,
		iface:  iface.Number,
		pipe:   pipe,
	}
	h.emit(DeviceEvent{
		Kind:      EventKeyboardAttached,
		Address:   addr,
		Interface: iface.Number,
		Slot:      uint8(free),
	})
}

func (h *Hub) findFreeWiredGamepadSlot(addr, iface uint8) (int, bool) {
	free := -1
	for i := range h.wiredGamepads {
		slot := &h.wiredGamepads[i]
		if slot.active {
			if slot.addr == addr && slot.iface == iface {
				return -1, false
			}
		} else if free < 0 {
			free = i
		}
	}
	if free < 0 {
		return -1, false
	}
	return free, true
}

func (h *Hub) attachWiredGamepad(dev usb.BusDevice, iface usb.InterfaceDescriptor) {
	addr := dev.Address
	if iface.HIDReportBytes == 0 || iface.HIDReportBytes > usb.MaxConfigurationBytes {
		return
	}
	ep, ok := iface.FindEndpoint(usb.TransferInterrupt, true)
	if !ok || ep.MaxPacketSize == 0 || ep.MaxPacketSize > usb.MaxHIDReportBytes {
		return
	}
	free, ok := h.findFreeWiredGamepadSlot(addr, iface.Number)
	if !ok {
		return
	}
	n, err := h.bus.ControlTransfer(addr, 0x81, 0x06, 0x2200, uint16(iface.Number), h.descBuf[:iface.HIDReportBytes])
	if err != nil || n != int(iface.HIDReportBytes) {
		return
	}
	decoder, err := usb.ParseGamepad(h.descBuf[:n])
	if err != nil || decoder.MaxReportBytes() > int(ep.MaxPacketSize) {
		return
	}
	pipe, err := h.bus.OpenPipe(addr, ep)
	if err != nil {
		return
	}
	h.wiredGamepads[free] = wiredGamepadSlot{
		active:    true,
		addr:      addr,
		iface:     iface.Number,
		name:      h.usbHIDInputName(dev),
		vendorID:  dev.Descriptor.VendorID,
		productID: dev.Descriptor.ProductID,
		version:   dev.Descriptor.DeviceVersion,
		kind:      controllers.KindGenericHID,
		pipe:      pipe,
		decoder:   decoder,
		state:     usb.GamepadState{Hat: -1},
	}
	h.assignPlayerPort(playerPortWired, uint8(free))
	h.padGeneration++
	h.emit(DeviceEvent{
		Kind:      EventGamepadAttached,
		Address:   addr,
		Interface: iface.Number,
		Slot:      uint8(free),
		Name:      controllers.KindGenericHID.String(),
	})
}

func xboxGIPNeedsSInit(vid, pid uint16) bool {
	return vid == 0x045e && (pid == 0x02ea || pid == 0x0b00)
}

func (h *Hub) attachWiredNativeController(dev usb.BusDevice, iface usb.InterfaceDescriptor, kind controllers.Kind) {
	inEP, ok := iface.FindEndpoint(usb.TransferInterrupt, true)
	if !ok || inEP.MaxPacketSize == 0 || inEP.MaxPacketSize > usb.MaxHIDReportBytes {
		return
	}
	free, ok := h.findFreeWiredGamepadSlot(dev.Address, iface.Number)
	if !ok {
		return
	}
	inPipe, err := h.bus.OpenPipe(dev.Address, inEP)
	if err != nil {
		return
	}
	var outPipe usb.Pipe
	if outEP, ok := iface.FindEndpoint(usb.TransferInterrupt, false); ok && outEP.MaxPacketSize > 0 && outEP.MaxPacketSize <= usb.MaxHIDReportBytes {
		outPipe, err = h.bus.OpenPipe(dev.Address, outEP)
		if err != nil {
			_ = inPipe.Close()
			return
		}
	}
	initStep := uint8(0)
	if kind == controllers.KindXboxGIP && !xboxGIPNeedsSInit(dev.Descriptor.VendorID, dev.Descriptor.ProductID) {
		initStep = 1
	}
	name := friendlyGamepadName(kind, "", false)
	if iface.Class == 3 {
		name = h.usbHIDInputName(dev)
	} else if kind == controllers.KindXInput || kind == controllers.KindXboxGIP {
		name = usbXPadInputName(dev.Descriptor.VendorID, dev.Descriptor.ProductID)
	}
	h.wiredGamepads[free] = wiredGamepadSlot{
		active:    true,
		addr:      dev.Address,
		iface:     iface.Number,
		name:      name,
		vendorID:  dev.Descriptor.VendorID,
		productID: dev.Descriptor.ProductID,
		version:   dev.Descriptor.DeviceVersion,
		kind:      kind,
		pipe:      inPipe,
		outPipe:   outPipe,
		initStep:  initStep,
		state:     usb.GamepadState{Hat: -1},
	}
	h.assignPlayerPort(playerPortWired, uint8(free))
	h.padGeneration++
	h.emit(DeviceEvent{
		Kind:      EventGamepadAttached,
		Address:   dev.Address,
		Interface: iface.Number,
		Slot:      uint8(free),
		Name:      kind.String(),
	})
}

// usbXPadInputName reproduces the input device name Linux's xpad driver
// exposes for wired XInput and Xbox One/Series GIP controllers so profile
// GUIDs generated on TamaGo match Linux-saved profiles.
func usbXPadInputName(vendorID, productID uint16) string {
	switch uint32(vendorID)<<16 | uint32(productID) {
	case 0x045e028e:
		return "Microsoft X-Box 360 pad"
	case 0x045e028f:
		return "Microsoft X-Box 360 pad v2"
	case 0x045e0291:
		return "Xbox 360 Wireless Receiver (XBOX)"
	case 0x045e02a9:
		return "Xbox 360 Wireless Receiver (Unofficial)"
	case 0x045e02d1:
		return "Microsoft X-Box One pad"
	case 0x045e02dd:
		return "Microsoft X-Box One pad (Firmware 2015)"
	case 0x045e02e3:
		return "Microsoft X-Box One Elite pad"
	case 0x045e02ea:
		return "Microsoft X-Box One S pad"
	case 0x045e0719:
		return "Xbox 360 Wireless Receiver"
	case 0x045e0b00:
		return "Microsoft X-Box One Elite 2 pad"
	case 0x045e0b0a:
		return "Microsoft X-Box Adaptive Controller"
	case 0x045e0b12:
		return "Microsoft Xbox Series S|X Controller"
	case 0x046dc21d:
		return "Logitech Gamepad F310"
	case 0x046dc21e:
		return "Logitech Gamepad F510"
	case 0x046dc21f:
		return "Logitech Gamepad F710"
	case 0x2dc82000:
		return "8BitDo Pro 2 Wired Controller fox Xbox"
	case 0x2dc8200a:
		return "8BitDo M30 Wired Controller for Xbox"
	case 0x2dc8200f:
		return "8BitDo Ultimate 3-mode Controller for Xbox"
	case 0x2dc8201e:
		return "8BitDo Pro 2 Wired Controller for Xbox"
	case 0x2dc83106:
		return "8BitDo Ultimate Wireless / Pro 2 Wired Controller"
	case 0x2dc83109:
		return "8BitDo Ultimate Wireless Bluetooth"
	case 0x2dc8310a:
		return "8BitDo Ultimate 2C Wireless Controller"
	case 0x2dc8310b:
		return "8BitDo Ultimate 2 Wireless Controller"
	case 0x2dc86001:
		return "8BitDo SN30 Pro"
	default:
		return "Generic X-Box pad"
	}
}

// usbHIDInputName reproduces the name Linux usbhid exposes for an input device:
// manufacturer and product strings joined by one space, or its VID:PID fallback.
func (h *Hub) usbHIDInputName(dev usb.BusDevice) string {
	var descriptor [18]byte
	n, err := h.bus.ControlTransfer(dev.Address, 0x80, 0x06, 0x0100, 0, descriptor[:])
	if err != nil || n != len(descriptor) || descriptor[0] != byte(len(descriptor)) || descriptor[1] != 1 {
		return fmt.Sprintf("HID %04x:%04x", dev.Descriptor.VendorID, dev.Descriptor.ProductID)
	}
	manufacturerIndex, productIndex := descriptor[14], descriptor[15]
	if manufacturerIndex == 0 && productIndex == 0 {
		return fmt.Sprintf("HID %04x:%04x", dev.Descriptor.VendorID, dev.Descriptor.ProductID)
	}
	var language [4]byte
	n, err = h.bus.ControlTransfer(dev.Address, 0x80, 0x06, 0x0300, 0, language[:])
	if err != nil || n < len(language) || language[1] != 3 || language[0] < byte(len(language)) {
		return fmt.Sprintf("HID %04x:%04x", dev.Descriptor.VendorID, dev.Descriptor.ProductID)
	}
	languageID := binary.LittleEndian.Uint16(language[2:4])
	manufacturer := h.usbString(dev.Address, manufacturerIndex, languageID)
	product := h.usbString(dev.Address, productIndex, languageID)
	if manufacturer != "" {
		if product != "" {
			return manufacturer + " " + product
		}
		return manufacturer
	}
	if product != "" {
		return product
	}
	return fmt.Sprintf("HID %04x:%04x", dev.Descriptor.VendorID, dev.Descriptor.ProductID)
}

func (h *Hub) usbString(addr, stringIndex uint8, languageID uint16) string {
	if stringIndex == 0 {
		return ""
	}
	var product [255]byte
	n, err := h.bus.ControlTransfer(addr, 0x80, 0x06, 0x0300|uint16(stringIndex), languageID, product[:])
	if err != nil || n < 2 || product[1] != 3 {
		return ""
	}
	length := int(product[0])
	if length < 2 || length > n || length%2 != 0 {
		return ""
	}
	units := make([]uint16, (length-2)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(product[2+i*2 : 4+i*2])
	}
	return string(utf16.Decode(units))
}

func (h *Hub) writeSlotPacket(slotIdx int, pkt []byte) bool {
	slot := &h.wiredGamepads[slotIdx]
	if !slot.active || slot.outPipe == nil || slot.outPending || len(pkt) == 0 {
		return false
	}
	err := slot.outPipe.Write(pkt)
	if err == nil {
		slot.outPending = true
		return true
	}
	if errors.Is(err, usbcontrol.ErrTransition) || errors.Is(err, usbcontrol.ErrBusy) {
		return false
	}
	h.closeWiredGamepad(slotIdx)
	return false
}

func (h *Hub) enqueueGIPAck(slot *wiredGamepadSlot, seq, cmdID, payloadLen uint8) {
	if int(slot.gipAckCount) == len(slot.gipAckQueue) {
		// Service stops polling when full, so this is only a defensive guard.
		// Never overwrite an older request whose ACK is still required.
		return
	}
	tail := (slot.gipAckHead + slot.gipAckCount) % uint8(len(slot.gipAckQueue))
	slot.gipAckQueue[tail] = gipAckRequest{seq: seq, cmdID: cmdID, payloadLen: payloadLen}
	slot.gipAckCount++
}

func (h *Hub) serviceWiredInit(slotIdx int) (paused bool) {
	slot := &h.wiredGamepads[slotIdx]
	if !slot.active || slot.outPipe == nil {
		return false
	}
	if slot.outPending {
		done, err := slot.outPipe.WriteDone()
		if err != nil {
			if errors.Is(err, usbcontrol.ErrTransition) || errors.Is(err, usbcontrol.ErrBusy) {
				return true
			}
			h.closeWiredGamepad(slotIdx)
			return false
		}
		if !done {
			return false
		}
		slot.outPending = false
	}
	if slot.gipAckCount > 0 {
		ack := slot.gipAckQueue[slot.gipAckHead]
		n := controllers.BuildXboxGIPAck(h.txBuf[:], ack.seq, ack.cmdID, ack.payloadLen)
		if h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
			slot.gipAckHead = (slot.gipAckHead + 1) % uint8(len(slot.gipAckQueue))
			slot.gipAckCount--
		}
		return false
	}
	now := h.cfg.Now()
	if !slot.initNextTry.IsZero() && now.Before(slot.initNextTry) {
		return false
	}
	switch slot.kind {
	case controllers.KindXInput:
		if slot.ledDirty && slot.ledAssigned {
			n := controllers.BuildXInputLED(h.txBuf[:], slot.ledPort)
			if h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
				slot.ledDirty = false
			}
		}
	case controllers.KindXboxGIP:
		switch slot.initStep {
		case 0:
			n := controllers.BuildXboxGIPSInit(h.txBuf[:], slot.txSeq)
			slot.txSeq++
			if h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
				slot.initStep = 1
			}
		case 1:
			n := controllers.BuildXboxGIPPowerOn(h.txBuf[:], slot.txSeq)
			slot.txSeq++
			if h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
				slot.initStep = 2
			}
		case 2:
			n := controllers.BuildXboxGIPLedOn(h.txBuf[:], slot.txSeq)
			slot.txSeq++
			if h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
				slot.initStep = 3
			}
		case 3:
			n := controllers.BuildXboxGIPAuthComplete(h.txBuf[:], slot.txSeq)
			slot.txSeq++
			if h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
				slot.initStep = 4
			}
		}
	case controllers.KindDualSense:
		if slot.ledDirty && slot.ledAssigned {
			n := controllers.BuildDualSenseUSBOutput(h.txBuf[:], slot.ledPort, slot.rumbleRight, slot.rumbleLeft)
			if h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
				slot.ledDirty = false
			}
		}
	case controllers.KindDualShock4:
		if slot.ledDirty && slot.ledAssigned {
			n := controllers.BuildDualShock4USBOutput(h.txBuf[:], slot.ledPort, slot.rumbleRight, slot.rumbleLeft)
			if h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
				slot.ledDirty = false
			}
		}
	case controllers.KindSwitchPro:
		switch slot.initStep {
		case 0:
			if slot.initAttempts >= 10 {
				slot.initStep = 1
				slot.initAttempts = 0
				slot.initNextTry = time.Time{}
				return false
			}
			n := controllers.BuildSwitchProUSBHandshake(h.txBuf[:])
			if h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
				slot.initAttempts++
				slot.initNextTry = now.Add(100 * time.Millisecond)
			}
		case 1:
			n := controllers.BuildSwitchProUSBEnableHID(h.txBuf[:])
			if h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
				slot.initStep = 2
				slot.initAttempts = 0
				slot.initNextTry = time.Time{}
			}
		case 2:
			if slot.switchCal.Valid || slot.initAttempts >= 5 {
				slot.initStep = 3
				slot.initAttempts = 0
				slot.initNextTry = time.Time{}
				return false
			}
			n := controllers.BuildSwitchProReadCalibration(h.txBuf[:], slot.txSeq&0x0f)
			slot.txSeq++
			if h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
				slot.initAttempts++
				slot.initNextTry = now.Add(100 * time.Millisecond)
			}
		case 3:
			n := controllers.BuildSwitchProSetFullMode(h.txBuf[:], slot.txSeq&0x0f)
			slot.txSeq++
			if h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
				slot.initStep = 4
			}
		default:
			if slot.ledDirty && slot.ledAssigned {
				n := controllers.BuildSwitchProSetPlayerLEDs(h.txBuf[:], slot.txSeq&0x0f, slot.ledPort)
				slot.txSeq++
				if h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
					slot.ledDirty = false
				}
			}
		}
	}
	return false
}

func (h *Hub) feedWiredGamepadPacket(slotIdx int, pkt []byte) {
	slot := &h.wiredGamepads[slotIdx]
	if slot.kind == controllers.KindGenericHID {
		state, changed, feedErr := slot.decoder.Feed(pkt)
		if feedErr == nil {
			slot.reported = true
			if changed {
				slot.state = state
				slot.ctrlState = usbGamepadStateToControllerState(state)
				slot.ctrlMask = usbGamepadToComputerMask(state)
				h.padGeneration++
				h.recomputeGamepad()
			}
		}
		return
	}

	prevCtrl := slot.ctrlState
	var updated bool
	switch slot.kind {
	case controllers.KindXInput:
		updated = controllers.ParseXInput(pkt, &slot.ctrlState)
	case controllers.KindXboxGIP:
		var ackSeq uint8
		var needsAck bool
		updated, ackSeq, needsAck = controllers.ParseXboxGIPInput(pkt, &slot.ctrlState)
		if updated && needsAck && slot.outPipe != nil {
			if slot.outPending || slot.gipAckCount > 0 {
				h.enqueueGIPAck(slot, ackSeq, pkt[0], pkt[3])
			} else {
				n := controllers.BuildXboxGIPAck(h.txBuf[:], ackSeq, pkt[0], pkt[3])
				if !h.writeSlotPacket(slotIdx, h.txBuf[:n]) {
					if !slot.active {
						return
					}
					h.enqueueGIPAck(slot, ackSeq, pkt[0], pkt[3])
				}
			}
		}
	case controllers.KindDualSense:
		updated = controllers.ParseDualSenseUSB(pkt, &slot.ctrlState)
	case controllers.KindDualShock4:
		updated = controllers.ParseDualShock4USB(pkt, &slot.ctrlState)
	case controllers.KindSwitchPro:
		if len(pkt) >= 2 && pkt[0] == 0x81 && pkt[1] == 0x02 {
			if slot.initStep == 0 {
				slot.initStep = 1
				slot.initAttempts = 0
				slot.initNextTry = time.Time{}
			}
			return
		}
		if controllers.ParseSwitchProSPICalibration(pkt, &slot.switchCal) && slot.initStep == 2 {
			slot.initStep = 3
			slot.initAttempts = 0
			slot.initNextTry = time.Time{}
		}
		updated = controllers.ParseSwitchProInput(pkt, &slot.switchCal, &slot.ctrlState)
	}
	if !updated {
		return
	}
	slot.reported = true
	nextState := slot.ctrlState.ToUSBGamepadState()
	nextMask := slot.ctrlState.ToComputerMask()
	nextHome := slot.ctrlState.Buttons&controllers.ButtonHome != 0
	if nextState != slot.state || nextMask != slot.ctrlMask || nextHome != slot.home {
		slot.state = nextState
		slot.ctrlMask = nextMask
		slot.home = nextHome
		h.padGeneration++
		h.recomputeGamepad()
	} else if slot.ctrlState.Buttons != prevCtrl.Buttons || slot.ctrlState.LeftX != prevCtrl.LeftX || slot.ctrlState.LeftY != prevCtrl.LeftY {
		h.padGeneration++
	}
}

// SetWiredRumble sends a rumble/LED packet to a wired native controller slot (0..3).
func (h *Hub) SetWiredRumble(slotIdx uint8, leftHeavy, rightLight uint8) error {
	if h == nil || slotIdx >= maxWiredGamepads {
		return errors.New("usbhub: invalid wired gamepad slot")
	}
	slot := &h.wiredGamepads[slotIdx]
	if !slot.active || slot.outPipe == nil {
		return errors.New("usbhub: wired controller output unavailable")
	}
	if slot.outPending {
		done, err := slot.outPipe.WriteDone()
		if err != nil {
			return err
		}
		if !done {
			return errors.New("usbhub: wired output pipe busy")
		}
		slot.outPending = false
	}
	if slot.gipAckCount > 0 {
		return errors.New("usbhub: wired output pipe busy")
	}
	var n int
	clearLEDDirty := false
	switch slot.kind {
	case controllers.KindXInput:
		n = controllers.BuildXInputRumble(h.txBuf[:], leftHeavy, rightLight)
	case controllers.KindXboxGIP:
		n = controllers.BuildXboxGIPRumble(h.txBuf[:], slot.txSeq, leftHeavy, rightLight)
		slot.txSeq++
	case controllers.KindDualSense:
		if slot.ledAssigned {
			n = controllers.BuildDualSenseUSBOutput(h.txBuf[:], slot.ledPort, rightLight, leftHeavy)
			clearLEDDirty = true
		} else {
			n = controllers.BuildDualSenseUSBRumble(h.txBuf[:], rightLight, leftHeavy)
		}
	case controllers.KindDualShock4:
		if slot.ledAssigned {
			n = controllers.BuildDualShock4USBOutput(h.txBuf[:], slot.ledPort, rightLight, leftHeavy)
			clearLEDDirty = true
		} else {
			n = controllers.BuildDualShock4USBRumble(h.txBuf[:], rightLight, leftHeavy)
		}
	default:
		return errors.New("usbhub: rumble unsupported on this wired device")
	}
	if n == 0 {
		return errors.New("usbhub: failed to build rumble packet")
	}
	if err := slot.outPipe.Write(h.txBuf[:n]); err != nil {
		return err
	}
	slot.rumbleLeft = leftHeavy
	slot.rumbleRight = rightLight
	if clearLEDDirty {
		slot.ledDirty = false
	}
	slot.outPending = true
	return nil
}

func (h *Hub) closeWiredKeyboard(idx int) {
	slot := &h.wiredKeyboards[idx]
	if !slot.active {
		return
	}
	addr, iface := slot.addr, slot.iface
	if slot.pipe != nil {
		_ = slot.pipe.Close()
	}
	*slot = wiredKeyboardSlot{}
	h.recomputeKeyboard()
	h.emit(DeviceEvent{
		Kind:      EventKeyboardDetached,
		Address:   addr,
		Interface: iface,
		Slot:      uint8(idx),
	})
}

func (h *Hub) attachWiredMouse(addr uint8, iface usb.InterfaceDescriptor) {
	ep, ok := iface.FindEndpoint(usb.TransferInterrupt, true)
	if !ok || ep.MaxPacketSize < 3 || ep.MaxPacketSize > usb.MaxHIDReportBytes {
		return
	}
	free := -1
	for i := range h.wiredMice {
		slot := &h.wiredMice[i]
		if slot.active {
			if slot.addr == addr && slot.iface == iface.Number {
				return
			}
		} else if free < 0 {
			free = i
		}
	}
	if free < 0 {
		return
	}
	if _, err := h.bus.ControlTransfer(addr, 0x21, 0x0b, 0, uint16(iface.Number), nil); err != nil {
		return
	}
	// Request 100 ms HID idle snapshots where supported so idle mice confirm
	// initial button state cleanly without blocking the neutral fence.
	_, _ = h.bus.ControlTransfer(addr, 0x21, 0x0a, 25<<8, uint16(iface.Number), nil)
	pipe, err := h.bus.OpenPipe(addr, ep)
	if err != nil {
		return
	}
	h.wiredMice[free] = wiredMouseSlot{
		active: true,
		addr:   addr,
		iface:  iface.Number,
		pipe:   pipe,
	}
	h.emit(DeviceEvent{
		Kind:      EventMouseAttached,
		Address:   addr,
		Interface: iface.Number,
		Slot:      uint8(free),
	})
}

func (h *Hub) closeWiredMouse(idx int) {
	slot := &h.wiredMice[idx]
	if !slot.active {
		return
	}
	addr, iface := slot.addr, slot.iface
	if slot.pipe != nil {
		_ = slot.pipe.Close()
	}
	*slot = wiredMouseSlot{}
	merged := h.computeMouseButtons()
	if merged != h.mergedMouseButtons {
		h.mergedMouseButtons = merged
		h.enqueueMouse(MouseUpdate{Buttons: merged})
	}
	h.emit(DeviceEvent{
		Kind:      EventMouseDetached,
		Address:   addr,
		Interface: iface,
		Slot:      uint8(idx),
	})
}

func (h *Hub) computeMouseButtons() uint8 {
	var merged uint8
	for i := range h.wiredMice {
		if h.wiredMice[i].active {
			merged |= h.wiredMice[i].buttons
		}
	}
	for i := range h.btMouseButtons {
		merged |= h.btMouseButtons[i]
	}
	return merged & 0x03
}

func (h *Hub) closeWiredGamepad(idx int) {
	slot := &h.wiredGamepads[idx]
	if !slot.active {
		return
	}
	addr, iface, kind := slot.addr, slot.iface, slot.kind
	if slot.pipe != nil {
		_ = slot.pipe.Close()
	}
	if slot.outPipe != nil {
		_ = slot.outPipe.Close()
	}
	*slot = wiredGamepadSlot{state: usb.GamepadState{Hat: -1}}
	h.releasePlayerPort(playerPortWired, uint8(idx))
	h.padGeneration++
	h.recomputeGamepad()
	h.emit(DeviceEvent{
		Kind:      EventGamepadDetached,
		Address:   addr,
		Interface: iface,
		Slot:      uint8(idx),
		Name:      kind.String(),
	})
}
