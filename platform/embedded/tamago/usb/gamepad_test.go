// SPDX-License-Identifier: GPL-3.0-or-later
package usb

import (
	"bytes"
	"encoding/hex"
	"math/rand"
	"testing"
)

// Synthetic standard HID layout, not a capture of the attached081f:e401 pad:
// two unsigned8-bit axes, twelve buttons and four constant padding bits.
const padDescriptor = "05010905a101150026ff007508950209300931810205091901290c150025017501950c8102750495018103c0"

func descriptor(t *testing.T, text string) []byte {
	t.Helper()
	data, err := hex.DecodeString(text)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func pad(t *testing.T, text string) *Gamepad {
	t.Helper()
	g, err := ParseGamepad(descriptor(t, text))
	if err != nil {
		t.Fatal(err)
	}
	return g
}
func TestGamepadAxesButtonsAndAtomicInvalidReport(t *testing.T) {
	g := pad(t, padDescriptor)
	if g.MaxReportBytes() != 4 {
		t.Fatal(g.MaxReportBytes())
	}
	s, changed, err := g.Feed([]byte{0, 255, 1, 8})
	if err != nil || !changed || s.Axes[0] != -32767 || s.Axes[1] != 32767 || s.Buttons != 0x801 || s.Hat != -1 {
		t.Fatal(s, changed, err)
	}
	if got, change, err := g.Feed([]byte{0, 255, 1, 8}); err != nil || change || got != s {
		t.Fatal(got, change, err)
	}
	for _, bad := range [][]byte{nil, {0, 255, 0}, {0, 255, 0, 0, 0}} {
		got, change, err := g.Feed(bad)
		if err == nil || change || got != s {
			t.Fatal("bad report changed state", got, change, err)
		}
	}
	s, _, err = g.Feed([]byte{128, 127, 0, 0})
	if err != nil || s.Buttons != 0 || s.Axes[0] < 0 || s.Axes[1] >= 0 {
		t.Fatal(s, err)
	}
}
func TestGamepadIDsPreserveOtherReportFields(t *testing.T) {
	g := pad(t, "05010905a1018501150026ff0075089502093009318102850205091901290815002501750195088102c0")
	a, _, err := g.Feed([]byte{1, 255, 0})
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := g.Feed([]byte{2, 0x81})
	if err != nil || b.Axes != a.Axes || b.Buttons != 0x81 {
		t.Fatal(b, err)
	}
	if got, change, err := g.Feed([]byte{3, 0, 0}); err == nil || change || got != b {
		t.Fatal(got, change, err)
	}
	if got, _, err := g.Feed([]byte{1, 0, 255}); err != nil || got.Buttons != b.Buttons || got.Axes[0] != -32767 || got.Axes[1] != 32767 {
		t.Fatal(got, err)
	}
}
func TestGamepadSignedUnalignedFieldsAndMaximumBeforeMinimum(t *testing.T) {
	// Three padding bits, signed12-bit X and Y, five padding bits.
	g := pad(t, "05010904a101750395018103263f0015c0750c9502093009318102750595018103c0")
	// The maximum above is63, minimum-64, encoded in a12-bit field.
	wire := uint32(0xfc0)<<3 | uint32(63)<<15
	s, _, err := g.Feed([]byte{byte(wire), byte(wire >> 8), byte(wire >> 16), byte(wire >> 24)})
	if err != nil || s.Axes[0] != -32767 || s.Axes[1] != 32767 {
		t.Fatal(s, err)
	}
	before := s
	wire = uint32(64)<<3 | uint32(0)<<15 // X exceeds logical maximum.
	s, changed, err := g.Feed([]byte{byte(wire), byte(wire >> 8), byte(wire >> 16), byte(wire >> 24)})
	if err == nil || changed || s != before {
		t.Fatal(s, changed, err)
	}
}
func TestGamepadHatNullAndGlobalPushPop(t *testing.T) {
	g := pad(t, "05010905a101150025077504950109398142a405091901290415002501750195048102b4750495018103c0")
	s, _, err := g.Feed([]byte{0x20, 0})
	if err != nil || s.Hat != 0 || s.Buttons != 2 {
		t.Fatal(s, err)
	}
	s, _, err = g.Feed([]byte{0x16, 0})
	if err != nil || s.Hat != 6 || s.Buttons != 1 {
		t.Fatal(s, err)
	}
	s, _, err = g.Feed([]byte{0x0f, 0})
	if err != nil || s.Hat != -1 || s.Buttons != 0 {
		t.Fatal(s, err)
	}
}
func TestGamepadInvalidLayoutsAndEveryTruncation(t *testing.T) {
	valid := descriptor(t, padDescriptor)
	for n := 0; n < len(valid); n++ {
		if _, err := ParseGamepad(valid[:n]); err == nil {
			t.Fatalf("accepted truncation%d", n)
		}
	}
	for _, text := range []string{
		"fe00", "05010902a101150025017501950109308102c0", // Mouse application.
		"05010905a1018500150025017501950109308102c0", // ID0.
		"05010905a101150025017501950109308106c0",     // Relative axis.
		"05010905a101150025017501950109308100c0",     // Array.
		"05010905a101150025017521950109308102c0",     // Size33.
		"05010905a101150025017508954109308102c0",     // Count65.
		"05010905a101150025017520952009308102c0",     // Report overflow.
		"05010905a1011500250175019502093009308102c0", // Duplicate axis.
		"05010905a101150025087503950109308102c0",     // Range doesn't fit.
		"05010905a101b4c0", "05010905a101a4c0",       // Stack errors.
		"05010905a101150025017501950119308102c0",                 // Incomplete usage range.
		"05010905a1011500250175019501093081028501750195018103c0", // Mixed IDs.
	} {
		if _, err := ParseGamepad(descriptor(t, text)); err == nil {
			t.Fatal("accepted", text)
		}
	}
	if _, err := ParseGamepad(bytes.Repeat(valid, 100)); err == nil {
		t.Fatal("unbounded descriptor")
	}
}
func TestGamepadMalformedInputCannotPartiallyChangeState(t *testing.T) {
	// Two8-bit fields each declare logical0..1. The second can be invalid
	// after the first appeared to update; the whole state must remain old.
	g := pad(t, "05010905a10105091901290215002501750895028102c0")
	before, _, err := g.Feed([]byte{0, 1})
	if err != nil {
		t.Fatal(err)
	}
	got, change, err := g.Feed([]byte{1, 2})
	if err == nil || change || got != before {
		t.Fatal(got, change, err)
	}
}
func TestGamepadUntrustedDescriptorSmoke(t *testing.T) {
	rng := rand.New(rand.NewSource(41))
	for n := 0; n < 4000; n++ {
		b := make([]byte, rng.Intn(96))
		rng.Read(b)
		g, err := ParseGamepad(b)
		if err == nil {
			if g.MaxReportBytes() > 64 {
				t.Fatal("report bound")
			}
			g.Feed(make([]byte, g.MaxReportBytes()))
		}
	}
}
