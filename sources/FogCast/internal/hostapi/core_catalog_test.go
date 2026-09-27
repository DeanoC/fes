package hostapi_test

import (
	"context"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"net/http/httptest"
	"strings"
	"testing"
)

type coreCatalogAPIService struct {
	fakeService
	installs int
}

func (s *coreCatalogAPIService) AvailableCores(context.Context) ([]fogcast.AvailableCore, error) {
	return []fogcast.AvailableCore{}, nil
}
func (s *coreCatalogAPIService) InstallAvailableCore(context.Context, string, string, string) (fogcast.InstalledCorePackage, error) {
	s.installs++
	return fogcast.InstalledCorePackage{}, nil
}
func TestCoreCatalogAPI(t *testing.T) {
	s := &coreCatalogAPIService{}
	h := hostapi.New(s)
	for _, tc := range []struct {
		method, path, body string
		code               int
	}{{"GET", "/api/v1/core-catalog", "", 200}, {"POST", "/api/v1/core-catalog/install", `{"source_id":"fes-first-party","core_id":"fes.sms","package_id":"` + strings.Repeat("a", 64) + `"}`, 200}, {"POST", "/api/v1/core-catalog/install", `{"source_id":"x","core_id":"fes.sms","package_id":"bad"}`, 400}} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		r.Host = "127.0.0.1"
		if tc.method == "POST" {
			r.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
	}
	if s.installs != 1 {
		t.Fatal(s.installs)
	}
}
