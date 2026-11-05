package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/kithost"
)

type scanCompositionService struct {
	compositionService
	scans int
}

func (s *scanCompositionService) Scan(context.Context) (catalog.ScanReport, error) {
	s.scans++
	return catalog.ScanReport{}, nil
}

func stubKitHost(t *testing.T, prepare func(context.Context, kithost.Options) (string, error)) {
	t.Helper()
	previous := prepareKitHost
	prepareKitHost = prepare
	t.Cleanup(func() { prepareKitHost = previous })
}

func runKit(t *testing.T, args []string, svc service) (int, fogcast.Paths, string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var got fogcast.Paths
	var stdout, stderr bytes.Buffer
	code := runWithComposer(ctx, args, &stdout, &stderr,
		func(_ context.Context, paths fogcast.Paths) (service, error) {
			got = paths
			return svc, nil
		},
		func(service, fogcast.Config, bridgeStarterFactory) (http.Handler, func() error, error) {
			cancel()
			return http.NewServeMux(), func() error { return nil }, nil
		},
	)
	return code, got, stderr.String()
}

func TestKitModeOpensTheGeneratedConfigAndScansOnce(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "config.toml")
	if err := os.WriteFile(config, []byte(`selected_target = "kit"
request_timeout_seconds = 12
upload_timeout_seconds = 300
state = "`+filepath.Join(root, "state")+`"
staging = "`+filepath.Join(root, "staging")+`"

[[targets]]
name = "kit"
enabled = true
address = "http://127.0.0.1:8182"
agent = "agent-token"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	var got kithost.Options
	stubKitHost(t, func(_ context.Context, options kithost.Options) (string, error) {
		got = options
		return config, nil
	})
	svc := &scanCompositionService{}
	code, paths, stderr := runKit(t, []string{"-kit", "-kit-state", "/run/fogcast/fesdata3/k-test", "-listen", "127.0.0.1:0", "-headless"}, svc)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if paths.Config != config || !strings.HasPrefix(paths.Index, filepath.Join(root, "state")) {
		t.Fatalf("paths %+v", paths)
	}
	if got.StateRoot != "/run/fogcast/fesdata3/k-test" || got.AgentConfig != kithost.DefaultAgentConfig || got.Template != kithost.DefaultTemplate {
		t.Fatalf("options %+v", got)
	}
	if svc.scans != 1 {
		t.Fatalf("scans = %d, want 1", svc.scans)
	}
}

func TestKitModeRefusesAnExplicitConfig(t *testing.T) {
	stubKitHost(t, func(context.Context, kithost.Options) (string, error) {
		t.Fatal("prepared despite -config")
		return "", nil
	})
	code, _, stderr := runKit(t, []string{"-kit", "-config", "/tmp/other.toml", "-listen", "127.0.0.1:0"}, &scanCompositionService{})
	if code != 2 || !strings.Contains(stderr, "do not pass -config") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
}

func TestKitModeStopsWhenItCannotPrepare(t *testing.T) {
	stubKitHost(t, func(context.Context, kithost.Options) (string, error) {
		return "", errors.New("kit host: agent config /media/fat/fogcast/agent.toml: missing")
	})
	code, _, stderr := runKit(t, []string{"-kit", "-listen", "127.0.0.1:0"}, &scanCompositionService{})
	if code != 1 || !strings.Contains(stderr, "agent.toml") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
}

func TestDesktopModeDoesNotScanAtStart(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "config.toml")
	if err := os.WriteFile(config, []byte("base_url = \"http://127.0.0.1:8182\"\ntoken = \"t\"\nrequest_timeout_seconds = 12\nupload_timeout_seconds = 60\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := &scanCompositionService{}
	if code, _, stderr := runKit(t, []string{"-config", config, "-listen", "127.0.0.1:0", "-headless"}, svc); code != 0 || svc.scans != 0 {
		t.Fatalf("exit %d scans %d: %s", code, svc.scans, stderr)
	}
}

func TestKitModeShutdownLeavesOwnedSessionRunning(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "config.toml")
	if err := os.WriteFile(config, []byte("base_url = \"http://127.0.0.1:8182\"\ntoken = \"t\"\nrequest_timeout_seconds = 12\nupload_timeout_seconds = 60\n"), 0600); err != nil {
		t.Fatal(err)
	}
	stubKitHost(t, func(context.Context, kithost.Options) (string, error) { return config, nil })
	svc := &shutdownCompositionService{shutdownCleanup: true}
	code, _, stderr := runKit(t, []string{"-kit", "-listen", "127.0.0.1:0", "-headless"}, svc)
	if code != 0 || svc.stops != 0 {
		t.Fatalf("exit=%d stops=%d stderr=%s", code, svc.stops, stderr)
	}
}
