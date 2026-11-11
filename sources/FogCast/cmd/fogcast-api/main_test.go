package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/metadata"
	"github.com/DeanoC/FogCast/internal/remotemedia"
	"github.com/DeanoC/FogCast/kitlease"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

func serveComposition(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Host = "127.0.0.1"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestRunRejectsNonLoopbackListenAddress(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"--listen", "0.0.0.0:8787"}, &stdout, &stderr, nil)
	if code != 2 {
		t.Fatalf("exit = %d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "loopback") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunReportsGenericCompositionFailureWithoutLeakingError(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	config := `base_url = "http://127.0.0.1:8182"
token = "test-token"
request_timeout_seconds = 12
upload_timeout_seconds = 60

[[libraries]]
id = "test"
system = "snes"
root = "` + dir + `"
`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	wantErr := errors.New("metadata opener secret=must-not-leak")
	code := runWithComposer(context.Background(), []string{"--config", configPath}, &stderr, &stderr,
		func(context.Context, fogcast.Paths) (service, error) { return &compositionService{}, nil },
		func(service service, config fogcast.Config, makeStarter bridgeStarterFactory) (http.Handler, func() error, error) {
			return composeAPI(service, config, makeStarter, withMetadataOpener(func(context.Context, metadata.RuntimeConfig) (metadata.Runtime, error) {
				return nil, wantErr
			}))
		},
	)
	if code != 1 {
		t.Fatalf("exit = %d, want 1; stderr=%q", code, stderr.String())
	}
	if got := stderr.String(); !strings.Contains(got, "fogcast-api: API composition failed") || strings.Contains(got, wantErr.Error()) {
		t.Fatalf("stderr = %q", got)
	}
}

func TestExplicitConfigKeepsCatalogStoresPrivate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	state := filepath.Join(dir, "private-state")
	staging := filepath.Join(dir, "private-staging")
	configPath := filepath.Join(dir, "host.toml")
	config := fmt.Sprintf(`base_url = "http://127.0.0.1:8182"
token = "test-token"
request_timeout_seconds = 12
upload_timeout_seconds = 60
state = %q
staging = %q
`, state, staging)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	got := captureAPIPaths(t, []string{"--config", configPath, "--headless", "--listen", "127.0.0.1:0"})
	want := fogcast.Paths{
		Config:          configPath,
		Index:           filepath.Join(state, "library.sqlite3"),
		Staging:         staging,
		MetadataRoot:    filepath.Join(dir, "metadata"),
		UserLibrary:     filepath.Join(state, "library-user.sqlite3"),
		LibrarySettings: filepath.Join(state, "library-settings.json"),
		MediaIndex:      filepath.Join(state, "library-media.sqlite3"),
		MediaCache:      filepath.Join(dir, "media-cache"),
		CorePackages:    filepath.Join(state, "core-packages"),
	}
	if got != want {
		t.Fatalf("paths = %+v, want %+v", got, want)
	}
	if _, err := os.Stat(state); err != nil {
		t.Fatalf("private state: %v", err)
	}
	if _, err := os.Stat(staging); err != nil {
		t.Fatalf("private staging: %v", err)
	}
	for _, path := range []string{
		filepath.Join(home, ".local", "share", "fogcast"),
		filepath.Join(home, ".cache", "fogcast")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("home catalog path %q = %v", path, err)
		}
	}
}

func TestExplicitRelativeConfigUsesAbsoluteCatalogPaths(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	configPath := filepath.Join(dir, "host.toml")
	config := `base_url = "http://127.0.0.1:8182"
token = "test-token"
request_timeout_seconds = 12
upload_timeout_seconds = 60
`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	got := captureAPIPaths(t, []string{"--config", "host.toml", "--headless", "--listen", "127.0.0.1:0"})
	state := filepath.Join(dir, "state")
	want := fogcast.Paths{
		Config:          configPath,
		Index:           filepath.Join(state, "library.sqlite3"),
		Staging:         filepath.Join(dir, "staging"),
		MetadataRoot:    filepath.Join(dir, "metadata"),
		UserLibrary:     filepath.Join(state, "library-user.sqlite3"),
		LibrarySettings: filepath.Join(state, "library-settings.json"),
		MediaIndex:      filepath.Join(state, "library-media.sqlite3"),
		MediaCache:      filepath.Join(dir, "media-cache"),
		CorePackages:    filepath.Join(state, "core-packages"),
	}
	if got != want {
		t.Fatalf("paths = %+v, want %+v", got, want)
	}
}

func TestOmittedConfigUsesDefaultPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	want, err := fogcast.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(want.Config), 0o700); err != nil {
		t.Fatal(err)
	}
	config := `base_url = "http://127.0.0.1:8182"
token = "test-token"
request_timeout_seconds = 12
upload_timeout_seconds = 60
`
	if err := os.WriteFile(want.Config, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	got := captureAPIPaths(t, []string{"--headless", "--listen", "127.0.0.1:0"})
	if got != want {
		t.Fatalf("paths = %+v, want default paths %+v", got, want)
	}
}

func captureAPIPaths(t *testing.T, args []string) fogcast.Paths {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var got fogcast.Paths
	var stdout, stderr bytes.Buffer
	code := runWithComposer(ctx, args, &stdout, &stderr,
		func(_ context.Context, paths fogcast.Paths) (service, error) {
			got = paths
			cancel()
			return &compositionService{}, nil
		},
		func(service, fogcast.Config, bridgeStarterFactory) (http.Handler, func() error, error) {
			return http.NewServeMux(), func() error { return nil }, nil
		},
	)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%q", code, stderr.String())
	}
	return got
}

func TestLoadAPIConfigCanSourceMetadataFromSeparatePrivateConfig(t *testing.T) {
	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "launch.toml")
	metadataPath := filepath.Join(dir, "metadata.toml")
	primary := `base_url = "http://127.0.0.1:8182"
token = "launch-token"
request_timeout_seconds = 12
upload_timeout_seconds = 60

[[libraries]]
id = "test"
system = "snes"
root = "` + dir + `"
`
	secondary := `[metadata]
enabled = true
provider = "launchbox"
archive = "` + filepath.Join(dir, "Metadata.zip") + `"
`
	if err := os.WriteFile(primaryPath, []byte(primary), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metadataPath, []byte(secondary), 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := loadAPIConfig(primaryPath, metadataPath)
	if err != nil {
		t.Fatalf("loadAPIConfig: %v", err)
	}
	if config.Token != "launch-token" || len(config.Libraries) != 1 || config.Libraries[0].ID != "test" {
		t.Fatalf("primary launch config changed: %#v", config)
	}
	if !config.Metadata.Configured || !config.Metadata.Enabled || config.Metadata.Provider != "launchbox" || config.Metadata.Archive != filepath.Join(dir, "Metadata.zip") {
		t.Fatalf("metadata config = %#v", config.Metadata)
	}
}

func TestLoadAPIConfigPreservesPrimaryMetadataWithoutOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	configText := `base_url = "http://127.0.0.1:8182"
token = "launch-token"
request_timeout_seconds = 12
upload_timeout_seconds = 60

[metadata]
enabled = false
provider = "launchbox"
`
	if err := os.WriteFile(path, []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := loadAPIConfig(path, "")
	if err != nil {
		t.Fatalf("loadAPIConfig: %v", err)
	}
	if !config.Metadata.Configured || config.Metadata.Enabled || config.Metadata.Provider != "launchbox" {
		t.Fatalf("metadata config = %#v", config.Metadata)
	}
}

func TestLoadAPIConfigRejectsOverrideWithoutMetadataSection(t *testing.T) {
	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "config.toml")
	overridePath := filepath.Join(dir, "metadata.toml")
	primary := `base_url = "http://127.0.0.1:8182"
token = "launch-token"
request_timeout_seconds = 12
upload_timeout_seconds = 60

[metadata]
enabled = false
provider = "launchbox"
`
	if err := os.WriteFile(primaryPath, []byte(primary), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overridePath, []byte(`token = "not-metadata"
`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := loadAPIConfig(primaryPath, overridePath); err == nil || !strings.Contains(err.Error(), "metadata section is required") {
		t.Fatalf("loadAPIConfig error = %v", err)
	}
}

func TestNormalizeListenAddressAcceptsLoopbackForms(t *testing.T) {
	for _, address := range []string{"127.0.0.1:8787", "localhost:8787", "[::1]:8787"} {
		if got, err := normalizeListenAddress(address); err != nil || got == "" {
			t.Errorf("normalize %q = %q, %v", address, got, err)
		}
	}
}

type compositionService struct{}

func (*compositionService) Games(context.Context) ([]catalog.Game, error) { return nil, nil }
func (*compositionService) Search(context.Context, string) ([]catalog.Game, error) {
	return nil, nil
}
func (*compositionService) Game(context.Context, string) (catalog.Game, error) {
	return catalog.Game{}, nil
}
func (*compositionService) Health(context.Context) (protocol.Health, error) {
	return protocol.Health{}, nil
}
func (*compositionService) Status(context.Context) (protocol.Status, error) {
	return protocol.Status{State: protocol.StateIdle}, nil
}
func (*compositionService) Launch(context.Context, string, fogcast.ProgressFunc) (protocol.CachedLaunchResponse, error) {
	return protocol.CachedLaunchResponse{}, nil
}
func (*compositionService) LoadDevelopmentRBF(context.Context, int64, io.Reader) (protocol.Status, error) {
	return protocol.Status{State: protocol.StateActive, Development: true}, nil
}
func (*compositionService) Stop(context.Context) (protocol.Status, error) {
	return protocol.Status{State: protocol.StateIdle}, nil
}
func (*compositionService) MeshBackendLibrary(context.Context) ([]fogcast.MeshBackendRow, []fogcast.MeshSkip) {
	return nil, nil
}
func (*compositionService) Close() error { return nil }

type shutdownCompositionService struct {
	compositionService
	mu              sync.Mutex
	stopErrs        []error
	stops           int
	shutdownCleanup bool
}

type folderWatchCompositionService struct {
	compositionService
	watchErr          error
	watchRelease      <-chan struct{}
	reconcileFailures int
}

func (s *folderWatchCompositionService) RunFolderWatch(ctx context.Context) error {
	if s.watchErr != nil {
		return s.watchErr
	}
	if s.watchRelease != nil {
		<-s.watchRelease
		return nil
	}
	<-ctx.Done()
	return ctx.Err()
}

func (s *folderWatchCompositionService) FolderWatchReconcileFailures() int {
	return s.reconcileFailures
}

type folderWatchLogWriter chan string

func (w folderWatchLogWriter) Write(p []byte) (int, error) {
	w <- string(p)
	return len(p), nil
}

func (s *shutdownCompositionService) Stop(context.Context) (protocol.Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stops++
	if len(s.stopErrs) == 0 {
		s.shutdownCleanup = false
		return protocol.Status{State: protocol.StateIdle}, nil
	}
	err := s.stopErrs[0]
	s.stopErrs = s.stopErrs[1:]
	if err == nil {
		s.shutdownCleanup = false
	}
	return protocol.Status{State: protocol.StateIdle}, err
}

func (s *shutdownCompositionService) ShutdownCleanupRequired() bool {
	return s.shutdownCleanup
}

func TestComposeAPIWiresRemoteInputController(t *testing.T) {
	service := &compositionService{}
	called := false
	handler, closeRemote, err := composeAPI(service, fogcast.Config{BaseURL: "http://127.0.0.1:8182", Token: "test-token", RemoteInput: fogcast.RemoteInputConfig{Enabled: true}}, func(fogcast.Config) (host.BridgeStarter, error) {
		called = true
		return host.BridgeStarterFunc(func(context.Context, host.BridgeSpec) (host.BridgeHandle, error) {
			return nil, errors.New("unused")
		}), nil
	})
	if err != nil || handler == nil || closeRemote == nil || !called {
		t.Fatalf("composition = handler:%v close:%v called:%v err:%v", handler != nil, closeRemote != nil, called, err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/session/input", nil)
	request.Host = "127.0.0.1"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"state":"detached"`) {
		t.Fatalf("input route = %d %s", response.Code, response.Body.String())
	}
	if err := closeRemote(); err != nil {
		t.Fatal(err)
	}
}

