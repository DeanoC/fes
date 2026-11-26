//go:build linux

package tenfoot

import (
	"bufio"
	"context"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/bluez"
)

func TestLinuxBluetoothControllerSettingsIntegration(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
	})

	var mu sync.Mutex
	var calls []string
	go func() {
		r := bufio.NewReader(serverConn)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			tr := strings.TrimRight(line, "\r\n")
			if strings.HasPrefix(tr, "\x00AUTH") || strings.HasPrefix(tr, "AUTH") {
				_, _ = io.WriteString(serverConn, "OK 01\r\n")
				continue
			}
			if tr == "BEGIN" {
				break
			}
		}
		for {
			var hdr [16]byte
			if _, err := io.ReadFull(r, hdr[:]); err != nil {
				return
			}
			bodyLen := binary.LittleEndian.Uint32(hdr[4:8])
			serial := binary.LittleEndian.Uint32(hdr[8:12])
			fieldsLen := binary.LittleEndian.Uint32(hdr[12:16])
			fields := make([]byte, fieldsLen)
			if _, err := io.ReadFull(r, fields); err != nil {
				return
			}
			if pad := (8 - (16+int(fieldsLen))%8) % 8; pad > 0 {
				var d [8]byte
				if _, err := io.ReadFull(r, d[:pad]); err != nil {
					return
				}
			}
			body := make([]byte, bodyLen)
			if _, err := io.ReadFull(r, body); err != nil {
				return
			}
			fs := string(fields)
			var sig string
			var rbody []byte
			switch {
			case strings.Contains(fs, "Hello"):
				sig, rbody = "s", dbusTestStr(":1.10")
			case strings.Contains(fs, "GetNameOwner"):
				sig, rbody = "s", dbusTestStr(":1.20")
			case strings.Contains(fs, "GetManagedObjects"):
				sig, rbody = "a{oa{sa{sv}}}", dbusTestManagedObjects()
			case strings.Contains(fs, "StartDiscovery"):
				mu.Lock()
				calls = append(calls, "StartDiscovery")
				mu.Unlock()
			case strings.Contains(fs, "StopDiscovery"):
				mu.Lock()
				calls = append(calls, "StopDiscovery")
				mu.Unlock()
			case strings.Contains(fs, "RemoveDevice"):
				mu.Lock()
				calls = append(calls, "RemoveDevice")
				mu.Unlock()
			}
			_, _ = serverConn.Write(encodeTestReply(serial, sig, rbody))
		}
	}()

	var dialOnce sync.Once
	app := pairedKitSettingsApp(t)
	bt := newLinuxBluetoothController(t.Context(), bluez.Config{
		CallTimeout: time.Second,
		Dial: func(context.Context) (io.ReadWriteCloser, error) {
			var conn io.ReadWriteCloser
			dialOnce.Do(func() { conn = clientConn })
			if conn == nil {
				return nil, io.EOF
			}
			return conn, nil
		},
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
	app.HandleCommand(CmdSelect, now)
	app.HandleCommand(CmdSelect, now)
	if !bt.BluetoothStatus().PairingActive {
		t.Fatal("expected PairingActive after two-press confirm")
	}
	app.HandleCommand(CmdSelect, now)
	if bt.BluetoothStatus().PairingActive {
		t.Fatal("expected PairingActive stopped after third press")
	}
	focusSettingsRow(t, app, "bt-device-AA:BB:CC:DD:EE:01", now)
	app.HandleCommand(CmdSelect, now)
	app.HandleCommand(CmdSelect, now)
	if len(bt.BluetoothStatus().Devices) != 0 {
		t.Fatalf("expected paired device forgotten, got %+v", bt.BluetoothStatus().Devices)
	}
	mu.Lock()
	gotCalls := strings.Join(calls, ",")
	mu.Unlock()
	if gotCalls != "StartDiscovery,StopDiscovery,RemoveDevice" {
		t.Fatalf("unexpected D-Bus call sequence: %q", gotCalls)
	}
}

type testEnc struct{ buf []byte }

func (e *testEnc) pad(n int) {
	for len(e.buf)%n != 0 {
		e.buf = append(e.buf, 0)
	}
}
func (e *testEnc) u32(v uint32) {
	e.pad(4)
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	e.buf = append(e.buf, b[:]...)
}
func (e *testEnc) str(s string) {
	e.u32(uint32(len(s)))
	e.buf = append(e.buf, s...)
	e.buf = append(e.buf, 0)
}
func (e *testEnc) sig(s string) {
	e.buf = append(e.buf, byte(len(s)))
	e.buf = append(e.buf, s...)
	e.buf = append(e.buf, 0)
}
func (e *testEnc) boolean(v bool) {
	if v {
		e.u32(1)
	} else {
		e.u32(0)
	}
}
func (e *testEnc) array(align int, fn func()) {
	e.pad(4)
	pos := len(e.buf)
	e.buf = append(e.buf, 0, 0, 0, 0)
	e.pad(align)
	start := len(e.buf)
	fn()
	binary.LittleEndian.PutUint32(e.buf[pos:pos+4], uint32(len(e.buf)-start))
}

func dbusTestStr(s string) []byte {
	var e testEnc
	e.str(s)
	return e.buf
}

func encodeTestReply(replySerial uint32, sig string, body []byte) []byte {
	var e testEnc
	e.buf = append(e.buf, 'l', 2, 0, 1)
	e.u32(uint32(len(body)))
	e.u32(replySerial + 100)
	e.array(8, func() {
		e.pad(8)
		e.buf = append(e.buf, 5)
		e.sig("u")
		e.u32(replySerial)
		if sig != "" {
			e.pad(8)
			e.buf = append(e.buf, 8)
			e.sig("g")
			e.sig(sig)
		}
	})
	e.pad(8)
	e.buf = append(e.buf, body...)
	return e.buf
}

func dbusTestManagedObjects() []byte {
	var e testEnc
	propS := func(k, v string) {
		e.pad(8)
		e.str(k)
		e.sig("s")
		e.str(v)
	}
	propB := func(k string, v bool) {
		e.pad(8)
		e.str(k)
		e.sig("b")
		e.boolean(v)
	}
	e.array(8, func() {
		e.pad(8)
		e.str("/org/bluez/hci0")
		e.array(8, func() {
			e.pad(8)
			e.str("org.bluez.Adapter1")
			e.array(8, func() {
				propS("Alias", "Realtek RTL8761BU")
				propB("Powered", true)
			})
		})
		e.pad(8)
		e.str("/org/bluez/hci0/dev_AA_BB_CC_DD_EE_01")
		e.array(8, func() {
			e.pad(8)
			e.str("org.bluez.Device1")
			e.array(8, func() {
				propS("Address", "AA:BB:CC:DD:EE:01")
				propS("Adapter", "/org/bluez/hci0")
				propS("Alias", "DualSense Wireless Controller")
				propS("Modalias", "bluetooth:v054Cp0CE6d0100")
				propB("Paired", true)
				propB("Connected", true)
			})
		})
	})
	return e.buf
}
