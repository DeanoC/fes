package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/agentconfig"
	"github.com/DeanoC/FogCast/internal/applianceupdate"
	"github.com/DeanoC/FogCast/internal/cast"
	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/DeanoC/FogCast/internal/flightdiag"
	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/input"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/internal/mister"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/internal/targetcache"
	"github.com/DeanoC/FogCast/internal/version"
	"github.com/DeanoC/FogCast/protocol"
)

const (
	targetCacheRoot         = "/media/fat/fogcast/cache"
	targetCacheActiveRecord = "/run/fogcast-active.json"
	developmentRBFPath      = "/tmp/fogcast-development/core.rbf"
	developmentCoreRoot     = "/tmp/fogcast-development/core-packages"
	targetIDFile            = "/media/fat/fogcast/target-id"
	rebootCommand           = "/sbin/reboot"
	bootIDFile              = "/proc/sys/kernel/random/boot_id"
	castShutdownTimeout     = 2 * time.Second
	// Release staging accepts multi-gigabyte raw images; this exceeds the
	// operator's five-minute transport timeout by a bounded margin while the
	// header timeout still limits slow request setup.
	applianceRequestTimeout = 6 * time.Minute
)

var errCastShutdown = errors.New("cast controller could not be stopped")

type runtimeBackend string

const (
	runtimeMain   runtimeBackend = "main"
	runtimeNative runtimeBackend = "native"
)

type runDependencies struct {
	openCache             func(targetcache.Config, core.Registry, ...targetcache.Option) (agent.ContentStore, error)
	newRuntime            func(agentconfig.Config, core.Registry) agent.Runtime
	configureRuntime      func(agent.Runtime, httpapi.InputController) error
	newInput              func(agentconfig.Config) (httpapi.InputController, error)
	inputBeforeInitialize bool
	newCast               func(agentconfig.Config) (httpapi.CastController, error)
	serve                 func(*http.Server) error
	advertise             func(context.Context, string, int) error
	targetIDPath          string
	newUpdate             func(*agent.Coordinator, func(context.Context) error) (*applianceupdate.Service, error)
}

func run(ctx context.Context, configPath string, backend runtimeBackend, logger *slog.Logger) error {
	dependencies, err := productionRunDependencies(backend)
	if err != nil {
		return err
	}
	return runWithDependencies(ctx, configPath, logger, dependencies)
}

func productionRunDependencies(backend runtimeBackend) (runDependencies, error) {
	return runtimeDependencies(backend, nil)
}

func runtimeDependencies(backend runtimeBackend, nativeControl misterruntime.Control) (runDependencies, error) {
	if backend != runtimeMain && backend != runtimeNative {
		return runDependencies{}, errors.New("target runtime backend is invalid")
	}
	dependencies := runDependencies{
		openCache: func(config targetcache.Config, registry core.Registry, options ...targetcache.Option) (agent.ContentStore, error) {
			return targetcache.Open(config, registry, options...)
		},
		newInput: func(cfg agentconfig.Config) (httpapi.InputController, error) {
			return input.NewTargetControllerWithConfig(cfg.InputListenAddress, cfg.InputUInputPath), nil
		},
		newCast: func(cfg agentconfig.Config) (httpapi.CastController, error) {
			return cast.New(cast.Config{
				Binary: cfg.CastBinary, RTPAddress: cfg.CastRTPAddress, Control: cfg.CastControlAddress,
				Framebuffer: cfg.CastFramebuffer, NativeCmd: cfg.CastNativeCmd, NativeMode: cfg.CastNativeMode,
				TokenFile:   cfg.CastTokenFile,
				Generation:  cfg.CastGeneration,
				StopTimeout: 2 * time.Second,
			}, nil)
		},
		serve:        func(server *http.Server) error { return server.ListenAndServe() },
		advertise:    discovery.Advertise,
		targetIDPath: targetIDFile,
	}
	if backend == runtimeMain {
		dependencies.newRuntime = func(cfg agentconfig.Config, registry core.Registry) agent.Runtime {
			paths := mister.Paths{
				MiSTerProcessComm: cfg.MiSTerProcessComm,
				CommandPipe:       cfg.CommandPipe,
				CoreNameFile:      cfg.CoreNameFile,
				BootIDFile:        bootIDFile,
				MenuRBF:           cfg.MenuRBF,
				MGLDirectory:      cfg.MGLDirectory,
				DevelopmentRBF:    developmentRBFPath,
				RebootCommand:     rebootCommand,
			}
			return mister.NewRuntime(paths, registry, &mister.FileCommandWriter{Path: cfg.CommandPipe}, mister.ProcProcessChecker{Root: "/proc"}, 25*time.Millisecond)
		}
		return dependencies, nil
	}
	if nativeControl == nil {
		nativeControl = misterruntime.NewClient(misterruntime.DefaultSocketPath)
	}
	dependencies.newRuntime = func(agentconfig.Config, core.Registry) agent.Runtime {
		return newNativeRuntime(nativeControl, rebootCommand)
	}
	dependencies.newInput = func(cfg agentconfig.Config) (httpapi.InputController, error) {
		return input.NewNativeTargetControllerWithConfig(cfg.InputListenAddress, cfg.InputUInputPath)
	}
	dependencies.configureRuntime = func(runtime agent.Runtime, controller httpapi.InputController) error {
		nativeRuntime, ok := runtime.(*misterruntime.Runtime)
		if !ok {
			return errors.New("native runtime cannot accept the input replacement barrier")
		}
		barrier, ok := controller.(misterruntime.CoreReplacementBarrier)
		if !ok {
			return errors.New("native input controller cannot fence core replacement")
		}
		nativeRuntime.ConfigureCoreReplacementBarrier(barrier)
		return nil
	}
	dependencies.inputBeforeInitialize = true
	dependencies.newUpdate = loadApplianceUpdate
	return dependencies, nil
}

