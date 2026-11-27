package hostapi_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/kitlease"
)

// A kit host restarted while another owner is playing may observe that play,
// but must not resume its lease or stop it using the observed generation.
func TestKitHostRestartDoesNotOwnForeignPlay(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	agentState := &restartAgent{status: routingActivePackageStatus(strings.Repeat("a", 64))}
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	defer manager.Close()
	if _, err := manager.Settled(ctx); err != nil {
		t.Fatal(err)
	}
	foreign, err := manager.Claim(kitlease.ClaimRequest{
		Owner: "other-shell", Purpose: "playing", RequestID: strings.Repeat("b", 32),
	})
	if err != nil {
		t.Fatal(err)
	}
	agent := httptest.NewServer(httpapi.New(agentState, "token-kit", "test", nil,
		httpapi.WithKitLease(manager), httpapi.WithTargetID(routingDevID)))
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
	assertForeign := func() {
		t.Helper()
		got := manager.Status()
		if got.State != "held" || got.Owner != foreign.Status.Owner || got.Generation != foreign.Status.Generation {
			t.Fatalf("foreign ownership changed: %+v", got)
		}
		agentState.mu.Lock()
		defer agentState.mu.Unlock()
		if agentState.stops != 0 {
			t.Fatalf("foreign play was stopped %d times", agentState.stops)
		}
	}
	for restart := 0; restart < 2; restart++ {
		service, err := fogcast.Open(ctx, paths, agent.Client())
		if err != nil {
			t.Fatal(err)
		}
		host := httptest.NewServer(hostapi.New(service))
		client := hostclient.NewClient(host.URL, host.Client())
		var session hostclient.SessionResult
		deadline := time.Now().Add(5 * time.Second)
		for {
			session, err = client.Session(ctx)
			if err == nil && session.State == "active" && session.CorePackage != nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("foreign observation after restart %d: %+v, %v", restart, session, err)
			}
			time.Sleep(50 * time.Millisecond)
		}
		if service.KitLease().CurrentToken() != "" {
			t.Fatal("host adopted foreign token")
		}
		stopped, err := client.StopExpectedStamped(ctx, session, hostclient.ClientStamp{})
		if err != nil || stopped.HTTPStatus != 403 || stopped.ErrorCode != "KIT_LEASE_DENIED" {
			t.Fatalf("foreign Stop was not denied: %+v, %v", stopped, err)
		}
		assertForeign()
		host.Close()
		if err := service.Close(); err != nil {
			t.Fatal(err)
		}
		assertForeign()
	}
}
