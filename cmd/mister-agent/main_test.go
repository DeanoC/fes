package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/agentconfig"
	"github.com/DeanoC/FogCast/internal/cast"
	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/input"
	"github.com/DeanoC/FogCast/internal/mister"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/internal/targetcache"
	"github.com/DeanoC/FogCast/protocol"
)

func TestRunDoesNotExposeMalformedConfigurationContents(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "agent.toml")
	content := "token = \"real-secret-token\"\nthis is not valid TOML\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	err := run(context.Background(), path, runtimeMain, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err == nil {
		t.Fatal("malformed configuration was accepted")
	}
	if strings.Contains(err.Error(), "real-secret-token") {
		t.Fatalf("configuration content leaked in error: %v", err)
	}
}

func TestLoadOrCreateTargetIDPersistsAndReusesIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fogcast", "target-id")
	first, err := loadOrCreateTargetID(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if !discovery.ValidID(first) {
		t.Fatalf("ID = %q", first)
	}
	second, err := loadOrCreateTargetID(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatalf("second ID = %q, want %q", second, first)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}

func TestLoadOrCreateTargetIDRejectsMalformedPersistedIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "target-id")
	if err := os.WriteFile(path, []byte("not-an-id\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOrCreateTargetID(path, ""); err == nil {
		t.Fatal("malformed persisted ID accepted")
	}
}

type compositionRuntime struct {
	reconciled      bool
	developmentSize int64
	developmentBody []byte
}

func (*compositionRuntime) Health(string) protocol.Health {
	return protocol.Health{Ready: true, MiSTerProcess: true, CommandPipe: true}
}

func (r *compositionRuntime) Reconcile(context.Context) protocol.Status {
	r.reconciled = true
	return protocol.Status{State: protocol.StateIdle}
}

func (*compositionRuntime) Prepare(core.Spec, string) (mister.PreparedLaunch, *protocol.APIError) {
	return mister.PreparedLaunch{}, &protocol.APIError{Code: protocol.CodeInternal, Message: "unused"}
}

func (*compositionRuntime) Launch(context.Context, mister.PreparedLaunch) (string, bool, *protocol.APIError) {
	return "", false, &protocol.APIError{Code: protocol.CodeInternal, Message: "unused"}
}

func (r *compositionRuntime) LoadDevelopmentRBF(_ context.Context, size int64, content io.Reader) (string, bool, *protocol.APIError) {
	body, err := io.ReadAll(content)
	if err != nil {
		return "", false, &protocol.APIError{Code: protocol.CodeInternal, Message: "test reader failed"}
	}
	r.developmentSize = size
	r.developmentBody = append([]byte(nil), body...)
	return "DEVCORE", true, nil
}

func (r *compositionRuntime) RecoverDevelopment(context.Context) (string, *protocol.APIError) {
	return "MENU", nil
}

func (*compositionRuntime) Stop(context.Context) (string, *protocol.APIError) {
	return "MENU", nil
}

type compositionStore struct {
	reconciled bool
}

type compositionInput struct {
	closed int
}

type compositionCast struct {
	stopped      bool
	stopErrors   []error
	stopContexts []context.Context
}

func (*compositionCast) Start(context.Context, string, string, uint64) error { return nil }
func (c *compositionCast) Stop(ctx context.Context, _ string, _ uint64) error {
	c.stopped = true
	c.stopContexts = append(c.stopContexts, ctx)
	if len(c.stopErrors) != 0 {
		err := c.stopErrors[0]
		c.stopErrors = c.stopErrors[1:]
		return err
	}
	return nil
}
func (*compositionCast) Status(context.Context) cast.Status {
	return cast.Status{State: cast.Active, Session: "session", Generation: 9}
}

func (*compositionInput) ReleaseAll(context.Context) error         { return nil }
func (*compositionInput) Attach(context.Context, input.Spec) error { return nil }
func (*compositionInput) Detach(context.Context, uint64) error     { return nil }
func (*compositionInput) OpenStream(context.Context, uint64) (net.Conn, error) {
	return nil, errors.New("unused")
}
func (c *compositionInput) Close() error { c.closed++; return nil }

