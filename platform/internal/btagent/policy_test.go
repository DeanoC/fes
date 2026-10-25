package btagent

import "testing"
import "time"

func TestIsGamepad(t *testing.T) {
	hid := []string{hidUUID}
	hog := []string{hogUUID}
	cases := []struct {
		name string
		dev  devInfo
		want bool
	}{
		{"gamepad class", devInfo{Class: 0x508}, true},
		{"dualsense class", devInfo{Class: 0x002508, UUIDs: hid}, true},
		{"joystick class", devInfo{Class: 0x504}, true},
		{"gamepad appearance", devInfo{Appearance: 0x03C4, UUIDs: hog}, true},
		{"joystick appearance", devInfo{Appearance: 0x03C3}, true},
		{"sony hid class 0", devInfo{UUIDs: hid, Modalias: "usb:v054Cp0CE6d0100"}, true},
		{"nintendo bluetooth modalias", devInfo{UUIDs: hid, Modalias: "bluetooth:v057Ep2009d0001"}, true},
		{"sony without hid or peripheral", devInfo{Modalias: "usb:v054Cp0CE6d0100"}, false},
		{"keyboard", devInfo{Class: 0x540, UUIDs: hid}, false},
		{"pointing", devInfo{Class: 0x580, UUIDs: hid}, false},
		{"unknown hid class 0", devInfo{UUIDs: hid}, false},
		{"unknown hog class 0", devInfo{UUIDs: hog}, false},
		{"keyboard appearance", devInfo{Appearance: 0x03C1, UUIDs: hog}, false},
		{"unknown vendor hid", devInfo{UUIDs: hid, Modalias: "usb:v1234p0001d0001"}, false},
		{"class 0", devInfo{}, false},
		{"remote with hid", devInfo{Class: 0x50C, UUIDs: hid}, false},
		{"sony phone", devInfo{Class: 0x200, UUIDs: hid, Modalias: "usb:v054Cp0001d0001"}, false},
		{"computer", devInfo{Class: 0x100, UUIDs: hid}, false},
		{"audio", devInfo{Class: 0x400, UUIDs: hog}, false},
	}
	for _, tc := range cases {
		if got := isGamepad(tc.dev); got != tc.want {
			t.Errorf("%s: got %v", tc.name, got)
		}
	}
}

func TestDecide(t *testing.T) {
	if decide(devInfo{Class: 0x508}) != actionPair {
		t.Fatal("unpaired gamepad")
	}
	if decide(devInfo{Paired: true, Class: 0x508}) != actionConnect {
		t.Fatal("paired gamepad")
	}
	if decide(devInfo{Paired: true, Connected: true, Class: 0x508}) != actionIgnore {
		t.Fatal("connected gamepad")
	}
	if decide(devInfo{UUIDs: []string{hidUUID}, Class: 0x540}) != actionIgnore {
		t.Fatal("keyboard must not be paired")
	}
	if decide(devInfo{UUIDs: []string{hidUUID}}) != actionIgnore {
		t.Fatal("unclassified hid device must not be paired")
	}
}

func TestAttemptBudget(t *testing.T) {
	a := newAttempts()
	now := time.Unix(1_700_000_000, 0)
	if !a.Start("p", now, actionPair) || !a.Has("p") {
		t.Fatal("start")
	}
	if a.Start("p", now, actionPair) {
		t.Fatal("second start while in flight")
	}
	a.Finish("p", false)
	if a.Has("p") {
		t.Fatal("finished path still in flight")
	}
	if a.Start("p", now.Add(29*time.Second), actionPair) {
		t.Fatal("retry inside the gap")
	}
	if !a.Start("p", now.Add(attemptGap), actionPair) {
		t.Fatal("second attempt")
	}
	a.Finish("p", false)
	if !a.Start("p", now.Add(2*attemptGap), actionPair) {
		t.Fatal("third attempt")
	}
	a.Finish("p", false)
	if a.Start("p", now.Add(3*attemptGap), actionPair) {
		t.Fatal("fourth attempt")
	}

	ok := newAttempts()
	if !ok.Start("pad", now, actionPair) {
		t.Fatal("success start")
	}
	ok.Finish("pad", true)
	if ok.Start("pad", now, actionPair) {
		t.Fatal("success retries immediately")
	}
	if !ok.Start("pad", now.Add(attemptGap), actionPair) {
		t.Fatal("success retries after the gap")
	}
}

func TestConnectRetriesAreNotLimitedByPairingBudget(t *testing.T) {
	a := newAttempts()
	now := time.Unix(1_700_000_000, 0)
	for i := 0; i < attemptMax; i++ {
		if !a.Start("pad", now, actionPair) {
			t.Fatalf("pair attempt %d did not start", i+1)
		}
		a.Finish("pad", false)
		now = now.Add(attemptGap)
	}
	if a.Start("pad", now, actionPair) {
		t.Fatal("pairing failure budget was not enforced")
	}
	for i := 0; i < attemptMax+1; i++ {
		now = now.Add(attemptGap)
		if !a.Start("pad", now, actionConnect) {
			t.Fatalf("reconnect attempt %d did not start", i+1)
		}
		if a.Has("pad") {
			t.Fatal("reconnect was marked as an in-flight pairing request")
		}
		a.Finish("pad", false)
	}
}