type targetBridgeTestService struct {
	compositionService
	active  string
	configs map[string]fogcast.TargetConfig
	leases  map[string]*targetclient.KitLease
	lookups []string
}

func (s *targetBridgeTestService) TargetConfigForName(name string) fogcast.TargetConfig {
	return s.configs[name]
}
func (s *targetBridgeTestService) KitLeaseForTarget(name string) *targetclient.KitLease {
	s.lookups = append(s.lookups, name)
	return s.leases[name]
}
func (*targetBridgeTestService) Close() error { return nil }

func TestPerTargetBridgeKeepsOriginAndLeaseAfterAnotherTargetLaunches(t *testing.T) {
	var aAuth string
	kitA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		aAuth = r.Header.Get("Authorization")
		if r.URL.Path == "/v1/kit/claim" {
			_ = json.NewEncoder(w).Encode(kitlease.Grant{Token: "grant-a", Status: kitlease.Status{State: "held", Generation: "generation-a", ExpiresInMS: 60000}})
			return
		}
		if r.Header.Get(targetclient.KitLeaseHeader) != "grant-a" {
			t.Errorf("A bridge lease header = %q", r.Header.Get(targetclient.KitLeaseHeader))
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ready": true})
	}))
	defer kitA.Close()
	kitB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("A bridge was redirected to kit B")
		_ = json.NewEncoder(w).Encode(map[string]bool{"ready": true})
	}))
	defer kitB.Close()
	aURL, _ := url.Parse(kitA.URL)
	bURL, _ := url.Parse(kitB.URL)
	service := &targetBridgeTestService{
		active: "kit-a",
		configs: map[string]fogcast.TargetConfig{
			"kit-a": {Name: "kit-a", Address: kitA.URL, Agent: "agent-a"},
			"kit-b": {Name: "kit-b", Address: kitB.URL, Agent: "agent-b"},
		},
		leases: map[string]*targetclient.KitLease{
			"kit-a": targetclient.NewKitLease(aURL, "agent-a", nil, "host", "input"),
			"kit-b": targetclient.NewKitLease(bURL, "agent-b", nil, "host", "input"),
		},
	}
	factory := func(config fogcast.Config) (host.BridgeStarter, error) {
		base, err := url.Parse(config.BaseURL)
		if err != nil {
			return nil, err
		}
		return host.NewHTTPBridgeStarter(host.HTTPBridgeStarterConfig{BaseURL: base, Token: config.Token})
	}
	starter, err := makeTargetBridgeStarter(fogcast.Config{}, "kit-a", service, factory, nil)
	if err != nil {
		t.Fatal(err)
	}
	service.active = "kit-b" // B launched after A's controller was created.
	handle, err := starter.Start(context.Background(), host.BridgeSpec{Session: 9, Token: []byte("0123456789abcdef"), Core: "SNES"})
	if err != nil {
		t.Fatalf("A bridge attach after B launch: %v", err)
	}
	if aAuth != "Bearer agent-a" {
		t.Fatalf("A bridge authorization = %q", aAuth)
	}
	if len(service.lookups) == 0 || service.lookups[len(service.lookups)-1] != "kit-a" {
		t.Fatalf("bridge lease lookups after B launch = %v", service.lookups)
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("A bridge detach after B launch: %v", err)
	}
	service.leases["kit-a"].Close(context.Background())
	service.leases["kit-b"].Close(context.Background())
}

