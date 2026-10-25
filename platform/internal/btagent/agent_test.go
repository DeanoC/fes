package btagent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestRunPairsGamepadRejectsPhoneAndRestoresAfterBlueZRestart(t *testing.T) {
	client, server := net.Pipe()
	errc := make(chan error, 1)
	go func() { errc <- fakeBlueZ(server) }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runErr := Run(ctx, client)
	peerErr := <-errc
	if peerErr != nil {
		t.Fatal(peerErr)
	}
	if runErr == nil {
		t.Fatal("expected the closed bus to end the agent")
	}
}

func TestAdapterNeedsSetupOnlyForUndesiredProperties(t *testing.T) {
	for name, props := range map[string]map[string]any{
		"desired powered":      {"Powered": true},
		"desired pairable":     {"Pairable": false},
		"desired discoverable": {"Discoverable": false},
		"discovering":          {"Discovering": true},
		"unrelated property":   {"Alias": "Controller adapter"},
		"powered off":          {"Powered": false},
		"pairable":             {"Pairable": true},
		"discoverable":         {"Discoverable": true},
		"discovery stopped":    {"Discovering": false},
	} {
		want := name == "powered off" || name == "pairable" || name == "discoverable" || name == "discovery stopped"
		if got := adapterNeedsSetup(props); got != want {
			t.Errorf("%s: adapterNeedsSetup() = %v, want %v", name, got, want)
		}
	}
}

