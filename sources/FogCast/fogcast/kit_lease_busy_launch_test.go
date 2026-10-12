package fogcast_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

// TestKitLeaseBusyCorePackageLaunchRefusesHostSession drives a real core-package
// launch through the service and the host session API. The target refuses the
// launch claim with KIT_LEASE_BUSY. The host answers HTTP 409 with the fixed
// ownership message and does not publish a play.
func TestKitLeaseBusyCorePackageLaunchRefusesHostSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	const (
		agent                 = "secret"
		upstreamOwnerText     = "raw-upstream-owner-holds-kit"
		fixedOwnershipMessage = "Another session owns the kit; release it from that session before retrying."
	)
	idle, err := json.Marshal(protocol.Status{State: protocol.StateIdle})
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var paths []string
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+agent {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/health":
			_ = json.NewEncoder(w).Encode(protocol.Health{APIVersion: "v1", Ready: true})
		case "GET /v1/status":
			w.Write(idle)
		case "POST /v1/kit/claim":
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(protocol.ErrorEnvelope{Error: protocol.APIError{
				Code: protocol.CodeKitLeaseBusy, Message: upstreamOwnerText,
			}})
		default:
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	}))
	defer peer.Close()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	config := fmt.Sprintf(`selected_target = "kit"
request_timeout_seconds = 5
upload_timeout_seconds = 5

[[targets]]
name = "kit"
enabled = true
address = %q
agent = %q
`, peer.URL, agent)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	pathsCfg := fogcast.Paths{
		Config:  configPath,
		Index:   filepath.Join(dir, "state", "library.sqlite3"),
		Staging: filepath.Join(dir, "staging"),
	}
	if err := os.MkdirAll(filepath.Dir(pathsCfg.Index), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pathsCfg.Staging, 0o700); err != nil {
		t.Fatal(err)
	}
	// Seed the catalog and package store before Open. Creating an entry
	// through the service inspects the target; this launch test only needs
	// the claim refusal on the already installed package.
	raw := busyLaunchPackageFixture(t)
	packages, err := corepackage.NewStore(filepath.Join(filepath.Dir(pathsCfg.Index), "core-packages"))
	if err != nil {
		t.Fatal(err)
	}
	imported, _, err := packages.Import(ctx, int64(len(raw)), bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	store, err := catalog.OpenContext(ctx, pathsCfg.Index)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := store.CreateCoreEntry(ctx, "Valid package Pong", imported.Descriptor.Core.ID, imported.PackageID)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	service, err := fogcast.Open(ctx, pathsCfg, peer.Client())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer service.Close()

	handler := hostapi.New(service)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/launch", strings.NewReader(`{"game_id":"`+entry.GameID+`"}`))
	request.Host = "127.0.0.1"
	request = request.WithContext(ctx)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Phase   string `json:"phase"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("status %d body %s: %v", response.Code, response.Body.String(), err)
	}
	if response.Code != http.StatusConflict || body.Error.Code != string(protocol.CodeKitLeaseBusy) || body.Error.Message != fixedOwnershipMessage || body.Error.Phase != "request" {
		t.Fatalf("launch status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), upstreamOwnerText) || strings.Contains(response.Body.String(), "failed internally") {
		t.Fatalf("public launch body kept upstream or internal wording: %s", response.Body.String())
	}

	mu.Lock()
	seen := append([]string(nil), paths...)
	mu.Unlock()
	claims, loads := 0, 0
	for _, path := range seen {
		switch path {
		case "POST /v1/kit/claim":
			claims++
		case "POST /v1/library/core/load":
			loads++
		}
	}
	if claims != 1 || loads != 0 {
		t.Fatalf("target calls=%v", seen)
	}
	if plays := service.PlaySessions(); len(plays) != 0 {
		t.Fatalf("refused claim published plays: %+v", plays)
	}

	listed := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil).WithContext(ctx)
	listRequest.Host = "127.0.0.1"
	handler.ServeHTTP(listed, listRequest)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"sessions":[]`) {
		t.Fatalf("sessions after refused claim: %d %s", listed.Code, listed.Body.String())
	}
}

func busyLaunchPackageFixture(t *testing.T) []byte {
	t.Helper()
	base := "../corepackage/testdata/core-bundle-v2/"
	manifest, err := os.ReadFile(base + "manifests/valid-basic.toml")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(base + "payloads/fes-fixture.rbf")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	for _, entry := range []struct {
		name string
		data []byte
	}{{"manifest.toml", manifest}, {"core.rbf", payload}} {
		header := make([]byte, 512)
		copy(header, entry.name)
		copy(header[100:], "0000644\x00")
		copy(header[108:], "0000000\x00")
		copy(header[116:], "0000000\x00")
		copy(header[124:], fmt.Sprintf("%011o\x00", len(entry.data)))
		copy(header[136:], "00000000000\x00")
		copy(header[148:], "        ")
		header[156] = '0'
		copy(header[257:], "ustar\x00")
		copy(header[263:], "00")
		sum := 0
		for _, value := range header {
			sum += int(value)
		}
		copy(header[148:], fmt.Sprintf("%06o\x00 ", sum))
		out.Write(header)
		out.Write(entry.data)
		if pad := (512 - len(entry.data)%512) % 512; pad != 0 {
			out.Write(make([]byte, pad))
		}
	}
	out.Write(make([]byte, 1024))
	return out.Bytes()
}