type compositionPreviewCapture struct {
	started chan struct{}
	closed  chan struct{}
	samples chan remotemedia.EncodedSample
	once    sync.Once
}

func (c *compositionPreviewCapture) Start() error { c.once.Do(func() { close(c.started) }); return nil }
func (c *compositionPreviewCapture) Next(ctx context.Context) (remotemedia.EncodedSample, error) {
	select {
	case sample := <-c.samples:
		return sample, nil
	case <-ctx.Done():
		return remotemedia.EncodedSample{}, ctx.Err()
	case <-c.closed:
		return remotemedia.EncodedSample{}, context.Canceled
	}
}
func (*compositionPreviewCapture) Stats() remotemedia.CaptureStats { return remotemedia.CaptureStats{} }
func (c *compositionPreviewCapture) Close() error {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	return nil
}

type compositionPreviewDecoder struct {
	started int
	done    chan struct{}
	writes  chan []byte
	once    sync.Once
}

func (d *compositionPreviewDecoder) Start() error { d.started++; return nil }
func (d *compositionPreviewDecoder) Write(p []byte) (int, error) {
	d.writes <- append([]byte(nil), p...)
	return len(p), nil
}
func (d *compositionPreviewDecoder) Close() error { d.once.Do(func() { close(d.done) }); return nil }
func (d *compositionPreviewDecoder) Wait() error  { <-d.done; return nil }
func (d *compositionPreviewDecoder) Kill() error  { d.once.Do(func() { close(d.done) }); return nil }

