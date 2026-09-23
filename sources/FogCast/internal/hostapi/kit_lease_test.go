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
