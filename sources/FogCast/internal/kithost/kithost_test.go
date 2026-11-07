package kithost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/protocol"
)

const testTargetID = "73dc9f5f-1a12-4a95-a820-a9b4e600769a"

// fakeAgent reports its target ID only to a caller with the bearer, as
// mister-agent's /v1/health does. It answers after ready health probes.
func fakeAgent(t *testing.T, token string, ready int32) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var probes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/health" {
			http.NotFound(w, r)
			return
		}
		if probes.Add(1) <= ready {
			http.Error(w, "starting", http.StatusServiceUnavailable)
			return
		}
		health := protocol.Health{}
		if r.Header.Get("Authorization") == "Bearer "+token {
			health.TargetID = testTargetID
		}
		_ = json.NewEncoder(w).Encode(health)
	}))
	t.Cleanup(server.Close)
	return server, &probes
}

func writeFile(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

type fixture struct {
	options Options
	root    string
	logs    []string
}

func newFixture(t *testing.T, listen, token string) *fixture {
	t.Helper()
	dir := t.TempDir()
	template, err := os.ReadFile("../../../../image/buildroot/board/fogcast-target/native-rootfs-overlay/usr/share/fogcast/config.toml")
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{root: filepath.Join(dir, "fesdata3", "fogcast-host")}
	f.options = Options{
		AgentConfig:     writeFile(t, filepath.Join(dir, "agent.toml"), "listen_address = \""+listen+"\"\ntoken = \""+token+"\"\n"),
		Template:        writeFile(t, filepath.Join(dir, "template.toml"), string(template)),
		StateRoot:       f.root,
		FallbackRoot:    filepath.Join(dir, "tmpfs", "kit-host"),
		AgentWait:       3 * time.Second,
		CheckPersistent: func(string) error { return nil },
		Logf:            func(format string, args ...any) { f.logs = append(f.logs, fmt.Sprintf(format, args...)) },
	}
	return f
}

func listenOf(server *httptest.Server) string {
	return "0.0.0.0:" + server.URL[strings.LastIndex(server.URL, ":")+1:]
}

func TestPrepareWaitsForTheAgentAndWritesItsTargetID(t *testing.T) {
	agent, probes := fakeAgent(t, "agent-token", 3)
	f := newFixture(t, listenOf(agent), "agent-token")
	path, err := Prepare(context.Background(), f.options)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(f.root, "config.toml") || probes.Load() < 4 {
		t.Fatalf("path %s probes %d", path, probes.Load())
	}
	config, err := fogcast.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	target := config.Targets[0]
	if target.TargetID != testTargetID || target.Agent != "agent-token" || !strings.HasPrefix(target.Address, "http://127.0.0.1:") || !config.CorePackages.Kit {
		t.Fatalf("target %+v kit %v", target, config.CorePackages.Kit)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode %v %v", info, err)
	}
	if paths, err := fogcast.PathsForConfig(path); err != nil || !strings.HasPrefix(paths.Index, f.root+"/state") {
		t.Fatalf("paths %+v %v", paths, err)
	}
}

func TestPrepareStartsWithoutATargetIDWhenTheAgentStaysAway(t *testing.T) {
	f := newFixture(t, "0.0.0.0:8182", "agent-token")
	f.options.AgentWait = 600 * time.Millisecond
	f.options.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return agentResponse(r, http.StatusServiceUnavailable, "starting"), nil
	})}
	started := time.Now()
	path, err := Prepare(context.Background(), f.options)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < f.options.AgentWait || elapsed > 2*time.Second {
		t.Fatalf("missing agent wait lasted %s; want bounded wait around %s", elapsed, f.options.AgentWait)
	}
	config, err := fogcast.LoadConfig(path)
	if err != nil || config.Targets[0].TargetID != "" {
		t.Fatalf("target %+v %v", config.Targets, err)
	}
	if !strings.Contains(strings.Join(f.logs, "|"), "did not get the agent's target ID") {
		t.Fatalf("missing target ID was not recorded: %q", f.logs)
	}
}

