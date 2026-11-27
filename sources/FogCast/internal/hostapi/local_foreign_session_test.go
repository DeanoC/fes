package hostapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

type foreignReadService struct {
	*fakeService
	kit          bool
	foreign      bool
	observeErr   error
	target       string
	observations int
}

func (s *foreignReadService) KitHostMode() bool                   { return s.kit }
func (s *foreignReadService) ForegroundSessionTargetName() string { return s.target }
func (s *foreignReadService) SessionTargetName() string           { return s.target }
func (s *foreignReadService) KitLocalForeignLease(_ context.Context, name string) (bool, error) {
	s.observations++
	if name != fogcast.KitHostTargetName {
		return false, errors.New("wrong target")
	}
	return s.foreign, s.observeErr
}

func TestKitForeignSessionReadIsObservationalAndResumesOwnedInput(t *testing.T) {
	core := "Pong"
	s := &foreignReadService{fakeService: &fakeService{status: protocol.Status{State: protocol.StateActive, ObservedCore: &core}}, kit: true, foreign: true, target: fogcast.KitHostTargetName}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputAttached, Ready: true, SessionID: "stale"}}
	h := hostapi.New(s, hostapi.WithRemoteInputFactory(func(string) (host.RemoteInputController, error) { return input, nil }))
	response := serve(t, h, http.MethodGet, "/api/v1/session")
	var body struct {
		State string                 `json:"state"`
		Input host.RemoteInputStatus `json:"input"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &body) != nil || body.State != "active" || body.Input.Ready || body.Input.SessionID != "" || body.Input.State != host.RemoteInputDetached {
		t.Fatalf("foreign read: %d %s", response.Code, response.Body)
	}
	if len(input.attach) != 0 || len(input.detach) != 0 {
		t.Fatalf("foreign read mutated bridge: attach=%v detach=%v", input.attach, input.detach)
	}
	s.foreign = false
	response = serve(t, h, http.MethodGet, "/api/v1/session")
	if response.Code != http.StatusOK || len(input.detach) != 1 || len(input.attach) != 1 {
		t.Fatalf("owned reconciliation did not resume: %d %s attach=%v detach=%v", response.Code, response.Body, input.attach, input.detach)
	}
}

func TestKitForeignSessionReadFailsClosedWithoutFreshObservation(t *testing.T) {
	core := "Pong"
	for _, tc := range []struct {
		name             string
		kit              bool
		target           string
		observeErr       error
		wantObservations int
	}{
		{"failed observation", true, fogcast.KitHostTargetName, errors.New("lost observation"), 1},
		{"non kit host", false, fogcast.KitHostTargetName, nil, 0},
		{"sibling", true, "sibling", nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &foreignReadService{fakeService: &fakeService{status: protocol.Status{State: protocol.StateActive, ObservedCore: &core}}, kit: tc.kit, foreign: true, target: tc.target, observeErr: tc.observeErr}
			input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputDetached}, attachErr: &protocol.APIError{Code: protocol.CodeKitLeaseBusy}}
			h := hostapi.New(s, hostapi.WithRemoteInputFactory(func(string) (host.RemoteInputController, error) { return input, nil }))
			response := serve(t, h, http.MethodGet, "/api/v1/session")
			if response.Code == http.StatusOK || s.observations != tc.wantObservations {
				t.Fatalf("unexpected admission %d %s observations=%d", response.Code, response.Body, s.observations)
			}
			if tc.observeErr != nil && (len(input.attach) != 0 || len(input.detach) != 0) {
				t.Fatalf("failed observation mutated input: %+v %+v", input.attach, input.detach)
			}
		})
	}
}
