package hostapi_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/corepackage"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

type coreLibraryAPIService struct {
	fakeService
	imports   int
	selection []string
}

func (s *coreLibraryAPIService) ImportCorePackage(context.Context, int64, io.Reader) (corepackage.Inspection, bool, error) {
	s.imports++
	return corepackage.Inspection{PackageID: strings.Repeat("a", 64)}, true, nil
}
func (s *coreLibraryAPIService) CorePackages(context.Context) ([]fogcast.InstalledCorePackage, error) {
	return []fogcast.InstalledCorePackage{}, nil
}
func (s *coreLibraryAPIService) CorePackage(context.Context, string) (corepackage.Inspection, error) {
	return corepackage.Inspection{}, nil
}
func (s *coreLibraryAPIService) CorePackageCompatibility(context.Context, string) (fogcast.CoreCompatibility, error) {
	return fogcast.CoreCompatibility{}, nil
}
func (s *coreLibraryAPIService) CoreEntries(context.Context) ([]catalog.CoreEntry, error) {
	return []catalog.CoreEntry{}, nil
}
func (s *coreLibraryAPIService) CoreEntry(context.Context, string) (catalog.CoreEntry, error) {
	return catalog.CoreEntry{}, nil
}
func (s *coreLibraryAPIService) CreateCoreEntry(context.Context, string, string) (catalog.CoreEntry, error) {
	return catalog.CoreEntry{}, nil
}
func (s *coreLibraryAPIService) SelectCoreEntry(_ context.Context, game, expected, id string) (catalog.CoreEntry, error) {
	s.selection = []string{game, expected, id}
	return catalog.CoreEntry{GameID: game, PackageID: id}, nil
}
func TestCorePackageImportAndSelectionAPI(t *testing.T) {
	s := &coreLibraryAPIService{}
	handler := hostapi.New(s)
	for _, tc := range []struct {
		method, path, body, content string
		want                        int
	}{
		{"POST", "/api/v1/core-packages", "archive", "text/plain", 400},
		{"POST", "/api/v1/core-packages", "archive", "application/octet-stream", 201},
		{"GET", "/api/v1/core-packages", "", "", 200},
		{"PUT", "/api/v1/library/core-entries/core-pong", `{"package_id":"` + strings.Repeat("b", 64) + `","expected_package_id":"` + strings.Repeat("a", 64) + `"}`, "application/json", 200},
		{"PUT", "/api/v1/library/core-entries/core-pong", `{"package_id":"bad","extra":true}`, "application/json", 400},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Host = "127.0.0.1"
		if tc.content != "" {
			req.Header.Set("Content-Type", tc.content)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != tc.want {
			t.Fatalf("%s %s = %d %s", tc.method, tc.path, response.Code, response.Body.String())
		}
	}
	if s.imports != 1 || len(s.selection) != 3 || s.selection[1] != strings.Repeat("a", 64) {
		t.Fatalf("imports=%d selection=%v", s.imports, s.selection)
	}
}

func TestCorePackageReadRoutesRejectBodies(t *testing.T) {
	s := &coreLibraryAPIService{}
	handler := hostapi.New(s)
	id := strings.Repeat("a", 64)
	for _, test := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/core-packages"},
		{http.MethodGet, "/api/v1/core-packages/" + id},
		{http.MethodPost, "/api/v1/core-packages/" + id + "/compatibility"},
		{http.MethodGet, "/api/v1/library/core-entries"},
		{http.MethodGet, "/api/v1/library/core-entries/core-pong"},
	} {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, strings.NewReader("unexpected"))
			request.Host = "127.0.0.1"
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"BAD_REQUEST"`) {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

type coreDataAPIService struct {
	coreLibraryAPIService
	updates int
	update  protocol.CoreSettingsUpdate
}

func (s *coreDataAPIService) CoreSettings(context.Context, string) (fogcast.CoreDataResult, error) {
	return fogcast.CoreDataResult{}, nil
}
func (s *coreDataAPIService) CoreProgress(context.Context, string) (fogcast.CoreDataResult, error) {
	return fogcast.CoreDataResult{}, nil
}
func (s *coreDataAPIService) SetCoreSettings(_ context.Context, _ string, u protocol.CoreSettingsUpdate) (fogcast.CoreDataResult, error) {
	s.updates++
	s.update = u
	return fogcast.CoreDataResult{}, nil
}
func TestCoreDataAPIRequiresCompleteTypedCASAndEmptyReads(t *testing.T) {
	s := &coreDataAPIService{}
	handler := hostapi.New(s)
	for _, tc := range []struct {
		method, path, body string
		code               int
	}{
		{"GET", "settings", "", 200}, {"GET", "progress", "", 200}, {"GET", "progress", "{}", 400},
		{"PUT", "settings", `{"expected_package_id":"` + strings.Repeat("a", 64) + `","expected_revision":"absent","paddle_speed":2}`, 200},
		{"PUT", "settings", `{"expected_package_id":"` + strings.Repeat("a", 64) + `","expected_revision":"absent"}`, 400},
		{"PUT", "settings", `{"expected_package_id":"` + strings.Repeat("a", 64) + `","expected_revision":"absent","paddle_speed":null}`, 400},
		{"PUT", "settings", `{"expected_package_id":"` + strings.Repeat("a", 64) + `","expected_revision":"absent","paddle_speed":3}`, 400},
		{"PUT", "settings", `{"expected_package_id":"` + strings.Repeat("a", 64) + `","expected_revision":"absent","paddle_speed":1,"data_root":"/tmp"}`, 400},
	} {
		req := httptest.NewRequest(tc.method, "/api/v1/library/core-entries/core-pong/"+tc.path, strings.NewReader(tc.body))
		req.Host = "127.0.0.1"
		req.Header.Set("Content-Type", "application/json")
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		if out.Code != tc.code {
			t.Fatalf("%s %s %s got %d %s", tc.method, tc.path, tc.body, out.Code, out.Body)
		}
	}
	if s.updates != 1 || s.update.PaddleSpeed != 2 {
		t.Fatalf("updates=%d update=%+v", s.updates, s.update)
	}
}
