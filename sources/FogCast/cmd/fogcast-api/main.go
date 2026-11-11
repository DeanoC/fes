// Command fogcast-api serves the local FogCast application API for FogCast clients.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/internal/kithost"
	"github.com/DeanoC/FogCast/internal/mediasession"
	"github.com/DeanoC/FogCast/internal/metadata"
	"github.com/DeanoC/FogCast/internal/remotemedia"
	"github.com/DeanoC/FogCast/storage"
	"github.com/DeanoC/FogCast/targetclient"
)

type service interface {
	hostapi.Service
	Close() error
}

type shutdownOwnership interface {
	ShutdownCleanupRequired() bool
}

type openService func(context.Context, fogcast.Paths) (service, error)
type composeAPIFunc func(service, fogcast.Config, bridgeStarterFactory) (http.Handler, func() error, error)

type bridgeStarterFactory func(fogcast.Config) (host.BridgeStarter, error)

type captureSourceFactory func(fogcast.MediaConfig) (remotemedia.CaptureSource, error)
type metadataOpener func(context.Context, metadata.RuntimeConfig) (metadata.Runtime, error)
type compositionOption func(*compositionDeps)

const (
	targetCleanupTimeout   = 2 * time.Second
	folderWatchStopTimeout = 2 * time.Second
	folderWatchLogInterval = time.Second
)

var errFolderWatchStopTimeout = errors.New("folder-watch stop timed out")

func kitLibraryScan(ctx context.Context, candidate service, stderr io.Writer) {
	scanner, ok := candidate.(interface {
		Scan(context.Context) (catalog.ScanReport, error)
	})
	if !ok {
		return
	}
	if _, err := scanner.Scan(ctx); err != nil && ctx.Err() == nil {
		fmt.Fprintf(stderr, "fogcast-api: kit library scan failed: %v\n", err)
	}
}

// prepareKitHost is replaced by tests.
var prepareKitHost = kithost.Prepare

// stopFolderWatch cancels the watch loop and waits up to timeout. A stuck
// SMB Lstat is not interruptible; Service.Close still waits for an in-flight
// reconcile and will not close the catalog under a live scan.
func stopFolderWatch(cancel context.CancelFunc, done <-chan struct{}, timeout time.Duration) error {
	if cancel != nil {
		cancel()
	}
	if timeout <= 0 {
		timeout = folderWatchStopTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		return errFolderWatchStopTimeout
	}
}

func runFolderWatch(ctx context.Context, candidate service, stderr io.Writer, logInterval time.Duration) {
	runner, ok := candidate.(interface {
		RunFolderWatch(context.Context) error
	})
	if !ok {
		return
	}
	watchErrors := make(chan error, 1)
	go func() { watchErrors <- runner.RunFolderWatch(ctx) }()
	counter, reportsFailures := candidate.(interface {
		FolderWatchReconcileFailures() int
	})
	if !reportsFailures {
		reportFolderWatchTerminalError(ctx, stderr, <-watchErrors)
		return
	}
	if logInterval <= 0 {
		logInterval = folderWatchLogInterval
	}
	ticker := time.NewTicker(logInterval)
	defer ticker.Stop()
	lastFailures := 0
	for {
		select {
		case err := <-watchErrors:
			reportFolderWatchFailures(stderr, counter.FolderWatchReconcileFailures(), &lastFailures)
			reportFolderWatchTerminalError(ctx, stderr, err)
			return
		case <-ticker.C:
			reportFolderWatchFailures(stderr, counter.FolderWatchReconcileFailures(), &lastFailures)
		}
	}
}

func reportFolderWatchFailures(stderr io.Writer, failures int, lastFailures *int) {
	if failures <= *lastFailures {
		return
	}
	fmt.Fprintf(stderr, "fogcast-api: folder-watch reconciliation failures: %d\n", failures)
	*lastFailures = failures
}

func reportFolderWatchTerminalError(ctx context.Context, stderr io.Writer, err error) {
	if err != nil && ctx.Err() == nil {
		fmt.Fprintln(stderr, "fogcast-api: folder-watch failed")
	}
}

