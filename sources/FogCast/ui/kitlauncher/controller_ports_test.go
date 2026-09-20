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

func TestSecondPlayerOnlyFlowsToNegotiatedPorts(t *testing.T) {
	e := remoteinput.Event{Player: 1, Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}
	if got, ok := encodePlayHIDEvent(Session{}, e); !ok || got.Player != 0 || got.Code != e.Code {
		t.Fatalf("legacy surviving pad lost: %+v %v", got, ok)
	}
	p := &CorePackageSession{}
	p.ActiveInterfaces = append(p.ActiveInterfaces, struct {
		ID    string `json:"id"`
		Major uint16 `json:"major"`
		Minor uint16 `json:"minor"`
	}{ID: "fes.gamepad.ports", Major: 1})
	got, ok := encodePlayHIDEvent(Session{CorePackage: p}, e)
	if !ok || got != e {
		t.Fatalf("ports event %+v %v", got, ok)
	}
}
