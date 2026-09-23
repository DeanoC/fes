package hostapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

type leasedService struct {
	*fakeService
	releases    int
	releaseHook func()
}

func (s *leasedService) ReleaseKitLease(context.Context) error {
	s.releases++
	if s.releaseHook != nil {
		s.releaseHook()
	}
	return nil
}

// playFilteredLeases mirrors Service.ReleaseKitLease: a grant that still
// backs a PlaySession stays held, and idle grants are released.
type playFilteredLeases struct {
	*fakeService
	held         map[string]bool
	releaseCalls int
}

func (s *playFilteredLeases) ReleaseKitLease(context.Context) error {
	s.releaseCalls++
	playing := map[string]bool{}
	for _, play := range s.PlaySessions() {
		playing[play.Target] = true
	}
	for target, held := range s.held {
		if held && !playing[target] {
			s.held[target] = false
		}
	}
	return nil
}
func TestExplicitStopReleasesKitOnlyAfterCleanup(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			order := []string{}
			gameID := "cleanup-game"
			observed := "CORE"
			base := &fakeService{
				status:  protocol.Status{State: protocol.StateIdle},
				stopped: protocol.Status{State: protocol.StateIdle},
				launch:  protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID, ObservedCore: &observed}},
				order:   &order,
			}
			if fail {
				base.stopErr = errors.New("cleanup failed")
			}
			input := &fakeRemoteInput{order: &order, status: host.RemoteInputStatus{State: host.RemoteInputAttached}}
			service := &leasedService{fakeService: base, releaseHook: func() {
				if len(input.detach) != 1 {
					t.Error("lease released before input detach")
				}
				order = append(order, "lease.release")
			}}
			handler := hostapi.New(service, hostapi.WithRemoteInput(input))
			if fail {
				launch := httptest.NewRequest(http.MethodPost, "/api/v1/session/launch", strings.NewReader(`{"game_id":"cleanup-game"}`))
				launch.Host = "127.0.0.1"
				launch.Header.Set("Content-Type", "application/json")
				launchResponse := httptest.NewRecorder()
				handler.ServeHTTP(launchResponse, launch)
				if launchResponse.Code != http.StatusOK {
					t.Fatalf("launch: %d %s", launchResponse.Code, launchResponse.Body.String())
				}
				order = order[:0]
				input.detach = nil
			}
			response := serve(t, handler, http.MethodPost, "/api/v1/session/stop")
			if fail {
				if service.releases != 0 {
					t.Fatal("released after failed cleanup")
				}
				return
			}
			if response.Code != http.StatusOK {
				t.Fatalf("stop: %d %s", response.Code, response.Body.String())
			}
			if got := strings.Join(order, ","); got != "input.detach,service.stop,lease.release" {
				t.Fatalf("order %q", got)
			}
		})
	}
}

func TestSoftStopRetainsKitLeaseAfterIdleCleanup(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		stopped  protocol.State
		wantCode int
		wantRel  int
	}{
		{name: "retain", body: `{"retain_lease":true}`, stopped: protocol.StateIdle, wantCode: http.StatusOK, wantRel: 0},
		{name: "explicit false", body: `{"retain_lease":false}`, stopped: protocol.StateIdle, wantCode: http.StatusOK, wantRel: 1},
		{name: "retain with stamp", body: `{"retain_lease":true,"client_ts_utc":"2026-09-23T12:00:00Z","flight_id":"de305d54-75b4-431b-adb2-eb6b9e546014"}`, stopped: protocol.StateIdle, wantCode: http.StatusOK, wantRel: 0},
		{name: "stopping does not release", body: `{"retain_lease":false}`, stopped: protocol.StateStopping, wantCode: http.StatusOK, wantRel: 0},
		{name: "bad flag", body: `{"retain_lease":"yes"}`, stopped: protocol.StateIdle, wantCode: http.StatusBadRequest, wantRel: 0},
		{name: "unknown field", body: `{"retain_lease":true,"lease":"keep"}`, stopped: protocol.StateIdle, wantCode: http.StatusBadRequest, wantRel: 0},
		{name: "retain and release", body: `{"retain_lease":true,"release_idle":true}`, stopped: protocol.StateIdle, wantCode: http.StatusBadRequest, wantRel: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			order := []string{}
			base := &fakeService{status: protocol.Status{State: protocol.StateIdle}, stopped: protocol.Status{State: tc.stopped}, order: &order}
			input := &fakeRemoteInput{order: &order, status: host.RemoteInputStatus{State: host.RemoteInputAttached}}
			service := &leasedService{fakeService: base, releaseHook: func() {
				order = append(order, "lease.release")
			}}
			request := httptest.NewRequest(http.MethodPost, "/api/v1/session/stop", strings.NewReader(tc.body))
			request.Host = "127.0.0.1"
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			hostapi.New(service, hostapi.WithRemoteInput(input)).ServeHTTP(response, request)
			if response.Code != tc.wantCode {
				t.Fatalf("stop = %d %s", response.Code, response.Body.String())
			}
			if service.releases != tc.wantRel {
				t.Fatalf("releases = %d, want %d; order %q", service.releases, tc.wantRel, strings.Join(order, ","))
			}
			if tc.wantRel == 1 {
				if got := strings.Join(order, ","); got != "input.detach,service.stop,lease.release" {
					t.Fatalf("order %q", got)
				}
			} else if tc.wantCode == http.StatusOK {
				if got := strings.Join(order, ","); got != "input.detach,service.stop" {
					t.Fatalf("order %q", got)
				}
			} else if len(order) != 0 {
				t.Fatalf("rejected stop mutated %q", strings.Join(order, ","))
			}
		})
	}
}

