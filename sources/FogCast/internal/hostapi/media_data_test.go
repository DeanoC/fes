package hostapi_test

import (
	"context"
	"encoding/json"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type diskDataService struct {
	fakeService
	inserts, saves int
	binding        protocol.LibraryMediaBinding
	err            error
}

func (s *diskDataService) InsertLibraryDisk(_ context.Context, b protocol.LibraryMediaBinding) (protocol.Status, error) {
	s.inserts++
	s.binding = b
	return s.status, s.err
}
func (s *diskDataService) SaveMedia(_ context.Context, b protocol.MediaUnitBinding) (protocol.Status, error) {
	s.saves++
	return s.status, s.err
}
func TestDiskDataHostRoutesKeepSessionAndRejectStaleBinding(t *testing.T) {
	b := protocol.LibraryMediaBinding{MediaUnitBinding: protocol.MediaUnitBinding{PackageID: strings.Repeat("a", 64), Generation: 9, Target: "dev"}, GameID: "st-desktop", BaseMediaID: strings.Repeat("b", 64)}
	s := &diskDataService{fakeService: fakeService{sessionTarget: "dev", status: protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{PackageID: b.PackageID, Generation: b.Generation, ABI: protocol.RuntimeContract{ID: "fes.computer", Major: 1}}}}}
	handler := hostapi.New(s)
	before := serve(t, handler, "GET", "/api/v1/session")
	var session struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(before.Body.Bytes(), &session) != nil || session.ID == "" {
		t.Fatal(before.Body)
	}
	send := func(path, body string, change func(*http.Request)) int {
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
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
		return w.Code
	}
	body := `{"game_id":"st-desktop","base_media_id":"` + b.BaseMediaID + `"}`
	if send("/api/v1/session/disk/insert", body, nil) != 200 || s.inserts != 1 || s.binding != b {
		t.Fatal("insert")
	}
	if send("/api/v1/session/disk/save", "", nil) != 200 || s.saves != 1 {
		t.Fatal("save")
	}
	if send("/api/v1/session/disk/insert", strings.TrimSuffix(body, "}")+`,"extra":1}`, nil) == 200 {
		t.Fatal("unknown field")
	}
	if send("/api/v1/session/disk/save", "", func(r *http.Request) { r.Header.Set(protocol.HostSessionIDHeader, "stale") }) == 200 {
		t.Fatal("stale session")
	}
	if s.inserts != 1 || s.saves != 1 {
		t.Fatal("invalid request dispatched")
	}
	s.err = &protocol.APIError{Code: protocol.CodeSaveFailed, Message: "publish failed", Phase: "save"}
	if send("/api/v1/session/disk/save", "", nil) == 200 {
		t.Fatal("failed save accepted")
	}
	if after := serve(t, handler, "GET", "/api/v1/session"); !strings.Contains(after.Body.String(), session.ID) {
		t.Fatal("save failure lost session")
	}
}
