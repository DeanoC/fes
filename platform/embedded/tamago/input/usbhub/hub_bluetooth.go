// SPDX-License-Identifier: GPL-3.0-or-later

package usbhub

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/bt/hidp"
	"github.com/DeanoC/fes/platform/embedded/tamago/bt/l2cap"
	"github.com/DeanoC/fes/platform/embedded/tamago/input/controllers"
)

// StartPairing opens a bounded SSP pairing window in menu mode.
func (h *Hub) StartPairing(duration time.Duration) error {
	if h == nil || h.bt == nil {
		return errors.New("usbhub: Bluetooth unavailable")
	}
	err := h.bt.StartPairing(duration)
	h.btGeneration++
	return err
}

// StopPairing closes any open Bluetooth pairing window.
func (h *Hub) StopPairing() error {
	if h == nil || h.bt == nil {
		return nil
	}
	err := h.bt.StopPairing()
	h.btGeneration++
	return err
}

// PairingActive reports whether a Bluetooth pairing window is open.
func (h *Hub) PairingActive() bool {
	return h != nil && h.bt != nil && h.bt.PairingActive()
}

// PairingRemaining returns the remaining time in the active Bluetooth pairing window.
func (h *Hub) PairingRemaining() time.Duration {
	if h == nil || h.bt == nil {
		return 0
	}
	return h.bt.PairingRemaining()
}

// BluetoothGeneration returns a monotonically increasing counter bumped whenever
// Bluetooth dongle, pairing, connected slot, or bond persistence state changes.
func (h *Hub) BluetoothGeneration() uint64 {
	if h == nil {
		return 0
	}
	return h.btGeneration
}

// GamepadGeneration returns a monotonically increasing counter bumped whenever
// connected wired/Bluetooth gamepads, player port assignments, or physical
// button/stick states change.
func (h *Hub) GamepadGeneration() uint64 {
	if h == nil {
		return 0
	}
	return h.padGeneration
}

// GamepadDevicesInto writes the currently connected wired USB and Classic
// Bluetooth gamepads into dst without allocating and returns the count written.
func (h *Hub) GamepadDevicesInto(dst []GamepadDevice) int {
	if h == nil || len(dst) == 0 {
		return 0
	}
	count := 0
	for i := range h.wiredGamepads {
		if count >= len(dst) {
			return count
		}
		slot := &h.wiredGamepads[i]
		if !slot.active {
			continue
		}
		player := 0
		if port, ok := h.playerPortFor(playerPortWired, uint8(i)); ok {
			player = int(port) + 1
		}
		dst[count] = GamepadDevice{
			ID:           1 + i,
			Name:         friendlyGamepadName(slot.kind, slot.name, false),
			IdentityName: slot.name,
			Vendor:       slot.vendorID,
			Product:      slot.productID,
			Version:      slot.version,
			Kind:         slot.kind,
			Player:       player,
			Buttons:      slot.ctrlState.Buttons,
			LeftX:        slot.ctrlState.LeftX,
			LeftY:        slot.ctrlState.LeftY,
			RawButtons:   rawButtonsForWired(slot),
			RawAxes:      rawAxesForWired(slot),
		}
		count++
	}
	for i := range h.btSlotActive {
		if count >= len(dst) {
			return count
		}
		if !h.btSlotActive[i] {
			continue
		}
		rawName := h.btSlotName[i]
		kind := h.btSlotKind[i]
		if h.bt != nil {
			if ps, ok := h.bt.Player(uint8(i)); ok {
				if ps.Name != "" {
					rawName = ps.Name
				}
				if ps.Kind != controllers.KindUnknown {
					kind = ps.Kind
				}
			}
		}
		var vendor, product, version uint16
		identityName := rawName
		if h.bt != nil {
			if ps, ok := h.bt.Player(uint8(i)); ok {
				bondName, bondVendor, bondProduct, bondVersion := h.bluetoothIdentity(ps.Addr)
				vendor, product, version = bondVendor, bondProduct, bondVersion
				if identityName == "" {
					identityName = bondName
				}
				if rawName == "" {
					rawName = bondName
				}
			}
		}
		player := 0
		if port, ok := h.playerPortFor(playerPortBT, uint8(i)); ok {
			player = int(port) + 1
		}
		st := h.btCtrlState[i]
		dst[count] = GamepadDevice{
			ID:           101 + i,
			Name:         friendlyGamepadName(kind, rawName, true),
			IdentityName: identityName,
			Vendor:       vendor,
			Product:      product,
			Version:      version,
			Kind:         kind,
			Bluetooth:    true,
			Player:       player,
			Buttons:      st.Buttons,
			LeftX:        st.LeftX,
			LeftY:        st.LeftY,
			RawButtons:   rawButtonsForCtrl(st),
			RawAxes:      rawAxesForCtrl(st),
		}
		count++
	}
	return count
}

