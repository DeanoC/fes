package tenfoot

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/gfx"
)

type fakeBluetoothController struct {
	status    BluetoothStatus
	startErr  error
	stopErr   error
	forgetErr error
	starts    []time.Duration
	stops     int
	forgotten []string
}

func (f *fakeBluetoothController) BluetoothStatus() BluetoothStatus {
	st := f.status
	if len(st.Devices) > 0 {
		st.Devices = append([]BluetoothDevice(nil), st.Devices...)
	}
	return st
}

func (f *fakeBluetoothController) StartBluetoothPairing(duration time.Duration) error {
	f.starts = append(f.starts, duration)
	if f.startErr != nil {
		return f.startErr
	}
	f.status.PairingActive = true
	f.status.PairingRemaining = duration
	return nil
}

func (f *fakeBluetoothController) StopBluetoothPairing() error {
	f.stops++
	if f.stopErr != nil {
		return f.stopErr
	}
	f.status.PairingActive = false
	f.status.PairingRemaining = 0
	return nil
}

func (f *fakeBluetoothController) ForgetBluetoothDevice(address string) error {
	f.forgotten = append(f.forgotten, address)
	if f.forgetErr != nil {
		return f.forgetErr
	}
	kept := f.status.Devices[:0]
	for _, dev := range f.status.Devices {
		if !strings.EqualFold(dev.Address, address) {
			kept = append(kept, dev)
		} else if dev.Connected && f.status.ConnectedCount > 0 {
			f.status.ConnectedCount--
		}
	}
	f.status.Devices = kept
	return nil
}

func TestSetBluetoothValidationAndSectionOmission(t *testing.T) {
	app := pairedKitSettingsApp(t)
	if err := app.SetBluetooth(nil); err == nil {
		t.Fatal("expected error for nil BluetoothController")
	}
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	if strings.Contains(settingsLabels(app.Snapshot()), "Bluetooth") {
		t.Fatal("Bluetooth section should be omitted when no BluetoothController is attached")
	}

	app.Start(t.Context())
	defer app.Stop()
	if err := app.SetBluetooth(&fakeBluetoothController{}); err == nil {
		t.Fatal("expected SetBluetooth after Start to fail")
	}
}

