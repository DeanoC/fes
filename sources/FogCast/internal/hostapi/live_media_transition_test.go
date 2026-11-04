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

// Exercise the actual HTTP coordinator: display handoff holds admission while
// insert/eject arrives with the already captured session and package binding.
func TestLiveMediaWaitsForDisplayAdmission(t *testing.T) {
	for _, clear := range []bool{false, true} {
		for _, admission := range []string{"serialized", "cancelled", "expired"} {
			rejectAdmission := admission != "serialized"
			name := "insert"
			if clear {
				name = "eject"
			}
			name += "/" + admission
			t.Run(name, func(t *testing.T) {
				packageID := strings.Repeat("a", 64)
				service := &blockedMediaDisplay{
					liveMediaService: liveMediaService{fakeService: fakeService{sessionTarget: "dev", status: protocol.Status{
						State: protocol.StateActive, CorePackage: &protocol.CorePackageStatus{
							PackageID: packageID, Generation: 9,
							ABI: protocol.RuntimeContract{ID: "fes.computer", Major: 1},
						},
					}}},
					entered: make(chan struct{}), release: make(chan struct{}),
				}
				handler := hostapi.New(service)
				var session struct{ ID string }
				initial := serve(t, handler, http.MethodGet, "/api/v1/session")
				if err := json.Unmarshal(initial.Body.Bytes(), &session); err != nil || session.ID == "" {
					t.Fatalf("session: %s, %v", initial.Body, err)
				}
				request := func(ctx context.Context, path, body string) *httptest.ResponseRecorder {
					r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).WithContext(ctx)
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
				mediaBody := `{"media_id":"` + strings.Repeat("b", 64) + `","name":"prior.p"}`
				if w := request(context.Background(), "/api/v1/session/live-media", mediaBody); w.Code != http.StatusOK {
					t.Fatalf("initial insert: %d %s", w.Code, w.Body)
				}
				displayDone := make(chan *httptest.ResponseRecorder, 1)
				go func() { displayDone <- request(context.Background(), "/api/v1/session/display", `{"visible":true}`) }()
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
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				path := "/api/v1/session/live-media"
				if clear {
					path, mediaBody = path+"/clear", ""
				}
				if admission == "cancelled" {
					cancel()
				}
				done := make(chan *httptest.ResponseRecorder, 1)
				go func() { done <- request(ctx, path, mediaBody) }()
				if rejectAdmission {
					select {
					case w := <-done:
						if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"no_change":true`) {
							t.Fatalf("pre-dispatch rejection lost no-change guarantee: %d %s", w.Code, w.Body)
						}
					case <-time.After(time.Second):
						t.Fatal("rejected admission did not return")
					}
				} else {
					select {
					case w := <-done:
						t.Fatalf("media did not wait for display handoff: %d %s", w.Code, w.Body)
					case <-time.After(50 * time.Millisecond):
					}
				}
				release.Do(func() { close(service.release) })
				if !rejectAdmission {
					select {
					case w := <-done:
						if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), packageID) || !strings.Contains(w.Body.String(), `"generation":9`) {
							t.Fatalf("serialized media: %d %s", w.Code, w.Body)
						}
					case <-time.After(time.Second):
						t.Fatal("media did not proceed after display handoff")
					}
				}
				wantInsert, wantClear := 1, 0
				if !rejectAdmission {
					if clear {
						wantClear++
					} else {
						wantInsert++
					}
				}
				if service.replaceCalls != wantInsert || service.clearCalls != wantClear {
					t.Fatalf("mutation counts insert=%d eject=%d, want %d/%d", service.replaceCalls, service.clearCalls, wantInsert, wantClear)
				}
				if rejectAdmission {
					current := serve(t, handler, http.MethodGet, "/api/v1/session")
					if !strings.Contains(current.Body.String(), `"media_name":"prior.p"`) {
						t.Fatalf("rejection lost prior medium: %s", current.Body)
					}
				}
			})
		}
	}
}
