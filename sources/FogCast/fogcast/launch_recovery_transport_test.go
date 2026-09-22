package fogcast

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

func TestLibraryRecoveryLeasedTransport(t *testing.T) {
	for _, loseLease := range []bool{false, true} {
		name := "retained-lease"
		if loseLease {
			name = "lease-lost-after-stop"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			root := t.TempDir()
			store, err := catalog.OpenContext(ctx, filepath.Join(root, "catalog.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			packages, err := corepackage.NewStore(filepath.Join(root, "packages"))
			if err != nil {
				t.Fatal(err)
			}
			raw := libraryPackageFixture(t, "0.1.0")
			pkg, _, err := packages.Import(ctx, int64(len(raw)), bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			entry, err := store.CreateCoreEntry(ctx, "Valid package Pong", pkg.Descriptor.Core.ID, pkg.PackageID)
			if err != nil {
				t.Fatal(err)
			}

			const targetID = "f2bb8d43-3cf5-4407-9a11-dfb7cb0086aa"
			const token = "retained-test-lease"
			var mu sync.Mutex
			var mutations []string
			state := protocol.Status{State: protocol.StateIdle, LastError: &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "retained idle failure"}}
			held := false
			var client *targetclient.Client
			peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.Header.Get("Authorization") != "Bearer secret" {
					t.Error("missing bearer authentication")
				}
				readOnlyInspection := r.Method == http.MethodPost && r.URL.Path == "/v1/development/core/inspect"
				if r.Method == http.MethodPost && !readOnlyInspection {
					mutations = append(mutations, r.URL.Path)
				}
				if r.Method == http.MethodPost && !readOnlyInspection && r.URL.Path != "/v1/kit/claim" && r.Header.Get("X-FogCast-Kit-Lease") != token {
					t.Errorf("%s did not reuse the original lease", r.URL.Path)
				}
				switch r.Method + " " + r.URL.Path {
				case "GET /v1/health":
					json.NewEncoder(w).Encode(protocol.Health{APIVersion: "v1", TargetID: targetID, Ready: true})
				case "GET /v1/kit/lease":
					ownership := targetclient.KitOwnership{State: "free"}
					if held {
						ownership.State, ownership.Generation = "held", "original-generation"
					}
					json.NewEncoder(w).Encode(ownership)
				case "GET /v1/status":
					json.NewEncoder(w).Encode(state)
				case "POST /v1/kit/claim":
					held = true
					json.NewEncoder(w).Encode(map[string]any{"status": map[string]any{"state": "held", "generation": "original-generation", "expires_in_ms": 60000}, "token": token})
				case "POST /v1/development/core/inspect":
					body, err := io.ReadAll(r.Body)
					if err != nil || !bytes.Equal(body, raw) {
						t.Errorf("inspection did not transfer selected immutable package: %v", err)
					}
					json.NewEncoder(w).Encode(protocol.CoreInspection{PackageID: pkg.PackageID, Descriptor: pkg.Descriptor, Compatible: true})
				case "POST /v1/stop":
					state = protocol.Status{State: protocol.StateIdle}
					// Deterministically invalidate authority after cleanup but before the
					// service receives idle and attempts activation; no timer races.
					if loseLease {
						client.InvalidateKitSession()
					}
					json.NewEncoder(w).Encode(state)
				case "POST /v1/library/core/load":
					body, err := io.ReadAll(r.Body)
					if err != nil || !bytes.Equal(body, raw) || r.Header.Get("X-FogCast-Package-ID") != pkg.PackageID {
						t.Errorf("activation did not transfer selected immutable package: %v", err)
					}
					core := pkg.Descriptor.Core.ID
					state = protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core,
						CorePackage: &protocol.CorePackageStatus{PackageID: pkg.PackageID, Generation: 1,
							ABI: protocol.RuntimeContract{ID: pkg.Descriptor.ABI.ID, Major: 1}, BuildID: pkg.Descriptor.Build.ID,
							PersistenceMode: "volatile"}}
					json.NewEncoder(w).Encode(state)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					http.Error(w, "unexpected", http.StatusInternalServerError)
				}
			}))
			defer peer.Close()
			base, _ := url.Parse(peer.URL)
			lease := targetclient.NewKitLease(base, "secret", peer.Client(), "host", "launch")
			client = targetclient.NewClient(base, "secret", peer.Client()).WithKitLease(lease)
			defer client.InvalidateKitSession() // cancel renewal without another remote mutation
			s := newService(Config{Targets: []TargetConfig{{Name: "kit", Enabled: true, Address: peer.URL, Agent: "secret", TargetID: targetID}}, SelectedTarget: "kit", RequestTimeout: time.Second, UploadTimeout: time.Second}, Paths{}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, client)
			s.resolveTarget = func(context.Context, string) ([]string, error) { return nil, nil }
			s.corePackages = packages
			claim, _ := http.NewRequestWithContext(ctx, http.MethodPost, peer.URL+"/v1/stop", nil)
			if err := lease.Authorize(claim, true); err != nil {
				t.Fatal(err)
			}
			result, err := s.Launch(ctx, entry.GameID, nil)
			if loseLease {
				if err == nil || result.Status.State == protocol.StateActive || lease.Held() {
					t.Fatalf("lost lease admitted activation: result=%+v err=%v", result, err)
				}
			} else if err != nil || result.Status.GameID == nil || *result.Status.GameID != entry.GameID || result.Status.State != protocol.StateActive {
				t.Fatalf("recovered launch: result=%+v err=%v", result, err)
			}
			want := []string{"/v1/kit/claim", "/v1/stop"}
			if !loseLease {
				want = append(want, "/v1/library/core/load")
			}
			mu.Lock()
			defer mu.Unlock()
			if !reflect.DeepEqual(mutations, want) {
				t.Fatalf("mutations=%v want=%v", mutations, want)
			}
		})
	}
}
