// SPDX-License-Identifier: GPL-3.0-or-later
package hidp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/bt/hci"
	"github.com/DeanoC/fes/platform/embedded/tamago/bt/l2cap"
	"github.com/DeanoC/fes/platform/embedded/tamago/input/controllers"
)

const (
	MaxPlayerSlots = 4

	// HIDP transaction header bytes (Bluetooth HID Profile 1.1).
	hidpHandshake        byte = 0x00 // HANDSHAKE (0x00) | HANDSHAKE_SUCCESSFUL (0x00)
	hidpGetReportFeature byte = 0x43 // GET_REPORT (0x40) | Feature (0x03)
	hidpSetProtocolBoot  byte = 0x70 // SET_PROTOCOL (0x70) | Boot Protocol (0x00)
	hidpDataInput        byte = 0xa1 // DATA (0xA0) | Input (0x01)
	hidpDataOutput       byte = 0xa2 // DATA (0xA0) | Output (0x02)

	initStepDone uint8 = 0xff

	switchProSubcmdRetryInterval = 250 * time.Millisecond
	switchProMaxStepAttempts     = 5
)

var neutralKeyboardReport [8]byte

// PlayerSlot tracks one active Classic Bluetooth controller connection.
type PlayerSlot struct {
	Connected bool
	Slot      uint8
	Handle    uint16
	Addr      l2cap.BDAddr
	Name      string
	Kind      controllers.Kind
	State     controllers.State

	ctrlOpen       bool
	intrOpen       bool
	initStep       uint8
	initWaitingAck bool
	initNextTry    time.Time
	initAttempts   uint8
	cal            controllers.SwitchStickCal
	outputSeq      uint8
	ledManaged     bool
	ledAssigned    bool
	ledPort        uint8
	ledSentPort    uint8
	ledDirty       bool
	ledWaiting     bool
	ledNextTry     time.Time
	ledAttempts    uint8
	rumbleRight    uint8
	rumbleLeft     uint8
	MouseButtons   uint8
}

func (p *PlayerSlot) ready() bool {
	return p.Connected && p.ctrlOpen && p.intrOpen
}

func (p *PlayerSlot) effectiveLEDPort() (uint8, bool) {
	if !p.ledManaged {
		return p.Slot, true
	}
	if p.ledAssigned {
		return p.ledPort, true
	}
	return 0, false
}

func (p *PlayerSlot) clearLEDState() {
	p.ledManaged = false
	p.ledAssigned = false
	p.ledPort = 0
	p.ledSentPort = 0
	p.ledDirty = false
	p.ledWaiting = false
	p.ledNextTry = time.Time{}
	p.ledAttempts = 0
	p.rumbleRight = 0
	p.rumbleLeft = 0
}

// Session runs Classic Bluetooth HIDP (PSM 0x0011 Control + PSM 0x0013
// Interrupt) over an l2cap.Manager for up to 4 concurrent Sony DualSense,
// DualShock 4, Nintendo Switch Pro, BT HID Keyboard, and BT HID Boot Mouse
// devices with zero steady-state heap allocations.
type Session struct {
	tr  *hci.Transport
	mgr *l2cap.Manager
	now func() time.Time

	slots [MaxPlayerSlots]PlayerSlot
	txBuf [1 + controllers.DualSenseBTOutputLen]byte

	OnGamepadState       func(slot uint8, kind controllers.Kind, state controllers.State)
	OnKeyboardReport     func(report []byte)
	OnSlotKeyboardReport func(slot uint8, report []byte)
	OnSlotMouseReport    func(slot uint8, dx, dy int8, buttons uint8)
	OnSlotChanged        func(slot uint8, connected bool, kind controllers.Kind, name string)
}

// NewSession attaches Classic BT HIDP handling to an initialized hci.Transport.
func NewSession(tr *hci.Transport, bonds *l2cap.BondStore, now func() time.Time) (*Session, error) {
	if tr == nil {
		return nil, errors.New("bt/hidp: nil HCI transport")
	}
	if now == nil {
		now = time.Now
	}
	mgr, err := l2cap.NewManager(tr, bonds, now)
	if err != nil {
		return nil, err
	}
	s := &Session{tr: tr, mgr: mgr, now: now}
	mgr.OnChannelOpen = s.onChannelOpen
	mgr.OnChannelClose = s.onChannelClose
	mgr.OnDisconnect = s.onDisconnect
	mgr.OnNameResolved = s.onNameResolved
	mgr.OnHIDControl = s.onHIDControl
	mgr.OnHIDInterrupt = s.onHIDInterrupt
	return s, nil
}

