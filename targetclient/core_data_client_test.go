package targetclient_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

func TestCoreDataClientBindsPackageAndDoesNotReplayMutation(t *testing.T) {
	descriptor := validCoreInspectionDescriptor()
	descriptor.Interfaces = append(descriptor.Interfaces, corepackage.Interface{ID: "fes.pong.progress", Major: 1, Required: true})
	var err error
	id := strings.Repeat("a", 64)
	for _, kind := range []string{"read", "write", "wrong identity", "lost reply"} {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				b, _ := io.ReadAll(r.Body)
				if string(b) != "package" || r.Header.Get("Authorization") != "Bearer secret" || r.Header.Get("X-FogCast-Package-ID") != id || r.URL.RawQuery != "" {
					t.Errorf("request=%s headers=%v", r.URL, r.Header)
				}
				if kind == "lost reply" {
					conn, _, _ := w.(http.Hijacker).Hijack()
					conn.Close()
					return
				}
				gotID := id
				if kind == "wrong identity" {
					gotID = strings.Repeat("b", 64)
				}
				json.NewEncoder(w).Encode(protocol.CoreDataInspection{CoreData: protocol.CoreData{PackageID: gotID, CoreID: "fes.pong", Mode: "persistent", Layout: &protocol.RuntimeContract{ID: "fes.pong.progress", Major: 1}, Revision: strings.Repeat("c", 64), PaddleSpeed: 2}, Descriptor: descriptor})
			}))
			defer server.Close()
			base, _ := url.Parse(server.URL)
			client := targetclient.NewClient(base, "secret", server.Client())
			if kind == "write" || kind == "lost reply" {
				_, err = client.UpdateCoreSettings(context.Background(), 7, strings.NewReader("package"), protocol.CoreSettingsUpdate{ExpectedPackageID: id, ExpectedRevision: "absent", PaddleSpeed: 2})
			} else {
				_, err = client.InspectCoreData(context.Background(), 7, strings.NewReader("package"), id)
			}
			if (kind == "wrong identity" || kind == "lost reply") != (err != nil) {
				t.Fatalf("err=%v", err)
			}
			if calls.Load() != 1 {
				t.Fatalf("calls=%d", calls.Load())
			}
		})
	}
}

func TestSettingsWriteReleasesOnlyItsNewSuccessfulLease(t *testing.T) {
	for _, kind := range []string{"new lease", "existing lease", "lost response", "cleanup failure", "stale revision"} {
		t.Run(kind, func(t *testing.T) {
			descriptor := validCoreInspectionDescriptor()
			descriptor.Interfaces = append(descriptor.Interfaces, corepackage.Interface{ID: "fes.pong.progress", Major: 1, Required: true})
			id := strings.Repeat("a", 64)
			var claims, writes, releases atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/kit/claim":
					claims.Add(1)
					fmt.Fprintf(w, `{"status":{"state":"held","generation":"one","expires_in_ms":60000},"token":"settings-token"}`)
				case "/v1/kit/release":
					releases.Add(1)
					if kind == "cleanup failure" {
						http.Error(w, `{"error":{"code":"INTERNAL","message":"cleanup failed"}}`, 500)
					} else {
						io.WriteString(w, `{"state":"free"}`)
					}
				case "/v1/library/core/settings":
					writes.Add(1)
					if kind == "stale revision" && writes.Load() == 1 {
						http.Error(w, `{"error":{"code":"STALE_REVISION","message":"stale","phase":"core_data"}}`, 409)
						return
					}
					if r.Header.Get(targetclient.KitLeaseHeader) != "settings-token" {
						t.Error("missing owner")
					}
					if kind == "lost response" {
						conn, _, _ := w.(http.Hijacker).Hijack()
						conn.Close()
						return
					}
					json.NewEncoder(w).Encode(protocol.CoreDataInspection{CoreData: protocol.CoreData{PackageID: id, CoreID: "fes.pong", Layout: &protocol.RuntimeContract{ID: "fes.pong.progress", Major: 1}, Mode: "persistent", Revision: strings.Repeat("d", 64), PaddleSpeed: 2, BestRally: 17}, Descriptor: descriptor})
				}
			}))
			defer server.Close()
			base, _ := url.Parse(server.URL)
			lease := targetclient.NewKitLease(base, "bearer", server.Client(), "test", "settings")
			defer lease.Close(context.Background())
			client := targetclient.NewClient(base, "bearer", server.Client()).WithKitLease(lease)
			if kind == "existing lease" {
				req, _ := http.NewRequest("POST", server.URL+"/v1/launch", nil)
				if err := lease.Authorize(req, true); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, err := client.UpdateCoreSettings(ctx, 7, strings.NewReader("package"), protocol.CoreSettingsUpdate{ExpectedPackageID: id, ExpectedRevision: "absent", PaddleSpeed: 2})
			wantRelease := 0
			if kind == "new lease" || kind == "cleanup failure" || kind == "stale revision" {
				wantRelease = 1
			}
			if claims.Load() != 1 || writes.Load() != 1 || releases.Load() != int32(wantRelease) {
				t.Fatalf("claims=%d writes=%d releases=%d", claims.Load(), writes.Load(), releases.Load())
			}
			if (kind == "lost response" || kind == "cleanup failure" || kind == "stale revision") != (err != nil) {
				t.Fatalf("err=%v", err)
			}
			if kind == "stale revision" {
				_, err = client.UpdateCoreSettings(ctx, 7, strings.NewReader("package"), protocol.CoreSettingsUpdate{ExpectedPackageID: id, ExpectedRevision: "absent", PaddleSpeed: 2})
				if err != nil || claims.Load() != 2 || writes.Load() != 2 || releases.Load() != 2 {
					t.Fatalf("corrected write err=%v claims=%d writes=%d releases=%d", err, claims.Load(), writes.Load(), releases.Load())
				}
			}

		})
	}
}

func TestCoreDataClientRejectsMissingDurableFields(t *testing.T) {
	descriptor := validCoreInspectionDescriptor()
	descriptor.Interfaces = append(descriptor.Interfaces, corepackage.Interface{ID: "fes.pong.progress", Major: 1, Required: true})
	data := protocol.CoreDataInspection{CoreData: protocol.CoreData{PackageID: strings.Repeat("a", 64), CoreID: "fes.pong", Layout: &protocol.RuntimeContract{ID: "fes.pong.progress", Major: 1}, Mode: "persistent", Revision: "absent", PaddleSpeed: 1}, Descriptor: descriptor}
	raw, _ := json.Marshal(data)
	for _, pair := range [][2]string{{`,"best_rally":0`, ""}, {`"paddle_speed":1`, `"paddle_speed":null`}, {`"layout":{"id":"fes.pong.progress","major":1,"minor":0}`, `"layout":{"id":"fes.pong.progress","major":1}`}} {
		t.Run(pair[0], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.WriteString(w, strings.Replace(string(raw), pair[0], pair[1], 1))
			}))
			defer server.Close()
			base, _ := url.Parse(server.URL)
			_, err := targetclient.NewClient(base, "secret", server.Client()).InspectCoreData(context.Background(), 7, strings.NewReader("package"), data.PackageID)
			if err == nil {
				t.Fatal("accepted incomplete data")
			}
		})
	}
}