func (*compositionStore) Probe(context.Context, protocol.System, protocol.ContentKey) (protocol.CacheProbeResponse, *protocol.APIError) {
	return protocol.CacheProbeResponse{Present: false}, nil
}

func (*compositionStore) Put(context.Context, protocol.System, protocol.ContentIdentity, io.Reader) (protocol.CacheUploadResponse, *protocol.APIError) {
	return protocol.CacheUploadResponse{}, &protocol.APIError{Code: protocol.CodeInternal, Message: "unused"}
}

func (*compositionStore) Resolve(context.Context, protocol.System, protocol.ContentIdentity) (targetcache.Resolved, *protocol.APIError) {
	return targetcache.Resolved{}, &protocol.APIError{Code: protocol.CodeContentNotCached, Message: "not cached"}
}

func (*compositionStore) PinForLaunch(protocol.System, protocol.ContentIdentity) *protocol.APIError {
	return &protocol.APIError{Code: protocol.CodeInternal, Message: "unused"}
}

func (*compositionStore) RecordLaunchIntent(protocol.System, protocol.ContentIdentity) (targetcache.LaunchIntent, *protocol.APIError) {
	return targetcache.LaunchIntent{}, &protocol.APIError{Code: protocol.CodeInternal, Message: "unused"}
}

func (*compositionStore) RecordDirectLaunchIntent(protocol.System) (targetcache.LaunchIntent, *protocol.APIError) {
	return targetcache.LaunchIntent{}, &protocol.APIError{Code: protocol.CodeInternal, Message: "unused"}
}

func (*compositionStore) AbortLaunch(protocol.System, protocol.ContentIdentity, targetcache.LaunchIntent) *protocol.APIError {
	return nil
}

func (*compositionStore) AbortDirectLaunch(protocol.System, targetcache.LaunchIntent) *protocol.APIError {
	return nil
}

func (*compositionStore) CommitDirectLaunch(protocol.System, targetcache.LaunchIntent) *protocol.APIError {
	return &protocol.APIError{Code: protocol.CodeInternal, Message: "unused"}
}

func (*compositionStore) CommitLaunch(protocol.System, protocol.ContentIdentity) *protocol.APIError {
	return &protocol.APIError{Code: protocol.CodeInternal, Message: "unused"}
}

func (*compositionStore) ClearActive() *protocol.APIError { return nil }

func (*compositionStore) ActiveRecordSystems(context.Context) (targetcache.ActiveRecords, bool, *protocol.APIError) {
	return targetcache.ActiveRecords{}, false, nil
}

func (s *compositionStore) ReconcileActive(context.Context, protocol.Status, *targetcache.ActiveRecordEntry) *protocol.APIError {
	s.reconciled = true
	return nil
}

