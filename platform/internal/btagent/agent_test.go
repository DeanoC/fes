package btagent

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"testing"
	"time"
)

func TestRunRecoversStaleBondReconnectsWithoutRePairAndRestoresAfterBlueZRestart(t *testing.T) {
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

func writeDeviceProperties(conn net.Conn, serial uint32, devicePath string, properties ...fixtureProp) error {
	e := &encoder{}
	e.str(deviceIface)
	e.array(8, func() {
		for _, prop := range properties {
			e.pad(8)
			e.str(prop.name)
			e.sig(prop.sig)
			prop.write(e)
		}
	})
	e.array(4, func() {})
	return writeMessage(conn, &Message{Type: msgSignal, Serial: serial, Path: devicePath, Iface: propsIface, Member: "PropertiesChanged", Signature: "sa{sv}as"}, e.buf)
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
	pad := devInfo{Path: path, Paired: true, Class: 0x508}
	if decide(pad) != actionConnect {
		t.Fatal("paired controller did not select connect")
	}
	attempts := newAttempts()
	if !attempts.Start(pad, time.Unix(1_700_000_000, 0), actionConnect) {
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
		{"RequestConfirmation", bodyConfirmPasskey(path, 654321)},
		{"DisplayPasskey", bodyDisplayPasskey(path)},
		{"RequestAuthorization", bodyPath(path)},
	} {
		if _, _, errName := a.dispatch(&Message{Member: callback.member, Body: callback.body}); errName == "" {
			t.Errorf("%s accepted during connect", callback.member)
		}
	}
}

func TestPairingCallbacksMatchNoInputNoOutput(t *testing.T) {
	path := gamepadPath
	pad := devInfo{Path: path, Class: 0x508}
	attempts := newAttempts()
	if !attempts.Start(pad, time.Unix(1_700_000_000, 0), actionPair) {
		t.Fatal("gamepad pairing did not start")
	}
	a := &agent{attempts: attempts}
	for _, passkey := range []uint32{0, 654321} {
		if _, _, errName := a.dispatch(&Message{Member: "RequestConfirmation", Body: bodyConfirmPasskey(path, passkey)}); errName != "org.bluez.Error.Rejected" {
			t.Errorf("RequestConfirmation error = %q for passkey %d, want org.bluez.Error.Rejected", errName, passkey)
		}
	}
	if _, _, errName := a.dispatch(&Message{Member: "RequestAuthorization", Body: bodyPath(path)}); errName != "" {
		t.Fatalf("Just Works gamepad authorization rejected during Pair: %s", errName)
	}
	sig, pinBody, errName := a.dispatch(&Message{Member: "RequestPinCode", Body: bodyPath(path)})
	if errName != "" || sig != "s" {
		t.Fatalf("legacy gamepad PIN request: signature=%q error=%q", sig, errName)
	}
	pinDecoder := &decoder{b: pinBody}
	pin, rest, err := pinDecoder.readComplete("s")
	if err != nil || rest != "" || pin != "0000" {
		t.Fatalf("legacy gamepad PIN = %v (rest %q, err %v), want 0000", pin, rest, err)
	}
	for _, callback := range []struct {
		member string
		body   []byte
	}{
		{"RequestPasskey", bodyPath(path)},
		{"DisplayPinCode", bodyPath(path)},
		{"DisplayPasskey", bodyDisplayPasskey(path)},
	} {
		if _, _, errName := a.dispatch(&Message{Member: callback.member, Body: callback.body}); errName == "" {
			t.Errorf("%s accepted by a NoInputNoOutput agent", callback.member)
		}
	}
	for _, dev := range []devInfo{
		{Path: phonePath, Class: 0x200},
		{Path: "/org/bluez/hci0/dev_12_34_56_78_90_AB", UUIDs: []string{hidUUID}, Modalias: "usb:v045Ep0001d0001"},
	} {
		if attempts.Start(dev, time.Unix(1_700_000_000, 0), actionPair) {
			t.Fatalf("non-gamepad %s started pairing", dev.Path)
		}
		if _, _, errName := a.dispatch(&Message{Member: "RequestConfirmation", Body: bodyConfirm(dev.Path)}); errName == "" {
			t.Errorf("RequestConfirmation accepted non-gamepad %s", dev.Path)
		}
		if _, _, errName := a.dispatch(&Message{Member: "RequestAuthorization", Body: bodyPath(dev.Path)}); errName != "org.bluez.Error.Rejected" {
			t.Errorf("RequestAuthorization error = %q for non-gamepad %s, want org.bluez.Error.Rejected", errName, dev.Path)
		}
	}
	attempts.Finish(path, false, time.Unix(1_700_000_001, 0))
	if _, _, errName := a.dispatch(&Message{Member: "RequestAuthorization", Body: bodyPath(path)}); errName != "org.bluez.Error.Rejected" {
		t.Errorf("RequestAuthorization error = %q after Pair finished, want org.bluez.Error.Rejected", errName)
	}
}

func TestRequestConfirmationRejectionIsLogged(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })

	a := &agent{attempts: newAttempts()}
	if _, _, errName := a.dispatch(&Message{Member: "RequestConfirmation", Body: bodyConfirmPasskey(gamepadPath, 654321)}); errName != "org.bluez.Error.Rejected" {
		t.Fatalf("RequestConfirmation error = %q, want org.bluez.Error.Rejected", errName)
	}
	if !strings.Contains(output.String(), "rejecting passkey confirmation") {
		t.Fatalf("RequestConfirmation rejection was not logged: %q", output.String())
	}
}