func TestSettingsBluetoothPairingAndForgetLifecycle(t *testing.T) {
	app := pairedKitSettingsApp(t)
	bt := &fakeBluetoothController{
		status: BluetoothStatus{
			DongleConnected: false,
		},
	}
	if err := app.SetBluetooth(bt); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	if got := settingsRowByID(t, app, settingsSectionRowPrefix+settingsSectionBluetooth).Value; got != "No adapter" {
		t.Fatalf("no-adapter summary = %q", got)
	}

	// Open Bluetooth section with no adapter -> bt-pair refuses gracefully.
	focusSettingsRow(t, app, "bt-pair", now)
	if snap := app.Snapshot(); snap.Settings.Title != "Bluetooth" || settingsLabels(snap) != "Adapter, Pair controller, Paired devices" {
		t.Fatalf("no-adapter rows = %#v", snap.Settings)
	}
	app.HandleCommand(CmdSelect, now)
	if snap := app.Snapshot(); snap.Settings.Status != "Connect a USB Bluetooth adapter" {
		t.Fatalf("no-adapter pair status = %q", snap.Settings.Status)
	}

	// Plug adapter + add one connected pad (P1) and one paired-offline controller.
	bt.status = BluetoothStatus{
		DongleConnected: true,
		DongleName:      "Realtek RTL8761BU",
		ConnectedCount:  1,
		Devices: []BluetoothDevice{
			{Address: "AA:BB:CC:DD:EE:01", Name: "DualSense Wireless Controller", Kind: "DualSense", Connected: true, Slot: 1},
			{Address: "AA:BB:CC:DD:EE:02", Name: "Pro Controller", Kind: "Switch Pro"},
		},
		BondsPending: true,
	}
	snap := app.Snapshot()
	if !strings.HasPrefix(snap.Settings.Hint, "Bluetooth bonds waiting to save · ") {
		t.Fatalf("expected bond pending prefix in hint, got %q", snap.Settings.Hint)
	}
	if got := settingsRowByID(t, app, "bt-adapter").Value; got != "Realtek RTL8761BU" {
		t.Fatalf("adapter value = %q", got)
	}
	if got := settingsRowByID(t, app, "bt-device-AA:BB:CC:DD:EE:01").Value; got != "Connected  ·  P1" {
		t.Fatalf("connected device value = %q", got)
	}
	if got := settingsRowByID(t, app, "bt-device-AA:BB:CC:DD:EE:02").Value; got != "Paired  ·  Switch Pro" {
		t.Fatalf("offline device value = %q", got)
	}

	// Pairing requires a second Select before opening the 30s window.
	focusSettingsRow(t, app, "bt-pair", now)
	app.HandleCommand(CmdSelect, now)
	if len(bt.starts) != 0 {
		t.Fatal("first Select should only confirm the nearby-controller pairing action")
	}
	if got := settingsRowByID(t, app, "bt-pair").Value; got != "A again to pair nearby controller" {
		t.Fatalf("pairing confirmation = %q", got)
	}
	app.HandleCommand(CmdSelect, now)
	if len(bt.starts) != 1 || bt.starts[0] != 30*time.Second {
		t.Fatalf("expected 30s pairing start, got %v", bt.starts)
	}
	bt.status.PairingRemaining = 14200 * time.Millisecond
	if got := settingsRowByID(t, app, "bt-pair").Value; got != "Pairing (15s)  ·  A stop" {
		t.Fatalf("pairing value = %q", got)
	}
	// Back to section list checks summary during pairing.
	app.HandleCommand(CmdBack, now)
	if got := settingsRowByID(t, app, settingsSectionRowPrefix+settingsSectionBluetooth).Value; got != "Pairing  ·  15s left" {
		t.Fatalf("pairing summary = %q", got)
	}
	focusSettingsRow(t, app, "bt-pair", now)
	app.HandleCommand(CmdSelect, now)
	if bt.stops != 1 || bt.status.PairingActive {
		t.Fatalf("expected pairing stopped, stops=%d active=%t", bt.stops, bt.status.PairingActive)
	}

	// Render check at 1280x720.
	dev, err := gfx.NewSoftware(1280, 720)
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Close()
	presenter, err := NewPresenter(dev)
	if err != nil {
		t.Fatal(err)
	}
	defer presenter.Close()
	if err := presenter.Draw(app.Snapshot()); err != nil {
		t.Fatalf("Draw settings bluetooth failed: %v", err)
	}

	// First Select on paired device arms two-press confirm; Up/Down cancels it.
	focusSettingsRow(t, app, "bt-device-AA:BB:CC:DD:EE:02", now)
	app.HandleCommand(CmdSelect, now)
	if got := settingsRowByID(t, app, "bt-device-AA:BB:CC:DD:EE:02").Value; got != "A again to forget" {
		t.Fatalf("forget confirm = %q", got)
	}
	if len(bt.forgotten) != 0 {
		t.Fatal("first Select should not forget without confirmation")
	}
	app.HandleCommand(CmdUp, now)
	app.HandleCommand(CmdDown, now)
	if got := settingsRowByID(t, app, "bt-device-AA:BB:CC:DD:EE:02").Value; got != "Paired  ·  Switch Pro" {
		t.Fatalf("moving away did not cancel forget confirm: %q", got)
	}

	// Failed bond save prefix surfaces in footer, then second Select forgets device.
	bt.status.BondsPending = false
	bt.status.BondsSaveFailed = true
	snap = app.Snapshot()
	if !strings.HasPrefix(snap.Settings.Hint, "Bluetooth bonds not saved · ") {
		t.Fatalf("expected bond save failed prefix in hint, got %q", snap.Settings.Hint)
	}

	app.HandleCommand(CmdSelect, now)
	app.HandleCommand(CmdSelect, now)
	if len(bt.forgotten) != 1 || bt.forgotten[0] != "AA:BB:CC:DD:EE:02" {
		t.Fatalf("expected AA:BB:CC:DD:EE:02 forgotten, got %v", bt.forgotten)
	}
	if len(bt.status.Devices) != 1 {
		t.Fatalf("expected 1 device remaining, got %+v", bt.status.Devices)
	}

	// Error paths on Start/Stop/Forget surface cleanly in settingsStatus.
	bt.forgetErr = errors.New("forget failed")
	focusSettingsRow(t, app, "bt-device-AA:BB:CC:DD:EE:01", now)
	app.HandleCommand(CmdSelect, now)
	app.HandleCommand(CmdSelect, now)
	if got := app.Snapshot().Settings.Status; got != "forget failed" {
		t.Fatalf("forget error status = %q", got)
	}
	bt.startErr = errors.New("start failed")
	focusSettingsRow(t, app, "bt-pair", now)
	app.HandleCommand(CmdSelect, now)
	app.HandleCommand(CmdSelect, now)
	if got := app.Snapshot().Settings.Status; got != "start failed" {
		t.Fatalf("start error status = %q", got)
	}
	bt.startErr = nil
	bt.status.PairingActive = true
	bt.stopErr = errors.New("stop failed")
	app.HandleCommand(CmdSelect, now)
	if got := app.Snapshot().Settings.Status; got != "stop failed" {
		t.Fatalf("stop error status = %q", got)
	}
}
