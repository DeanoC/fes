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

type partsController struct {
	fakeDevelopmentController
	loads, inspections int
}

func (c *partsController) LoadPartsCore(context.Context, int64, io.Reader) (protocol.Status, *protocol.APIError) {
	c.loads++
	return protocol.Status{State: protocol.StateActive, Development: true}, nil
}
func (c *partsController) InspectPartsCore(context.Context, int64, io.Reader) (protocol.PartsInspection, *protocol.APIError) {
	c.inspections++
	return protocol.PartsInspection{PersistenceMode: "volatile"}, nil
}

func TestPartsRoutesAreDeveloperOnlyAndLeaseBound(t *testing.T) {
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	defer manager.Close()
	grant := claimKit(t, manager)
	c := &partsController{}
	handler := httpapi.New(&fakeController{}, "bearer", "test", nil, httpapi.WithDevelopment(c), httpapi.WithKitLease(manager))
	for _, tc := range []struct {
		path, lease, packageID string
		code                   int
	}{
		{"/v1/development/core/parts/inspect", "", "", 200},
		{"/v1/development/core/parts", "", "", 403},
		{"/v1/development/core/parts", grant.Token, "", 200},
		{"/v1/development/core/parts", grant.Token, strings.Repeat("a", 64), 400},
		{"/v1/development/core/parts/inspect?payload_path=/tmp/other", "", "", 400},
	} {
		req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader("parts"))
		req.Header.Set("Authorization", "Bearer bearer")
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set(httpapi.KitLeaseHeader, tc.lease)
		if tc.packageID != "" {
			req.Header.Set("X-FogCast-Package-ID", tc.packageID)
		}
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		if out.Code != tc.code {
			t.Fatalf("%s: %d %s", tc.path, out.Code, out.Body)
		}
	}
	if c.loads != 1 || c.inspections != 1 {
		t.Fatalf("dispatches: load=%d inspect=%d", c.loads, c.inspections)
	}
}
