package hostapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

type diskDataService struct {
	liveMediaService
	inserts, saves int
	binding        protocol.LibraryMediaBinding
	err            error
	insert         func(context.Context, protocol.LibraryMediaBinding) (protocol.Status, error)
	savedStatus    *protocol.Status
}

func (s *diskDataService) InsertLibraryDisk(ctx context.Context, b protocol.LibraryMediaBinding) (protocol.Status, error) {
	s.inserts++
	s.binding = b
	if s.insert != nil {
		return s.insert(ctx, b)
	}
	return s.status, s.err
}
func (s *diskDataService) SaveMedia(_ context.Context, b protocol.MediaUnitBinding) (protocol.Status, error) {
	s.saves++
	if s.savedStatus != nil {
		return *s.savedStatus, s.err
	}
	return s.status, s.err
}

type sessionProjectionService struct{ diskDataService }

func (s *sessionProjectionService) TargetIDForName(name string) string {
	if name == s.sessionTarget {
		return s.sessionTargetID
	}
	return ""
}

func (s *sessionProjectionService) LoadDevelopmentMedia(_ context.Context, _ int64, _ io.Reader, _ protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	return s.status, nil
}

func (s *sessionProjectionService) SetSessionDisplay(_ context.Context, _ protocol.SessionDisplay, _ protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	return s.status, nil
}

func TestDiskDataResponsesPreserveSessionExecution(t *testing.T) {
	for _, tc := range []struct{ name, execution string }{
		{name: "recognized native package", execution: fogcast.ExecutionFPGANative},
		{name: "development package", execution: fogcast.ExecutionFPGADevelopment},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := protocol.LibraryMediaBinding{MediaUnitBinding: protocol.MediaUnitBinding{PackageID: strings.Repeat("a", 64), Generation: 9, Target: "dev", TargetID: "target-1"}, GameID: "pcxt", BaseMediaID: strings.Repeat("b", 64)}
			game, system := b.GameID, protocol.System("pcxt")
			status := protocol.Status{State: protocol.StateActive, Development: true, GameID: &game, System: &system, CorePackage: &protocol.CorePackageStatus{PackageID: b.PackageID, Generation: b.Generation, ABI: protocol.RuntimeContract{ID: "fes.computer", Major: 1, Minor: 1}}}
			revision := strings.Repeat("d", 64)
			saved := status
			pkg := *status.CorePackage
			pkg.MediaUnits = []protocol.MediaUnitStatus{{Unit: 0, State: protocol.MediaUnitReady, Persistence: &protocol.MediaDataStatus{Mode: "persistent", GameID: game, BaseMediaID: b.BaseMediaID, Revision: revision}}}
			saved.CorePackage = &pkg
			s := &sessionProjectionService{diskDataService{liveMediaService: liveMediaService{fakeService: fakeService{sessionTarget: b.Target, sessionTargetID: b.TargetID, status: status, launch: protocol.CachedLaunchResponse{Status: status}, execution: tc.execution, game: catalog.Game{ID: game, Kind: catalog.SourceKindCorePackage}}}, savedStatus: &saved}}
			handler := hostapi.New(s)
			if launched := launchSessionOn(t, handler, game, b.Target); launched.Code != http.StatusOK {
				t.Fatalf("launch: status=%d body=%s", launched.Code, launched.Body)
			}
			type identity struct {
				ID          string `json:"id"`
				Target      string `json:"target"`
				TargetID    string `json:"target_id"`
				FlightID    string `json:"flight_id"`
				GameID      string `json:"game_id"`
				State       string `json:"state"`
				Execution   string `json:"execution"`
				CorePackage struct {
					PackageID  string                     `json:"package_id"`
					Generation uint64                     `json:"generation"`
					MediaUnits []protocol.MediaUnitStatus `json:"media_units"`
				} `json:"core_package"`
			}
			read := func(w *httptest.ResponseRecorder) identity {
				t.Helper()
				var got identity
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || w.Code != http.StatusOK {
					t.Fatalf("status=%d body=%s", w.Code, w.Body)
				}
				return got
			}
			before := read(serve(t, handler, "GET", "/api/v1/session?target="+b.Target))
			if before.ID == "" || before.FlightID == "" || before.Target != b.Target || before.TargetID != b.TargetID || before.GameID != game || before.State != "active" || before.Execution != tc.execution || before.CorePackage.PackageID != b.PackageID || before.CorePackage.Generation != b.Generation {
				t.Fatalf("unexpected initial session: %+v", before)
			}
			for _, operation := range []struct{ path, body, contentType string }{
				{"/api/v1/session/disk/insert", `{"game_id":"pcxt","base_media_id":"` + b.BaseMediaID + `"}`, "application/json"},
				{"/api/v1/session/disk/save", "", ""},
				{"/api/v1/session/live-media", `{"media_id":"` + strings.Repeat("c", 64) + `","name":"test.dsk"}`, "application/json"},
				{"/api/v1/session/live-media/clear", "", ""},
				{"/api/v1/session/development-media", "raw", "application/octet-stream"},
				{"/api/v1/session/display", `{"visible":false,"focus":"machine"}`, "application/json"},
			} {
				t.Run(operation.path, func(t *testing.T) {
					r := httptest.NewRequest("POST", operation.path, strings.NewReader(operation.body))
					r.Host = "127.0.0.1"
					b.SetHeaders(r.Header)
					r.Header.Set(protocol.HostSessionIDHeader, before.ID)
					if operation.contentType != "" {
						r.Header.Set("Content-Type", operation.contentType)
					}
					w := httptest.NewRecorder()
					handler.ServeHTTP(w, r)
					got := read(w)
					if got.ID != before.ID || got.Target != before.Target || got.TargetID != before.TargetID || got.FlightID != before.FlightID || got.GameID != before.GameID || got.State != before.State || got.Execution != before.Execution || got.CorePackage.PackageID != before.CorePackage.PackageID || got.CorePackage.Generation != before.CorePackage.Generation {
						t.Fatalf("session identity changed: initial=%+v result=%+v", before, got)
					}
					if operation.path == "/api/v1/session/disk/save" && (len(got.CorePackage.MediaUnits) != 1 || got.CorePackage.MediaUnits[0].Persistence == nil || got.CorePackage.MediaUnits[0].Persistence.Revision != revision || got.CorePackage.MediaUnits[0].Persistence.GameID != game || got.CorePackage.MediaUnits[0].Persistence.BaseMediaID != b.BaseMediaID) {
						t.Fatalf("saved persistence binding changed: %+v", got.CorePackage.MediaUnits)
					}
				})
			}
			if s.inserts != 1 || s.saves != 1 {
				t.Fatalf("unexpected operation counts: inserts=%d saves=%d", s.inserts, s.saves)
			}
		})
	}
}

