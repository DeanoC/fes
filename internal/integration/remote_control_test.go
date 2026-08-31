package integration_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/agentconfig"
	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/mister"
	"github.com/DeanoC/FogCast/internal/version"
	"github.com/DeanoC/FogCast/protocol"
)

type commandWriter struct {
	coreNameFile string
	mu           sync.Mutex
	commands     []string
}

func (w *commandWriter) Write(_ context.Context, command string) error {
	w.mu.Lock()
	w.commands = append(w.commands, command)
	w.mu.Unlock()
	coreName := ""
	if command == "load_core /media/fat/menu.rbf\n" {
		coreName = "MENU"
	} else {
		mglPath := strings.TrimSuffix(strings.TrimPrefix(command, "load_core "), "\n")
		content, err := os.ReadFile(mglPath)
		if err != nil {
			return err
		}
		switch {
		case strings.Contains(string(content), "<rbf>_Console/MegaDrive</rbf>"):
			coreName = "MegaDrive"
		case strings.Contains(string(content), "<rbf>_Console/SNES</rbf>"):
			coreName = "SNES"
		}
	}
	if coreName == "" {
		return errors.New("unrecognized load_core command")
	}
	return os.WriteFile(w.coreNameFile, []byte(coreName+"\n"), 0o600)
}

func (w *commandWriter) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.commands)
}

type runningProcess bool

func (p runningProcess) Running(string) bool {
	return bool(p)
}