type compositionDeps struct {
	newCapture     captureSourceFactory
	newPreview     remotemedia.PreviewDecoderFactory
	previewHandler http.Handler
	openMetadata   metadataOpener
}

func withCaptureSourceFactory(factory captureSourceFactory) compositionOption {
	return func(deps *compositionDeps) { deps.newCapture = factory }
}

func withPreviewDecoderFactory(factory remotemedia.PreviewDecoderFactory) compositionOption {
	return func(deps *compositionDeps) { deps.newPreview = factory }
}

func withPreviewHandler(handler http.Handler) compositionOption {
	return func(deps *compositionDeps) { deps.previewHandler = handler }
}

func withMetadataOpener(opener metadataOpener) compositionOption {
	return func(deps *compositionDeps) { deps.openMetadata = opener }
}

func defaultCaptureSource(config fogcast.MediaConfig) (remotemedia.CaptureSource, error) {
	capture, err := remotemedia.OpenNativeCapture(remotemedia.CaptureConfig{
		Device: config.CaptureDevice, Width: config.Width, Height: config.Height, FPS: remotemedia.FrameRate{Numerator: config.FPSNumerator, Denominator: config.FPSDenominator}, Bitrate: config.Bitrate, GOP: config.GOP,
	})
	return capture, err
}

func defaultBridgeStarter(config fogcast.Config) (host.BridgeStarter, error) {
	baseURL, err := url.Parse(config.BaseURL)
	if err != nil {
		return nil, host.ErrRemoteInputInvalid
	}
	return host.NewHTTPBridgeStarter(host.HTTPBridgeStarterConfig{BaseURL: baseURL, Token: config.Token})
}

