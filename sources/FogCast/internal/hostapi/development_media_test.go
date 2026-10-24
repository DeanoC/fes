package hostapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

type mediaService struct {
	fakeService
	calls        int
	replaceCalls int
	err          error
}

func (s *mediaService) LoadDevelopmentMedia(_ context.Context, n int64, r io.Reader, b protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	s.calls++
	return s.status, s.err
}
func (s *mediaService) ReplaceLiveMedia(_ context.Context, _, _ string, _ protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	s.replaceCalls++
	return s.status, nil
}
func (s *mediaService) ClearLiveMedia(_ context.Context, _ protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	return s.status, nil
}
func TestDevelopmentMediaHostSessionBindingPreservesInput(t *testing.T) {
	status := protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 9, ABI: protocol.RuntimeContract{ID: "fes.simple-computer", Major: 1}, ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.media.blob", Major: 1}}}}
	service := &mediaService{fakeService: fakeService{status: status, sessionTarget: "dev"}}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputAttached, Ready: true}}
	handler := hostapi.New(service, hostapi.WithRemoteInput(input))
	before := serve(t, handler, http.MethodGet, "/api/v1/session")
	detachedBefore := len(input.detach)
	var session struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(before.Body.Bytes(), &session); err != nil || session.ID == "" {
		t.Fatalf("session: %s", before.Body)
	}
	liveInsert := httptest.NewRequest(http.MethodPost, "/api/v1/session/live-media", strings.NewReader(`{"media_id":"`+strings.Repeat("b", 64)+`","name":"prior.p"}`))
	liveInsert.Host = "127.0.0.1"
	liveInsert.Header.Set("Content-Type", "application/json")
	liveInsert.Header.Set(protocol.HostSessionIDHeader, session.ID)
	protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9, Target: "dev"}.SetHeaders(liveInsert.Header)
	liveReply := httptest.NewRecorder()
	handler.ServeHTTP(liveReply, liveInsert)
	if liveReply.Code != http.StatusOK || !strings.Contains(liveReply.Body.String(), `"media_name":"prior.p"`) {
		t.Fatalf("live insert status=%d body=%s", liveReply.Code, liveReply.Body)
	}
	loadDevelopmentMedia := func(id string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/session/development-media", strings.NewReader("raw"))
		r.Host = "127.0.0.1"
		r.Header.Set("Content-Type", "application/octet-stream")
		r.Header.Set("X-FogCast-Session-ID", id)
		protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9, Target: "dev"}.SetHeaders(r.Header)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if rejected := loadDevelopmentMedia("stale-host"); rejected.Code != http.StatusConflict {
		t.Fatalf("stale-host status=%d body=%s", rejected.Code, rejected.Body)
	}
	for _, rejection := range []error{
		protocol.LiveMediaBusyError(),
		&protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "completed development-media rejection", Phase: "input", NoChange: true},
	} {
		service.err = rejection
		if rejected := loadDevelopmentMedia(session.ID); rejected.Code == http.StatusOK || !strings.Contains(rejected.Body.String(), `"no_change":true`) {
			t.Fatalf("rejected development-media load = %d %s", rejected.Code, rejected.Body.String())
		}
		if current := serve(t, handler, http.MethodGet, "/api/v1/session"); !strings.Contains(current.Body.String(), `"media_name":"prior.p"`) {
			t.Fatalf("rejected development-media load lost prior filename: %s", current.Body.String())
		}
	}
	service.err = nil
	if loaded := loadDevelopmentMedia(session.ID); loaded.Code != http.StatusOK {
		t.Fatalf("development-media status=%d body=%s", loaded.Code, loaded.Body)
	}
	if service.calls != 3 || service.replaceCalls != 1 || len(input.detach) != detachedBefore {
		t.Fatalf("calls=%d replacements=%d detached=%v", service.calls, service.replaceCalls, input.detach)
	}
	if after := serve(t, handler, http.MethodGet, "/api/v1/session"); strings.Contains(after.Body.String(), `"media_name"`) {
		t.Fatalf("development-media replacement retained the live filename: %s", after.Body)
	}
}