type fpgaCompositionService struct{ compositionService }

func (*fpgaCompositionService) Launch(context.Context, string, fogcast.ProgressFunc) (protocol.CachedLaunchResponse, error) {
	gameID := "actraiser"
	system := protocol.SystemSNES
	core := "SNES"
	return protocol.CachedLaunchResponse{Status: protocol.Status{
		State: protocol.StateActive, GameID: &gameID, System: &system, ObservedCore: &core,
	}}, nil
}

type fpgaPreviewTrackingService struct {
	fpgaCompositionService
	status protocol.Status
	stops  int
}

func (s *fpgaPreviewTrackingService) Status(context.Context) (protocol.Status, error) {
	return s.status, nil
}

func (s *fpgaPreviewTrackingService) Stop(context.Context) (protocol.Status, error) {
	s.stops++
	return protocol.Status{State: protocol.StateIdle}, nil
}

func strPtr(value string) *string                      { return &value }
func systemPtr(value protocol.System) *protocol.System { return &value }

func TestComposeAPIFPGAMJPEGPreviewUsesCaptureInHostUI(t *testing.T) {
	capture := &compositionPreviewCapture{started: make(chan struct{}), closed: make(chan struct{}), samples: make(chan remotemedia.EncodedSample, 1)}
	decoder := &compositionPreviewDecoder{done: make(chan struct{}), writes: make(chan []byte, 1)}
	fixtureJPEG := []byte{0xff, 0xd8, 1, 2, 0xff, 0xd9}
	previewHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=fixture")
		_, _ = w.Write(fixtureJPEG)
	})
	config := fogcast.Config{Token: "test-token", Media: fogcast.MediaConfig{Enabled: true, CaptureDevice: "fixture-device"}}
	handler, cleanup, err := composeAPI(&fpgaCompositionService{}, config, nil,
		withCaptureSourceFactory(func(fogcast.MediaConfig) (remotemedia.CaptureSource, error) { return capture, nil }),
		withPreviewDecoderFactory(func(context.Context) (remotemedia.ManagedDecoder, error) { return decoder, nil }),
		withPreviewHandler(previewHandler),
	)
	if err != nil {
		t.Fatal(err)
	}
	cleaned := false
	t.Cleanup(func() {
		if !cleaned {
			_ = cleanup()
		}
	})
	response := serveComposition(t, handler, http.MethodPost, "/api/v1/session/launch", `{"game_id":"actraiser"}`)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"execution":"fpga_native"`) || !strings.Contains(response.Body.String(), `"media":"active"`) {
		t.Fatalf("launch = %d %s", response.Code, response.Body.String())
	}
	select {
	case <-capture.started:
	default:
		t.Fatal("preview capture did not start")
	}
	if decoder.started != 1 {
		t.Fatalf("preview decoder starts = %d", decoder.started)
	}
	capture.samples <- remotemedia.EncodedSample{AVCC: []byte{0, 0, 0, 1, 0x65}, SPS: []byte{0x67}, PPS: []byte{0x68}, NALLengthSize: 4, Keyframe: true}
	select {
	case annexB := <-decoder.writes:
		if !strings.Contains(string(annexB), string([]byte{0, 0, 0, 1, 0x65})) {
			t.Fatalf("preview decoder did not receive capture frame: %x", annexB)
		}
	case <-time.After(time.Second):
		t.Fatal("preview decoder did not receive capture frame")
	}
	previewRoute := serveComposition(t, handler, http.MethodGet, "/api/v1/session/preview", "")
	if previewRoute.Code != http.StatusOK || !strings.Contains(previewRoute.Body.String(), string(fixtureJPEG)) {
		t.Fatalf("preview route = %d %x", previewRoute.Code, previewRoute.Body.Bytes())
	}
	err = cleanup()
	cleaned = true
	if err != nil {
		t.Fatalf("preview cleanup: %v", err)
	}
	select {
	case <-capture.closed:
	default:
		t.Fatal("preview cleanup did not close capture")
	}
}

func TestComposeAPIFPGAPreviewStartFailureKeepsFPGASession(t *testing.T) {
	service := &fpgaPreviewTrackingService{
		status: protocol.Status{State: protocol.StateActive, GameID: strPtr("actraiser"), System: systemPtr(protocol.SystemSNES), ObservedCore: strPtr("SNES")},
	}
	config := fogcast.Config{Token: "test-token", Media: fogcast.MediaConfig{Enabled: true, CaptureDevice: "fixture-device"}}
	handler, cleanup, err := composeAPI(service, config, nil,
		withCaptureSourceFactory(func(fogcast.MediaConfig) (remotemedia.CaptureSource, error) {
			t.Fatal("preview decoder failure must not open capture")
			return nil, errors.New("capture device is busy")
		}),
		withPreviewDecoderFactory(func(context.Context) (remotemedia.ManagedDecoder, error) {
			return nil, errors.New("ffmpeg is unavailable")
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	response := serveComposition(t, handler, http.MethodPost, "/api/v1/session/launch", `{"game_id":"actraiser"}`)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"execution":"fpga_native"`) || !strings.Contains(response.Body.String(), `"media":"failed"`) {
		t.Fatalf("launch = %d %s", response.Code, response.Body.String())
	}
	if service.stops != 0 {
		t.Fatalf("preview start failure stopped FPGA session: stops=%d", service.stops)
	}
	again := serveComposition(t, handler, http.MethodPost, "/api/v1/session/launch", `{"game_id":"actraiser"}`)
	if again.Code != http.StatusOK || strings.Contains(again.Body.String(), `"code":"TARGET_UNAVAILABLE"`) {
		t.Fatalf("second launch = %d %s", again.Code, again.Body.String())
	}
}