func composeAPI(service service, config fogcast.Config, makeStarter bridgeStarterFactory, options ...compositionOption) (http.Handler, func() error, error) {
	if service == nil {
		return nil, nil, errors.New("fogcast-api: composition dependency is unavailable")
	}
	deps := compositionDeps{newCapture: defaultCaptureSource, openMetadata: metadata.Open}
	for _, option := range options {
		if option != nil {
			option(&deps)
		}
	}
	var serverOptions []hostapi.ServerOption
	if config.StorageConfigPath != "" {
		serverOptions = append(serverOptions, hostapi.WithStorage(storage.New(config.StorageConfigPath)))
	}
	var cleanup []func() error
	metadataConfig := metadata.RuntimeConfig{
		Root: config.MetadataRoot, Configured: config.Metadata.Configured, Enabled: config.Metadata.Enabled,
		ProviderName: metadata.ProviderName(config.Metadata.Provider), ClientID: config.Metadata.ClientID,
		ClientSecret: config.Metadata.ClientSecret, Archive: config.Metadata.Archive,
	}
	if deps.openMetadata == nil {
		return nil, nil, errors.New("fogcast-api: metadata opener is unavailable")
	}
	metadataRuntime, err := deps.openMetadata(context.Background(), metadataConfig)
	if err != nil {
		return nil, nil, errors.New("fogcast-api: metadata configuration failed")
	}
	serverOptions = append(serverOptions, hostapi.WithMetadata(metadataRuntime, metadata.StateForConfig(metadata.RuntimeConfig{Configured: config.Metadata.Configured, Enabled: config.Metadata.Enabled})))
	if metadataRuntime != nil {
		// Metadata is appended before later media/input cleanup so the reverse
		// composition order stops those dependants before closing metadata.
		cleanup = append(cleanup, metadataRuntime.Close)
	}
	if config.Media.Enabled {
		if deps.newCapture == nil {
			_ = closeComposition(cleanup)
			return nil, nil, errors.New("fogcast-api: media capture source is unavailable")
		}
		preview := remotemedia.NewMJPEGPreview()
		decoderFactory := preview.Decoder
		previewHandler := http.Handler(preview)
		if deps.newPreview != nil {
			decoderFactory = deps.newPreview
		}
		if deps.previewHandler != nil {
			previewHandler = deps.previewHandler
		}
		component, previewErr := remotemedia.NewLocalPreview(func() (remotemedia.CaptureSource, error) {
			return deps.newCapture(config.Media)
		}, decoderFactory)
		if previewErr != nil {
			_ = closeComposition(cleanup)
			return nil, nil, errors.New("fogcast-api: media configuration failed")
		}
		mediaOwner := newCompositionMediaSession(hostapi.NewMediaSessionAdapter(mediasession.New(component, noopMediaComponent{})))
		cleanup = append(cleanup, mediaOwner.Close)
		serverOptions = append(serverOptions, hostapi.WithMediaSession(mediaOwner), hostapi.WithMediaPreview(previewHandler))
	}

	if !config.RemoteInput.Enabled {
		bindSessionTargetOrigin(service, nil)
		return hostapi.New(service, serverOptions...), func() error { return closeComposition(cleanup) }, nil
	}
	if makeStarter == nil {
		_ = closeComposition(cleanup)
		return nil, nil, errors.New("fogcast-api: bridge starter is unavailable")
	}
	starter, err := makeStarter(config)
	if err != nil {
		_ = closeComposition(cleanup)
		return nil, nil, errors.New("fogcast-api: remote input configuration failed")
	}
	var targetStarter *host.HTTPBridgeStarter
	if typed, ok := starter.(*host.HTTPBridgeStarter); ok {
		targetStarter = typed
		if provider, ok := service.(interface{ KitLease() *targetclient.KitLease }); ok {
			targetStarter.WithKitLease(provider.KitLease())
			targetStarter.WithKitLeaseSource(provider.KitLease)
		}
	}
	remoteInput, err := host.NewRemoteInput(host.RemoteInputConfig{Starter: starter})
	if err != nil {
		_ = closeComposition(cleanup)
		return nil, nil, errors.New("fogcast-api: remote input configuration failed")
	}
	var targetInputsMu sync.Mutex
	targetInputs := make(map[string][]*host.RemoteInput)
	targetInputFactory := func(targetName string) host.RemoteInputController {
		targetStarter, createErr := makeTargetBridgeStarter(config, targetName, service, makeStarter, starter)
		if createErr != nil {
			return nil
		}
		controller, createErr := host.NewRemoteInput(host.RemoteInputConfig{Starter: targetStarter})
		if createErr != nil {
			return nil
		}
		targetInputsMu.Lock()
		targetInputs[targetName] = append(targetInputs[targetName], controller)
		targetInputsMu.Unlock()
		return controller
	}
	if provider, ok := service.(interface{ SetTargetReset(func(string)) }); ok {
		provider.SetTargetReset(func(targetName string) {
			targetInputsMu.Lock()
			defer targetInputsMu.Unlock()
			for _, controller := range targetInputs[targetName] {
				controller.Invalidate()
			}
		})
	}
	bindSessionTargetOrigin(service, targetStarter)
	serverOptions = append(serverOptions, hostapi.WithRemoteInput(remoteInput), hostapi.WithRemoteInputFactory(targetInputFactory))
	return hostapi.New(service, serverOptions...), func() error {
		return closeAPIComposition(func() error {
			targetInputsMu.Lock()
			defer targetInputsMu.Unlock()
			var closeErr error
			for _, controllers := range targetInputs {
				for _, controller := range controllers {
					closeErr = errors.Join(closeErr, controller.Close())
				}
			}
			return errors.Join(closeErr, remoteInput.Close())
		}, cleanup)
	}, nil
}

func makeTargetBridgeStarter(config fogcast.Config, targetName string, service service, makeStarter bridgeStarterFactory, fallback host.BridgeStarter) (host.BridgeStarter, error) {
	if targetName == "" {
		return fallback, nil
	}
	targetConfig := config
	if resolver, ok := service.(interface {
		TargetConfigForName(string) fogcast.TargetConfig
	}); ok {
		if target := resolver.TargetConfigForName(targetName); target.Name != "" {
			targetConfig.BaseURL, targetConfig.Token = target.Address, target.Agent
		}
	}
	starter, err := makeStarter(targetConfig)
	if err != nil {
		return nil, err
	}
	if typed, ok := starter.(*host.HTTPBridgeStarter); ok {
		if provider, ok := service.(interface {
			KitLeaseForTarget(string) *targetclient.KitLease
		}); ok {
			typed.WithKitLeaseSource(func() *targetclient.KitLease { return provider.KitLeaseForTarget(targetName) })
		}
	}
	return starter, nil
}