// Service pumps HCI + L2CAP + HIDP traffic with zero steady-state allocations.
func (s *Session) Service() error {
	if err := s.mgr.Service(); err != nil {
		return err
	}
	for i := range s.slots {
		if !s.slots[i].Connected {
			continue
		}
		if s.slots[i].initStep != initStepDone {
			s.maybeInitController(&s.slots[i])
		} else if s.slots[i].ledDirty {
			s.maybeSyncPlayerLED(&s.slots[i])
		}
	}
	return nil
}

// SetPairingMode opens or closes the bounded SSP pairing window.
func (s *Session) SetPairingMode(enabled bool, duration time.Duration) error {
	return s.mgr.SetPairingMode(enabled, duration)
}

// PairingActive reports whether the SSP pairing window is currently open.
func (s *Session) PairingActive() bool { return s.mgr.PairingActive() }

// PairingRemaining returns the remaining duration of an active SSP pairing window.
func (s *Session) PairingRemaining() time.Duration {
	if s == nil || s.mgr == nil {
		return 0
	}
	return s.mgr.PairingRemaining()
}

// DisconnectAddr disconnects an active or pending link to addr and closes its HID channels.
func (s *Session) DisconnectAddr(addr l2cap.BDAddr) (bool, error) {
	if s == nil || s.mgr == nil {
		return false, nil
	}
	return s.mgr.DisconnectAddr(addr)
}

// SetGameplayActive disables inquiry/page scan during active gameplay to avoid
// 10–30 ms RF scan window spikes, and restores Page Scan in menu/idle.
func (s *Session) SetGameplayActive(active bool) error {
	var cancelErr error
	if active && s.mgr.PairingActive() {
		cancelErr = s.mgr.SetPairingMode(false, 0)
	}
	if err := s.tr.SetGameplayRadioMode(active); err != nil {
		if cancelErr != nil {
			return cancelErr
		}
		return err
	}
	return cancelErr
}

// Slot returns a snapshot of player slot index (0..3).
func (s *Session) Slot(index uint8) (PlayerSlot, bool) {
	if index >= MaxPlayerSlots || !s.slots[index].ready() {
		return PlayerSlot{}, false
	}
	return s.slots[index], true
}

// ConnectedCount returns the number of currently connected BT HIDP devices.
func (s *Session) ConnectedCount() int {
	n := 0
	for i := range s.slots {
		if s.slots[i].ready() {
			n++
		}
	}
	return n
}

func (s *Session) syncBondSlot(addr l2cap.BDAddr, slotIdx uint8) {
	if s.mgr == nil {
		return
	}
	bonds := s.mgr.Bonds()
	if bonds == nil {
		return
	}
	if rec, ok := bonds.Lookup(addr); ok && rec.Slot != slotIdx {
		rec.Slot = slotIdx
		_ = bonds.Upsert(rec)
	}
}

func (s *Session) findOrAllocSlot(handle uint16, addr l2cap.BDAddr, name string, kind controllers.Kind) *PlayerSlot {
	for i := range s.slots {
		if s.slots[i].Connected && s.slots[i].Handle == handle {
			if name != "" {
				s.slots[i].Name = name
			}
			if kind != controllers.KindUnknown {
				s.slots[i].Kind = kind
			}
			s.syncBondSlot(s.slots[i].Addr, s.slots[i].Slot)
			return &s.slots[i]
		}
	}
	// Prefer persisted slot index when available and free.
	if rec, ok := s.mgr.Bonds().Lookup(addr); ok && rec.Slot < MaxPlayerSlots && !s.slots[rec.Slot].Connected {
		slot := &s.slots[rec.Slot]
		*slot = PlayerSlot{Connected: true, Slot: rec.Slot, Handle: handle, Addr: addr, Name: name, Kind: kind}
		return slot
	}
	for i := range s.slots {
		if !s.slots[i].Connected {
			slotIdx := uint8(i)
			s.slots[i] = PlayerSlot{Connected: true, Slot: slotIdx, Handle: handle, Addr: addr, Name: name, Kind: kind}
			s.syncBondSlot(addr, slotIdx)
			return &s.slots[i]
		}
	}
	return nil
}

