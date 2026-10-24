package btagent

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestHelloMessageMatchesSpec(t *testing.T) {
	// Independent encoding of Hello: little-endian, serial 1, header fields
	// path, interface, member and destination, then pad to 8 bytes.
	literal, err := hex.DecodeString("6c01000100000000010000006d00000001016f00150000002f6f72672f667265656465736b746f702f4442757300000002017300140000006f72672e667265656465736b746f702e4442757300000000030173000500000048656c6c6f00000006017300140000006f72672e667265656465736b746f702e4442757300000000")
	if err != nil {
		t.Fatal(err)
	}
	msg := &Message{
		Type:   msgMethodCall,
		Serial: 1,
		Path:   "/org/freedesktop/DBus",
		Iface:  "org.freedesktop.DBus",
		Member: "Hello",
		Dest:   "org.freedesktop.DBus",
	}
	var buf bytes.Buffer
	if err := writeMessage(&buf, msg, nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), literal) {
		t.Fatalf("hello bytes\n got %x\nwant %x", buf.Bytes(), literal)
	}
	got, err := readMessage(bytes.NewReader(literal))
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != msgMethodCall || got.Serial != 1 || got.Path != msg.Path || got.Iface != msg.Iface || got.Member != "Hello" || got.Dest != msg.Dest || len(got.Body) != 0 {
		t.Fatalf("decoded hello %+v", got)
	}
}

func TestStringReplyRoundTrip(t *testing.T) {
	body := &encoder{}
	body.str("0000")
	msg := &Message{Type: msgMethodReturn, Serial: 2, ReplySerial: 7, Signature: "s"}
	var buf bytes.Buffer
	if err := writeMessage(&buf, msg, body.buf); err != nil {
		t.Fatal(err)
	}
	got, err := readMessage(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.ReplySerial != 7 || got.Signature != "s" {
		t.Fatalf("header %+v", got)
	}
	d := &decoder{b: got.Body}
	v, rest, err := d.readComplete("s")
	if err != nil || rest != "" || v != "0000" {
		t.Fatalf("body %v %q %v", v, rest, err)
	}
}

func TestManagedObjectsRoundTrip(t *testing.T) {
	body := encodeObjects([]fixtureObj{{
		path: "/org/bluez/hci0/dev_AA",
		ifaces: []fixtureIface{{
			name: deviceIface,
			props: []fixtureProp{
				propString("Name", "Pad"),
				propUint("Class", 0x508),
				propBool("Paired", false),
				propStrings("UUIDs", hidUUID),
			},
		}},
	}})
	msg := &Message{Type: msgMethodReturn, Serial: 3, ReplySerial: 1, Signature: "a{oa{sa{sv}}}"}
	var buf bytes.Buffer
	if err := writeMessage(&buf, msg, body); err != nil {
		t.Fatal(err)
	}
	got, err := readMessage(&buf)
	if err != nil {
		t.Fatal(err)
	}
	devices, adapters, err := decodeObjects(got.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(adapters) != 0 || len(devices) != 1 {
		t.Fatalf("objects %d adapters %d", len(devices), len(adapters))
	}
	dev := devices["/org/bluez/hci0/dev_AA"]
	if dev.Name != "Pad" || dev.Class != 0x508 || dev.Paired || len(dev.UUIDs) != 1 || dev.UUIDs[0] != hidUUID {
		t.Fatalf("device %+v", dev)
	}
}

type fixtureProp struct {
	name, sig string
	write     func(*encoder)
}

type fixtureIface struct {
	name  string
	props []fixtureProp
}

type fixtureObj struct {
	path   string
	ifaces []fixtureIface
}

func propBool(name string, v bool) fixtureProp {
	return fixtureProp{name, "b", func(e *encoder) { e.boolean(v) }}
}

func propUint(name string, v uint32) fixtureProp {
	return fixtureProp{name, "u", func(e *encoder) { e.u32(v) }}
}

func propString(name, v string) fixtureProp {
	return fixtureProp{name, "s", func(e *encoder) { e.str(v) }}
}

func propStrings(name string, values ...string) fixtureProp {
	return fixtureProp{name, "as", func(e *encoder) {
		e.array(4, func() {
			for _, v := range values {
				e.str(v)
			}
		})
	}}
}

func encodeObjects(objs []fixtureObj) []byte {
	e := &encoder{}
	e.array(8, func() {
		for _, obj := range objs {
			e.pad(8)
			e.str(obj.path)
			e.array(8, func() {
				for _, iface := range obj.ifaces {
					e.pad(8)
					e.str(iface.name)
					e.array(8, func() {
						for _, prop := range iface.props {
							e.pad(8)
							e.str(prop.name)
							e.sig(prop.sig)
							prop.write(e)
						}
					})
				}
			})
		}
	})
	return e.buf
}
