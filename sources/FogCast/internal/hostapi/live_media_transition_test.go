package hostapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

type blockedMediaDisplay struct {
	liveMediaService
	entered chan struct{}
	release chan struct{}
}

func (s *blockedMediaDisplay) SetSessionDisplay(ctx context.Context, _ protocol.SessionDisplay, _ protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	close(s.entered)
	select {
	case <-s.release:
		return s.status, nil
	case <-ctx.Done():
		return protocol.Status{}, ctx.Err()
	}
}

// A host admission rejection must not dispatch media or discard its confirmed name.
func TestLiveMediaAdmissionBusyReportsNoChange(t *testing.T) {
	for _, clear := range []bool{false, true} {
		name := "insert"
		if clear {
			name = "eject"
		}
		t.Run(name, func(t *testing.T) {
			packageID := strings.Repeat("a", 64)
			service := &blockedMediaDisplay{
				liveMediaService: liveMediaService{fakeService: fakeService{sessionTarget: "dev", status: protocol.Status{
					State: protocol.StateActive, CorePackage: &protocol.CorePackageStatus{PackageID: packageID, Generation: 9, ABI: protocol.RuntimeContract{ID: "fes.computer", Major: 1}},
				}}}, entered: make(chan struct{}), release: make(chan struct{}),
			}
			handler := hostapi.New(service)
			var session struct{ ID string }
			initial := serve(t, handler, http.MethodGet, "/api/v1/session")
			if err := json.Unmarshal(initial.Body.Bytes(), &session); err != nil || session.ID == "" {
				t.Fatalf("session: %s, %v", initial.Body, err)
			}
			request := func(path, body string) *httptest.ResponseRecorder {
				r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
				r.Host = "127.0.0.1"
				if body != "" {
					r.Header.Set("Content-Type", "application/json")
				}
				r.Header.Set(protocol.HostSessionIDHeader, session.ID)
				protocol.DevelopmentMediaBinding{PackageID: packageID, Generation: 9, Target: "dev"}.SetHeaders(r.Header)
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				return w
			}
			body := `{"media_id":"` + strings.Repeat("b", 64) + `","name":"prior.p"}`
			if w := request("/api/v1/session/live-media", body); w.Code != http.StatusOK {
				t.Fatalf("initial insert: %d %s", w.Code, w.Body)
			}
			displayDone := make(chan *httptest.ResponseRecorder, 1)
			go func() { displayDone <- request("/api/v1/session/display", `{"visible":true}`) }()
			var release sync.Once
			t.Cleanup(func() {
				release.Do(func() { close(service.release) })
				select {
				case w := <-displayDone:
					if w.Code != http.StatusOK {
						t.Errorf("display: %d %s", w.Code, w.Body)
					}
				case <-time.After(2 * time.Second):
					t.Error("display request did not finish")
				}
			})
			select {
			case <-service.entered:
			case <-time.After(2 * time.Second):
				t.Fatal("display did not acquire admission")
			}
			path := "/api/v1/session/live-media"
			if clear {
				path, body = path+"/clear", ""
			}
			rejected := request(path, body)
			if rejected.Code != http.StatusConflict || !strings.Contains(rejected.Body.String(), `"no_change":true`) {
				t.Fatalf("admission rejection: %d %s", rejected.Code, rejected.Body)
			}
			release.Do(func() { close(service.release) })
			current := serve(t, handler, http.MethodGet, "/api/v1/session")
			if !strings.Contains(current.Body.String(), `"media_name":"prior.p"`) || !strings.Contains(current.Body.String(), `"generation":9`) {
				t.Fatalf("rejection changed media identity: %s", current.Body)
			}
			if service.replaceCalls != 1 || service.clearCalls != 0 {
				t.Fatalf("rejected operation dispatched: insert=%d eject=%d", service.replaceCalls, service.clearCalls)
			}
		})
	}
}
