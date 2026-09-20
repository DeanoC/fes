package hostapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

type mediaService struct {
	fakeService
	calls int
}

func (s *mediaService) LoadDevelopmentMedia(_ context.Context, n int64, r io.Reader, b protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	s.calls++
	return s.status, nil
}
func TestDevelopmentMediaHostSessionBindingPreservesInput(t *testing.T) {
	status := protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 9, ABI: protocol.RuntimeContract{ID: "fes.simple-computer", Major: 1}, ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.media.blob", Major: 1}}}}
	service := &mediaService{fakeService: fakeService{status: status, sessionTarget: "dev"}}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputAttached, Ready: true}}
	handler := hostapi.New(service, hostapi.WithRemoteInput(input))
	before := serve(t, handler, http.MethodGet, "/api/v1/session")
	detachedBefore := len(input.detach)
	var session struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(before.Body.Bytes(), &session); err != nil || session.ID == "" {
		t.Fatalf("session: %s", before.Body)
	}
	for _, id := range []string{"stale-host", session.ID} {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/session/development-media", strings.NewReader("raw"))
		r.Host = "127.0.0.1"
		r.Header.Set("Content-Type", "application/octet-stream")
		r.Header.Set("X-FogCast-Session-ID", id)
		protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9, Target: "dev"}.SetHeaders(r.Header)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		want := 409
		if id == session.ID {
			want = 200
		}
		if w.Code != want {
			t.Fatalf("status=%d body=%s", w.Code, w.Body)
		}
	}
	if service.calls != 1 || len(input.detach) != detachedBefore {
		t.Fatalf("calls=%d detached=%v", service.calls, input.detach)
	}
}