func (s *Session) findSlotByHandle(handle uint16) *PlayerSlot {
	for i := range s.slots {
		if s.slots[i].Connected && s.slots[i].Handle == handle {
			return &s.slots[i]
		}
	}
	return nil
}

func (s *Session) onChannelOpen(handle uint16, addr l2cap.BDAddr, name string, kind controllers.Kind, psm uint16) {
	slot := s.findOrAllocSlot(handle, addr, name, kind)
	if slot == nil {
		return
	}
	wasReady := slot.ctrlOpen && slot.intrOpen
	switch psm {
	case l2cap.PSMHIDControl:
		slot.ctrlOpen = true
	case l2cap.PSMHIDInterrupt:
		slot.intrOpen = true
	}
	if !wasReady && slot.ctrlOpen && slot.intrOpen && s.OnSlotChanged != nil {
		s.OnSlotChanged(slot.Slot, true, slot.Kind, slot.Name)
	}
	s.maybeInitController(slot)
}

func (s *Session) onNameResolved(handle uint16, _ l2cap.BDAddr, name string, kind controllers.Kind) {
	slot := s.findSlotByHandle(handle)
	if slot == nil {
		return
	}
	prevKind := slot.Kind
	slot.Name = name
	if kind != controllers.KindUnknown {
		slot.Kind = kind
	}
	if prevKind == controllers.KindUnknown && slot.Kind != controllers.KindUnknown {
		slot.initStep = 0
		slot.initWaitingAck = false
		slot.initNextTry = time.Time{}
		slot.initAttempts = 0
	}
	if prevKind == controllers.KindUnknown && slot.Kind != controllers.KindUnknown && slot.ctrlOpen && slot.intrOpen && s.OnSlotChanged != nil {
		s.OnSlotChanged(slot.Slot, true, slot.Kind, slot.Name)
	}
	s.maybeInitController(slot)
}

func (s *Session) emitKeyboardReport(slot uint8, report []byte) {
	if s.OnSlotKeyboardReport != nil {
		s.OnSlotKeyboardReport(slot, report)
	}
	if s.OnKeyboardReport != nil {
		s.OnKeyboardReport(report)
	}
}

func (s *Session) emitMouseReport(slot uint8, dx, dy int8, buttons uint8) {
	if s.OnSlotMouseReport != nil {
		s.OnSlotMouseReport(slot, dx, dy, buttons)
	}
}

func (s *Session) neutralizeSlotInput(idx uint8, kind controllers.Kind, hadMouseButtons bool) {
	if kind != controllers.KindUnknown {
		if s.OnGamepadState != nil {
			s.OnGamepadState(idx, kind, controllers.State{})
		}
		return
	}
	s.emitKeyboardReport(idx, neutralKeyboardReport[:])
	if hadMouseButtons {
		s.emitMouseReport(idx, 0, 0, 0)
	}
}

func (s *Session) onChannelClose(handle uint16, _ l2cap.BDAddr, psm uint16) {
	slot := s.findSlotByHandle(handle)
	if slot == nil {
		return
	}
	wasReady := slot.ready()
	hadActiveState := !slot.State.Neutral()
	hadMouseButtons := slot.MouseButtons != 0
	idx, kind, name := slot.Slot, slot.Kind, slot.Name
	switch psm {
	case l2cap.PSMHIDControl:
		slot.ctrlOpen = false
	case l2cap.PSMHIDInterrupt:
		slot.intrOpen = false
	default:
		return
	}
	slot.State = controllers.State{}
	slot.MouseButtons = 0
	slot.initStep = 0
	slot.initWaitingAck = false
	slot.initNextTry = time.Time{}
	slot.initAttempts = 0
	slot.clearLEDState()
	if !slot.ctrlOpen && !slot.intrOpen {
		*slot = PlayerSlot{}
	}
	// Clear any held buttons/axes before announcing channel loss so downstream
	// consumers driven by OnGamepadState / OnKeyboardReport / OnSlotMouseReport
	// never see stuck input.
	if wasReady || hadActiveState || kind == controllers.KindUnknown {
		s.neutralizeSlotInput(idx, kind, hadMouseButtons)
	}
	if wasReady && s.OnSlotChanged != nil {
		s.OnSlotChanged(idx, false, kind, name)
	}
}

