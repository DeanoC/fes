package hostapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
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

func TestLiveMediaEarlyValidationReportsNoChange(t *testing.T) {
	handler := hostapi.New(&liveMediaService{})
	for _, tc := range []struct {
		name  string
		path  string
		body  string
		setup func(*http.Request)
	}{
		{
			name:  "replace missing binding",
			path:  "/api/v1/session/live-media",
			body:  `{"media_id":"` + strings.Repeat("a", 64) + `","name":"game.p"}`,
			setup: func(r *http.Request) { r.Header.Set("Content-Type", "application/json") },
		},
		{
			name: "replace invalid body",
			path: "/api/v1/session/live-media",
			body: "{}",
			setup: func(r *http.Request) {
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set(protocol.HostSessionIDHeader, "session")
				protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("b", 64), Generation: 1, Target: "target"}.SetHeaders(r.Header)
			},
		},
		{
			name: "clear nonempty body",
			path: "/api/v1/session/live-media/clear",
			body: "unexpected",
			setup: func(r *http.Request) {
				r.Header.Set(protocol.HostSessionIDHeader, "session")
				protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("b", 64), Generation: 1, Target: "target"}.SetHeaders(r.Header)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			r.Host = "127.0.0.1"
			tc.setup(r)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"no_change":true`) {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
		})
	}
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
	service.err = protocol.LiveMediaBusyError()
	cw := httptest.NewRecorder()
	handler.ServeHTTP(cw, clear)
	if cw.Code != http.StatusConflict || service.clearCalls != 1 || !strings.Contains(cw.Body.String(), `"no_change":true`) {
		t.Fatalf("busy eject status=%d body=%s calls=%d", cw.Code, cw.Body, service.clearCalls)
	}
	stillInserted := serve(t, handler, http.MethodGet, "/api/v1/session")
	if !strings.Contains(stillInserted.Body.String(), `"media_name":"other.p"`) {
		t.Fatalf("no-op eject rejection lost confirmed name: %s", stillInserted.Body)
	}
	service.err = &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "completed eject rejection", Phase: "input", NoChange: true}
	cw = httptest.NewRecorder()
	handler.ServeHTTP(cw, clear)
	if cw.Code == http.StatusOK || !strings.Contains(cw.Body.String(), `"no_change":true`) {
		t.Fatalf("completed eject rejection status=%d body=%s", cw.Code, cw.Body)
	}
	stillInserted = serve(t, handler, http.MethodGet, "/api/v1/session")
	if !strings.Contains(stillInserted.Body.String(), `"media_name":"other.p"`) {
		t.Fatalf("completed eject rejection lost confirmed name: %s", stillInserted.Body)
	}

	service.err = nil
	cw = httptest.NewRecorder()
	handler.ServeHTTP(cw, clear)
	if cw.Code != http.StatusOK || service.clearCalls != 3 {
		t.Fatalf("eject status=%d body=%s calls=%d", cw.Code, cw.Body, service.clearCalls)
	}
	if strings.Contains(cw.Body.String(), `"media_name"`) {
		t.Fatalf("ejected media still named: %s", cw.Body)
	}
	if !strings.Contains(cw.Body.String(), `"media_ejected":true`) {
		t.Fatalf("confirmed eject state missing: %s", cw.Body)
	}
	if current := serve(t, handler, http.MethodGet, "/api/v1/session"); !strings.Contains(current.Body.String(), `"media_ejected":true`) {
		t.Fatalf("confirmed eject state was not retained: %s", current.Body)
	}
	reinsert := httptest.NewRequest(http.MethodPost, "/api/v1/session/live-media", strings.NewReader(`{"media_id":"`+strings.Repeat("b", 64)+`","name":"after.p"}`))
	reinsert.Host = "127.0.0.1"
	reinsert.Header = r.Header.Clone()
	reinsertReply := httptest.NewRecorder()
	handler.ServeHTTP(reinsertReply, reinsert)
	if reinsertReply.Code != http.StatusOK || !strings.Contains(reinsertReply.Body.String(), `"media_name":"after.p"`) || strings.Contains(reinsertReply.Body.String(), `"media_ejected"`) {
		t.Fatalf("reinsertion did not replace the confirmed-empty state: %d %s", reinsertReply.Code, reinsertReply.Body)
	}

	if len(input.detach) != detachedBefore {
		t.Fatalf("input detached unexpectedly: %v", input.detach)
	}
}

func TestLiveMediaNoChangeRejectionsPreserveName(t *testing.T) {
	packageID := strings.Repeat("a", 64)
	service := &liveMediaService{fakeService: fakeService{status: protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{
		PackageID: packageID, Generation: 9,
		ABI:              protocol.RuntimeContract{ID: "fes.simple-computer", Major: 1},
		ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.media.blob", Major: 1}},
	}}, sessionTarget: "dev"}}
	handler := hostapi.New(service)
	var session struct {
		ID string `json:"id"`
	}
	if response := serve(t, handler, http.MethodGet, "/api/v1/session"); json.Unmarshal(response.Body.Bytes(), &session) != nil || session.ID == "" {
		t.Fatalf("session: %s", response.Body)
	}
	request := func(path, name string) *httptest.ResponseRecorder {
		var body *strings.Reader
		if name != "" {
			body = strings.NewReader(`{"media_id":"` + strings.Repeat("b", 64) + `","name":"` + name + `"}`)
		}
		var req *http.Request
		if body == nil {
			req = httptest.NewRequest(http.MethodPost, path, nil)
		} else {
			req = httptest.NewRequest(http.MethodPost, path, body)
			req.Header.Set("Content-Type", "application/json")
		}
		req.Host = "127.0.0.1"
		req.Header.Set(protocol.HostSessionIDHeader, session.ID)
		protocol.DevelopmentMediaBinding{PackageID: packageID, Generation: 9, Target: "dev"}.SetHeaders(req.Header)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	if inserted := request("/api/v1/session/live-media", "first.p"); inserted.Code != http.StatusOK || !strings.Contains(inserted.Body.String(), `"media_name":"first.p"`) {
		t.Fatalf("initial insert = %d %s", inserted.Code, inserted.Body.String())
	}
	service.err = protocol.LiveMediaBusyError()
	if rejected := request("/api/v1/session/live-media", "second.p"); rejected.Code != http.StatusConflict || !strings.Contains(rejected.Body.String(), `"no_change":true`) {
		t.Fatalf("busy insert = %d %s", rejected.Code, rejected.Body.String())
	}
	if current := serve(t, handler, http.MethodGet, "/api/v1/session"); !strings.Contains(current.Body.String(), `"media_name":"first.p"`) {
		t.Fatalf("busy insert lost prior filename: %s", current.Body.String())
	}
	service.err = &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "completed insert rejection", Phase: "input", NoChange: true}
	if rejected := request("/api/v1/session/live-media", "second.p"); rejected.Code == http.StatusOK || !strings.Contains(rejected.Body.String(), `"no_change":true`) {
		t.Fatalf("acknowledged insert rejection = %d %s", rejected.Code, rejected.Body.String())
	}
	if current := serve(t, handler, http.MethodGet, "/api/v1/session"); !strings.Contains(current.Body.String(), `"media_name":"first.p"`) {
		t.Fatalf("acknowledged insert rejection lost prior filename: %s", current.Body.String())
	}
	service.err = &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "FES GP command rejected with response 3", Phase: "input"}
	if uncertain := request("/api/v1/session/live-media", "second.p"); uncertain.Code == http.StatusOK {
		t.Fatalf("numeric rejection unexpectedly succeeded: %s", uncertain.Body.String())
	}
	if current := serve(t, handler, http.MethodGet, "/api/v1/session"); strings.Contains(current.Body.String(), `"media_name"`) {
		t.Fatalf("numeric rejection retained stale filename: %s", current.Body.String())
	}
	service.err = nil
	if inserted := request("/api/v1/session/live-media", "third.p"); inserted.Code != http.StatusOK || !strings.Contains(inserted.Body.String(), `"media_name":"third.p"`) {
		t.Fatalf("restore insert = %d %s", inserted.Code, inserted.Body.String())
	}
	service.err = protocol.LiveMediaBusyError()
	if rejected := request("/api/v1/session/live-media/clear", ""); rejected.Code != http.StatusConflict {
		t.Fatalf("busy eject = %d %s", rejected.Code, rejected.Body.String())
	}
	if current := serve(t, handler, http.MethodGet, "/api/v1/session"); !strings.Contains(current.Body.String(), `"media_name":"third.p"`) {
		t.Fatalf("busy eject lost prior filename: %s", current.Body.String())
	}
	service.err = &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "FES GP command rejected with response 4", Phase: "input"}
	if rejected := request("/api/v1/session/live-media/clear", ""); rejected.Code == http.StatusOK || strings.Contains(rejected.Body.String(), `"no_change":true`) {
		t.Fatalf("post-eject state failure was treated as no change: %d %s", rejected.Code, rejected.Body.String())
	}
	if current := serve(t, handler, http.MethodGet, "/api/v1/session"); strings.Contains(current.Body.String(), `"media_name"`) {
		t.Fatalf("post-eject state failure retained stale filename: %s", current.Body.String())
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

func TestLiveMediaNameDoesNotSurviveSameGenerationRelaunch(t *testing.T) {
	packageID := strings.Repeat("a", 64)
	gameID := "fpga-zx81"
	active := protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{
		PackageID: packageID, Generation: 9,
		ABI:              protocol.RuntimeContract{ID: "fes.computer", Major: 1},
		ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.media.zx81-tape", Major: 1}},
	}}
	service := &liveMediaService{fakeService: fakeService{
		game:          catalog.Game{ID: gameID, Kind: catalog.SourceKindCorePackage},
		launch:        protocol.CachedLaunchResponse{Status: active},
		status:        active,
		sessionTarget: "dev",
	}}
	handler := hostapi.New(service)
	firstLaunch := launchSession(t, handler, gameID)
	if firstLaunch.Code != http.StatusOK {
		t.Fatalf("first launch = %d %s", firstLaunch.Code, firstLaunch.Body.String())
	}
	var first struct {
		ID       string `json:"id"`
		FlightID string `json:"flight_id"`
	}
	if err := json.Unmarshal(firstLaunch.Body.Bytes(), &first); err != nil || first.ID == "" || first.FlightID == "" {
		t.Fatalf("first launch identity = %s: %v", firstLaunch.Body.String(), err)
	}

	insert := httptest.NewRequest(http.MethodPost, "/api/v1/session/live-media", strings.NewReader(`{"media_id":"`+strings.Repeat("b", 64)+`","name":"maze.p"}`))
	insert.Host = "127.0.0.1"
	insert.Header.Set("Content-Type", "application/json")
	insert.Header.Set(protocol.HostSessionIDHeader, first.ID)
	protocol.DevelopmentMediaBinding{PackageID: packageID, Generation: 9, Target: "dev"}.SetHeaders(insert.Header)
	inserted := httptest.NewRecorder()
	handler.ServeHTTP(inserted, insert)
	if inserted.Code != http.StatusOK || !strings.Contains(inserted.Body.String(), `"media_name":"maze.p"`) {
		t.Fatalf("insert = %d %s", inserted.Code, inserted.Body.String())
	}

	// The second package launch reports the same target package generation. Its
	// new flight must still retire the filename confirmed for the first one.
	secondLaunch := launchSession(t, handler, gameID)
	if secondLaunch.Code != http.StatusOK {
		t.Fatalf("same-generation relaunch = %d %s", secondLaunch.Code, secondLaunch.Body.String())
	}
	var second struct {
		FlightID string `json:"flight_id"`
	}
	if err := json.Unmarshal(secondLaunch.Body.Bytes(), &second); err != nil || second.FlightID == "" || second.FlightID == first.FlightID {
		t.Fatalf("relaunch flight = %s, first=%q: %v", secondLaunch.Body.String(), first.FlightID, err)
	}
	if strings.Contains(secondLaunch.Body.String(), `"media_name"`) {
		t.Fatalf("same-generation relaunch revived stale filename: %s", secondLaunch.Body.String())
	}
	if current := serve(t, handler, http.MethodGet, "/api/v1/session"); strings.Contains(current.Body.String(), `"media_name"`) {
		t.Fatalf("status retained previous-flight filename: %s", current.Body.String())
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

// A Spectrum cassette inserted by digest has no name; the service reads the
// media to tell a tape from a snapshot.
func TestLiveMediaPassesANamelessInsert(t *testing.T) {
	status := protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{
		PackageID: strings.Repeat("a", 64), Generation: 9,
		ABI:              protocol.RuntimeContract{ID: "fes.computer", Major: 1},
		ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.media.spectrum-tape", Major: 2}},
	}}
	service := &liveMediaService{fakeService: fakeService{status: status, sessionTarget: "dev"}}
	handler := hostapi.New(service)
	var session struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(serve(t, handler, http.MethodGet, "/api/v1/session").Body.Bytes(), &session); err != nil || session.ID == "" {
		t.Fatalf("session: %v", err)
	}
	for body, want := range map[string]int{
		`{"media_id":"` + strings.Repeat("b", 64) + `"}`:                      200,
		`{"media_id":"` + strings.Repeat("b", 64) + `","name":"game"}`:        400,
		`{"media_id":"` + strings.Repeat("b", 64) + `","name":"../game.z80"}`: 400,
	} {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/session/live-media", strings.NewReader(body))
		r.Host = "127.0.0.1"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-FogCast-Session-ID", session.ID)
		protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9, Target: "dev"}.SetHeaders(r.Header)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("%s: status %d, want %d: %s", body, w.Code, want, w.Body)
		}
	}
	if service.replaceCalls != 1 || service.name != "" {
		t.Fatalf("calls=%d name=%q", service.replaceCalls, service.name)
	}
}
