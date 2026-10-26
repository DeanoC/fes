package btagent

import (
	"testing"
	"time"
)

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
		{"dualsense exact product", devInfo{UUIDs: hid, Modalias: "usb:v054Cp0CE6d0100"}, true},
		{"dualshock 4 exact product", devInfo{UUIDs: hid, Modalias: "bluetooth:v054Cp05C4d0001"}, true},
		{"xbox exact product", devInfo{UUIDs: hog, Modalias: "bluetooth:v045Ep02FDd0001"}, true},
		{"nintendo pro exact product", devInfo{UUIDs: hid, Modalias: "bluetooth:v057Ep2009d0001"}, true},
		{"8bitdo ultimate wireless classless", devInfo{Modalias: "usb:v2DC8p3106d0001"}, true},
		{"8bitdo xbox pro 2 classless", devInfo{Modalias: "usb:v2DC8p2000d0001"}, true},
		{"8bitdo ultimate 3-mode classless", devInfo{Modalias: "usb:v2DC8p200Fd0001"}, true},
		{"8bitdo pro 2 xbox classless", devInfo{Modalias: "usb:v2DC8p201Ed0001"}, true},
		{"8bitdo ultimate bluetooth classless", devInfo{Modalias: "bluetooth:v2DC8p3109d0001"}, true},
		{"8bitdo ultimate 2c classless", devInfo{Modalias: "bluetooth:v2DC8p310Ad0001"}, true},
		{"8bitdo ultimate 2 classless", devInfo{Modalias: "bluetooth:v2DC8p310Bd0001"}, true},
		{"8bitdo sn30 pro classless", devInfo{Modalias: "bluetooth:v2DC8p6001d0001"}, true},
		{"8bitdo unknown product class 0", devInfo{UUIDs: hid, Modalias: "usb:v2DC8p0001d0001"}, false},
		{"known vendor unknown product class 0", devInfo{UUIDs: hid, Modalias: "usb:v045Ep0001d0001"}, false},
		{"known vendor unknown product hog class 0", devInfo{UUIDs: hog, Modalias: "bluetooth:v057Ep0001d0001"}, false},
		{"exact controller product without hid uuid", devInfo{Modalias: "usb:v054Cp0CE6d0100"}, true},
		{"dualsense exact product without hid or peripheral", devInfo{Modalias: "usb:v054Cp0CE6d0100"}, true},
		{"sony peripheral hid no minor", devInfo{Class: 0x500, UUIDs: hid, Modalias: "usb:v054Cp0CE6d0100"}, true},
		{"microsoft keyboard", devInfo{Class: 0x540, UUIDs: hid, Modalias: "usb:v045Ep0001d0001"}, false},
		{"microsoft mouse", devInfo{Class: 0x580, UUIDs: hid, Modalias: "usb:v045Ep0002d0001"}, false},
		{"microsoft keyboard appearance", devInfo{Appearance: 0x03C1, UUIDs: hog, Modalias: "bluetooth:v045Ep0003d0001"}, false},
		{"gamepad class with keyboard pointing bits", devInfo{Class: 0x5c8}, false},
		{"gamepad appearance with keyboard class bits", devInfo{Class: 0x5c8, Appearance: appearanceGamepad}, false},
		{"known controller with keyboard class bits", devInfo{Class: 0x5c8, UUIDs: hid, Modalias: "usb:v045Ep0001d0001"}, false},
		{"gamepad class with keyboard appearance", devInfo{Class: 0x508, Appearance: appearanceKeyboard}, false},
		{"joystick appearance with mouse appearance", devInfo{Appearance: appearanceMouse, UUIDs: hog}, false},
		{"microsoft peripheral without hid", devInfo{Class: 0x500, Modalias: "usb:v045Ep0002d0001"}, false},
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
	if decide(devInfo{Class: 0x5c8, Appearance: appearanceGamepad, UUIDs: []string{hidUUID}, Modalias: "usb:v045Ep0001d0001"}) != actionIgnore {
		t.Fatal("keyboard and pointing class bits must override gamepad indicators")
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
	pad := devInfo{Path: "p", Class: 0x508}
	if !a.Start(pad, now, actionPair) || !a.Has("p") {
		t.Fatal("start")
	}
	if a.Start(pad, now, actionPair) {
		t.Fatal("second start while in flight")
	}
	a.Finish("p", false, now)
	if a.Has("p") {
		t.Fatal("finished path still in flight")
	}
	if a.Start(pad, now.Add(29*time.Second), actionPair) {
		t.Fatal("retry inside the gap")
	}
	secondAt := now.Add(attemptGap)
	if !a.Start(pad, secondAt, actionPair) {
		t.Fatal("second attempt")
	}
	a.Finish("p", false, secondAt)
	thirdAt := now.Add(2 * attemptGap)
	if !a.Start(pad, thirdAt, actionPair) {
		t.Fatal("third attempt")
	}
	a.Finish("p", false, thirdAt)
	if a.Start(pad, now.Add(3*attemptGap), actionPair) {
		t.Fatal("fourth attempt")
	}
	if a.Start(pad, thirdAt.Add(attemptCooldown-time.Nanosecond), actionPair) {
		t.Fatal("pairing lockout expired early")
	}
	if !a.Start(pad, thirdAt.Add(attemptCooldown), actionPair) {
		t.Fatal("pairing lockout did not expire")
	}
	a.Finish("p", true, thirdAt.Add(attemptCooldown))

	ok := newAttempts()
	okPad := devInfo{Path: "pad", Class: 0x508}
	if !ok.Start(okPad, now, actionPair) {
		t.Fatal("success start")
	}
	ok.Finish("pad", true, now)
	if !ok.Start(okPad, now, actionConnect) {
		t.Fatal("successful pairing suppressed the first connect")
	}
	ok.Finish("pad", true, now)
	if ok.Start(okPad, now, actionConnect) {
		t.Fatal("connect retry bypassed its own spacing")
	}
	if ok.Start(okPad, now, actionPair) {
		t.Fatal("success retries immediately")
	}
	if !ok.Start(okPad, now.Add(attemptGap), actionPair) {
		t.Fatal("success retries after the gap")
	}
}

