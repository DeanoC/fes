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
	retry        func() bool
	presses      int
	chordSent    bool
	retrySent    bool
}

func (p *transitionPad) Poll() ([]remoteinput.Event, error) {
	if p.retry != nil && p.retry() && !p.retrySent {
		p.retrySent = true
		a, _ := remoteinput.NormalizeGamepad("a", true)
		up, _ := remoteinput.NormalizeGamepad("a", false)
		return []remoteinput.Event{a, up}, nil
	}
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
	launchGate             <-chan struct{}
	launches, stops, reads int
}

func (*transitionCore) List(context.Context) ([]localcores.Core, error) {
	return []localcores.Core{{CoreID: "fes.sms", PackageID: strings.Repeat("a", 64)}}, nil
}
func (f *transitionCore) LaunchROM(context.Context, string, string) error {
	f.mu.Lock()
	f.launches++
	err := f.launchErr
	gate := f.launchGate
	if f.launchErr == nil {
		f.status = localcores.RunStatus{Phase: "launching"}
	}
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	return err
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
		name, phase                         string
		timeout, lateIdle, stop, launchFail bool
		pauseRetry, retryLaunch             bool
	}{
		{name: "running", phase: "running"}, {name: "failure", phase: "error"},
		{name: "timeout_then_running", phase: "running", timeout: true},
		{name: "timeout_pause_retry", phase: "running", timeout: true, pauseRetry: true},
		{name: "timeout_blocks_second_launch", phase: "running", timeout: true, retryLaunch: true},
		{name: "timeout_then_idle", phase: "idle", timeout: true, lateIdle: true},
		{name: "deferred_stop", phase: "running", stop: true},
		{name: "deferred_stop_launch_failure", phase: "idle", stop: true, launchFail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			advance := transitionClock(t)
			f := &transitionCore{status: localcores.RunStatus{Phase: "idle"}}
			var releaseLaunch chan struct{}
			if tc.launchFail {
				f.launchErr = errors.New("response lost")
				releaseLaunch = make(chan struct{})
				f.launchGate = releaseLaunch
			}
			if tc.phase == "error" {
				f.launchErr = errors.New("missing cartridge")
			}
			c := localTransitionClient(t, f)
			var pauses, resumes, successfulPauses int
			c.SetMenuDisplayHandoff(func(context.Context) error {
				pauses++
				if tc.pauseRetry && pauses == 2 {
					return errors.New("pause failed")
				}
				successfulPauses++
				return nil
			}, func() { resumes++ })
			// Every case cancels on success; the deadline only bounds a hang
			// and must tolerate -race on a loaded two-core runner.
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			pad := &transitionPad{}
			var last Model
			var sawLoading, sawTimeout, sawLaunchFailure bool
			pad.retry = func() bool { return tc.retryLaunch && sawTimeout }
			if tc.launchFail {
				pad.retry = func() bool { return sawLaunchFailure }
			}
			var timeoutReads int
			pad.ready = func() bool { return len(last.Games) > 0 && !sawLoading }
			pad.chord = func() bool { return tc.stop && sawLoading }
			var launchReleased bool
			launcherObserve = func(m Model) {
				last = m
				if tc.pauseRetry && m.Session.State == "active" && successfulPauses < 2 {
					t.Error("late running adopted before menu pause succeeded")
				}
				if m.Session.State == "launching" {
					sawLoading = true
				}
				if tc.launchFail && pad.chordSent && !launchReleased {
					close(releaseLaunch)
					launchReleased = true
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
					} else if tc.retryLaunch {
						f.set("launching")
					} else {
						f.set("running")
					}
				}
				if tc.launchFail && strings.Contains(m.Message, "Couldn't start") {
					sawLaunchFailure = true
				}
				if tc.retryLaunch && sawTimeout && pad.retrySent {
					f.set("running")
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
				if tc.launchFail && launches >= 2 {
					cancel()
				}
			}
			if err := Run(ctx, c, func(Model) {}, func() (Pad, error) { return pad, nil }); err != nil {
				t.Fatal(err)
			}
			launches, stops := f.counts()
			wantLaunches := 1
			if tc.launchFail {
				wantLaunches = 2
			}
			if launches != wantLaunches || !sawLoading {
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
			case "deferred_stop_launch_failure":
				if !pad.chordSent || stops != 0 || last.Session.State != "idle" || last.Busy || launches != 2 {
					t.Fatalf("deferred stop failure blocked a later launch: chord=%v launches=%d stops=%d model=%+v", pad.chordSent, launches, stops, last)
				}
			}
		})
	}
}

