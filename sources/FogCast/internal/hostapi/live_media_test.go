package hostapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/hostclient"
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
	afterInsert  protocol.Status
	afterEject   protocol.Status
}

func (s *liveMediaService) ReplaceLiveMedia(_ context.Context, mediaID, name string, b protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	s.replaceCalls++
	s.mediaID, s.name = mediaID, name
	if s.err != nil {
		return protocol.Status{}, s.err
	}
	if s.afterInsert.CorePackage != nil {
		s.status = s.afterInsert
	}
	return s.status, nil
}

func (s *liveMediaService) ClearLiveMedia(_ context.Context, b protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	s.clearCalls++
	if s.err != nil {
		return protocol.Status{}, s.err
	}
	if s.afterEject.CorePackage != nil {
		s.status = s.afterEject
	}
	return s.status, nil
}

func TestC64DiskInsertAndEjectStateReachesSessionAPI(t *testing.T) {
	packageID := strings.Repeat("a", 64)
	disk := protocol.C64DiskInterface()
	status := func(state string) protocol.Status {
		return protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{
			PackageID: packageID, Generation: 9, ABI: protocol.RuntimeContract{ID: "fes.computer", Major: 1},
			ActiveInterfaces: []protocol.RuntimeInterface{{ID: disk.ID, Major: disk.Major, Minor: disk.Minor}},
			MediaUnits:       []protocol.MediaUnitStatus{{Unit: 0, Interface: disk, MinBytes: uint32(protocol.C64DiskBytes), MaxBytes: uint32(protocol.C64DiskBytes), ChunkBytes: 512, State: state}},
		}}
	}
	service := &liveMediaService{fakeService: fakeService{status: status("empty"), sessionTarget: "dev"}, afterInsert: status("ready"), afterEject: status("empty")}
	handler := hostapi.New(service)
	decode := func(response *httptest.ResponseRecorder, want string) {
		t.Helper()
		if response.Code != http.StatusOK {
			t.Fatalf("session status=%d body=%s", response.Code, response.Body)
		}
		observed, err := hostclient.DecodeSession(response.Code, response.Body.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		var unit protocol.MediaUnitStatus
		found := false
		if observed.CorePackage != nil {
			for _, candidate := range observed.CorePackage.MediaUnits {
				if candidate.Unit == 0 {
					unit, found = candidate, true
					break
				}
			}
		}
		if !found || unit.State != want {
			t.Fatalf("session media unit=%+v state=%q, want %q", unit, unit.State, want)
		}
	}
	initial := serve(t, handler, http.MethodGet, "/api/v1/session")
	var session struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(initial.Body.Bytes(), &session); err != nil || session.ID == "" {
		t.Fatalf("session: %s err=%v", initial.Body, err)
	}
	insert := httptest.NewRequest(http.MethodPost, "/api/v1/session/live-media", strings.NewReader(`{"media_id":"`+strings.Repeat("b", 64)+`","name":"game.d64"}`))
	insert.Host = "127.0.0.1"
	insert.Header.Set("Content-Type", "application/json")
	insert.Header.Set("X-FogCast-Session-ID", session.ID)
	protocol.DevelopmentMediaBinding{PackageID: packageID, Generation: 9, Target: "dev"}.SetHeaders(insert.Header)
	insertResponse := httptest.NewRecorder()
	handler.ServeHTTP(insertResponse, insert)
	decode(insertResponse, "ready")
	decode(serve(t, handler, http.MethodGet, "/api/v1/session"), "ready")

	eject := httptest.NewRequest(http.MethodPost, "/api/v1/session/live-media/clear", nil)
	eject.Host = "127.0.0.1"
	eject.Header.Set("X-FogCast-Session-ID", session.ID)
	protocol.DevelopmentMediaBinding{PackageID: packageID, Generation: 9, Target: "dev"}.SetHeaders(eject.Header)
	ejectResponse := httptest.NewRecorder()
	handler.ServeHTTP(ejectResponse, eject)
	decode(ejectResponse, "empty")
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

	badName := httptest.NewRequest(http.MethodPost, "/api/v1/session/live-media", strings.NewReader(`{"media_id":"`+strings.Repeat("b", 64)+`","name":"maze.wav"}`))
	badName.Host = "127.0.0.1"
	badName.Header.Set("Content-Type", "application/json")
	badName.Header.Set("X-FogCast-Session-ID", session.ID)
	protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9, Target: "dev"}.SetHeaders(badName.Header)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, badName)
	if w.Code != 400 || service.replaceCalls != 0 {
		t.Fatalf("wav admission status=%d calls=%d", w.Code, service.replaceCalls)
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
