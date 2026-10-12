package kitlauncher

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
)

func TestShouldPaintHDMISkipsSplashIdleAndKeepsTemporaryOverlay(t *testing.T) {
	catalog := Model{
		Session:       Session{State: "idle"},
		Games:         []hostclient.Game{{ID: "sonic", Title: "Sonic", System: "megadrive"}},
		WheelOpen:     false,
		DetailOpen:    true,
		AttractActive: true,
		SearchOpen:    true,
		Message:       connectingMessage,
	}
	if ShouldPaintHDMI(catalog) {
		t.Fatal("confirmed idle without an HPS framebuffer painted kit chrome")
	}
	active := catalog
	active.Session.State = "active"
	active.Session.HPSFramebuffer = true
	active.Busy = false
	active.DetailOpen = false
	active.AttractActive = false
	if ShouldPaintHDMI(active) {
		t.Fatal("active session painted over the game")
	}
	local := active
	local.Session.State = "local"
	if ShouldPaintHDMI(local) {
		t.Fatal("kit-local session painted over the core")
	}
	local.Busy = true
	local.Message = "Stopping game"
	if !ShouldPaintHDMI(local) {
		t.Fatal("kit-local stopping feedback requires the temporary overlay")
	}
	for _, state := range []string{"launching", "stopping"} {
		startup := Model{Session: Session{State: state, HPSFramebuffer: true}}
		if !sessionHoldsMenu(state) || !sessionLocalLifecycle(state) {
			t.Fatalf("%s does not hold the menu through startup or idle load", state)
		}
		if sessionOccupiesHDMI(state) || !ShouldPaintHDMI(startup) {
			t.Fatalf("%s suppressed the linuxfb shelf; only a busy overlay should paint", state)
		}
	}
	loading := catalog
	loading.Session.HPSFramebuffer = true
	loading.Busy = true
	loading.Message = "Loading game"
	if !ShouldPaintHDMI(loading) {
		t.Fatal("HPS framebuffer idle should show loading feedback")
	}
	stopping := Model{Busy: true, Message: "Stopping game", Session: Session{State: "active", HPSFramebuffer: true}}
	if !ShouldPaintHDMI(stopping) {
		t.Fatal("stopping feedback requires the temporary overlay")
	}
	stopping.Session.HPSFramebuffer = false
	if ShouldPaintHDMI(stopping) {
		t.Fatal("splash idle painted stopping chrome")
	}
	shelf := Model{
		Session:   Session{State: "idle", HPSFramebuffer: true},
		WheelOpen: true,
		Message:   OfflineMessage,
		Games:     catalog.Games,
	}
	if !ShouldPaintHDMI(shelf) {
		t.Fatal("HPS framebuffer idle should allow the temporary shelf overlay")
	}
	connecting := Model{Message: connectingMessage, WheelOpen: true}
	if ShouldPaintHDMI(connecting) {
		t.Fatal("unconfirmed splash idle painted the connecting screen")
	}
}

