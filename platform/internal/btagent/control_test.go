package btagent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func httpUnixClient(sock string) (*http.Client, func()) {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		},
	}
	return &http.Client{Transport: tr, Timeout: 3 * time.Second}, tr.CloseIdleConnections
}

func TestControlIPCPairingWindowExpiryAndForgetSuppression(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	sock := tempSocketPath(t)
	ctrl, err := ListenControl(sock)
	if err != nil {
		t.Fatalf("ListenControl: %v", err)
	}
	defer ctrl.Close()

	var (
		mu             sync.Mutex
		startCalls     int
		stopCalls      int
		pairCalls      int
		removeCalls    int
		reAdded        bool
		bonded         = true
		pairedDeviceUp = true
	)

	go func() {
		defer serverConn.Close()
		r := bufio.NewReader(serverConn)
		line, err := r.ReadString('\n')
		if err != nil || !strings.Contains(line, "AUTH EXTERNAL") {
			return
		}
		_, _ = serverConn.Write([]byte("OK 00112233\r\n"))
		begin, err := r.ReadString('\n')
		if err != nil || begin != "BEGIN\r\n" {
			return
		}
		var serial uint32
		next := func() uint32 { serial++; return serial }
		sendSignal := func(member, path, sig string, body []byte) {
			_ = writeMessage(serverConn, &Message{Type: msgSignal, Serial: next(), Path: path, Iface: objectsIface, Member: member, Signature: sig}, body)
		}
		for {
			msg, err := readMessage(r)
			if err != nil {
				return
			}
			if msg.Type != msgMethodCall {
				continue
			}
			switch msg.Member {
			case "Hello":
				_ = reply(serverConn, next(), msg.Serial, "s", bodyString(":1.1"))
			case "AddMatch":
				_ = reply(serverConn, next(), msg.Serial, "", nil)
			case "GetNameOwner":
				_ = reply(serverConn, next(), msg.Serial, "s", bodyString(":1.1"))
			case "RegisterAgent", "RequestDefaultAgent", "Set":
				_ = reply(serverConn, next(), msg.Serial, "", nil)
			case "GetManagedObjects":
				objs := []fixtureObj{
					{
						path: "/org/bluez/hci0",
						ifaces: []fixtureIface{{
							name:  adapterIface,
							props: []fixtureProp{propString("Alias", "Kit BT Adapter"), propBool("Powered", true)},
						}},
					},
				}
				mu.Lock()
				hasPad := pairedDeviceUp
				isBonded := bonded
				mu.Unlock()
				if hasPad {
					objs = append(objs, fixtureObj{
						path: gamepadPath,
						ifaces: []fixtureIface{{
							name: deviceIface,
							props: []fixtureProp{
								propString("Address", "AA:BB:CC:DD:EE:FF"),
								propString("Alias", "DualSense Wireless Controller"),
								propString("Modalias", "bluetooth:v054Cp0CE6d0100"),
								propUint("Class", 0x508),
								propBool("Paired", isBonded),
								propBool("Connected", false),
								propStrings("UUIDs", hidUUID),
							},
						}},
					})
				}
				_ = reply(serverConn, next(), msg.Serial, "a{oa{sa{sv}}}", encodeObjects(objs))
			case "Connect":
				_ = reply(serverConn, next(), msg.Serial, "", nil)
				sendSignal(
					"PropertiesChanged", gamepadPath, "sa{sv}as",
					func() []byte {
						e := &encoder{}
						e.str(deviceIface)
						e.array(8, func() {
							e.pad(8)
							e.str("Connected")
							e.sig("b")
							e.boolean(true)
						})
						e.array(4, func() {})
						return e.buf
					}(),
				)
			case "StartDiscovery":
				mu.Lock()
				startCalls++
				mu.Unlock()
				_ = reply(serverConn, next(), msg.Serial, "", nil)
			case "StopDiscovery":
				mu.Lock()
				stopCalls++
				mu.Unlock()
				_ = reply(serverConn, next(), msg.Serial, "", nil)
			case "RemoveDevice":
				mu.Lock()
				removeCalls++
				bonded = false
				pairedDeviceUp = false
				shouldReAdd := !reAdded
				reAdded = true
				mu.Unlock()
				_ = reply(serverConn, next(), msg.Serial, "", nil)
				removed := &encoder{}
				removed.str(gamepadPath)
				removed.array(4, func() { removed.str(deviceIface) })
				sendSignal("InterfacesRemoved", "/", "oas", removed.buf)
				if shouldReAdd {
					// Simulate forgotten pad still advertising as unpaired while discovered.
					e := &encoder{}
					e.str(gamepadPath)
					e.array(8, func() {
						e.pad(8)
						e.str(deviceIface)
						e.array(8, func() {
							for _, prop := range []fixtureProp{
								propString("Address", "AA:BB:CC:DD:EE:FF"),
								propString("Alias", "DualSense Wireless Controller"),
								propString("Modalias", "bluetooth:v054Cp0CE6d0100"),
								propUint("Class", 0x508),
								propBool("Paired", false),
								propBool("Connected", false),
								propStrings("UUIDs", hidUUID),
							} {
								e.pad(8)
								e.str(prop.name)
								e.sig(prop.sig)
								prop.write(e)
							}
						})
					})
					sendSignal("InterfacesAdded", "/", "oa{sa{sv}}", e.buf)
				}
			case "Pair":
				mu.Lock()
				pairCalls++
				bonded = true
				pairedDeviceUp = true
				mu.Unlock()
				_ = reply(serverConn, next(), msg.Serial, "", nil)
			default:
				_ = replyError(serverConn, next(), msg.Serial, "org.freedesktop.DBus.Error.UnknownMethod", msg.Member)
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- RunWithControl(ctx, clientConn, ctrl)
	}()

	hc, closeHC := httpUnixClient(sock)
	defer closeHC()

	getStatus := func() Status {
		resp, err := hc.Get("http://fes-bluetooth/status")
		if err != nil {
			t.Fatalf("GET /status: %v", err)
		}
		defer resp.Body.Close()
		var st Status
		if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
			t.Fatalf("decode status: %v", err)
		}
		return st
	}

	// Wait until idle adapter + auto-reconnected paired device appear in /status.
	var st Status
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		st = getStatus()
		if st.AdapterConnected && st.ConnectedCount == 1 && len(st.Devices) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !st.AdapterConnected || st.AdapterName != "Kit BT Adapter" || st.PairingActive || len(st.Devices) != 1 || st.Devices[0].Kind != "PlayStation" {
		t.Fatalf("unexpected initial status: %+v", st)
	}
	mu.Lock()
	if startCalls != 0 {
		mu.Unlock()
		t.Fatalf("expected 0 StartDiscovery calls while idle, got %d", startCalls)
	}
	mu.Unlock()

	// Start a pairing window while forgotten/unpaired re-add can happen cleanly.
	resp, err := hc.Post("http://fes-bluetooth/pairing/start", "application/json", strings.NewReader(`{"duration_ms":5000}`))
	if err != nil {
		t.Fatalf("POST /pairing/start: %v", err)
	}
	_ = resp.Body.Close()

	// Wait until StartDiscovery called.
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		mu.Lock()
		sc := startCalls
		mu.Unlock()
		if sc >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Forget paired controller during the active window -> RemoveDevice runs and re-appeared
	// unpaired InterfacesAdded must NOT trigger Pair during the current window.
	forgetResp, err := hc.Post("http://fes-bluetooth/devices/forget", "application/json", bytes.NewBufferString(`{"address":"AA:BB:CC:DD:EE:FF"}`))
	if err != nil {
		t.Fatalf("POST /devices/forget: %v", err)
	}
	var afterForget Status
	if err := json.NewDecoder(forgetResp.Body).Decode(&afterForget); err != nil {
		t.Fatalf("decode forget response: %v", err)
	}
	_ = forgetResp.Body.Close()
	if forgetResp.StatusCode != http.StatusOK || len(afterForget.Devices) != 0 {
		t.Fatalf("expected device removed in forget response, code=%d status=%+v", forgetResp.StatusCode, afterForget)
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if removeCalls != 1 || pairCalls != 0 {
		mu.Unlock()
		t.Fatalf("expected forgotten device not to re-pair in same window: remove=%d pair=%d", removeCalls, pairCalls)
	}
	mu.Unlock()

	// Explicitly starting a fresh pairing window clears suppression and pairs the controller again.
	resp2, err := hc.Post("http://fes-bluetooth/pairing/start", "application/json", strings.NewReader(`{"duration_ms":1200}`))
	if err != nil {
		t.Fatalf("second POST /pairing/start: %v", err)
	}
	_ = resp2.Body.Close()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		mu.Lock()
		pc := pairCalls
		sc := stopCalls
		mu.Unlock()
		if pc == 1 && sc >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	if pairCalls != 1 {
		mu.Unlock()
		t.Fatalf("expected explicit fresh StartPairing to pair forgotten controller, pairCalls=%d", pairCalls)
	}
	if stopCalls == 0 {
		mu.Unlock()
		t.Fatal("expected pairing window expiry to call StopDiscovery")
	}
	mu.Unlock()
	if st := getStatus(); st.PairingActive || len(st.Devices) != 1 {
		t.Fatalf("expected expired pairing window and re-paired device, got %+v", st)
	}

	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("RunWithControl exit err: %v", fmt.Errorf("%w", err))
	}
}
