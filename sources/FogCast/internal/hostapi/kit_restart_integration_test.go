package hostapi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/protocol"
)

type restartAgent struct {
	mu     sync.Mutex
	status protocol.Status
	stops  int
}

func (a *restartAgent) Health(string) protocol.Health {
	return protocol.Health{TargetID: routingDevID, BootID: routingDevBoot, APIVersion: "v1", Ready: true}
}
func (a *restartAgent) Status() protocol.Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	status := a.status
	if status.CorePackage != nil {
		pkg := *status.CorePackage
		status.CorePackage = &pkg
	}
	return status
}
func (a *restartAgent) Stop(context.Context) (protocol.Status, *protocol.APIError) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stops++
	a.status = protocol.Status{State: protocol.StateIdle}
	return a.status, nil
}

// Recreate the actual host service and API around a live agent lease. The
// running agent deliberately reports a package generation without a game ID.
func TestKitHostAPIRecoversAgentLeaseAndStopsAfterHostRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	agentState := &restartAgent{status: routingActivePackageStatus(strings.Repeat("a", 64))}
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	defer manager.Close()
	if _, err := manager.Settled(ctx); err != nil {
		t.Fatal(err)
	}
	var failedHealth atomic.Int32
	agentHandler := httpapi.New(agentState, "token-kit", "test", nil, httpapi.WithKitLease(manager), httpapi.WithTargetID(routingDevID))
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/health" && failedHealth.Load() > 0 {
			failedHealth.Add(-1)
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		agentHandler.ServeHTTP(w, r)
	}))
	defer agent.Close()
	root := t.TempDir()
	configPath := filepath.Join(root, "config.toml")
	config := fmt.Sprintf(`selected_target = "kit"
request_timeout_seconds = 5
upload_timeout_seconds = 5
[core_packages]
kit = true
[[targets]]
name = "kit"
enabled = true
address = %q
agent = "token-kit"
target_id = %q
`, agent.URL, routingDevID)
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	paths := fogcast.Paths{Config: configPath, Index: filepath.Join(root, "state", "library.sqlite3"), Staging: filepath.Join(root, "staging"), CorePackages: filepath.Join(root, "packages")}
	first, err := fogcast.Open(ctx, paths, agent.Client())
	if err != nil {
		t.Fatal(err)
	}
	claim, _ := http.NewRequestWithContext(ctx, http.MethodPost, agent.URL+"/v1/launch", nil)
	if err := first.KitLease().Authorize(claim, true); err != nil {
		t.Fatal(err)
	}
	token := first.KitLease().CurrentToken()
	if token == "" {
		t.Fatal("no local grant")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if manager.Status().State != "held" {
		t.Fatalf("host close released grant: %+v", manager.Status())
	}
	// The target monitor must recover on its own even if the agent is briefly
	// unavailable: no UI or other host API request should be needed to keep
	// the agent's grace-period lease alive after the supervisor restarts us.
	failedHealth.Store(2)
	second, err := fogcast.Open(ctx, paths, agent.Client())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	deadline := time.Now().Add(12 * time.Second)
	for second.KitLease().CurrentToken() != token && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if got := second.KitLease().CurrentToken(); got != token {
		t.Fatalf("target monitor did not recover the original grant without an API request; got %q", got)
	}
	if remaining := failedHealth.Load(); remaining != 0 {
		t.Fatalf("did not exercise both transient agent failures: %d remaining", remaining)
	}
	host := httptest.NewServer(hostapi.New(second))
	defer host.Close()
	client := hostclient.NewClient(host.URL, host.Client())
	session, err := client.Session(ctx)
	if err != nil || session.GameID != "" || session.ID == "" || session.CorePackage == nil || session.CorePackage.Generation != 1 {
		t.Fatalf("recovered session: %+v, %v", session, err)
	}
	if second.KitLease().CurrentToken() != token {
		t.Fatal("restarted host did not recover the original grant")
	}
	agentState.mu.Lock()
	agentState.status.CorePackage.Generation++
	agentState.mu.Unlock()
	changed, err := client.StopExpectedStamped(ctx, session, hostclient.ClientStamp{})
	if err != nil || changed.ErrorCode != "SESSION_CHANGED" {
		t.Fatalf("replacement stop: %+v, %v", changed, err)
	}
	agentState.mu.Lock()
	stops := agentState.stops
	agentState.status.CorePackage.Generation--
	agentState.mu.Unlock()
	if stops != 0 {
		t.Fatal("replacement generation was stopped")
	}
	stopped, err := client.StopExpectedStamped(ctx, session, hostclient.ClientStamp{})
	if err != nil || stopped.State != "idle" {
		t.Fatalf("original stop: %+v, %v", stopped, err)
	}
	agentState.mu.Lock()
	stops = agentState.stops
	agentState.mu.Unlock()
	if stops != 1 {
		t.Fatalf("stop count %d", stops)
	}
	if manager.Status().State != "free" {
		t.Fatalf("grant not released: %+v", manager.Status())
	}
}

// A configured sibling retains ordinary claim/release semantics even when it
// is reachable on loopback, and cannot inherit the local kit's restart grant.
func TestKitHostSiblingKeepsOrdinaryLease(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	localManager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	defer localManager.Close()
	remoteManager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	defer remoteManager.Close()
	for _, manager := range []*kitlease.Manager{localManager, remoteManager} {
		if _, err := manager.Settled(ctx); err != nil {
			t.Fatal(err)
		}
	}
	local := httptest.NewServer(httpapi.New(&restartAgent{}, "local-token", "test", nil, httpapi.WithKitLease(localManager)))
	defer local.Close()
	remote := httptest.NewServer(httpapi.New(&restartAgent{}, "remote-token", "test", nil, httpapi.WithKitLease(remoteManager)))
	defer remote.Close()
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	config := fmt.Sprintf(`selected_target = "kit"
request_timeout_seconds = 5
upload_timeout_seconds = 5
[core_packages]
kit = true
[[targets]]
name = "kit"
enabled = true
address = %q
agent = "local-token"
target_id = %q
[[targets]]
name = "sibling"
enabled = true
address = %q
agent = "remote-token"
target_id = %q
`, local.URL, routingDevID, remote.URL, routingKit2ID)
	if err := os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	paths := fogcast.Paths{Config: path, Index: filepath.Join(root, "state", "library.sqlite3"), Staging: filepath.Join(root, "staging"), CorePackages: filepath.Join(root, "packages")}
	service, err := fogcast.Open(ctx, paths, &http.Client{})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"kit", "sibling"} {
		request, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://unused/v1/launch", nil)
		if err := service.KitLeaseForTarget(name).Authorize(request, true); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	if got := localManager.Status().State; got != "held" {
		t.Fatalf("local grant state=%s", got)
	}
	if got := remoteManager.Status().State; got != "free" {
		t.Fatalf("sibling grant state=%s", got)
	}
}
