package kitlauncher

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/remoteinput"
)

func transitionClock(t *testing.T) func(time.Duration) {
	t.Helper()
	oldNow, oldObserve := launcherNow, launcherObserve
	oldStatus, oldPoll := launcherStatusInterval, launcherPollInterval
	var mu sync.Mutex
	now := time.Unix(1000, 0)
	launcherNow = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	launcherStatusInterval, launcherPollInterval = 20*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() {
		launcherNow = oldNow
		launcherObserve = oldObserve
		launcherStatusInterval = oldStatus
		launcherPollInterval = oldPoll
	})
	return func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }
}

type transitionPad struct {
	ready, chord func() bool
	presses      int
	chordSent    bool
}

func (p *transitionPad) Poll() ([]remoteinput.Event, error) {
	if p.ready != nil && p.ready() && p.presses < 2 {
		p.presses++
		a, _ := remoteinput.NormalizeGamepad("a", true)
		up, _ := remoteinput.NormalizeGamepad("a", false)
		return []remoteinput.Event{a, up}, nil
	}
	if p.presses == 2 && p.chord != nil && p.chord() && !p.chordSent {
		p.chordSent = true
		s, _ := remoteinput.NormalizeGamepad("select", true)
		a, _ := remoteinput.NormalizeGamepad("start", true)
		return []remoteinput.Event{s, a}, nil
	}
	return nil, nil
}
func (*transitionPad) Close() error { return nil }

type transitionCore struct {
	mu                     sync.Mutex
	status                 localcores.RunStatus
	launchErr              error
	launches, stops, reads int
}

func (*transitionCore) List(context.Context) ([]localcores.Core, error) {
	return []localcores.Core{{CoreID: "fes.sms", PackageID: strings.Repeat("a", 64)}}, nil
}
func (f *transitionCore) LaunchROM(context.Context, string, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.launches++
	if f.launchErr == nil {
		f.status = localcores.RunStatus{Phase: "launching"}
	}
	return f.launchErr
}
func (f *transitionCore) Status(context.Context) (localcores.RunStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	return f.status, nil
}
func (f *transitionCore) Stop(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	f.status = localcores.RunStatus{Phase: "idle"}
	return nil
}
func (f *transitionCore) set(phase string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = localcores.RunStatus{Phase: phase, Running: phase == "running"}
}
func (f *transitionCore) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.launches, f.stops
}
func (f *transitionCore) readsCount() int { f.mu.Lock(); defer f.mu.Unlock(); return f.reads }

func localTransitionClient(t *testing.T, f *transitionCore) *Client {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "sms")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Data Storm.sms"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "catalog.toml")
	body := "base_url = \"http://127.0.0.1:1\"\ntoken = \"synthetic-token\"\nrequest_timeout_seconds = 1\nupload_timeout_seconds = 2\n\n[[libraries]]\nid = \"sms-main\"\nsystem = \"sms\"\nroot = \"" + root + "\"\n"
	if err := os.WriteFile(cfg, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	c := NewClient(Config{API: "http://127.0.0.1:1", MenuDisplay: true})
	c.SetCatalogConfig(cfg)
	c.SetLocalCores(f)
	c.localCore = func(context.Context) (bool, error) { return false, nil }
	return c
}

