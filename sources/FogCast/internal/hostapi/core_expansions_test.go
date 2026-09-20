package hostapi_test

import (
	"context"
	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type expansionAPIService struct {
	coreLibraryAPIService
	bound []string
}

func (s *expansionAPIService) ImportCoreExpansion(context.Context, int64, io.Reader) (catalog.CoreExpansion, error) {
	s.imports++
	return catalog.CoreExpansion{ExpansionID: strings.Repeat("b", 64)}, nil
}
func (s *expansionAPIService) CoreExpansions(context.Context) ([]catalog.CoreExpansion, error) {
	return []catalog.CoreExpansion{}, nil
}
func (s *expansionAPIService) CoreEntryExpansion(_ context.Context, id string) (catalog.CoreEntryExpansion, error) {
	return catalog.CoreEntryExpansion{GameID: id}, nil
}
func (s *expansionAPIService) SelectCoreEntryExpansion(_ context.Context, game, base, expected, asset string) (catalog.CoreEntryExpansion, error) {
	s.bound = []string{game, base, expected, asset}
	return catalog.CoreEntryExpansion{GameID: game, ExpansionID: asset}, nil
}
func TestExpansionAPIRequiresExactSelection(t *testing.T) {
	s := &expansionAPIService{}
	handler := hostapi.New(s)
	for _, tc := range []struct {
		body string
		code int
	}{
		{`{"package_id":"` + strings.Repeat("a", 64) + `","expected_expansion_id":"","expansion_id":"` + strings.Repeat("b", 64) + `"}`, 200},
		{`{"package_id":"` + strings.Repeat("a", 64) + `","expansion_id":""}`, 400},
		{`{"package_id":"bad","expected_expansion_id":"","expansion_id":""}`, 400},
	} {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/library/core-entries/fpga-example/expansion", strings.NewReader(tc.body))
		req.Host = "127.0.0.1"
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != tc.code {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	}
	if len(s.bound) != 4 || s.bound[0] != "fpga-example" || s.bound[2] != "" || s.bound[3] != strings.Repeat("b", 64) {
		t.Fatalf("selection %+v", s.bound)
	}
}