func (s *Session) onDisconnect(handle uint16, _ l2cap.BDAddr) {
	slot := s.findSlotByHandle(handle)
	if slot == nil {
		return
	}
	wasReady := slot.ready()
	hadActiveState := !slot.State.Neutral()
	hadMouseButtons := slot.MouseButtons != 0
	idx, kind, name := slot.Slot, slot.Kind, slot.Name
	*slot = PlayerSlot{}
	if wasReady || hadActiveState || kind == controllers.KindUnknown {
		s.neutralizeSlotInput(idx, kind, hadMouseButtons)
	}
	if wasReady && s.OnSlotChanged != nil {
		s.OnSlotChanged(idx, false, kind, name)
	}
}

func (s *Session) maybeInitController(slot *PlayerSlot) {
	if slot == nil {
		return
	}
	// Reconcile bond slot even if LinkKeyNotification completed after onChannelOpen.
	s.syncBondSlot(slot.Addr, slot.Slot)
	if !slot.ready() || slot.initStep == initStepDone {
		return
	}
	switch slot.Kind {
	case controllers.KindDualSense, controllers.KindDualShock4:
		if slot.initStep == 0 {
			// 1. Send HIDP GET_REPORT Feature 0x05 over PSM 0x0011 to unlock full
			// 78-byte high-rate Bluetooth input reports (0x31 / 0x11).
			getRep := [2]byte{hidpGetReportFeature, controllers.DualSenseUnlockFeatureReportID(slot.Kind)}
			if err := s.mgr.SendChannel(slot.Handle, l2cap.PSMHIDControl, getRep[:]); err != nil {
				return
			}
			slot.initStep = 1
		}
		// 2. Send initial Player 1..4 lightbar/LED output report on PSM 0x0013
		// (0x31 for DualSense, 0x11 CRC-protected for DualShock 4) when assigned.
		if slot.initStep == 1 {
			if _, ok := slot.effectiveLEDPort(); ok {
				var err error
				if slot.Kind == controllers.KindDualShock4 {
					err = s.sendDualShock4Output(slot, slot.rumbleRight, slot.rumbleLeft)
				} else {
					err = s.sendDualSenseOutput(slot, slot.rumbleRight, slot.rumbleLeft)
				}
				if err != nil {
					return
				}
			} else {
				slot.ledDirty = false
			}
			slot.initStep = initStepDone
		}

	case controllers.KindSwitchPro:
		if slot.initWaitingAck {
			if s.now().Before(slot.initNextTry) {
				return
			}
			if slot.initAttempts >= switchProMaxStepAttempts {
				switch slot.initStep {
				case 0:
					slot.initStep = 1
				case 1:
					slot.initStep = 2
				default:
					slot.ledDirty = false
					slot.initStep = initStepDone
				}
				slot.initWaitingAck = false
				slot.initAttempts = 0
				slot.initNextTry = time.Time{}
				if slot.initStep == initStepDone {
					return
				}
			} else {
				slot.initWaitingAck = false
			}
		}

		s.txBuf[0] = hidpDataOutput
		var n int
		switch slot.initStep {
		case 0:
			n = controllers.BuildSwitchProReadCalibration(s.txBuf[1:], slot.outputSeq)
		case 1:
			n = controllers.BuildSwitchProSetFullMode(s.txBuf[1:], slot.outputSeq)
		case 2:
			port, ok := slot.effectiveLEDPort()
			if !ok {
				slot.ledDirty = false
				slot.initStep = initStepDone
				slot.initWaitingAck = false
				slot.initAttempts = 0
				slot.initNextTry = time.Time{}
				return
			}
			n = controllers.BuildSwitchProSetPlayerLEDs(s.txBuf[1:], slot.outputSeq, port)
			slot.ledSentPort = port
		default:
			return
		}
		if err := s.mgr.SendChannel(slot.Handle, l2cap.PSMHIDInterrupt, s.txBuf[:1+n]); err != nil {
			return
		}
		slot.outputSeq = (slot.outputSeq + 1) & 0x0f
		slot.initWaitingAck = true
		slot.initAttempts++
		slot.initNextTry = s.now().Add(switchProSubcmdRetryInterval)

	case controllers.KindUnknown:
		// Boot keyboard fallback: select Boot Protocol (0x70) on Control channel
		// and wait for HANDSHAKE_SUCCESSFUL (0x00) before decoding boot reports.
		if slot.initWaitingAck {
			if s.now().Before(slot.initNextTry) {
				return
			}
			slot.initWaitingAck = false
		}
		if slot.initAttempts >= switchProMaxStepAttempts {
			return
		}
		pkt := [1]byte{hidpSetProtocolBoot}
		if err := s.mgr.SendChannel(slot.Handle, l2cap.PSMHIDControl, pkt[:]); err != nil {
			return
		}
		slot.initWaitingAck = true
		slot.initAttempts++
		slot.initNextTry = s.now().Add(switchProSubcmdRetryInterval)
	}
}

