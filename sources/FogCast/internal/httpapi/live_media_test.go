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

type liveMediaHTTPController struct {
	fakeDevelopmentController
	replaceCalls int
	clearCalls   int
	err          *protocol.APIError
}

func (c *liveMediaHTTPController) ReplaceLiveMedia(_ context.Context, size int64, body io.Reader, b protocol.DevelopmentMediaBinding) (protocol.Status, *protocol.APIError) {
	c.replaceCalls++
	data, err := io.ReadAll(body)
	if err != nil || int64(len(data)) != size {
		panic("wrong live media transport")
	}
	if c.err != nil {
		return c.status, c.err
	}
	return protocol.Status{State: protocol.StateActive, Development: true}, nil
}

func (c *liveMediaHTTPController) ClearLiveMedia(_ context.Context, b protocol.DevelopmentMediaBinding) (protocol.Status, *protocol.APIError) {
	c.clearCalls++
	if c.err != nil {
		return c.status, c.err
	}
	return protocol.Status{State: protocol.StateActive, Development: true}, nil
}

func TestLiveMediaAgentHTTPAdmissionAndLease(t *testing.T) {
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	defer manager.Close()
	grant := claimKit(t, manager)
	controller := &liveMediaHTTPController{}
	handler := httpapi.New(&fakeController{}, "bearer", "test", nil, httpapi.WithDevelopment(controller), httpapi.WithKitLease(manager))

	r := httptest.NewRequest(http.MethodPost, "/v1/development/live-media", strings.NewReader("raw"))
	r.Header.Set("Authorization", "Bearer bearer")
	r.Header.Set(httpapi.KitLeaseHeader, grant.Token)
	r.Header.Set("Content-Type", "application/octet-stream")
	(protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9}).SetHeaders(r.Header)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 || controller.replaceCalls != 1 {
		t.Fatalf("status=%d body=%s calls=%d", w.Code, w.Body, controller.replaceCalls)
	}

	clear := httptest.NewRequest(http.MethodPost, "/v1/development/clear-media", nil)
	clear.Header.Set("Authorization", "Bearer bearer")
	clear.Header.Set(httpapi.KitLeaseHeader, grant.Token)
	(protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9}).SetHeaders(clear.Header)
	cw := httptest.NewRecorder()
	handler.ServeHTTP(cw, clear)
	if cw.Code != 200 || controller.clearCalls != 1 {
		t.Fatalf("clear status=%d body=%s calls=%d", cw.Code, cw.Body, controller.clearCalls)
	}

	oversized := httptest.NewRequest(http.MethodPost, "/v1/development/live-media", strings.NewReader(strings.Repeat("x", 16385)))
	oversized.Header.Set("Authorization", "Bearer bearer")
	oversized.Header.Set(httpapi.KitLeaseHeader, grant.Token)
	oversized.Header.Set("Content-Type", "application/octet-stream")
	(protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9}).SetHeaders(oversized.Header)
	ow := httptest.NewRecorder()
	handler.ServeHTTP(ow, oversized)
	if ow.Code != 400 || controller.replaceCalls != 1 {
		t.Fatalf("oversized status=%d calls=%d", ow.Code, controller.replaceCalls)
	}

	controller.err = protocol.LiveMediaBusyError()
	busy := httptest.NewRequest(http.MethodPost, "/v1/development/live-media", strings.NewReader("x"))
	busy.Header.Set("Authorization", "Bearer bearer")
	busy.Header.Set(httpapi.KitLeaseHeader, grant.Token)
	busy.Header.Set("Content-Type", "application/octet-stream")
	(protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9}).SetHeaders(busy.Header)
	bw := httptest.NewRecorder()
	handler.ServeHTTP(bw, busy)
	if bw.Code != 409 || controller.replaceCalls != 2 {
		t.Fatalf("busy status=%d calls=%d body=%s", bw.Code, controller.replaceCalls, bw.Body)
	}
}
