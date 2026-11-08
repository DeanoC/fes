// SPDX-License-Identifier: GPL-3.0-or-later
package usb

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

func TestCapturedSystemControlDescriptorCannotBecomeGamepad(t *testing.T) {
	d, err := os.ReadFile("../../../../tests/fixtures/nano-usb-gamepad/system-control-report.bin")
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(d)
	if len(d) != 53 || hex.EncodeToString(digest[:]) != "3d277392ac48c3c8d0afa6545e6b7c21d6e80286fcc78a89657e639f1b537e53" {
		t.Fatal("system-control capture changed")
	}
	if g, err := ParseGamepad(d); err == nil || g != nil || !strings.Contains(err.Error(), "gamepad application required") {
		t.Fatal("non-gamepad descriptor admitted", g, err)
	}
}

func TestCapturedJoystickDescriptorIgnoresVendorFieldsAndOutputStream(t *testing.T) {
	d, err := os.ReadFile("../../../../tests/fixtures/nano-usb-gamepad/joystick-report.bin")
	if err != nil {
		t.Fatal(err)
	}
	g, err := ParseGamepad(d)
	if err != nil {
		t.Fatal(err)
	}
	digest := g.DescriptorSHA256()
	if hex.EncodeToString(digest[:]) != "171f93692f2d718af6c6e9fc5b7d8c2580ac33fa2e3044ee2393c07ec839dc11" {
		t.Fatal("capture changed")
	}
	if g.MaxReportBytes() != 8 {
		t.Fatal("output item changed input length", g.MaxReportBytes())
	}
	// Constructed report, not a physical input capture. Padding and vendor bits
	// are deliberately nonzero; only X/Y and buttons1/10 are exposed.
	s, _, err := g.Feed([]byte{255, 0, 255, 170, 85, 31, 224, 255})
	if err != nil || s.Axes != [6]int16{32767, -32767} || s.Buttons != 0x201 || s.Hat != -1 {
		t.Fatal(s, err)
	}
	if got, changed, err := g.Feed([]byte{255, 0, 0, 0, 0, 16, 32, 0}); err != nil || changed || got != s {
		t.Fatal("vendor/padding bits became controls", got, err)
	}
	if got, changed, err := g.Feed(make([]byte, 7)); err == nil || changed || got != s {
		t.Fatal("output-sized input accepted", got, err)
	}
	s, _, err = g.Feed([]byte{128, 128, 0, 0, 0, 0, 0, 0})
	if err != nil || s.Buttons != 0 || s.Axes[0] != 128 || s.Axes[1] != 128 {
		t.Fatal(s, err)
	}
}
func TestRepeatedVendorUsagesPreserveFollowingControlOffsets(t *testing.T) {
	// Ten vendor bits followed by two button bits and four padding bits.
	g := pad(t, "05010904a1010600ff7501950a150025010901810205091901290295028102750495018103c0")
	s, _, err := g.Feed([]byte{255, 0xfb})
	if err != nil || s.Buttons != 2 {
		t.Fatal(s, err)
	}
	s, _, err = g.Feed([]byte{0, 0x04})
	if err != nil || s.Buttons != 1 {
		t.Fatal(s, err)
	}
	// Repeating a known control is still ambiguous; a missing usage is not
	// permission to invent a control, and excess usages remain unsupported.
	for _, text := range []string{
		"05010904a1017501950215002501050909018102c0",
		"05010904a10175019502150025018102c0",
		"05010904a10175019501150025010509190129028102c0",
	} {
		if _, err := ParseGamepad(descriptor(t, text)); err == nil {
			t.Fatal("ambiguous layout accepted", text)
		}
	}
}
