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
	hidpGetReportFeature byte = 0x43 // GET_REPORT (0x40) | Feature (0x03)
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
}

// Session runs Classic Bluetooth HIDP (PSM 0x0011 Control + PSM 0x0013
// Interrupt) over an l2cap.Manager for up to 4 concurrent Sony DualSense,
// DualShock 4, Nintendo Switch Pro, and BT HID Keyboard devices with zero
// steady-state heap allocations.
type Session struct {
	tr  *hci.Transport
	mgr *l2cap.Manager
	now func() time.Time

	slots [MaxPlayerSlots]PlayerSlot
	txBuf [1 + controllers.DualSenseBTOutputLen]byte

	OnGamepadState   func(slot uint8, kind controllers.Kind, state controllers.State)
	OnKeyboardReport func(report []byte)
	OnSlotChanged    func(slot uint8, connected bool, kind controllers.Kind, name string)
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
		if s.slots[i].Connected && s.slots[i].initStep != initStepDone {
			s.maybeInitController(&s.slots[i])
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

// SetGameplayActive disables inquiry/page scan during active gameplay to avoid
// 10–30 ms RF scan window spikes, and restores Page Scan in menu/idle.
func (s *Session) SetGameplayActive(active bool) error {
	if active && s.mgr.PairingActive() {
		_ = s.mgr.SetPairingMode(false, 0)
	}
	return s.tr.SetGameplayRadioMode(active)
}

// Slot returns a snapshot of player slot index (0..3).
func (s *Session) Slot(index uint8) (PlayerSlot, bool) {
	if index >= MaxPlayerSlots || !s.slots[index].Connected {
		return PlayerSlot{}, false
	}
	return s.slots[index], true
}

// ConnectedCount returns the number of currently connected BT HIDP devices.
func (s *Session) ConnectedCount() int {
	n := 0
	for i := range s.slots {
		if s.slots[i].Connected {
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
	s.maybeInitController(slot)
	if !wasReady && slot.ctrlOpen && slot.intrOpen && s.OnSlotChanged != nil {
		s.OnSlotChanged(slot.Slot, true, slot.Kind, slot.Name)
	}
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
	s.maybeInitController(slot)
	if prevKind == controllers.KindUnknown && slot.Kind != controllers.KindUnknown && slot.ctrlOpen && slot.intrOpen && s.OnSlotChanged != nil {
		s.OnSlotChanged(slot.Slot, true, slot.Kind, slot.Name)
	}
}

func (s *Session) onDisconnect(handle uint16, _ l2cap.BDAddr) {
	slot := s.findSlotByHandle(handle)
	if slot == nil {
		return
	}
	idx, kind, name := slot.Slot, slot.Kind, slot.Name
	*slot = PlayerSlot{}
	// Clear any held buttons/axes before announcing slot removal so downstream
	// consumers driven by OnGamepadState / OnKeyboardReport never see stuck input.
	if kind != controllers.KindUnknown {
		if s.OnGamepadState != nil {
			s.OnGamepadState(idx, kind, controllers.State{})
		}
	} else if s.OnKeyboardReport != nil {
		s.OnKeyboardReport(neutralKeyboardReport[:])
	}
	if s.OnSlotChanged != nil {
		s.OnSlotChanged(idx, false, kind, name)
	}
}

func (s *Session) maybeInitController(slot *PlayerSlot) {
	if slot == nil {
		return
	}
	// Reconcile bond slot even if LinkKeyNotification completed after onChannelOpen.
	s.syncBondSlot(slot.Addr, slot.Slot)
	if slot.initStep == initStepDone || slot.Kind == controllers.KindUnknown {
		return
	}
	switch slot.Kind {
	case controllers.KindDualSense, controllers.KindDualShock4:
		if !slot.ctrlOpen || !slot.intrOpen {
			return
		}
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
		// (0x31 for DualSense, 0x11 CRC-protected for DualShock 4).
		if slot.initStep == 1 {
			var err error
			if slot.Kind == controllers.KindDualShock4 {
				err = s.sendDualShock4Output(slot, 0, 0)
			} else {
				err = s.sendDualSenseOutput(slot, 0, 0)
			}
			if err != nil {
				return
			}
			slot.initStep = initStepDone
		}

	case controllers.KindSwitchPro:
		if !slot.intrOpen {
			return
		}
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
			n = controllers.BuildSwitchProSetPlayerLEDs(s.txBuf[1:], slot.outputSeq, slot.Slot)
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
	}
}

func (s *Session) sendDualSenseOutput(slot *PlayerSlot, right, left uint8) error {
	s.txBuf[0] = hidpDataOutput
	n := controllers.BuildDualSenseBTOutput(s.txBuf[1:], slot.outputSeq, slot.Slot, right, left)
	if n == 0 {
		return errors.New("bt/hidp: DualSense output build failed")
	}
	if err := s.mgr.SendChannel(slot.Handle, l2cap.PSMHIDInterrupt, s.txBuf[:1+n]); err != nil {
		return err
	}
	slot.outputSeq = (slot.outputSeq + 1) & 0x0f
	return nil
}

func (s *Session) sendDualShock4Output(slot *PlayerSlot, right, left uint8) error {
	s.txBuf[0] = hidpDataOutput
	n := controllers.BuildDualShock4BTOutput(s.txBuf[1:], slot.Slot, right, left)
	if n == 0 {
		return errors.New("bt/hidp: DualShock 4 output build failed")
	}
	return s.mgr.SendChannel(slot.Handle, l2cap.PSMHIDInterrupt, s.txBuf[:1+n])
}

// SetRumble sends a rumble/LED update to an active DualSense or DualShock 4 slot (0..3).
func (s *Session) SetRumble(slotIdx uint8, right, left uint8) error {
	if slotIdx >= MaxPlayerSlots || !s.slots[slotIdx].Connected {
		return fmt.Errorf("bt/hidp: slot %d not connected", slotIdx)
	}
	slot := &s.slots[slotIdx]
	if !slot.intrOpen {
		return nil
	}
	switch slot.Kind {
	case controllers.KindDualSense:
		return s.sendDualSenseOutput(slot, right, left)
	case controllers.KindDualShock4:
		return s.sendDualShock4Output(slot, right, left)
	default:
		return nil
	}
}

func (s *Session) onHIDControl(_ uint16, _ []byte) {
	// Feature report replies (e.g. DualSense 0x05 calibration) complete the
	// unlock transition on the controller side; no hot-path parsing needed.
}

func (s *Session) onHIDInterrupt(handle uint16, payload []byte) {
	if len(payload) < 2 || payload[0] != hidpDataInput {
		return
	}
	slot := s.findSlotByHandle(handle)
	if slot == nil {
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
		// Classic BT HID boot keyboard fallback: 8-byte payload (or 9 bytes with report ID 0x01).
		if s.OnKeyboardReport != nil {
			if len(report) == 8 {
				s.OnKeyboardReport(report)
			} else if len(report) == 9 && report[0] == 0x01 {
				s.OnKeyboardReport(report[1:9])
			}
		}
	}
}

func (s *Session) handleSwitchProSubcmdReply(slot *PlayerSlot, report []byte) {
	controllers.ParseSwitchProSPICalibration(report, &slot.cal)
	if len(report) < 15 || slot.initStep == initStepDone || !slot.initWaitingAck {
		return
	}
	ack := report[13]
	subcmd := report[14]
	if ack&0x80 == 0 {
		// Controller explicitly NACKed; remain waiting so maybeInitController
		// retries once switchProSubcmdRetryInterval elapses.
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
