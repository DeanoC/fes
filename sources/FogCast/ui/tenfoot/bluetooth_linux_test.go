//go:build linux

package tenfoot

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func tempBTSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "tenfoot-bt-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "bt.sock")
}

type fakeBTAgentServer struct {
	mu      sync.Mutex
	calls   []string
	status  btAgentStatusJSON
	pairing bool
}

func (f *fakeBTAgentServer) serve(t *testing.T, sock string) {
	t.Helper()
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen %s: %v", sock, err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		st := f.snapshotLocked()
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(st)
	})
	mux.HandleFunc("POST /pairing/start", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls = append(f.calls, "start")
		f.pairing = true
		f.status.PairingActive = true
		f.status.PairingRemainingMS = 30000
		st := f.snapshotLocked()
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(st)
	})
	mux.HandleFunc("POST /pairing/stop", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls = append(f.calls, "stop")
		f.pairing = false
		f.status.PairingActive = false
		f.status.PairingRemainingMS = 0
		st := f.snapshotLocked()
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(st)
	})
	mux.HandleFunc("POST /devices/forget", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Address string `json:"address"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.calls = append(f.calls, "forget:"+strings.ToUpper(req.Address))
		kept := f.status.Devices[:0]
		f.status.ConnectedCount = 0
		for _, d := range f.status.Devices {
			if strings.EqualFold(d.Address, req.Address) {
				continue
			}
			if d.Connected {
				f.status.ConnectedCount++
			}
			kept = append(kept, d)
		}
		f.status.Devices = kept
		st := f.snapshotLocked()
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(st)
	})

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
}

func (f *fakeBTAgentServer) snapshotLocked() btAgentStatusJSON {
	st := f.status
	if len(f.status.Devices) > 0 {
		st.Devices = append([]btAgentDeviceJSON(nil), f.status.Devices...)
	}
	return st
}

func TestLinuxBluetoothControllerSettingsIntegration(t *testing.T) {
	sock := tempBTSocketPath(t)
	srv := &fakeBTAgentServer{
		status: btAgentStatusJSON{
			AdapterConnected: true,
			AdapterName:      "Realtek RTL8761BU",
			ConnectedCount:   1,
			Devices: []btAgentDeviceJSON{{
				Address:   "AA:BB:CC:DD:EE:01",
				Name:      "DualSense Wireless Controller",
				Kind:      "PlayStation",
				Connected: true,
			}},
		},
	}
	srv.serve(t, sock)

	app := pairedKitSettingsApp(t)
	bt := newLinuxBluetoothController(t.Context(), linuxBluetoothConfig{
		SocketPath:   sock,
		PollInterval: 50 * time.Millisecond,
		CallTimeout:  time.Second,
	})
	defer bt.Close()
	if err := app.SetBluetooth(bt); err != nil {
		t.Fatalf("SetBluetooth: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if st := bt.BluetoothStatus(); st.DongleConnected && len(st.Devices) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	if got := settingsRowByID(t, app, settingsSectionRowPrefix+settingsSectionBluetooth).Value; got != "1 connected" {
		t.Fatalf("expected Settings › Bluetooth summary \"1 connected\", got %q", got)
	}
	focusSettingsRow(t, app, "bt-pair", now)
	if got := settingsRowByID(t, app, "bt-adapter").Value; got != "Realtek RTL8761BU" {
		t.Fatalf("adapter value = %q", got)
	}

	// Two-press confirmation starts pairing, next press stops it.
	app.HandleCommand(CmdSelect, now)
	app.HandleCommand(CmdSelect, now)
	if !bt.BluetoothStatus().PairingActive {
		t.Fatal("expected PairingActive after two-press confirm")
	}
	app.HandleCommand(CmdSelect, now)
	if bt.BluetoothStatus().PairingActive {
		t.Fatal("expected PairingActive stopped after third press")
	}

	// Two-press confirmation forgets paired device.
	focusSettingsRow(t, app, "bt-device-AA:BB:CC:DD:EE:01", now)
	app.HandleCommand(CmdSelect, now)
	app.HandleCommand(CmdSelect, now)
	if len(bt.BluetoothStatus().Devices) != 0 {
		t.Fatalf("expected paired device forgotten, got %+v", bt.BluetoothStatus().Devices)
	}

	srv.mu.Lock()
	gotCalls := strings.Join(srv.calls, ",")
	srv.mu.Unlock()
	if gotCalls != "start,stop,forget:AA:BB:CC:DD:EE:01" {
		t.Fatalf("unexpected IPC call sequence: %q", gotCalls)
	}
}

func TestStartLinuxBluetoothRespectsSmokeAndSocketPresence(t *testing.T) {
	t.Setenv("FES_BLUETOOTH_SOCKET", filepath.Join(t.TempDir(), "missing.sock"))
	if bt := startLinuxBluetooth(t.Context(), Options{KitHost: true, Smoke: true}); bt != nil {
		bt.Close()
		t.Fatal("expected nil Bluetooth controller during Smoke")
	}
	if bt := startLinuxBluetooth(t.Context(), Options{KitHost: false}); bt != nil {
		bt.Close()
		t.Fatal("expected nil Bluetooth controller on non-KitHost without socket")
	}
	if bt := startLinuxBluetooth(t.Context(), Options{KitHost: true}); bt == nil {
		t.Fatal("expected KitHost to attach Bluetooth controller over configured socket")
	} else {
		bt.Close()
	}
}