func TestRunComposesFixedCacheContentHandlerAndUploadTimeouts(t *testing.T) {
	const targetID = "01234567-89ab-cdef-0123-456789abcdef"
	configPath := writeCompositionConfig(t, "cache_max_bytes = 67108864\ntarget_id = \""+targetID+"\"\n")
	configBytes, configErr := os.ReadFile(configPath)
	if configErr != nil {
		t.Fatal(configErr)
	}
	configBytes = []byte(strings.Replace(string(configBytes), "127.0.0.1:8182", "0.0.0.0:8182", 1))
	if err := os.WriteFile(configPath, configBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := &compositionRuntime{}
	store := &compositionStore{}
	var opened targetcache.Config
	listened := false
	advertised := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps := runDependencies{
		openCache: func(config targetcache.Config, registry core.Registry, options ...targetcache.Option) (agent.ContentStore, error) {
			opened = config
			if _, ok := registry.Lookup(protocol.SystemSNES); !ok {
				t.Fatal("cache opener did not receive the target registry")
			}
			if len(options) != 1 {
				t.Fatalf("cache options = %d, want logger only", len(options))
			}
			return store, nil
		},
		newRuntime: func(agentconfig.Config, core.Registry) agent.Runtime { return runtime },
		advertise: func(ctx context.Context, id string, port int) error {
			if id != targetID || port != 8182 {
				t.Errorf("advertisement = %q:%d", id, port)
			}
			close(advertised)
			return errors.New("multicast unavailable")
		},
		serve: func(server *http.Server) error {
			listened = true
			select {
			case <-advertised:
			case <-time.After(time.Second):
				t.Fatal("advertisement did not start")
			}
			if server.ReadHeaderTimeout.String() != "2s" || server.ReadTimeout.String() != "1m15s" || server.WriteTimeout.String() != "1m15s" {
				t.Fatalf("server timeouts = header %s read %s write %s", server.ReadHeaderTimeout, server.ReadTimeout, server.WriteTimeout)
			}
			healthRequest := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
			healthRequest.Header.Set("Authorization", "Bearer test-token")
			healthResponse := httptest.NewRecorder()
			server.Handler.ServeHTTP(healthResponse, healthRequest)
			if !strings.Contains(healthResponse.Body.String(), `"target_id":"`+targetID+`"`) {
				t.Fatalf("authenticated health = %s", healthResponse.Body.String())
			}
			digest := strings.Repeat("a", 64)
			request := httptest.NewRequest(http.MethodGet, "/v2/cache/snes/"+digest+"?extension=sfc", nil)
			request.Header.Set("Authorization", "Bearer test-token")
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK || response.Body.String() != "{\"present\":false}\n" {
				t.Fatalf("content route = HTTP %d %q", response.Code, response.Body.String())
			}
			var kitToken string
			claimDeadline := time.Now().Add(time.Second)
			for kitToken == "" {
				claim := httptest.NewRequest(http.MethodPost, "/v1/kit/claim", strings.NewReader(`{"request_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","owner":"test","purpose":"development"}`))
				claim.Header.Set("Authorization", "Bearer test-token")
				result := httptest.NewRecorder()
				server.Handler.ServeHTTP(result, claim)
				var grant struct {
					Token string `json:"token"`
				}
				_ = json.Unmarshal(result.Body.Bytes(), &grant)
				kitToken = grant.Token
				if kitToken == "" && time.Now().After(claimDeadline) {
					t.Fatalf("claim: %d %s", result.Code, result.Body.String())
				}
				if kitToken == "" {
					time.Sleep(time.Millisecond)
				}
			}
			payload := []byte("development-rbf")
			request = httptest.NewRequest(http.MethodPost, "/v1/development/rbf", strings.NewReader(string(payload)))
			request.ContentLength = int64(len(payload))
			request.Header.Set("Authorization", "Bearer test-token")
			request.Header.Set("Content-Type", "application/octet-stream")
			request.Header.Set(httpapi.KitLeaseHeader, kitToken)
			response = httptest.NewRecorder()
			server.Handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK || rbfStatus(response.Body.Bytes()).State != protocol.StateActive {
				t.Fatalf("development route = HTTP %d %q", response.Code, response.Body.String())
			}
			cancel()
			return http.ErrServerClosed
		},
	}
	if err := runWithDependencies(ctx, configPath, slog.New(slog.NewJSONHandler(io.Discard, nil)), deps); err != nil {
		t.Fatal(err)
	}
	if opened.Root != "/media/fat/fogcast/cache" || opened.ActiveRecord != "/run/fogcast-active.json" || opened.MaxBytes != 67108864 {
		t.Fatalf("cache config = %#v", opened)
	}
	if !runtime.reconciled || !store.reconciled || !listened {
		t.Fatalf("startup runtime=%v store=%v listened=%v", runtime.reconciled, store.reconciled, listened)
	}
	if runtime.developmentSize != int64(len("development-rbf")) || string(runtime.developmentBody) != "development-rbf" {
		t.Fatalf("development runtime = size %d body %q", runtime.developmentSize, runtime.developmentBody)
	}
}

func TestDiscoveryListenerUsesConfiguredPortAndSkipsLoopback(t *testing.T) {
	for _, tc := range []struct {
		address   string
		port      int
		advertise bool
	}{{"0.0.0.0:9191", 9191, true}, {"[::]:8282", 8282, true}, {"127.0.0.1:8182", 8182, false}, {"[::1]:8182", 8182, false}} {
		port, advertise := discoveryListener(tc.address)
		if port != tc.port || advertise != tc.advertise {
			t.Errorf("discoveryListener(%q) = (%d, %t), want (%d, %t)", tc.address, port, advertise, tc.port, tc.advertise)
		}
	}
}

func TestAdvertisementCleanupCancelsAndJoinsWorker(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	cleanup := startAdvertisement(context.Background(), "01234567-89ab-cdef-0123-456789abcdef", 9191, slog.New(slog.NewTextHandler(io.Discard, nil)), func(ctx context.Context, _ string, _ int) error {
		close(started)
		<-ctx.Done()
		close(stopped)
		return ctx.Err()
	})
	<-started
	cleanup()
	select {
	case <-stopped:
	default:
		t.Fatal("cleanup returned before advertisement worker stopped")
	}
}

func rbfStatus(body []byte) protocol.Status {
	var status protocol.Status
	_ = json.Unmarshal(body, &status)
	return status
}

func TestRunComposesTargetInputController(t *testing.T) {
	configPath := writeCompositionConfig(t, "")
	runtime := &compositionRuntime{}
	store := &compositionStore{}
	inputController := &compositionInput{}
	seenInput := false
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps := runDependencies{
		openCache: func(targetcache.Config, core.Registry, ...targetcache.Option) (agent.ContentStore, error) {
			return store, nil
		},
		newRuntime: func(agentconfig.Config, core.Registry) agent.Runtime { return runtime },
		newInput: func(agentconfig.Config) (httpapi.InputController, error) {
			if !runtime.reconciled {
				t.Fatal("Main input controller was constructed before runtime initialization")
			}
			seenInput = true
			return inputController, nil
		},
		serve: func(*http.Server) error { cancel(); return http.ErrServerClosed },
	}
	if err := runWithDependencies(ctx, configPath, slog.New(slog.NewJSONHandler(io.Discard, nil)), deps); err != nil {
		t.Fatal(err)
	}
	if !seenInput {
		t.Fatal("target input controller was not composed")
	}
	if inputController.closed != 1 {
		t.Fatalf("target input close calls = %d, want 1", inputController.closed)
	}
}

func TestNativeRunCreatesGamepadBeforeRuntimeAndClosesItAfterServing(t *testing.T) {
	configPath := writeCompositionConfig(t, "")
	inputController := &compositionInput{}
	store := &compositionStore{}
	var order []string
	ctx, cancel := context.WithCancel(context.Background())
	deps := runDependencies{
		openCache: func(targetcache.Config, core.Registry, ...targetcache.Option) (agent.ContentStore, error) {
			order = append(order, "cache")
			return store, nil
		},
		newInput: func(agentconfig.Config) (httpapi.InputController, error) {
			order = append(order, "input")
			return inputController, nil
		},
		inputBeforeInitialize: true,
		newRuntime: func(agentconfig.Config, core.Registry) agent.Runtime {
			order = append(order, "runtime")
			return &compositionRuntime{}
		},
		serve: func(*http.Server) error {
			order = append(order, "serve")
			if inputController.closed != 0 {
				t.Fatal("native gamepad was closed before agent shutdown")
			}
			cancel()
			return http.ErrServerClosed
		},
	}
	if err := runWithDependencies(ctx, configPath, slog.New(slog.NewJSONHandler(io.Discard, nil)), deps); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(order, ","); got != "cache,input,runtime,serve" {
		t.Fatalf("native startup order = %q", got)
	}
	if inputController.closed != 1 {
		t.Fatalf("native gamepad close calls = %d, want 1", inputController.closed)
	}
}

func TestRunStopsCastControllerOnShutdown(t *testing.T) {
	configPath := writeCompositionConfig(t, "cast_binary = \"/tmp/fbbridge\"\ncast_rtp_address = \":5534\"\ncast_control_address = \":5535\"\ncast_framebuffer = \"/dev/fb0\"\ncast_native_cmd = \"/dev/MiSTer_cmd\"\ncast_native_mode = \"8888 1 1920 1080\"\ncast_token_file = \"/tmp/cast-token\"\ncast_generation = 9\n")
	runtime := &compositionRuntime{}
	store := &compositionStore{}
	castController := &compositionCast{}
	ctx, cancel := context.WithCancel(context.Background())
	deps := runDependencies{
		openCache: func(targetcache.Config, core.Registry, ...targetcache.Option) (agent.ContentStore, error) {
			return store, nil
		},
		newRuntime: func(agentconfig.Config, core.Registry) agent.Runtime { return runtime },
		newCast: func(agentconfig.Config) (httpapi.CastController, error) {
			return castController, nil
		},
		serve: func(*http.Server) error {
			cancel()
			return http.ErrServerClosed
		},
	}
	if err := runWithDependencies(ctx, configPath, slog.New(slog.NewJSONHandler(io.Discard, nil)), deps); err != nil {
		t.Fatal(err)
	}
	if !castController.stopped {
		t.Fatal("cast controller was not stopped on agent shutdown")
	}
}

func TestRunRetriesCastShutdownAndReturnsStableFailure(t *testing.T) {
	configPath := writeCompositionConfig(t, "cast_binary = \"/tmp/fbbridge\"\ncast_rtp_address = \":5534\"\ncast_control_address = \":5535\"\ncast_framebuffer = \"/dev/fb0\"\ncast_native_cmd = \"/dev/MiSTer_cmd\"\ncast_native_mode = \"8888 1 1920 1080\"\ncast_token_file = \"/tmp/cast-token\"\ncast_generation = 9\n")
	castController := &compositionCast{}
	ctx, cancel := context.WithCancel(context.Background())
	deps := runDependencies{
		openCache: func(targetcache.Config, core.Registry, ...targetcache.Option) (agent.ContentStore, error) {
			return &compositionStore{}, nil
		},
		newRuntime: func(agentconfig.Config, core.Registry) agent.Runtime { return &compositionRuntime{} },
		newCast:    func(agentconfig.Config) (httpapi.CastController, error) { return castController, nil },
		serve: func(server *http.Server) error {
			waitForKitStartup(t, server.Handler)
			castController.stopContexts = nil
			castController.stopErrors = []error{errors.New("private-token first failure"), nil}
			cancel()
			return http.ErrServerClosed
		},
	}
	err := runWithDependencies(ctx, configPath, slog.New(slog.NewJSONHandler(io.Discard, nil)), deps)
	if err == nil || err.Error() != "cast controller could not be stopped" {
		t.Fatalf("shutdown error = %v, want stable cast shutdown failure", err)
	}
	if strings.Contains(err.Error(), "private-token") {
		t.Fatalf("shutdown detail leaked: %v", err)
	}
	if len(castController.stopContexts) != 2 {
		t.Fatalf("stop calls = %d, want 2", len(castController.stopContexts))
	}
	for i, stopCtx := range castController.stopContexts {
		if _, ok := stopCtx.Deadline(); !ok {
			t.Fatalf("stop context %d has no bounded deadline", i)
		}
	}
	if castController.stopContexts[0] == castController.stopContexts[1] {
		t.Fatal("cast shutdown retry reused the original cleanup context")
	}
}

func TestRunCacheInventoryFailureAbortsBeforeListening(t *testing.T) {
	configPath := writeCompositionConfig(t, "")
	privateDetail := filepath.Join(t.TempDir(), "private-cache-entry")
	listened := false
	deps := runDependencies{
		openCache: func(config targetcache.Config, _ core.Registry, _ ...targetcache.Option) (agent.ContentStore, error) {
			if config.Root != "/media/fat/fogcast/cache" || config.ActiveRecord != "/run/fogcast-active.json" || config.MaxBytes != 2<<30 {
				t.Fatalf("cache config = %#v", config)
			}
			return nil, errors.New(privateDetail)
		},
		newRuntime: func(agentconfig.Config, core.Registry) agent.Runtime {
			t.Fatal("runtime constructed after cache inventory failure")
			return nil
		},
		serve: func(*http.Server) error {
			listened = true
			return nil
		},
	}
	err := runWithDependencies(context.Background(), configPath, slog.New(slog.NewJSONHandler(io.Discard, nil)), deps)
	if err == nil || listened {
		t.Fatalf("inventory failure err=%v listened=%v", err, listened)
	}
	if strings.Contains(err.Error(), privateDetail) {
		t.Fatalf("cache inventory detail leaked: %v", err)
	}
}

func TestProductionCacheDependencyCreatesPrivateInventory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cache")
	dependencies, err := productionRunDependencies(runtimeMain)
	if err != nil {
		t.Fatal(err)
	}
	store, err := dependencies.openCache(targetcache.Config{
		Root:         root,
		ActiveRecord: filepath.Join(t.TempDir(), "run", "fogcast-active.json"),
		MaxBytes:     64 << 20,
	}, core.DefaultRegistry(), targetcache.WithLogger(slog.New(slog.NewJSONHandler(io.Discard, nil))))
	if err != nil {
		t.Fatal(err)
	}
	if store == nil {
		t.Fatal("production cache opener returned a nil store")
	}
	for _, directory := range []string{root, filepath.Join(root, "megadrive"), filepath.Join(root, "snes")} {
		info, err := os.Stat(directory)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Fatalf("cache directory %q = %v, err=%v, want requested directory mode 0700 on regular filesystem", filepath.Base(directory), info, err)
		}
	}
}

