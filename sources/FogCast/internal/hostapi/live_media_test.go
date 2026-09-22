package hostapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

type liveMediaService struct {
	fakeService
	replaceCalls int
	clearCalls   int
	mediaID      string
	name         string
	err          error
}

func (s *liveMediaService) ReplaceLiveMedia(_ context.Context, mediaID, name string, b protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	s.replaceCalls++
	s.mediaID, s.name = mediaID, name
	if s.err != nil {
		return protocol.Status{}, s.err
	}
	return s.status, nil
}

func (s *liveMediaService) ClearLiveMedia(_ context.Context, b protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	s.clearCalls++
	if s.err != nil {
		return protocol.Status{}, s.err
	}
	return s.status, nil
}

func TestLiveMediaHostSessionChangeTapeAndEject(t *testing.T) {
	status := protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{
		PackageID: strings.Repeat("a", 64), Generation: 9,
		ABI:              protocol.RuntimeContract{ID: "fes.simple-computer", Major: 1},
		ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.media.blob", Major: 1}},
	}}
	service := &liveMediaService{fakeService: fakeService{status: status, sessionTarget: "dev"}}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputAttached, Ready: true}}
	handler := hostapi.New(service, hostapi.WithRemoteInput(input))
	before := serve(t, handler, http.MethodGet, "/api/v1/session")
	var session struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(before.Body.Bytes(), &session); err != nil || session.ID == "" {
		t.Fatalf("session: %s", before.Body)
	}
	detachedBefore := len(input.detach)

	r := httptest.NewRequest(http.MethodPost, "/api/v1/session/live-media", strings.NewReader(`{"media_id":"`+strings.Repeat("b", 64)+`","name":"maze.p"}`))
	r.Host = "127.0.0.1"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-FogCast-Session-ID", session.ID)
	protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9, Target: "dev"}.SetHeaders(r.Header)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 || service.replaceCalls != 1 || service.mediaID != strings.Repeat("b", 64) || service.name != "maze.p" {
		t.Fatalf("change-tape status=%d body=%s calls=%d id=%s name=%s", w.Code, w.Body, service.replaceCalls, service.mediaID, service.name)
	}

	clear := httptest.NewRequest(http.MethodPost, "/api/v1/session/live-media/clear", nil)
	clear.Host = "127.0.0.1"
	clear.Header.Set("X-FogCast-Session-ID", session.ID)
	protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9, Target: "dev"}.SetHeaders(clear.Header)
	cw := httptest.NewRecorder()
	handler.ServeHTTP(cw, clear)
	if cw.Code != 200 || service.clearCalls != 1 {
		t.Fatalf("eject status=%d body=%s calls=%d", cw.Code, cw.Body, service.clearCalls)
	}
	if len(input.detach) != detachedBefore {
		t.Fatalf("input detached unexpectedly: %v", input.detach)
	}
}

func TestLiveMediaHostRejectsBadAdmissionAndStaleSession(t *testing.T) {
	status := protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{
		PackageID: strings.Repeat("a", 64), Generation: 9,
		ABI:              protocol.RuntimeContract{ID: "fes.simple-computer", Major: 1},
		ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.media.blob", Major: 1}},
	}}
	service := &liveMediaService{fakeService: fakeService{status: status, sessionTarget: "dev"}}
	handler := hostapi.New(service)
	before := serve(t, handler, http.MethodGet, "/api/v1/session")
	var session struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(before.Body.Bytes(), &session)

	badName := httptest.NewRequest(http.MethodPost, "/api/v1/session/live-media", strings.NewReader(`{"media_id":"`+strings.Repeat("b", 64)+`","name":"maze.tzx"}`))
	badName.Host = "127.0.0.1"
	badName.Header.Set("Content-Type", "application/json")
	badName.Header.Set("X-FogCast-Session-ID", session.ID)
	protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9, Target: "dev"}.SetHeaders(badName.Header)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, badName)
	if w.Code != 400 || service.replaceCalls != 0 {
		t.Fatalf("tzx admission status=%d calls=%d", w.Code, service.replaceCalls)
	}

	stale := httptest.NewRequest(http.MethodPost, "/api/v1/session/live-media", strings.NewReader(`{"media_id":"`+strings.Repeat("b", 64)+`","name":"maze.p"}`))
	stale.Host = "127.0.0.1"
	stale.Header.Set("Content-Type", "application/json")
	stale.Header.Set("X-FogCast-Session-ID", "stale-host")
	protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9, Target: "dev"}.SetHeaders(stale.Header)
	sw := httptest.NewRecorder()
	handler.ServeHTTP(sw, stale)
	if sw.Code != 409 || service.replaceCalls != 0 {
		t.Fatalf("stale session status=%d calls=%d", sw.Code, service.replaceCalls)
	}

	service.err = protocol.LiveMediaBusyError()
	busy := httptest.NewRequest(http.MethodPost, "/api/v1/session/live-media", strings.NewReader(`{"media_id":"`+strings.Repeat("b", 64)+`","name":"maze.p"}`))
	busy.Host = "127.0.0.1"
	busy.Header.Set("Content-Type", "application/json")
	busy.Header.Set("X-FogCast-Session-ID", session.ID)
	protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9, Target: "dev"}.SetHeaders(busy.Header)
	bw := httptest.NewRecorder()
	handler.ServeHTTP(bw, busy)
	if bw.Code != 409 || service.replaceCalls != 1 {
		t.Fatalf("busy status=%d calls=%d body=%s", bw.Code, service.replaceCalls, bw.Body)
	}
}
