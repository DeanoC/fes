package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/protocol"
)

type displayHTTPController struct {
	fakeDevelopmentController
	calls   int
	visible bool
}

func (c *displayHTTPController) SetSessionDisplay(_ context.Context, visible bool, b protocol.DevelopmentMediaBinding) (protocol.Status, *protocol.APIError) {
	c.calls++
	c.visible = visible
	return protocol.Status{State: protocol.StateActive, Development: true}, nil
}

func TestSessionDisplayRequiresLeaseAndExplicitBoolean(t *testing.T) {
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	defer manager.Close()
	grant := claimKit(t, manager)
	c := &displayHTTPController{}
	handler := httpapi.New(&fakeController{}, "bearer", "test", nil, httpapi.WithDevelopment(c), httpapi.WithKitLease(manager))
	for _, tc := range []struct {
		body, lease string
		want        int
	}{
		{`{"visible":true}`, "", 403}, {`{}`, grant.Token, 400}, {`{"visible":null}`, grant.Token, 400},
		{`{"visible":true} {}`, grant.Token, 400}, {`{"visible":true,"reset":true}`, grant.Token, 400},
		{`{"visible":true}`, grant.Token, 200}, {`{"visible":false}`, grant.Token, 200},
	} {
		r := httptest.NewRequest(http.MethodPost, "/v1/session/display", strings.NewReader(tc.body))
		r.Header.Set("Authorization", "Bearer bearer")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set(httpapi.KitLeaseHeader, tc.lease)
		(protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9}).SetHeaders(r.Header)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("body=%s status=%d response=%s", tc.body, w.Code, w.Body)
		}
	}
	if c.calls != 2 || c.visible {
		t.Fatalf("calls=%d visible=%v", c.calls, c.visible)
	}
}
