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
	"github.com/DeanoC/FogCast/internal/appliancedata"
	"github.com/DeanoC/FogCast/internal/applianceupdate"
	"github.com/DeanoC/FogCast/internal/buildinputs"
	"github.com/DeanoC/FogCast/internal/cast"

	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/DeanoC/FogCast/internal/flightdiag"
	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/input"
	"github.com/DeanoC/FogCast/internal/kitcontent"
	"github.com/DeanoC/FogCast/internal/kitlease"

	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/internal/targetcache"
	"github.com/DeanoC/FogCast/internal/version"
	"github.com/DeanoC/FogCast/protocol"
)

const (
	targetCacheRoot         = "/media/fat/fogcast/cache"
	targetCacheActiveRecord = "/run/fogcast-active.json"
	meshContentRoot         = "/media/fat/fogcast/mesh-content"
	launcherConfigPath      = "/media/fat/fogcast/launcher.json"
	installedPackageRoot    = "/usr/share/mister-runtime/core-packages"
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

type runDependencies struct {
	prepareDataPartition  func() error
	openCache             func(targetcache.Config, ...targetcache.Option) (agent.ContentStore, error)
	newRuntime            func(agentconfig.Config) agent.Runtime
	configureRuntime      func(agent.Runtime, httpapi.InputController) error
	newInput              func(agentconfig.Config) (httpapi.InputController, error)
	inputBeforeInitialize bool
	newCast               func(agentconfig.Config) (httpapi.CastController, error)
	serve                 func(*http.Server) error
	advertise             func(context.Context, string, int) error
	targetIDPath          string
	meshContentRoot       string
	// packageRoots are the directories the node document scans for
	// described packages. Nil uses the installed and development roots.
	packageRoots []string
	newUpdate    func(*agent.Coordinator, func(context.Context) error) (*applianceupdate.Service, error)
}

func run(ctx context.Context, configPath string, logger *slog.Logger) error {
	dependencies, err := productionRunDependencies()
	if err != nil {
		return err
	}
	return runWithDependencies(ctx, configPath, logger, dependencies)
}

func productionRunDependencies() (runDependencies, error) {
	return runtimeDependencies(nil)
}

func runtimeDependencies(nativeControl misterruntime.Control) (runDependencies, error) {
	dependencies := runDependencies{
		openCache: func(config targetcache.Config, options ...targetcache.Option) (agent.ContentStore, error) {
			return targetcache.Open(config, options...)
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
		serve:           func(server *http.Server) error { return server.ListenAndServe() },
		advertise:       discovery.Advertise,
		targetIDPath:    targetIDFile,
		meshContentRoot: meshContentRoot,
	}
	if nativeControl == nil {
		nativeControl = misterruntime.NewClient(misterruntime.DefaultSocketPath)
	}
	dependencies.newRuntime = func(agentconfig.Config) agent.Runtime {
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
		if ports, ok := controller.(interface {
			ConfigureControllerPorts(func(context.Context) (*input.ControllerBinding, error), input.ControllerPoster)
			ObserveCore(func(context.Context) (input.CoreObservation, error))
		}); ok {
			ports.ObserveCore(func(ctx context.Context) (input.CoreObservation, error) {
				return observeRuntimeInput(ctx, nativeRuntime)
			})
			ports.ConfigureControllerPorts(func(ctx context.Context) (*input.ControllerBinding, error) {
				obs, err := observeRuntimeInput(ctx, nativeRuntime)
				if err != nil {
					return nil, err
				}
				return obs.Binding, nil
			}, func(ctx context.Context, packageID string, generation uint64, port, buttons uint8, keypad uint16) error {
				if ctx == nil {
					ctx = context.Background()
				}
				// Host posts arrive without a deadline. Keep that 2s bound.
				// A kit-local frame already carries localCoreWriteTimeout;
				// WithTimeout keeps the sooner of the two.
				ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
				defer cancel()
				return nativeRuntime.SetController(ctx, misterruntime.ControllerRequest{PackageID: packageID, Generation: generation, Port: port, Buttons: buttons, Keypad: keypad})
			})
		}
		if keys, ok := controller.(interface {
			SetKeyboardPoster(func(context.Context, uint64) error)
		}); ok {
			keys.SetKeyboardPoster(func(ctx context.Context, matrix uint64) error {
				if ctx == nil {
					ctx = context.Background()
				}
				err := nativeRuntime.SetKeyboard(ctx, matrix)
				var apiErr *protocol.APIError
				if errors.As(err, &apiErr) && apiErr.Code == protocol.CodeUnsupportedOperation {
					// Neutralize is a no-op when no simple-computer core is loaded.
					return nil
				}
				return err
			})
		}
		return nil
	}
	dependencies.inputBeforeInitialize = true
	dependencies.newUpdate = loadApplianceUpdate
	return dependencies, nil
}

func bindApplianceData(logger *slog.Logger) error {
	result, err := appliancedata.Prepare(appliancedata.ProductionConfig())
	if err != nil {
		logger.Error("appliance data partition bind failed", "error", err, "skipped", result.Skipped)
		return nil
	}
	if result.Skipped != "" {
		logger.Info("appliance data partition bind skipped", "reason", result.Skipped, "device", result.Device)
		return nil
	}
	if len(result.Bound) > 0 {
		logger.Info("appliance data partition bound", "device", result.Device, "paths", result.Bound)
	}
	return nil
}

func observeRuntimeInput(ctx context.Context, runtime *misterruntime.Runtime) (input.CoreObservation, error) {
	status, err := runtime.ControllerStatus(ctx)
	if err != nil {
		return input.CoreObservation{}, err
	}
	var ports, keypad, keyboard bool
	for _, contract := range status.Capabilities.ActiveInterfaces {
		if contract.Major != 1 || contract.Minor != 0 {
			continue
		}
		switch contract.ID {
		case "fes.gamepad.ports":
			ports = true
		case "fes.keypad.ports":
			keypad = true
		case "fes.keyboard":
			keyboard = true
		}
	}
	active := status.OK && status.State == "running_development" && status.ActivePackage != nil && status.Generation != nil && *status.Generation != 0
	observation := input.CoreObservation{Active: active, Keyboard: keyboard}
	if !ports {
		return observation, nil
	}
	if !active {
		return observation, errors.New("controller package is not active")
	}
	observation.Binding = &input.ControllerBinding{
		PackageID:  status.ActivePackage.PackageID,
		Generation: *status.Generation,
		Keypad:     keypad,
	}
	return observation, nil
}

func newNativeRuntime(control misterruntime.Control, rebootPath string) *misterruntime.Runtime {
	_ = os.MkdirAll(developmentCoreRoot, 0o700)
	// The fixed target data root outlives package staging and image updates.
	// The runtime validates storage access and refuses launch/update if this failed.
	_ = os.MkdirAll(misterruntime.CoreDataRoot, 0o700)
	return misterruntime.NewRuntime(control, bootIDFile, 25*time.Millisecond, 250*time.Millisecond,
		misterruntime.WithDevelopmentRBFPath(developmentRBFPath),
		misterruntime.WithCorePackageRoot(developmentCoreRoot),
		misterruntime.WithRebootCommand(rebootPath))
}

func runWithDependencies(ctx context.Context, configPath string, logger *slog.Logger, dependencies runDependencies) (resultErr error) {
	cfg, err := agentconfig.Load(configPath)
	if err != nil {
		if _, retired := agentconfig.RetiredSettingsMessage(err); retired {
			return err
		}
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
	if dependencies.prepareDataPartition == nil {
		dependencies.prepareDataPartition = func() error {
			return bindApplianceData(logger)
		}
	}
	if err := dependencies.prepareDataPartition(); err != nil {
		logger.Error("appliance data partition bind failed", "error", err)
	}
	cache, err := dependencies.openCache(targetcache.Config{
		Root:         targetCacheRoot,
		ActiveRecord: targetCacheActiveRecord,
		MaxBytes:     cfg.CacheMaxBytes,
	}, targetcache.WithLogger(logger))
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
	runtime := dependencies.newRuntime(cfg)
	if configurable, ok := runtime.(interface{ ConfigureDiagnostics(flightdiag.Sink) }); ok {
		configurable.ConfigureDiagnostics(diagnostics)
	}
	if dependencies.configureRuntime != nil {
		if err := dependencies.configureRuntime(runtime, inputController); err != nil {
			return errors.New("native input replacement barrier could not be configured")
		}
	}
	if feed, ok := inputController.(*input.TargetController); ok && feed != nil {
		go func() {
			err := feed.ServeLocalInput(ctx, input.DefaultLocalInputSocket)
			if err != nil && ctx.Err() == nil && !errors.Is(err, net.ErrClosed) {
				logger.Error("kit-local input feed stopped", "error", err)
			}
		}()
	}
	coordinator := agent.New(runtime, 10*time.Second, 5*time.Second,
		agent.WithCoreLoadTimeout(60*time.Second),
		agent.WithOperationContext(ctx), agent.WithEventSink(diagnostics),
		agent.WithArtifacts(buildinputs.Snapshot(buildinputs.Paths{}, version.Revision)))
	content := agent.NewContentController(coordinator, cache)
	startup, cancel := context.WithTimeout(ctx, 40*time.Second)
	coordinator.Initialize(startup)
	cancel()
	options := []httpapi.Option{httpapi.WithContent(content), httpapi.WithDevelopment(coordinator)}
	if dependencies.meshContentRoot != "" && targetID != "" {
		// The kit store is served for this node's id. Pull and link are
		// kit-lease mutations and their bodies stay empty. mesh_content
		// defaults on: the source reads the host through the provisioned
		// launcher credential. The node document reads installed and
		// staged package manifests on each request, so a stage published
		// after startup is included and a removed stage is not.
		// mesh_content = false keeps a nil source and an empty ABI list.
		var source kitcontent.Source
		if cfg.MeshContent {
			opened, sourceErr := kitcontent.OpenLauncherSource(launcherConfigPath)
			switch {
			case sourceErr == nil:
				source = opened
			case errors.Is(sourceErr, os.ErrNotExist):
			default:
				logger.Error("mesh content source unavailable", "error", sourceErr)
			}
		}
		meshStore, meshErr := kitcontent.Open(dependencies.meshContentRoot, targetID, source, nil)
		if meshErr != nil {
			logger.Error("mesh content store unavailable", "error", meshErr)
		} else {
			if cfg.MeshContent {
				roots := dependencies.packageRoots
				if roots == nil {
					roots = []string{installedPackageRoot, developmentCoreRoot}
				}
				meshStore.SetPackageRoots(roots)
			}
			options = append(options, httpapi.WithMeshContent(meshStore))
		}
	}
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
		var probe func(context.Context) bool
		if native, ok := runtime.(*misterruntime.Runtime); ok {
			probe = native.RuntimeSocketOpen
		}
		cleanupErr := kitLeaseCleanup(cleanup, probe, 2*time.Second, cleanupPeripherals, coordinator.Stop)
		if errors.Is(cleanupErr, kitlease.ErrRuntimeUnreachable) {
			slog.Warn("kit lease cleanup: runtime socket unavailable; releasing lease")
		} else if cleanupErr != nil {
			slog.Error("kit lease cleanup failed", "err", cleanupErr)
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
	flag.Parse()
	if flag.NArg() != 0 {
		_, _ = fmt.Fprintln(os.Stderr, "usage: mister-agent [--config path]")
		os.Exit(2)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, *configPath, logger); err != nil {
		message := "startup failed"
		if migration, ok := agentconfig.RetiredSettingsMessage(err); ok {
			message = migration
		}
		_, _ = fmt.Fprintln(os.Stderr, "mister-agent: "+message)
		os.Exit(1)
	}
}
