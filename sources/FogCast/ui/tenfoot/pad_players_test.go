package tenfoot

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/remoteinput"
)

func padButtonFor(player uint8, code remoteinput.Code, down bool) remoteinput.Event {
	e := padButton(code, down)
	e.Player = player
	return e
}

func playerEvents(events []remoteinput.Event, player uint8) []remoteinput.Event {
	var out []remoteinput.Event
	for _, e := range events {
		if e.Player == player {
			out = append(out, e)
		}
	}
	return out
}

func TestChordPairsTrackEachPad(t *testing.T) {
	var pairs chordPairs
	if pairs.set(0, remoteinput.ButtonSelect, true) || pairs.set(1, remoteinput.ButtonStart, true) {
		t.Fatal("Select on one pad and Start on another made a chord")
	}
	if pairs.anyHeld() || !pairs.anyDown() {
		t.Fatal("cross-pad buttons misreported")
	}
	if !pairs.set(1, remoteinput.ButtonSelect, true) || !pairs.anyHeld() || !pairs.at(1).held() || pairs.at(0).held() {
		t.Fatal("same-pad pair did not chord")
	}
	pairs.clear(1)
	if pairs.anyHeld() || !pairs.anyDown() || !pairs.at(0).selectDown {
		t.Fatal("clear touched the other pad")
	}
	if pairs.set(2, remoteinput.ButtonStart, true) || pairs.at(2).held() {
		t.Fatal("out-of-range player was tracked")
	}
	pairs.set(0, remoteinput.ButtonA, false)
	if !pairs.at(0).selectDown {
		t.Fatal("a non-pair button changed the pair")
	}
}

// The same rule holds for a library session with a session display.
func TestSessionStopChordNeedsBothButtonsOnOnePad(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(http.NotFound))
	t.Cleanup(server.Close)
	app, feed := kitDisplayApp(t, server)
	now := time.Now()
	armed := func() bool {
		app.mu.Lock()
		defer app.mu.Unlock()
		return !app.playPadChordSince.IsZero()
	}
	app.HandleLocalPad(padButtonFor(0, remoteinput.ButtonSelect, true), now)
	app.HandleLocalPad(padButtonFor(1, remoteinput.ButtonStart, true), now)
	if armed() {
		t.Fatal("Select on pad 1 and Start on pad 2 armed the session stop chord")
	}
	if got := playerEvents(feed.snapshot(), 1); len(got) != 1 || got[0].Code != remoteinput.ButtonStart || got[0].Action != remoteinput.ActionPress {
		t.Fatalf("pad 2 Start did not reach the machine: %+v", got)
	}
	before := len(playerEvents(feed.snapshot(), 0))
	app.HandleLocalPad(padButtonFor(1, remoteinput.ButtonSelect, true), now)
	if !armed() {
		t.Fatal("same-pad chord did not arm")
	}
	got := feed.snapshot()
	if len(playerEvents(got, 0)) != before {
		t.Fatalf("pad 2's chord disturbed pad 1: %+v", got)
	}
	if last := got[len(got)-2:]; last[0].Player != 1 || last[0].Code != remoteinput.ButtonStart || last[0].Action != remoteinput.ActionRelease ||
		last[1].Player != 1 || last[1].Code != remoteinput.ButtonSelect || last[1].Action != remoteinput.ActionRelease {
		t.Fatalf("chord did not release pad 2's pair at the machine: %+v", got)
	}
}

// A chord on one pad must not swallow the other pad's releases. Pad 1 holds
// A while pad 2 fumbles Select+Start; pad 1's A still comes up at the machine.
func TestSessionChordLeavesTheOtherPadsHoldsAlone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(http.NotFound))
	t.Cleanup(server.Close)
	app, feed := kitDisplayApp(t, server)
	now := time.Now()
	app.HandleLocalPad(padButtonFor(0, remoteinput.ButtonA, true), now)
	app.HandleLocalPad(padButtonFor(1, remoteinput.ButtonSelect, true), now)
	app.HandleLocalPad(padButtonFor(1, remoteinput.ButtonStart, true), now)
	app.HandleLocalPad(padButtonFor(1, remoteinput.ButtonStart, false), now.Add(500*time.Millisecond))
	app.HandleLocalPad(padButtonFor(0, remoteinput.ButtonA, false), now.Add(600*time.Millisecond))
	got := playerEvents(feed.snapshot(), 0)
	if len(got) != 2 || got[1].Code != remoteinput.ButtonA || got[1].Action != remoteinput.ActionRelease {
		t.Fatalf("pad 1's A did not come up at the machine: %+v", feed.snapshot())
	}
}