func (s *Session) sendDualSenseOutput(slot *PlayerSlot, right, left uint8) error {
	s.txBuf[0] = hidpDataOutput
	var n int
	clearLEDDirty := false
	if port, ok := slot.effectiveLEDPort(); ok {
		n = controllers.BuildDualSenseBTOutput(s.txBuf[1:], slot.outputSeq, port, right, left)
		clearLEDDirty = true
	} else {
		n = controllers.BuildDualSenseBTRumble(s.txBuf[1:], slot.outputSeq, right, left)
	}
	if n == 0 {
		return errors.New("bt/hidp: DualSense output build failed")
	}
	if err := s.mgr.SendChannel(slot.Handle, l2cap.PSMHIDInterrupt, s.txBuf[:1+n]); err != nil {
		return err
	}
	slot.rumbleRight = right
	slot.rumbleLeft = left
	if clearLEDDirty {
		slot.ledDirty = false
	}
	slot.outputSeq = (slot.outputSeq + 1) & 0x0f
	return nil
}

func (s *Session) sendDualShock4Output(slot *PlayerSlot, right, left uint8) error {
	s.txBuf[0] = hidpDataOutput
	var n int
	clearLEDDirty := false
	if port, ok := slot.effectiveLEDPort(); ok {
		n = controllers.BuildDualShock4BTOutput(s.txBuf[1:], port, right, left)
		clearLEDDirty = true
	} else {
		n = controllers.BuildDualShock4BTRumble(s.txBuf[1:], right, left)
	}
	if n == 0 {
		return errors.New("bt/hidp: DualShock 4 output build failed")
	}
	if err := s.mgr.SendChannel(slot.Handle, l2cap.PSMHIDInterrupt, s.txBuf[:1+n]); err != nil {
		return err
	}
	slot.rumbleRight = right
	slot.rumbleLeft = left
	if clearLEDDirty {
		slot.ledDirty = false
	}
	return nil
}

func (s *Session) maybeSyncPlayerLED(slot *PlayerSlot) {
	if !slot.ready() || !slot.ledDirty {
		return
	}
	port, ok := slot.effectiveLEDPort()
	if !ok {
		slot.ledDirty = false
		slot.ledWaiting = false
		slot.ledAttempts = 0
		slot.ledNextTry = time.Time{}
		return
	}
	switch slot.Kind {
	case controllers.KindDualSense:
		_ = s.sendDualSenseOutput(slot, slot.rumbleRight, slot.rumbleLeft)
	case controllers.KindDualShock4:
		_ = s.sendDualShock4Output(slot, slot.rumbleRight, slot.rumbleLeft)
	case controllers.KindSwitchPro:
		if slot.ledWaiting {
			if s.now().Before(slot.ledNextTry) {
				return
			}
			if slot.ledAttempts >= switchProMaxStepAttempts {
				slot.ledDirty = false
				slot.ledWaiting = false
				slot.ledAttempts = 0
				slot.ledNextTry = time.Time{}
				return
			}
			slot.ledWaiting = false
		}
		s.txBuf[0] = hidpDataOutput
		n := controllers.BuildSwitchProSetPlayerLEDs(s.txBuf[1:], slot.outputSeq, port)
		if err := s.mgr.SendChannel(slot.Handle, l2cap.PSMHIDInterrupt, s.txBuf[:1+n]); err != nil {
			return
		}
		slot.outputSeq = (slot.outputSeq + 1) & 0x0f
		slot.ledSentPort = port
		slot.ledWaiting = true
		slot.ledAttempts++
		slot.ledNextTry = s.now().Add(switchProSubcmdRetryInterval)
	default:
		slot.ledDirty = false
	}
}

