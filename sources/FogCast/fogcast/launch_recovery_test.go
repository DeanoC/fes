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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

func TestLibraryLaunchRecoversRetainedLegacyFailure(t *testing.T) {
	for _, mode := range []string{"recover", "cleanup-fails", "cleanup-save-error", "foreign-owner", "unsafe-save", "unsafe-recovery", "unsafe-package", "unsafe-game", "unsafe-development", "unsafe-phase", "cancel", "unclean-stop", "reboot-response", "invalid-package", "invalid-media", "incompatible-package"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
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
			client := &packageLibraryClient{fakeServiceClient: &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateIdle}}}
			s := newService(Config{RequestTimeout: time.Second, UploadTimeout: time.Second}, Paths{}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, client)
			s.corePackages = packages
			raw := libraryPackageFixture(t, "0.1.0")
			pkg, _, err := s.ImportCorePackage(ctx, int64(len(raw)), bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			client.inspection = protocol.CoreInspection{PackageID: pkg.PackageID, Descriptor: pkg.Descriptor, Compatible: true}
			entry, err := s.CreateCoreEntry(ctx, "Valid package Pong", pkg.PackageID)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.EnsureBuiltinPong(ctx); err != nil {
				t.Fatal(err)
			}
			client.nativeLaunch = func(context.Context, protocol.LaunchRequest) (protocol.Status, error) {
				client.statusResult = protocol.Status{State: protocol.StateIdle, LastError: &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "target runtime is unavailable"}}
				return client.statusResult, client.statusResult.LastError
			}
			if _, err := s.Launch(ctx, catalog.BuiltinPongID, nil); err == nil || client.nativeLaunchCalls != 1 {
				t.Fatalf("legacy launch err=%v calls=%d", err, client.nativeLaunchCalls)
			}
			if mode == "invalid-package" {
				s.corePackages, err = corepackage.NewStore(filepath.Join(root, "empty-packages"))
				if err != nil {
					t.Fatal(err)
				}
			}
			if mode == "invalid-media" {
				s.catalog = &launchInvalidMediaCatalog{Store: store}
			}
			if mode == "incompatible-package" {
				client.inspection = protocol.CoreInspection{PackageID: pkg.PackageID, Descriptor: pkg.Descriptor, Compatible: false, CompatibilityError: &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "unsupported target", Phase: "compatibility"}}
			}
			if mode == "unsafe-save" {
				client.statusResult.LastError.Code = protocol.CodeSaveFailed
			}
			if mode == "unsafe-recovery" {
				client.statusResult.Recovery = protocol.RecoveryRebootRequired
			}
			if mode == "unsafe-package" {
				client.statusResult.CorePackage = &protocol.CorePackageStatus{PackageID: pkg.PackageID}
			}
			if mode == "unsafe-game" {
				game := "prior-game"
				client.statusResult.GameID = &game
			}
			if mode == "unsafe-development" {
				client.statusResult.Development = true
			}
			if mode == "unsafe-phase" {
				client.statusResult.LastError.Phase = "save"
			}
			request, cancel := context.WithCancel(ctx)
			defer cancel()
			client.stopFn = func(c context.Context) (protocol.Status, error) {
				if mode == "cleanup-save-error" {
					return protocol.Status{}, &protocol.APIError{Code: protocol.CodeSaveFailed, Phase: "save", Message: "private source path"}
				}
				if mode == "reboot-response" {
					return protocol.Status{State: protocol.StateStopping, Development: true, Recovery: protocol.RecoveryRebootRequired}, nil
				}
				if mode == "cleanup-fails" {
					return protocol.Status{}, errors.New("cleanup failed")
				}
				if mode == "foreign-owner" {
					return protocol.Status{}, &protocol.APIError{Code: protocol.CodeKitLeaseDenied}
				}
				if mode == "unclean-stop" {
					return client.statusResult, nil
				}
				if mode == "cancel" {
					cancel()
					return protocol.Status{State: protocol.StateIdle}, nil
				}
				client.statusResult = protocol.Status{State: protocol.StateIdle}
				return client.statusResult, nil
			}
			client.coreLoad = func(context.Context, int64, io.Reader) (protocol.Status, error) {
				if client.statusResult.LastError != nil {
					return protocol.Status{}, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Phase: "admission"}
				}
				core := "fes.pong"
				return protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core, CorePackage: &protocol.CorePackageStatus{PackageID: pkg.PackageID, Generation: 1, ABI: protocol.RuntimeContract{ID: pkg.Descriptor.ABI.ID, Major: 1}, BuildID: pkg.Descriptor.Build.ID, Gamepad: true}}, nil
			}
			result, err := s.Launch(request, entry.GameID, nil)
			if mode == "recover" {
				if err != nil || result.Status.GameID == nil || *result.Status.GameID != entry.GameID || client.stopCalls != 1 || client.coreCalls != 1 {
					t.Fatalf("result=%+v err=%v stop=%d load=%d", result, err, client.stopCalls, client.coreCalls)
				}
				if _, err = s.Stop(ctx); err != nil {
					t.Fatal(err)
				}
				if _, err = s.Launch(ctx, entry.GameID, nil); err != nil || client.coreCalls != 2 || client.stopCalls != 2 {
					t.Fatalf("explicit stop/relaunch: err=%v stops=%d loads=%d", err, client.stopCalls, client.coreCalls)
				}
			} else {
				wantStops := 1
				if strings.HasPrefix(mode, "unsafe-") || mode == "invalid-package" || mode == "invalid-media" || mode == "incompatible-package" {
					wantStops = 0
				}
				if err == nil || client.coreCalls != 0 || client.stopCalls != wantStops {
					t.Fatalf("err=%v stop=%d load=%d", err, client.stopCalls, client.coreCalls)
				}
				if mode == "cancel" && !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation lost: %v", err)
				}
				if mode == "cleanup-save-error" {
					var api *protocol.APIError
					if !errors.As(err, &api) || api.Code != protocol.CodeSaveFailed || api.Phase != "save" || strings.Contains(err.Error(), "private") {
						t.Fatalf("cleanup diagnostics lost/unsafe: %v", err)
					}
				}
				if mode == "incompatible-package" {
					var api *protocol.APIError
					if !errors.As(err, &api) || api.Phase != "compatibility" || client.stopCalls != 0 {
						t.Fatalf("incompatible package was recovered: err=%v stop=%d", err, client.stopCalls)
					}
				}
			}
			if client.developmentReboots != 0 {
				t.Fatal("automatic recovery rebooted")
			}
		})
	}
}