func TestSoftStopRecordsIdleThenExplicitStopReleases(t *testing.T) {
	order := []string{}
	base := &fakeService{status: protocol.Status{State: protocol.StateIdle}, stopped: protocol.Status{State: protocol.StateIdle}, order: &order}
	input := &fakeRemoteInput{order: &order, status: host.RemoteInputStatus{State: host.RemoteInputAttached}}
	service := &leasedService{fakeService: base, releaseHook: func() {
		order = append(order, "lease.release")
	}}
	handler := hostapi.New(service, hostapi.WithRemoteInput(input))
	soft := httptest.NewRequest(http.MethodPost, "/api/v1/session/stop", strings.NewReader(`{"retain_lease":true}`))
	soft.Host = "127.0.0.1"
	soft.Header.Set("Content-Type", "application/json")
	softResponse := httptest.NewRecorder()
	handler.ServeHTTP(softResponse, soft)
	if softResponse.Code != http.StatusOK {
		t.Fatalf("soft-stop = %d %s", softResponse.Code, softResponse.Body.String())
	}
	var stopped struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(softResponse.Body.Bytes(), &stopped); err != nil {
		t.Fatal(err)
	}
	if stopped.State != "idle" || service.releases != 0 {
		t.Fatalf("soft-stop state=%q releases=%d", stopped.State, service.releases)
	}
	events := decodeStampedEvents(t, handler)
	var sawIdleStop bool
	for _, event := range events {
		if event.Event == "session.stop" && event.State == protocol.StateIdle {
			sawIdleStop = true
		}
		if event.State == protocol.StateActive && event.Event == "session.stop" {
			t.Fatalf("soft-stop record stayed active: %+v", events)
		}
	}
	if !sawIdleStop {
		t.Fatalf("session record = %+v", events)
	}
	explicit := httptest.NewRequest(http.MethodPost, "/api/v1/session/stop", nil)
	explicit.Host = "127.0.0.1"
	explicitResponse := httptest.NewRecorder()
	handler.ServeHTTP(explicitResponse, explicit)
	if explicitResponse.Code != http.StatusOK || service.releases != 1 {
		t.Fatalf("explicit stop = %d releases=%d body=%s", explicitResponse.Code, service.releases, explicitResponse.Body.String())
	}
}

func TestExplicitIdleStopReleasesWhenSelectedTargetUnavailable(t *testing.T) {
	unavailable := &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "target runtime is unavailable"}
	foreign := &protocol.APIError{Code: protocol.CodeKitLeaseDenied, Message: "kit lease is foreign; HID is fail-closed"}
	cases := []struct {
		name    string
		body    string
		after   func(*fakeService)
		wantRel int
	}{
		{name: "selected stop fails", after: func(s *fakeService) { s.stopErr = unavailable }, wantRel: 1},
		{name: "selected probe fails", after: func(s *fakeService) { s.statusErr = unavailable }, wantRel: 1},
		{name: "foreign selected session", after: func(s *fakeService) { s.stopErr = foreign }, wantRel: 1},
		{name: "soft-stop keeps grant", body: `{"retain_lease":true}`, after: func(s *fakeService) { s.stopErr = unavailable }, wantRel: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := &fakeService{status: protocol.Status{State: protocol.StateIdle}, stopped: protocol.Status{State: protocol.StateIdle}}
			service := &leasedService{fakeService: base}
			handler := hostapi.New(service)
			soft := httptest.NewRequest(http.MethodPost, "/api/v1/session/stop", strings.NewReader(`{"retain_lease":true}`))
			soft.Host = "127.0.0.1"
			soft.Header.Set("Content-Type", "application/json")
			softResponse := httptest.NewRecorder()
			handler.ServeHTTP(softResponse, soft)
			if softResponse.Code != http.StatusOK || service.releases != 0 {
				t.Fatalf("soft-stop = %d releases=%d body=%s", softResponse.Code, service.releases, softResponse.Body.String())
			}
			tc.after(base)
			var body io.Reader
			if tc.body != "" {
				body = strings.NewReader(tc.body)
			}
			request := httptest.NewRequest(http.MethodPost, "/api/v1/session/stop", body)
			request.Host = "127.0.0.1"
			if tc.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code == http.StatusOK {
				t.Fatalf("unavailable stop succeeded: %s", response.Body.String())
			}
			if service.releases != tc.wantRel {
				t.Fatalf("releases = %d, want %d; body=%s", service.releases, tc.wantRel, response.Body.String())
			}
		})
	}
}