func TestDefaultAgentWaitRemainsThirtySeconds(t *testing.T) {
	if DefaultAgentWait != 30*time.Second {
		t.Fatalf("default agent wait = %s, want 30s", DefaultAgentWait)
	}
}

func TestWaitForAgentFailsWithinBoundWhenAgentNeverAppears(t *testing.T) {
	const wait = 350 * time.Millisecond
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return agentResponse(r, http.StatusServiceUnavailable, "starting"), nil
	})}
	started := time.Now()
	_, err := WaitForAgent(context.Background(), client, "http://agent.invalid", "agent-token", wait)
	if err == nil {
		t.Fatal("agent that never appeared returned a target ID")
	}
	if elapsed := time.Since(started); elapsed < wait || elapsed > wait+250*time.Millisecond {
		t.Fatalf("missing agent wait lasted %s; want bounded wait around %s", elapsed, wait)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func agentResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(body)), Request: request,
	}
}

// A wrong token never yields a target ID, so kit mode does not guess one,
// and it stops waiting at the first answer.
func TestPrepareDoesNotTakeATargetIDWithoutTheBearer(t *testing.T) {
	agent, probes := fakeAgent(t, "agent-token", 0)
	f := newFixture(t, listenOf(agent), "other-token")
	f.options.AgentWait = 10 * time.Second
	started := time.Now()
	path, err := Prepare(context.Background(), f.options)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second || probes.Load() != 1 {
		t.Fatalf("waited %s over %d probes for a rejected token", elapsed, probes.Load())
	}
	if config, err := fogcast.LoadConfig(path); err != nil || config.Targets[0].TargetID != "" {
		t.Fatalf("target %+v %v", config.Targets, err)
	}
}

func TestPrepareRefusesAnAgentConfigThatDisagreesWithTheAgent(t *testing.T) {
	agent, _ := fakeAgent(t, "agent-token", 0)
	f := newFixture(t, listenOf(agent), "agent-token")
	writeFile(t, f.options.AgentConfig, "listen_address = \""+listenOf(agent)+"\"\ntoken = \"agent-token\"\ntarget_id = \"0d0f2c4e-5b6a-4c3d-8e9f-a1b2c3d4e5f6\"\n")
	if _, err := Prepare(context.Background(), f.options); err == nil || !strings.Contains(err.Error(), "target ID") {
		t.Fatalf("err = %v", err)
	}
}

func TestPrepareFallsBackToTmpfsWithoutFESDATA3(t *testing.T) {
	agent, _ := fakeAgent(t, "agent-token", 0)
	f := newFixture(t, listenOf(agent), "agent-token")
	f.options.CheckPersistent = func(path string) error {
		if path != f.root {
			t.Errorf("checked %s", path)
		}
		// mister-agent prepares FESDATA3, so the check follows the wait.
		if !strings.Contains(strings.Join(f.logs, "|"), "reached the agent") {
			t.Error("FESDATA3 checked before the agent wait")
		}
		return errors.New("FESDATA3 is not mounted")
	}
	path, err := Prepare(context.Background(), f.options)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, f.options.FallbackRoot) {
		t.Fatalf("path %s, want under %s", path, f.options.FallbackRoot)
	}
	if _, err := os.Stat(f.root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created the unverified root: %v", err)
	}
	if !strings.Contains(strings.Join(f.logs, "|"), "not on FESDATA3") {
		t.Fatalf("logs %q", f.logs)
	}
}

func TestPrepareFallsBackWhenStateOrStagingIsMountedSeparately(t *testing.T) {
	for _, childName := range []string{"state", "staging"} {
		t.Run(childName, func(t *testing.T) {
			agent, _ := fakeAgent(t, "agent-token", 0)
			f := newFixture(t, listenOf(agent), "agent-token")
			child := filepath.Join(f.root, childName)
			if err := os.MkdirAll(child, 0o700); err != nil {
				t.Fatal(err)
			}
			checked := []string{}
			f.options.CheckPersistent = func(path string) error {
				checked = append(checked, path)
				if path == child {
					return errors.New("nested mount on another filesystem")
				}
				return nil
			}

			path, err := Prepare(context.Background(), f.options)
			if err != nil {
				t.Fatal(err)
			}
			wantPath := filepath.Join(f.options.FallbackRoot, "config.toml")
			if path != wantPath {
				t.Fatalf("config path %s, want fallback %s (checked %v)", path, wantPath, checked)
			}
			wantChecked := []string{f.root}
			if childName == "staging" {
				wantChecked = append(wantChecked, filepath.Join(f.root, "state"))
			}
			wantChecked = append(wantChecked, child)
			if strings.Join(checked, "\n") != strings.Join(wantChecked, "\n") {
				t.Fatalf("checked paths %v, want %v", checked, wantChecked)
			}
			if _, err := os.Stat(filepath.Join(f.root, "config.toml")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("wrote config beneath separately mounted child: %v", err)
			}
		})
	}
}

