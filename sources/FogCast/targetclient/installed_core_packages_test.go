package targetclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

func installedClientFixture(t *testing.T) ([]byte, corepackage.Inspection) {
	t.Helper()
	root := t.TempDir()
	fixtureRoot := filepath.Join("..", "corepackage", "testdata", "core-bundle-v2")
	for _, item := range []struct{ source, target string }{{"manifests/valid-basic.toml", "manifest.toml"}, {"payloads/fes-fixture.rbf", "core.rbf"}} {
		data, err := os.ReadFile(filepath.Join(fixtureRoot, item.source))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, item.target), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	inspection, err := corepackage.InspectPackage(root)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := corepackage.CanonicalArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	return archive, inspection
}

func TestInstalledCorePackageClientTransfersAndSelectsWithoutClaim(t *testing.T) {
	archive, value := installedClientFixture(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get(KitLeaseHeader) != "" {
			t.Errorf("request authority: %v", r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/core-packages":
			body, _ := io.ReadAll(r.Body)
			if r.ContentLength != int64(len(archive)) || !bytes.Equal(body, archive) || r.Header.Get("Content-Type") != "application/octet-stream" {
				t.Error("install did not transfer bounded package")
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(protocol.KitCoreInstall{Package: value, Created: true})
		case "PUT /v1/core-selections/fes.pong":
			var body protocol.KitCoreSelectionRequest
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.ExpectedPackageID != "" || body.PackageID != value.PackageID || r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("selection request = %+v", body)
			}
			_ = json.NewEncoder(w).Encode(protocol.KitCoreSelection{CoreID: value.Descriptor.Core.ID, PackageID: value.PackageID})
		case "GET /v1/core-packages":
			_ = json.NewEncoder(w).Encode(protocol.KitCorePackages{Packages: []corepackage.Inspection{value}, Selections: []protocol.KitCoreSelection{{CoreID: value.Descriptor.Core.ID, PackageID: value.PackageID}}})
		case "GET /v1/core-selections":
			_ = json.NewEncoder(w).Encode([]protocol.KitCoreSelection{{CoreID: value.Descriptor.Core.ID, PackageID: value.PackageID}})
		default:
			t.Errorf("unexpected path / authority claim: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	client := NewClient(base, "test-token", server.Client())
	client.kitLease = NewKitLease(base, "test-token", server.Client(), "test-owner", "test")
	// Closed execution authority must not block immutable installs or selections.
	client.kitLease.closed = true
	installed, err := client.InstallCorePackage(context.Background(), int64(len(archive)), bytes.NewReader(archive), value.PackageID)
	if err != nil || installed.Package.PackageID != value.PackageID || !installed.Created {
		t.Fatalf("install = %+v %v", installed, err)
	}
	selected, err := client.SelectCorePackage(context.Background(), value.Descriptor.Core.ID, "", value.PackageID)
	if err != nil || selected.PackageID != value.PackageID {
		t.Fatalf("select = %+v %v", selected, err)
	}
	inventory, err := client.InstalledCorePackages(context.Background())
	if err != nil || len(inventory.Packages) != 1 {
		t.Fatalf("inventory = %+v %v", inventory, err)
	}
	selections, err := client.CoreSelections(context.Background())
	if err != nil || len(selections) != 1 {
		t.Fatalf("selections = %+v %v", selections, err)
	}
	if requests.Load() != 4 {
		t.Fatalf("request count = %d", requests.Load())
	}
}

func TestInstalledCorePackageClientRejectsForeignAndMalformedReplies(t *testing.T) {
	archive, value := installedClientFixture(t)
	foreign := strings.Repeat("f", 64)
	for _, test := range []struct {
		name  string
		reply any
		call  func(*Client) error
	}{
		{"install-foreign", protocol.KitCoreInstall{Package: corepackage.Inspection{PackageID: foreign, Descriptor: value.Descriptor}}, func(c *Client) error {
			_, e := c.InstallCorePackage(context.Background(), int64(len(archive)), bytes.NewReader(archive), value.PackageID)
			return e
		}},
		{"select-foreign-package", protocol.KitCoreSelection{CoreID: "fes.pong", PackageID: foreign}, func(c *Client) error {
			_, e := c.SelectCorePackage(context.Background(), "fes.pong", "", value.PackageID)
			return e
		}},
		{"select-foreign-core", protocol.KitCoreSelection{CoreID: "fes.foreign", PackageID: value.PackageID}, func(c *Client) error {
			_, e := c.SelectCorePackage(context.Background(), "fes.pong", "", value.PackageID)
			return e
		}},
		{"inventory-missing-package", protocol.KitCorePackages{Selections: []protocol.KitCoreSelection{{CoreID: "fes.pong", PackageID: value.PackageID}}}, func(c *Client) error { _, e := c.InstalledCorePackages(context.Background()); return e }},
		{"inventory-invalid-descriptor", protocol.KitCorePackages{Packages: []corepackage.Inspection{{PackageID: value.PackageID}}}, func(c *Client) error { _, e := c.InstalledCorePackages(context.Background()); return e }},
		{"duplicate-selection", []protocol.KitCoreSelection{{CoreID: "fes.pong", PackageID: value.PackageID}, {CoreID: "fes.pong", PackageID: foreign}}, func(c *Client) error { _, e := c.CoreSelections(context.Background()); return e }},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(test.reply) }))
			defer server.Close()
			base, _ := url.Parse(server.URL)
			if err := test.call(NewClient(base, "test-token", server.Client())); err == nil {
				t.Fatal("accepted malformed response")
			}
		})
	}
}

func TestInstalledCorePackageClientDoesNotRetryLostMutationReply(t *testing.T) {
	archive, value := installedClientFixture(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		connection, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = connection.Close()
		}
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	client := NewClient(base, "test-token", server.Client())
	_, err := client.InstallCorePackage(context.Background(), int64(len(archive)), bytes.NewReader(archive), value.PackageID)
	if err == nil || requests.Load() != 1 {
		t.Fatalf("install retries = %d, %v", requests.Load(), err)
	}
	_, err = client.SelectCorePackage(context.Background(), "fes.pong", "", value.PackageID)
	if err == nil || requests.Load() != 2 {
		t.Fatalf("selection retries = %d, %v", requests.Load(), err)
	}
}

func TestInstalledCorePackageClientBoundedReplyAndConflict(t *testing.T) {
	_, value := installedClientFixture(t)
	for _, conflict := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if conflict {
				w.WriteHeader(409)
				_ = json.NewEncoder(w).Encode(protocol.ErrorEnvelope{Error: protocol.APIError{Code: protocol.CodeCoreSelectionConflict, Message: "refresh"}})
			} else {
				_, _ = io.WriteString(w, strings.Repeat(" ", maxResponseBytes+1))
			}
		}))
		base, _ := url.Parse(server.URL)
		_, err := NewClient(base, "test-token", server.Client()).SelectCorePackage(context.Background(), "fes.pong", "", value.PackageID)
		server.Close()
		if err == nil {
			t.Fatal("accepted oversized/conflicting response")
		}
		if conflict {
			var apiErr *protocol.APIError
			if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeCoreSelectionConflict {
				t.Fatalf("conflict = %v", err)
			}
		}
	}
}
