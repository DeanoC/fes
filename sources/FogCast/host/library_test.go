package host_test

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/protocol"
)

type recordingTransport struct {
	mu       sync.Mutex
	requests []*http.Request
}

func (t *recordingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.requests = append(t.requests, request.Clone(request.Context()))
	t.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"state":"active","game_id":"snes-test","system":"snes","expected_core":"SNES","observed_core":"SNES","last_error":null}`)),
		Request:    request,
	}, nil
}

func (t *recordingTransport) snapshot() []*http.Request {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]*http.Request(nil), t.requests...)
}

func TestLibraryGamesAndLaunchByID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	manifestPath := filepath.Join(dir, "games.toml")
	config := `base_url = "http://192.0.2.10:8182"
token = "test-token"
request_timeout_seconds = 12
manifest_path = "games.toml"
`
	manifest := `[[games]]
id = "megadrive-test"
title = "Mega Drive test game"
system = "megadrive"
rom_path = "/media/fat/games/MegaDrive/test.md"

[[games]]
id = "snes-test"
title = "SNES test game"
system = "snes"
rom_path = "/media/fat/games/SNES/test.sfc"
`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	transport := &recordingTransport{}
	library, err := host.Open(configPath, &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	games := library.Games()
	if len(games) != 2 {
		t.Fatalf("games = %#v", games)
	}
	games[0].Title = "changed"
	if second := library.Games(); second[0].Title != "Mega Drive test game" {
		t.Fatalf("caller mutation changed library: %#v", second)
	}
	status, err := library.Launch(context.Background(), "snes-test")
	if err != nil || status.State != protocol.StateActive {
		t.Fatalf("launch = %#v, %v", status, err)
	}
	requests := transport.snapshot()
	if len(requests) != 1 || requests[0].URL.Path != "/v1/launch" {
		t.Fatalf("requests = %#v", requests)
	}
	if _, err := library.Launch(context.Background(), "missing-id"); err == nil {
		t.Fatal("unknown game ID launched")
	}
	if requests = transport.snapshot(); len(requests) != 1 {
		t.Fatalf("unknown game caused an HTTP request: %d", len(requests))
	}
}
