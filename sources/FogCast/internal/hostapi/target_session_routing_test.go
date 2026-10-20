package hostapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

const (
	routingDevID   = "73dc9f5f-1a12-4a95-a820-a9b4e600769a"
	routingKit2ID  = "67c5f4e2-d288-49bb-9049-39ecf39cf6f6"
	routingDevBoot = "fa79433a-2c8a-4b91-9b0a-000000000001"
	routingBBoot   = "2bd61aea-5f16-42ee-9a0b-000000000002"
)

type routingConnection struct {
	State    string `json:"state"`
	Address  string `json:"address"`
	TargetID string `json:"target_id"`
	BootID   string `json:"boot_id"`
}

type routingSession struct {
	State      string            `json:"state"`
	Target     string            `json:"target"`
	TargetID   string            `json:"target_id"`
	Connection routingConnection `json:"connection"`
	Error      *struct {
		Code string `json:"code"`
	} `json:"error"`
}

type routingKit struct {
	mu          sync.Mutex
	name        string
	targetID    string
	bootID      string
	token       string
	leaseToken  string
	generation  string
	recovery    bool
	unavailable bool
	held        bool
	status      protocol.Status
	packageID   string
	calls       map[string]int
}

func newRoutingKit(name, targetID, bootID, token string) *routingKit {
	return &routingKit{
		name: name, targetID: targetID, bootID: bootID, token: token,
		leaseToken: "lease-" + name, generation: "generation-" + name,
		status: protocol.Status{State: protocol.StateIdle}, calls: make(map[string]int),
	}
}

func (k *routingKit) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := r.Method + " " + r.URL.Path
	k.mu.Lock()
	k.calls[key]++
	if r.Header.Get("Authorization") != "Bearer "+k.token {
		k.mu.Unlock()
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch key {
	case "GET /v1/health":
		if k.unavailable {
			k.mu.Unlock()
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		health := protocol.Health{TargetID: k.targetID, APIVersion: "v1", AgentVersion: "test", Ready: true, BootID: k.bootID}
		k.mu.Unlock()
		_ = json.NewEncoder(w).Encode(health)
	case "GET /v1/kit/lease":
		state, owner := "free", ""
		if k.recovery {
			state, owner = "recovery-required", "kit-recovery"
		} else if k.held {
			state, owner = "held", "fogcast@test"
		}
		generation := ""
		if k.held {
			generation = k.generation
		}
		k.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]string{"state": state, "owner": owner, "generation": generation})
	case "POST /v1/kit/claim":
		if k.recovery {
			k.mu.Unlock()
			http.Error(w, "recovery required", http.StatusConflict)
			return
		}
		k.held = true
		token, generation := k.leaseToken, k.generation
		k.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": map[string]any{"state": "held", "generation": generation, "expires_in_ms": 60000},
			"token":  token,
		})
	case "POST /v1/kit/renew":
		token, generation := k.leaseToken, k.generation
		k.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": map[string]any{"state": "held", "generation": generation, "expires_in_ms": 60000},
			"token":  token,
		})
	case "POST /v1/kit/release":
		k.held = false
		k.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]string{"state": "free"})
	case "GET /v1/status":
		status := k.status
		k.mu.Unlock()
		_ = json.NewEncoder(w).Encode(status)
	case "POST /v1/library/core/load":
		if !k.held || r.Header.Get("X-FogCast-Kit-Lease") != k.leaseToken {
			k.mu.Unlock()
			http.Error(w, "missing kit lease", http.StatusConflict)
			return
		}
		data, err := io.ReadAll(r.Body)
		if err != nil || len(data) == 0 {
			k.mu.Unlock()
			http.Error(w, "missing package", http.StatusBadRequest)
			return
		}
		k.packageID = r.Header.Get("X-FogCast-Package-ID")
		status := routingActivePackageStatus(k.packageID)
		k.status = status
		k.mu.Unlock()
		_ = json.NewEncoder(w).Encode(status)
	case "POST /v1/stop":
		if !k.held || r.Header.Get("X-FogCast-Kit-Lease") != k.leaseToken {
			k.mu.Unlock()
			http.Error(w, "missing kit lease", http.StatusConflict)
			return
		}
		k.status = protocol.Status{State: protocol.StateIdle}
		k.mu.Unlock()
		_ = json.NewEncoder(w).Encode(protocol.Status{State: protocol.StateIdle})
	default:
		k.mu.Unlock()
		http.Error(w, "unexpected kit request", http.StatusNotFound)
	}
}