func TestDiskDataHostRoutesKeepSessionAndRejectStaleBinding(t *testing.T) {
	b := protocol.LibraryMediaBinding{MediaUnitBinding: protocol.MediaUnitBinding{PackageID: strings.Repeat("a", 64), Generation: 9, Target: "dev"}, GameID: "st-desktop", BaseMediaID: strings.Repeat("b", 64)}
	s := &diskDataService{liveMediaService: liveMediaService{fakeService: fakeService{sessionTarget: "dev", status: protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{PackageID: b.PackageID, Generation: b.Generation, ABI: protocol.RuntimeContract{ID: "fes.computer", Major: 1}}}}}}
	handler := hostapi.New(s)
	before := serve(t, handler, "GET", "/api/v1/session")
	var session struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(before.Body.Bytes(), &session) != nil || session.ID == "" {
		t.Fatal(before.Body)
	}
	sendContext := func(ctx context.Context, path, body string, change func(*http.Request)) *httptest.ResponseRecorder {
		r := httptest.NewRequestWithContext(ctx, "POST", path, strings.NewReader(body))
		r.Host = "127.0.0.1"
		b.SetHeaders(r.Header)
		r.Header.Set(protocol.HostSessionIDHeader, session.ID)
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if change != nil {
			change(r)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	send := func(path, body string, change func(*http.Request)) *httptest.ResponseRecorder {
		return sendContext(context.Background(), path, body, change)
	}
	liveInsert := func(name string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/session/live-media", strings.NewReader(`{"media_id":"`+strings.Repeat("c", 64)+`","name":"`+name+`"}`))
		r.Host = "127.0.0.1"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set(protocol.HostSessionIDHeader, session.ID)
		protocol.DevelopmentMediaBinding{PackageID: b.PackageID, Generation: b.Generation, Target: b.Target}.SetHeaders(r.Header)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	body := `{"game_id":"st-desktop","base_media_id":"` + b.BaseMediaID + `"}`
	if live := liveInsert("old.d64"); live.Code != http.StatusOK || !strings.Contains(live.Body.String(), `"media_name":"old.d64"`) || !strings.Contains(live.Body.String(), `"execution":"fpga_native"`) {
		t.Fatalf("live insert status=%d body=%s", live.Code, live.Body)
	}
	dir := t.TempDir()
	config := filepath.Join(dir, "config.toml")
	configText := "selected_target = \"none\"\nrequest_timeout_seconds = 15\nupload_timeout_seconds = 120\n[[targets]]\nname = \"none\"\nenabled = false\naddress = \"\"\nagent = \"\"\n"
	if err := os.WriteFile(config, []byte(configText), 0600); err != nil {
		t.Fatal(err)
	}
	actual, err := fogcast.Open(context.Background(), fogcast.Paths{
		Config: config, Index: filepath.Join(dir, "index.sqlite3"), Staging: filepath.Join(dir, "staging"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = actual.Close() })
	s.insert = actual.InsertLibraryDisk
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if rejected := sendContext(canceled, "/api/v1/session/disk/insert", body, nil); rejected.Code == http.StatusOK || !strings.Contains(rejected.Body.String(), `"no_change":true`) {
		t.Fatalf("canceled service insert = %d %s", rejected.Code, rejected.Body.String())
	}
	if current := serve(t, handler, "GET", "/api/v1/session"); !strings.Contains(current.Body.String(), `"media_name":"old.d64"`) {
		t.Fatalf("service lifecycle cancellation lost prior filename: %s", current.Body.String())
	}
	s.insert = nil
	identity := protocol.MediaUnitIdentityError()
	identity.NoChange = true
	for _, rejection := range []error{
		protocol.LiveMediaBusyError(),
		identity,
		&protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "completed insert rejection", Phase: "input", NoChange: true},
	} {
		s.err = rejection
		if rejected := send("/api/v1/session/disk/insert", body, nil); rejected.Code == http.StatusOK || !strings.Contains(rejected.Body.String(), `"no_change":true`) {
			t.Fatalf("rejected library insert = %d %s", rejected.Code, rejected.Body.String())
		}
		if current := serve(t, handler, "GET", "/api/v1/session"); !strings.Contains(current.Body.String(), `"media_name":"old.d64"`) {
			t.Fatalf("rejected library insert lost prior filename: %s", current.Body.String())
		}
	}
	s.err = nil
	if inserted := send("/api/v1/session/disk/insert", body, nil); inserted.Code != http.StatusOK || s.inserts != 5 || s.binding != b {
		t.Fatal("insert")
	}
	if strings.Contains(serve(t, handler, "GET", "/api/v1/session").Body.String(), `"media_name"`) {
		t.Fatal("library disk insertion retained the live-media filename")
	}
	if live := liveInsert("saved.d64"); live.Code != http.StatusOK || !strings.Contains(live.Body.String(), `"media_name":"saved.d64"`) {
		t.Fatalf("second live insert status=%d body=%s", live.Code, live.Body)
	}
	if saved := send("/api/v1/session/disk/save", "", nil); saved.Code != http.StatusOK || s.saves != 1 || !strings.Contains(saved.Body.String(), `"media_name":"saved.d64"`) {
		t.Fatal("save")
	}
	if send("/api/v1/session/disk/insert", strings.TrimSuffix(body, "}")+`,"extra":1}`, nil).Code == http.StatusOK {
		t.Fatal("unknown field")
	}
	if send("/api/v1/session/disk/save", "", func(r *http.Request) { r.Header.Set(protocol.HostSessionIDHeader, "stale") }).Code == http.StatusOK {
		t.Fatal("stale session")
	}
	if s.inserts != 5 || s.saves != 1 || s.replaceCalls != 2 {
		t.Fatal("invalid request dispatched")
	}
	s.err = &protocol.APIError{Code: protocol.CodeSaveFailed, Message: "publish failed", Phase: "save"}
	if send("/api/v1/session/disk/save", "", nil).Code == http.StatusOK {
		t.Fatal("failed save accepted")
	}
	if after := serve(t, handler, "GET", "/api/v1/session"); !strings.Contains(after.Body.String(), session.ID) || !strings.Contains(after.Body.String(), `"media_name":"saved.d64"`) {
		t.Fatalf("save failure changed the session or mounted media name: %s", after.Body)
	}
}