func TestRunLocalStateTransitions(t *testing.T) {
	for _, tc := range []struct {
		name, phase             string
		timeout, lateIdle, stop bool
	}{
		{name: "running", phase: "running"}, {name: "failure", phase: "error"},
		{name: "timeout_then_running", phase: "running", timeout: true},
		{name: "timeout_then_idle", phase: "idle", timeout: true, lateIdle: true},
		{name: "deferred_stop", phase: "running", stop: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			advance := transitionClock(t)
			f := &transitionCore{status: localcores.RunStatus{Phase: "idle"}}
			if tc.phase == "error" {
				f.launchErr = errors.New("missing cartridge")
			}
			c := localTransitionClient(t, f)
			var pauses, resumes int
			c.SetMenuDisplayHandoff(func(context.Context) error { pauses++; return nil }, func() { resumes++ })
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			pad := &transitionPad{}
			var last Model
			var sawLoading, sawTimeout bool
			var timeoutReads int
			pad.ready = func() bool { return len(last.Games) > 0 && !sawLoading }
			pad.chord = func() bool { return tc.stop && sawLoading }
			launcherObserve = func(m Model) {
				last = m
				if m.Session.State == "launching" {
					sawLoading = true
				}
				launches, stops := f.counts()
				if tc.timeout && launches > 0 && m.Session.State == "launching" && !sawTimeout {
					advance(localLoadTimeout + launchTimeoutGrace + time.Second)
				}
				if strings.Contains(m.Message, "took too long") {
					if !sawTimeout {
						timeoutReads = f.readsCount()
					}
					sawTimeout = true
					if tc.lateIdle {
						f.set("idle")
					} else {
						f.set("running")
					}
				}
				if launches > 0 && !tc.timeout && tc.phase == "running" && sawLoading {
					f.set("running")
				}
				if tc.phase == "error" && strings.Contains(m.Message, "Couldn't start") {
					cancel()
				}
				if tc.lateIdle && sawTimeout && f.readsCount() > timeoutReads && m.Session.State == "idle" && m.LoadStarted.IsZero() && !m.Busy && !strings.Contains(m.Message, "Loading") {
					cancel()
				}
				if tc.timeout && !tc.lateIdle && sawTimeout && m.Session.State == "active" {
					cancel()
				}
				if tc.phase == "running" && !tc.stop && !tc.timeout && m.Session.State == "active" {
					cancel()
				}
				if tc.stop && stops > 0 && m.Session.State == "idle" {
					cancel()
				}
			}
			if err := Run(ctx, c, func(Model) {}, func() (Pad, error) { return pad, nil }); err != nil {
				t.Fatal(err)
			}
			launches, stops := f.counts()
			if launches != 1 || !sawLoading {
				t.Fatalf("launches=%d loading=%v final=%+v", launches, sawLoading, last)
			}
			switch tc.name {
			case "running":
				if last.Session.State != "active" || last.LoadPhase != "" || !last.LoadStarted.IsZero() || pauses != 1 || resumes != 0 {
					t.Fatalf("running model=%+v pause/resume=%d/%d", last, pauses, resumes)
				}
			case "failure":
				if last.Session.State != "idle" || !strings.Contains(last.Message, "Couldn't start") || !strings.Contains(last.Message, "Data Storm") || resumes != 1 {
					t.Fatalf("failure model=%+v resumes=%d", last, resumes)
				}
			case "timeout_then_running":
				if !sawTimeout || last.Session.State != "active" || last.LoadPhase != "" || !last.LoadStarted.IsZero() || pauses != 2 || resumes != 1 {
					t.Fatalf("late adoption model=%+v pause/resume=%d/%d", last, pauses, resumes)
				}
			case "timeout_then_idle":
				if !sawTimeout || last.Session.State != "idle" || !last.LoadStarted.IsZero() || !strings.Contains(last.Message, "took too long") || resumes != 1 {
					t.Fatalf("late idle model=%+v resumes=%d", last, resumes)
				}
			case "deferred_stop":
				if !pad.chordSent || stops != 1 {
					t.Fatalf("chord=%v stops=%d model=%+v", pad.chordSent, stops, last)
				}
			}
		})
	}
}