// Pad 1 arms its chord, pad 2 arms its own later, then pad 1 lets go. The stop
// waits until pad 2 has held its pair for a full second. Pad 2 letting go
// instead must not postpone pad 1's stop.
func TestStopChordTimerFollowsThePadStillHolding(t *testing.T) {
	t0 := time.Unix(10, 0)
	hold := func(a *App, player uint8, at time.Time) {
		a.HandleLocalPad(padButtonFor(player, remoteinput.ButtonSelect, true), at)
		a.HandleLocalPad(padButtonFor(player, remoteinput.ButtonStart, true), at)
	}
	t.Run("library session", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(http.NotFound))
		t.Cleanup(server.Close)
		app, _ := kitDisplayApp(t, server)
		hold(app, 0, t0)
		hold(app, 1, t0.Add(900*time.Millisecond))
		app.HandleLocalPad(padButtonFor(0, remoteinput.ButtonSelect, false), t0.Add(950*time.Millisecond))
		app.mu.Lock()
		defer app.mu.Unlock()
		app.tickSessionPadLocked(t0.Add(time.Second))
		if app.playPadChordFired || !app.playPadChordSince.Equal(t0.Add(900*time.Millisecond)) {
			t.Fatalf("session chord fired=%v since=%v", app.playPadChordFired, app.playPadChordSince)
		}
	})
	t.Run("library session, later pad lets go", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(http.NotFound))
		t.Cleanup(server.Close)
		app, _ := kitDisplayApp(t, server)
		hold(app, 0, t0)
		hold(app, 1, t0.Add(500*time.Millisecond))
		app.HandleLocalPad(padButtonFor(1, remoteinput.ButtonStart, false), t0.Add(600*time.Millisecond))
		app.mu.Lock()
		defer app.mu.Unlock()
		if !app.playPadChordSince.Equal(t0) {
			t.Fatalf("pad 2 letting go moved pad 1's session hold to %v", app.playPadChordSince.Sub(t0))
		}
	})
}

// A launch whose answer was lost reconciles to idle while a pad holds
// Select+Start, and the pad lets go on the menu. Idle does not track pairs,
// so the pair must not survive to stop the next core with nothing held.
func TestSessionPairFollowsTheHardwareThroughStopAndEnd(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(http.NotFound))
	t.Cleanup(server.Close)
	app, _ := kitDisplayApp(t, server)
	now := time.Now()
	app.mu.Lock()
	app.playPadPair.set(0, remoteinput.ButtonSelect, true)
	app.playPadPair.set(0, remoteinput.ButtonStart, true)
	app.stopPhase = "stopping"
	app.mu.Unlock()
	app.HandleLocalPad(padButtonFor(0, remoteinput.ButtonSelect, false), now)
	app.HandleLocalPad(padButtonFor(0, remoteinput.ButtonStart, false), now)
	app.mu.Lock()
	if app.playPadPair.anyDown() {
		app.mu.Unlock()
		t.Fatalf("releases during stop were not tracked: %+v", app.playPadPair)
	}
	app.playPadPair.set(1, remoteinput.ButtonStart, true)
	app.invalidateSessionDisplayLocked()
	defer app.mu.Unlock()
	if app.playPadPair.anyDown() {
		t.Fatalf("session end kept a pair: %+v", app.playPadPair)
	}
}

// Unplugging a pad forgets only that pad's state, for the session and the
// kit-local core alike.
func TestForgetPadKeepsTheOtherPad(t *testing.T) {
	a, b := padButtonFor(0, remoteinput.ButtonA, true), padButtonFor(1, remoteinput.ButtonA, true)
	app := &App{
		playPadHeld:       map[padKey]remoteinput.Event{{0, a.Code}: a, {1, b.Code}: b},
		playPadSuppressed: map[padKey]bool{{0, remoteinput.ButtonB}: true, {1, remoteinput.ButtonB}: true},
	}
	for player := uint8(0); player < 2; player++ {
		app.playPadPair.set(player, remoteinput.ButtonSelect, true)
	}
	app.forgetPadLocked(0, time.Now())
	if len(app.playPadHeld) != 1 || len(app.playPadSuppressed) != 1 ||
		!app.playPadSuppressed[padKey{1, remoteinput.ButtonB}] {
		t.Fatalf("forget touched pad 2: held=%v suppressed=%v", app.playPadHeld, app.playPadSuppressed)
	}
	if app.playPadPair.at(0) != (chordPair{}) ||
		!app.playPadPair.at(1).selectDown {
		t.Fatalf("pairs after forget: session=%+v", app.playPadPair)
	}
}
