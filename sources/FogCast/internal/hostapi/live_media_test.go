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

	if !strings.Contains(w.Body.String(), `"media_name":"maze.p"`) {
		t.Fatalf("missing inserted name: %s", w.Body)
	}
	read := serve(t, handler, http.MethodGet, "/api/v1/session")
	if !strings.Contains(read.Body.String(), `"media_name":"maze.p"`) {
		t.Fatalf("name lost on refresh: %s", read.Body)
	}

	service.status.CorePackage.Generation++
	replaced := serve(t, handler, http.MethodGet, "/api/v1/session")
	if strings.Contains(replaced.Body.String(), `"media_name"`) {
		t.Fatalf("name leaked to replacement generation: %s", replaced.Body)
	}
	service.status.CorePackage.Generation--

	// A reply lost after dispatch cannot leave a previous filename presented as
	// confirmed. The next observed state has no trustworthy name.
	service.err = &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "reply lost"}
	retry := httptest.NewRequest(http.MethodPost, "/api/v1/session/live-media", strings.NewReader(`{"media_id":"`+strings.Repeat("b", 64)+`","name":"other.p"}`))
	retry.Host = "127.0.0.1"
	retry.Header = r.Header.Clone()
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, retry)
	if rw.Code == 200 {
		t.Fatal("expected uncertain insert failure")
	}
	uncertain := serve(t, handler, http.MethodGet, "/api/v1/session")
	if strings.Contains(uncertain.Body.String(), `"media_name"`) {
		t.Fatalf("uncertain insert retained name: %s", uncertain.Body)
	}

	service.err = nil

	confirmed := httptest.NewRequest(http.MethodPost, "/api/v1/session/live-media", strings.NewReader(`{"media_id":"`+strings.Repeat("b", 64)+`","name":"other.p"}`))
	confirmed.Host = "127.0.0.1"
	confirmed.Header = r.Header.Clone()
	confirmedReply := httptest.NewRecorder()
	handler.ServeHTTP(confirmedReply, confirmed)
	if confirmedReply.Code != 200 || !strings.Contains(confirmedReply.Body.String(), `"media_name":"other.p"`) {
		t.Fatalf("insert not confirmed: %s", confirmedReply.Body)
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
	if strings.Contains(cw.Body.String(), `"media_name"`) {
		t.Fatalf("ejected media still named: %s", cw.Body)
	}

	if len(input.detach) != detachedBefore {
		t.Fatalf("input detached unexpectedly: %v", input.detach)
	}
}

func TestLiveMediaNameClearsAfterSessionStopsOrChanges(t *testing.T) {
	packageID := strings.Repeat("a", 64)
	active := protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{
		PackageID: packageID, Generation: 9,
		ABI:              protocol.RuntimeContract{ID: "fes.simple-computer", Major: 1},
		ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.media.blob", Major: 1}},
	}}
	for _, tc := range []struct {
		name        string
		intervening func(*liveMediaService)
	}{
		{
			name: "stopped before same-generation restart",
			intervening: func(service *liveMediaService) {
				service.status = protocol.Status{State: protocol.StateIdle}
			},
		},
		{
			name: "package generation changed",
			intervening: func(service *liveMediaService) {
				changed := active
				changed.CorePackage = &protocol.CorePackageStatus{PackageID: packageID, Generation: 10}
				service.status = changed
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &liveMediaService{fakeService: fakeService{status: active, sessionTarget: "dev"}}
			handler := hostapi.New(service)
			var session struct {
				ID string `json:"id"`
			}
			if response := serve(t, handler, http.MethodGet, "/api/v1/session"); json.Unmarshal(response.Body.Bytes(), &session) != nil || session.ID == "" {
				t.Fatalf("session: %s", response.Body)
			}

			insert := httptest.NewRequest(http.MethodPost, "/api/v1/session/live-media", strings.NewReader(`{"media_id":"`+strings.Repeat("b", 64)+`","name":"maze.p"}`))
			insert.Host = "127.0.0.1"
			insert.Header.Set("Content-Type", "application/json")
			insert.Header.Set(protocol.HostSessionIDHeader, session.ID)
			protocol.DevelopmentMediaBinding{PackageID: packageID, Generation: 9, Target: "dev"}.SetHeaders(insert.Header)
			inserted := httptest.NewRecorder()
			handler.ServeHTTP(inserted, insert)
			if inserted.Code != http.StatusOK || !strings.Contains(inserted.Body.String(), `"media_name":"maze.p"`) {
				t.Fatalf("insert = %d %s", inserted.Code, inserted.Body.String())
			}

			tc.intervening(service)
			observed := serve(t, handler, http.MethodGet, "/api/v1/session")
			if strings.Contains(observed.Body.String(), `"media_name"`) {
				t.Fatalf("changed session retained filename: %s", observed.Body.String())
			}

			// The runtime may restart and reuse the package's old generation.
			service.status = active
			restarted := serve(t, handler, http.MethodGet, "/api/v1/session")
			if strings.Contains(restarted.Body.String(), `"media_name"`) {
				t.Fatalf("same-generation session revived stale filename: %s", restarted.Body.String())
			}
		})
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

func TestLiveMediaHostAcceptsDiskNamesForComputerSessions(t *testing.T) {
	status := protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{
		PackageID: strings.Repeat("a", 64), Generation: 2, ABI: protocol.RuntimeContract{ID: "fes.computer", Major: 1},
		ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.media.apple2-floppy", Major: 1}},
	}}
	service := &liveMediaService{fakeService: fakeService{status: status, sessionTarget: "dev"}}
	handler := hostapi.New(service)
	var session struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(serve(t, handler, http.MethodGet, "/api/v1/session").Body.Bytes(), &session)
	for _, tc := range []struct {
		name string
		code int
	}{{"DOS33.dsk", 200}, {"game.DO", 200}, {"prodos.po", 400}, {"image.nib", 400}} {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/session/live-media", strings.NewReader(`{"media_id":"`+strings.Repeat("b", 64)+`","name":"`+tc.name+`"}`))
		r.Host = "127.0.0.1"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-FogCast-Session-ID", session.ID)
		protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 2, Target: "dev"}.SetHeaders(r.Header)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.code || (tc.code == 200 && service.name != tc.name) {
			t.Fatalf("%s: status=%d body=%s", tc.name, w.Code, w.Body)
		}
	}
	if service.replaceCalls != 2 {
		t.Fatalf("replace calls %d", service.replaceCalls)
	}
}