func TestProductionRuntimeBackendDefaultsToMain(t *testing.T) {
	t.Parallel()
	dependencies, err := productionRunDependencies(runtimeMain)
	if err != nil {
		t.Fatal(err)
	}
	runtime := dependencies.newRuntime(agentconfig.Config{}, core.NewRegistry())
	if _, ok := runtime.(*mister.Runtime); !ok {
		t.Fatalf("default runtime = %T, want *mister.Runtime", runtime)
	}
}

func TestProductionRuntimeBackendSelectsNativeOnlyWhenExplicit(t *testing.T) {
	t.Parallel()
	mainDependencies, err := productionRunDependencies(runtimeMain)
	if err != nil {
		t.Fatal(err)
	}
	nativeDependencies, err := productionRunDependencies(runtimeNative)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := mainDependencies.newRuntime(agentconfig.Config{}, core.NewRegistry()).(*mister.Runtime); !ok {
		t.Fatal("main backend did not compose Main runtime")
	}
	if _, ok := nativeDependencies.newRuntime(agentconfig.Config{}, core.NewRegistry()).(*misterruntime.Runtime); !ok {
		t.Fatal("native backend did not compose native runtime")
	}
	if mainDependencies.inputBeforeInitialize {
		t.Fatal("Main backend moved input construction before runtime initialization")
	}
	if !nativeDependencies.inputBeforeInitialize {
		t.Fatal("native backend did not require gamepad construction before runtime initialization")
	}
}

