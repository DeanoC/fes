package hostapi_test

import (
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

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/internal/hostexec"
	"github.com/DeanoC/FogCast/protocol"
)

type integrationProcess struct {
	done chan struct{}
	once sync.Once
}

func (p *integrationProcess) Wait() error { <-p.done; return nil }
func (p *integrationProcess) Kill() error {
	p.once.Do(func() { close(p.done) })
	return nil
}

type integrationMedia struct {
	mu      sync.Mutex
	started int
	stopped int
	active  *integrationMediaHandle
}

type integrationMediaHandle struct {
	owner   *integrationMedia
	mu      sync.Mutex
	stopped bool
}

func (m *integrationMedia) Start(context.Context, string) (hostapi.MediaHandle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started++
	h := &integrationMediaHandle{owner: m}
	m.active = h
	return h, nil
}

func (h *integrationMediaHandle) Stop(context.Context) error {
	h.mu.Lock()
	if h.stopped {
		h.mu.Unlock()
		return nil
	}
	h.stopped = true
	h.mu.Unlock()
	h.owner.mu.Lock()
	h.owner.stopped++
	h.owner.mu.Unlock()
	return nil
}

func TestHostOnlyRunnerUsesOneRootSessionThroughHTTP(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	roms := filepath.Join(dir, "roms")
	if err := os.MkdirAll(roms, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(roms, "runner-test.sfc"), []byte("synthetic rom"), 0600); err != nil {
		t.Fatal(err)
	}
	core := filepath.Join(dir, "snes.so")
	if err := os.WriteFile(core, []byte("synthetic core"), 0600); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "config.toml")
	text := fmt.Sprintf(`selected_target = "kit"
request_timeout_seconds = 1
upload_timeout_seconds = 1

[[targets]]
name = "kit"
enabled = true
address = "http://127.0.0.1:1"
agent = "unreachable"

[[libraries]]
id = "snes"
system = "snes"
root = %q

[host_emulator]
binary = "/fake/retroarch"

[[host_emulator.cores]]
platform = "snes"
core = %q
`, roms, core)
	if err := os.WriteFile(config, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	processes := make(chan *integrationProcess, 4)
	starter := func(context.Context, string, ...string) (hostexec.Process, error) {
		p := &integrationProcess{done: make(chan struct{})}
		processes <- p
		return p, nil
	}
	service, err := fogcast.OpenWithHostProcessStarter(ctx, fogcast.Paths{
		Config: config, Index: filepath.Join(dir, "catalog.sqlite3"), Staging: filepath.Join(dir, "staging"),
	}, &http.Client{Timeout: time.Second}, starter)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if _, err := service.ReconcileFolderWatch(ctx); err != nil {
		t.Fatal(err)
	}
	games, err := service.Games(ctx)
	if err != nil || len(games) != 1 {
		t.Fatalf("games=%+v err=%v", games, err)
	}
	if execution, err := service.SessionExecution(ctx, games[0].ID); err != nil || execution != fogcast.ExecutionHostOnly {
		t.Fatalf("resolved execution=%q err=%v game=%+v", execution, err, games[0])
	}
	media := &integrationMedia{}
	server := httptest.NewServer(hostapi.New(service, hostapi.WithMediaSession(media)))
	defer server.Close()
	request := func(method, path, body string) (*http.Response, map[string]any) {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var value map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&value); err != nil {
			t.Fatal(err)
		}
		return resp, value
	}
	get := func() (int, map[string]any) {
		resp, value := request(http.MethodGet, "/api/v1/session", "")
		return resp.StatusCode, value
	}
	stop := func() (int, map[string]any) {
		resp, value := request(http.MethodPost, "/api/v1/session/stop", "")
		return resp.StatusCode, value
	}
	launch := func() (int, map[string]any) {
		body, _ := json.Marshal(map[string]string{"game_id": games[0].ID})
		resp, value := request(http.MethodPost, "/api/v1/session/launch", string(body))
		return resp.StatusCode, value
	}
	status, before := get()
	if status != 200 || before["state"] != string(protocol.StateIdle) {
		t.Fatalf("before: %d %+v", status, before)
	}
	status, started := launch()
	if status != 200 || started["state"] != string(protocol.StateActive) || started["execution"] != fogcast.ExecutionHostOnly {
		t.Fatalf("launch: %d %+v", status, started)
	}
	id := started["id"]
	status, during := get()
	if status != 200 || during["id"] != id || during["execution"] != fogcast.ExecutionHostOnly {
		t.Fatalf("during: %d %+v; launch id=%v", status, during, id)
	}
	firstProcess := <-processes
	if status, _ := launch(); status != http.StatusConflict {
		t.Fatalf("second launch status=%d, want 409", status)
	}
	if status, stopped := stop(); status != 200 || stopped["state"] != string(protocol.StateIdle) {
		t.Fatalf("stop: %d %+v", status, stopped)
	}
	_ = firstProcess
	if status, after := get(); status != 200 || after["state"] != string(protocol.StateIdle) {
		t.Fatalf("after stop: %d %+v", status, after)
	}
	if status, _ := launch(); status != 200 {
		t.Fatalf("relaunch status=%d", status)
	}
	process := <-processes
	process.once.Do(func() { close(process.done) })
	deadline := time.Now().Add(2 * time.Second)
	var after map[string]any
	for {
		status, after = get()
		if status != 200 || after["state"] != string(protocol.StateActive) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("natural exit was not reaped: %+v", after)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if status != 200 || after["state"] != string(protocol.StateIdle) {
		t.Fatalf("after natural exit: %d %+v", status, after)
	}
	media.mu.Lock()
	stoppedCount := media.stopped
	media.mu.Unlock()
	if stoppedCount != 2 {
		t.Fatalf("media handles stopped=%d, want 2", stoppedCount)
	}
	if status, result := stop(); status != 200 || result["state"] != string(protocol.StateIdle) {
		t.Fatalf("stop after exit: %d %+v", status, result)
	}
	if status, result := stop(); status != 200 || result["state"] != string(protocol.StateIdle) {
		t.Fatalf("stop without session: %d %+v", status, result)
	}
	resp, explicit := request(http.MethodGet, "/api/v1/session?target=kit", "")
	if resp.StatusCode != http.StatusServiceUnavailable || explicit["error"] == nil {
		t.Fatalf("explicit target status=%d %+v", resp.StatusCode, explicit)
	}
	explicitStopBody := `{"target":"kit"}`
	resp, explicit = request(http.MethodPost, "/api/v1/session/stop", explicitStopBody)
	if resp.StatusCode == http.StatusOK || explicit["error"] == nil {
		t.Fatalf("explicit target Stop=%d %+v", resp.StatusCode, explicit)
	}
}
