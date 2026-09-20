package httpapi_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/protocol"
)

type mediaController struct {
	fakeDevelopmentController
	calls  int
	stream bool
}

func (c *mediaController) LoadDevelopmentMedia(_ context.Context, size int64, body io.Reader, binding protocol.DevelopmentMediaBinding) (protocol.Status, *protocol.APIError) {
	c.calls++
	c.stream = binding.Stream
	data, err := io.ReadAll(body)
	if err != nil || int64(len(data)) != size || binding.PackageID != strings.Repeat("a", 64) || binding.Generation != 9 {
		panic("wrong media transport")
	}
	return protocol.Status{State: protocol.StateActive}, nil
}

func TestMediaStreamHTTPExplicitRouteAndAdmission(t *testing.T) {
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	defer manager.Close()
	grant := claimKit(t, manager)
	controller := &mediaController{}
	handler := httpapi.New(&fakeController{}, "bearer", "test", nil, httpapi.WithDevelopment(controller), httpapi.WithKitLease(manager))
	for _, tc := range []struct {
		name, path, bearer, lease string
		size, want                int
	}{
		{"stream", "/v1/development/media-stream", "bearer", grant.Token, 32768, 200},
		{"oversize", "/v1/development/media-stream", "bearer", grant.Token, 32769, 400},
		{"legacy unchanged", "/v1/development/media", "bearer", grant.Token, 32768, 400},
		{"auth", "/v1/development/media-stream", "wrong", grant.Token, 1, 401},
		{"lease", "/v1/development/media-stream", "bearer", "", 1, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := controller.calls
			r := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(strings.Repeat("x", tc.size)))
			r.Header.Set("Authorization", "Bearer "+tc.bearer)
			r.Header.Set(httpapi.KitLeaseHeader, tc.lease)
			r.Header.Set("Content-Type", "application/octet-stream")
			(protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9}).SetHeaders(r.Header)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			if tc.want == 200 {
				if controller.calls != before+1 || !controller.stream {
					t.Fatal("stream selection lost")
				}
			} else if controller.calls != before {
				t.Fatal("rejected request dispatched")
			}
		})
	}
}

func TestDevelopmentMediaHTTPAdmissionAndLease(t *testing.T) {
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	defer manager.Close()
	grant := claimKit(t, manager)
	controller := &mediaController{}
	handler := httpapi.New(&fakeController{}, "bearer", "test", nil, httpapi.WithDevelopment(controller), httpapi.WithKitLease(manager))
	for _, tc := range []struct {
		name, token, id, gen, contentType string
		size                              int
		want                              int
	}{
		{"accepted", grant.Token, strings.Repeat("a", 64), "9", "application/octet-stream", 16384, 200},
		{"missing lease", "", strings.Repeat("a", 64), "9", "application/octet-stream", 1, 403},
		{"foreign lease", "foreign", strings.Repeat("a", 64), "9", "application/octet-stream", 1, 403},
		{"empty", grant.Token, strings.Repeat("a", 64), "9", "application/octet-stream", 0, 400},
		{"oversized", grant.Token, strings.Repeat("a", 64), "9", "application/octet-stream", 16385, 400},
		{"missing package", grant.Token, "", "9", "application/octet-stream", 1, 400},
		{"zero generation", grant.Token, strings.Repeat("a", 64), "0", "application/octet-stream", 1, 400},
		{"wrong type", grant.Token, strings.Repeat("a", 64), "9", "application/json", 1, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := controller.calls
			r := httptest.NewRequest(http.MethodPost, "/v1/development/media", strings.NewReader(strings.Repeat("x", tc.size)))
			r.Header.Set("Authorization", "Bearer bearer")
			r.Header.Set(httpapi.KitLeaseHeader, tc.token)
			r.Header.Set("Content-Type", tc.contentType)
			r.Header.Set("X-FogCast-Package-ID", tc.id)
			r.Header.Set("X-FogCast-Core-Generation", tc.gen)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.want || (tc.want != 200 && controller.calls != before) {
				t.Fatalf("status=%d body=%s calls=%d", w.Code, w.Body, controller.calls)
			}
		})
	}
}

func TestDevelopmentMediaRequiresLeaseEvenWithoutController(t *testing.T) {
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	defer manager.Close()
	handler := httpapi.New(&fakeController{}, "bearer", "test", nil, httpapi.WithKitLease(manager))
	r := httptest.NewRequest(http.MethodPost, "/v1/development/media", nil)
	r.Header.Set("Authorization", "Bearer bearer")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("unguarded media route: %d", w.Code)
	}
}