func bindSessionTargetOrigin(service service, starter *host.HTTPBridgeStarter) {
	apply := func(target fogcast.TargetConfig, lease *targetclient.KitLease) {
		address := kitTargetAddress(target, lease)
		if starter != nil && address != "" {
			if parsed, err := url.Parse(address); err == nil {
				_ = starter.SetOrigin(parsed, target.Agent)
			}
		}
	}
	if origin, ok := service.(interface {
		SetTargetOrigin(func(fogcast.TargetConfig, *targetclient.KitLease))
	}); ok {
		origin.SetTargetOrigin(apply)
	}
	if selected, ok := service.(interface{ SelectedTargetConfig() fogcast.TargetConfig }); ok {
		var lease *targetclient.KitLease
		if provider, ok := service.(interface{ KitLease() *targetclient.KitLease }); ok {
			lease = provider.KitLease()
		}
		apply(selected.SelectedTargetConfig(), lease)
	}
}

// kitTargetAddress selects the verified endpoint for the input bridge.
// An adopted lease endpoint wins over the configured address.
func kitTargetAddress(target fogcast.TargetConfig, lease *targetclient.KitLease) string {
	if lease != nil {
		if endpoint := lease.Endpoint(); endpoint != nil && endpoint.Scheme != "" && endpoint.Host != "" {
			return endpoint.String()
		}
	}
	return strings.TrimSpace(target.Address)
}

type noopMediaComponent struct{}

func (noopMediaComponent) Start(context.Context, string) (mediasession.ComponentHandle, error) {
	return noopMediaHandle{}, nil
}

type noopMediaHandle struct{}

func (noopMediaHandle) Stop(context.Context) error { return nil }

// compositionMediaSession tracks preview handles so shutdown releases capture
// even when a session was not explicitly stopped.
type compositionMediaSession struct {
	media   hostapi.MediaSession
	mu      sync.Mutex
	handles []hostapi.MediaHandle
}

func newCompositionMediaSession(media hostapi.MediaSession) *compositionMediaSession {
	return &compositionMediaSession{media: media}
}

func (s *compositionMediaSession) Start(ctx context.Context, gameID string) (hostapi.MediaHandle, error) {
	handle, err := s.media.Start(ctx, gameID)
	if handle != nil {
		s.mu.Lock()
		s.handles = append(s.handles, handle)
		s.mu.Unlock()
	}
	return handle, err
}

func (s *compositionMediaSession) Close() error {
	s.mu.Lock()
	handles := append([]hostapi.MediaHandle(nil), s.handles...)
	s.mu.Unlock()
	var result error
	for _, handle := range handles {
		if err := handle.Stop(context.Background()); err != nil {
			result = errors.Join(result, err)
			_ = handle.Stop(context.Background())
		}
	}
	return result
}

func closeComposition(cleanup []func() error) error {
	var first error
	for i := len(cleanup) - 1; i >= 0; i-- {
		if err := cleanup[i](); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func closeAPIComposition(closeRemoteInput func() error, cleanup []func() error) error {
	var first error
	if err := closeRemoteInput(); err != nil {
		first = err
	}
	if err := closeComposition(cleanup); err != nil && first == nil {
		first = err
	}
	return first
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if filepath.Base(os.Args[0]) == kitBinaryName {
		os.Exit(kitMain(ctx, os.Args[1:], os.Stdout, os.Stderr, openFogcast))
	}
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr, openFogcast))
}

func openFogcast(ctx context.Context, paths fogcast.Paths) (service, error) {
	opened, err := fogcast.Open(ctx, paths, nil)
	if err != nil {
		return nil, err
	}
	opened.EnableMeshContent()
	opened.EnableMeshPlacement()
	return opened, nil
}

type runCloser struct {
	label string
	close func() error
}

func finishRun(code int, stderr io.Writer, closers ...runCloser) int {
	for i := len(closers) - 1; i >= 0; i-- {
		if closers[i].close != nil && closers[i].close() != nil {
			fmt.Fprintf(stderr, "fogcast-api: %s failed\n", closers[i].label)
			code = 1
		}
	}
	return code
}