func newNativeRuntime(control misterruntime.Control, rebootPath string) *misterruntime.Runtime {
	_ = os.MkdirAll(developmentCoreRoot, 0o700)
	// The fixed target data root outlives package staging and image updates.
	// The runtime validates storage access and refuses launch/update if this failed.
	_ = os.MkdirAll(misterruntime.CoreDataRoot, 0o700)
	return misterruntime.NewRuntime(control, bootIDFile, 25*time.Millisecond, 250*time.Millisecond,
		misterruntime.WithDevelopmentRBFPath(developmentRBFPath),
		misterruntime.WithCorePackageRoot(developmentCoreRoot),
		misterruntime.WithSaveRoot("/media/fat/fogcast/saves/snes"),
		misterruntime.WithRebootCommand(rebootPath))
}

func runWithDependencies(ctx context.Context, configPath string, logger *slog.Logger, dependencies runDependencies) (resultErr error) {
	cfg, err := agentconfig.Load(configPath)
	if err != nil {
		return errors.New("target configuration could not be loaded")
	}
	targetID := cfg.TargetID
	if dependencies.targetIDPath != "" {
		targetID, err = loadOrCreateTargetID(dependencies.targetIDPath, cfg.TargetID)
		if err != nil {
			logger.Error("target discovery disabled", "error", err)
			targetID = ""
		}
	}
	registry := core.DefaultRegistry()
	cache, err := dependencies.openCache(targetcache.Config{
		Root:         targetCacheRoot,
		ActiveRecord: targetCacheActiveRecord,
		MaxBytes:     cfg.CacheMaxBytes,
	}, registry, targetcache.WithLogger(logger))
	if err != nil {
		return errors.New("target cache could not be opened")
	}
	var inputController httpapi.InputController
	if dependencies.inputBeforeInitialize && dependencies.newInput != nil {
		inputController, err = dependencies.newInput(cfg)
		if err != nil || inputController == nil {
			return errors.New("remote input controller could not be configured")
		}
		defer inputController.Close()
	}
	diagnostics := flightdiag.NewRecorder(flightdiag.DefaultVaultRoot)
	runtime := dependencies.newRuntime(cfg, registry)
	if configurable, ok := runtime.(interface{ ConfigureDiagnostics(flightdiag.Sink) }); ok {
		configurable.ConfigureDiagnostics(diagnostics)
	}
	if dependencies.configureRuntime != nil {
		if err := dependencies.configureRuntime(runtime, inputController); err != nil {
			return errors.New("native input replacement barrier could not be configured")
		}
	}
	coordinator := agent.New(runtime, registry, 10*time.Second, 5*time.Second,
		agent.WithOperationContext(ctx), agent.WithEventSink(diagnostics))
	content := agent.NewContentController(coordinator, cache)
	startup, cancel := context.WithTimeout(ctx, 40*time.Second)
	coordinator.Initialize(startup)
	cancel()
	options := []httpapi.Option{httpapi.WithContent(content), httpapi.WithDevelopment(coordinator)}
	if targetID != "" {
		options = append(options, httpapi.WithTargetID(targetID))
	}
	var leaseCast httpapi.CastController
	if cfg.CastBinary != "" {
		if dependencies.newCast == nil {
			return errors.New("cast controller could not be configured")
		}
		castController, castErr := dependencies.newCast(cfg)
		if castErr != nil {
			return errors.New("cast controller could not be configured")
		}
		leaseCast = castController
		options = append(options, httpapi.WithCast(castController))
		defer func() {
			status := castController.Status(context.Background())
			if status.State != cast.Active {
				return
			}
			shutdown, cancel := context.WithTimeout(context.Background(), castShutdownTimeout)
			stopErr := castController.Stop(shutdown, status.Session, status.Generation)
			cancel()
			if stopErr == nil {
				return
			}

			retry, retryCancel := context.WithTimeout(context.Background(), castShutdownTimeout)
			_ = castController.Stop(retry, status.Session, status.Generation)
			retryCancel()
			resultErr = errCastShutdown
		}()
	}
	if !dependencies.inputBeforeInitialize && dependencies.newInput != nil {
		inputController, err = dependencies.newInput(cfg)
		if err != nil || inputController == nil {
			return errors.New("remote input controller could not be configured")
		}
		defer inputController.Close()
	}
	if inputController != nil {
		options = append(options, httpapi.WithInput(inputController))
	}
	cleanupPeripherals := func(cleanup context.Context) error {
		var cleanupErr error
		if inputController != nil {
			if releaser, ok := inputController.(interface{ ReleaseAll(context.Context) error }); ok {
				cleanupErr = errors.Join(cleanupErr, releaser.ReleaseAll(cleanup))
			} else {
				cleanupErr = errors.Join(cleanupErr, errors.New("input controller cannot release kit ownership"))
			}
		}
		if leaseCast != nil {
			status := leaseCast.Status(cleanup)
			if status.State == cast.Active {
				cleanupErr = errors.Join(cleanupErr, leaseCast.Stop(cleanup, status.Session, status.Generation))
			}
		}
		return cleanupErr
	}
	if dependencies.newUpdate != nil {
		updater, updateErr := dependencies.newUpdate(coordinator, cleanupPeripherals)
		if updateErr != nil {
			return errors.New("appliance boot identity could not be verified")
		}
		if updater != nil {
			options = append(options, httpapi.WithUpdate(updater))
		}
	}
	leases := kitlease.New(90*time.Second, func(cleanup context.Context) error {
		cleanupErr := cleanupPeripherals(cleanup)
		status, stopErr := coordinator.Stop(cleanup)
		if stopErr != nil || status.State != protocol.StateIdle {
			cleanupErr = errors.Join(cleanupErr, errors.New("kit runtime did not become idle"))
		}
		return cleanupErr
	}, kitlease.WithEventSink(diagnostics))
	defer leases.Close()
	options = append(options, httpapi.WithKitLease(leases), httpapi.WithDiagnostics(diagnostics))
	handler := httpapi.New(coordinator, cfg.Token, version.Version, logger, options...)
	server := &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           handler,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       applianceRequestTimeout,
		WriteTimeout:      applianceRequestTimeout,
		IdleTimeout:       30 * time.Second,
	}
	advertisePort, advertiseOnLAN := discoveryListener(cfg.ListenAddress)
	if targetID != "" && dependencies.advertise != nil && advertiseOnLAN {
		stopAdvertisement := startAdvertisement(ctx, targetID, advertisePort, logger, dependencies.advertise)
		defer stopAdvertisement()
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	err = dependencies.serve(server)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func startAdvertisement(parent context.Context, id string, port int, logger *slog.Logger, advertise func(context.Context, string, int) error) func() {
	ctx, cancel := context.WithCancel(parent)
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		if err := advertise(ctx, id, port); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("target discovery advertisement stopped", "error", err)
		}
	}()
	return func() {
		cancel()
		workers.Wait()
	}
}

func discoveryListener(address string) (int, bool) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return 0, false
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return 0, false
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return port, false
	}
	return port, true
}

func main() {
	configPath := flag.String("config", "/media/fat/fogcast/agent.toml", "target configuration path")
	runtimeValue := flag.String("runtime", string(runtimeMain), "target runtime backend (main or native)")
	flag.Parse()
	backend := runtimeBackend(*runtimeValue)
	if flag.NArg() != 0 || (backend != runtimeMain && backend != runtimeNative) {
		_, _ = fmt.Fprintln(os.Stderr, "usage: mister-agent [--config path] [--runtime main|native]")
		os.Exit(2)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, *configPath, backend, logger); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "mister-agent: startup failed")
		os.Exit(1)
	}
}
