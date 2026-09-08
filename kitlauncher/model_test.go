package kitlauncher

import (
	"testing"
	"time"

	"github.com/DeanoC/FogCast/host/tenfoot"
	"github.com/DeanoC/FogCast/host/tenfoot/inputmap"
	"github.com/DeanoC/FogCast/remoteinput"
)

func TestMenuAndGameControlsRemainSeparate(t *testing.T) {
	m := Model{Games: []tenfoot.Game{{ID: "pong", Launchable: true}, {ID: "sonic", Launchable: true}}, Connected: true, TargetReady: true}
	right, _ := remoteinput.NormalizeAxis("left-x", 32767)
	m.Input(right, time.Now())
	if m.Focus != 1 {
		t.Fatal("navigation lost")
	}
	m.Input(right, time.Now())
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

func TestCatalogGridNavigationClamps(t *testing.T) {
	m := Model{Games: makeGames(25), Connected: true, TargetReady: true}
	now := time.Now()
	press := func(name string) {
		e, err := remoteinput.NormalizeGamepad(name, true)
		if err != nil {
			t.Fatal(err)
		}
		m.Input(e, now)
	}
	press("dpad-right")
	if m.Focus != 1 {
		t.Fatalf("right %d", m.Focus)
	}
	for i := 0; i < 8; i++ {
		press("dpad-right")
	}
	if m.Focus != 3 {
		t.Fatalf("row clamp %d", m.Focus)
	}
	press("dpad-left")
	if m.Focus != 2 {
		t.Fatalf("left %d", m.Focus)
	}
	m.Focus = 0
	press("dpad-left")
	if m.Focus != 0 {
		t.Fatalf("left catalog clamp wrapped %d", m.Focus)
	}
	press("dpad-up")
	if m.Focus != 0 {
		t.Fatalf("up catalog clamp wrapped %d", m.Focus)
	}
	press("dpad-down")
	if m.Focus != 4 {
		t.Fatalf("down by columns %d", m.Focus)
	}
	m.Focus = 11
	press("dpad-down")
	if m.Focus != 15 {
		t.Fatalf("page boundary %d", m.Focus)
	}
	m.Focus = 24
	press("dpad-right")
	if m.Focus != 24 {
		t.Fatalf("last-row clamp %d", m.Focus)
	}
	press("dpad-down")
	if m.Focus != 24 {
		t.Fatalf("last-row down clamp %d", m.Focus)
	}
	press("dpad-up")
	if m.Focus != 20 {
		t.Fatalf("up from last row %d", m.Focus)
	}
}

func TestAnalogStickRisingEdgeDoesNotSpam(t *testing.T) {
	m := Model{Games: makeGames(25), Connected: true, TargetReady: true}
	now := time.Now()
	right, _ := remoteinput.NormalizeAxis("left-x", 32767)
	down, _ := remoteinput.NormalizeAxis("left-y", 32767)
	idleX, _ := remoteinput.NormalizeAxis("left-x", 0)
	idleY, _ := remoteinput.NormalizeAxis("left-y", 0)
	m.Input(right, now)
	m.Input(right, now)
	m.Input(right, now)
	if m.Focus != 1 {
		t.Fatalf("held X %d", m.Focus)
	}
	m.Input(idleX, now)
	m.Input(down, now)
	m.Input(down, now)
	if m.Focus != 5 {
		t.Fatalf("held Y %d", m.Focus)
	}
	m.Input(idleY, now)
	left, _ := remoteinput.NormalizeAxis("left-x", -32767)
	m.Input(left, now)
	if m.Focus != 4 {
		t.Fatalf("left stick %d", m.Focus)
	}
}

func makeGames(n int) []tenfoot.Game {
	games := make([]tenfoot.Game, n)
	for i := range games {
		games[i] = tenfoot.Game{ID: "g" + string(rune('a'+i%26)), Launchable: true}
	}
	return games
}

func TestIdentityRemapStillLaunchesOnA(t *testing.T) {
	r := mustRemapper(t, "")
	m := Model{Games: []tenfoot.Game{{ID: "pong", Launchable: true}}, Connected: true, TargetReady: true}
	a, _ := remoteinput.NormalizeGamepad("a", true)
	if action := m.Input(r.Apply(a), time.Now()); action != "launch" {
		t.Fatalf("identity launch %q", action)
	}
}

func TestSwapABRemapLaunchesOnB(t *testing.T) {
	r := mustRemapper(t, "swap-ab")
	m := Model{Games: []tenfoot.Game{{ID: "pong", Launchable: true}}, Connected: true, TargetReady: true}
	a, _ := remoteinput.NormalizeGamepad("a", true)
	if action := m.Input(r.Apply(a), time.Now()); action != "" {
		t.Fatalf("A launched under swap-ab: %q", action)
	}
	b, _ := remoteinput.NormalizeGamepad("b", true)
	if action := m.Input(r.Apply(b), time.Now()); action != "launch" {
		t.Fatalf("B launch %q", action)
	}
}

func mustRemapper(t *testing.T, spec string) *inputmap.Remapper {
	t.Helper()
	p, err := inputmap.Resolve(spec)
	if err != nil {
		t.Fatal(err)
	}
	r, err := inputmap.NewRemapper(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
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