func (h *Hub) bluetoothIdentity(addr l2cap.BDAddr) (name string, vendor, product, version uint16) {
	if h == nil || h.cfg.Bonds == nil {
		return "", 0, 0, 0
	}
	rec, ok := h.cfg.Bonds.Lookup(addr)
	if !ok {
		return "", 0, 0, 0
	}
	return rec.Name, rec.VendorID, rec.ProductID, rec.Version
}

func friendlyGamepadName(kind controllers.Kind, rawName string, bluetooth bool) string {
	if trimmed := strings.TrimSpace(rawName); trimmed != "" {
		return trimmed
	}
	switch kind {
	case controllers.KindDualSense:
		return "DualSense Wireless Controller"
	case controllers.KindDualShock4:
		return "DualShock 4 Wireless Controller"
	case controllers.KindSwitchPro:
		return "Nintendo Switch Pro Controller"
	case controllers.KindXInput:
		return "Xbox 360 Controller"
	case controllers.KindXboxGIP:
		return "Xbox Wireless Controller"
	default:
		if bluetooth {
			return "Bluetooth Gamepad"
		}
		return "USB Gamepad"
	}
}

// BluetoothSnapshot returns a merged view of adapter state, active pairing window,
// bonded devices, connected player slots, and pending/failed bond saves.
func (h *Hub) BluetoothSnapshot() BluetoothSnapshot {
	if h == nil || h.bt == nil {
		return BluetoothSnapshot{}
	}
	snap := BluetoothSnapshot{
		DongleConnected:  h.bt.DongleConnected(),
		DongleName:       h.bt.DongleSpec().Name,
		PairingActive:    h.bt.PairingActive(),
		PairingRemaining: h.bt.PairingRemaining(),
		ConnectedCount:   h.bt.ConnectedPlayers(),
		BondsPending:     h.cfg.Bonds != nil && h.cfg.Bonds.Dirty() && h.cfg.PersistBonds != nil,
		BondsSaveFailed:  h.bondSaveFailed,
	}
	byAddr := make(map[l2cap.BDAddr]BluetoothDevice)
	if h.cfg.Bonds != nil {
		for _, rec := range h.cfg.Bonds.List() {
			byAddr[rec.Addr] = BluetoothDevice{
				Address: rec.Addr.String(),
				Name:    rec.Name,
				Kind:    rec.Kind,
			}
		}
	}
	for slotIdx := uint8(0); slotIdx < hidp.MaxPlayerSlots; slotIdx++ {
		ps, ok := h.bt.Player(slotIdx)
		if !ok {
			continue
		}
		dev := byAddr[ps.Addr]
		dev.Address = ps.Addr.String()
		if ps.Name != "" {
			dev.Name = ps.Name
		}
		if ps.Kind != controllers.KindUnknown {
			dev.Kind = ps.Kind
		}
		dev.Connected = true
		if port, ok := h.playerPortFor(playerPortBT, slotIdx); ok {
			dev.Slot = port + 1
		} else {
			dev.Slot = 0
		}
		byAddr[ps.Addr] = dev
	}
	if len(byAddr) > 0 {
		snap.Devices = make([]BluetoothDevice, 0, len(byAddr))
		for _, dev := range byAddr {
			if strings.TrimSpace(dev.Name) == "" {
				if dev.Kind != controllers.KindUnknown {
					dev.Name = dev.Kind.String()
				} else {
					dev.Name = "Bluetooth device"
				}
			}
			snap.Devices = append(snap.Devices, dev)
		}
		slices.SortFunc(snap.Devices, func(a, b BluetoothDevice) int {
			if a.Connected != b.Connected {
				if a.Connected {
					return -1
				}
				return 1
			}
			if a.Connected && a.Slot != b.Slot {
				if a.Slot == 0 {
					return 1
				}
				if b.Slot == 0 {
					return -1
				}
				if a.Slot < b.Slot {
					return -1
				}
				return 1
			}
			if cmp := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); cmp != 0 {
				return cmp
			}
			return strings.Compare(a.Address, b.Address)
		})
	}
	return snap
}

