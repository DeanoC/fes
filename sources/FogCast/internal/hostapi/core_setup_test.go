package hostapi_test

import (
	"context"
	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"net/http/httptest"
	"strings"
	"testing"
)

type coreSetupAPIService struct {
	fakeService
	calls int
}

func (s *coreSetupAPIService) CoreSetup(context.Context, string, string, string) (fogcast.CoreSetup, error) {
	s.calls++
	return fogcast.CoreSetup{}, nil
}
func (s *coreSetupAPIService) CreateCoreSetupEntry(context.Context, fogcast.CoreSetupRequest) (catalog.CoreEntry, error) {
	s.calls++
	return catalog.CoreEntry{}, nil
}
func TestCoreSetupAPI(t *testing.T) {
	s := &coreSetupAPIService{}
	h := hostapi.New(s)
	pid := strings.Repeat("a", 64)
	for _, tc := range []struct {
		method, path, body string
		code               int
	}{{"GET", "/api/v1/core-catalog/fes.sms/setup?source_id=fes-first-party&package_id=" + pid, "", 200}, {"POST", "/api/v1/core-catalog/entries", `{"library_source_id":"test-library","source_id":"fes-first-party","core_id":"fes.sms","package_id":"` + pid + `","title":"Test"}`, 200}, {"POST", "/api/v1/core-catalog/entries", `{"library_source_id":"test-library","source_id":"fes-first-party","core_id":"fes.sms","package_id":"` + pid + `","title":"Test","path":"/private"}`, 400}} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		r.Host = "127.0.0.1"
		if tc.method == "POST" {
			r.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body)
		}
	}
	if s.calls != 2 {
		t.Fatal(s.calls)
	}
}