// SetPlayerPort configures or updates the unified P1..P4 player-port LED
// assignment for an active HIDP slot (0..3). Calling SetPlayerPort marks the
// slot's LED state as Hub-managed so overflow (unassigned) controllers never
// light a stale P1 indicator until promoted.
func (s *Session) SetPlayerPort(slotIdx uint8, playerPort uint8, assigned bool) error {
	if slotIdx >= MaxPlayerSlots || !s.slots[slotIdx].ready() {
		return fmt.Errorf("bt/hidp: slot %d not connected", slotIdx)
	}
	slot := &s.slots[slotIdx]
	port := playerPort & 3
	if !assigned {
		port = 0
	}
	changed := !slot.ledManaged || slot.ledAssigned != assigned || (assigned && slot.ledPort != port)
	slot.ledManaged = true
	slot.ledAssigned = assigned
	slot.ledPort = port
	if !changed {
		return nil
	}
	slot.ledWaiting = false
	slot.ledAttempts = 0
	slot.ledNextTry = time.Time{}
	if !assigned {
		slot.ledDirty = false
		return nil
	}
	switch slot.Kind {
	case controllers.KindDualSense, controllers.KindDualShock4, controllers.KindSwitchPro:
		slot.ledDirty = true
		if slot.initStep == initStepDone {
			s.maybeSyncPlayerLED(slot)
		}
	default:
		slot.ledDirty = false
	}
	return nil
}

// SetRumble sends a rumble/LED update to an active DualSense or DualShock 4 slot (0..3).
func (s *Session) SetRumble(slotIdx uint8, right, left uint8) error {
	if slotIdx >= MaxPlayerSlots || !s.slots[slotIdx].ready() {
		return fmt.Errorf("bt/hidp: slot %d not connected", slotIdx)
	}
	slot := &s.slots[slotIdx]
	switch slot.Kind {
	case controllers.KindDualSense:
		return s.sendDualSenseOutput(slot, right, left)
	case controllers.KindDualShock4:
		return s.sendDualShock4Output(slot, right, left)
	default:
		return nil
	}
}

func (s *Session) onHIDControl(handle uint16, payload []byte) {
	// Handle SET_PROTOCOL(BOOT) handshake for boot keyboard fallback; gamepads use
	// feature reports whose responses don't gate input decoding here.
	slot := s.findSlotByHandle(handle)
	if slot != nil && slot.ready() && slot.Kind == controllers.KindUnknown && slot.initWaitingAck && len(payload) >= 1 && (payload[0]&0xf0) == hidpHandshake {
		slot.initWaitingAck = false
		slot.initNextTry = time.Time{}
		if payload[0] == hidpHandshake {
			slot.initStep = initStepDone
			slot.initAttempts = 0
		} else {
			slot.initAttempts = switchProMaxStepAttempts
		}
	}
}