func TestNativeCompositionWiresExplicitRecoveryCommand(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	marker := filepath.Join(dir, "started")
	reboot := filepath.Join(dir, "reboot")
	if err := os.WriteFile(reboot, []byte("#!/bin/sh\n: > '"+marker+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	runtime := newNativeRuntime(&idleCompositionControl{}, reboot)
	if _, apiErr := runtime.RecoverDevelopment(context.Background()); apiErr != nil {
		t.Fatalf("recovery error = %#v", apiErr)
	}
	deadline := time.Now().Add(250 * time.Millisecond)
	for {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("explicit recovery command was not started")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestProductionRuntimeBackendRejectsUnknownValueWithoutFallback(t *testing.T) {
	t.Parallel()
	if _, err := productionRunDependencies(runtimeBackend("automatic")); err == nil {
		t.Fatal("unknown runtime backend was accepted")
	}
	err := run(context.Background(), filepath.Join(t.TempDir(), "missing-config.toml"), runtimeBackend("automatic"), slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err == nil || err.Error() != "target runtime backend is invalid" {
		t.Fatalf("run error = %v", err)
	}
}

type idleCompositionControl struct {
	statusCalls      int
	stopCalls        int
	developmentCalls int
	developmentPath  string
}

func (c *idleCompositionControl) Status(context.Context) (misterruntime.Response, error) {
	c.statusCalls++
	return misterruntime.Response{Protocol: 1, OK: true, State: "idle", Execution: "none", Version: "test"}, nil
}

func (c *idleCompositionControl) Stop(context.Context) (misterruntime.Response, error) {
	c.stopCalls++
	return misterruntime.Response{Protocol: 1, OK: true, State: "idle", Execution: "none", Version: "test"}, nil
}

func (*idleCompositionControl) Launch(context.Context, misterruntime.LaunchRequest) (misterruntime.Response, error) {
	return misterruntime.Response{}, errors.New("unused")
}

func (c *idleCompositionControl) LoadDevelopmentRBF(_ context.Context, path string) (misterruntime.Response, error) {
	c.developmentCalls++
	c.developmentPath = path
	coreName := "MegaDrive"
	return misterruntime.Response{Protocol: 1, OK: true, State: "running_development", Execution: "development", Core: &coreName, Version: "test"}, nil
}

func TestNativeCompositionReportsIdleAndLoadsDevelopmentAtTheVolatilePath(t *testing.T) {
	if err := os.Remove(developmentRBFPath); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Remove(developmentRBFPath + ".new"); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Remove(developmentRBFPath)
		_ = os.Remove(developmentRBFPath + ".new")
	})
	control := &idleCompositionControl{}
	dependencies, err := runtimeDependencies(runtimeNative, control)
	if err != nil {
		t.Fatal(err)
	}
	runtime := dependencies.newRuntime(agentconfig.Config{}, core.NewRegistry())
	status := runtime.Reconcile(context.Background())
	if status.State != protocol.StateIdle || status.LastError != nil {
		t.Fatalf("status = %#v", status)
	}
	body := strings.NewReader("rbf")
	observed, attempted, apiErr := runtime.LoadDevelopmentRBF(context.Background(), 3, body)
	if observed != "MegaDrive" || !attempted || apiErr != nil {
		t.Fatalf("development = observed:%q attempted:%t error:%#v", observed, attempted, apiErr)
	}
	staged, err := os.ReadFile(developmentRBFPath)
	if err != nil || string(staged) != "rbf" {
		t.Fatalf("staged bytes = %q, error %v", staged, err)
	}
	if control.statusCalls != 2 || control.developmentCalls != 1 || control.developmentPath != developmentRBFPath || control.stopCalls != 0 {
		t.Fatalf("control calls = status:%d development:%d path:%q stop:%d", control.statusCalls, control.developmentCalls, control.developmentPath, control.stopCalls)
	}
}

func writeCompositionConfig(t *testing.T, extra string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.toml")
	content := `listen_address = "127.0.0.1:8182"
token = "test-token"
mister_process_comm = "MiSTer"
command_pipe = "/dev/MiSTer_cmd"
core_name_file = "/tmp/CORENAME"
menu_rbf = "/media/fat/menu.rbf"
mgl_directory = "/tmp/fogcast"
` + extra
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func waitForKitStartup(t *testing.T, handler http.Handler) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		request := httptest.NewRequest(http.MethodGet, "/v1/kit/lease", nil)
		request.Header.Set("Authorization", "Bearer test-token")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		var status struct {
			State string `json:"state"`
		}
		_ = json.Unmarshal(response.Body.Bytes(), &status)
		if status.State == "free" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("kit startup: %s", response.Body.String())
		}
		time.Sleep(time.Millisecond)
	}
}