func stopServiceForShutdown(service service) error {
	owner, ok := service.(shutdownOwnership)
	if !ok || !owner.ShutdownCleanupRequired() {
		return nil
	}
	var firstFailure error
	for stopped := 0; stopped < 64 && owner.ShutdownCleanupRequired(); stopped++ {
		var stopErr error
		for attempt := 0; attempt < 2; attempt++ {
			stopCtx, cancel := context.WithTimeout(context.Background(), targetCleanupTimeout)
			_, stopErr = service.Stop(stopCtx)
			if stopErr == nil {
				if releaser, ok := service.(interface{ ReleaseKitLease(context.Context) error }); ok {
					stopErr = releaser.ReleaseKitLease(stopCtx)
				}
			}
			cancel()
			if stopErr != nil && firstFailure == nil {
				firstFailure = stopErr
			}
			if stopErr == nil {
				break
			}
		}
		if stopErr != nil {
			return stopErr
		}
	}
	if owner.ShutdownCleanupRequired() {
		return errors.New("fogcast-api: shutdown cleanup did not settle all target sessions")
	}
	return firstFailure
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, open openService) int {
	return runWithComposer(ctx, args, stdout, stderr, open, func(service service, config fogcast.Config, makeStarter bridgeStarterFactory) (http.Handler, func() error, error) {
		return composeAPI(service, config, makeStarter)
	})
}

