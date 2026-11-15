// SPDX-License-Identifier: GPL-3.0-or-later
package input

import (
	"encoding/hex"
	"errors"
	"reflect"
	"testing"
)

// Synthetic HID schema; this is not the connected controller's descriptor.
const menuPadDescriptor = "05010905a101150026ff007508950209300931810205091901290c150025017501950c8102750495018103c0"

type gamepadQueue struct {
	reports [][]byte
	err     error
	polls   int
}

func (q *gamepadQueue) PollGamepad() ([]byte, error) {
	q.polls++
	if len(q.reports) == 0 {
		return nil, q.err
	}
	r := q.reports[0]
	q.reports = q.reports[1:]
	return r, nil
}
func menuPad(t *testing.T, q *gamepadQueue) *USBGamepad {
	t.Helper()
	d, err := hex.DecodeString(menuPadDescriptor)
	if err != nil {
		t.Fatal(err)
	}
	g, err := NewUSBGamepad(q, d, GamepadButtons{Confirm: 1, Back: 2})
	if err != nil {
		t.Fatal(err)
	}
	return g
}
func TestGamepadMenuEdgesAndState(t *testing.T) {
	q := &gamepadQueue{reports: [][]byte{{255, 0, 1, 0}, {255, 0, 1, 0}, {128, 128, 0, 0}, {0, 255, 2, 0}}}
	g := menuPad(t, q)
	events, err := g.Poll()
	want := []Event{{Key: "up"}, {Key: "right"}, {Key: "return"}, {Key: "down"}, {Key: "left"}, {Key: "escape"}}
	if err != nil || !reflect.DeepEqual(events, want) {
		t.Fatal(events, err)
	}
	s := g.State()
	if s.Axes[0] != -32767 || s.Axes[1] != 32767 || s.Buttons != 2 {
		t.Fatal(s)
	}
	// A bad report must neither release a held control nor change core state.
	if _, err := g.Decode([]byte{0}); err == nil {
		t.Fatal("invalid report accepted")
	}
	if g.State() != s {
		t.Fatal("invalid report changed state")
	}
	if e, err := g.Decode([]byte{0, 255, 2, 0}); err != nil || len(e) != 0 {
		t.Fatal(e, err)
	}
}
func TestGamepadMenuPollBoundAndFailure(t *testing.T) {
	q := &gamepadQueue{}
	for n := 0; n < 9; n++ {
		q.reports = append(q.reports, []byte{128, 128, 0, 0})
	}
	g := menuPad(t, q)
	if _, err := g.Poll(); err != nil || len(q.reports) != 1 || q.polls != 8 {
		t.Fatal(err, q.polls)
	}
	failure := errors.New("controller disconnected")
	q.err = failure
	if _, err := g.Poll(); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	d, _ := hex.DecodeString(menuPadDescriptor)
	for _, b := range []GamepadButtons{{0, 1}, {1, 1}, {33, 2}, {1, 33}} {
		if _, err := NewUSBGamepad(q, d, b); err == nil {
			t.Fatal("invalid mapping accepted", b)
		}
	}
	if _, err := NewUSBGamepad(nil, d, GamepadButtons{1, 2}); err == nil {
		t.Fatal("nil source accepted")
	}
}
func TestGamepadMenuHatOverridesAxesAndNeutralRestoresAxes(t *testing.T) {
	d, err := hex.DecodeString("05010905a101150026ff0075089502093009318102150025077504950109398142750495018103c0")
	if err != nil {
		t.Fatal(err)
	}
	g, err := NewUSBGamepad(&gamepadQueue{}, d, GamepadButtons{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	e, err := g.Decode([]byte{255, 255, 7})
	if err != nil || !reflect.DeepEqual(e, []Event{{Key: "up"}, {Key: "left"}}) {
		t.Fatal(e, err)
	}
	e, err = g.Decode([]byte{255, 255, 15})
	if err != nil || !reflect.DeepEqual(e, []Event{{Key: "down"}, {Key: "right"}}) {
		t.Fatal(e, err)
	}
}
