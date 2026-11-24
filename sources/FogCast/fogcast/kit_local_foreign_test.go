package fogcast

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/DeanoC/FogCast/kitlease"
	"github.com/DeanoC/FogCast/targetclient"
)

func TestKitLocalForeignLeaseUsesFreshScopedOwnership(t *testing.T) {
	var mu sync.Mutex
	state := kitlease.Status{State: "held", Owner: "other", Generation: strings.Repeat("a", 64), ExpiresInMS: 60000}
	fail := false
	reads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/v1/kit/lease":
			reads++
			if fail {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
				return
			}
			_ = json.NewEncoder(w).Encode(state)
		case "/v1/kit/claim":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": state, "token": "owned-token"})
		case "/v1/kit/release":
			_ = json.NewEncoder(w).Encode(kitlease.Status{State: "free"})
		default:
			t.Errorf("unexpected target request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	lease := targetclient.NewKitLease(u, "token", server.Client(), "local", "test")
	t.Cleanup(func() { _ = lease.Close(context.Background()) })
	client := targetclient.NewClient(u, "token", server.Client()).WithKitLease(lease)
	service := &Service{kitHost: true, targets: []TargetConfig{{Name: KitHostTargetName, Enabled: true, Address: server.URL, TargetID: "kit-id", Agent: "token"}}, targetClients: map[string]serviceClient{KitHostTargetName: client}}
	observe := func(want bool, wantErr bool) {
		t.Helper()
		got, err := service.KitLocalForeignLease(context.Background(), KitHostTargetName)
		if got != want || (err != nil) != wantErr {
			t.Fatalf("foreign=%v err=%v want=%v error=%v", got, err, want, wantErr)
		}
	}
	observe(true, false)
	mu.Lock()
	before := reads
	mu.Unlock()
	if foreign, err := service.KitLocalForeignLease(context.Background(), "sibling"); foreign || err != nil {
		t.Fatalf("sibling = %v %v", foreign, err)
	}
	mu.Lock()
	if reads != before {
		t.Fatal("sibling caused local target read")
	}
	state.State = "maintenance"
	mu.Unlock()
	observe(false, false)
	mu.Lock()
	state.State = "recovery-required"
	mu.Unlock()
	observe(false, false)
	mu.Lock()
	state.State = "held"
	state.Generation = ""
	mu.Unlock()
	observe(false, true)
	mu.Lock()
	state.Generation = strings.Repeat("z", 64)
	mu.Unlock()
	observe(false, true)
	mu.Lock()
	state.Generation = strings.Repeat("a", 64)
	fail = true
	mu.Unlock()
	observe(false, true)
	mu.Lock()
	fail = false
	state.Owner = "local"
	mu.Unlock()
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL+"/v1/input/attach", nil)
	if err := lease.Authorize(request, true); err != nil {
		t.Fatal(err)
	}
	observe(false, false)
	mu.Lock()
	state.Owner = "replacement"
	state.Generation = strings.Repeat("b", 64)
	mu.Unlock()
	observe(true, false)
	service.kitHost = false
	if foreign, err := service.KitLocalForeignLease(context.Background(), KitHostTargetName); foreign || err != nil {
		t.Fatalf("non-kit host = %v %v", foreign, err)
	}
}

func TestKitLocalForeignLeaseRejectsTargetReplacementDuringObservation(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/kit/lease" {
			http.NotFound(w, r)
			return
		}
		close(entered)
		<-release
		_ = json.NewEncoder(w).Encode(kitlease.Status{State: "held", Owner: "external", Generation: strings.Repeat("a", 64)})
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	client := targetclient.NewClient(u, "token", server.Client())
	service := &Service{kitHost: true, targets: []TargetConfig{{Name: KitHostTargetName, Enabled: true, Address: server.URL, TargetID: "kit-id", Agent: "token"}}, targetClients: map[string]serviceClient{KitHostTargetName: client}}
	done := make(chan error, 1)
	go func() { _, err := service.KitLocalForeignLease(context.Background(), KitHostTargetName); done <- err }()
	<-entered
	service.targetMu.Lock()
	service.targets[0].TargetID = "replacement"
	service.targetMu.Unlock()
	close(release)
	if err := <-done; err == nil {
		t.Fatal("target replacement was admitted from an old observation")
	}
}