type launchInvalidMediaCatalog struct{ *catalog.Store }

func (s *launchInvalidMediaCatalog) CoreEntry(ctx context.Context, id string) (catalog.CoreEntry, error) {
	entry, err := s.Store.CoreEntry(ctx, id)
	entry.MediaRole = "invalid-role"
	entry.MediaID = strings.Repeat("a", 64)
	return entry, err
}

func TestLibraryRecoveryAdmissionRejectsUntrustedPeer(t *testing.T) {
	const id = "f2bb8d43-3cf5-4407-9a11-dfb7cb0086aa"
	for _, mode := range []string{"wrong-identity", "wrong-api", "foreign-owner"} {
		t.Run(mode, func(t *testing.T) {
			var mutations, ownerships atomic.Int32
			peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					mutations.Add(1)
					t.Errorf("unexpected mutation %s", r.URL.Path)
					http.Error(w, "unexpected", 500)
					return
				}
				switch r.URL.Path {
				case "/v1/health":
					peerID, version := id, "v1"
					if mode == "wrong-identity" {
						peerID = "wrong"
					}
					if mode == "wrong-api" {
						version = "v2"
					}
					json.NewEncoder(w).Encode(protocol.Health{APIVersion: version, TargetID: peerID, Ready: true})
				case "/v1/kit/lease":
					ownerships.Add(1)
					json.NewEncoder(w).Encode(targetclient.KitOwnership{State: "held", Generation: "foreign-generation"})
				case "/v1/status":
					json.NewEncoder(w).Encode(protocol.Status{State: protocol.StateIdle, LastError: &protocol.APIError{Code: protocol.CodeMiSTerUnavailable}})
				default:
					t.Errorf("unexpected read %s", r.URL.Path)
					http.Error(w, "unexpected", 500)
				}
			}))
			defer peer.Close()
			base, _ := url.Parse(peer.URL)
			lease := targetclient.NewKitLease(base, "secret", peer.Client(), "host", "launch")
			client := targetclient.NewClient(base, "secret", peer.Client()).WithKitLease(lease)
			s := newService(Config{Targets: []TargetConfig{{Name: "kit", Enabled: true, Address: peer.URL, Agent: "secret", TargetID: id}}, SelectedTarget: "kit", RequestTimeout: time.Second, UploadTimeout: time.Second}, Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, client)
			s.resolveTarget = func(context.Context, string) ([]string, error) { return nil, nil }
			raw := libraryPackageFixture(t, "0.1.0")
			_, err := s.loadCore(context.Background(), func(context.Context) (coreLoadSource, error) {
				return coreLoadSource{size: int64(len(raw)), body: bytes.NewReader(raw), entry: &catalog.CoreEntry{PackageID: strings.Repeat("a", 64)}}, nil
			})
			if err == nil || mutations.Load() != 0 {
				t.Fatalf("err=%v mutations=%d", err, mutations.Load())
			}
			if mode == "foreign-owner" && ownerships.Load() != 1 {
				t.Fatal("ownership was not observed")
			}
		})
	}
}
