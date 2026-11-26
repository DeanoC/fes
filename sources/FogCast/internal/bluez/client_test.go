// SPDX-License-Identifier: GPL-3.0-or-later

package bluez

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
)

type fakeBlueZServer struct {
	t      *testing.T
	client net.Conn
	server net.Conn

	mu          sync.Mutex
	owner       string
	adapters    map[string]adapterProps
	devices     map[string]deviceProps
	calls       []fakeDBusCall
	inProgress  bool
	removeErr   string
	signalSeq   uint32
	stoppedOnce sync.Once
}

type fakeDBusCall struct {
	Path   string
	Iface  string
	Member string
	Arg0   string
	Arg1   string
	Bool   bool
}

func newFakeBlueZServer(t *testing.T) *fakeBlueZServer {
	t.Helper()
	client, server := net.Pipe()
	s := &fakeBlueZServer{
		t:      t,
		client: client,
		server: server,
		owner:  ":1.42",
		adapters: map[string]adapterProps{
			"/org/bluez/hci0": {
				Path:    "/org/bluez/hci0",
				Name:    "Realtek RTL8761BU",
				Powered: true,
			},
		},
		devices: map[string]deviceProps{},
	}
	go s.serve()
	t.Cleanup(s.close)
	return s
}

func (s *fakeBlueZServer) dial(context.Context) (io.ReadWriteCloser, error) {
	return s.client, nil
}

func (s *fakeBlueZServer) close() {
	s.stoppedOnce.Do(func() {
		_ = s.client.Close()
		_ = s.server.Close()
	})
}

func (s *fakeBlueZServer) serve() {
	r := bufio.NewReader(s.server)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(trimmed, "\x00AUTH") || strings.HasPrefix(trimmed, "AUTH") {
			if _, err := io.WriteString(s.server, "OK 12345678\r\n"); err != nil {
				return
			}
			continue
		}
		if trimmed == "BEGIN" {
			break
		}
	}
	for {
		msg, err := readMessage(r)
		if err != nil {
			return
		}
		s.handleMessage(msg)
	}
}

func (s *fakeBlueZServer) handleMessage(msg *message) {
	reply := &message{Type: msgMethodReturn, ReplySerial: msg.Serial, Dest: ":1.99"}
	var body []byte

	switch msg.Iface + "." + msg.Member {
	case dbusIface + ".Hello":
		reply.Signature = "s"
		body = bodyString(":1.99")
	case dbusIface + ".AddMatch":
		// no-op
	case dbusIface + ".GetNameOwner":
		s.mu.Lock()
		owner := s.owner
		s.mu.Unlock()
		if owner == "" {
			reply.Type = msgError
			reply.ErrorName = "org.freedesktop.DBus.Error.NameHasNoOwner"
			reply.Signature = "s"
			body = bodyString("no owner")
		} else {
			reply.Signature = "s"
			body = bodyString(owner)
		}
	case objectsIface + ".GetManagedObjects":
		reply.Signature = "a{oa{sa{sv}}}"
		s.mu.Lock()
		body = encodeManagedObjects(s.adapters, s.devices)
		s.mu.Unlock()
	case propsIface + ".Set":
		d := &decoder{b: msg.Body}
		vals, err := d.readSig("ssv")
		if err == nil && len(vals) == 3 {
			iface, _ := vals[0].(string)
			prop, _ := vals[1].(string)
			flag, _ := vals[2].(bool)
			s.mu.Lock()
			s.calls = append(s.calls, fakeDBusCall{Path: msg.Path, Iface: iface, Member: "Set", Arg0: prop, Bool: flag})
			if prop == "Powered" {
				if ad, ok := s.adapters[msg.Path]; ok {
					ad.Powered = flag
					s.adapters[msg.Path] = ad
				}
			}
			s.mu.Unlock()
		}
	case adapterIface + ".StartDiscovery":
		s.mu.Lock()
		s.calls = append(s.calls, fakeDBusCall{Path: msg.Path, Iface: msg.Iface, Member: msg.Member})
		inProgress := s.inProgress
		s.mu.Unlock()
		if inProgress {
			reply.Type = msgError
			reply.ErrorName = "org.bluez.Error.InProgress"
			reply.Signature = "s"
			body = bodyString("already discovering")
		}
	case adapterIface + ".StopDiscovery":
		s.mu.Lock()
		s.calls = append(s.calls, fakeDBusCall{Path: msg.Path, Iface: msg.Iface, Member: msg.Member})
		s.mu.Unlock()
	case adapterIface + ".RemoveDevice":
		devPath, _ := decodeObjectArg(msg.Body)
		s.mu.Lock()
		s.calls = append(s.calls, fakeDBusCall{Path: msg.Path, Iface: msg.Iface, Member: msg.Member, Arg0: devPath})
		errName := s.removeErr
		if errName == "" {
			delete(s.devices, devPath)
		}
		s.mu.Unlock()
		if errName != "" {
			reply.Type = msgError
			reply.ErrorName = errName
			reply.Signature = "s"
			body = bodyString("remove failed")
		}
	}

	s.mu.Lock()
	_ = writeMessage(s.server, reply, body)
	s.mu.Unlock()
}