// ForgetBluetoothDevice closes any active pairing window, deletes the stored
// bond for address, and disconnects any active connection to it.
func (h *Hub) ForgetBluetoothDevice(address string) error {
	if h == nil || h.bt == nil {
		return errors.New("usbhub: Bluetooth unavailable")
	}
	addr, err := l2cap.ParseBDAddr(address)
	if err != nil {
		return err
	}
	wasPairing := h.bt.PairingActive()
	found, err := h.bt.ForgetDevice(addr)
	if found {
		h.bondRetryAt = time.Time{}
		h.bondRetryDelay = 0
		h.bondSaveFailed = false
		h.btGeneration++
	} else if h.bt.PairingActive() != wasPairing {
		h.btGeneration++
	}
	if err != nil {
		return err
	}
	if !found {
		return errors.New("usbhub: Bluetooth device not found")
	}
	return nil
}

// SaveBondsIfDirty persists updated Bluetooth bonds when configured and
// clear-marks dirty on success. After a failed attempt it returns (false, nil)
// without writing until a doubling delay (1 s, capped at 1 min) has elapsed,
// so a refusing store reports each failure once per retry, not once per poll.
func (h *Hub) SaveBondsIfDirty(ctx context.Context) (bool, error) {
	if h == nil || h.cfg.Bonds == nil || !h.cfg.Bonds.Dirty() || h.cfg.PersistBonds == nil {
		return false, nil
	}
	now := h.cfg.Now()
	if !h.bondRetryAt.IsZero() && now.Before(h.bondRetryAt) {
		return false, nil
	}
	raw, err := h.cfg.Bonds.MarshalJSON()
	if err == nil {
		err = h.cfg.PersistBonds(ctx, raw)
	}
	if err != nil {
		h.bondRetryDelay = min(max(2*h.bondRetryDelay, bondRetryInitial), bondRetryMax)
		h.bondRetryAt = now.Add(h.bondRetryDelay)
		if !h.bondSaveFailed {
			h.bondSaveFailed = true
			h.btGeneration++
		}
		return false, err
	}
	h.cfg.Bonds.ClearDirty()
	h.bondRetryAt, h.bondRetryDelay = time.Time{}, 0
	h.bondSaveFailed = false
	h.btGeneration++
	return true, nil
}

func (h *Hub) activateBTMouse(slot uint8) {
	// Unresolved HIDP slots may emit a boot-mouse report, so they remain active
	// for the neutral fence until a keyboard report or known non-mouse kind
	// clears them.
	if !h.btMouseActive[slot] {
		h.btMouseReported[slot] = false
		h.btMouseButtons[slot] = 0
	}
	h.btMouseActive[slot] = true
}

func (h *Hub) deactivateBTMouse(slot uint8) {
	h.btMouseActive[slot] = false
	h.btMouseReported[slot] = false
	if h.btMouseButtons[slot] != 0 {
		h.btMouseButtons[slot] = 0
		merged := h.computeMouseButtons()
		if merged != h.mergedMouseButtons {
			h.mergedMouseButtons = merged
			h.enqueueMouse(MouseUpdate{Buttons: merged})
		}
	}
}
