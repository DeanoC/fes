// SPDX-License-Identifier: GPL-3.0-or-later
package bt

import (
	"errors"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/bt/hci"
	"github.com/DeanoC/fes/platform/embedded/tamago/bt/hidp"
	"github.com/DeanoC/fes/platform/embedded/tamago/bt/l2cap"
	"github.com/DeanoC/fes/platform/embedded/tamago/input/controllers"
	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

const dongleRetryBackoff = 500 * time.Millisecond

// Manager supervises one hotpluggable USB Bluetooth HCI dongle on a usb.BusHost
// and drives Classic Bluetooth HIDP sessions for up to 4 concurrent Sony
// DualSense, DualShock 4, Nintendo Switch Pro, and BT HID Keyboard devices.
type Manager struct {
	bus   usb.BusHost
	bonds *l2cap.BondStore
	fw    hci.FirmwareLoader
	now   func() time.Time

	dongleAddr     uint8
	tr             *hci.Transport
	sess           *hidp.Session
	gameplayActive bool
	retryAfter     time.Time

	OnGamepadState   func(slot uint8, kind controllers.Kind, state controllers.State)
	OnKeyboardReport func(report []byte)
	OnSlotChanged    func(slot uint8, connected bool, kind controllers.Kind, name string)
	OnDongleChanged  func(connected bool, spec hci.DongleSpec)
}

// NewManager constructs a Bluetooth input manager bound to bus. Any USB
// Bluetooth dongle already attached to bus is discovered on the first Service().
func NewManager(bus usb.BusHost, bonds *l2cap.BondStore, fw hci.FirmwareLoader, now func() time.Time) *Manager {
	if bonds == nil {
		bonds = l2cap.NewBondStore()
	}
	if now == nil {
		now = time.Now
	}
	return &Manager{bus: bus, bonds: bonds, fw: fw, now: now}
}

// DongleConnected reports whether an initialized USB BT HCI dongle is active.
func (m *Manager) DongleConnected() bool { return m != nil && m.sess != nil }

// DongleSpec returns the active dongle's curated specification and quirks.
func (m *Manager) DongleSpec() hci.DongleSpec {
	if m == nil || m.tr == nil {
		return hci.DongleSpec{}
	}
	return m.tr.Spec()
}

// Bonds returns the shared BondStore for FES/BTBONDS.JSON checkpointing.
func (m *Manager) Bonds() *l2cap.BondStore { return m.bonds }

// PairingActive reports whether a bounded SSP pairing window is currently open.
func (m *Manager) PairingActive() bool {
	return m != nil && m.sess != nil && m.sess.PairingActive()
}

// StartPairing opens a bounded SSP pairing window (default 30 s) for Tenfoot
// Settings. Refused while gameplay is active so RF inquiry never disrupts play.
func (m *Manager) StartPairing(duration time.Duration) error {
	if m == nil || m.sess == nil {
		return errors.New("bt: no Bluetooth dongle connected")
	}
	if m.gameplayActive {
		return errors.New("bt: cannot start pairing while gameplay is active")
	}
	return m.sess.SetPairingMode(true, duration)
}

// StopPairing closes an open SSP pairing window and restores Page Scan only.
func (m *Manager) StopPairing() error {
	if m == nil || m.sess == nil {
		return nil
	}
	return m.sess.SetPairingMode(false, 0)
}

// SetGameplayActive transitions the radio between gameplay mode (ScanDisabled,
// zero RF scan spikes) and menu/idle mode (Page Scan only for fast reconnect).
func (m *Manager) SetGameplayActive(active bool) error {
	if m == nil {
		return nil
	}
	m.gameplayActive = active
	if m.sess == nil {
		return nil
	}
	return m.sess.SetGameplayActive(active)
}

// ConnectedPlayers returns the count of active BT HIDP controllers.
func (m *Manager) ConnectedPlayers() int {
	if m == nil || m.sess == nil {
		return 0
	}
	return m.sess.ConnectedCount()
}

// Player returns a snapshot of player slot index (0..3).
func (m *Manager) Player(slot uint8) (hidp.PlayerSlot, bool) {
	if m == nil || m.sess == nil {
		return hidp.PlayerSlot{}, false
	}
	return m.sess.Slot(slot)
}

// SetRumble sends a rumble/LED update to an active DualSense slot (0..3).
func (m *Manager) SetRumble(slot uint8, right, left uint8) error {
	if m == nil || m.sess == nil {
		return errors.New("bt: no Bluetooth dongle connected")
	}
	return m.sess.SetRumble(slot, right, left)
}

func (m *Manager) attachDongle(dev usb.BusDevice) {
	if m.sess != nil {
		return // One active BT HCI radio at a time
	}
	if _, _, ok := hci.FindBluetoothInterface(dev); !ok {
		return
	}
	tr, err := hci.Open(m.bus, dev)
	if err != nil {
		return
	}
	if err := tr.Initialize(m.fw); err != nil {
		_ = tr.Close()
		return
	}
	sess, err := hidp.NewSession(tr, m.bonds, m.now)
	if err != nil {
		_ = tr.Close()
		return
	}
	sess.OnGamepadState = func(slot uint8, kind controllers.Kind, state controllers.State) {
		if m.OnGamepadState != nil {
			m.OnGamepadState(slot, kind, state)
		}
	}
	sess.OnKeyboardReport = func(report []byte) {
		if m.OnKeyboardReport != nil {
			m.OnKeyboardReport(report)
		}
	}
	sess.OnSlotChanged = func(slot uint8, connected bool, kind controllers.Kind, name string) {
		if m.OnSlotChanged != nil {
			m.OnSlotChanged(slot, connected, kind, name)
		}
	}
	if m.gameplayActive {
		_ = sess.SetGameplayActive(true)
	}
	m.dongleAddr = dev.Address
	m.tr = tr
	m.sess = sess
	if m.OnDongleChanged != nil {
		m.OnDongleChanged(true, tr.Spec())
	}
}

func (m *Manager) detachDongle() {
	if m.sess == nil && m.tr == nil {
		return
	}
	spec := m.DongleSpec()
	// Emit neutral/disconnected notifications for any active player slots so
	// gameplay never sees stuck buttons or keys after a dongle unplug.
	if m.sess != nil {
		var neutralKeyboardReport [8]byte
		for idx := uint8(0); idx < hidp.MaxPlayerSlots; idx++ {
			if slot, ok := m.sess.Slot(idx); ok {
				if slot.Kind != controllers.KindUnknown {
					if m.OnGamepadState != nil {
						m.OnGamepadState(idx, slot.Kind, controllers.State{})
					}
				} else if m.OnKeyboardReport != nil {
					m.OnKeyboardReport(neutralKeyboardReport[:])
				}
				if m.OnSlotChanged != nil {
					m.OnSlotChanged(idx, false, slot.Kind, slot.Name)
				}
			}
		}
	}
	if m.tr != nil {
		_ = m.tr.Close()
	}
	m.dongleAddr = 0
	m.tr = nil
	m.sess = nil
	if m.OnDongleChanged != nil {
		m.OnDongleChanged(false, spec)
	}
}

// Service advances the underlying USB bus, attaches/detaches hotplugged USB BT
// dongles, and pumps active HIDP controller traffic.
func (m *Manager) Service() error {
	if m == nil || m.bus == nil {
		return errors.New("bt: nil bus host")
	}
	if err := m.bus.Service(); err != nil {
		return err
	}
	for {
		ev, ok := m.bus.NextEvent()
		if !ok {
			break
		}
		switch ev.Kind {
		case usb.DeviceAttached:
			m.attachDongle(ev.Device)
			if m.sess != nil {
				m.retryAfter = time.Time{}
			} else {
				m.retryAfter = m.now().Add(dongleRetryBackoff)
			}
		case usb.DeviceDetached:
			if m.dongleAddr != 0 && ev.Device.Address == m.dongleAddr {
				m.detachDongle()
				m.retryAfter = time.Time{}
			}
		}
	}
	if m.sess == nil && (m.retryAfter.IsZero() || !m.now().Before(m.retryAfter)) {
		for _, dev := range m.bus.Devices() {
			if _, _, ok := hci.FindBluetoothInterface(dev); !ok {
				continue
			}
			m.attachDongle(dev)
			if m.sess != nil {
				m.retryAfter = time.Time{}
				break
			}
			m.retryAfter = m.now().Add(dongleRetryBackoff)
		}
	}
	if m.sess == nil {
		return nil
	}
	if err := m.sess.Service(); err != nil {
		// Isolate per-dongle pipe errors by detaching cleanly without crashing the bus,
		// and back off before re-attaching a still-enumerated adapter.
		m.detachDongle()
		m.retryAfter = m.now().Add(dongleRetryBackoff)
	}
	return nil
}
