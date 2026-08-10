// Command fogcast-api serves the local FogCast application API for POC3 clients.
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
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/DeanoC/FogCast-POC/fogcast"
	"github.com/DeanoC/FogCast-POC/host"
	"github.com/DeanoC/FogCast-POC/internal/hostapi"
	"github.com/DeanoC/FogCast-POC/internal/mediasession"
	"github.com/DeanoC/FogCast-POC/internal/remotemedia"
)

type service interface {
	hostapi.Service
	Close() error
}

type openService func(context.Context, fogcast.Paths) (service, error)

type bridgeStarterFactory func(fogcast.Config) (host.BridgeStarter, error)

type captureSourceFactory func(fogcast.MediaConfig) (remotemedia.CaptureSource, error)
type compositionOption func(*compositionDeps)

type targetCast interface {
	CastStart(context.Context, string, string, uint64) (host.CastStatus, error)
	CastStop(context.Context, string, uint64) (host.CastStatus, error)
	CastStatus(context.Context) (host.CastStatus, error)
}

const (
	targetCleanupTimeout = 2 * time.Second
	targetStatusInterval = 100 * time.Millisecond
	targetStatusFailures = 3
)

type compositionDeps struct {
	newCapture      captureSourceFactory
	receiverOptions []remotemedia.ManagedReceiverOption
	senderOptions   []remotemedia.ManagedSenderOption
	targetCast      targetCast
}

func withCaptureSourceFactory(factory captureSourceFactory) compositionOption {
	return func(deps *compositionDeps) { deps.newCapture = factory }
}

func withManagedReceiverOptions(options ...remotemedia.ManagedReceiverOption) compositionOption {
	return func(deps *compositionDeps) { deps.receiverOptions = options }
}

func withManagedSenderOptions(options ...remotemedia.ManagedSenderOption) compositionOption {
	return func(deps *compositionDeps) { deps.senderOptions = options }
}

func withTargetCast(controller targetCast) compositionOption {
	return func(deps *compositionDeps) { deps.targetCast = controller }
}