func TestImmediateHostLaunchErrorRepausesOnLateActive(t *testing.T) {
	transitionClock(t)
	var sessionReads, presents, pauses, resumes atomic.Int64
	var launchFailed atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/session":
			sessionReads.Add(1)
			if !launchFailed.Load() {
				_, _ = w.Write([]byte(`{"state":"idle"}`))
			} else {
				_, _ = w.Write([]byte(`{"state":"active","game_id":"pong"}`))
			}
		case "/api/v1/health":
			_, _ = w.Write([]byte(`{"ready":true,"target":{"reachable":true,"ready":true}}`))
		case "/api/v1/games":
			_, _ = w.Write([]byte(`{"games":[{"id":"pong","title":"Pong","state":"available","root_online":true,"launchable":true}]}`))
		case "/api/v1/session/launch":
			launchFailed.Store(true)
			http.Error(w, "launch failed", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	c := NewClient(Config{API: server.URL, MenuDisplay: true})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	pad := &transitionPad{}
	var last Model
	var presentsAtSecondPause atomic.Int64
	var recordedSecondPause atomic.Bool
	launcherObserve = func(m Model) {
		last = m
		if m.Session.State == "active" {
			cancel()
		}
	}
	c.SetMenuDisplayHandoff(func(context.Context) error {
		if pauses.Add(1) == 2 {
			presentsAtSecondPause.Store(presents.Load())
			recordedSecondPause.Store(true)
		}
		return nil
	}, func() { resumes.Add(1) })
	pad.ready = func() bool { return len(last.Games) > 0 && last.Session.State == "idle" }
	if err := Run(ctx, c, func(Model) { presents.Add(1) }, func() (Pad, error) { return pad, nil }); err != nil {
		t.Fatal(err)
	}
	if last.Session.State != "active" || pauses.Load() != 2 || resumes.Load() != 1 || !recordedSecondPause.Load() {
		t.Fatalf("late active not safely adopted: state=%q pauses=%d resumes=%d", last.Session.State, pauses.Load(), resumes.Load())
	}
	before := presentsAtSecondPause.Load()
	if presents.Load() != before {
		t.Fatalf("menu presented after second pause returned: %d -> %d", before, presents.Load())
	}
}

func TestImmediateHostLaunchErrorRePauseFailureKeepsSessionIdle(t *testing.T) {
	transitionClock(t)
	var presents, pauses, resumes atomic.Int64
	var launchFailed atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/session":
			if launchFailed.Load() {
				_, _ = w.Write([]byte(`{"state":"active","game_id":"pong"}`))
			} else {
				_, _ = w.Write([]byte(`{"state":"idle"}`))
			}
		case "/api/v1/health":
			_, _ = w.Write([]byte(`{"ready":true,"target":{"reachable":true,"ready":true}}`))
		case "/api/v1/games":
			_, _ = w.Write([]byte(`{"games":[{"id":"pong","title":"Pong","state":"available","root_online":true,"launchable":true}]}`))
		case "/api/v1/session/launch":
			launchFailed.Store(true)
			http.Error(w, "launch failed", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	c := NewClient(Config{API: server.URL, MenuDisplay: true})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	pad := &transitionPad{}
	var last Model
	var sawRepauseFailure atomic.Bool
	var activeObserved atomic.Bool
	launcherObserve = func(m Model) {
		last = m
		if m.Session.State == "active" {
			activeObserved.Store(true)
		}
	}
	c.SetMenuDisplayHandoff(func(context.Context) error {
		count := pauses.Add(1)
		if count >= 2 {
			sawRepauseFailure.Store(true)
			if count == 3 {
				cancel()
			}
			return errors.New("repause failed")
		}
		return nil
	}, func() { resumes.Add(1) })
	pad.ready = func() bool { return len(last.Games) > 0 && last.Session.State == "idle" }
	if err := Run(ctx, c, func(Model) { presents.Add(1) }, func() (Pad, error) { return pad, nil }); err != nil {
		t.Fatal(err)
	}
	if last.Session.State != "idle" || activeObserved.Load() || resumes.Load() != 1 || pauses.Load() != 3 || !sawRepauseFailure.Load() {
		t.Fatalf("failed repause exposed active session or did not retry: state=%q pauses=%d resumes=%d", last.Session.State, pauses.Load(), resumes.Load())
	}
}

func TestRunHostHandoffFailureOrdering(t *testing.T) {
	for _, pauseFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "immediate_reject", true: "pause_failure"}[pauseFails], func(t *testing.T) {
			transitionClock(t)
			var launches atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/session":
					_, _ = w.Write([]byte(`{"state":"idle"}`))
				case "/api/v1/health":
					_, _ = w.Write([]byte(`{"ready":true,"target":{"reachable":true,"ready":true}}`))
				case "/api/v1/platforms":
					_, _ = w.Write([]byte(`{"platforms":[{"id":"pong","game_count":1}]}`))
				case "/api/v1/games":
					_, _ = w.Write([]byte(`{"games":[{"id":"pong","title":"Pong","system":"pong","state":"available","root_online":true,"launchable":true}]}`))
				case "/api/v1/session/launch":
					launches.Add(1)
					w.WriteHeader(http.StatusConflict)
					_, _ = w.Write([]byte(`{"error":{"code":"IN_USE","message":"busy"}}`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			c := NewClient(Config{API: server.URL, MenuDisplay: true})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			pad := &transitionPad{}
			var last Model
			var firstFrame bool
			var paused bool
			var resumes int
			pad.ready = func() bool { return len(last.Games) > 0 && last.TargetReady && !firstFrame }
			c.SetMenuDisplayHandoff(func(context.Context) error {
				if !firstFrame {
					t.Error("pause preceded loading frame")
				}
				paused = true
				if pauseFails {
					return errors.New("pause failed")
				}
				return nil
			}, func() {
				if last.Session.State == "launching" {
					t.Error("resume preceded clearing launching")
				}
				resumes++
				paused = false
			})
			launcherObserve = func(m Model) {
				last = m
				if m.Session.State == "idle" && resumes > 0 && m.Message != "" {
					cancel()
				}
			}
			if err := Run(ctx, c, func(m Model) {
				if m.Session.State == "launching" {
					chrome := m.SessionChrome()
					if chrome.Phase == "" || chrome.Elapsed == "" || strings.Contains(chrome.Elapsed, "%") {
						t.Errorf("first loading frame: %+v", chrome)
					}
					firstFrame = true
				}
			}, func() (Pad, error) { return pad, nil }); err != nil {
				t.Fatal(err)
			}
			if !firstFrame || !pad.pressStarted() || paused || resumes != 1 || last.Session.State != "idle" || last.LoadPhase != "" || last.Message == "" {
				t.Fatalf("handoff frame=%v paused=%v resumes=%d model=%+v", firstFrame, paused, resumes, last)
			}
			if pauseFails && launches.Load() != 0 || !pauseFails && launches.Load() != 1 {
				t.Fatalf("launch calls %d", launches.Load())
			}
		})
	}
}

func (p *transitionPad) pressStarted() bool { return p.presses > 0 }

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
					_, _ = w.Write([]byte(`{"games":[{"id":"pong","title":"Pong","system":"pong","state":"available","root_online":true,"launchable":true},{"id":"pong2","title":"Pong 2","system":"pong","state":"available","root_online":true,"launchable":true}]}`))
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
				if !strings.Contains(last.Message, "Couldn't start Pong") || last.LoadPhase != "" || resumes != 1 || last.SessionChrome().State != "idle" {
					t.Fatalf("failed model=%+v resumes=%d", last, resumes)
				}
				move, _ := remoteinput.NormalizeGamepad("dpad-right", true)
				last.Input(move, time.Now())
				if last.Focus != 1 {
					t.Fatal("failed launch left the menu selector frozen")
				}
				now := time.Unix(2000, 0)
				selectPress, _ := remoteinput.NormalizeGamepad("select", true)
				startPress, _ := remoteinput.NormalizeGamepad("start", true)
				last.Input(selectPress, now)
				last.Input(startPress, now.Add(10*time.Millisecond))
				if action := last.Tick(now.Add(1100 * time.Millisecond)); action != "stop" {
					t.Fatalf("failed launch lost cleanup chord: %q", action)
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
