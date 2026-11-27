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

func TestControlIPCStopPairingCancelsInFlightPair(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	sock := tempSocketPath(t)
	ctrl, err := ListenControl(sock)
	if err != nil {
		t.Fatalf("ListenControl: %v", err)
	}
	defer ctrl.Close()

	pairStarted := make(chan struct{})
	cancelCalled := make(chan struct{})
	pairableRestored := make(chan struct{})
	stopDiscoveryCalled := make(chan struct{})

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
		var (
			serial     uint32
			pairSerial uint32
			pairSeen   bool
			onceCancel sync.Once
			onceStop   sync.Once
			onceRest   sync.Once
		)
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
			case "RegisterAgent", "RequestDefaultAgent":
				_ = reply(serverConn, next(), msg.Serial, "", nil)
			case "Set":
				d := &decoder{b: msg.Body}
				vals, decErr := d.readSig("ssv")
				_ = reply(serverConn, next(), msg.Serial, "", nil)
				if decErr == nil && len(vals) == 3 && msg.Path == "/org/bluez/hci0" && vals[0] == adapterIface && vals[1] == "Pairable" {
					if pairable, ok := vals[2].(bool); ok && pairSeen && !pairable {
						onceRest.Do(func() { close(pairableRestored) })
					}
				}
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
				_ = reply(serverConn, next(), msg.Serial, "a{oa{sa{sv}}}", encodeObjects(objs))
			case "StartDiscovery":
				_ = reply(serverConn, next(), msg.Serial, "", nil)
				e := &encoder{}
				e.str(gamepadPath)
				e.array(8, func() {
					e.pad(8)
					e.str(deviceIface)
					e.array(8, func() {
						for _, prop := range []fixtureProp{
							propString("Address", "11:22:33:44:55:66"),
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
			case "StopDiscovery":
				onceStop.Do(func() { close(stopDiscoveryCalled) })
				_ = reply(serverConn, next(), msg.Serial, "", nil)
			case "Pair":
				// Hold the Pair call open to simulate a sleeping/unresponsive controller
				// so POST /pairing/stop must unblock without waiting for pairCallTimeout.
				pairSerial = msg.Serial
				pairSeen = true
				close(pairStarted)
			case "CancelPairing":
				onceCancel.Do(func() { close(cancelCalled) })
				_ = reply(serverConn, next(), msg.Serial, "", nil)
				if pairSerial != 0 {
					_ = replyError(serverConn, next(), pairSerial, "org.bluez.Error.Failed", "Cancelled")
					pairSerial = 0
				}
			default:
				_ = reply(serverConn, next(), msg.Serial, "", nil)
			}
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- RunWithControl(ctx, clientConn, ctrl)
	}()

	hc, closeIdle := httpUnixClient(sock)
	defer closeIdle()

	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		resp, err := hc.Get("http://fes-bluetooth/status")
		if err == nil {
			var st Status
			_ = json.NewDecoder(resp.Body).Decode(&st)
			_ = resp.Body.Close()
			if st.AdapterConnected {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	resp, err := hc.Post("http://fes-bluetooth/pairing/start", "application/json", strings.NewReader(`{"duration_ms":10000}`))
	if err != nil {
		t.Fatalf("POST /pairing/start: %v", err)
	}
	_ = resp.Body.Close()

	select {
	case <-pairStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for in-flight Pair call")
	}

	startStop := time.Now()
	stopResp, err := hc.Post("http://fes-bluetooth/pairing/stop", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /pairing/stop while Pair in flight: %v", err)
	}
	var stopped Status
	if err := json.NewDecoder(stopResp.Body).Decode(&stopped); err != nil {
		t.Fatalf("decode stop response: %v", err)
	}
	_ = stopResp.Body.Close()
	if time.Since(startStop) > time.Second {
		t.Fatalf("POST /pairing/stop blocked for %s waiting on Pair", time.Since(startStop))
	}
	if stopResp.StatusCode != http.StatusOK || stopped.PairingActive {
		t.Fatalf("unexpected stop response: code=%d status=%+v", stopResp.StatusCode, stopped)
	}

	select {
	case <-cancelCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("expected CancelPairing after stopPairing cancelled Pair")
	}
	select {
	case <-pairableRestored:
	case <-time.After(2 * time.Second):
		t.Fatal("expected adapter Pairable=false to be restored")
	}
	select {
	case <-stopDiscoveryCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("expected StopDiscovery on stopPairing")
	}

	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("RunWithControl exit err: %v", fmt.Errorf("%w", err))
	}
}

func TestControlIPCForgetPreservesDeviceWhenRemoveDeviceFails(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	sock := tempSocketPath(t)
	ctrl, err := ListenControl(sock)
	if err != nil {
		t.Fatalf("ListenControl: %v", err)
	}
	defer ctrl.Close()

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
			case "RegisterAgent", "RequestDefaultAgent", "Set", "Connect":
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
					{
						path: gamepadPath,
						ifaces: []fixtureIface{{
							name: deviceIface,
							props: []fixtureProp{
								propString("Address", "AA:BB:CC:DD:EE:FF"),
								propString("Alias", "DualSense Wireless Controller"),
								propString("Modalias", "bluetooth:v054Cp0CE6d0100"),
								propUint("Class", 0x508),
								propBool("Paired", true),
								propBool("Connected", true),
								propStrings("UUIDs", hidUUID),
							},
						}},
					},
				}
				_ = reply(serverConn, next(), msg.Serial, "a{oa{sa{sv}}}", encodeObjects(objs))
			case "RemoveDevice":
				_ = replyError(serverConn, next(), msg.Serial, "org.bluez.Error.Failed", "busy")
			default:
				_ = reply(serverConn, next(), msg.Serial, "", nil)
			}
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- RunWithControl(ctx, clientConn, ctrl)
	}()

	hc, closeIdle := httpUnixClient(sock)
	defer closeIdle()

	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		resp, err := hc.Get("http://fes-bluetooth/status")
		if err == nil {
			var st Status
			_ = json.NewDecoder(resp.Body).Decode(&st)
			_ = resp.Body.Close()
			if len(st.Devices) == 1 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	forgetResp, err := hc.Post("http://fes-bluetooth/devices/forget", "application/json", bytes.NewBufferString(`{"address":"AA:BB:CC:DD:EE:FF"}`))
	if err != nil {
		t.Fatalf("POST /devices/forget: %v", err)
	}
	_ = forgetResp.Body.Close()
	if forgetResp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("forget status code = %d, want 500", forgetResp.StatusCode)
	}

	statusResp, err := hc.Get("http://fes-bluetooth/status")
	if err != nil {
		t.Fatalf("GET /status after failed forget: %v", err)
	}
	var st Status
	if err := json.NewDecoder(statusResp.Body).Decode(&st); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	_ = statusResp.Body.Close()
	if len(st.Devices) != 1 || st.Devices[0].Address != "AA:BB:CC:DD:EE:FF" {
		t.Fatalf("expected failed RemoveDevice to preserve device, got %+v", st.Devices)
	}

	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("RunWithControl exit err: %v", fmt.Errorf("%w", err))
	}
}
