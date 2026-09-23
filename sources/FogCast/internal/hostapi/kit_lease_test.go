package hostapi_test

import (
	"context"
	"errors"
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
			base := &fakeService{status: protocol.Status{State: protocol.StateIdle}, stopped: protocol.Status{State: protocol.StateIdle}, order: &order}
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
			response := serve(t, hostapi.New(service, hostapi.WithRemoteInput(input)), http.MethodPost, "/api/v1/session/stop")
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