func defaultCaptureSource(config fogcast.MediaConfig) (remotemedia.CaptureSource, error) {
	if runtime.GOOS != "darwin" {
		return nil, errors.New("physical capture is unavailable on this platform")
	}
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

func newTargetCast(config fogcast.Config) (targetCast, error) {
	if strings.TrimSpace(config.BaseURL) == "" {
		return nil, nil
	}
	baseURL, err := url.Parse(config.BaseURL)
	if err != nil {
		return nil, err
	}
	return host.NewClient(baseURL, config.Token, nil), nil
}

func composeAPI(service service, config fogcast.Config, makeStarter bridgeStarterFactory, options ...compositionOption) (http.Handler, func() error, error) {
	if service == nil {
		return nil, nil, errors.New("fogcast-api: composition dependency is unavailable")
	}
	deps := compositionDeps{newCapture: defaultCaptureSource}
	for _, option := range options {
		if option != nil {
			option(&deps)
		}
	}
	var serverOptions []hostapi.ServerOption
	var cleanup []func() error
	if config.Media.Enabled {
		var err error
		if deps.newCapture == nil {
			return nil, nil, errors.New("fogcast-api: media capture source is unavailable")
		}
		sender := &managedSenderComponent{
			media:      config.Media,
			token:      config.Token,
			newCapture: deps.newCapture,
			options:    deps.senderOptions,
		}
		target := deps.targetCast
		if target == nil {
			if castService, ok := service.(targetCast); ok {
				target = castService
			} else {
				target, err = newTargetCast(config)
			}
			if err != nil {
				return nil, nil, errors.New("fogcast-api: target cast configuration failed")
			}
		}
		var receiver mediasession.Component
		if target != nil {
			receiver = noopMediaComponent{}
		} else {
			receiver, err = remotemedia.NewManagedReceiver(remotemedia.ManagedReceiverConfig{
				Session: config.Media.Session, Generation: config.Media.Generation, Token: config.Token,
				SSRC: config.Media.SSRC, RTPAddress: config.Media.RTPListen, ControlAddress: config.Media.ControlAddress, Decoder: config.Media.Decoder,
			}, deps.receiverOptions...)
			if err != nil {
				return nil, nil, errors.New("fogcast-api: media configuration failed")
			}
		}
		mediaOwner := newCompositionMediaSession(hostapi.NewMediaSessionAdapter(mediasession.New(sender, receiver)), target, config.Media.Session, config.Token, config.Media.Generation)
		cleanup = append(cleanup, mediaOwner.Close)
		serverOptions = append(serverOptions, hostapi.WithMediaSession(mediaOwner))
	}
	if !config.RemoteInput.Enabled {
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
	remoteInput, err := host.NewRemoteInput(host.RemoteInputConfig{Starter: starter})
	if err != nil {
		_ = closeComposition(cleanup)
		return nil, nil, errors.New("fogcast-api: remote input configuration failed")
	}
	serverOptions = append(serverOptions, hostapi.WithRemoteInput(remoteInput))
	return hostapi.New(service, serverOptions...), func() error {
		return closeAPIComposition(remoteInput.Close, cleanup)
	}, nil
}

type managedSenderComponent struct {
	media      fogcast.MediaConfig
	token      string
	newCapture captureSourceFactory
	options    []remotemedia.ManagedSenderOption
}

func (s *managedSenderComponent) Start(ctx context.Context, gameID string) (mediasession.ComponentHandle, error) {
	source, err := s.newCapture(s.media)
	if err != nil || source == nil {
		if source != nil {
			return cleanupUnownedCapture(source, "media capture could not be started")
		}
		if err != nil {
			return nil, fmt.Errorf("media capture could not be started: %w", err)
		}
		return nil, errors.New("media capture could not be started")
	}
	sender, err := remotemedia.NewManagedSender(remotemedia.ManagedSenderConfig{
		Session: s.media.Session, Generation: s.media.Generation, Token: s.token,
		SSRC: s.media.SSRC, RTPAddress: s.media.RTPDestination, ControlAddress: s.media.ControlAddress,
		Bitrate: s.media.Bitrate, GOP: s.media.GOP, MTU: s.media.MTU,
	}, source, s.options...)
	if err != nil {
		return cleanupUnownedCapture(source, "media sender could not be configured")
	}
	handle, err := sender.Start(ctx, gameID)
	if err != nil || handle == nil {
		if handle == nil {
			return cleanupUnownedCapture(source, "media sender could not be started")
		}
		return handle, errors.New("media sender could not be started")
	}
	return handle, nil
}

type captureCleanupHandle struct {
	mu     sync.Mutex
	source remotemedia.CaptureSource
	closed bool
}

func (h *captureCleanupHandle) Stop(context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	if err := h.source.Close(); err != nil {
		return errors.New("media capture could not be stopped")
	}
	h.closed = true
	return nil
}

func cleanupUnownedCapture(source remotemedia.CaptureSource, message string) (mediasession.ComponentHandle, error) {
	handle := &captureCleanupHandle{source: source}
	if err := handle.Stop(context.Background()); err != nil {
		return handle, errors.New(message)
	}
	return nil, errors.New(message)
}

type noopMediaComponent struct{}

func (noopMediaComponent) Start(context.Context, string) (mediasession.ComponentHandle, error) {
	return noopMediaHandle{}, nil
}

type noopMediaHandle struct{}

func (noopMediaHandle) Stop(context.Context) error { return nil }

type compositionMediaSession struct {
	mu           sync.Mutex
	media        hostapi.MediaSession
	target       targetCast
	session      string
	token        string
	generation   uint64
	targetActive bool
	handle       hostapi.MediaHandle
}

func newCompositionMediaSession(media hostapi.MediaSession, target targetCast, session, token string, generation uint64) *compositionMediaSession {
	return &compositionMediaSession{media: media, target: target, session: session, token: token, generation: generation}
}

func (s *compositionMediaSession) Start(ctx context.Context, gameID string) (hostapi.MediaHandle, error) {
	if s.target != nil {
		status, err := s.target.CastStart(ctx, s.session, s.token, s.generation)
		if err != nil || status.State != "active" || status.Session != s.session || status.Generation != s.generation {
			s.mu.Lock()
			s.targetActive = true
			s.mu.Unlock()
			cleanupCtx, cancel := boundedTargetContext(context.Background())
			_, cleanupErr := s.target.CastStop(cleanupCtx, s.session, s.generation)
			cancel()
			if cleanupErr == nil {
				s.mu.Lock()
				s.targetActive = false
				s.mu.Unlock()
				return nil, errors.New("target cast could not be started")
			}
			_, monitorCancel := context.WithCancel(context.Background())
			owned := &compositionMediaHandle{
				owner: s, localStopped: true, done: make(chan struct{}), monitorCancel: monitorCancel,
			}
			s.mu.Lock()
			s.handle = owned
			s.mu.Unlock()
			return owned, errors.New("target cast could not be started")
		}
		s.mu.Lock()
		s.targetActive = true
		s.mu.Unlock()
	}
	handle, err := s.media.Start(ctx, gameID)
	if err != nil || handle == nil {
		targetStopped := s.target == nil
		var targetErr error
		if s.target != nil {
			cleanupCtx, cancel := boundedTargetContext(context.Background())
			_, targetErr = s.target.CastStop(cleanupCtx, s.session, s.generation)
			cancel()
			if targetErr == nil {
				targetStopped = true
				s.mu.Lock()
				s.targetActive = false
				s.mu.Unlock()
			}
		}
		if err == nil {
			err = errors.New("media sender could not be started")
		}
		if handle == nil && targetErr != nil {
			err = errors.Join(err, targetErr)
		}
		if handle != nil || !targetStopped {
			_, monitorCancel := context.WithCancel(context.Background())
			owned := &compositionMediaHandle{
				owner: s, handle: handle, localStopped: handle == nil,
				targetStopped: targetStopped, done: make(chan struct{}), monitorCancel: monitorCancel,
			}
			s.mu.Lock()
			s.handle = owned
			s.mu.Unlock()
			return owned, err
		}
		return nil, err
	}
	monitorCtx, monitorCancel := context.WithCancel(context.Background())
	owned := &compositionMediaHandle{
		owner: s, handle: handle, done: make(chan struct{}), monitorCancel: monitorCancel,
		targetStopped: s.target == nil,
	}
	s.mu.Lock()
	s.handle = owned
	s.mu.Unlock()
	go owned.monitor(monitorCtx)
	return owned, nil
}

func (s *compositionMediaSession) Close() error {
	s.mu.Lock()
	handle := s.handle
	targetActive := s.targetActive
	s.mu.Unlock()
	var first error
	if handle != nil {
		if err := handle.Stop(context.Background()); err != nil {
			first = err
			_ = handle.Stop(context.Background())
		}
	} else if targetActive && s.target != nil {
		cleanupCtx, cancel := boundedTargetContext(context.Background())
		_, err := s.target.CastStop(cleanupCtx, s.session, s.generation)
		cancel()
		if err != nil {
			first = err
			retryCtx, retryCancel := boundedTargetContext(context.Background())
			_, _ = s.target.CastStop(retryCtx, s.session, s.generation)
			retryCancel()
		} else {
			s.mu.Lock()
			s.targetActive = false
			s.mu.Unlock()
		}
	}
	return first
}

type compositionMediaHandle struct {
	owner         *compositionMediaSession
	handle        hostapi.MediaHandle
	mu            sync.Mutex
	localStopped  bool
	targetStopped bool
	done          chan struct{}
	doneOnce      sync.Once
	monitorCancel context.CancelFunc
}

func (h *compositionMediaHandle) Stop(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	var first error
	if !h.localStopped {
		localCtx, cancel := boundedTargetContext(ctx)
		err := h.handle.Stop(localCtx)
		cancel()
		if err != nil {
			first = err
		} else {
			h.localStopped = true
		}
	}
	h.owner.mu.Lock()
	targetActive := h.owner.targetActive
	target := h.owner.target
	h.owner.mu.Unlock()
	if !h.targetStopped && targetActive && target != nil {
		targetCtx, cancel := boundedTargetContext(context.Background())
		_, err := target.CastStop(targetCtx, h.owner.session, h.owner.generation)
		cancel()
		if err != nil {
			if first == nil {
				first = err
			}
		} else {
			h.targetStopped = true
			h.owner.mu.Lock()
			h.owner.targetActive = false
			h.owner.mu.Unlock()
		}
	}
	if !targetActive || target == nil {
		h.targetStopped = true
	}
	if h.localStopped && h.targetStopped {
		h.monitorCancel()
		h.doneOnce.Do(func() { close(h.done) })
		h.owner.mu.Lock()
		if h.owner.handle == h {
			h.owner.handle = nil
		}
		h.owner.mu.Unlock()
	}
	return first
}

func (h *compositionMediaHandle) Done() <-chan struct{} {
	return h.done
}

func (h *compositionMediaHandle) monitor(ctx context.Context) {
	var localDone <-chan struct{}
	if terminal, ok := h.handle.(interface{ Done() <-chan struct{} }); ok {
		localDone = terminal.Done()
	}
	ticker := time.NewTicker(targetStatusInterval)
	defer ticker.Stop()
	consecutiveFailures := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-localDone:
			h.doneOnce.Do(func() { close(h.done) })
			return
		case <-ticker.C:
			h.owner.mu.Lock()
			targetActive := h.owner.targetActive
			target := h.owner.target
			h.owner.mu.Unlock()
			if !targetActive || target == nil {
				continue
			}
			statusCtx, cancel := boundedTargetContext(ctx)
			status, err := target.CastStatus(statusCtx)
			cancel()
			if err != nil {
				consecutiveFailures++
				if consecutiveFailures >= targetStatusFailures {
					h.doneOnce.Do(func() { close(h.done) })
					return
				}
				continue
			}
			consecutiveFailures = 0
			if status.State != "active" || status.Session != h.owner.session || status.Generation != h.owner.generation {
				h.mu.Lock()
				h.targetStopped = true
				h.owner.mu.Lock()
				h.owner.targetActive = false
				h.owner.mu.Unlock()
				h.mu.Unlock()
				h.doneOnce.Do(func() { close(h.done) })
				return
			}
		}
	}
}

func boundedTargetContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, targetCleanupTimeout)
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
	open := func(ctx context.Context, paths fogcast.Paths) (service, error) {
		return fogcast.Open(ctx, paths, nil)
	}
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr, open))
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
	var first error
	for attempt := 0; attempt < 2; attempt++ {
		stopCtx, cancel := context.WithTimeout(context.Background(), targetCleanupTimeout)
		_, err := service.Stop(stopCtx)
		cancel()
		if err == nil {
			return first
		}
		if first == nil {
			first = err
		}
	}
	return first
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, open openService) (exitCode int) {
	var closers []runCloser
	defer func() { exitCode = finishRun(exitCode, stderr, closers...) }()
	flags := flag.NewFlagSet("fogcast-api", flag.ContinueOnError)
	flags.SetOutput(stderr)
	listen := flags.String("listen", "127.0.0.1:8787", "loopback HTTP listen address")
	configPath := flags.String("config", "", "FogCast configuration path")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return 2
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
	paths, err := fogcast.DefaultPaths()
	if err != nil {
		fmt.Fprintln(stderr, "fogcast-api: cannot determine default paths")
		return 1
	}
	if strings.TrimSpace(*configPath) != "" {
		paths.Config = *configPath
	}
	config, err := fogcast.LoadConfig(paths.Config)
	if err != nil {
		fmt.Fprintln(stderr, "fogcast-api: configuration load failed")
		return 1
	}
	fogcastService, err := open(ctx, paths)
	if err != nil || fogcastService == nil {
		fmt.Fprintln(stderr, "fogcast-api: service load failed")
		return 1
	}
	closers = append(closers, runCloser{label: "service cleanup", close: fogcastService.Close})
	handler, closeRemoteInput, err := composeAPI(fogcastService, config, defaultBridgeStarter)
	if err != nil {
		fmt.Fprintln(stderr, "fogcast-api: remote input configuration failed")
		return 1
	}
	closers = append(closers,
		runCloser{label: "session cleanup", close: func() error { return stopServiceForShutdown(fogcastService) }},
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
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.Serve(listener) }()
	fmt.Fprintf(stdout, "FogCast API listening on http://%s\n", listener.Addr())

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintln(stderr, "fogcast-api: shutdown failed")
			return 1
		}
		if err := <-serveErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(stderr, "fogcast-api: server failed")
			return 1
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
