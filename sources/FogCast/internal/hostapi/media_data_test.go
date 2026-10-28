package hostapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	return s.status, s.err
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
	if live := liveInsert("old.d64"); live.Code != http.StatusOK || !strings.Contains(live.Body.String(), `"media_name":"old.d64"`) {
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