func (s *fakeBlueZServer) sendSignal(objPath, iface, member, sig string, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.signalSeq++
	msg := &message{
		Type:      msgSignal,
		Serial:    1000 + s.signalSeq,
		Path:      objPath,
		Iface:     iface,
		Member:    member,
		Signature: sig,
	}
	_ = writeMessage(s.server, msg, body)
}

func decodeObjectArg(body []byte) (string, error) {
	d := &decoder{b: body}
	v, _, err := d.readComplete("o")
	if err != nil {
		return "", err
	}
	s, _ := v.(string)
	return s, nil
}

func writePropString(e *encoder, name, val string) {
	e.pad(8)
	e.str(name)
	e.sig("s")
	e.str(val)
}

func writePropBool(e *encoder, name string, val bool) {
	e.pad(8)
	e.str(name)
	e.sig("b")
	e.boolean(val)
}

func writePropU32(e *encoder, name string, val uint32) {
	e.pad(8)
	e.str(name)
	e.sig("u")
	e.u32(val)
}

func encodeAdapterDict(e *encoder, ad adapterProps) {
	e.pad(8)
	e.str(adapterIface)
	e.array(8, func() {
		writePropString(e, "Alias", ad.Name)
		writePropBool(e, "Powered", ad.Powered)
		writePropBool(e, "Discovering", ad.Discovering)
	})
}

func encodeDeviceDict(e *encoder, dev deviceProps) {
	e.pad(8)
	e.str(deviceIface)
	e.array(8, func() {
		writePropString(e, "Address", dev.Address)
		writePropString(e, "Adapter", dev.Adapter)
		writePropString(e, "Alias", dev.Name)
		if dev.Modalias != "" {
			writePropString(e, "Modalias", dev.Modalias)
		}
		if dev.Class != 0 {
			writePropU32(e, "Class", dev.Class)
		}
		writePropBool(e, "Paired", dev.Paired)
		writePropBool(e, "Connected", dev.Connected)
	})
}

func encodeManagedObjects(adapters map[string]adapterProps, devices map[string]deviceProps) []byte {
	e := &encoder{}
	e.array(8, func() {
		for p, ad := range adapters {
			e.pad(8)
			e.str(p)
			e.array(8, func() { encodeAdapterDict(e, ad) })
		}
		for p, dev := range devices {
			e.pad(8)
			e.str(p)
			e.array(8, func() { encodeDeviceDict(e, dev) })
		}
	})
	return e.buf
}

func encodeAddedDevice(dev deviceProps) []byte {
	e := &encoder{}
	e.str(dev.Path)
	e.array(8, func() { encodeDeviceDict(e, dev) })
	return e.buf
}

func encodeRemovedIfaces(objPath string, ifaces ...string) []byte {
	e := &encoder{}
	e.str(objPath)
	e.array(4, func() {
		for _, iface := range ifaces {
			e.str(iface)
		}
	})
	return e.buf
}

func encodeChangedBool(iface, prop string, val bool) []byte {
	e := &encoder{}
	e.str(iface)
	e.array(8, func() {
		writePropBool(e, prop, val)
	})
	e.array(4, func() {})
	return e.buf
}

func encodeNameOwnerChanged(name, oldOwner, newOwner string) []byte {
	e := &encoder{}
	e.str(name)
	e.str(oldOwner)
	e.str(newOwner)
	return e.buf
}

