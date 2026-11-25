package tenfoot

import (
	"fmt"
	"strings"
	"time"
)

const (
	defaultBluetoothPairingDuration  = 30 * time.Second
	settingsBluetoothDeviceRowPrefix = "bt-device-"
)

// BluetoothDevice describes one paired or connected Bluetooth device for
// Settings › Bluetooth.
type BluetoothDevice struct {
	Address   string
	Name      string
	Kind      string
	Connected bool
	Slot      int
}

// BluetoothStatus describes the local Bluetooth adapter, active pairing window,
// paired/connected devices, and bond save status.
type BluetoothStatus struct {
	DongleConnected  bool
	DongleName       string
	PairingActive    bool
	PairingRemaining time.Duration
	ConnectedCount   int
	Devices          []BluetoothDevice
	BondsPending     bool
	BondsSaveFailed  bool
}

// BluetoothController connects Settings › Bluetooth to a local Bluetooth stack.
// BluetoothStatus must be nonblocking and perform no IO; no method may call back into App.
type BluetoothController interface {
	BluetoothStatus() BluetoothStatus
	StartBluetoothPairing(duration time.Duration) error
	StopBluetoothPairing() error
	ForgetBluetoothDevice(address string) error
}

// SetBluetooth attaches an optional local Bluetooth controller before Start.
func (a *App) SetBluetooth(bt BluetoothController) error {
	if a == nil || bt == nil {
		return fmt.Errorf("tenfoot bluetooth controller is nil")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		return fmt.Errorf("tenfoot bluetooth controller must be set before Start")
	}
	a.bluetooth = bt
	return nil
}

func settingsBluetoothDeviceRowID(addr string) string {
	return settingsBluetoothDeviceRowPrefix + strings.ToUpper(strings.TrimSpace(addr))
}

func parseSettingsBluetoothDeviceRowID(id string) (string, bool) {
	addr, ok := strings.CutPrefix(id, settingsBluetoothDeviceRowPrefix)
	if !ok || strings.TrimSpace(addr) == "" {
		return "", false
	}
	return addr, true
}

func btRemainingSeconds(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int((d + time.Second - 1) / time.Second)
}

func (a *App) bluetoothStatusLocked() BluetoothStatus {
	if a.bluetooth == nil {
		return BluetoothStatus{}
	}
	return a.bluetooth.BluetoothStatus()
}

func (a *App) settingsBluetoothSummaryLocked() string {
	st := a.bluetoothStatusLocked()
	if st.PairingActive {
		if sec := btRemainingSeconds(st.PairingRemaining); sec > 0 {
			return fmt.Sprintf("Pairing  ·  %ds left", sec)
		}
		return "Pairing…"
	}
	if !st.DongleConnected {
		if len(st.Devices) > 0 {
			return fmt.Sprintf("No adapter  ·  %d paired", len(st.Devices))
		}
		return "No adapter"
	}
	if st.ConnectedCount > 0 {
		if len(st.Devices) > st.ConnectedCount {
			return fmt.Sprintf("%d connected  ·  %d paired", st.ConnectedCount, len(st.Devices))
		}
		return fmt.Sprintf("%d connected", st.ConnectedCount)
	}
	if len(st.Devices) > 0 {
		return fmt.Sprintf("%d paired", len(st.Devices))
	}
	return "Ready"
}

func (a *App) settingsBluetoothRowsLocked() []SettingsRow {
	if a.bluetooth == nil {
		return nil
	}
	st := a.bluetooth.BluetoothStatus()
	adapter := "Not connected"
	if st.DongleConnected {
		adapter = strings.TrimSpace(st.DongleName)
		if adapter == "" {
			adapter = "Connected"
		}
	}
	pairValue := "Connect a USB Bluetooth adapter"
	switch {
	case st.PairingActive:
		if sec := btRemainingSeconds(st.PairingRemaining); sec > 0 {
			pairValue = fmt.Sprintf("Pairing (%ds)  ·  A stop", sec)
		} else {
			pairValue = "Pairing  ·  A stop"
		}
	case st.DongleConnected:
		if a.settingsConfirm == "bt-pair" {
			pairValue = "A again to pair nearby controller"
		} else {
			pairValue = "A pair controller (30s)"
		}
	}
	rows := []SettingsRow{
		{ID: "bt-adapter", Label: "Adapter", Value: adapter},
		{ID: "bt-pair", Label: "Pair controller", Value: pairValue},
	}
	if len(st.Devices) == 0 {
		return append(rows, SettingsRow{ID: "bt-none", Label: "Paired devices", Value: "None"})
	}
	for _, dev := range st.Devices {
		addr := strings.TrimSpace(dev.Address)
		if addr == "" {
			continue
		}
		id := settingsBluetoothDeviceRowID(addr)
		label := strings.TrimSpace(dev.Name)
		kind := strings.TrimSpace(dev.Kind)
		if label == "" {
			label = kind
		}
		if label == "" {
			label = addr
		}
		var value string
		switch {
		case a.settingsConfirm == id:
			value = "A again to forget"
		case dev.Connected:
			if dev.Slot > 0 {
				value = fmt.Sprintf("Connected  ·  P%d", dev.Slot)
			} else {
				value = "Connected"
			}
		case kind != "" && !strings.EqualFold(kind, label):
			value = "Paired  ·  " + kind
		default:
			value = "Paired"
		}
		rows = append(rows, SettingsRow{ID: id, Label: label, Value: value})
	}
	return rows
}

func (a *App) toggleSettingsBluetoothPairingLocked() {
	if a.bluetooth == nil {
		return
	}
	st := a.bluetooth.BluetoothStatus()
	if st.PairingActive {
		if err := a.bluetooth.StopBluetoothPairing(); err != nil {
			a.settingsStatus = settingsStatusError(err)
		} else {
			a.settingsStatus = "Bluetooth pairing stopped"
		}
		a.status = a.settingsStatus
		return
	}
	if !st.DongleConnected {
		a.settingsConfirm = ""
		a.settingsStatus = "Connect a USB Bluetooth adapter"
		a.status = a.settingsStatus
		return
	}
	if a.settingsConfirm != "bt-pair" {
		a.settingsConfirm = "bt-pair"
		a.settingsStatus = ""
		return
	}
	a.settingsConfirm = ""
	if err := a.bluetooth.StartBluetoothPairing(defaultBluetoothPairingDuration); err != nil {
		a.settingsStatus = settingsStatusError(err)
	} else {
		a.settingsStatus = "Put controller in pairing mode (30s)"
	}
	a.status = a.settingsStatus
}

func (a *App) handleSettingsBluetoothDeviceLocked(id, addr string) {
	if a.bluetooth == nil {
		return
	}
	if a.settingsConfirm != id {
		a.settingsConfirm = id
		a.settingsStatus = ""
		return
	}
	a.settingsConfirm = ""
	if err := a.bluetooth.ForgetBluetoothDevice(addr); err != nil {
		a.settingsStatus = settingsStatusError(err)
		a.status = a.settingsStatus
		return
	}
	a.settingsStatus = "Controller forgotten"
	a.status = a.settingsStatus
	a.clampSettingsIndexLocked()
}