func TestBlueZOwnerChangeIsIdempotent(t *testing.T) {
	a := &agent{
		bluezOwner:         ":1.2",
		registeredOwner:    ":1.2",
		adapters:           map[string]struct{}{"/org/bluez/hci0": {}},
		configuredAdapters: map[string]struct{}{"/org/bluez/hci0": {}},
	}
	msg := &Message{Member: "NameOwnerChanged", Body: bodyNameOwnerChanged(bluezDest, ":1.1", ":1.2")}
	if err := a.onSignal(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if a.bluezOwner != ":1.2" || a.registeredOwner != ":1.2" {
		t.Fatalf("duplicate owner signal changed registration state: owner %q registered %q", a.bluezOwner, a.registeredOwner)
	}
	if _, ok := a.configuredAdapters["/org/bluez/hci0"]; !ok {
		t.Fatal("duplicate owner signal cleared adapter setup")
	}
}

func TestAuthorizeServiceDuringPairedControllerConnect(t *testing.T) {
	path := gamepadPath
	if decide(devInfo{Paired: true, Class: 0x508}) != actionConnect {
		t.Fatal("paired controller did not select connect")
	}
	attempts := newAttempts()
	if !attempts.Start(path, time.Unix(1_700_000_000, 0), actionConnect) {
		t.Fatal("paired controller connect did not start")
	}
	a := &agent{attempts: attempts}
	for _, uuid := range []string{hidUUID, hogUUID} {
		_, _, errName := a.dispatch(&Message{Member: "AuthorizeService", Body: bodyPathString(path, uuid)})
		if errName != "" {
			t.Errorf("AuthorizeService rejected %s during connect: %s", uuid, errName)
		}
	}
	if _, _, errName := a.dispatch(&Message{Member: "AuthorizeService", Body: bodyPathString(path, "0000110b-0000-1000-8000-00805f9b34fb")}); errName == "" {
		t.Fatal("audio service authorized during connect")
	}
	for _, callback := range []struct {
		member string
		body   []byte
	}{
		{"RequestPinCode", bodyPath(path)},
		{"RequestPasskey", bodyPath(path)},
		{"RequestConfirmation", bodyConfirm(path)},
		{"RequestAuthorization", bodyPath(path)},
	} {
		if _, _, errName := a.dispatch(&Message{Member: callback.member, Body: callback.body}); errName == "" {
			t.Errorf("%s accepted during connect", callback.member)
		}
	}
}

func fakeBlueZ(conn net.Conn) error {
	defer conn.Close()
	r := bufio.NewReader(conn)
	line, err := r.ReadString('\n')
	if err != nil {
		return err
	}
	if !strings.Contains(line, "AUTH EXTERNAL") {
		return errors.New("missing EXTERNAL auth")
	}
	if _, err := conn.Write([]byte("OK 00112233445566778899aabbccddeeff\r\n")); err != nil {
		return err
	}
	begin, err := r.ReadString('\n')
	if err != nil {
		return err
	}
	if begin != "BEGIN\r\n" {
		return errors.New("missing BEGIN")
	}
	var serial uint32
	var matches []string
	sawPairable := false
	sawDiscoverable := false
	var registerCount, startDiscoveryCount int
	var poweredSets, pairableSets, discoverableSets int
	expectedMatches := []string{
		"type='signal',sender='org.bluez',path='/',interface='org.freedesktop.DBus.ObjectManager',member='InterfacesAdded'",
		"type='signal',sender='org.bluez',path='/',interface='org.freedesktop.DBus.ObjectManager',member='InterfacesRemoved'",
		"type='signal',sender='org.bluez',path_namespace='/org/bluez',interface='org.freedesktop.DBus.Properties',member='PropertiesChanged'",
		"type='signal',sender='org.freedesktop.DBus',path='/org/freedesktop/DBus',interface='org.freedesktop.DBus',member='NameOwnerChanged',arg0='org.bluez'",
	}
	next := func() uint32 {
		serial++
		return serial
	}
	callAgent := func(member, sig string, body []byte) (*Message, error) {
		msg := &Message{Type: msgMethodCall, Serial: next(), Path: agentPath, Iface: agentIface, Member: member, Signature: sig}
		if err := writeMessage(conn, msg, body); err != nil {
			return nil, err
		}
		return readMessage(r)
	}
	for {
		msg, err := readMessage(r)
		if err != nil {
			return err
		}
		if msg.Type != msgMethodCall {
			return errors.New("expected a method call")
		}
		switch msg.Member {
		case "Hello":
			body := &encoder{}
			body.str(":1.1")
			if err := reply(conn, next(), msg.Serial, "s", body.buf); err != nil {
				return err
			}
		case "AddMatch":
			if msg.Dest != dbusDest || msg.Path != dbusPath || msg.Iface != dbusIface || msg.Signature != "s" {
				return errors.New("invalid AddMatch call")
			}
			d := &decoder{b: msg.Body}
			rule, rest, err := d.readComplete("s")
			if err != nil || rest != "" {
				return errors.New("invalid AddMatch rule")
			}
			if len(matches) >= len(expectedMatches) || rule != expectedMatches[len(matches)] {
				return fmt.Errorf("unexpected AddMatch rule %v", rule)
			}
			matches = append(matches, rule.(string))
			if err := reply(conn, next(), msg.Serial, "", nil); err != nil {
				return err
			}
		case "GetNameOwner":
			d := &decoder{b: msg.Body}
			name, rest, err := d.readComplete("s")
			if err != nil || rest != "" || name != bluezDest {
				return errors.New("GetNameOwner did not query org.bluez")
			}
			if err := reply(conn, next(), msg.Serial, "s", bodyString(":1.1")); err != nil {
				return err
			}
		case "RegisterAgent":
			if len(matches) != 4 {
				return errors.New("signal matches were not installed before agent registration")
			}
			d := &decoder{b: msg.Body}
			vals, err := d.readSig("os")
			if err != nil || len(vals) != 2 || vals[0] != agentPath || vals[1] != agentCap {
				return errors.New("agent was not registered as KeyboardDisplay")
			}
			if err := reply(conn, next(), msg.Serial, "", nil); err != nil {
				return err
			}
			registerCount++
		case "RequestDefaultAgent":
			if err := reply(conn, next(), msg.Serial, "", nil); err != nil {
				return err
			}
		case "Set":
			d := &decoder{b: msg.Body}
			values, err := d.readSig("ssv")
			if err != nil || len(values) != 3 {
				return fmt.Errorf("invalid adapter property Set: signature %q values %v error %v", msg.Signature, values, err)
			}
			switch values[0] {
			case adapterIface:
				switch values[1] {
				case "Powered":
					poweredSets++
					if values[2] != true {
						return errors.New("adapter is not powered")
					}
				case "Pairable":
					pairableSets++
					sawPairable = true
					if values[2] != false {
						return errors.New("adapter remains pairable")
					}
				case "Discoverable":
					discoverableSets++
					sawDiscoverable = true
					if values[2] != false {
						return errors.New("adapter remains discoverable")
					}
				}
			case deviceIface:
				if values[1] == "Trusted" && values[2] != true {
					return errors.New("controller was not trusted")
				}
			default:
				return fmt.Errorf("unexpected property Set interface %v", values[0])
			}
			if err := reply(conn, next(), msg.Serial, "", nil); err != nil {
				return err
			}
		case "StartDiscovery":
			if len(matches) != 4 {
				return errors.New("discovery started before signal matches")
			}
			if !sawPairable || !sawDiscoverable {
				return errors.New("adapter pairing or discoverability was not disabled")
			}
			if err := reply(conn, next(), msg.Serial, "", nil); err != nil {
				return err
			}
			startDiscoveryCount++
			if startDiscoveryCount == 2 {
				if registerCount != 2 || poweredSets != 2 || pairableSets != 2 || discoverableSets != 2 {
					return fmt.Errorf("BlueZ restart setup counts: register=%d powered=%d pairable=%d discoverable=%d", registerCount, poweredSets, pairableSets, discoverableSets)
				}
				if err := writeMessage(conn, &Message{Type: msgSignal, Serial: next(), Path: dbusPath, Iface: dbusIface, Member: "NameOwnerChanged", Signature: "sss"}, bodyNameOwnerChanged(bluezDest, ":1.1", ":1.2")); err != nil {
					return err
				}
				return nil
			}
			signal := &Message{
				Type:      msgSignal,
				Serial:    next(),
				Path:      "/",
				Iface:     objectsIface,
				Member:    "InterfacesAdded",
				Signature: "oa{sa{sv}}",
			}
			if err := writeMessage(conn, signal, encodeDeviceAdded(gamepadPath)); err != nil {
				return err
			}
		case "GetManagedObjects":
			body := sampleObjects()
			if err := reply(conn, next(), msg.Serial, "a{oa{sa{sv}}}", body); err != nil {
				return err
			}
		case "Pair":
			if msg.Path != gamepadPath {
				return errors.New("paired " + msg.Path)
			}
			pin, err := callAgent("RequestPinCode", "o", bodyPath(gamepadPath))
			if err != nil {
				return err
			}
			if pin.Type != msgMethodReturn {
				return errors.New("pin rejected")
			}
			d := &decoder{b: pin.Body}
			got, _, err := d.readComplete("s")
			if err != nil || got != "0000" {
				return errors.New("pin is not 0000")
			}
			phone, err := callAgent("RequestConfirmation", "ou", bodyConfirm(phonePath))
			if err != nil {
				return err
			}
			if phone.Type != msgError {
				return errors.New("phone confirmation accepted")
			}
			ok, err := callAgent("RequestConfirmation", "ou", bodyConfirm(gamepadPath))
			if err != nil {
				return err
			}
			if ok.Type != msgMethodReturn {
				return errors.New("gamepad confirmation rejected")
			}
			audio, err := callAgent("AuthorizeService", "os", bodyPathString(gamepadPath, "0000110b-0000-1000-8000-00805f9b34fb"))
			if err != nil {
				return err
			}
			if audio.Type != msgError {
				return errors.New("audio service authorized")
			}
			hid, err := callAgent("AuthorizeService", "os", bodyPathString(gamepadPath, hidUUID))
			if err != nil {
				return err
			}
			if hid.Type != msgMethodReturn {
				return errors.New("HID service rejected")
			}
			if err := reply(conn, next(), msg.Serial, "", nil); err != nil {
				return err
			}
		case "Connect":
			if msg.Path != gamepadPath {
				return errors.New("connected " + msg.Path)
			}
			if err := reply(conn, next(), msg.Serial, "", nil); err != nil {
				return err
			}
			if err := writeMessage(conn, &Message{Type: msgSignal, Serial: next(), Path: dbusPath, Iface: dbusIface, Member: "NameOwnerChanged", Signature: "sss"}, bodyNameOwnerChanged(bluezDest, ":1.1", ":1.2")); err != nil {
				return err
			}
		default:
			return errors.New("unexpected method " + msg.Member)
		}
	}
}

func reply(conn net.Conn, serial, to uint32, sig string, body []byte) error {
	msg := &Message{Type: msgMethodReturn, Serial: serial, ReplySerial: to, Signature: sig}
	return writeMessage(conn, msg, body)
}

func bodyConfirm(path string) []byte {
	e := &encoder{}
	e.str(path)
	e.u32(0)
	return e.buf
}

func bodyNameOwnerChanged(name, oldOwner, newOwner string) []byte {
	e := &encoder{}
	e.str(name)
	e.str(oldOwner)
	e.str(newOwner)
	return e.buf
}

const (
	gamepadPath = "/org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF"
	phonePath   = "/org/bluez/hci0/dev_11_22_33_44_55_66"
)

func sampleObjects() []byte {
	return encodeObjects([]fixtureObj{
		{path: "/org/bluez/hci0", ifaces: []fixtureIface{{name: adapterIface}}},
	})
}

func encodeDeviceAdded(path string) []byte {
	e := &encoder{}
	e.str(path)
	e.array(8, func() {
		e.pad(8)
		e.str(deviceIface)
		e.array(8, func() {
			props := []fixtureProp{
				propString("Name", "Pad"),
				propUint("Class", 0x508),
				propBool("Paired", false),
				propBool("Connected", false),
				propStrings("UUIDs", hidUUID),
			}
			for _, prop := range props {
				e.pad(8)
				e.str(prop.name)
				e.sig(prop.sig)
				prop.write(e)
			}
		})
	})
	return e.buf
}
