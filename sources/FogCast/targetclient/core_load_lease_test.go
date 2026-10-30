package targetclient_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

func TestCoreLoadRejectsActiveReplyAfterLeaseLoss(t *testing.T) {
	for _, route := range []string{"development", "library", "parts"} {
		for _, lose := range []bool{false, true} {
			name := route + "/held"
			if lose {
				name = route + "/lost"
			}
			t.Run(name, func(t *testing.T) {
				status := libraryPartsStatus()
				if route != "parts" {
					status.CorePackage.PartsComposition = nil
				}
				var claims, loads atomic.Int32
				var client *targetclient.Client
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/v1/kit/claim":
						claims.Add(1)
						io.WriteString(w, `{"status":{"state":"held","generation":"one","expires_in_ms":60000},"token":"owned"}`)
					case "/v1/development/core", "/v1/library/core/load", "/v1/library/core/parts":
						loads.Add(1)
						if r.Header.Get(targetclient.KitLeaseHeader) != "owned" {
							t.Error("load did not use its grant")
						}
						if lose {
							client.InvalidateKitSession()
						}
						json.NewEncoder(w).Encode(status)
					default:
						t.Errorf("unexpected mutation or replay: %s", r.URL.Path)
						http.Error(w, "unexpected", http.StatusInternalServerError)
					}
				}))
				defer server.Close()
				base, _ := url.Parse(server.URL)
				lease := targetclient.NewKitLease(base, "secret", server.Client(), "host", "launch")
				client = targetclient.NewClient(base, "secret", server.Client()).WithKitLease(lease)
				defer client.InvalidateKitSession()
				var result protocol.Status
				var err error
				switch route {
				case "development":
					result, err = client.LoadCore(context.Background(), 1, strings.NewReader("x"))
				case "library":
					result, err = client.LoadLibraryCore(context.Background(), 1, strings.NewReader("x"), status.CorePackage.PackageID)
				case "parts":
					result, err = client.LoadLibraryPartsCore(context.Background(), 1, strings.NewReader("x"), status.CorePackage.PackageID)
				}
				if lose {
					var api *protocol.APIError
					if !errors.Is(err, targetclient.ErrKitLeaseLost) || !errors.As(err, &api) || api.Phase != "recovery" || result.State == protocol.StateActive || result.CorePackage != nil {
						t.Fatalf("late active reply admitted: result=%+v err=%v", result, err)
					}
					if lease.Held() {
						t.Fatal("lost grant was reacquired")
					}
				} else if err != nil || result.State != protocol.StateActive {
					t.Fatalf("held grant rejected: %+v %v", result, err)
				}
				if claims.Load() != 1 || loads.Load() != 1 {
					t.Fatalf("claims=%d loads=%d", claims.Load(), loads.Load())
				}
			})
		}
	}
}