func (s *Session) onHIDInterrupt(handle uint16, payload []byte) {
	if len(payload) < 2 || payload[0] != hidpDataInput {
		return
	}
	slot := s.findSlotByHandle(handle)
	if slot == nil || !slot.ready() {
		return
	}
	report := payload[1:]
	switch slot.Kind {
	case controllers.KindDualSense:
		if controllers.ParseDualSenseBT(report, &slot.State) {
			slot.State.TimestampNS = s.now().UnixNano()
			if s.OnGamepadState != nil {
				s.OnGamepadState(slot.Slot, slot.Kind, slot.State)
			}
		}

	case controllers.KindDualShock4:
		if controllers.ParseDualShock4BT(report, &slot.State) {
			slot.State.TimestampNS = s.now().UnixNano()
			if s.OnGamepadState != nil {
				s.OnGamepadState(slot.Slot, slot.Kind, slot.State)
			}
		}

	case controllers.KindSwitchPro:
		if report[0] == controllers.SwitchProReportSubcmdReply {
			s.handleSwitchProSubcmdReply(slot, report)
		}
		if controllers.ParseSwitchProInput(report, &slot.cal, &slot.State) {
			slot.State.TimestampNS = s.now().UnixNano()
			if s.OnGamepadState != nil {
				s.OnGamepadState(slot.Slot, slot.Kind, slot.State)
			}
		}

	default:
		// Classic BT HID boot keyboard/mouse fallback: only decode reports
		// after SET_PROTOCOL(BOOT) handshake succeeded. Raw 8-byte reports are
		// checked before Report ID 0x02 so Left Shift (modifier 0x02) on a raw
		// keyboard report is never misread as a boot mouse.
		if slot.initStep != initStepDone {
			return
		}
		switch {
		case len(report) == 8:
			s.emitKeyboardReport(slot.Slot, report)
		case len(report) == 9 && report[0] == 0x01:
			s.emitKeyboardReport(slot.Slot, report[1:9])
		case len(report) >= 4 && len(report) <= 7 && report[0] == 0x02:
			buttons := report[1] & 0x03
			slot.MouseButtons = buttons
			s.emitMouseReport(slot.Slot, int8(report[2]), int8(report[3]), buttons)
		case len(report) == 3:
			buttons := report[0] & 0x03
			slot.MouseButtons = buttons
			s.emitMouseReport(slot.Slot, int8(report[1]), int8(report[2]), buttons)
		}
	}
}

func (s *Session) handleSwitchProSubcmdReply(slot *PlayerSlot, report []byte) {
	controllers.ParseSwitchProSPICalibration(report, &slot.cal)
	if len(report) < 15 {
		return
	}
	ack := report[13]
	subcmd := report[14]
	if slot.initStep == initStepDone {
		if slot.ledDirty && slot.ledWaiting && ack&0x80 != 0 && subcmd == controllers.SwitchProSubcmdSetPlayerLD {
			slot.ledWaiting = false
			slot.ledAttempts = 0
			slot.ledNextTry = time.Time{}
			if !slot.ledAssigned || slot.ledPort == slot.ledSentPort {
				slot.ledDirty = false
			} else {
				s.maybeSyncPlayerLED(slot)
			}
		}
		return
	}
	if !slot.initWaitingAck || ack&0x80 == 0 {
		// Controller explicitly NACKed or no subcommand ACK is pending; remain
		// waiting so maybeInitController retries once the interval elapses.
		return
	}
	switch slot.initStep {
	case 0:
		if subcmd != controllers.SwitchProSubcmdSPIRead || len(report) < 38 {
			return
		}
		addr := binary.LittleEndian.Uint32(report[15:19])
		if addr != controllers.SwitchProFactoryStickCalAddr || report[19] < controllers.SwitchProFactoryStickCalLen {
			return
		}
		slot.initStep = 1
	case 1:
		if subcmd != controllers.SwitchProSubcmdSetMode {
			return
		}
		slot.initStep = 2
	case 2:
		if subcmd != controllers.SwitchProSubcmdSetPlayerLD {
			return
		}
		if slot.ledManaged && slot.ledAssigned && slot.ledPort != slot.ledSentPort {
			slot.ledDirty = true
			slot.initWaitingAck = false
			slot.initAttempts = 0
			slot.initNextTry = time.Time{}
			s.maybeInitController(slot)
			return
		}
		slot.ledDirty = false
		slot.initStep = initStepDone
	default:
		return
	}
	slot.initWaitingAck = false
	slot.initAttempts = 0
	slot.initNextTry = time.Time{}
	if slot.initStep != initStepDone {
		s.maybeInitController(slot)
	}
}
