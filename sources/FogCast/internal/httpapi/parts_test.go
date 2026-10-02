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
	loads, inspections, libraryLoads int
	packageID                        string
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

func (c *partsController) LoadLibraryPartsCore(_ context.Context, _ int64, _ io.Reader, id string) (protocol.Status, *protocol.APIError) {
	c.libraryLoads++
	c.packageID = id
	return protocol.Status{State: protocol.StateActive, Development: true}, nil
}

func TestLibraryPartsRouteRequiresExactContextAuthAndLease(t *testing.T) {
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	defer manager.Close()
	grant := claimKit(t, manager)
	c := &partsController{}
	handler := httpapi.New(&fakeController{}, "bearer", "test", nil, httpapi.WithDevelopment(c), httpapi.WithKitLease(manager))
	id := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name, auth, lease, path string
		ids                     []string
		code                    int
	}{
		{"auth", "", grant.Token, "/v1/library/core/parts", []string{id}, 401},
		{"lease", "bearer", "", "/v1/library/core/parts", []string{id}, 403},
		{"missing identity", "bearer", grant.Token, "/v1/library/core/parts", nil, 400},
		{"duplicate identity", "bearer", grant.Token, "/v1/library/core/parts", []string{id, id}, 400},
		{"uppercase identity", "bearer", grant.Token, "/v1/library/core/parts", []string{strings.Repeat("A", 64)}, 400},
		{"query", "bearer", grant.Token, "/v1/library/core/parts?data_root=/other", []string{id}, 400},
		{"library", "bearer", grant.Token, "/v1/library/core/parts", []string{id}, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader("parts"))
			req.Header.Set("Authorization", "Bearer "+tc.auth)
			req.Header.Set("Content-Type", "application/octet-stream")
			req.Header.Set(httpapi.KitLeaseHeader, tc.lease)
			for _, id := range tc.ids {
				req.Header.Add("X-FogCast-Package-ID", id)
			}
			out := httptest.NewRecorder()
			handler.ServeHTTP(out, req)
			if out.Code != tc.code {
				t.Fatalf("code=%d body=%s", out.Code, out.Body)
			}
		})
	}
	if c.libraryLoads != 1 || c.loads != 0 || c.packageID != id {
		t.Fatalf("library=%d developer=%d id=%s", c.libraryLoads, c.loads, c.packageID)
	}
}
