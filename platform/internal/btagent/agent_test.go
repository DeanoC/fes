package btagent

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func TestRunPairsGamepadAndRejectsPhone(t *testing.T) {
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
		case "RegisterAgent":
			d := &decoder{b: msg.Body}
			vals, err := d.readSig("os")
			if err != nil || len(vals) != 2 || vals[0] != agentPath || vals[1] != agentCap {
				return errors.New("agent was not registered as KeyboardDisplay")
			}
			if err := reply(conn, next(), msg.Serial, "", nil); err != nil {
				return err
			}
		case "RequestDefaultAgent", "Set", "StartDiscovery":
			if err := reply(conn, next(), msg.Serial, "", nil); err != nil {
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
			return nil
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

const (
	gamepadPath = "/org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF"
	phonePath   = "/org/bluez/hci0/dev_11_22_33_44_55_66"
)

func sampleObjects() []byte {
	return encodeObjects([]fixtureObj{
		{path: "/org/bluez/hci0", ifaces: []fixtureIface{{name: adapterIface}}},
		{path: gamepadPath, ifaces: []fixtureIface{{name: deviceIface, props: []fixtureProp{
			propString("Name", "Pad"),
			propUint("Class", 0x508),
			propBool("Paired", false),
			propBool("Connected", false),
			propStrings("UUIDs", hidUUID),
		}}}},
		{path: phonePath, ifaces: []fixtureIface{{name: deviceIface, props: []fixtureProp{
			propString("Name", "Phone"),
			propUint("Class", 0x200),
			propBool("Paired", false),
			propStrings("UUIDs", hidUUID),
		}}}},
	})
}
