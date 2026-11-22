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
	display protocol.SessionDisplay
}

func (c *displayHTTPController) SetSessionDisplay(_ context.Context, display protocol.SessionDisplay, b protocol.DevelopmentMediaBinding) (protocol.Status, *protocol.APIError) {
	c.calls++
	c.display = display
	return protocol.Status{State: protocol.StateActive, Development: true}, nil
}

func TestSessionDisplayRequiresLeaseExplicitBooleanAndFocus(t *testing.T) {
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	defer manager.Close()
	grant := claimKit(t, manager)
	c := &displayHTTPController{}
	handler := httpapi.New(&fakeController{}, "bearer", "test", nil, httpapi.WithDevelopment(c), httpapi.WithKitLease(manager))
	for _, tc := range []struct {
		body, lease string
		want        int
	}{
		{`{"visible":true,"focus":"launcher"}`, "", 403}, {`{}`, grant.Token, 400}, {`{"visible":null,"focus":"machine"}`, grant.Token, 400},
		{`{"visible":true,"focus":"launcher"} {}`, grant.Token, 400}, {`{"visible":true,"focus":"launcher","reset":true}`, grant.Token, 400},
		{`{"visible":true,"focus":"menu"}`, grant.Token, 400}, {`{"visible":false,"focus":"launcher"}`, grant.Token, 400},
		// Only an omitted focus takes the default.
		{`{"visible":true,"focus":null}`, grant.Token, 400}, {`{"visible":true,"focus":""}`, grant.Token, 400},
		{`{"visible":false,"focus":null}`, grant.Token, 400}, {`{"visible":false,"focus":""}`, grant.Token, 400},
		{`{"visible":true,"focus":"launcher"}`, grant.Token, 200}, {`{"visible":true,"focus":"machine"}`, grant.Token, 200},
		// A host that predates focus sends visibility alone.
		{`{"visible":true}`, grant.Token, 200}, {`{"visible":false,"focus":"machine"}`, grant.Token, 200},
		{`{"visible":false}`, grant.Token, 200},
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
	if c.calls != 5 || c.display != protocol.SessionDisplayClosed {
		t.Fatalf("calls=%d display=%+v", c.calls, c.display)
	}
}