func TestPrepareNeedsTheAgentConfig(t *testing.T) {
	f := newFixture(t, "0.0.0.0:8182", "agent-token")
	f.options.AgentConfig = filepath.Join(t.TempDir(), "missing.toml")
	if _, err := Prepare(context.Background(), f.options); err == nil || !strings.Contains(err.Error(), "missing.toml") {
		t.Fatalf("err = %v", err)
	}
}

func TestPrepareKeepsAnAddedLibraryAcrossRestarts(t *testing.T) {
	agent, _ := fakeAgent(t, "agent-token", 0)
	f := newFixture(t, listenOf(agent), "agent-token")
	path, err := Prepare(context.Background(), f.options)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	added := "\n[[libraries]]\nid = \"md-usb\"\nsystem = \"megadrive\"\nroot = \"/media/usb0/md\"\n"
	writeFile(t, path, string(data)+added)
	if _, err := Prepare(context.Background(), f.options); err != nil {
		t.Fatal(err)
	}
	config, err := fogcast.LoadConfig(path)
	if err != nil || len(config.Libraries) != 2 || config.Libraries[1].Path != "/media/usb0/md" {
		t.Fatalf("libraries %+v %v", config.Libraries, err)
	}
}

func TestPrepareMovesAnUnreadableConfigAside(t *testing.T) {
	agent, _ := fakeAgent(t, "agent-token", 0)
	f := newFixture(t, listenOf(agent), "agent-token")
	if err := os.MkdirAll(f.root, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(f.root, "config.toml"), "key_from_a_newer_bundle = true\n")
	path, err := Prepare(context.Background(), f.options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fogcast.LoadConfig(path); err != nil {
		t.Fatal(err)
	}
	aside, _ := filepath.Glob(filepath.Join(f.root, "config.toml.unreadable-*"))
	if len(aside) != 1 {
		t.Fatalf("aside = %v", aside)
	}
	if data, err := os.ReadFile(aside[0]); err != nil || !strings.Contains(string(data), "newer_bundle") {
		t.Fatalf("kept %q %v", data, err)
	}
}

func TestPrepareStopsWhenCancelledDuringTheWait(t *testing.T) {
	agent, _ := fakeAgent(t, "agent-token", 1<<30)
	f := newFixture(t, listenOf(agent), "agent-token")
	f.options.AgentWait = time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := Prepare(ctx, f.options); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(f.root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state created after cancellation: %v", err)
	}
}

func TestAgentAddressStaysOnLoopback(t *testing.T) {
	for listen, want := range map[string]string{
		"0.0.0.0:8182":   "http://127.0.0.1:8182",
		":8182":          "http://127.0.0.1:8182",
		"[::]:9000":      "http://127.0.0.1:9000",
		"127.0.0.1:8182": "http://127.0.0.1:8182",
		"localhost:8182": "http://127.0.0.1:8182",
		"[::1]:8182":     "http://[::1]:8182",
		"127.0.0.2:8182": "http://127.0.0.2:8182",
	} {
		if got, err := AgentAddress(listen); err != nil || got != want {
			t.Fatalf("%s: %s %v", listen, got, err)
		}
	}
	for _, listen := range []string{"192.168.10.84:8182", "0.0.0.0", "0.0.0.0:0", "0.0.0.0:http"} {
		if _, err := AgentAddress(listen); err == nil {
			t.Fatalf("%s accepted", listen)
		}
	}
}