func TestExplicitIdleStopReleasesSoftStoppedLegacyNativeWhenSelectedTargetFails(t *testing.T) {
	unavailable := &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "target runtime is unavailable"}
	gameID := "snes-mario"
	system := protocol.SystemSNES
	core := "SNES"
	active := protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ExpectedCore: &core, ObservedCore: &core}
	cases := []struct {
		name    string
		body    string
		after   func(*fakeService)
		wantRel int
	}{
		{name: "selected stop fails", after: func(s *fakeService) { s.stopErr = unavailable }, wantRel: 1},
		{name: "selected probe fails", after: func(s *fakeService) {
			s.stopErr = context.DeadlineExceeded
			s.statusErr = unavailable
		}, wantRel: 1},
		{name: "soft-stop keeps grant", body: `{"retain_lease":true}`, after: func(s *fakeService) { s.stopErr = unavailable }, wantRel: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := &fakeService{
				status:  protocol.Status{State: protocol.StateIdle},
				launch:  protocol.CachedLaunchResponse{Status: active},
				stopped: protocol.Status{State: protocol.StateIdle},
			}
			service := &leasedService{fakeService: base}
			handler := hostapi.New(service)
			if launch := launchSession(t, handler, gameID); launch.Code != http.StatusOK || !strings.Contains(launch.Body.String(), `"execution":"fpga_native"`) {
				t.Fatalf("launch = %d %s", launch.Code, launch.Body.String())
			}
			soft := httptest.NewRequest(http.MethodPost, "/api/v1/session/stop", strings.NewReader(`{"retain_lease":true}`))
			soft.Host = "127.0.0.1"
			soft.Header.Set("Content-Type", "application/json")
			softResponse := httptest.NewRecorder()
			handler.ServeHTTP(softResponse, soft)
			if softResponse.Code != http.StatusOK || service.releases != 0 {
				t.Fatalf("soft-stop = %d releases=%d body=%s", softResponse.Code, service.releases, softResponse.Body.String())
			}
			// The leftover marker is the legacy label, not an empty coordinator.
			observed := serve(t, handler, http.MethodGet, "/api/v1/session")
			if observed.Code != http.StatusOK || !strings.Contains(observed.Body.String(), `"state":"idle"`) || !strings.Contains(observed.Body.String(), `"execution":"fpga_native"`) {
				t.Fatalf("soft-stopped legacy session = %d %s", observed.Code, observed.Body.String())
			}
			tc.after(base)
			var body io.Reader
			if tc.body != "" {
				body = strings.NewReader(tc.body)
			}
			request := httptest.NewRequest(http.MethodPost, "/api/v1/session/stop", body)
			request.Host = "127.0.0.1"
			if tc.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code == http.StatusOK {
				t.Fatalf("unavailable stop succeeded: %s", response.Body.String())
			}
			if service.releases != tc.wantRel {
				t.Fatalf("releases = %d, want %d; body=%s", service.releases, tc.wantRel, response.Body.String())
			}
		})
	}
}