func (k *routingKit) count(methodPath string) int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.calls[methodPath]
}

func (k *routingKit) setRecovery(recovery bool) {
	k.mu.Lock()
	k.recovery = recovery
	k.mu.Unlock()
}

func (k *routingKit) setUnavailable(unavailable bool) {
	k.mu.Lock()
	k.unavailable = unavailable
	k.mu.Unlock()
}

func TestNamedSessionOperationsUseOnlyTheirConfiguredKit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	dev := newRoutingKit("dev", routingDevID, routingDevBoot, "token-dev")
	kit2 := newRoutingKit("kit2", routingKit2ID, routingBBoot, "token-kit2")
	dev.recovery = true
	devServer := httptest.NewServer(dev)
	defer devServer.Close()
	kit2Server := httptest.NewServer(kit2)
	defer kit2Server.Close()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	config := fmt.Sprintf(`selected_target = "dev"
request_timeout_seconds = 5
upload_timeout_seconds = 5

[[targets]]
name = "dev"
enabled = true
address = %q
agent = "token-dev"
target_id = %q

[[targets]]
name = "kit2"
enabled = true
address = %q
agent = "token-kit2"
target_id = %q
`, devServer.URL, routingDevID, kit2Server.URL, routingKit2ID)
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	paths := fogcast.Paths{
		Config: configPath, Index: filepath.Join(dir, "state", "library.sqlite3"),
		Staging: filepath.Join(dir, "staging"), CorePackages: filepath.Join(dir, "packages"),
	}
	raw := routingPackageFixture(t)
	packages, err := corepackage.NewStore(paths.CorePackages)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	inspection, _, err := packages.Import(ctx, int64(len(raw)), bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("Import package fixture: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.Index), 0700); err != nil {
		t.Fatal(err)
	}
	catalogStore, err := catalog.OpenContext(ctx, paths.Index)
	if err != nil {
		t.Fatalf("Open catalog: %v", err)
	}
	entry, err := catalogStore.CreateCoreEntry(ctx, "Two target routing fixture", inspection.Descriptor.Core.ID, inspection.PackageID)
	if err != nil {
		_ = catalogStore.Close()
		t.Fatalf("CreateCoreEntry: %v", err)
	}
	if err := catalogStore.Close(); err != nil {
		t.Fatalf("Close catalog: %v", err)
	}
	service, err := fogcast.Open(ctx, paths, &http.Client{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer service.Close()
	handler := hostapi.New(service)

	// The selected dev kit is recovering. A scoped kit2 status must still
	// resolve and report kit2 without probing or reusing dev's connection.
	devStatusBefore := dev.count("GET /v1/status")
	devLeaseBefore := dev.count("GET /v1/kit/lease")
	kit2StatusBefore := kit2.count("GET /v1/status")
	code, body := serveRoutingSession(t, handler, http.MethodGet, "/api/v1/session?target=kit2", "")
	var session routingSession
	if err := json.Unmarshal(body, &session); err != nil {
		t.Fatalf("kit2 session status=%d body=%s: %v", code, body, err)
	}
	if code != http.StatusOK || session.Target != "kit2" || session.TargetID != routingKit2ID ||
		session.Connection.State != "ready" || session.Connection.Address != kit2Server.URL ||
		session.Connection.TargetID != routingKit2ID || session.Connection.BootID != routingBBoot {
		t.Fatalf("kit2 session status=%d result=%+v body=%s", code, session, body)
	}
	if dev.count("GET /v1/status") != devStatusBefore || dev.count("GET /v1/kit/lease") != devLeaseBefore || kit2.count("GET /v1/status") <= kit2StatusBefore {
		t.Fatalf("kit2 GET used wrong server: dev status/lease=%d/%d (before %d/%d), kit2 status=%d (before %d)",
			dev.count("GET /v1/status"), dev.count("GET /v1/kit/lease"), devStatusBefore, devLeaseBefore,
			kit2.count("GET /v1/status"), kit2StatusBefore)
	}

	dev.setRecovery(false)
	code, body = serveRoutingSession(t, handler, http.MethodGet, "/api/v1/session?target=dev", "")
	if err := json.Unmarshal(body, &session); err != nil {
		t.Fatalf("dev session status=%d body=%s: %v", code, body, err)
	}
	if code != http.StatusOK || session.Target != "dev" || session.TargetID != routingDevID ||
		session.Connection.State != "ready" || session.Connection.Address != devServer.URL ||
		session.Connection.TargetID != routingDevID || session.Connection.BootID != routingDevBoot {
		t.Fatalf("dev session status=%d result=%+v body=%s", code, session, body)
	}

	for _, target := range []struct {
		name    string
		id      string
		bootID  string
		address string
		other   *routingKit
		kit     *routingKit
	}{
		{name: "dev", id: routingDevID, bootID: routingDevBoot, address: devServer.URL, kit: dev, other: kit2},
		{name: "kit2", id: routingKit2ID, bootID: routingBBoot, address: kit2Server.URL, kit: kit2, other: dev},
	} {
		beforeLoad, otherLoad := target.kit.count("POST /v1/library/core/load"), target.other.count("POST /v1/library/core/load")
		code, body = serveRoutingSession(t, handler, http.MethodPost, "/api/v1/session/launch", fmt.Sprintf(`{"game_id":%q,"target":%q}`, entry.GameID, target.name))
		if err := json.Unmarshal(body, &session); err != nil {
			t.Fatalf("%s launch status=%d body=%s: %v", target.name, code, body, err)
		}
		if code != http.StatusOK || session.State != string(protocol.StateActive) || session.Target != target.name || session.TargetID != target.id {
			t.Fatalf("%s launch status=%d result=%+v body=%s", target.name, code, session, body)
		}
		if target.kit.count("POST /v1/library/core/load") != beforeLoad+1 || target.other.count("POST /v1/library/core/load") != otherLoad {
			t.Fatalf("%s launch routed to wrong kit: load counts %d/%d (before %d/%d)", target.name,
				target.kit.count("POST /v1/library/core/load"), target.other.count("POST /v1/library/core/load"), beforeLoad, otherLoad)
		}

		code, body = serveRoutingSession(t, handler, http.MethodGet, "/api/v1/session?target="+target.name, "")
		if err := json.Unmarshal(body, &session); err != nil {
			t.Fatalf("%s post-launch session status=%d body=%s: %v", target.name, code, body, err)
		}
		if code != http.StatusOK || session.Target != target.name || session.TargetID != target.id ||
			session.Connection.Address != target.address ||
			session.Connection.TargetID != target.id || session.Connection.BootID != target.bootID {
			t.Fatalf("%s post-launch session status=%d result=%+v body=%s", target.name, code, session, body)
		}

		beforeStop, otherStop := target.kit.count("POST /v1/stop"), target.other.count("POST /v1/stop")
		code, body = serveRoutingSession(t, handler, http.MethodPost, "/api/v1/session/stop", fmt.Sprintf(`{"target":%q}`, target.name))
		if err := json.Unmarshal(body, &session); err != nil {
			t.Fatalf("%s stop status=%d body=%s: %v", target.name, code, body, err)
		}
		if code != http.StatusOK || session.State != string(protocol.StateIdle) || session.Target != target.name || session.TargetID != target.id {
			t.Fatalf("%s stop status=%d result=%+v body=%s", target.name, code, session, body)
		}
		if target.kit.count("POST /v1/stop") != beforeStop+1 || target.other.count("POST /v1/stop") != otherStop {
			t.Fatalf("%s stop routed to wrong kit: stop counts %d/%d (before %d/%d)", target.name,
				target.kit.count("POST /v1/stop"), target.other.count("POST /v1/stop"), beforeStop, otherStop)
		}

		code, body = serveRoutingSession(t, handler, http.MethodGet, "/api/v1/session?target="+target.name, "")
		if err := json.Unmarshal(body, &session); err != nil {
			t.Fatalf("%s post-stop session status=%d body=%s: %v", target.name, code, body, err)
		}
		if code != http.StatusOK || session.State != string(protocol.StateIdle) || session.Target != target.name ||
			session.TargetID != target.id || session.Connection.Address != target.address ||
			session.Connection.TargetID != target.id || session.Connection.BootID != target.bootID {
			t.Fatalf("%s post-stop session status=%d result=%+v body=%s", target.name, code, session, body)
		}
	}

	kit2.setUnavailable(true)
	code, body = serveRoutingSession(t, handler, http.MethodGet, "/api/v1/session?target=kit2", "")
	if err := json.Unmarshal(body, &session); err != nil {
		t.Fatalf("unavailable kit2 status=%d body=%s: %v", code, body, err)
	}
	if code != http.StatusServiceUnavailable || session.Error == nil || session.Error.Code != "TARGET_UNAVAILABLE" ||
		session.Connection.State != "disconnected" || session.Connection.Address != kit2Server.URL ||
		session.Connection.TargetID != routingKit2ID || session.Connection.BootID != routingBBoot {
		t.Fatalf("unavailable kit2 status=%d result=%+v body=%s", code, session, body)
	}
}

func serveRoutingSession(t *testing.T, handler http.Handler, method, path, body string) (int, []byte) {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Host = "127.0.0.1"
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response.Code, response.Body.Bytes()
}

func routingPackageFixture(t *testing.T) []byte {
	t.Helper()
	base := "../../corepackage/testdata/core-bundle-v2/"
	manifest, err := os.ReadFile(base + "manifests/valid-basic.toml")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(base + "payloads/fes-fixture.rbf")
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	for _, member := range []struct {
		name string
		data []byte
	}{{name: "manifest.toml", data: manifest}, {name: "core.rbf", data: payload}} {
		header := make([]byte, 512)
		copy(header, member.name)
		copy(header[100:], "0000644\x00")
		copy(header[108:], "0000000\x00")
		copy(header[116:], "0000000\x00")
		copy(header[124:], fmt.Sprintf("%011o\x00", len(member.data)))
		copy(header[136:], "00000000000\x00")
		copy(header[148:], "        ")
		header[156] = '0'
		copy(header[257:], "ustar\x00")
		copy(header[263:], "00")
		checksum := 0
		for _, value := range header {
			checksum += int(value)
		}
		copy(header[148:], fmt.Sprintf("%06o\x00 ", checksum))
		archive.Write(header)
		archive.Write(member.data)
		archive.Write(make([]byte, (512-len(member.data)%512)%512))
	}
	archive.Write(make([]byte, 1024))
	return archive.Bytes()
}

func routingActivePackageStatus(packageID string) protocol.Status {
	inspection := &protocol.CorePackageStatus{
		PackageID: packageID, Generation: 1,
		ABI:     protocol.RuntimeContract{ID: "fes.simple-game", Major: 1},
		BuildID: "0123456789abcdef0123456789abcdef",
		ActiveInterfaces: []protocol.RuntimeInterface{
			{ID: "fes.gamepad", Major: 1},
			{ID: "fes.video.fixed-720p60", Major: 1},
		},
		Gamepad: true, PersistenceMode: "volatile",
	}
	coreID := "fes.pong"
	return protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &coreID, CorePackage: inspection}
}