func TestRunMenuDisplayPausesForKitLocalSession(t *testing.T) {
	oldObserve := launcherObserve
	oldPoll := launcherPollInterval
	defer func() {
		launcherObserve = oldObserve
		launcherPollInterval = oldPoll
	}()
	launcherPollInterval = 20 * time.Millisecond

	// 0: host idle, menu presents. 1: host local, menu must pause.
	// 2: host idle again, menu must resume and present.
	var phase atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/session":
			state := "idle"
			if phase.Load() == 1 {
				state = "local"
			}
			_, _ = w.Write([]byte(`{"state":"` + state + `","game_id":"should-not-survive"}`))
		case "/api/v1/health":
			_, _ = w.Write([]byte(`{"ready":true,"target":{"reachable":true,"ready":true}}`))
		case "/api/v1/platforms":
			_, _ = w.Write([]byte(`{"platforms":[{"id":"megadrive","game_count":1}]}`))
		case "/api/v1/games":
			_, _ = w.Write([]byte(`{"games":[{"id":"sonic","title":"Sonic","system":"megadrive","launchable":true}]}`))
		case "/api/v1/library/attract":
			_, _ = w.Write([]byte(`{"idle_seconds":60,"items":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	var pauses, resumes, idleBefore, localPaints, idleAfter atomic.Int64
	client := NewClient(Config{API: server.URL, MenuDisplay: true})
	client.SetMenuDisplayHandoff(func(context.Context) error {
		pauses.Add(1)
		return nil
	}, func() { resumes.Add(1) })
	launcherObserve = func(m Model) {
		if phase.Load() == 1 && m.Session.State == "local" && m.Connected && pauses.Load() > 0 {
			phase.Store(2)
		}
	}
	err := Run(ctx, client, func(m Model) {
		switch {
		case phase.Load() == 0 && m.Session.State == "idle" && m.Connected && !m.Busy:
			idleBefore.Add(1)
			phase.Store(1)
		case m.Session.State == "local" && !m.Busy:
			localPaints.Add(1)
		case phase.Load() == 2 && resumes.Load() > 0 && m.Session.State == "idle" && !m.Busy:
			idleAfter.Add(1)
			cancel()
		}
	}, func() (Pad, error) { return nil, errNoPad })
	if err != nil {
		t.Fatal(err)
	}
	if idleBefore.Load() == 0 {
		t.Fatal("menu display did not present while idle")
	}
	if pauses.Load() == 0 {
		t.Fatal("menu display was not paused for a kit-local session")
	}
	if localPaints.Load() != 0 {
		t.Fatalf("menu display presented %d frames during kit-local play", localPaints.Load())
	}
	if resumes.Load() == 0 || idleAfter.Load() == 0 {
		t.Fatalf("menu display did not resume after kit-local play: pauses=%d resumes=%d idleAfter=%d", pauses.Load(), resumes.Load(), idleAfter.Load())
	}
}

func TestRunMenuDisplayResumesWhenHostlessLocalCoreExits(t *testing.T) {
	oldObserve := launcherObserve
	oldPoll := launcherPollInterval
	defer func() {
		launcherObserve = oldObserve
		launcherPollInterval = oldPoll
	}()
	launcherPollInterval = 20 * time.Millisecond

	// 0: host reports local and the runtime core is bound; the menu pauses.
	// 1: the host is unreachable while that core stays bound; the menu stays paused.
	// 2: an in-flight Stop reports starting; the menu stays paused.
	// 3: idle load failed and the runtime is in recovery; the menu stays paused.
	// 4: status confirms a completed idle menu; the menu resumes and presents.
	var phase atomic.Int32
	var startingProbes, recoveryProbes atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if phase.Load() != 0 {
			http.Error(w, "host down", http.StatusBadGateway)
			return
		}
		switch r.URL.Path {
		case "/api/v1/session":
			_, _ = w.Write([]byte(`{"state":"local","game_id":"should-not-survive"}`))
		case "/api/v1/health":
			_, _ = w.Write([]byte(`{"ready":true,"target":{"reachable":true,"ready":true}}`))
		case "/api/v1/platforms":
			_, _ = w.Write([]byte(`{"platforms":[{"id":"megadrive","game_count":1}]}`))
		case "/api/v1/games":
			_, _ = w.Write([]byte(`{"games":[{"id":"sonic","title":"Sonic","system":"megadrive","launchable":true}]}`))
		case "/api/v1/library/attract":
			_, _ = w.Write([]byte(`{"idle_seconds":60,"items":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	var pauses, resumes, absentHolds, startingHolds, recoveryHolds, heldPaints, idleAfter atomic.Int64
	client := NewClient(Config{API: server.URL, MenuDisplay: true})
	client.localCore = func(context.Context) (LocalCoreSample, error) {
		switch phase.Load() {
		case 0, 1:
			return LocalCoreSample{Bound: true}, nil
		case 2:
			startingProbes.Add(1)
			return LocalCoreSample{}, nil
		case 3:
			recoveryProbes.Add(1)
			return LocalCoreSample{}, nil
		default:
			return LocalCoreSample{MenuIdle: true}, nil
		}
	}
	client.SetMenuDisplayHandoff(func(context.Context) error {
		pauses.Add(1)
		return nil
	}, func() { resumes.Add(1) })
	launcherObserve = func(m Model) {
		if resumes.Load() != 0 || m.Session.State != "local" {
			return
		}
		switch phase.Load() {
		case 0:
			if m.Connected && pauses.Load() > 0 {
				phase.Store(1)
			}
		case 1:
			if !m.Connected && pauses.Load() > 0 && absentHolds.Add(1) >= 8 {
				phase.Store(2)
			}
		case 2:
			if !m.Connected && startingProbes.Load() >= 1 && startingHolds.Add(1) >= 8 {
				phase.Store(3)
			}
		case 3:
			if !m.Connected && recoveryProbes.Load() >= 1 && recoveryHolds.Add(1) >= 8 {
				phase.Store(4)
			}
		}
	}
	err := Run(ctx, client, func(m Model) {
		switch phase.Load() {
		case 1, 2, 3:
			heldPaints.Add(1)
		case 4:
			if resumes.Load() > 0 && m.Session.State == "idle" && !m.Busy {
				idleAfter.Add(1)
				cancel()
			}
		}
	}, func() (Pad, error) { return nil, errNoPad })
	if err != nil {
		t.Fatal(err)
	}
	if pauses.Load() == 0 {
		t.Fatal("menu display was not paused for a kit-local session")
	}
	if absentHolds.Load() < 8 {
		t.Fatalf("host-absent local hold was not observed: holds=%d phase=%d", absentHolds.Load(), phase.Load())
	}
	if startingProbes.Load() < 1 || startingHolds.Load() < 8 {
		t.Fatalf("in-flight Stop did not keep the menu paused: probes=%d holds=%d resumes=%d phase=%d", startingProbes.Load(), startingHolds.Load(), resumes.Load(), phase.Load())
	}
	if recoveryProbes.Load() < 1 || recoveryHolds.Load() < 8 {
		t.Fatalf("failed idle load resumed the menu: probes=%d holds=%d resumes=%d phase=%d", recoveryProbes.Load(), recoveryHolds.Load(), resumes.Load(), phase.Load())
	}
	if heldPaints.Load() != 0 {
		t.Fatalf("menu display presented %d frames before the idle menu was confirmed", heldPaints.Load())
	}
	if resumes.Load() == 0 || idleAfter.Load() == 0 {
		t.Fatalf("menu display did not resume after the idle menu was confirmed: pauses=%d resumes=%d idleAfter=%d phase=%d", pauses.Load(), resumes.Load(), idleAfter.Load(), phase.Load())
	}
}

func TestRunMenuDisplayHoldsAcrossLocalStartupIdleLoadAndResume(t *testing.T) {
	oldObserve := launcherObserve
	oldPoll := launcherPollInterval
	defer func() {
		launcherObserve = oldObserve
		launcherPollInterval = oldPoll
	}()
	launcherPollInterval = 20 * time.Millisecond

	// 0: host idle, menu presents.
	// 1: kit-local startup (launching). The menu pauses and does not present.
	// 2: the core is running (local). The menu stays paused.
	// 3: idle load (stopping). The menu stays paused.
	// 4: the host reports idle. The menu resumes and presents.
	var phase atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := "idle"
		switch phase.Load() {
		case 1:
			state = "launching"
		case 2:
			state = "local"
		case 3:
			state = "stopping"
		}
		switch r.URL.Path {
		case "/api/v1/session":
			_, _ = w.Write([]byte(`{"state":"` + state + `"}`))
		case "/api/v1/health":
			_, _ = w.Write([]byte(`{"ready":true,"target":{"reachable":true,"ready":true}}`))
		case "/api/v1/platforms":
			_, _ = w.Write([]byte(`{"platforms":[{"id":"megadrive","game_count":1}]}`))
		case "/api/v1/games":
			_, _ = w.Write([]byte(`{"games":[{"id":"sonic","title":"Sonic","system":"megadrive","launchable":true}]}`))
		case "/api/v1/library/attract":
			_, _ = w.Write([]byte(`{"idle_seconds":60,"items":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	var pauses, resumes, startupHolds, runningHolds, stoppingHolds, heldPaints, idleBefore, idleAfter atomic.Int64
	client := NewClient(Config{API: server.URL, MenuDisplay: true})
	client.SetMenuDisplayHandoff(func(context.Context) error {
		pauses.Add(1)
		return nil
	}, func() { resumes.Add(1) })
	launcherObserve = func(m Model) {
		if resumes.Load() != 0 {
			return
		}
		switch phase.Load() {
		case 1:
			if m.Session.State == "launching" && pauses.Load() > 0 && startupHolds.Add(1) >= 4 {
				phase.Store(2)
			}
		case 2:
			if m.Session.State == "local" && runningHolds.Add(1) >= 4 {
				phase.Store(3)
			}
		case 3:
			if m.Session.State == "stopping" && stoppingHolds.Add(1) >= 4 {
				phase.Store(4)
			}
		}
	}
	err := Run(ctx, client, func(m Model) {
		switch {
		case phase.Load() == 0 && m.Session.State == "idle" && m.Connected && !m.Busy:
			idleBefore.Add(1)
			phase.Store(1)
		case m.Session.State == "launching" || m.Session.State == "local" || m.Session.State == "stopping":
			heldPaints.Add(1)
		case phase.Load() == 4 && resumes.Load() > 0 && m.Session.State == "idle" && !m.Busy:
			idleAfter.Add(1)
			cancel()
		}
	}, func() (Pad, error) { return nil, errNoPad })
	if err != nil {
		t.Fatal(err)
	}
	if idleBefore.Load() == 0 {
		t.Fatal("menu display did not present while idle")
	}
	if pauses.Load() == 0 || startupHolds.Load() < 4 {
		t.Fatalf("menu display was not paused for kit-local startup: pauses=%d holds=%d phase=%d", pauses.Load(), startupHolds.Load(), phase.Load())
	}
	if runningHolds.Load() < 4 {
		t.Fatalf("running kit-local session did not keep the pause: holds=%d resumes=%d phase=%d", runningHolds.Load(), resumes.Load(), phase.Load())
	}
	if stoppingHolds.Load() < 4 {
		t.Fatalf("idle load resumed the menu: holds=%d resumes=%d phase=%d", stoppingHolds.Load(), resumes.Load(), phase.Load())
	}
	if heldPaints.Load() != 0 {
		t.Fatalf("menu display presented %d frames during startup, play, or idle load", heldPaints.Load())
	}
	if resumes.Load() == 0 || idleAfter.Load() == 0 {
		t.Fatalf("menu display did not resume after idle: pauses=%d resumes=%d idleAfter=%d phase=%d", pauses.Load(), resumes.Load(), idleAfter.Load(), phase.Load())
	}
}

func TestRunMenuDisplayHostlessResumeFollowsPostHoldIdle(t *testing.T) {
	oldObserve := launcherObserve
	oldPoll := launcherPollInterval
	defer func() {
		launcherObserve = oldObserve
		launcherPollInterval = oldPoll
	}()
	launcherPollInterval = 20 * time.Millisecond

	// 0: the host reports local while the probe still reads the shelf idle
	// menu. That sample is from before the hold and must not resume it.
	// 1: the host is unreachable and the runtime is starting. Hold.
	// 2: idle load failed and the runtime is in recovery. Hold.
	// 3: a probe taken during the hold confirms a completed idle menu. Resume.
	// The core is never reported bound: startup can finish before a running probe.
	var phase atomic.Int32
	var shelfProbes, startingProbes, recoveryProbes atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if phase.Load() != 0 {
			http.Error(w, "host down", http.StatusBadGateway)
			return
		}
		switch r.URL.Path {
		case "/api/v1/session":
			_, _ = w.Write([]byte(`{"state":"local"}`))
		case "/api/v1/health":
			_, _ = w.Write([]byte(`{"ready":true,"target":{"reachable":true,"ready":true}}`))
		case "/api/v1/platforms":
			_, _ = w.Write([]byte(`{"platforms":[{"id":"megadrive","game_count":1}]}`))
		case "/api/v1/games":
			_, _ = w.Write([]byte(`{"games":[{"id":"sonic","title":"Sonic","system":"megadrive","launchable":true}]}`))
		case "/api/v1/library/attract":
			_, _ = w.Write([]byte(`{"idle_seconds":60,"items":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	var pauses, resumes, startingHolds, recoveryHolds, heldPaints, idleAfter atomic.Int64
	client := NewClient(Config{API: server.URL, MenuDisplay: true})
	client.localCore = func(context.Context) (LocalCoreSample, error) {
		switch phase.Load() {
		case 0:
			shelfProbes.Add(1)
			return LocalCoreSample{MenuIdle: true}, nil
		case 1:
			startingProbes.Add(1)
			return LocalCoreSample{}, nil
		case 2:
			recoveryProbes.Add(1)
			return LocalCoreSample{}, nil
		default:
			return LocalCoreSample{MenuIdle: true}, nil
		}
	}
	client.SetMenuDisplayHandoff(func(context.Context) error {
		pauses.Add(1)
		phase.Store(1)
		return nil
	}, func() { resumes.Add(1) })
	launcherObserve = func(m Model) {
		if resumes.Load() != 0 || m.Session.State != "local" || m.Connected {
			return
		}
		switch phase.Load() {
		case 1:
			if startingProbes.Load() >= 1 && startingHolds.Add(1) >= 8 {
				phase.Store(2)
			}
		case 2:
			if recoveryProbes.Load() >= 1 && recoveryHolds.Add(1) >= 8 {
				phase.Store(3)
			}
		}
	}
	err := Run(ctx, client, func(m Model) {
		switch phase.Load() {
		case 1, 2:
			heldPaints.Add(1)
		case 3:
			if resumes.Load() > 0 && m.Session.State == "idle" && !m.Busy {
				idleAfter.Add(1)
				cancel()
			}
		}
	}, func() (Pad, error) { return nil, errNoPad })
	if err != nil {
		t.Fatal(err)
	}
	if shelfProbes.Load() == 0 || pauses.Load() == 0 {
		t.Fatalf("hold did not start after a shelf idle sample: probes=%d pauses=%d", shelfProbes.Load(), pauses.Load())
	}
	if startingProbes.Load() < 1 || startingHolds.Load() < 8 {
		t.Fatalf("startup did not keep the menu paused: probes=%d holds=%d resumes=%d phase=%d", startingProbes.Load(), startingHolds.Load(), resumes.Load(), phase.Load())
	}
	if recoveryProbes.Load() < 1 || recoveryHolds.Load() < 8 {
		t.Fatalf("recovery resumed the menu: probes=%d holds=%d resumes=%d phase=%d", recoveryProbes.Load(), recoveryHolds.Load(), resumes.Load(), phase.Load())
	}
	if heldPaints.Load() != 0 {
		t.Fatalf("menu display presented %d frames before the idle menu was confirmed", heldPaints.Load())
	}
	if resumes.Load() == 0 || idleAfter.Load() == 0 {
		t.Fatalf("menu display did not resume after the post-hold idle menu: pauses=%d resumes=%d idleAfter=%d phase=%d", pauses.Load(), resumes.Load(), idleAfter.Load(), phase.Load())
	}
}

func TestRunMenuDisplayPaintsIdleWithoutFramebuffer(t *testing.T) {
	server := idleFramebufferServer(t, ``)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	var paints atomic.Int64
	client := NewClient(Config{API: server.URL, MenuDisplay: true})
	client.SetMenuDisplayHandoff(func(context.Context) error { return nil }, func() {})
	err := Run(ctx, client, func(m Model) {
		if m.Session.State == "idle" && !m.Session.HPSFramebuffer {
			paints.Add(1)
		}
	}, func() (Pad, error) { return nil, errNoPad })
	if err != nil {
		t.Fatal(err)
	}
	if paints.Load() == 0 {
		t.Fatal("native menu did not paint idle without linuxfb")
	}
}

func TestApplyObservedSessionKeepsOmittedFramebufferAndHonorsExplicitFalse(t *testing.T) {
	current := Session{State: "idle", HPSFramebuffer: true}
	omitted := adaptSession(hostclient.SessionResult{State: "idle"})
	if omitted.HPSFramebufferKnown {
		t.Fatal("omitted hps_framebuffer was treated as known")
	}
	merged := applyObservedSession(current, omitted)
	if !merged.HPSFramebuffer || merged.State != "idle" {
		t.Fatalf("omitted observation dropped the kit bit: %+v", merged)
	}
	off := false
	explicit := adaptSession(hostclient.SessionResult{State: "idle", HPSFramebuffer: &off})
	if !explicit.HPSFramebufferKnown || explicit.HPSFramebuffer {
		t.Fatalf("explicit false = %+v", explicit)
	}
	merged = applyObservedSession(current, explicit)
	if merged.HPSFramebuffer {
		t.Fatal("explicit false did not replace the kit bit")
	}
	on := true
	splash := Session{State: "idle"}
	merged = applyObservedSession(splash, adaptSession(hostclient.SessionResult{State: "idle", HPSFramebuffer: &on}))
	if !merged.HPSFramebuffer {
		t.Fatal("explicit true did not enable the temporary overlay")
	}
}

func TestRunSkipsPresentAfterConfirmedIdleWithoutHPSFramebuffer(t *testing.T) {
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previous)

	server := idleFramebufferServer(t, ``)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()
	var paints atomic.Int64
	var idlePaints atomic.Int64
	err := Run(ctx, NewClient(Config{API: server.URL}), func(m Model) {
		paints.Add(1)
		if m.Session.State == "idle" {
			idlePaints.Add(1)
		}
	}, func() (Pad, error) { return nil, errNoPad })
	if err != nil {
		t.Fatal(err)
	}
	if paints.Load() != 0 || idlePaints.Load() != 0 {
		t.Fatalf("splash idle painted %d frames (%d idle)", paints.Load(), idlePaints.Load())
	}
	got := logs.String()
	want := "kit hdmi: confirmed idle without HPS framebuffer (no 0x002f); leaving splash visible"
	if strings.Count(got, want) != 1 {
		t.Fatalf("splash log count=%d\n%s", strings.Count(got, want), got)
	}
}

func TestRunPaintsTemporaryOverlayWhenIdleEnablesHPSFramebuffer(t *testing.T) {
	server := idleFramebufferServer(t, ``)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()
	var idlePaints atomic.Int64
	err := Run(ctx, NewClient(Config{API: server.URL, HPSFramebuffer: true}), func(m Model) {
		if m.Session.State == "idle" && !m.Busy && m.WheelOpen {
			idlePaints.Add(1)
			cancel()
		}
	}, func() (Pad, error) { return nil, errNoPad })
	if err != nil {
		t.Fatal(err)
	}
	if idlePaints.Load() == 0 {
		t.Fatal("HPS framebuffer idle did not present the temporary shelf")
	}
}

func TestRunSessionFramebufferOverridesLauncherConfig(t *testing.T) {
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previous)

	server := idleFramebufferServer(t, `,"hps_framebuffer":false`)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()
	var idlePaints atomic.Int64
	err := Run(ctx, NewClient(Config{API: server.URL, HPSFramebuffer: true}), func(m Model) {
		if m.Session.State == "idle" {
			idlePaints.Add(1)
		}
	}, func() (Pad, error) { return nil, errNoPad })
	if err != nil {
		t.Fatal(err)
	}
	if idlePaints.Load() != 0 {
		t.Fatalf("session hps_framebuffer=false still painted %d idle frames", idlePaints.Load())
	}
	if !strings.Contains(logs.String(), "leaving splash visible") {
		t.Fatalf("missing splash log\n%s", logs.String())
	}

	enabled := idleFramebufferServer(t, `,"hps_framebuffer":true`)
	defer enabled.Close()
	ctx, cancel = context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()
	idlePaints.Store(0)
	err = Run(ctx, NewClient(Config{API: enabled.URL}), func(m Model) {
		if m.Session.State == "idle" && m.Session.HPSFramebuffer {
			idlePaints.Add(1)
			cancel()
		}
	}, func() (Pad, error) { return nil, errNoPad })
	if err != nil {
		t.Fatal(err)
	}
	if idlePaints.Load() == 0 {
		t.Fatal("session hps_framebuffer=true did not present")
	}
}

func TestActiveSessionSkipsPresentEvenWhenHPSFramebufferEnabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/session":
			_, _ = w.Write([]byte(`{"state":"active","game_id":"sonic","hps_framebuffer":true}`))
		case "/api/v1/health":
			_, _ = w.Write([]byte(`{"ready":true,"target":{"reachable":true,"ready":true}}`))
		case "/api/v1/platforms":
			_, _ = w.Write([]byte(`{"platforms":[]}`))
		case "/api/v1/games":
			_, _ = w.Write([]byte(`{"games":[]}`))
		case "/api/v1/library/attract":
			_, _ = w.Write([]byte(`{"idle_seconds":60,"items":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 900*time.Millisecond)
	defer cancel()
	var activePaints atomic.Int64
	err := Run(ctx, NewClient(Config{API: server.URL, HPSFramebuffer: true}), func(m Model) {
		if m.Session.State == "active" && !m.Busy {
			activePaints.Add(1)
		}
	}, func() (Pad, error) { return nil, errNoPad })
	if err != nil {
		t.Fatal(err)
	}
	if activePaints.Load() != 0 {
		t.Fatalf("active session painted %d frames", activePaints.Load())
	}
}

var errNoPad = errPad("no pad")

type errPad string

func (e errPad) Error() string { return string(e) }

func idleFramebufferServer(t *testing.T, extra string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/session":
			_, _ = w.Write([]byte(`{"state":"idle"` + extra + `}`))
		case "/api/v1/health":
			_, _ = w.Write([]byte(`{"ready":true,"target":{"reachable":true,"ready":true}}`))
		case "/api/v1/platforms":
			_, _ = w.Write([]byte(`{"platforms":[{"id":"megadrive","game_count":1}]}`))
		case "/api/v1/games":
			_, _ = w.Write([]byte(`{"games":[{"id":"sonic","title":"Sonic","system":"megadrive","launchable":true}]}`))
		case "/api/v1/library/attract":
			_, _ = w.Write([]byte(`{"idle_seconds":60,"items":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
}
