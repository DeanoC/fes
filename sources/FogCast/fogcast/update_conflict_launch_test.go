package fogcast

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

// TestUpdateConflictLaunchReleasesOnlyANewLease drives a kit core-package
// launch through the service. The target grants the claim and then refuses
// activation with UPDATE_CONFLICT. A newly acquired lease is released. A
// lease that already backs another live play on that kit is kept.
func TestUpdateConflictLaunchReleasesOnlyANewLease(t *testing.T) {
	for _, livePlay := range []bool{false, true} {
		name := "release new lease"
		if livePlay {
			name = "keep lease backing a live play"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
			const token = "update-conflict-lease"
			idle, err := json.Marshal(protocol.Status{State: protocol.StateIdle})
			if err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			var calls []string
			peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Body != nil {
					_, _ = io.Copy(io.Discard, r.Body)
				}
				mu.Lock()
				calls = append(calls, r.Method+" "+r.URL.Path)
				mu.Unlock()
				if r.Header.Get("Authorization") != "Bearer secret" {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
				switch r.Method + " " + r.URL.Path {
				case "GET /v1/health":
					_ = json.NewEncoder(w).Encode(protocol.Health{APIVersion: "v1", TargetID: targetID, Ready: true})
				case "GET /v1/kit/lease":
					_ = json.NewEncoder(w).Encode(targetclient.KitOwnership{State: "free"})
				case "GET /v1/status":
					w.Write(idle)
				case "POST /v1/kit/claim":
					_ = json.NewEncoder(w).Encode(map[string]any{
						"status": map[string]any{"state": "held", "generation": "original-generation", "expires_in_ms": 60000},
						"token":  token,
					})
				case "POST /v1/library/core/load":
					if r.Header.Get("X-FogCast-Kit-Lease") != token {
						t.Errorf("load missing claimed lease")
					}
					w.WriteHeader(http.StatusInternalServerError)
					_ = json.NewEncoder(w).Encode(protocol.ErrorEnvelope{Error: protocol.APIError{
						Code: "UPDATE_CONFLICT", Message: "revision changed",
					}})
				case "POST /v1/kit/release":
					if r.Header.Get("X-FogCast-Kit-Lease") != token {
						t.Errorf("release missing claimed lease")
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"state": "free"})
				default:
					http.Error(w, "unexpected", http.StatusInternalServerError)
				}
			}))
			defer peer.Close()
			base, err := url.Parse(peer.URL)
			if err != nil {
				t.Fatal(err)
			}
			lease := targetclient.NewKitLease(base, "secret", peer.Client(), "host", "launch")
			client := targetclient.NewClient(base, "secret", peer.Client()).WithKitLease(lease)
			defer client.InvalidateKitSession()
			s := newService(Config{
				Targets:        []TargetConfig{{Name: "kit", Enabled: true, Address: peer.URL, Agent: "secret", TargetID: targetID}},
				SelectedTarget: "kit",
				RequestTimeout: time.Second,
				UploadTimeout:  time.Second,
			}, Paths{}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, client)
			s.resolveTarget = func(context.Context, string) ([]string, error) { return nil, nil }
			s.corePackages = packages
			if livePlay {
				s.plays["kit"] = targetPlay{
					execution: ExecutionFPGANative, gameID: "kept-play", system: catalog.CorePlatform,
					packageID: pkg.PackageID, packageGeneration: 1,
				}
			}

			_, err = s.Launch(ctx, entry.GameID, nil)
			mu.Lock()
			seen := append([]string(nil), calls...)
			mu.Unlock()
			var api *protocol.APIError
			if !errors.As(err, &api) || api.Code != "UPDATE_CONFLICT" {
				t.Fatalf("launch error = %#v calls=%v", err, seen)
			}
			releases := 0
			for _, path := range seen {
				if path == "POST /v1/kit/release" {
					releases++
				}
			}
			if livePlay {
				if releases != 0 || !lease.Held() {
					t.Fatalf("live play lost its lease: releases=%d held=%t posts=%v", releases, lease.Held(), seen)
				}
				return
			}
			if releases != 1 || lease.Held() {
				t.Fatalf("new lease was not released: releases=%d held=%t posts=%v err=%v", releases, lease.Held(), seen, err)
			}
		})
	}
}
