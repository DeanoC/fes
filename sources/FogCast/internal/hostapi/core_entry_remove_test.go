package hostapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

type removalAPIService struct {
	coreLibraryAPIService
	calls   []string
	failure error
}

func (s *removalAPIService) RemoveCoreEntry(_ context.Context, id string) error {
	s.calls = append(s.calls, id)
	return s.failure
}

func TestRemoveCoreEntryAPI(t *testing.T) {
	s := &removalAPIService{}
	handler := hostapi.New(s)
	request := httptest.NewRequest(http.MethodDelete, "http://127.0.0.1/api/v1/library/core-entries/fpga-zx81", nil)
	request.Body = nil
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != 200 || body["game_id"] != "fpga-zx81" || len(s.calls) != 1 || s.calls[0] != "fpga-zx81" {
		t.Fatalf("remove response %d %s calls %v err %v", response.Code, response.Body.String(), s.calls, err)
	}
	for _, tc := range []struct {
		name, path, body string
		failure          error
		want             int
	}{
		{"bad id", "/api/v1/library/core-entries/Bad_ID", "", nil, 400},
		{"body rejected", "/api/v1/library/core-entries/fpga-zx81", "{}", nil, 400},
		{"busy", "/api/v1/library/core-entries/fpga-zx81", "", &protocol.APIError{Code: protocol.CodeBusy}, 409},
		{"offline", "/api/v1/library/core-entries/fpga-zx81", "", &protocol.APIError{Code: protocol.CodeMiSTerUnavailable}, 503},
		{"not found", "/api/v1/library/core-entries/fpga-zx81", "", &protocol.APIError{Code: protocol.CodeROMNotFound}, 404},
		{"internal", "/api/v1/library/core-entries/fpga-zx81", "", &protocol.APIError{Code: protocol.CodeInternal}, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s.failure = tc.failure
			r := httptest.NewRequest(http.MethodDelete, "http://127.0.0.1"+tc.path, strings.NewReader(tc.body))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status %d body %s", w.Code, w.Body.String())
			}
			if tc.name == "busy" && !strings.Contains(w.Body.String(), "Stop the active session") {
				t.Fatalf("busy response is not actionable: %s", w.Body.String())
			}
			if tc.name == "offline" && !strings.Contains(w.Body.String(), "Connect the kits and retry") {
				t.Fatalf("offline response is not actionable: %s", w.Body.String())
			}
		})
	}
}