func TestPreviewCaptureDeviceOverrideEnablesMJPEGWithoutStoredMedia(t *testing.T) {
	config := fogcast.Config{Media: fogcast.MediaConfig{}}
	applyPreviewCaptureDevice(&config, "  fixture ShadowCast  ")
	if !config.Media.Enabled || config.Media.CaptureDevice != "fixture ShadowCast" {
		t.Fatalf("media = %#v", config.Media)
	}
}

func TestStopFolderWatchTimesOutWhenReconcileIsStuck(t *testing.T) {
	started := time.Now()
	err := stopFolderWatch(func() {}, make(chan struct{}), 30*time.Millisecond)
	if !errors.Is(err, errFolderWatchStopTimeout) {
		t.Fatalf("stop = %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("folder-watch stop blocked beyond the bound")
	}
}

func TestStopFolderWatchReturnsWhenWatchStops(t *testing.T) {
	done := make(chan struct{})
	close(done)
	if err := stopFolderWatch(func() {}, done, time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestRunReportsFolderWatchTerminalFailureWithoutLeakingError(t *testing.T) {
	watchErr := errors.New("folder-watch secret=must-not-leak")
	watchService := &folderWatchCompositionService{watchErr: watchErr}
	var stderr bytes.Buffer
	runFolderWatch(context.Background(), watchService, &stderr, time.Hour)
	if got := stderr.String(); !strings.Contains(got, "fogcast-api: folder-watch failed") || strings.Contains(got, watchErr.Error()) {
		t.Fatalf("stderr = %q", got)
	}
}

func TestRunReportsFolderWatchReconcileFailuresWhileWatcherRemainsActive(t *testing.T) {
	release := make(chan struct{})
	watchService := &folderWatchCompositionService{watchRelease: release, reconcileFailures: 2}
	logs := make(folderWatchLogWriter, 1)
	done := make(chan struct{})
	go func() {
		runFolderWatch(context.Background(), watchService, logs, time.Millisecond)
		close(done)
	}()
	select {
	case got := <-logs:
		if !strings.Contains(got, "fogcast-api: folder-watch reconciliation failures: 2") {
			t.Fatalf("stderr = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("folder-watch failure count was not reported")
	}
	select {
	case <-done:
		t.Fatal("folder watch returned before the active runner stopped")
	default:
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("folder watch did not return after the runner stopped")
	}
}

func TestRunCleanupPropagatesStableCloseFailure(t *testing.T) {
	var stderr bytes.Buffer
	code := finishRun(0, &stderr,
		runCloser{label: "API cleanup", close: func() error { return errors.New("token=secret") }},
		runCloser{label: "service cleanup", close: func() error { return nil }},
	)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if got := stderr.String(); !strings.Contains(got, "fogcast-api: API cleanup failed") || strings.Contains(got, "secret") {
		t.Fatalf("stderr = %q", got)
	}
}

func TestStopServiceForShutdownRetriesAndPropagatesFirstFailure(t *testing.T) {
	first := errors.New("host stop failed")
	service := &shutdownCompositionService{stopErrs: []error{first, nil}, shutdownCleanup: true}
	if err := stopServiceForShutdown(service); !errors.Is(err, first) {
		t.Fatalf("shutdown error = %v, want first failure", err)
	}
	service.mu.Lock()
	stops := service.stops
	service.mu.Unlock()
	if stops != 2 {
		t.Fatalf("shutdown stop attempts = %d, want 2", stops)
	}
}

func TestStopServiceForShutdownSkipsNeverOwnedIdle(t *testing.T) {
	service := &shutdownCompositionService{}

	if err := stopServiceForShutdown(service); err != nil {
		t.Fatalf("shutdown error = %v", err)
	}
	if service.stops != 0 {
		t.Fatalf("shutdown stop attempts = %d, want 0", service.stops)
	}
}

func TestStopServiceForShutdownSkipsPostStopIdle(t *testing.T) {
	service := &shutdownCompositionService{stops: 1}

	if err := stopServiceForShutdown(service); err != nil {
		t.Fatalf("shutdown error = %v", err)
	}
	if service.stops != 1 {
		t.Fatalf("shutdown stop attempts = %d, want unchanged at 1", service.stops)
	}
}

func TestStopServiceForShutdownStopsOwnedSession(t *testing.T) {
	service := &shutdownCompositionService{shutdownCleanup: true}

	if err := stopServiceForShutdown(service); err != nil {
		t.Fatalf("shutdown error = %v", err)
	}
	if service.stops != 1 {
		t.Fatalf("shutdown stop attempts = %d, want 1", service.stops)
	}
}

func TestCloseAPICompositionAttemptsCompositionAfterRemoteInputFailure(t *testing.T) {
	remoteErr := errors.New("remote input close failed")
	compositionCalled := false
	err := closeAPIComposition(func() error { return remoteErr }, []func() error{
		func() error {
			compositionCalled = true
			return errors.New("composition close failed")
		},
	})
	if !errors.Is(err, remoteErr) {
		t.Fatalf("close error = %v, want first remote-input failure", err)
	}
	if !compositionCalled {
		t.Fatal("remote-input failure skipped composition cleanup")
	}
}

type compositionMetadataRuntime struct {
	mu     sync.Mutex
	closes int
}

func (r *compositionMetadataRuntime) Lookup(context.Context, metadata.LookupInput) (metadata.Result, error) {
	return metadata.Result{}, nil
}
func (r *compositionMetadataRuntime) OpenArtwork(context.Context, string) (metadata.Artwork, error) {
	return metadata.Artwork{}, errors.New("unused metadata artwork")
}
func (r *compositionMetadataRuntime) Close() error {
	r.mu.Lock()
	r.closes++
	r.mu.Unlock()
	return nil
}
func (r *compositionMetadataRuntime) closeCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closes
}

func TestComposeAPIMetadataCleanupCoversPostOpenFailureBranches(t *testing.T) {
	base := fogcast.Config{MetadataRoot: t.TempDir(), Metadata: fogcast.MetadataConfig{Configured: true, Enabled: true, Provider: "igdb"}}
	tests := []struct {
		name   string
		config fogcast.Config
		start  bridgeStarterFactory
		setup  []compositionOption
	}{
		{
			name:   "nil capture factory",
			config: fogcast.Config{MetadataRoot: base.MetadataRoot, Metadata: base.Metadata, Media: fogcast.MediaConfig{Enabled: true}},
			setup:  []compositionOption{withCaptureSourceFactory(nil)},
		},
		{
			name:   "nil bridge factory",
			config: fogcast.Config{MetadataRoot: base.MetadataRoot, Metadata: base.Metadata, RemoteInput: fogcast.RemoteInputConfig{Enabled: true}},
		},
		{
			name:   "bridge factory error",
			config: fogcast.Config{MetadataRoot: base.MetadataRoot, Metadata: base.Metadata, RemoteInput: fogcast.RemoteInputConfig{Enabled: true}},
			start: func(fogcast.Config) (host.BridgeStarter, error) {
				return nil, errors.New("bridge construction failed")
			},
		},
		{
			name:   "nil bridge starter",
			config: fogcast.Config{MetadataRoot: base.MetadataRoot, Metadata: base.Metadata, RemoteInput: fogcast.RemoteInputConfig{Enabled: true}},
			start:  func(fogcast.Config) (host.BridgeStarter, error) { return nil, nil },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opened := &compositionMetadataRuntime{}
			options := append([]compositionOption{withMetadataOpener(func(context.Context, metadata.RuntimeConfig) (metadata.Runtime, error) {
				return opened, nil
			})}, test.setup...)
			handler, cleanup, err := composeAPI(&compositionService{}, test.config, test.start, options...)
			if err == nil || handler != nil || cleanup != nil {
				t.Fatalf("failure composition = handler:%v cleanup:%v err:%v", handler != nil, cleanup != nil, err)
			}
			if got := opened.closeCount(); got != 1 {
				t.Fatalf("metadata close count = %d, want 1", got)
			}
		})
	}
}

func TestComposeAPIMetadataCleanupOnSuccessIsExplicitAndIdempotent(t *testing.T) {
	opened := &compositionMetadataRuntime{}
	config := fogcast.Config{MetadataRoot: t.TempDir(), Metadata: fogcast.MetadataConfig{Configured: true, Enabled: true, Provider: "igdb"}}
	handler, cleanup, err := composeAPI(&compositionService{}, config, nil, withMetadataOpener(func(context.Context, metadata.RuntimeConfig) (metadata.Runtime, error) {
		return opened, nil
	}))
	if err != nil || handler == nil || cleanup == nil {
		t.Fatalf("success composition = handler:%v cleanup:%v err:%v", handler != nil, cleanup != nil, err)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if got := opened.closeCount(); got != 2 {
		t.Fatalf("cleanup should invoke runtime Close once per explicit call, got %d", got)
	}
}

func TestComposeAPIMetadataCleanupOnRemoteInputSuccess(t *testing.T) {
	opened := &compositionMetadataRuntime{}
	config := fogcast.Config{
		MetadataRoot: t.TempDir(),
		Metadata:     fogcast.MetadataConfig{Configured: true, Enabled: true, Provider: "igdb", ClientID: "client", ClientSecret: "secret"},
		RemoteInput:  fogcast.RemoteInputConfig{Enabled: true},
	}
	var gotConfig metadata.RuntimeConfig
	handler, cleanup, err := composeAPI(&compositionService{}, config, func(fogcast.Config) (host.BridgeStarter, error) {
		return host.BridgeStarterFunc(func(context.Context, host.BridgeSpec) (host.BridgeHandle, error) {
			return nil, errors.New("unused bridge")
		}), nil
	}, withMetadataOpener(func(_ context.Context, received metadata.RuntimeConfig) (metadata.Runtime, error) {
		gotConfig = received
		return opened, nil
	}))
	if err != nil || handler == nil || cleanup == nil {
		t.Fatalf("remote-input success composition = handler:%v cleanup:%v err:%v", handler != nil, cleanup != nil, err)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if got := opened.closeCount(); got != 1 {
		t.Fatalf("metadata close count = %d, want 1", got)
	}
	if gotConfig.Root != config.MetadataRoot || !gotConfig.Configured || !gotConfig.Enabled || gotConfig.ProviderName != metadata.ProviderIGDB || gotConfig.ClientID != "client" || gotConfig.ClientSecret != "secret" {
		t.Fatalf("metadata opener config = %#v", gotConfig)
	}
}
