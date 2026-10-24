package btagent

import "testing"
import "time"

func TestIsController(t *testing.T) {
	hid := []string{hidUUID}
	hog := []string{hogUUID}
	cases := []struct {
		name  string
		uuids []string
		class uint32
		want  bool
	}{
		{"gamepad", nil, 0x508, true},
		{"joystick", nil, 0x504, true},
		{"keyboard", nil, 0x540, true},
		{"pointing", nil, 0x580, true},
		{"hid class 0", hid, 0, true},
		{"hog class 0", hog, 0, true},
		{"mixed case hid", []string{"00001124-0000-1000-8000-00805F9B34FB"}, 0, true},
		{"class 0", nil, 0, false},
		{"remote without hid", nil, 0x50C, false},
		{"remote with hid", hid, 0x50C, true},
		{"phone with hid", hid, 0x200, false},
		{"computer", hid, 0x100, false},
		{"audio", hog, 0x400, false},
	}
	for _, tc := range cases {
		if got := isController(tc.uuids, tc.class); got != tc.want {
			t.Errorf("%s: got %v", tc.name, got)
		}
	}
}

func TestDecide(t *testing.T) {
	if decide(false, false, []string{hidUUID}, 0) != actionPair {
		t.Fatal("unpaired controller")
	}
	if decide(true, false, nil, 0x508) != actionConnect {
		t.Fatal("paired controller")
	}
	if decide(true, true, nil, 0x508) != actionIgnore {
		t.Fatal("connected controller")
	}
	if decide(false, false, []string{hidUUID}, 0x200) != actionIgnore {
		t.Fatal("phone")
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
