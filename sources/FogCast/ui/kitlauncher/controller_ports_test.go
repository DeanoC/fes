package kitlauncher

import (
	"testing"
	"time"

	"github.com/DeanoC/FogCast/remoteinput"
)

func TestStopChordDoesNotCombinePlayers(t *testing.T) {
	m := Model{Session: Session{State: "active"}}
	now := time.Now()
	m.Input(remoteinput.Event{Player: 0, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonSelect}, now)
	m.Input(remoteinput.Event{Player: 1, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonStart}, now)
	if got := m.Tick(now.Add(2 * time.Second)); got != "" {
		t.Fatalf("cross-player chord=%q", got)
	}
	m.Input(remoteinput.Event{Player: 1, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonSelect}, now.Add(2*time.Second))
	if got := m.Tick(now.Add(4 * time.Second)); got != "stop" {
		t.Fatalf("same-player chord=%q", got)
	}
	m.ResetControls()
	if got := m.Tick(now.Add(6 * time.Second)); got != "" {
		t.Fatal("stale chord")
	}
}

func TestLocalFrameKeepsPlayerIndex(t *testing.T) {
	e := remoteinput.Event{Player: 1, Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}
	frame := localInputFrame(1, e, time.Unix(0, 0))
	if frame.Player != 1 || frame.Code != uint16(e.Code) || frame.Header.Session == 0 {
		t.Fatalf("frame %+v", frame)
	}
	key := remoteinput.Event{Device: remoteinput.DeviceKeyboard, Kind: remoteinput.KindKey, Action: remoteinput.ActionPress, Code: remoteinput.KeyA}
	keyFrame := localInputFrame(2, key, time.Unix(0, 0))
	if keyFrame.Device != uint8(remoteinput.DeviceKeyboard) || keyFrame.Kind != uint8(remoteinput.KindKey) || keyFrame.Code != uint16(key.Code) {
		t.Fatalf("keyboard frame %+v", keyFrame)
	}
}
