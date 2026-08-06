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

type compositionDeps struct {
	newCapture      captureSourceFactory
	receiverOptions []remotemedia.ManagedReceiverOption
	senderOptions   []remotemedia.ManagedSenderOption
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

func defaultCaptureSource(config fogcast.MediaConfig) (remotemedia.CaptureSource, error) {
	if runtime.GOOS != "darwin" {
		return nil, errors.New("physical capture is unavailable on this platform")
	}
	capture, err := remotemedia.OpenNativeCapture(remotemedia.CaptureConfig{
		Device: config.CaptureDevice, Bitrate: config.Bitrate, GOP: config.GOP,
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
	deps := compositionDeps{newCapture: defaultCaptureSource}
	for _, option := range options {
		if option != nil {
			option(&deps)
		}
	}
	var serverOptions []hostapi.ServerOption
	var cleanup []func() error
	if config.Media.Enabled {
		if deps.newCapture == nil {
			return nil, nil, errors.New("fogcast-api: media capture source is unavailable")
		}
		source, err := deps.newCapture(config.Media)
		if err != nil || source == nil {
			return nil, nil, errors.New("fogcast-api: media configuration failed")
		}
		receiver, err := remotemedia.NewManagedReceiver(remotemedia.ManagedReceiverConfig{
			Session: config.Media.Session, Generation: config.Media.Generation, Token: config.Token,
			SSRC: config.Media.SSRC, RTPAddress: config.Media.RTPListen, ControlAddress: config.Media.ControlAddress, Decoder: config.Media.Decoder,
		}, deps.receiverOptions...)
		if err != nil {
			_ = source.Close()
			return nil, nil, errors.New("fogcast-api: media configuration failed")
		}
		sender, err := remotemedia.NewManagedSender(remotemedia.ManagedSenderConfig{
			Session: config.Media.Session, Generation: config.Media.Generation, Token: config.Token,
			SSRC: config.Media.SSRC, RTPAddress: config.Media.RTPDestination, ControlAddress: config.Media.ControlAddress,
			Bitrate: config.Media.Bitrate, GOP: config.Media.GOP, MTU: config.Media.MTU,
		}, source, deps.senderOptions...)
		if err != nil {
			_ = source.Close()
			return nil, nil, errors.New("fogcast-api: media configuration failed")
		}
		mediaOwner := newCompositionMediaSession(hostapi.NewMediaSessionAdapter(mediasession.New(sender, receiver)))
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
		if err := remoteInput.Close(); err != nil {
			return err
		}
		return closeComposition(cleanup)
	}, nil
}

type compositionMediaSession struct {
	mu     sync.Mutex
	media  hostapi.MediaSession
	handle hostapi.MediaHandle
}

func newCompositionMediaSession(media hostapi.MediaSession) *compositionMediaSession {
	return &compositionMediaSession{media: media}
}

func (s *compositionMediaSession) Start(ctx context.Context, gameID string) (hostapi.MediaHandle, error) {
	handle, err := s.media.Start(ctx, gameID)
	if err != nil || handle == nil {
		return handle, err
	}
	owned := &compositionMediaHandle{owner: s, handle: handle}
	s.mu.Lock()
	s.handle = owned
	s.mu.Unlock()
	return owned, nil
}

func (s *compositionMediaSession) Close() error {
	s.mu.Lock()
	handle := s.handle
	s.handle = nil
	s.mu.Unlock()
	if handle == nil {
		return nil
	}
	return handle.Stop(context.Background())
}

type compositionMediaHandle struct {
	owner  *compositionMediaSession
	handle hostapi.MediaHandle
	once   sync.Once
}

func (h *compositionMediaHandle) Stop(ctx context.Context) error {
	var err error
	h.once.Do(func() {
		err = h.handle.Stop(ctx)
		h.owner.mu.Lock()
		if h.owner.handle == h {
			h.owner.handle = nil
		}
		h.owner.mu.Unlock()
	})
	return err
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

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	open := func(ctx context.Context, paths fogcast.Paths) (service, error) {
		return fogcast.Open(ctx, paths, nil)
	}
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr, open))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, open openService) int {
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
	defer fogcastService.Close()
	handler, closeRemoteInput, err := composeAPI(fogcastService, config, defaultBridgeStarter)
	if err != nil {
		fmt.Fprintln(stderr, "fogcast-api: remote input configuration failed")
		return 1
	}
	defer closeRemoteInput()

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
