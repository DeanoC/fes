package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/internal/metadata"
	"github.com/DeanoC/FogCast/internal/remotemedia"
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
		return protocol.Status{State: protocol.StateIdle}, nil
	}
	err := s.stopErrs[0]
	s.stopErrs = s.stopErrs[1:]
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

type compositionCapture struct {
	mu        sync.Mutex
	closed    int
	closeErrs []error
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

func (*compositionCapture) Start() error { return nil }
func (*compositionCapture) Next(context.Context) (remotemedia.EncodedSample, error) {
	return remotemedia.EncodedSample{}, context.Canceled
}
func (*compositionCapture) Stats() remotemedia.CaptureStats { return remotemedia.CaptureStats{} }
func (c *compositionCapture) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed++
	if len(c.closeErrs) != 0 {
		err := c.closeErrs[0]
		c.closeErrs = c.closeErrs[1:]
		return err
	}
	return nil
}
func (c *compositionCapture) closeCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

type compositionPacketConn struct{ closed chan struct{} }

func (c *compositionPacketConn) ReadFrom([]byte) (int, net.Addr, error) {
	<-c.closed
	return 0, nil, errors.New("closed")
}
func (c *compositionPacketConn) Close() error {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	return nil
}
func (*compositionPacketConn) LocalAddr() net.Addr { return &net.UDPAddr{} }

type compositionTransport struct{}

func (*compositionTransport) AcceptControl(remotemedia.ControlMessage) error  { return nil }
func (*compositionTransport) Ingest([]byte) ([]remotemedia.AccessUnit, error) { return nil, nil }
func (*compositionTransport) Close() error                                    { return nil }

type compositionRunner struct {
	done   chan struct{}
	source remotemedia.CaptureSource
}

func (r *compositionRunner) Run(ctx context.Context) error { <-ctx.Done(); close(r.done); return nil }
func (r *compositionRunner) Close() error                  { return r.source.Close() }

type hostOnlyCompositionService struct{ compositionService }

func (*hostOnlyCompositionService) SessionExecution(context.Context, string) (string, error) {
	return "host_only", nil
}

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

type compositionTargetCast struct {
	mu               sync.Mutex
	started          int
	stopped          int
	state            string
	session          string
	generation       uint64
	stopErr          error
	startErr         error
	reportSession    string
	reportGeneration uint64
	blockStop        bool
	statusErr        error
}

func (c *compositionTargetCast) CastStart(_ context.Context, session, _ string, generation uint64) (targetclient.CastStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.started++
	c.state = "active"
	c.session = session
	c.generation = generation
	if c.startErr != nil {
		return targetclient.CastStatus{}, c.startErr
	}
	reportSession, reportGeneration := session, generation
	if c.reportSession != "" {
		reportSession = c.reportSession
	}
	if c.reportGeneration != 0 {
		reportGeneration = c.reportGeneration
	}
	return targetclient.CastStatus{State: "active", Session: reportSession, Generation: reportGeneration}, nil
}

func (c *compositionTargetCast) CastStartWithMedia(ctx context.Context, session, token string, generation uint64, media protocol.CastMediaSet) (targetclient.CastStatus, error) {
	status, err := c.CastStart(ctx, session, token, generation)
	if err == nil {
		status.Media = &protocol.CastStatusMedia{Version: media.Version, Video: media.Video, Audio: media.Audio, Ready: true, Capabilities: protocol.CastMediaCapabilities{Version: protocol.CastMediaSetVersion, Video: true, Audio: true}}
	}
	return status, err
}
func (c *compositionTargetCast) CastStop(ctx context.Context, session string, generation uint64) (targetclient.CastStatus, error) {
	c.mu.Lock()
	if c.state == "active" && (c.session != session || c.generation != generation) {
		c.mu.Unlock()
		return targetclient.CastStatus{}, errors.New("stale cast identity")
	}
	c.stopped++
	block := c.blockStop
	err := c.stopErr
	if err == nil && !block {
		c.state = "idle"
	}
	c.mu.Unlock()
	if block {
		<-ctx.Done()
		return targetclient.CastStatus{}, ctx.Err()
	}
	if err != nil {
		return targetclient.CastStatus{}, err
	}
	return targetclient.CastStatus{State: "idle"}, nil
}
func (c *compositionTargetCast) CastStatus(context.Context) (targetclient.CastStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.statusErr != nil {
		return targetclient.CastStatus{}, c.statusErr
	}
	return targetclient.CastStatus{State: c.state, Session: c.session, Generation: c.generation}, nil
}
func (c *compositionTargetCast) counts() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.started, c.stopped
}
func (c *compositionTargetCast) setState(state string) {
	c.mu.Lock()
	c.state = state
	c.mu.Unlock()
}
func (c *compositionTargetCast) setStopError(err error) {
	c.mu.Lock()
	c.stopErr = err
	c.mu.Unlock()
}
func (c *compositionTargetCast) setStatusError(err error) {
	c.mu.Lock()
	c.statusErr = err
	c.mu.Unlock()
}

func (c *compositionTargetCast) setIdentity(session string, generation uint64) {
	c.mu.Lock()
	c.state = "active"
	c.session = session
	c.generation = generation
	c.mu.Unlock()
}

