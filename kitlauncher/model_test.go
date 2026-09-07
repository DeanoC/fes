package kitlauncher

import (
	"github.com/DeanoC/FogCast/host/tenfoot"
	"github.com/DeanoC/FogCast/remoteinput"
	"testing"
	"time"
)

func TestMenuAndGameControlsRemainSeparate(t *testing.T) {
	m := Model{Games: []tenfoot.Game{{ID: "pong", Launchable: true}, {ID: "sonic", Launchable: true}}, Connected: true, TargetReady: true}
	down, _ := remoteinput.NormalizeAxis("left-y", 32767)
	m.Input(down, time.Now())
	if m.Focus != 1 {
		t.Fatal("navigation lost")
	}
	m.Input(down, time.Now())
	if m.Focus != 1 {
		t.Fatal("held axis repeated")
	}
	a, _ := remoteinput.NormalizeGamepad("a", true)
	if action := m.Input(a, time.Now()); action != "launch" {
		t.Fatal(action)
	}
	m.Session.State = "active"
	b, _ := remoteinput.NormalizeGamepad("b", true)
	if action := m.Input(b, time.Now()); action != "" {
		t.Fatal("B stopped gameplay")
	}
	m.Connected = false
	m.Session.State = "idle"
	if m.Input(a, time.Now()) != "" {
		t.Fatal("offline launch")
	}
}

func TestFailedSessionCanRequestRecoveryStop(t *testing.T) {
	now := time.Now()
	m := Model{Session: Session{State: "failed"}}
	selectPress, _ := remoteinput.NormalizeGamepad("select", true)
	startPress, _ := remoteinput.NormalizeGamepad("start", true)
	m.Input(selectPress, now)
	m.Input(startPress, now)
	if action := m.Tick(now.Add(time.Second)); action != "stop" {
		t.Fatalf("failed-session recovery action = %q, want stop", action)
	}
}
