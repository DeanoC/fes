// SPDX-License-Identifier: GPL-3.0-or-later
package usb

import (
	"bytes"
	"errors"
	"testing"
)

func gamepadConfigurationFixture() []byte {
	b := configurationFixture()
	b[15], b[16] = 0, 0
	b[31] = 4
	return b
}
func TestGamepadConfigurationRequiresUnambiguousBoundedHID(t *testing.T) {
	b := gamepadConfigurationFixture()
	got, err := ParseGamepadConfiguration(b)
	if err != nil || got.ReportDescriptorBytes != 63 || got.MaxPacketSize != 4 {
		t.Fatal(got, err)
	}
	if _, err := ParseConfiguration(b); !errors.Is(err, ErrNoBootKeyboard) {
		t.Fatal(err)
	}
	if _, err := ParseGamepadConfiguration(configurationFixture()); !errors.Is(err, ErrNoGamepadInterface) {
		t.Fatal(err)
	}
	for n := 0; n < len(b); n++ {
		if _, err := ParseGamepadConfiguration(b[:n]); !errors.Is(err, ErrDescriptor) {
			t.Fatal(n, err)
		}
	}
	for _, mutate := range []func([]byte) []byte{
		func(b []byte) []byte { b[25], b[26] = 1, 16; return b },
		func(b []byte) []byte { b[25] = 0; return b },
		func(b []byte) []byte { b[31] = 0; return b },
		func(b []byte) []byte {
			b[4] = 2
			extra := bytes.Clone(b[9:])
			extra[2] = 1
			return fixTotal(append(b, extra...))
		},
	} {
		if _, err := ParseGamepadConfiguration(mutate(bytes.Clone(b))); !errors.Is(err, ErrDescriptor) {
			t.Fatal(err)
		}
	}
}