func TestConnectRetriesAreNotLimitedByPairingBudget(t *testing.T) {
	a := newAttempts()
	now := time.Unix(1_700_000_000, 0)
	pad := devInfo{Path: "pad", Class: 0x508}
	for i := 0; i < attemptMax; i++ {
		if !a.Start(pad, now, actionPair) {
			t.Fatalf("pair attempt %d did not start", i+1)
		}
		a.Finish("pad", false, now)
		now = now.Add(attemptGap)
	}
	if a.Start(pad, now, actionPair) {
		t.Fatal("pairing failure budget was not enforced")
	}
	for i := 0; i < attemptMax+1; i++ {
		now = now.Add(attemptGap)
		if !a.Start(pad, now, actionConnect) {
			t.Fatalf("reconnect attempt %d did not start", i+1)
		}
		if a.Has("pad") {
			t.Fatal("reconnect was marked as an in-flight pairing request")
		}
		a.Finish("pad", false, now)
	}
}

func TestAttemptStartRequiresGamepadPolicy(t *testing.T) {
	a := newAttempts()
	now := time.Unix(1_700_000_000, 0)
	if a.Start(devInfo{Path: "unknown", UUIDs: []string{hidUUID}, Modalias: "usb:v045Ep0001d0001"}, now, actionPair) {
		t.Fatal("known-vendor class-0 HID device started pairing")
	}
	if a.Start(devInfo{Path: "keyboard", Class: 0x540}, now, actionPair) {
		t.Fatal("keyboard started pairing")
	}
	if !a.Start(devInfo{Path: "controller", Modalias: "usb:v054Cp0CE6d0100"}, now, actionPair) {
		t.Fatal("exact controller did not start pairing")
	}
}
