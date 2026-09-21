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