func TestRemoteControlEndToEnd(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	megaRoot := filepath.Join(dir, "games", "MegaDrive")
	snesRoot := filepath.Join(dir, "games", "SNES")
	if err := os.MkdirAll(megaRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(snesRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	megaROM := filepath.Join(megaRoot, "test.md")
	snesROM := filepath.Join(snesRoot, "test.sfc")
	if err := os.WriteFile(megaROM, []byte("mega"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snesROM, []byte("snes"), 0o600); err != nil {
		t.Fatal(err)
	}
	defaultRegistry := core.DefaultRegistry()
	megaSpec, _ := defaultRegistry.Lookup(protocol.SystemMegaDrive)
	snesSpec, _ := defaultRegistry.Lookup(protocol.SystemSNES)
	megaSpec.ROMRoot = megaRoot
	snesSpec.ROMRoot = snesRoot
	registry := core.NewRegistry(megaSpec, snesSpec)
	coreNameFile := filepath.Join(dir, "CORENAME")
	commandPipe := filepath.Join(dir, "MiSTer_cmd")
	if err := os.WriteFile(coreNameFile, []byte("MENU\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(commandPipe, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	writer := &commandWriter{coreNameFile: coreNameFile}
	paths := mister.Paths{MiSTerProcessComm: "MiSTer", CommandPipe: commandPipe, CoreNameFile: coreNameFile, MenuRBF: "/media/fat/menu.rbf", MGLDirectory: filepath.Join(dir, "mgl")}
	runtime := mister.NewRuntime(paths, registry, writer, runningProcess(true), time.Millisecond)
	coordinator := agent.New(runtime, registry, time.Second, time.Second)
	coordinator.Initialize(context.Background())
	handler := httpapi.New(coordinator, "test-token", version.Version, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	server := httptest.NewServer(handler)
	defer server.Close()
	configPath := filepath.Join(dir, "config.toml")
	manifestPath := filepath.Join(dir, "games.toml")
	config := "base_url = \"" + server.URL + "\"\ntoken = \"test-token\"\nrequest_timeout_seconds = 12\nmanifest_path = \"games.toml\"\n"
	manifest := "[[games]]\nid=\"megadrive-test\"\ntitle=\"Mega Drive test\"\nsystem=\"megadrive\"\nrom_path=\"" + megaROM + "\"\n[[games]]\nid=\"snes-test\"\ntitle=\"SNES test\"\nsystem=\"snes\"\nrom_path=\"" + snesROM + "\"\n"
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	library, err := host.Open(configPath, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	health, err := library.Health(ctx)
	if err != nil || !health.Ready || health.AgentVersion != version.Version {
		t.Fatalf("health = %#v, %v", health, err)
	}
	status, err := library.Launch(ctx, "megadrive-test")
	if err != nil || status.State != protocol.StateActive || status.ObservedCore == nil || *status.ObservedCore != "MegaDrive" {
		t.Fatalf("Mega Drive launch = %#v, %v", status, err)
	}
	status, err = library.Launch(ctx, "snes-test")
	if err != nil || status.ObservedCore == nil || *status.ObservedCore != "SNES" {
		t.Fatalf("SNES launch = %#v, %v", status, err)
	}
	baseURL, _ := url.Parse(server.URL)
	badTokenClient := host.NewClient(baseURL, "wrong-token", server.Client())
	if _, err := badTokenClient.Status(ctx); apiErrorCode(err) != protocol.CodeUnauthorized {
		t.Fatalf("wrong-token error = %#v", err)
	}
	client := host.NewClient(baseURL, "test-token", server.Client())
	outsideROM := filepath.Join(dir, "outside.sfc")
	if err := os.WriteFile(outsideROM, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	invalidRequests := []struct {
		request protocol.LaunchRequest
		code    protocol.ErrorCode
	}{
		{request: protocol.LaunchRequest{GameID: "unknown-test", System: "mystery", ROMPath: snesROM}, code: protocol.CodeUnsupportedSystem},
		{request: protocol.LaunchRequest{GameID: "missing-rom", System: protocol.SystemSNES, ROMPath: filepath.Join(snesRoot, "missing.sfc")}, code: protocol.CodeROMNotFound},
		{request: protocol.LaunchRequest{GameID: "escaped-rom", System: protocol.SystemSNES, ROMPath: outsideROM}, code: protocol.CodeInvalidROMPath},
	}
	for _, check := range invalidRequests {
		if _, err := client.Launch(ctx, check.request); apiErrorCode(err) != check.code {
			t.Fatalf("launch %#v error = %#v, want %s", check.request, err, check.code)
		}
		status, err := library.Status(ctx)
		if err != nil || status.ObservedCore == nil || *status.ObservedCore != "SNES" {
			t.Fatalf("status after invalid request = %#v, %v", status, err)
		}
	}
	if writer.count() != 2 {
		t.Fatalf("invalid requests emitted commands; command count = %d", writer.count())
	}
	restarted := agent.New(runtime, registry, time.Second, time.Second)
	restarted.Initialize(context.Background())
	restartedStatus := restarted.Status()
	if restartedStatus.State != protocol.StateActive || restartedStatus.System == nil || *restartedStatus.System != protocol.SystemSNES || restartedStatus.GameID != nil {
		t.Fatalf("restart status = %#v", restartedStatus)
	}
	if writer.count() != 2 {
		t.Fatalf("reconciliation emitted a command; command count = %d", writer.count())
	}
	status, err = library.Stop(ctx)
	if err != nil || status.State != protocol.StateIdle {
		t.Fatalf("stop = %#v, %v", status, err)
	}
	if writer.count() != 3 {
		t.Fatalf("command count after stop = %d", writer.count())
	}
	assertRegistryCannotBeConfigured(t, dir)
}

func apiErrorCode(err error) protocol.ErrorCode {
	var apiErr *protocol.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}
	return ""
}

func assertRegistryCannotBeConfigured(t *testing.T, dir string) {
	t.Helper()
	content := `listen_address = "0.0.0.0:8182"
token = "test-token"
mister_process_comm = "MiSTer"
command_pipe = "/dev/MiSTer_cmd"
core_name_file = "/tmp/CORENAME"
menu_rbf = "/media/fat/menu.rbf"
mgl_directory = "/tmp/mister-remote"
cores = []
`
	path := filepath.Join(dir, "agent-with-cores.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := agentconfig.Load(path); err == nil {
		t.Fatal("target configuration accepted a cores override")
	}
}
