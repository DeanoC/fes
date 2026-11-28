// SPDX-License-Identifier: GPL-3.0-or-later

package usbhub

import (
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/DeanoC/fes/platform/embedded/tamago/input/controllers"
	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

// KeyboardNeutral reports whether the combined keyboard is known to be
// released: the bus has been enumerated, every wired keyboard has delivered a
// report (SET_IDLE makes idle keyboards repeat theirs) and the merged report is
// all-up. A keyboard that has not reported yet may be held, so it keeps this
// false, matching the single-keyboard rule that a missing report never arms.
func (h *Hub) KeyboardNeutral() bool {
	if h == nil {
		return true
	}
	if !h.initialized || h.mergedKeyboard != ([8]byte{}) {
		return false
	}
	for i := range h.wiredKeyboards {
		if h.wiredKeyboards[i].active && !h.wiredKeyboards[i].reported {
			return false
		}
	}
	return true
}

// GamepadNeutral reports whether the combined gamepad is known to be neutral:
// the bus has been enumerated, every wired gamepad has delivered a report, and
// the merged buttons, sticks, hats and Home are released.
func (h *Hub) GamepadNeutral() bool {
	if h == nil {
		return true
	}
	if !h.initialized || !input.GamepadNeutral(h.mergedGamepad) || h.mergedHome || h.mergedPorts != ([MaxPlayerPorts]uint8{}) {
		return false
	}
	for i := range h.wiredGamepads {
		if h.wiredGamepads[i].active && !h.wiredGamepads[i].reported {
			return false
		}
	}
	return true
}

// NextKeyboardReport pops the next merged 8-byte boot keyboard snapshot change, if any.
func (h *Hub) NextKeyboardReport() ([8]byte, bool) {
	if h == nil || h.kbCount == 0 {
		return [8]byte{}, false
	}
	report := h.kbQueue[h.kbHead]
	h.kbHead = (h.kbHead + 1) % maxQueuedReports
	h.kbCount--
	return report, true
}

// MouseNeutral reports whether the bus has been enumerated, every active wired
// mouse has delivered an initial report, each Bluetooth mouse or unresolved
// HID slot has either reported or been identified as another input type, and
// all wired + Bluetooth Left/Right mouse buttons are released.
func (h *Hub) MouseNeutral() bool {
	if h == nil {
		return true
	}
	if !h.initialized || h.mergedMouseButtons != 0 {
		return false
	}
	for i := range h.wiredMice {
		if h.wiredMice[i].active && !h.wiredMice[i].reported {
			return false
		}
	}
	for i := range h.btMouseActive {
		if h.btMouseActive[i] && !h.btMouseReported[i] {
			return false
		}
	}
	return true
}

// NextMouseUpdate pops the next queued relative boot-mouse update, if any.
func (h *Hub) NextMouseUpdate() (MouseUpdate, bool) {
	if h == nil || h.mouseCount == 0 {
		return MouseUpdate{}, false
	}
	u := h.mouseQueue[h.mouseHead]
	h.mouseHead = (h.mouseHead + 1) % maxQueuedReports
	h.mouseCount--
	return u, true
}

// NextGamepadUpdate pops the next merged gamepad snapshot change, if any.
func (h *Hub) NextGamepadUpdate() (GamepadUpdate, bool) {
	if h == nil || h.padCount == 0 {
		return GamepadUpdate{State: usb.GamepadState{Hat: -1}}, false
	}
	u := h.padQueue[h.padHead]
	h.padHead = (h.padHead + 1) % maxQueuedReports
	h.padCount--
	return u, true
}

func (h *Hub) enqueueKeyboard(report [8]byte) {
	if h.kbCount == maxQueuedReports {
		h.kbHead = (h.kbHead + 1) % maxQueuedReports
		h.kbCount--
	}
	tail := (h.kbHead + h.kbCount) % maxQueuedReports
	h.kbQueue[tail] = report
	h.kbCount++
}

func (h *Hub) enqueueGamepad(u GamepadUpdate) {
	if h.padCount == maxQueuedReports {
		h.padHead = (h.padHead + 1) % maxQueuedReports
		h.padCount--
	}
	tail := (h.padHead + h.padCount) % maxQueuedReports
	h.padQueue[tail] = u
	h.padCount++
}

func (h *Hub) enqueueMouse(u MouseUpdate) {
	if h.mouseCount == maxQueuedReports {
		tailIdx := (h.mouseHead + h.mouseCount - 1) % maxQueuedReports
		tail := &h.mouseQueue[tailIdx]
		if tail.Buttons == u.Buttons {
			sumX := int(tail.DX) + int(u.DX)
			sumY := int(tail.DY) + int(u.DY)
			if sumX >= -128 && sumX <= 127 && sumY >= -128 && sumY <= 127 {
				tail.DX = int8(sumX)
				tail.DY = int8(sumY)
				return
			}
		}
		h.mouseHead = (h.mouseHead + 1) % maxQueuedReports
		h.mouseCount--
	}
	tail := (h.mouseHead + h.mouseCount) % maxQueuedReports
	h.mouseQueue[tail] = u
	h.mouseCount++
}

func (h *Hub) recomputeKeyboard() {
	var next [8]byte
	keyCount := 0
	addReport := func(rep [8]byte) {
		next[0] |= rep[0]
		for _, usage := range rep[2:] {
			if usage < 4 || usage > 0xdf {
				continue
			}
			dup := false
			for j := 0; j < keyCount; j++ {
				if next[2+j] == usage {
					dup = true
					break
				}
			}
			if !dup && keyCount < 6 {
				next[2+keyCount] = usage
				keyCount++
			}
		}
	}
	for i := range h.wiredKeyboards {
		if h.wiredKeyboards[i].active {
			addReport(h.wiredKeyboards[i].report)
		}
	}
	for i := range h.btKeyboards {
		addReport(h.btKeyboards[i])
	}
	if next != h.mergedKeyboard {
		h.mergedKeyboard = next
		h.enqueueKeyboard(next)
	}
}

func (h *Hub) recomputeGamepad() {
	merged := usb.GamepadState{Hat: -1}
	var dirs [4]bool
	var home bool
	haveActive := false
	mergeState := func(st usb.GamepadState) {
		merged.Buttons |= st.Buttons
		d := input.GamepadDirections(st)
		for i := range dirs {
			if d[i] {
				dirs[i] = true
			}
		}
		for i := 0; i < 4; i++ {
			if absI16(st.Axes[i]) > absI16(merged.Axes[i]) {
				merged.Axes[i] = st.Axes[i]
			}
		}
		for i := 4; i < 6; i++ {
			if !haveActive || st.Axes[i] > merged.Axes[i] {
				merged.Axes[i] = st.Axes[i]
			}
		}
		haveActive = true
	}
	for i := range h.wiredGamepads {
		if h.wiredGamepads[i].active {
			mergeState(h.wiredGamepads[i].state)
			if h.wiredGamepads[i].home {
				home = true
			}
		}
	}
	for i := range h.btGamepads {
		if h.btSlotActive[i] {
			mergeState(h.btGamepads[i])
			if h.btHome[i] {
				home = true
			}
		}
	}
	merged.Hat = directionsToHat(dirs)
	var ports [MaxPlayerPorts]uint8
	for p, owner := range h.playerPorts {
		switch owner.source {
		case playerPortWired:
			if int(owner.slot) < len(h.wiredGamepads) && h.wiredGamepads[owner.slot].active {
				ports[p] = h.wiredGamepads[owner.slot].ctrlMask
			}
		case playerPortBT:
			if int(owner.slot) < len(h.btMasks) && h.btSlotActive[owner.slot] {
				ports[p] = h.btMasks[owner.slot]
			}
		}
	}
	var (
		pads            [MaxGamepadDevices]GamepadPadState
		padCount        uint8
		nonNeutral      [MaxGamepadDevices]GamepadPadState
		nonNeutralCount uint8
	)
	recordPad := func(id int, player uint8, st controllers.State, rawButtons uint32, rawAxes [6]int16) {
		snap := GamepadPadState{
			ID:         id,
			Player:     player,
			Buttons:    st.Buttons,
			LeftX:      st.LeftX,
			LeftY:      st.LeftY,
			RawButtons: rawButtons,
			RawAxes:    rawAxes,
		}
		if int(padCount) < len(pads) {
			pads[padCount] = snap
			padCount++
		}
		if (st.Buttons != 0 || st.LeftX != 0 || st.LeftY != 0 || rawButtons != 0 || rawAxes != ([6]int16{})) && int(nonNeutralCount) < len(nonNeutral) {
			nonNeutral[nonNeutralCount] = snap
			nonNeutralCount++
		}
	}
	for i := range h.wiredGamepads {
		slot := &h.wiredGamepads[i]
		if !slot.active {
			continue
		}
		var player uint8
		if port, ok := h.playerPortFor(playerPortWired, uint8(i)); ok {
			player = port + 1
		}
		recordPad(1+i, player, slot.ctrlState, rawButtonsForWired(slot), rawAxesForWired(slot))
	}
	for i := range h.btSlotActive {
		if !h.btSlotActive[i] {
			continue
		}
		var player uint8
		if port, ok := h.playerPortFor(playerPortBT, uint8(i)); ok {
			player = port + 1
		}
		recordPad(101+i, player, h.btCtrlState[i], rawButtonsForCtrl(h.btCtrlState[i]), rawAxesForCtrl(h.btCtrlState[i]))
	}
	if merged != h.mergedGamepad || home != h.mergedHome || ports != h.mergedPorts ||
		nonNeutralCount != h.mergedActivePadCount || nonNeutral != h.mergedActivePads {
		h.mergedGamepad = merged
		h.mergedHome = home
		h.mergedPorts = ports
		h.mergedActivePadCount = nonNeutralCount
		h.mergedActivePads = nonNeutral
		h.enqueueGamepad(GamepadUpdate{
			State:       merged,
			HomePressed: home,
			Ports:       ports,
			PadCount:    padCount,
			Pads:        pads,
		})
	}
}

func wiredKindHasPlayerLED(kind controllers.Kind) bool {
	switch kind {
	case controllers.KindXInput, controllers.KindDualSense, controllers.KindDualShock4, controllers.KindSwitchPro:
		return true
	default:
		return false
	}
}

func (h *Hub) syncPlayerPortLED(source playerPortSource, slot uint8, port uint8) {
	switch source {
	case playerPortWired:
		if int(slot) >= len(h.wiredGamepads) {
			return
		}
		w := &h.wiredGamepads[slot]
		if !w.active {
			return
		}
		if !w.ledAssigned || w.ledPort != port {
			w.ledAssigned = true
			w.ledPort = port
			w.ledDirty = wiredKindHasPlayerLED(w.kind)
		}
	case playerPortBT:
		if h.bt != nil {
			_ = h.bt.SetPlayerPort(slot, port, true)
		}
	}
}

func (h *Hub) assignPlayerPort(source playerPortSource, slot uint8) int {
	for p, owner := range h.playerPorts {
		if owner.source == source && owner.slot == slot {
			return p
		}
	}
	for p, owner := range h.playerPorts {
		if owner.source == playerPortNone {
			h.playerPorts[p] = playerPortOwner{source: source, slot: slot}
			h.syncPlayerPortLED(source, slot, uint8(p))
			h.padGeneration++
			return p
		}
	}
	return -1
}

func (h *Hub) releasePlayerPort(source playerPortSource, slot uint8) {
	released := false
	for p, owner := range h.playerPorts {
		if owner.source == source && owner.slot == slot {
			h.playerPorts[p] = playerPortOwner{}
			released = true
		}
	}
	if released {
		h.padGeneration++
		h.promoteWaitingPlayerPorts()
	}
}

func (h *Hub) promoteWaitingPlayerPorts() {
	for i := range h.wiredGamepads {
		if h.wiredGamepads[i].active && h.assignPlayerPort(playerPortWired, uint8(i)) < 0 {
			return
		}
	}
	for i := range h.btSlotActive {
		if !h.btSlotActive[i] {
			continue
		}
		if _, already := h.playerPortFor(playerPortBT, uint8(i)); already {
			continue
		}
		if h.assignPlayerPort(playerPortBT, uint8(i)) < 0 {
			return
		}
		h.btGeneration++
	}
}

func (h *Hub) playerPortFor(source playerPortSource, slot uint8) (uint8, bool) {
	for p, owner := range h.playerPorts {
		if owner.source == source && owner.slot == slot {
			return uint8(p), true
		}
	}
	return 0, false
}

func rawButtonsForCtrl(st controllers.State) uint32 {
	return uint32(st.Buttons&0x7fff) &^ uint32(controllers.ButtonHome)
}

func rawAxesForCtrl(st controllers.State) [6]int16 {
	return [6]int16{
		st.LeftX,
		st.LeftY,
		st.RightX,
		st.RightY,
		int16(int32(st.LeftTrigger) * 32767 / 255),
		int16(int32(st.RightTrigger) * 32767 / 255),
	}
}

func rawButtonsForWired(slot *wiredGamepadSlot) uint32 {
	if slot.kind == controllers.KindGenericHID {
		return slot.state.Buttons
	}
	return rawButtonsForCtrl(slot.ctrlState)
}

func rawAxesForWired(slot *wiredGamepadSlot) [6]int16 {
	if slot.kind == controllers.KindGenericHID {
		return slot.state.Axes
	}
	return rawAxesForCtrl(slot.ctrlState)
}

func usbGamepadStateToControllerState(st usb.GamepadState) controllers.State {
	buttons := controllers.Button(st.Buttons & 0x7fff)
	switch st.Hat {
	case 0:
		buttons |= controllers.ButtonDPadUp
	case 1:
		buttons |= controllers.ButtonDPadUp | controllers.ButtonDPadRight
	case 2:
		buttons |= controllers.ButtonDPadRight
	case 3:
		buttons |= controllers.ButtonDPadDown | controllers.ButtonDPadRight
	case 4:
		buttons |= controllers.ButtonDPadDown
	case 5:
		buttons |= controllers.ButtonDPadDown | controllers.ButtonDPadLeft
	case 6:
		buttons |= controllers.ButtonDPadLeft
	case 7:
		buttons |= controllers.ButtonDPadUp | controllers.ButtonDPadLeft
	}
	return controllers.State{
		Buttons: buttons,
		LeftX:   st.Axes[0],
		LeftY:   st.Axes[1],
	}
}

func usbGamepadToComputerMask(st usb.GamepadState) uint8 {
	var mask uint8
	dirs := input.GamepadDirections(st)
	for i, down := range dirs {
		if down {
			mask |= 1 << uint(i)
		}
	}
	if st.Buttons&(1<<0) != 0 {
		mask |= 1 << 4
	}
	if st.Buttons&(1<<1) != 0 {
		mask |= 1 << 5
	}
	if st.Buttons&(1<<8) != 0 {
		mask |= 1 << 6
	}
	if st.Buttons&(1<<9) != 0 {
		mask |= 1 << 7
	}
	return mask
}

func directionsToHat(dirs [4]bool) int8 {
	up := dirs[0] && !dirs[1]
	down := dirs[1] && !dirs[0]
	left := dirs[2] && !dirs[3]
	right := dirs[3] && !dirs[2]
	switch {
	case up && right:
		return 1
	case down && right:
		return 3
	case down && left:
		return 5
	case up && left:
		return 7
	case up:
		return 0
	case right:
		return 2
	case down:
		return 4
	case left:
		return 6
	default:
		return -1
	}
}

func absI16(v int16) int32 {
	if v < 0 {
		return -int32(v)
	}
	return int32(v)
}

func validBootKeyboardReport(report []byte) bool {
	if len(report) != 8 {
		return false
	}
	for i, usage := range report[2:] {
		if usage == 0 {
			continue
		}
		if usage <= 3 || (usage >= 0xe0 && usage <= 0xe7) {
			return false
		}
		for j := 0; j < i; j++ {
			if report[2+j] == usage {
				return false
			}
		}
	}
	return true
}