func TestFailedExplicitStopReleasesIdleGrantsWhenAnotherPlaySurvives(t *testing.T) {
	unavailable := &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "target runtime is unavailable"}
	saveFailed := &protocol.APIError{Code: protocol.CodeSaveFailed, Message: "save failed", Phase: "save"}
	gameB := "snes-foreground"
	systemB := protocol.SystemSNES
	core := "SNES"
	foreground := protocol.Status{State: protocol.StateActive, GameID: &gameB, System: &systemB, ExpectedCore: &core, ObservedCore: &core}
	promotedB := fogcast.PlaySession{
		Target:    "kit-b",
		TargetID:  "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
		Execution: fogcast.ExecutionFPGANative,
		GameID:    "nes-still-playing",
		System:    protocol.SystemNES,
	}
	cases := []struct {
		name  string
		after func(*fakeService)
	}{
		{name: "stop unavailable", after: func(s *fakeService) { s.stopErr = unavailable }},
		{name: "save failed", after: func(s *fakeService) { s.stopErr = saveFailed }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := &fakeService{
				status:        protocol.Status{State: protocol.StateIdle},
				launch:        protocol.CachedLaunchResponse{Status: foreground},
				stopped:       protocol.Status{State: protocol.StateIdle},
				sessionTarget: "kit-a",
				playSessions: []fogcast.PlaySession{
					promotedB,
					{Target: "kit-a", TargetID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", Execution: fogcast.ExecutionFPGANative, GameID: gameB, System: systemB},
				},
			}
			// A and C were Soft-stopped earlier. B is the play that survives
			// a failed explicit Stop. ReleaseKitLease must drop A/C and keep B.
			service := &playFilteredLeases{fakeService: base, held: map[string]bool{
				"kit-a": true,
				"kit-b": true,
				"kit-c": true,
			}}
			handler := hostapi.New(service)
			if launch := launchSessionOn(t, handler, gameB, "kit-a"); launch.Code != http.StatusOK || !strings.Contains(launch.Body.String(), `"execution":"fpga_native"`) {
				t.Fatalf("launch A = %d %s", launch.Code, launch.Body.String())
			}
			soft := httptest.NewRequest(http.MethodPost, "/api/v1/session/stop", strings.NewReader(`{"retain_lease":true}`))
			soft.Host = "127.0.0.1"
			soft.Header.Set("Content-Type", "application/json")
			softResponse := httptest.NewRecorder()
			handler.ServeHTTP(softResponse, soft)
			if softResponse.Code != http.StatusOK || service.releaseCalls != 0 {
				t.Fatalf("soft-stop A = %d releases=%d body=%s", softResponse.Code, service.releaseCalls, softResponse.Body.String())
			}
			if !service.held["kit-a"] || !service.held["kit-b"] || !service.held["kit-c"] {
				t.Fatalf("soft-stop released grants: %#v", service.held)
			}
			// clearForegroundPlayLocked has promoted B. The coordinator only
			// observed A's idle response, so its local marker is idle too.
			base.playSessions = []fogcast.PlaySession{promotedB}
			observed := serve(t, handler, http.MethodGet, "/api/v1/session")
			if observed.Code != http.StatusOK || !strings.Contains(observed.Body.String(), `"state":"idle"`) {
				t.Fatalf("coordinator after soft-stop A = %d %s", observed.Code, observed.Body.String())
			}
			plays := serve(t, handler, http.MethodGet, "/api/v1/sessions")
			if plays.Code != http.StatusOK || !strings.Contains(plays.Body.String(), `"target":"kit-b"`) || strings.Contains(plays.Body.String(), `"target":"kit-a"`) {
				t.Fatalf("promoted plays = %d %s", plays.Code, plays.Body.String())
			}
			tc.after(base)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/session/stop", nil)
			request.Host = "127.0.0.1"
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code == http.StatusOK {
				t.Fatalf("stop of B succeeded: %s", response.Body.String())
			}
			if service.releaseCalls != 1 {
				t.Fatalf("release calls = %d, want the service filter to run", service.releaseCalls)
			}
			if service.held["kit-a"] || service.held["kit-c"] {
				t.Fatalf("idle grants stayed held: %#v", service.held)
			}
			if !service.held["kit-b"] {
				t.Fatalf("released promoted lease while B is active: %#v", service.held)
			}
			stillPlaying := serve(t, handler, http.MethodGet, "/api/v1/sessions")
			if stillPlaying.Code != http.StatusOK || !strings.Contains(stillPlaying.Body.String(), `"target":"kit-b"`) {
				t.Fatalf("B after failed stop = %d %s", stillPlaying.Code, stillPlaying.Body.String())
			}
		})
	}
}

func TestReleaseIdleDropsIdleGrantsWithoutStoppingSurvivor(t *testing.T) {
	game := "nes-still-playing"
	system := protocol.SystemNES
	base := &fakeService{
		status:  protocol.Status{State: protocol.StateActive, GameID: &game, System: &system},
		stopped: protocol.Status{State: protocol.StateIdle},
		playSessions: []fogcast.PlaySession{{
			Target:    "kit-b",
			TargetID:  "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
			Execution: fogcast.ExecutionFPGANative,
			GameID:    game,
			System:    system,
		}},
	}
	service := &playFilteredLeases{fakeService: base, held: map[string]bool{
		"kit-a": true,
		"kit-b": true,
	}}
	handler := hostapi.New(service)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/stop", strings.NewReader(`{"release_idle":true}`))
	request.Host = "127.0.0.1"
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"state":"active"`) {
		t.Fatalf("release_idle = %d %s", response.Code, response.Body.String())
	}
	if len(base.stopCtxErrs) != 0 {
		t.Fatalf("release_idle stopped the survivor: %d", len(base.stopCtxErrs))
	}
	if service.releaseCalls != 1 {
		t.Fatalf("release calls = %d", service.releaseCalls)
	}
	if service.held["kit-a"] {
		t.Fatalf("idle grant stayed held: %#v", service.held)
	}
	if !service.held["kit-b"] {
		t.Fatalf("released surviving lease: %#v", service.held)
	}
}
