package integration_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/internal/mister"
	"github.com/DeanoC/FogCast/internal/targetcache"
	"github.com/DeanoC/FogCast/internal/version"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

func TestFogCastContentEndToEnd(t *testing.T) {
	dir := t.TempDir()
	snesRoot := filepath.Join(dir, "nas", "SNES")
	megaRoot := filepath.Join(dir, "nas", "MegaDrive")
	v1Root := filepath.Join(dir, "target-v1", "SNES")
	for _, directory := range []string{snesRoot, megaRoot, v1Root} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	cachedSNESBytes := bytes.Repeat([]byte("synthetic-snes-cache\x00"), 1024)
	uncachedSNESBytes := []byte("synthetic-snes-offline-miss")
	interruptedMegaBytes := bytes.Repeat([]byte("synthetic-megadrive-upload\x00"), 2048)
	writeSyntheticZIP(t, filepath.Join(snesRoot, "cached-game.zip"), "inside/CACHED.SFC", cachedSNESBytes)
	writeFile(t, filepath.Join(snesRoot, "uncached-game.sfc"), uncachedSNESBytes)
	writeFile(t, filepath.Join(megaRoot, "retry-game.md"), interruptedMegaBytes)
	v1ROM := filepath.Join(v1Root, "legacy.sfc")
	writeFile(t, v1ROM, []byte("synthetic-v1-rom"))

	registry := integrationRegistry(t, v1Root)
	coreNameFile := filepath.Join(dir, "CORENAME")
	commandPipe := filepath.Join(dir, "MiSTer_cmd")
	writeFile(t, coreNameFile, []byte("MENU\n"))
	writeFile(t, commandPipe, nil)
	writer := &commandWriter{coreNameFile: coreNameFile}
	runtimePaths := mister.Paths{
		MiSTerProcessComm: "MiSTer",
		CommandPipe:       commandPipe,
		CoreNameFile:      coreNameFile,
		MenuRBF:           "/media/fat/menu.rbf",
		MGLDirectory:      filepath.Join(dir, "mgl"),
	}
	runtime := mister.NewRuntime(runtimePaths, registry, writer, runningProcess(true), time.Millisecond)
	cacheConfig := targetcache.Config{
		Root:         filepath.Join(dir, "target-cache"),
		ActiveRecord: filepath.Join(dir, "run", "fogcast-active.json"),
		MaxBytes:     4 * protocol.MaxContentBytes,
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	manager, err := targetcache.Open(cacheConfig, registry, targetcache.WithLogger(logger))
	if err != nil {
		t.Fatal(err)
	}
	firstObserver := &uploadObserver{}
	firstServer, firstCoordinator := startContentServer(t, runtime, registry, manager, firstObserver, logger)

	hostConfig := filepath.Join(dir, "fogcast.toml")
	paths := fogcast.Paths{
		Config:  hostConfig,
		Index:   filepath.Join(dir, "host-state", "library.sqlite3"),
		Staging: filepath.Join(dir, "host-staging"),
	}
	writeFogCastIntegrationConfig(t, hostConfig, firstServer.URL, snesRoot, megaRoot)
	interrupt := &interruptUploadTransport{base: firstServer.Client().Transport, observer: firstObserver}
	httpClient := &http.Client{Transport: interrupt}
	service, err := fogcast.Open(context.Background(), paths, httpClient)
	if err != nil {
		t.Fatal(err)
	}

	report, err := service.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Roots) != 2 || report.Roots[0].Added != 2 || report.Roots[1].Added != 1 {
		t.Fatalf("initial scan = %#v", report)
	}
	games, err := service.Games(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cachedSNES := integrationGame(t, games, "cached-game.zip")
	uncachedSNES := integrationGame(t, games, "uncached-game.sfc")
	interruptMega := integrationGame(t, games, "retry-game.md")

	firstLaunch, err := service.Launch(context.Background(), cachedSNES.ID, nil)
	if err != nil || firstLaunch.Status.State != protocol.StateActive || firstLaunch.Content.Size != int64(len(cachedSNESBytes)) {
		t.Fatalf("first cached launch = %#v, %v", firstLaunch, err)
	}
	firstRequests, firstBytes := firstObserver.snapshot()
	if firstRequests != 1 || firstBytes != int64(len(cachedSNESBytes)) {
		t.Fatalf("first upload requests=%d bytes=%d", firstRequests, firstBytes)
	}
	if manager.Usage() != int64(len(cachedSNESBytes)) {
		t.Fatalf("cache usage = %d, want %d", manager.Usage(), len(cachedSNESBytes))
	}

	secondLaunch, err := service.Launch(context.Background(), cachedSNES.ID, nil)
	if err != nil || secondLaunch.Content != firstLaunch.Content {
		t.Fatalf("second cached launch = %#v, %v", secondLaunch, err)
	}
	secondRequests, secondBytes := firstObserver.snapshot()
	if secondRequests != firstRequests || secondBytes != firstBytes {
		t.Fatalf("cache hit uploaded again: requests %d->%d bytes %d->%d", firstRequests, secondRequests, firstBytes, secondBytes)
	}

	// The interrupt transport waits for the target to consume a partial body
	// before failing. The assertions below prove partial consumption, transfer
	// failure, no final publication or .part residue, and a successful retry;
	// Task 8 covers cancellation after a real .part exists.
	interrupt.arm()
	_, err = service.Launch(context.Background(), interruptMega.ID, nil)
	if apiErrorCode(err) != protocol.CodeTransferFailed {
		t.Fatalf("interrupted upload error = %#v", err)
	}
	waitForNoUploadParts(t, cacheConfig.Root)
	requestsAfterInterrupt, bytesAfterInterrupt := firstObserver.snapshot()
	if requestsAfterInterrupt != secondRequests+1 || bytesAfterInterrupt <= secondBytes || bytesAfterInterrupt >= secondBytes+int64(len(interruptedMegaBytes)) {
		t.Fatalf("interrupted upload requests=%d bytes=%d before=%d/%d", requestsAfterInterrupt, bytesAfterInterrupt, secondRequests, secondBytes)
	}

	retriedLaunch, err := service.Launch(context.Background(), interruptMega.ID, nil)
	if err != nil || retriedLaunch.Status.State != protocol.StateActive || retriedLaunch.Content.Size != int64(len(interruptedMegaBytes)) {
		t.Fatalf("retried launch = %#v, %v", retriedLaunch, err)
	}
	waitForNoUploadParts(t, cacheConfig.Root)
	retryRequests, retryBytes := firstObserver.snapshot()
	if retryRequests != requestsAfterInterrupt+1 || retryBytes != bytesAfterInterrupt+int64(len(interruptedMegaBytes)) {
		t.Fatalf("retry upload requests=%d bytes=%d", retryRequests, retryBytes)
	}

	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	firstServer.Close()

	reconstructed, err := targetcache.Open(cacheConfig, registry, targetcache.WithLogger(logger))
	if err != nil {
		t.Fatal(err)
	}
	secondObserver := &uploadObserver{}
	secondServer, secondCoordinator := startContentServer(t, runtime, registry, reconstructed, secondObserver, logger)
	defer secondServer.Close()
	// Closing the host releases its lease and restores idle; the reconstructed
	// cache must still serve the next launch without another upload.
	if secondCoordinator.Status().State != protocol.StateIdle {
		t.Fatalf("reconstructed status = %#v", secondCoordinator.Status())
	}
	writeFogCastIntegrationConfig(t, hostConfig, secondServer.URL, snesRoot, megaRoot)
	service, err = fogcast.Open(context.Background(), paths, secondServer.Client())
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	reconstructedLaunch, err := service.Launch(context.Background(), interruptMega.ID, nil)
	if err != nil || reconstructedLaunch.Status.State != protocol.StateActive {
		t.Fatalf("reconstructed cache launch = %#v, %v", reconstructedLaunch, err)
	}
	if requests, uploaded := secondObserver.snapshot(); requests != 0 || uploaded != 0 {
		t.Fatalf("reconstructed hit uploaded requests=%d bytes=%d", requests, uploaded)
	}

	offlineRoot := snesRoot + "-offline"
	if err := os.Rename(snesRoot, offlineRoot); err != nil {
		t.Fatal(err)
	}
	offlineReport, err := service.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(offlineReport.Roots) != 2 || !offlineReport.Roots[0].Offline {
		t.Fatalf("offline scan = %#v", offlineReport)
	}
	offlineCachedLaunch, err := service.Launch(context.Background(), cachedSNES.ID, nil)
	if err != nil || offlineCachedLaunch.Status.State != protocol.StateActive || offlineCachedLaunch.Status.System == nil || *offlineCachedLaunch.Status.System != protocol.SystemSNES {
		t.Fatalf("offline cached launch = %#v, %v", offlineCachedLaunch, err)
	}
	commandsBeforeMiss := writer.count()
	_, err = service.Launch(context.Background(), uncachedSNES.ID, nil)
	if apiErrorCode(err) != protocol.CodeSourceUnavailable {
		t.Fatalf("offline uncached launch error = %#v", err)
	}
	status, err := service.Status(context.Background())
	if err != nil || status.State != protocol.StateActive || status.GameID == nil || *status.GameID != cachedSNES.ID || status.ObservedCore == nil || *status.ObservedCore != "SNES" {
		t.Fatalf("status after offline miss = %#v, %v", status, err)
	}
	if writer.count() != commandsBeforeMiss {
		t.Fatalf("offline miss changed active core: commands %d -> %d", commandsBeforeMiss, writer.count())
	}

	baseURL, err := url.Parse(secondServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	// A different application must not replace the current owner.
	v1Lease := targetclient.NewKitLease(baseURL, "test-token", secondServer.Client(), "integration-v1", "legacy launch")
	defer v1Lease.Close(context.Background())
	v1Client := targetclient.NewClient(baseURL, "test-token", secondServer.Client()).WithKitLease(v1Lease)
	commandsBeforeForeignLaunch := writer.count()
	if _, err := v1Client.Launch(context.Background(), protocol.LaunchRequest{GameID: "snes-legacy", System: protocol.SystemSNES, ROMPath: v1ROM}); err == nil {
		t.Fatal("foreign application replaced the current kit owner")
	}
	if writer.count() != commandsBeforeForeignLaunch {
		t.Fatal("foreign lease claim mutated the core")
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	v1Status, err := v1Client.Launch(context.Background(), protocol.LaunchRequest{GameID: "snes-legacy", System: protocol.SystemSNES, ROMPath: v1ROM})
	if err != nil || v1Status.State != protocol.StateActive || v1Status.ObservedCore == nil || *v1Status.ObservedCore != "SNES" {
		t.Fatalf("v1 launch = %#v, %v", v1Status, err)
	}
	v1Status, err = v1Client.Stop(context.Background())
	if err != nil || v1Status.State != protocol.StateIdle {
		t.Fatalf("v1 stop = %#v, %v", v1Status, err)
	}
	if firstCoordinator.Status().State != protocol.StateIdle {
		t.Fatalf("first agent coordinator unexpectedly mutated after reconstruction: %#v", firstCoordinator.Status())
	}
}

func integrationRegistry(t *testing.T, v1SNESRoot string) core.Registry {
	t.Helper()
	defaults := core.DefaultRegistry()
	mega, ok := defaults.Lookup(protocol.SystemMegaDrive)
	if !ok {
		t.Fatal("Mega Drive registry entry missing")
	}
	snes, ok := defaults.Lookup(protocol.SystemSNES)
	if !ok {
		t.Fatal("SNES registry entry missing")
	}
	mega.ROMRoot = filepath.Join(filepath.Dir(v1SNESRoot), "MegaDrive")
	if err := os.MkdirAll(mega.ROMRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	snes.ROMRoot = v1SNESRoot
	return core.NewRegistry(mega, snes)
}

func startContentServer(t *testing.T, runtime agent.Runtime, registry core.Registry, store agent.ContentStore, observer *uploadObserver, logger *slog.Logger) (*httptest.Server, *agent.Coordinator) {
	t.Helper()
	coordinator := agent.New(runtime, registry, time.Second, time.Second)
	content := agent.NewContentController(coordinator, store)
	coordinator.Initialize(context.Background())
	leases := kitlease.New(90*time.Second, func(ctx context.Context) error {
		status, err := coordinator.Stop(ctx)
		if err != nil {
			return err
		}
		if status.State != protocol.StateIdle {
			return errors.New("kit did not reach idle")
		}
		return nil
	})
	t.Cleanup(leases.Close)
	handler := httpapi.New(coordinator, "test-token", version.Version, logger, httpapi.WithContent(content), httpapi.WithKitLease(leases))
	return httptest.NewServer(observer.wrap(handler)), coordinator
}

func writeFogCastIntegrationConfig(t *testing.T, path, baseURL, snesRoot, megaRoot string) {
	t.Helper()
	content := `base_url = "` + baseURL + `"
token = "test-token"
request_timeout_seconds = 2
upload_timeout_seconds = 5

[[libraries]]
id = "snes-main"
system = "snes"
root = "` + snesRoot + `"

[[libraries]]
id = "megadrive-main"
system = "megadrive"
root = "` + megaRoot + `"
`
	writeFile(t, path, []byte(content))
}

func integrationGame(t *testing.T, games []catalog.Game, relativePath string) catalog.Game {
	t.Helper()
	for _, game := range games {
		if game.RelativePath == relativePath {
			return game
		}
	}
	t.Fatalf("game %q missing from %#v", relativePath, games)
	return catalog.Game{}
}

func writeSyntheticZIP(t *testing.T, path, member string, data []byte) {
	t.Helper()
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	entry, err := writer.Create(member)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(data); err != nil {
		t.Fatal(err)
	}
	readme, err := writer.Create("README.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readme.Write([]byte("synthetic integration fixture")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, archive.Bytes())
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

type uploadObserver struct {
	mu       sync.Mutex
	requests int
	bytes    int64
}

func (o *uploadObserver) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPut && strings.HasPrefix(request.URL.Path, "/v2/cache/") {
			o.mu.Lock()
			o.requests++
			o.mu.Unlock()
			request.Body = &observedBody{ReadCloser: request.Body, observer: o}
		}
		next.ServeHTTP(response, request)
	})
}

func (o *uploadObserver) snapshot() (int, int64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.requests, o.bytes
}

type observedBody struct {
	io.ReadCloser
	observer *uploadObserver
}

func (b *observedBody) Read(buffer []byte) (int, error) {
	count, err := b.ReadCloser.Read(buffer)
	b.observer.mu.Lock()
	b.observer.bytes += int64(count)
	b.observer.mu.Unlock()
	return count, err
}

type interruptUploadTransport struct {
	base          http.RoundTripper
	observer      *uploadObserver
	mu            sync.Mutex
	armed         bool
	observedBytes int64
}

func (t *interruptUploadTransport) arm() {
	t.mu.Lock()
	t.armed = true
	_, t.observedBytes = t.observer.snapshot()
	t.mu.Unlock()
}

func (t *interruptUploadTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t.mu.Lock()
	interrupt := t.armed && request.Method == http.MethodPut
	if interrupt {
		t.armed = false
	}
	t.mu.Unlock()
	if !interrupt {
		return t.base.RoundTrip(request)
	}
	copy := request.Clone(request.Context())
	copy.Body = &interruptedUploadBody{source: request.Body, remaining: 16 << 10, observer: t.observer, observedBytes: t.observedBytes}
	copy.GetBody = nil
	return t.base.RoundTrip(copy)
}

type interruptedUploadBody struct {
	source        io.ReadCloser
	remaining     int
	failed        bool
	observer      *uploadObserver
	observedBytes int64
}

func (b *interruptedUploadBody) Read(buffer []byte) (int, error) {
	if b.failed || b.remaining == 0 {
		b.failed = true
		deadline := time.Now().Add(time.Second)
		for {
			_, observed := b.observer.snapshot()
			if observed > b.observedBytes || time.Now().After(deadline) {
				break
			}
			time.Sleep(time.Millisecond)
		}
		return 0, errors.New("synthetic interrupted upload")
	}
	if len(buffer) > b.remaining {
		buffer = buffer[:b.remaining]
	}
	count, err := b.source.Read(buffer)
	b.remaining -= count
	if err != nil {
		return count, err
	}
	return count, nil
}

func (b *interruptedUploadBody) Close() error { return b.source.Close() }

func waitForNoUploadParts(t *testing.T, root string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		found := false
		for _, system := range []protocol.System{protocol.SystemMegaDrive, protocol.SystemSNES} {
			entries, err := os.ReadDir(filepath.Join(root, string(system)))
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".fogcast-upload-") && strings.HasSuffix(entry.Name(), ".part") {
					found = true
				}
			}
		}
		if !found {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("interrupted upload left a .part artifact; this fixture expects no artifact or publication")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
