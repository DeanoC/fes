package tenfoot

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/rooms"
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

// Select on one pad and Start on another is two players playing. The
// kit-local stop chord needs both buttons on the same pad.
func TestLocalStopChordNeedsBothButtonsOnOnePad(t *testing.T) {
	feed := &fakePadFeed{}
	app := &App{localPhase: localPhaseRunning, localFeed: feed}
	t0 := time.Unix(10, 0)
	armed := func() bool {
		app.mu.Lock()
		defer app.mu.Unlock()
		return !app.localChordSince.IsZero()
	}
	app.HandleLocalPad(padButtonFor(0, remoteinput.ButtonSelect, true), t0)
	app.HandleLocalPad(padButtonFor(1, remoteinput.ButtonStart, true), t0)
	if armed() {
		t.Fatal("Select on pad 1 and Start on pad 2 armed the stop chord")
	}
	got := feed.snapshot()
	if len(got) != 2 || got[0].Player != 0 || got[0].Code != remoteinput.ButtonSelect || got[1].Player != 1 || got[1].Code != remoteinput.ButtonStart {
		t.Fatalf("cross-pad presses were not forwarded as two players: %+v", got)
	}
	// Pad 2 completes its own chord: its Start comes back up at the core.
	// Pad 1's Select stays down.
	app.HandleLocalPad(padButtonFor(1, remoteinput.ButtonSelect, true), t0.Add(10*time.Millisecond))
	app.mu.Lock()
	sentSelect := app.localSent[padKey{0, remoteinput.ButtonSelect}]
	app.mu.Unlock()
	if !armed() || !sentSelect {
		t.Fatalf("same-pad chord armed=%v, pad 1 Select still sent=%v", armed(), sentSelect)
	}
	got = feed.snapshot()
	if last := got[len(got)-1]; len(got) != 3 || last.Player != 1 || last.Code != remoteinput.ButtonStart || last.Action != remoteinput.ActionRelease {
		t.Fatalf("chord did not release only pad 2's Start: %+v", got)
	}
	// Pad 1 letting go reaches the core and leaves pad 2's chord armed.
	app.HandleLocalPad(padButtonFor(0, remoteinput.ButtonSelect, false), t0.Add(20*time.Millisecond))
	got = feed.snapshot()
	if last := got[len(got)-1]; !armed() || len(got) != 4 || last.Player != 0 || last.Code != remoteinput.ButtonSelect || last.Action != remoteinput.ActionRelease {
		t.Fatalf("pad 1 release: armed=%v events=%+v", armed(), got)
	}
	// Pad 2 letting go of Select cancels the chord and rearms it.
	app.HandleLocalPad(padButtonFor(1, remoteinput.ButtonSelect, false), t0.Add(30*time.Millisecond))
	if armed() {
		t.Fatal("releasing one chord button left the chord armed")
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
	t.Run("kit-local core", func(t *testing.T) {
		app := &App{localPhase: localPhaseRunning, localFeed: &fakePadFeed{}}
		hold(app, 0, t0)
		hold(app, 1, t0.Add(900*time.Millisecond))
		app.HandleLocalPad(padButtonFor(0, remoteinput.ButtonSelect, false), t0.Add(950*time.Millisecond))
		app.mu.Lock()
		defer app.mu.Unlock()
		app.tickLocalCoreLocked(t0.Add(time.Second))
		if app.localChordFired {
			t.Fatal("stop fired after pad 2 held its pair for 100 ms")
		}
		app.tickLocalCoreLocked(t0.Add(900*time.Millisecond + localChordHold))
		if !app.localChordFired {
			t.Fatal("pad 2's held pair never stopped the core")
		}
	})
	t.Run("later pad lets go", func(t *testing.T) {
		app := &App{localPhase: localPhaseRunning, localFeed: &fakePadFeed{}}
		hold(app, 0, t0)
		hold(app, 1, t0.Add(500*time.Millisecond))
		app.HandleLocalPad(padButtonFor(1, remoteinput.ButtonStart, false), t0.Add(600*time.Millisecond))
		app.mu.Lock()
		defer app.mu.Unlock()
		app.tickLocalCoreLocked(t0.Add(time.Second))
		if !app.localChordFired {
			t.Fatalf("pad 2 letting go postponed pad 1's stop to %v", app.localChordSince.Sub(t0))
		}
	})
	t.Run("earlier pad unplugged", func(t *testing.T) {
		app := &App{localPhase: localPhaseRunning, localFeed: &fakePadFeed{}}
		hold(app, 0, t0)
		hold(app, 1, t0.Add(900*time.Millisecond))
		app.mu.Lock()
		defer app.mu.Unlock()
		app.forgetPadLocked(0, t0.Add(950*time.Millisecond))
		app.tickLocalCoreLocked(t0.Add(time.Second))
		if app.localChordFired || !app.localChordSince.Equal(t0.Add(900*time.Millisecond)) {
			t.Fatalf("unplug kept the earlier pad's timer: fired=%v since=%v", app.localChordFired, app.localChordSince)
		}
	})
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
func TestReconciledLaunchForgetsHeldPairs(t *testing.T) {
	fake := &fakeLocalCores{err: localcores.ErrUnavailable}
	app := NewApp(nil, 1280, 720, 50)
	app.localCores = fake
	app.mu.Lock()
	app.startLocalCoreLocked(rooms.Destination{Label: "Pong", PackageID: tenfootPongID})
	app.mu.Unlock()
	waitUntil := func(what string, pred func() bool) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			app.mu.Lock()
			ok := pred()
			app.mu.Unlock()
			if ok {
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s", what)
	}
	waitUntil("reconcile fence", func() bool { return app.localReconcileAfterFailure && app.localPhase == localPhaseLaunching })
	app.HandleLocalPad(padButton(remoteinput.ButtonSelect, true), time.Now())
	app.HandleLocalPad(padButton(remoteinput.ButtonStart, true), time.Now())
	app.mu.Lock()
	app.pollLocalStatusLocked(time.Now())
	app.mu.Unlock()
	waitUntil("reconciled idle", func() bool { return app.localPhase == "" && !app.localReconcileAfterFailure })
	app.HandleLocalPad(padButton(remoteinput.ButtonSelect, false), time.Now())
	app.HandleLocalPad(padButton(remoteinput.ButtonStart, false), time.Now())
	app.mu.Lock()
	stale := app.localPair.anyDown()
	app.mu.Unlock()
	fake.mu.Lock()
	fake.err = nil
	fake.mu.Unlock()
	app.mu.Lock()
	app.startLocalCoreLocked(rooms.Destination{Label: "Pong", PackageID: tenfootPongID})
	app.mu.Unlock()
	waitUntil("running", func() bool { return app.localPhase == localPhaseRunning })
	app.mu.Lock()
	armed := !app.localChordSince.IsZero()
	app.tickLocalCoreLocked(time.Now().Add(2 * time.Second))
	app.mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	if stale || armed || fake.stopCount() != 0 {
		t.Fatalf("pair kept through idle=%v; next core armed=%v; stops=%d", stale, armed, fake.stopCount())
	}
}

// A launch that fails leaves no pair or sent state for the next launch.
func TestFailedLaunchForgetsHeldPairs(t *testing.T) {
	app := &App{localPhase: localPhaseLaunching}
	t0 := time.Unix(10, 0)
	app.HandleLocalPad(padButtonFor(0, remoteinput.ButtonSelect, true), t0)
	app.HandleLocalPad(padButtonFor(0, remoteinput.ButtonStart, true), t0)
	app.mu.Lock()
	app.localSent = map[padKey]bool{{1, remoteinput.ButtonA}: true}
	app.failLocalCoreLocked(errors.New("launch refused"), "")
	defer app.mu.Unlock()
	if app.localPair.anyDown() || app.localSent != nil {
		t.Fatalf("failed launch kept pad state: pair=%+v sent=%v", app.localPair, app.localSent)
	}
}

// While a session stops, pair releases are swallowed but still tracked; a
// session that ends forgets the pair. Either way a later lone press is not a
// chord.
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
		localSent:         map[padKey]bool{{0, remoteinput.ButtonA}: true, {1, remoteinput.ButtonA}: true},
	}
	for player := uint8(0); player < 2; player++ {
		app.playPadPair.set(player, remoteinput.ButtonSelect, true)
		app.localPair.set(player, remoteinput.ButtonStart, true)
	}
	app.forgetPadLocked(0, time.Now())
	if len(app.playPadHeld) != 1 || len(app.playPadSuppressed) != 1 || len(app.localSent) != 1 ||
		!app.playPadSuppressed[padKey{1, remoteinput.ButtonB}] || !app.localSent[padKey{1, remoteinput.ButtonA}] {
		t.Fatalf("forget touched pad 2: held=%v suppressed=%v sent=%v", app.playPadHeld, app.playPadSuppressed, app.localSent)
	}
	if app.playPadPair.at(0) != (chordPair{}) || app.localPair.at(0) != (chordPair{}) ||
		!app.playPadPair.at(1).selectDown || !app.localPair.at(1).startDown {
		t.Fatalf("pairs after forget: session=%+v local=%+v", app.playPadPair, app.localPair)
	}
}
