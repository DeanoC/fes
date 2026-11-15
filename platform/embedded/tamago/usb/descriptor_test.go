package usb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func configurationFixture() []byte {
	return []byte{
		9, 2, 34, 0, 1, 1, 0, 0x80, 50,
		9, 4, 0, 0, 1, 3, 1, 1, 0,
		9, 0x21, 0x11, 0x01, 0, 1, 0x22, 63, 0,
		7, 5, 0x81, 3, 8, 0, 10,
	}
}

func fixTotal(data []byte) []byte {
	binary.LittleEndian.PutUint16(data[2:4], uint16(len(data)))
	return data
}

func TestParseConfiguration(t *testing.T) {
	data := configurationFixture()
	got, err := ParseConfiguration(append(bytes.Clone(data), 0, 0, 0))
	want := Configuration{Value: 1, Interface: 0, Endpoint: 0x81, MaxPacketSize: 8, Interval: 10}
	if err != nil || got != want {
		t.Fatalf("configuration = %+v, %v; want %+v", got, err, want)
	}
	// Another class and an alternate setting may precede the boot interface.
	composite := append(bytes.Clone(data[:9]), []byte{
		9, 4, 1, 0, 0, 0xff, 0, 0, 0,
		9, 4, 0, 1, 0, 3, 1, 1, 0,
	}...)
	composite = append(composite, data[9:]...)
	composite[4] = 2
	got, err = ParseConfiguration(fixTotal(composite))
	if err != nil || got != want {
		t.Fatalf("composite configuration = %+v, %v", got, err)
	}
}

func TestRejectConfigurationBoundsAndMalformedDescriptors(t *testing.T) {
	valid := configurationFixture()
	for length := 0; length < len(valid); length++ {
		if _, err := ParseConfiguration(valid[:length]); !errors.Is(err, ErrDescriptor) {
			t.Fatalf("accepted truncated length %d: %v", length, err)
		}
	}
	tests := map[string]func([]byte) []byte{
		"zero descriptor length":       func(b []byte) []byte { b[9] = 0; return b },
		"one byte descriptor":          func(b []byte) []byte { b[9] = 1; return b },
		"descriptor overrun":           func(b []byte) []byte { b[27] = 8; return b },
		"short total":                  func(b []byte) []byte { b[2] = 8; return b },
		"huge total":                   func(b []byte) []byte { b[2], b[3] = 1, 16; return b },
		"zero config value":            func(b []byte) []byte { b[5] = 0; return b },
		"missing mandatory attribute":  func(b []byte) []byte { b[7] = 0; return b },
		"interface count":              func(b []byte) []byte { b[4] = 2; return b },
		"endpoint count":               func(b []byte) []byte { b[13] = 2; return b },
		"endpoint zero":                func(b []byte) []byte { b[29] = 0x80; return b },
		"reserved address bits":        func(b []byte) []byte { b[29] = 0x91; return b },
		"short packet":                 func(b []byte) []byte { b[31] = 7; return b },
		"high bandwidth unsupported":   func(b []byte) []byte { b[32] = 8; return b },
		"zero interval":                func(b []byte) []byte { b[33] = 0; return b },
		"missing report descriptor":    func(b []byte) []byte { b[24] = 0x23; return b },
		"empty report descriptor":      func(b []byte) []byte { b[25] = 0; return b },
		"invalid HID descriptor count": func(b []byte) []byte { b[23] = 2; return b },
		"no interrupt input":           func(b []byte) []byte { b[30] = 2; return b },
		"duplicate endpoint": func(b []byte) []byte {
			b[13] = 2
			return fixTotal(append(b, b[27:]...))
		},
		"multiple input endpoints": func(b []byte) []byte {
			b[13] = 2
			return fixTotal(append(b, 7, 5, 0x82, 3, 8, 0, 10))
		},
		"multiple keyboards": func(b []byte) []byte {
			b[4] = 2
			second := bytes.Clone(b[9:])
			second[2] = 1
			return fixTotal(append(b, second...))
		},
		"duplicate alternate": func(b []byte) []byte {
			return fixTotal(append(b, b[9:]...))
		},
		"endpoint before interface": func(b []byte) []byte {
			out := append(bytes.Clone(b[:9]), b[27:]...)
			out = append(out, b[9:27]...)
			return fixTotal(out)
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseConfiguration(mutate(bytes.Clone(valid))); !errors.Is(err, ErrDescriptor) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	noKeyboard := bytes.Clone(valid)
	noKeyboard[16] = 2 // Boot mouse protocol.
	if _, err := ParseConfiguration(noKeyboard); !errors.Is(err, ErrNoBootKeyboard) {
		t.Fatalf("non-keyboard error = %v", err)
	}
}

func FuzzParseConfiguration(f *testing.F) {
	f.Add(configurationFixture())
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		configuration, err := ParseConfiguration(data)
		if err == nil && (configuration.Value == 0 || configuration.Endpoint&0x80 == 0 || configuration.MaxPacketSize < 8) {
			t.Fatalf("invalid accepted configuration: %+v", configuration)
		}
	})
}