func waitUntil(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for condition")
}

func TestClientInitialObjectsSignalsPairingAndForget(t *testing.T) {
	srv := newFakeBlueZServer(t)
	srv.adapters["/org/bluez/hci0"] = adapterProps{
		Path:    "/org/bluez/hci0",
		Name:    "Realtek RTL8761BU",
		Powered: false,
	}
	srv.devices["/org/bluez/hci0/dev_AA_BB_CC_DD_EE_01"] = deviceProps{
		Path:      "/org/bluez/hci0/dev_AA_BB_CC_DD_EE_01",
		Adapter:   "/org/bluez/hci0",
		Address:   "aa:bb:cc:dd:ee:01",
		Name:      "Wireless Controller",
		Modalias:  "bluetooth:v054Cp0CE6d0100",
		Paired:    true,
		Connected: true,
	}
	srv.devices["/org/bluez/hci0/dev_AA_BB_CC_DD_EE_02"] = deviceProps{
		Path:      "/org/bluez/hci0/dev_AA_BB_CC_DD_EE_02",
		Adapter:   "/org/bluez/hci0",
		Address:   "AA:BB:CC:DD:EE:02",
		Name:      "Pro Controller",
		Modalias:  "bluetooth:v057Ep2009d0001",
		Paired:    true,
		Connected: false,
	}
	srv.devices["/org/bluez/hci0/dev_99_88_77_66_55_44"] = deviceProps{
		Path:      "/org/bluez/hci0/dev_99_88_77_66_55_44",
		Adapter:   "/org/bluez/hci0",
		Address:   "99:88:77:66:55:44",
		Name:      "Nearby Phone",
		Class:     uint32(2 << 8),
		Paired:    false,
		Connected: false,
	}

	var nowMu sync.Mutex
	now := time.Unix(1000, 0)
	getNow := func() time.Time {
		nowMu.Lock()
		defer nowMu.Unlock()
		return now
	}
	advance := func(d time.Duration) {
		nowMu.Lock()
		now = now.Add(d)
		nowMu.Unlock()
	}

	c := Start(t.Context(), Config{
		Dial:        srv.dial,
		Now:         getNow,
		CallTimeout: time.Second,
	})
	defer c.Close()

	waitUntil(t, func() bool {
		st := c.Status()
		return st.AdapterConnected && len(st.Devices) == 2
	})

	st := c.Status()
	if st.AdapterName != "Realtek RTL8761BU" || st.ConnectedCount != 1 {
		t.Fatalf("unexpected initial status: %+v", st)
	}
	if st.Devices[0].Address != "AA:BB:CC:DD:EE:01" || st.Devices[0].Kind != "DualSense" || !st.Devices[0].Connected {
		t.Fatalf("unexpected first sorted device: %+v", st.Devices[0])
	}
	if st.Devices[1].Address != "AA:BB:CC:DD:EE:02" || st.Devices[1].Kind != "Switch Pro" || st.Devices[1].Connected {
		t.Fatalf("unexpected second sorted device: %+v", st.Devices[1])
	}

	// StartPairing powers adapter on (never touches Pairable/Discoverable) and starts discovery.
	srv.mu.Lock()
	srv.inProgress = true // StartDiscovery InProgress must be treated as success.
	srv.mu.Unlock()
	if err := c.StartPairing(30 * time.Second); err != nil {
		t.Fatalf("StartPairing: %v", err)
	}
	st = c.Status()
	if !st.PairingActive || st.PairingRemaining != 30*time.Second {
		t.Fatalf("expected 30s pairing active, got %+v", st)
	}
	advance(10 * time.Second)
	if rem := c.Status().PairingRemaining; rem != 20*time.Second {
		t.Fatalf("expected 20s remaining, got %v", rem)
	}
	srv.mu.Lock()
	var sawPower, sawPairable, sawDiscoverable, sawStart bool
	for _, call := range srv.calls {
		if call.Member == "Set" && call.Arg0 == "Powered" && call.Bool {
			sawPower = true
		}
		if call.Member == "Set" && call.Arg0 == "Pairable" {
			sawPairable = true
		}
		if call.Member == "Set" && call.Arg0 == "Discoverable" {
			sawDiscoverable = true
		}
		if call.Member == "StartDiscovery" {
			sawStart = true
		}
	}
	srv.mu.Unlock()
	if !sawPower || !sawStart || sawPairable || sawDiscoverable {
		t.Fatalf("unexpected adapter calls power=%t start=%t pairable=%t discoverable=%t", sawPower, sawStart, sawPairable, sawDiscoverable)
	}

	// StopPairing sends StopDiscovery.
	if err := c.StopPairing(); err != nil {
		t.Fatalf("StopPairing: %v", err)
	}
	if c.Status().PairingActive {
		t.Fatal("expected PairingActive=false after StopPairing")
	}
	srv.mu.Lock()
	lastCall := srv.calls[len(srv.calls)-1]
	srv.mu.Unlock()
	if lastCall.Member != "StopDiscovery" {
		t.Fatalf("expected StopDiscovery, got %+v", lastCall)
	}

	// Live InterfacesAdded + PropertiesChanged update cached state.
	added := deviceProps{
		Path:      "/org/bluez/hci0/dev_11_22_33_44_55_66",
		Adapter:   "/org/bluez/hci0",
		Address:   "11:22:33:44:55:66",
		Name:      "8BitDo Ultimate",
		Modalias:  "bluetooth:v2DC8p3109d0001",
		Paired:    true,
		Connected: false,
	}
	srv.sendSignal("/", objectsIface, "InterfacesAdded", "oa{sa{sv}}", encodeAddedDevice(added))
	waitUntil(t, func() bool { return len(c.Status().Devices) == 3 })
	srv.sendSignal(added.Path, propsIface, "PropertiesChanged", "sa{sv}as", encodeChangedBool(deviceIface, "Connected", true))
	waitUntil(t, func() bool { return c.Status().ConnectedCount == 2 })

	// ForgetDevice sends RemoveDevice and drops from cached list.
	if err := c.ForgetDevice("aa:bb:cc:dd:ee:02"); err != nil {
		t.Fatalf("ForgetDevice: %v", err)
	}
	if len(c.Status().Devices) != 2 {
		t.Fatalf("expected 2 devices after ForgetDevice, got %+v", c.Status().Devices)
	}
	if err := c.ForgetDevice("00:00:00:00:00:00"); err == nil {
		t.Fatal("expected error forgetting unknown address")
	}

	// InterfacesRemoved drops device cleanly.
	srv.sendSignal("/", objectsIface, "InterfacesRemoved", "oas", encodeRemovedIfaces(added.Path, deviceIface))
	waitUntil(t, func() bool { return len(c.Status().Devices) == 1 })

	// BlueZ NameOwnerChanged loss clears adapter and devices cleanly; re-acquire restores.
	srv.sendSignal(dbusPath, dbusIface, "NameOwnerChanged", "sss", encodeNameOwnerChanged(bluezDest, ":1.42", ""))
	waitUntil(t, func() bool { return !c.Status().AdapterConnected && len(c.Status().Devices) == 0 })

	// Quick sanity check on deadline auto-expiry timer: start short window and advance clock?
	// Keep real duration short to verify worker timer fires StopDiscovery automatically.
	srv.sendSignal(dbusPath, dbusIface, "NameOwnerChanged", "sss", encodeNameOwnerChanged(bluezDest, "", ":1.43"))
	waitUntil(t, func() bool { return c.Status().AdapterConnected })

	_ = binary.LittleEndian // keep encoding/binary unused check satisfied if needed
}

func TestClientPairingAutoExpiryAndConcurrency(t *testing.T) {
	srv := newFakeBlueZServer(t)
	c := Start(t.Context(), Config{
		Dial:        srv.dial,
		CallTimeout: time.Second,
	})
	defer c.Close()

	waitUntil(t, func() bool { return c.Status().AdapterConnected })

	if err := c.StartPairing(40 * time.Millisecond); err != nil {
		t.Fatalf("StartPairing: %v", err)
	}
	waitUntil(t, func() bool { return !c.Status().PairingActive })
	waitUntil(t, func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		for _, call := range srv.calls {
			if call.Member == "StopDiscovery" {
				return true
			}
		}
		return false
	})

	// Exercise concurrent Status readers under -race.
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				_ = c.Status()
				time.Sleep(time.Millisecond)
			}
		}()
	}
	wg.Wait()
}