func runWithComposer(ctx context.Context, args []string, stdout, stderr io.Writer, open openService, compose composeAPIFunc) (exitCode int) {
	var closers []runCloser
	defer func() { exitCode = finishRun(exitCode, stderr, closers...) }()
	flags := flag.NewFlagSet("fogcast-api", flag.ContinueOnError)
	flags.SetOutput(stderr)
	listen := flags.String("listen", "127.0.0.1:8787", "loopback HTTP listen address")
	configPath := flags.String("config", "", "FogCast configuration path")
	launcherPath := flags.String("launcher-config", "", "private JSON configuration for the optional paired-kit listener")
	sourceListen := flags.String("source-listen", "", "optional read-only library source listen address")
	metadataConfigPath := flags.String("metadata-config", "", "FogCast configuration path supplying the metadata section")
	previewCaptureDevice := flags.String("preview-capture-device", "", "local capture device shown in the FPGA Play surface")
	headless := flags.Bool("headless", false, "API-only: do not start local capture or session preview")
	cpuProfile := flags.String("cpu-profile", "", "write CPU pprof to this file until exit")
	heapProfile := flags.String("heap-profile", "", "write heap/allocation pprof on SIGUSR1 and at exit")
	kitMode := flags.Bool("kit", false, "kit host: generate the config from the kit's agent config, keep state on FESDATA3, and drive only the kit's own agent")
	kitAgentConfig := flags.String("kit-agent-config", kithost.DefaultAgentConfig, "kit mode: the agent's config")
	kitTemplate := flags.String("kit-template", kithost.DefaultTemplate, "kit mode: the image's catalog config, the template for a first start")
	kitState := flags.String("kit-state", kithost.DefaultStateRoot, "kit mode: the state root, which must be on FESDATA3")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return 2
	}
	stopProfiles, err := startProfiles(ctx, *cpuProfile, *heapProfile, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "fogcast-api: profile: %v\n", err)
		return 1
	}
	defer stopProfiles()
	var launcherConfig *launcherListenerConfig
	if *launcherPath != "" {
		parsed, err := loadLauncherConfig(*launcherPath)
		if err != nil {
			if errors.Is(err, hostapi.ErrLauncherSharedToken) {
				fmt.Fprintln(stderr, "fogcast-api: launcher configuration load failed: "+err.Error())
				return 2
			}
			fmt.Fprintln(stderr, "fogcast-api: launcher configuration load failed")
			return 2
		}
		launcherConfig = &parsed
	}
	address, err := normalizeListenAddress(*listen)
	if err != nil {
		fmt.Fprintln(stderr, "fogcast-api: listen address must be loopback")
		return 2
	}
	if open == nil {
		fmt.Fprintln(stderr, "fogcast-api: service opener is unavailable")
		return 1
	}
	// An explicit custom config selects its own catalog stores. PathsForConfig
	// honors absolute state and staging in that file; the canonical user config
	// keeps default stores when a service overrides HOME.
	configFile := strings.TrimSpace(*configPath)
	if *kitMode {
		if configFile != "" {
			fmt.Fprintln(stderr, "fogcast-api: -kit generates its own configuration; do not pass -config")
			return 2
		}
		generated, err := prepareKitHost(ctx, kithost.Options{
			AgentConfig: *kitAgentConfig, Template: *kitTemplate, StateRoot: *kitState,
			Logf: func(format string, args ...any) { fmt.Fprintf(stderr, format+"\n", args...) },
		})
		if err != nil {
			fmt.Fprintf(stderr, "fogcast-api: %v\n", err)
			return 1
		}
		configFile = generated
	}
	var paths fogcast.Paths
	if configFile != "" {
		paths, err = fogcast.PathsForConfig(configFile)
		if err != nil {
			fmt.Fprintln(stderr, "fogcast-api: configuration paths are unavailable")
			return 1
		}
	} else {
		paths, err = fogcast.DefaultPaths()
		if err != nil {
			fmt.Fprintln(stderr, "fogcast-api: cannot determine default paths")
			return 1
		}
	}
	config, err := loadAPIConfig(paths.Config, *metadataConfigPath)
	config.StorageConfigPath = filepath.Join(filepath.Dir(paths.Config), "host-drives.json")
	if err != nil {
		fmt.Fprintln(stderr, "fogcast-api: configuration load failed")
		return 1
	}
	if *sourceListen != "" && !fogcast.ValidLibrarySourceToken(config.LibrarySourceToken) {
		fmt.Fprintln(stderr, "fogcast-api: library source listener requires a valid library_source_token in the configuration")
		return 2
	}
	if launcherConfig != nil {
		if launcherConfig.reusesAgentToken(config) {
			fmt.Fprintln(stderr, "fogcast-api: launcher requires a separate credential: a launcher bearer equals a configured target's agent token")
			return 2
		}
		config.RemoteInput.Enabled = true
	}
	applyPreviewCaptureDevice(&config, *previewCaptureDevice)
	if *headless {
		config.Media = fogcast.MediaConfig{}
	}
	config.MetadataRoot = paths.MetadataRoot
	fogcastService, err := open(ctx, paths)
	if err != nil || fogcastService == nil {
		fmt.Fprintln(stderr, "fogcast-api: service load failed")
		return 1
	}
	closers = append(closers, runCloser{label: "service cleanup", close: fogcastService.Close})
	watchCtx, watchCancel := context.WithCancel(ctx)
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		if *kitMode {
			// Kit mode turns the folder watch off, so this is its one
			// library scan. It runs here so the API serves meanwhile.
			kitLibraryScan(watchCtx, fogcastService, stderr)
		}
		runFolderWatch(watchCtx, fogcastService, stderr, folderWatchLogInterval)
	}()
	closers = append(closers, runCloser{label: "folder-watch stop", close: func() error {
		return stopFolderWatch(watchCancel, watchDone, folderWatchStopTimeout)
	}})
	handler, closeRemoteInput, err := compose(fogcastService, config, defaultBridgeStarter)
	if err != nil {
		fmt.Fprintln(stderr, "fogcast-api: API composition failed")
		return 1
	}
	closers = append(closers,
		runCloser{label: "session cleanup", close: func() error {
			if *kitMode {
				return nil
			}
			return stopServiceForShutdown(fogcastService)
		}},
		runCloser{label: "API cleanup", close: closeRemoteInput},
	)

	listener, err := net.Listen("tcp", address)
	if err != nil {
		fmt.Fprintln(stderr, "fogcast-api: listen failed")
		return 1
	}
	defer listener.Close()

	mux := http.NewServeMux()
	mux.Handle("/", hostapi.UIHandler())
	mux.Handle("/api/", handler)
	server := &http.Server{
		Handler:           mux,
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ReadHeaderTimeout: 5 * time.Second,
		// Preview is a long-lived MJPEG stream; per-write deadlines live on
		// the handler. A global WriteTimeout would detach the in-page picture.
		WriteTimeout:   0,
		IdleTimeout:    30 * time.Second,
		MaxHeaderBytes: 16 << 10,
	}
	defer server.Close()
	serveErrors := make(chan error, 3)
	serveCount := 1
	var launcherServer *http.Server
	if launcherConfig != nil {
		launcherHandler, err := hostapi.NewLauncherHandler(handler, launcherConfig.LauncherConfig)
		if err != nil {
			fmt.Fprintln(stderr, "fogcast-api: launcher composition failed")
			return 1
		}
		launcherListener, err := net.Listen("tcp", launcherConfig.Listen)
		if err != nil {
			fmt.Fprintln(stderr, "fogcast-api: launcher listen failed")
			return 1
		}
		defer launcherListener.Close()
		launcherServer = &http.Server{Handler: launcherHandler, BaseContext: func(net.Listener) context.Context { return ctx }, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
		defer launcherServer.Close()
		go func() { serveErrors <- launcherServer.Serve(launcherListener) }()
		serveCount++
		fmt.Fprintf(stdout, "FogCast launcher listening on http://%s\n", launcherListener.Addr())
	}
	var sourceServer *http.Server
	if *sourceListen != "" {
		source, ok := fogcastService.(hostapi.LibrarySourceService)
		if !ok {
			fmt.Fprintln(stderr, "fogcast-api: library source unavailable")
			return 1
		}
		sourceListener, err := net.Listen("tcp", *sourceListen)
		if err != nil {
			fmt.Fprintln(stderr, "fogcast-api: library source listen failed")
			return 1
		}
		defer sourceListener.Close()
		sourceServer = &http.Server{Handler: hostapi.NewLibrarySourceHandler(source, config.LibrarySourceToken), BaseContext: func(net.Listener) context.Context { return ctx }, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
		defer sourceServer.Close()
		go func() { serveErrors <- sourceServer.Serve(sourceListener) }()
		serveCount++
		fmt.Fprintf(stdout, "FogCast library source listening on http://%s\n", sourceListener.Addr())
	}
	go func() { serveErrors <- server.Serve(listener) }()
	fmt.Fprintf(stdout, "FogCast API listening on http://%s\n", listener.Addr())

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if sourceServer != nil {
			if err := sourceServer.Shutdown(shutdownCtx); err != nil {
				fmt.Fprintln(stderr, "fogcast-api: library source shutdown failed")
				return 1
			}
		}
		if launcherServer != nil {
			if err := launcherServer.Shutdown(shutdownCtx); err != nil {
				fmt.Fprintln(stderr, "fogcast-api: launcher shutdown failed")
				return 1
			}
		}
		if err := server.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintln(stderr, "fogcast-api: shutdown failed")
			return 1
		}
		for range serveCount {
			if err := <-serveErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
				fmt.Fprintln(stderr, "fogcast-api: server failed")
				return 1
			}
		}
		return 0
	case err := <-serveErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(stderr, "fogcast-api: server failed")
			return 1
		}
		return 0
	}
}

func applyPreviewCaptureDevice(config *fogcast.Config, raw string) {
	if config == nil {
		return
	}
	if device := strings.TrimSpace(raw); device != "" {
		config.Media = fogcast.MediaConfig{Enabled: true, Decoder: "mjpeg", CaptureDevice: device}
	}
}

func loadAPIConfig(configPath, metadataConfigPath string) (fogcast.Config, error) {
	config, err := fogcast.LoadConfig(configPath)
	if err != nil {
		return fogcast.Config{}, err
	}
	metadataConfigPath = strings.TrimSpace(metadataConfigPath)
	if metadataConfigPath == "" {
		return config, nil
	}
	metadataConfig, err := fogcast.LoadMetadataConfig(metadataConfigPath)
	if err != nil {
		return fogcast.Config{}, err
	}
	config.Metadata = metadataConfig
	return config, nil
}

func normalizeListenAddress(address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port == "" {
		return "", errors.New("invalid listen address")
	}
	if strings.EqualFold(host, "localhost") {
		return net.JoinHostPort("127.0.0.1", port), nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return "", errors.New("listen address is not loopback")
	}
	return net.JoinHostPort(ip.String(), port), nil
}