func TestComposeAPIStartsAndStopsTargetCastWithHostMedia(t *testing.T) {
	target := &compositionTargetCast{}
	capture := &compositionCapture{}
	config := fogcast.Config{Token: "token", Media: fogcast.MediaConfig{Enabled: true, Session: "session", SSRC: 7, RTPListen: "127.0.0.1:5000", RTPDestination: "127.0.0.1:5001", CaptureDevice: "injected", Decoder: "none"}}
	handler, cleanup, err := composeAPI(&hostOnlyCompositionService{}, config, nil,
		withTargetCast(target),
		withCaptureSourceFactory(func(fogcast.MediaConfig) (remotemedia.CaptureSource, error) { return capture, nil }),
		withManagedReceiverOptions(
			remotemedia.WithManagedReceiverBind(func(string, *net.UDPAddr) (remotemedia.ManagedPacketConn, error) {
				return &compositionPacketConn{closed: make(chan struct{})}, nil
			}),
			remotemedia.WithManagedReceiverFactory(func(remotemedia.ReceiverConfig) (remotemedia.ManagedReceiverTransport, error) {
				return &compositionTransport{}, nil
			}),
		),
		withManagedSenderOptions(remotemedia.WithManagedSenderFactory(func(remotemedia.SenderConfig, remotemedia.CaptureSource) (remotemedia.ManagedSenderRunner, error) {
			return &compositionRunner{done: make(chan struct{}), source: capture}, nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/launch", strings.NewReader(`{"game_id":"game-1"}`))
	request.Host = "127.0.0.1"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	started, stopped := target.counts()
	if started != 1 || stopped != 1 {
		t.Fatalf("target lifecycle = %d/%d response=%d %s", started, stopped, response.Code, response.Body.String())
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
}

func TestComposeAPIFPGALocalPlaybackUsesFFPlayReceiverWithoutTargetCast(t *testing.T) {
	target := &compositionTargetCast{}
	capture := &compositionCapture{}
	config := fogcast.Config{Token: "test-token", Media: fogcast.MediaConfig{
		Enabled: true, Session: "fixture-session", Generation: 3, SSRC: 7,
		RTPListen: "127.0.0.1:5000", RTPDestination: "127.0.0.1:5000",
		CaptureDevice: "fixture-device", Decoder: "ffplay",
	}}
	handler, cleanup, err := composeAPI(&fpgaCompositionService{}, config, nil,
		withTargetCast(target),
		withCaptureSourceFactory(func(fogcast.MediaConfig) (remotemedia.CaptureSource, error) { return capture, nil }),
		withManagedReceiverOptions(
			remotemedia.WithManagedReceiverBind(func(string, *net.UDPAddr) (remotemedia.ManagedPacketConn, error) {
				return &compositionPacketConn{closed: make(chan struct{})}, nil
			}),
			remotemedia.WithManagedReceiverFactory(func(remotemedia.ReceiverConfig) (remotemedia.ManagedReceiverTransport, error) {
				return &compositionTransport{}, nil
			}),
			remotemedia.WithManagedReceiverDecoder(func(context.Context) (remotemedia.ManagedDecoder, error) { return nil, nil }),
		),
		withManagedSenderOptions(remotemedia.WithManagedSenderFactory(func(remotemedia.SenderConfig, remotemedia.CaptureSource) (remotemedia.ManagedSenderRunner, error) {
			return &compositionRunner{done: make(chan struct{}), source: capture}, nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	response := serveComposition(t, handler, http.MethodPost, "/api/v1/session/launch", `{"game_id":"actraiser"}`)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"execution":"fpga_native"`) || !strings.Contains(response.Body.String(), `"media":"active"`) {
		t.Fatalf("launch = %d %s", response.Code, response.Body.String())
	}
	started, stopped := target.counts()
	if started != 0 || stopped != 0 {
		t.Fatalf("local playback claimed target cast: starts=%d stops=%d", started, stopped)
	}
}

func TestComposeAPIFPGAMJPEGPreviewUsesCaptureInHostUIWithoutTargetCast(t *testing.T) {
	target := &compositionTargetCast{}
	capture := &compositionPreviewCapture{started: make(chan struct{}), closed: make(chan struct{}), samples: make(chan remotemedia.EncodedSample, 1)}
	decoder := &compositionPreviewDecoder{done: make(chan struct{}), writes: make(chan []byte, 1)}
	fixtureJPEG := []byte{0xff, 0xd8, 1, 2, 0xff, 0xd9}
	previewHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=fixture")
		_, _ = w.Write(fixtureJPEG)
	})
	config := fogcast.Config{Token: "test-token", Media: fogcast.MediaConfig{Enabled: true, CaptureDevice: "fixture-device", Decoder: "mjpeg"}}
	handler, cleanup, err := composeAPI(&fpgaCompositionService{}, config, nil,
		withTargetCast(target),
		withCaptureSourceFactory(func(fogcast.MediaConfig) (remotemedia.CaptureSource, error) { return capture, nil }),
		withPreviewDecoderFactory(func(context.Context) (remotemedia.ManagedDecoder, error) { return decoder, nil }),
		withPreviewHandler(previewHandler),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
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
	started, stopped := target.counts()
	if started != 0 || stopped != 0 {
		t.Fatalf("local preview claimed target cast: starts=%d stops=%d", started, stopped)
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
}

func TestComposeAPIFPGAPreviewStartFailureKeepsFPGASession(t *testing.T) {
	service := &fpgaPreviewTrackingService{
		status: protocol.Status{State: protocol.StateActive, GameID: strPtr("actraiser"), System: systemPtr(protocol.SystemSNES), ObservedCore: strPtr("SNES")},
	}
	config := fogcast.Config{Token: "test-token", Media: fogcast.MediaConfig{Enabled: true, CaptureDevice: "fixture-device", Decoder: "mjpeg"}}
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
	if !config.Media.Enabled || config.Media.Decoder != "mjpeg" || config.Media.CaptureDevice != "fixture ShadowCast" {
		t.Fatalf("media = %#v", config.Media)
	}
}

func TestComposeAPIRejectsAudioForLocalFFPlayRoute(t *testing.T) {
	config := fogcast.Config{Token: "test-token", Media: fogcast.MediaConfig{
		Enabled: true, Session: "fixture-session", Generation: 3, SSRC: 7,
		RTPListen: "127.0.0.1:5000", RTPDestination: "127.0.0.1:5000",
		CaptureDevice: "fixture-device", Decoder: "ffplay", Audio: compositionAudioConfig(),
	}}
	if handler, cleanup, err := composeAPI(&fpgaCompositionService{}, config, nil); err == nil || handler != nil || cleanup != nil {
		t.Fatalf("audio local playback composition = handler:%v cleanup:%v err:%v", handler != nil, cleanup != nil, err)
	}
}

func TestComposeAPIInjectsEnabledMedia(t *testing.T) {
	service := &hostOnlyCompositionService{}
	captureMade, senderMade := false, false
	capture := &compositionCapture{}
	config := fogcast.Config{Token: "token", Media: fogcast.MediaConfig{Enabled: true, Session: "session", SSRC: 7, RTPListen: "127.0.0.1:5000", RTPDestination: "127.0.0.1:5001", CaptureDevice: "injected", Decoder: "none"}}
	handler, cleanup, err := composeAPI(service, config, nil,
		withCaptureSourceFactory(func(fogcast.MediaConfig) (remotemedia.CaptureSource, error) {
			captureMade = true
			return capture, nil
		}),
		withManagedReceiverOptions(
			remotemedia.WithManagedReceiverBind(func(string, *net.UDPAddr) (remotemedia.ManagedPacketConn, error) {
				return &compositionPacketConn{closed: make(chan struct{})}, nil
			}),
			remotemedia.WithManagedReceiverFactory(func(remotemedia.ReceiverConfig) (remotemedia.ManagedReceiverTransport, error) {
				return &compositionTransport{}, nil
			}),
		),
		withManagedSenderOptions(remotemedia.WithManagedSenderFactory(func(remotemedia.SenderConfig, remotemedia.CaptureSource) (remotemedia.ManagedSenderRunner, error) {
			senderMade = true
			return &compositionRunner{done: make(chan struct{}), source: capture}, nil
		})),
	)
	if err != nil || handler == nil || cleanup == nil || captureMade {
		t.Fatalf("composition = handler:%v cleanup:%v capture:%v err:%v", handler != nil, cleanup != nil, captureMade, err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/launch", strings.NewReader(`{"game_id":"game-1"}`))
	request.Host = "127.0.0.1"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code >= 500 || !senderMade {
		t.Fatalf("media launch = %d %s sender:%v", response.Code, response.Body.String(), senderMade)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if got := capture.closeCount(); got != 1 {
		t.Fatalf("capture close count = %d, want 1", got)
	}
}

func TestComposeAPIDoesNotOpenAudioWhenDisabled(t *testing.T) {
	called := false
	config := fogcast.Config{Token: "token", Media: fogcast.MediaConfig{Enabled: true, Session: "session", SSRC: 7, RTPListen: "127.0.0.1:5000", RTPDestination: "127.0.0.1:5001", CaptureDevice: "injected", Decoder: "none"}}
	_, cleanup, err := composeAPI(&hostOnlyCompositionService{}, config, nil,
		withAudioSourceFactory(func(remotemedia.AudioSourceConfig) (remotemedia.AudioSource, error) {
			called = true
			return nil, errors.New("must not be called")
		}),
	)
	if err != nil || cleanup == nil || called {
		t.Fatalf("composition err=%v cleanup=%v audio=%v", err, cleanup != nil, called)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
}

func TestComposeAPIOpensConfiguredAudioSourceOnlyAtLaunch(t *testing.T) {
	called := false
	config := fogcast.Config{Token: "token", Media: fogcast.MediaConfig{Enabled: true, Session: "session", Generation: 3, SSRC: 7, RTPListen: "127.0.0.1:5000", RTPDestination: "127.0.0.1:5001", CaptureDevice: "injected", Decoder: "none", Audio: compositionAudioConfig()}}
	handler, cleanup, err := composeAPI(&hostOnlyCompositionService{}, config, nil,
		withTargetCast(&compositionTargetCast{}),
		withCaptureSourceFactory(func(fogcast.MediaConfig) (remotemedia.CaptureSource, error) { return &compositionCapture{}, nil }),
		withAudioSourceFactory(func(remotemedia.AudioSourceConfig) (remotemedia.AudioSource, error) {
			called = true
			return &compositionAudioSource{}, nil
		}),
		withManagedSenderOptions(remotemedia.WithManagedSenderFactory(func(_ remotemedia.SenderConfig, source remotemedia.CaptureSource) (remotemedia.ManagedSenderRunner, error) {
			return &compositionRunner{done: make(chan struct{}), source: source}, nil
		})),
		withManagedAudioSenderOptions(remotemedia.WithManagedAudioSenderFactory(func(_ remotemedia.AudioSenderConfig, source remotemedia.AudioSource) (remotemedia.ManagedAudioSenderRunner, error) {
			return &compositionAudioRunner{source: source}, nil
		})),
	)
	if err != nil || cleanup == nil {
		t.Fatalf("composition err=%v cleanup=%v", err, cleanup != nil)
	}
	defer cleanup()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/launch", strings.NewReader(`{"game_id":"game-1"}`))
	request.Host = "127.0.0.1"
	response := httptest.NewRecorder()
	if called {
		t.Fatal("audio source opened before session launch")
	}
	handler.ServeHTTP(response, request)
	if response.Code >= 500 || !called {
		t.Fatalf("audio composition = %d %s called=%v", response.Code, response.Body.String(), called)
	}
}

func TestComposeAPICreatesFreshAudioSourceForEachLaunch(t *testing.T) {
	var audioSources []*compositionAudioSource
	config := fogcast.Config{Token: "token", Media: fogcast.MediaConfig{Enabled: true, Session: "session", Generation: 3, SSRC: 7, RTPListen: "127.0.0.1:5000", RTPDestination: "127.0.0.1:5001", CaptureDevice: "injected", Decoder: "none", Audio: compositionAudioConfig()}}
	handler, cleanup, err := composeAPI(&hostOnlyCompositionService{}, config, nil,
		withTargetCast(&compositionTargetCast{}),
		withCaptureSourceFactory(func(fogcast.MediaConfig) (remotemedia.CaptureSource, error) { return &compositionCapture{}, nil }),
		withAudioSourceFactory(func(remotemedia.AudioSourceConfig) (remotemedia.AudioSource, error) {
			source := &compositionAudioSource{}
			audioSources = append(audioSources, source)
			return source, nil
		}),
		withManagedSenderOptions(remotemedia.WithManagedSenderFactory(func(_ remotemedia.SenderConfig, source remotemedia.CaptureSource) (remotemedia.ManagedSenderRunner, error) {
			return &compositionRunner{done: make(chan struct{}), source: source}, nil
		})),
		withManagedAudioSenderOptions(remotemedia.WithManagedAudioSenderFactory(func(_ remotemedia.AudioSenderConfig, source remotemedia.AudioSource) (remotemedia.ManagedAudioSenderRunner, error) {
			return &compositionAudioRunner{source: source}, nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	for index := 0; index < 2; index++ {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/session/launch", strings.NewReader(`{"game_id":"game-1"}`))
		request.Host = "127.0.0.1"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code >= 500 {
			t.Fatalf("launch %d = %d %s", index+1, response.Code, response.Body.String())
		}
	}
	if len(audioSources) != 2 || audioSources[0] == audioSources[1] {
		t.Fatalf("audio sources = %#v, want two distinct sources", audioSources)
	}
	audioSources[0].mu.Lock()
	firstClosed := audioSources[0].closed
	audioSources[0].mu.Unlock()
	if firstClosed != 1 {
		t.Fatalf("first audio source close count = %d, want 1", firstClosed)
	}
}

func compositionAudioConfig() remotemedia.AudioConfig {
	return remotemedia.AudioConfig{
		Enabled:   true,
		Source:    remotemedia.AudioSourceConfig{Enabled: true, Kind: remotemedia.AudioSourceShadowCastUAC, SampleRate: remotemedia.AudioSampleRate, Channels: 1, FrameSamples: remotemedia.DefaultAudioFrameSamples},
		Transport: remotemedia.AudioTransportConfig{RTPDestination: "127.0.0.1:5004", ControlAddress: "127.0.0.1:5005", SSRC: 8, MTU: 1200, FormatCapabilityVersion: 1},
	}
}

type compositionAudioSource struct {
	mu     sync.Mutex
	closed int
}

func (*compositionAudioSource) Start() error { return nil }
func (*compositionAudioSource) Next(context.Context) (remotemedia.AudioSample, error) {
	return remotemedia.AudioSample{}, errors.New("not used by composition")
}
func (*compositionAudioSource) Stats() remotemedia.AudioStats { return remotemedia.AudioStats{} }
func (s *compositionAudioSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed++
	return nil
}

type compositionAudioRunner struct{ source remotemedia.AudioSource }

func (r *compositionAudioRunner) Run(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
func (r *compositionAudioRunner) Close() error                  { return r.source.Close() }

func TestManagedSenderComponentDoesNotDoubleCloseTransferredSources(t *testing.T) {
	capture := &compositionCapture{}
	audio := &compositionAudioSource{}
	component := &managedSenderComponent{
		media: fogcast.MediaConfig{
			Enabled: true, Session: "session", Generation: 3, SSRC: 7,
			RTPDestination: "127.0.0.1:5001", ControlAddress: "127.0.0.1:5002",
			Audio: compositionAudioConfig(),
		},
		token: "token",
		newSources: func(fogcast.MediaConfig) (remotemedia.CaptureSource, remotemedia.AudioSource, error) {
			return capture, audio, nil
		},
		options: []remotemedia.ManagedSenderOption{
			remotemedia.WithManagedSenderFactory(func(_ remotemedia.SenderConfig, source remotemedia.CaptureSource) (remotemedia.ManagedSenderRunner, error) {
				return &compositionRunner{done: make(chan struct{}), source: source}, nil
			}),
		},
		audioOptions: []remotemedia.ManagedAudioSenderOption{
			remotemedia.WithManagedAudioSenderFactory(func(remotemedia.AudioSenderConfig, remotemedia.AudioSource) (remotemedia.ManagedAudioSenderRunner, error) {
				return nil, errors.New("audio startup failed")
			}),
		},
	}
	handle, err := component.Start(context.Background(), "game")
	if err == nil || handle != nil {
		t.Fatalf("Start = handle:%v err:%v, want fully rolled back failure", handle != nil, err)
	}
	if got := capture.closeCount(); got != 1 {
		t.Fatalf("capture close count = %d, want 1", got)
	}
	audio.mu.Lock()
	got := audio.closed
	audio.mu.Unlock()
	if got != 1 {
		t.Fatalf("audio close count = %d, want 1", got)
	}
}

type blockingCompositionAudioSource struct {
	release chan struct{}
	closed  chan struct{}
}

func (*blockingCompositionAudioSource) Start() error { return nil }
func (*blockingCompositionAudioSource) Next(context.Context) (remotemedia.AudioSample, error) {
	return remotemedia.AudioSample{}, context.Canceled
}
func (*blockingCompositionAudioSource) Stats() remotemedia.AudioStats {
	return remotemedia.AudioStats{}
}
func (s *blockingCompositionAudioSource) Close() error {
	<-s.release
	close(s.closed)
	return nil
}

func TestMediaSourcesCleanupClosesCaptureWhileAudioCloseBlocks(t *testing.T) {
	audio := &blockingCompositionAudioSource{release: make(chan struct{}), closed: make(chan struct{})}
	capture := &compositionCapture{}
	handle := &mediaSourcesCleanupHandle{
		audio:   &mediaSourceCleanupSlot{source: audio},
		capture: &mediaSourceCleanupSlot{source: capture},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := handle.Stop(ctx); err == nil {
		t.Fatal("blocked audio cleanup unexpectedly succeeded")
	}
	if got := capture.closeCount(); got != 1 {
		t.Fatalf("capture close count = %d, want 1 despite blocked audio", got)
	}
	close(audio.release)
	select {
	case <-audio.closed:
	case <-time.After(time.Second):
		t.Fatal("blocked audio close did not finish after release")
	}
}

type legacyCompositionTarget struct{ target compositionTargetCast }

func (t *legacyCompositionTarget) CastStart(ctx context.Context, session, token string, generation uint64) (targetclient.CastStatus, error) {
	return t.target.CastStart(ctx, session, token, generation)
}
func (t *legacyCompositionTarget) CastStop(ctx context.Context, session string, generation uint64) (targetclient.CastStatus, error) {
	return t.target.CastStop(ctx, session, generation)
}
func (t *legacyCompositionTarget) CastStatus(ctx context.Context) (targetclient.CastStatus, error) {
	return t.target.CastStatus(ctx)
}

func TestComposeAPIRejectsAudioAdmissionBeforeOpeningSourcesOnLegacyTarget(t *testing.T) {
	opened := false
	config := fogcast.Config{Token: "token", Media: fogcast.MediaConfig{
		Enabled: true, Session: "session", Generation: 3, SSRC: 7,
		RTPListen: "127.0.0.1:5000", RTPDestination: "127.0.0.1:5001", Audio: compositionAudioConfig(),
	}}
	handler, cleanup, err := composeAPI(&hostOnlyCompositionService{}, config, nil,
		withTargetCast(&legacyCompositionTarget{}),
		withCaptureSourceFactory(func(fogcast.MediaConfig) (remotemedia.CaptureSource, error) {
			opened = true
			return &compositionCapture{}, nil
		}),
		withAudioSourceFactory(func(remotemedia.AudioSourceConfig) (remotemedia.AudioSource, error) {
			opened = true
			return &compositionAudioSource{}, nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/launch", strings.NewReader(`{"game_id":"game-1"}`))
	request.Host = "127.0.0.1"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code < 400 {
		t.Fatalf("legacy audio admission response = %d %s, want failure", response.Code, response.Body.String())
	}
	if opened {
		t.Fatal("source factory opened before legacy target admission rejection")
	}
}

func TestConfiguredCaptureSourceFactoryIsUsedByMediaComposition(t *testing.T) {
	service := &hostOnlyCompositionService{}
	called := false
	capture := &compositionCapture{}
	config := fogcast.Config{Token: "token", Media: fogcast.MediaConfig{Enabled: true, Session: "session", SSRC: 7, RTPListen: "127.0.0.1:5000", RTPDestination: "127.0.0.1:5001", CaptureDevice: "screen"}}
	handler, cleanup, err := composeAPI(service, config, nil,
		withCaptureSourceFactory(func(got fogcast.MediaConfig) (remotemedia.CaptureSource, error) {
			called = got.CaptureDevice == "screen"
			return capture, nil
		}),
		withTargetCast(&compositionTargetCast{}),
		withManagedSenderOptions(remotemedia.WithManagedSenderFactory(func(_ remotemedia.SenderConfig, source remotemedia.CaptureSource) (remotemedia.ManagedSenderRunner, error) {
			return &compositionRunner{done: make(chan struct{}), source: source}, nil
		})),
	)
	if err != nil || cleanup == nil || called {
		t.Fatalf("composition err=%v cleanup=%v called=%v", err, cleanup != nil, called)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/launch", strings.NewReader(`{"game_id":"game-1"}`))
	request.Host = "127.0.0.1"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code >= 500 || !called {
		t.Fatalf("media launch = %d %s called=%v", response.Code, response.Body.String(), called)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
}

func TestComposeAPICreatesFreshCaptureForEachHostMediaSession(t *testing.T) {
	service := &hostOnlyCompositionService{}
	var captures []*compositionCapture
	config := fogcast.Config{Token: "token", Media: fogcast.MediaConfig{Enabled: true, Session: "session", SSRC: 7, RTPListen: "127.0.0.1:5000", RTPDestination: "127.0.0.1:5001", CaptureDevice: "screen", Decoder: "none"}}
	handler, cleanup, err := composeAPI(service, config, nil,
		withCaptureSourceFactory(func(fogcast.MediaConfig) (remotemedia.CaptureSource, error) {
			capture := &compositionCapture{}
			captures = append(captures, capture)
			return capture, nil
		}),
		withTargetCast(&compositionTargetCast{}),
		withManagedSenderOptions(remotemedia.WithManagedSenderFactory(func(_ remotemedia.SenderConfig, source remotemedia.CaptureSource) (remotemedia.ManagedSenderRunner, error) {
			return &compositionRunner{done: make(chan struct{}), source: source}, nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	for i := 0; i < 2; i++ {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/session/launch", strings.NewReader(`{"game_id":"game-1"}`))
		request.Host = "127.0.0.1"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code >= 500 {
			t.Fatalf("launch %d = %d %s", i+1, response.Code, response.Body.String())
		}
	}
	if len(captures) != 2 {
		t.Fatalf("capture instances = %d, want 2", len(captures))
	}
	for i, capture := range captures {
		if got := capture.closeCount(); got != 1 {
			t.Fatalf("capture %d close count = %d, want 1", i+1, got)
		}
	}
}

type compositionDirectMediaSession struct {
	handle *compositionDirectMediaHandle
	err    error
}

func (s compositionDirectMediaSession) Start(context.Context, string) (hostapi.MediaHandle, error) {
	return s.handle, s.err
}

type compositionDirectMediaHandle struct {
	mu        sync.Mutex
	done      chan struct{}
	stopped   int
	stopErr   error
	blockStop bool
}

func (h *compositionDirectMediaHandle) Stop(ctx context.Context) error {
	h.mu.Lock()
	h.stopped++
	block := h.blockStop
	err := h.stopErr
	h.mu.Unlock()
	if block {
		<-ctx.Done()
		return ctx.Err()
	}
	return err
}
func (h *compositionDirectMediaHandle) Done() <-chan struct{} { return h.done }
func (h *compositionDirectMediaHandle) stopCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stopped
}

func TestCompositionMediaHandleReportsTargetCastTermination(t *testing.T) {
	local := &compositionDirectMediaHandle{done: make(chan struct{})}
	target := &compositionTargetCast{}
	session := newCompositionMediaSession(compositionDirectMediaSession{handle: local}, target, "session", "token", 9)
	handle, err := session.Start(context.Background(), "game")
	if err != nil {
		t.Fatal(err)
	}
	target.setState("idle")
	select {
	case <-handle.(interface{ Done() <-chan struct{} }).Done():
	case <-time.After(time.Second):
		t.Fatal("target termination was not reported")
	}
}

func TestCompositionMediaHandleRetriesFailedTargetStopAndRetainsOwnership(t *testing.T) {
	local := &compositionDirectMediaHandle{done: make(chan struct{})}
	target := &compositionTargetCast{}
	session := newCompositionMediaSession(compositionDirectMediaSession{handle: local}, target, "session", "token", 9)
	handle, err := session.Start(context.Background(), "game")
	if err != nil {
		t.Fatal(err)
	}
	stopErr := errors.New("target stop failed")
	target.setStopError(stopErr)
	if err := handle.Stop(context.Background()); !errors.Is(err, stopErr) {
		t.Fatalf("first stop = %v, want %v", err, stopErr)
	}
	if session.handle == nil {
		t.Fatal("failed stop cleared composition ownership")
	}
	if err := handle.Stop(context.Background()); !errors.Is(err, stopErr) {
		t.Fatalf("repeated stop = %v, want %v", err, stopErr)
	}
	if got := local.stopCount(); got != 1 {
		t.Fatalf("local stop count = %d, want 1", got)
	}
	target.setStopError(nil)
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("retry after target recovery = %v", err)
	}
	if session.handle != nil {
		t.Fatal("successful retry retained composition ownership")
	}
}

func TestCompositionMediaHandleBoundsTargetStop(t *testing.T) {
	local := &compositionDirectMediaHandle{done: make(chan struct{})}
	target := &compositionTargetCast{blockStop: true}
	session := newCompositionMediaSession(compositionDirectMediaSession{handle: local}, target, "session", "token", 9)
	handle, err := session.Start(context.Background(), "game")
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := handle.Stop(context.Background()); err == nil {
		t.Fatal("blocked target stop unexpectedly succeeded")
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("target stop took %v, want bounded cleanup", elapsed)
	}
}

func TestCompositionMediaHandleGivesTargetFreshDeadlineAfterLocalTimeout(t *testing.T) {
	local := &compositionDirectMediaHandle{done: make(chan struct{}), blockStop: true}
	target := &compositionTargetCast{}
	session := newCompositionMediaSession(compositionDirectMediaSession{handle: local}, target, "session", "token", 9)
	handle, err := session.Start(context.Background(), "game")
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Stop(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stop = %v, want local deadline failure", err)
	}
	_, stopped := target.counts()
	if stopped != 1 {
		t.Fatalf("target stop count = %d, want 1 after local timeout", stopped)
	}
}

type compositionFailingMediaSession struct{ err error }

func (s compositionFailingMediaSession) Start(context.Context, string) (hostapi.MediaHandle, error) {
	return nil, s.err
}

func TestCompositionStartReturnsRetryableOwnershipWhenTargetRollbackFails(t *testing.T) {
	target := &compositionTargetCast{stopErr: errors.New("rollback failed")}
	session := newCompositionMediaSession(compositionFailingMediaSession{err: errors.New("local start failed")}, target, "session", "token", 9)
	handle, err := session.Start(context.Background(), "game")
	if err == nil || handle == nil {
		t.Fatalf("Start = handle:%v err:%v, want retryable handle and start error", handle != nil, err)
	}
	target.setStopError(nil)
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("retry cleanup = %v", err)
	}
	if session.handle != nil {
		t.Fatal("successful retry retained cleanup ownership")
	}
}

func TestCompositionMediaHandleToleratesTransientTargetStatusFailure(t *testing.T) {
	local := &compositionDirectMediaHandle{done: make(chan struct{})}
	target := &compositionTargetCast{}
	session := newCompositionMediaSession(compositionDirectMediaSession{handle: local}, target, "session", "token", 9)
	handle, err := session.Start(context.Background(), "game")
	if err != nil {
		t.Fatal(err)
	}
	target.setStatusError(errors.New("temporary status failure"))
	time.Sleep(2 * targetStatusInterval)
	target.setStatusError(nil)
	select {
	case <-handle.(interface{ Done() <-chan struct{} }).Done():
		t.Fatal("transient status failure closed Done")
	case <-time.After(2 * targetStatusInterval):
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCompositionMediaHandleReportsPersistentTargetStatusFailure(t *testing.T) {
	local := &compositionDirectMediaHandle{done: make(chan struct{})}
	target := &compositionTargetCast{}
	session := newCompositionMediaSession(compositionDirectMediaSession{handle: local}, target, "session", "token", 9)
	handle, err := session.Start(context.Background(), "game")
	if err != nil {
		t.Fatal(err)
	}
	target.setStatusError(errors.New("persistent status failure"))
	select {
	case <-handle.(interface{ Done() <-chan struct{} }).Done():
	case <-time.After(time.Second):
		t.Fatal("persistent target status failure did not close Done")
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

func TestCompositionCloseRetriesTargetCleanupAndReportsFailure(t *testing.T) {
	local := &compositionDirectMediaHandle{done: make(chan struct{})}
	target := &compositionTargetCast{stopErr: errors.New("target cleanup failed")}
	session := newCompositionMediaSession(compositionDirectMediaSession{handle: local}, target, "session", "token", 9)
	if _, err := session.Start(context.Background(), "game"); err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); err == nil {
		t.Fatal("Close reported success after target cleanup failures")
	}
	_, stopped := target.counts()
	if stopped != 2 {
		t.Fatalf("target stop count = %d, want initial attempt plus bounded retry", stopped)
	}
}

func TestCompositionPartialStartRetainsLocalAndTargetCleanupOwnership(t *testing.T) {
	local := &compositionDirectMediaHandle{done: make(chan struct{})}
	target := &compositionTargetCast{stopErr: errors.New("target cleanup failed")}
	session := newCompositionMediaSession(compositionDirectMediaSession{
		handle: local,
		err:    errors.New("partial local start failed"),
	}, target, "session", "token", 9)
	handle, err := session.Start(context.Background(), "game")
	if err == nil || handle == nil {
		t.Fatalf("Start = handle %v err %v, want retryable partial ownership", handle, err)
	}
	target.setStopError(nil)
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("retry cleanup = %v", err)
	}
	if local.stopCount() != 1 {
		t.Fatalf("local partial stop count = %d, want 1", local.stopCount())
	}
	_, stopped := target.counts()
	if stopped != 2 {
		t.Fatalf("target stop count = %d, want failed rollback plus retry", stopped)
	}
}

func TestManagedSenderComponentRetainsFailedCallerOwnedCaptureCleanup(t *testing.T) {
	capture := &compositionCapture{closeErrs: []error{errors.New("first close failed")}}
	component := &managedSenderComponent{
		media: fogcast.MediaConfig{
			Session: "session", Generation: 9, SSRC: 7,
			RTPDestination: "127.0.0.1:5001", ControlAddress: "127.0.0.1:5002",
			Bitrate: 1_000_000, GOP: 30, MTU: 1200,
		},
		token: "token",
		newSources: func(fogcast.MediaConfig) (remotemedia.CaptureSource, remotemedia.AudioSource, error) {
			return capture, nil, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	handle, err := component.Start(ctx, "game")
	if err == nil || handle == nil {
		t.Fatalf("Start = handle:%v err:%v, want retryable capture ownership", handle != nil, err)
	}
	if got := capture.closeCount(); got != 1 {
		t.Fatalf("initial close count = %d, want 1", got)
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("retry cleanup = %v", err)
	}
	if got := capture.closeCount(); got != 2 {
		t.Fatalf("retry close count = %d, want 2", got)
	}
}

func TestManagedSenderComponentPreservesCaptureAuthorizationError(t *testing.T) {
	captureErr := errors.New("AVFoundation Camera authorization is not determined; grant Camera access to the FogCast helper")
	component := &managedSenderComponent{
		media: fogcast.MediaConfig{Session: "session", Generation: 1, SSRC: 7},
		token: "token",
		newSources: func(fogcast.MediaConfig) (remotemedia.CaptureSource, remotemedia.AudioSource, error) {
			return nil, nil, captureErr
		},
	}
	_, err := component.Start(context.Background(), "game")
	if err == nil || !errors.Is(err, captureErr) || !strings.Contains(err.Error(), "Camera authorization") {
		t.Fatalf("Start error = %v, want wrapped authorization error", err)
	}
}

func TestManagedSenderComponentRejectsMissingSourceFactory(t *testing.T) {
	component := &managedSenderComponent{}
	if _, err := component.Start(context.Background(), "game"); err == nil {
		t.Fatal("missing media source factory was accepted")
	}
}

func TestCompositionPartialLocalHandleSurvivesSuccessfulTargetRollback(t *testing.T) {
	local := &compositionDirectMediaHandle{done: make(chan struct{})}
	target := &compositionTargetCast{}
	session := newCompositionMediaSession(compositionDirectMediaSession{
		handle: local, err: errors.New("partial local start failed"),
	}, target, "session", "token", 9)
	handle, err := session.Start(context.Background(), "game")
	if err == nil || handle == nil || session.handle == nil {
		t.Fatalf("Start = handle:%v owned:%v err:%v", handle != nil, session.handle != nil, err)
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := local.stopCount(); got != 1 {
		t.Fatalf("local cleanup count = %d, want 1", got)
	}
	_, stopped := target.counts()
	if stopped != 1 {
		t.Fatalf("target stop count = %d, want rollback only", stopped)
	}
}

func TestCompositionFailedTargetStartRetainsFailedRollbackOwnership(t *testing.T) {
	target := &compositionTargetCast{
		startErr: errors.New("partial target start failed"),
		stopErr:  errors.New("target rollback failed"),
	}
	session := newCompositionMediaSession(compositionFailingMediaSession{}, target, "session", "token", 9)
	handle, err := session.Start(context.Background(), "game")
	if err == nil || handle == nil || session.handle == nil {
		t.Fatalf("Start = handle:%v owned:%v err:%v", handle != nil, session.handle != nil, err)
	}
	target.setStopError(nil)
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("retry target rollback = %v", err)
	}
	_, stopped := target.counts()
	if stopped != 2 {
		t.Fatalf("target stop count = %d, want failed rollback plus retry", stopped)
	}
}

func TestCompositionMismatchedStartIdentityRollsBackRequestedIdentity(t *testing.T) {
	target := &compositionTargetCast{
		reportSession: "foreign", reportGeneration: 10,
		stopErr: errors.New("requested identity rollback failed"),
	}
	session := newCompositionMediaSession(compositionFailingMediaSession{}, target, "requested", "token", 9)
	handle, err := session.Start(context.Background(), "game")
	if err == nil || handle == nil || session.handle == nil {
		t.Fatalf("Start = handle:%v owned:%v err:%v", handle != nil, session.handle != nil, err)
	}
	target.setStopError(nil)
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("retry requested-identity rollback = %v", err)
	}
	_, stopped := target.counts()
	if stopped != 2 {
		t.Fatalf("target stop count = %d, want immediate rollback plus retry", stopped)
	}
}

func TestCompositionOldGenerationCannotStopReplacementTarget(t *testing.T) {
	local := &compositionDirectMediaHandle{done: make(chan struct{})}
	target := &compositionTargetCast{}
	session := newCompositionMediaSession(compositionDirectMediaSession{handle: local}, target, "old", "token", 9)
	handle, err := session.Start(context.Background(), "game")
	if err != nil {
		t.Fatal(err)
	}
	target.setIdentity("replacement", 10)
	select {
	case <-handle.(interface{ Done() <-chan struct{} }).Done():
	case <-time.After(time.Second):
		t.Fatal("replacement identity was not terminal for old generation")
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("old generation local cleanup = %v", err)
	}
	target.mu.Lock()
	state, sessionID, generation := target.state, target.session, target.generation
	target.mu.Unlock()
	if state != "active" || sessionID != "replacement" || generation != 10 {
		t.Fatalf("replacement changed: state=%q session=%q generation=%d", state, sessionID, generation)
	}
}

func TestCompositionHandleStopsTheKitItStartedAfterRebind(t *testing.T) {
	local := &compositionDirectMediaHandle{done: make(chan struct{})}
	original := &compositionTargetCast{}
	session := newCompositionMediaSession(compositionDirectMediaSession{handle: local}, original, "session", "token", 9)
	handle, err := session.Start(context.Background(), "game")
	if err != nil {
		t.Fatal(err)
	}
	session.SetCastTarget(fogcast.TargetConfig{Name: "spare", Address: "http://192.0.2.11:8182", Agent: "token-b"})
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("stop = %v", err)
	}
	started, stopped := original.counts()
	if started != 1 || stopped != 1 {
		t.Fatalf("original cast started %d stopped %d", started, stopped)
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
			name:   "malformed target",
			config: fogcast.Config{MetadataRoot: base.MetadataRoot, Metadata: base.Metadata, BaseURL: "://bad", Media: fogcast.MediaConfig{Enabled: true}},
			setup: []compositionOption{withCaptureSourceFactory(func(fogcast.MediaConfig) (remotemedia.CaptureSource, error) {
				return &compositionCapture{}, nil
			})},
		},
		{
			name:   "direct receiver construction",
			config: fogcast.Config{MetadataRoot: base.MetadataRoot, Metadata: base.Metadata, Token: "token", Media: fogcast.MediaConfig{Enabled: true, RTPListen: "127.0.0.1:5000"}},
			setup: []compositionOption{withCaptureSourceFactory(func(fogcast.MediaConfig) (remotemedia.CaptureSource, error) {
				return &compositionCapture{}, nil
			})},
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