func TestStaleBondErrorClassificationAndRemovalBound(t *testing.T) {
	for _, err := range []error{
		&dbusError{Name: "org.bluez.Error.Failed", Message: "Key missing"},
		&dbusError{Name: "org.bluez.Error.Failed", Message: "br-connection-key-missing"},
		&dbusError{Name: "org.bluez.Error.AuthenticationFailed", Message: "stored key missing"},
	} {
		if !isStaleBondError(err) {
			t.Errorf("stale bond error not recognized: %v", err)
		}
	}
	for _, err := range []error{
		nil,
		&dbusError{Name: "org.bluez.Error.AuthenticationFailed"},
		&dbusError{Name: "org.bluez.Error.AuthenticationRejected"},
		&dbusError{Name: "org.bluez.Error.Failed", Message: "authentication failed"},
		errors.New("key-missing"),
	} {
		if isStaleBondError(err) {
			t.Errorf("ordinary connection error treated as stale bond: %v", err)
		}
	}

	a := &agent{}
	if !a.claimStaleBondRemoval(gamepadPath) {
		t.Fatal("first stale bond removal was not claimed")
	}
	if a.claimStaleBondRemoval(gamepadPath) {
		t.Fatal("stale bond removal was claimed twice")
	}
	a.releaseStaleBondRemoval(gamepadPath) // failed RemoveDevice can be retried
	if !a.claimStaleBondRemoval(gamepadPath) {
		t.Fatal("failed RemoveDevice did not release its claim")
	}
	a.releaseStaleBondRemoval(gamepadPath)
	if !a.claimStaleBondRemoval(gamepadPath) {
		t.Fatal("second bounded removal retry was not claimed")
	}
	a.releaseStaleBondRemoval(gamepadPath)
	if a.claimStaleBondRemoval(gamepadPath) {
		t.Fatal("stale-bond removal exceeded its cumulative attempt cap")
	}

	freshBond := &agent{}
	if !freshBond.claimStaleBondRemoval(gamepadPath) {
		t.Fatal("fresh-bond test could not claim initial removal")
	}
	freshBond.resetStaleBondRemoval(gamepadPath)
	if !freshBond.claimStaleBondRemoval(gamepadPath) {
		t.Fatal("successful re-pair did not clear the current-bond claim")
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
	adapterPairable := false
	var registerCount, startDiscoveryCount int
	var poweredSets, pairableSets, discoverableSets int
	var connectCount, removeDeviceCount, pairCount int
	var bonded, devicePresent bool
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
				return errors.New("agent was not registered as NoInputNoOutput")
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
					value, ok := values[2].(bool)
					if !ok {
						return errors.New("adapter Pairable value is not boolean")
					}
					adapterPairable = value
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
			if adapterPairable {
				return errors.New("adapter remained pairable while idle")
			}
			if err := reply(conn, next(), msg.Serial, "", nil); err != nil {
				return err
			}
			startDiscoveryCount++
			if startDiscoveryCount == 2 {
				if registerCount != 2 || poweredSets != 2 || pairableSets != 4 || discoverableSets != 2 {
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
			bonded, devicePresent = true, true
			if err := writeMessage(conn, signal, encodeDeviceAddedState(gamepadPath, true)); err != nil {
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
			if !devicePresent {
				return errors.New("Pair targeted a temporary device that was removed")
			}
			if !adapterPairable {
				return errors.New("Pair ran while the adapter was not bondable")
			}
			if bonded {
				return errors.New("agent re-paired an already bonded device")
			}
			pairCount++
			bonded = true
			authorization, err := callAgent("RequestAuthorization", "o", bodyPath(gamepadPath))
			if err != nil {
				return err
			}
			if authorization.Type != msgMethodReturn {
				return errors.New("Just Works gamepad authorization rejected")
			}
			pin, err := callAgent("RequestPinCode", "o", bodyPath(gamepadPath))
			if err != nil {
				return err
			}
			if pin.Type != msgMethodReturn || pin.Signature != "s" {
				return errors.New("legacy gamepad PIN request was rejected")
			}
			pinDecoder := &decoder{b: pin.Body}
			pinValue, rest, err := pinDecoder.readComplete("s")
			if err != nil || rest != "" || pinValue != "0000" {
				return errors.New("legacy gamepad received an unexpected PIN")
			}
			passkey, err := callAgent("RequestPasskey", "o", bodyPath(gamepadPath))
			if err != nil {
				return err
			}
			if passkey.Type != msgError {
				return errors.New("NoInputNoOutput agent supplied a passkey")
			}
			display, err := callAgent("DisplayPasskey", "ouq", bodyDisplayPasskey(gamepadPath))
			if err != nil {
				return err
			}
			if display.Type != msgError {
				return errors.New("NoInputNoOutput agent accepted a display-passkey request")
			}
			phone, err := callAgent("RequestConfirmation", "ou", bodyConfirm(phonePath))
			if err != nil {
				return err
			}
			if phone.Type != msgError {
				return errors.New("phone confirmation accepted")
			}
			ok, err := callAgent("RequestConfirmation", "ou", bodyConfirmPasskey(gamepadPath, 654321))
			if err != nil {
				return err
			}
			if ok.Type != msgError {
				return errors.New("NoInputNoOutput agent confirmed a passkey")
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
		case "RemoveDevice":
			if msg.Path != "/org/bluez/hci0" || msg.Iface != adapterIface || msg.Signature != "o" {
				return errors.New("stale device was not removed through Adapter1.RemoveDevice")
			}
			d := &decoder{b: msg.Body}
			device, rest, err := d.readComplete("o")
			if err != nil || rest != "" || device != gamepadPath {
				return errors.New("RemoveDevice targeted the wrong device")
			}
			removeDeviceCount++
			bonded, devicePresent = false, false
			if removeDeviceCount != 1 {
				return errors.New("stale device removed more than once")
			}
			if err := reply(conn, next(), msg.Serial, "", nil); err != nil {
				return err
			}
			removed := &encoder{}
			removed.str(gamepadPath)
			removed.array(4, func() { removed.str(deviceIface) })
			if err := writeMessage(conn, &Message{Type: msgSignal, Serial: next(), Path: "/", Iface: objectsIface, Member: "InterfacesRemoved", Signature: "oas"}, removed.buf); err != nil {
				return err
			}
			devicePresent = true
			if err := writeMessage(conn, &Message{Type: msgSignal, Serial: next(), Path: "/", Iface: objectsIface, Member: "InterfacesAdded", Signature: "oa{sa{sv}}"}, encodeDeviceAddedState(gamepadPath, false)); err != nil {
				return err
			}
		case "Connect":
			if msg.Path != gamepadPath {
				return errors.New("connected " + msg.Path)
			}
			if adapterPairable {
				return errors.New("adapter remained pairable during Connect")
			}
			connectCount++
			if connectCount == 1 {
				if err := replyError(conn, next(), msg.Serial, "org.bluez.Error.Failed", "Key missing"); err != nil {
					return err
				}
				continue
			}
			if connectCount == 2 {
				if pairCount != 1 || removeDeviceCount != 1 || !bonded || !devicePresent {
					return fmt.Errorf("pair result was not retained on the same bonded device: connect=%d pair=%d remove=%d bonded=%v present=%v", connectCount, pairCount, removeDeviceCount, bonded, devicePresent)
				}
				if err := reply(conn, next(), msg.Serial, "", nil); err != nil {
					return err
				}
				if err := writeDeviceProperties(conn, next(), gamepadPath, propBool("Paired", true), propBool("Connected", true)); err != nil {
					return err
				}
				if err := writeDeviceProperties(conn, next(), gamepadPath, propBool("Connected", false)); err != nil {
					return err
				}
				continue
			}
			if connectCount != 3 || pairCount != 1 || removeDeviceCount != 1 || !bonded || !devicePresent {
				return fmt.Errorf("reconnect unexpectedly paired or removed the device: connect=%d pair=%d remove=%d bonded=%v present=%v", connectCount, pairCount, removeDeviceCount, bonded, devicePresent)
			}
			if err := reply(conn, next(), msg.Serial, "", nil); err != nil {
				return err
			}
			if err := writeDeviceProperties(conn, next(), gamepadPath, propBool("Connected", true)); err != nil {
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

func replyError(conn net.Conn, serial, to uint32, name, message string) error {
	msg := &Message{Type: msgError, Serial: serial, ReplySerial: to, ErrorName: name, Signature: "s"}
	return writeMessage(conn, msg, bodyString(message))
}

func bodyConfirm(path string) []byte {
	return bodyConfirmPasskey(path, 0)
}

func bodyConfirmPasskey(path string, passkey uint32) []byte {
	e := &encoder{}
	e.str(path)
	e.u32(passkey)
	return e.buf
}

func bodyDisplayPasskey(path string) []byte {
	e := &encoder{}
	e.str(path)
	e.u32(123456)
	e.pad(2)
	e.buf = append(e.buf, 0, 0)
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

func encodeDeviceAddedState(path string, paired bool) []byte {
	e := &encoder{}
	e.str(path)
	e.array(8, func() {
		e.pad(8)
		e.str(deviceIface)
		e.array(8, func() {
			props := []fixtureProp{
				propString("Name", "Pad"),
				propUint("Class", 0x508),
				propBool("Paired", paired),
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