func TestRunHostStateTransitions(t *testing.T) {
	for _, tc := range []struct {
		name, phase, progress string
		timeout, stop         bool
	}{
		{name: "active", phase: "active"}, {name: "failed", phase: "failed"},
		{name: "timeout_then_active", phase: "active", timeout: true},
		{name: "stop_during_launch", phase: "launching", stop: true},
		{name: "progress_message", phase: "active", progress: "transferring cartridge"},
		{name: "progress_stage", phase: "active", progress: "loading_core"},
		{name: "indeterminate", phase: "active"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			advance := transitionClock(t)
			var state atomic.Value
			state.Store("idle")
			var launchCalls, stopCalls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/session":
					payload := map[string]any{"state": state.Load().(string), "game_id": "pong", "title": "Pong", "execution": "fpga_native"}
					if tc.progress != "" {
						if tc.name == "progress_stage" {
							payload["progress"] = map[string]string{"stage": tc.progress}
						} else {
							payload["progress"] = map[string]string{"message": tc.progress}
						}
					}
					_ = json.NewEncoder(w).Encode(payload)
				case "/api/v1/health":
					_, _ = w.Write([]byte(`{"ready":true,"target":{"reachable":true,"ready":true}}`))
				case "/api/v1/platforms":
					_, _ = w.Write([]byte(`{"platforms":[{"id":"pong","game_count":1}]}`))
				case "/api/v1/games":
					_, _ = w.Write([]byte(`{"games":[{"id":"pong","title":"Pong","system":"pong","state":"available","root_online":true,"launchable":true}]}`))
				case "/api/v1/session/launch":
					launchCalls.Add(1)
					state.Store("launching")
					_, _ = w.Write([]byte(`{"state":"active","game_id":"pong"}`))
				case "/api/v1/session/stop":
					stopCalls.Add(1)
					state.Store("idle")
					_, _ = w.Write([]byte(`{"state":"idle"}`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			c := NewClient(Config{API: server.URL, MenuDisplay: true})
			var pauses, resumes int
			c.SetMenuDisplayHandoff(func(context.Context) error { pauses++; return nil }, func() { resumes++ })
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			pad := &transitionPad{}
			var last Model
			var sawLoading, sawTimeout, sawPhase bool
			pad.ready = func() bool {
				return len(last.Games) > 0 && last.Connected && last.TargetReady && last.Session.State == "idle" && !sawLoading
			}
			pad.chord = func() bool { return tc.stop && sawLoading }
			launcherObserve = func(m Model) {
				last = m
				if m.Session.State == "launching" {
					sawLoading = true
					chrome := m.SessionChrome()
					if strings.Contains(chrome.Phase, "%") || strings.Contains(chrome.Elapsed, "%") {
						t.Errorf("invented percent: %+v", chrome)
					}
					if tc.progress != "" && strings.Contains(chrome.Phase, tc.progress) {
						sawPhase = true
					}
					if tc.progress == "" && chrome.Phase != "" {
						sawPhase = true
					}
				}
				if sawLoading && launchCalls.Load() > 0 && !tc.timeout && !tc.stop && state.Load().(string) == "launching" && (tc.progress == "" || sawPhase) {
					state.Store(tc.phase)
				}
				if tc.timeout && launchCalls.Load() > 0 && m.Session.State == "launching" && !sawTimeout {
					advance(localLoadTimeout + launchTimeoutGrace + time.Second)
				}
				if strings.Contains(m.Message, "took too long") {
					sawTimeout = true
					state.Store("active")
				}
				if tc.name == "failed" && strings.Contains(m.Message, "Couldn't start") {
					cancel()
				}
				if m.Session.State == "active" && (tc.name == "active" || tc.name == "timeout_then_active" || tc.name == "progress_message" || tc.name == "progress_stage" || tc.name == "indeterminate") {
					cancel()
				}
				if tc.stop && stopCalls.Load() > 0 {
					cancel()
				}
			}
			if err := Run(ctx, c, func(Model) {}, func() (Pad, error) { return pad, nil }); err != nil {
				t.Fatal(err)
			}
			if launchCalls.Load() != 1 || !sawLoading {
				t.Fatalf("launches=%d loading=%v model=%+v", launchCalls.Load(), sawLoading, last)
			}
			switch tc.name {
			case "failed":
				if !strings.Contains(last.Message, "Couldn't start Pong") || last.LoadPhase != "" || resumes != 1 {
					t.Fatalf("failed model=%+v resumes=%d", last, resumes)
				}
			case "timeout_then_active":
				if !sawTimeout || last.Session.State != "active" || !last.LoadStarted.IsZero() || pauses != 2 || resumes != 1 {
					t.Fatalf("adoption model=%+v pause/resume=%d/%d", last, pauses, resumes)
				}
			case "stop_during_launch":
				if !pad.chordSent || stopCalls.Load() != 1 {
					t.Fatalf("chord=%v stops=%d", pad.chordSent, stopCalls.Load())
				}
			default:
				if last.Session.State != "active" || last.LoadPhase != "" || !last.LoadStarted.IsZero() || pauses != 1 || resumes != 0 || !sawPhase {
					t.Fatalf("active model=%+v phase=%v pause/resume=%d/%d", last, sawPhase, pauses, resumes)
				}
			}
		})
	}
}
