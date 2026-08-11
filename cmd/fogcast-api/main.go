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
	"github.com/DeanoC/FogCast-POC/internal/metadata"
	"github.com/DeanoC/FogCast-POC/internal/remotemedia"
	"github.com/DeanoC/FogCast-POC/protocol"
)

type service interface {
	hostapi.Service
	Close() error
}

type openService func(context.Context, fogcast.Paths) (service, error)
type composeAPIFunc func(service, fogcast.Config, bridgeStarterFactory) (http.Handler, func() error, error)

type bridgeStarterFactory func(fogcast.Config) (host.BridgeStarter, error)

type captureSourceFactory func(fogcast.MediaConfig) (remotemedia.CaptureSource, error)
type audioSourceFactory func(remotemedia.AudioSourceConfig) (remotemedia.AudioSource, error)
type mediaSourcesFactory func(fogcast.MediaConfig) (remotemedia.CaptureSource, remotemedia.AudioSource, error)
type metadataOpener func(context.Context, metadata.RuntimeConfig) (metadata.Runtime, error)
type compositionOption func(*compositionDeps)

type targetCast interface {
	CastStart(context.Context, string, string, uint64) (host.CastStatus, error)
	CastStop(context.Context, string, uint64) (host.CastStatus, error)
	CastStatus(context.Context) (host.CastStatus, error)
}

type targetMediaCast interface {
	targetCast
	CastStartWithMedia(context.Context, string, string, uint64, protocol.CastMediaSet) (host.CastStatus, error)
}

const (
	targetCleanupTimeout = 2 * time.Second
	targetStatusInterval = 100 * time.Millisecond
	targetStatusFailures = 3
)

type compositionDeps struct {
	newCapture      captureSourceFactory
	newAudio        audioSourceFactory
	openMetadata    metadataOpener
	receiverOptions []remotemedia.ManagedReceiverOption
	senderOptions   []remotemedia.ManagedSenderOption
	audioOptions    []remotemedia.ManagedAudioSenderOption
	targetCast      targetCast
}

func withCaptureSourceFactory(factory captureSourceFactory) compositionOption {
	return func(deps *compositionDeps) { deps.newCapture = factory }
}

func withAudioSourceFactory(factory audioSourceFactory) compositionOption {
	return func(deps *compositionDeps) { deps.newAudio = factory }
}

func withMetadataOpener(opener metadataOpener) compositionOption {
	return func(deps *compositionDeps) { deps.openMetadata = opener }
}

func withManagedReceiverOptions(options ...remotemedia.ManagedReceiverOption) compositionOption {
	return func(deps *compositionDeps) { deps.receiverOptions = options }
}

func withManagedSenderOptions(options ...remotemedia.ManagedSenderOption) compositionOption {
	return func(deps *compositionDeps) { deps.senderOptions = options }
}

func withManagedAudioSenderOptions(options ...remotemedia.ManagedAudioSenderOption) compositionOption {
	return func(deps *compositionDeps) { deps.audioOptions = options }
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

func defaultAudioSource(config remotemedia.AudioSourceConfig) (remotemedia.AudioSource, error) {
	return remotemedia.OpenNativeAudioSource(config)
}

// newMediaSources is the default launch-time source seam. Composition tests
// replace it through makeMediaSourcesFactory without opening either source.
func newMediaSources(config fogcast.MediaConfig) (remotemedia.CaptureSource, remotemedia.AudioSource, error) {
	return makeMediaSourcesFactory(defaultCaptureSource, defaultAudioSource)(config)
}

func makeMediaSourcesFactory(newCapture captureSourceFactory, newAudio audioSourceFactory) mediaSourcesFactory {
	return func(config fogcast.MediaConfig) (remotemedia.CaptureSource, remotemedia.AudioSource, error) {
		if newCapture == nil {
			return nil, nil, errors.New("media capture source is unavailable")
		}
		capture, err := newCapture(config)
		if err != nil || capture == nil {
			if err != nil {
				return capture, nil, err
			}
			return capture, nil, errors.New("media capture source is unavailable")
		}
		if !config.Audio.Enabled {
			return capture, nil, nil
		}
		if newAudio == nil {
			return capture, nil, errors.New("audio source is unavailable")
		}
		audio, err := newAudio(config.Audio.Source)
		if err != nil || audio == nil {
			if err != nil {
				return capture, audio, err
			}
			return capture, audio, errors.New("audio source is unavailable")
		}
		return capture, audio, nil
	}
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
	deps := compositionDeps{newCapture: defaultCaptureSource, newAudio: defaultAudioSource, openMetadata: metadata.Open}
	for _, option := range options {
		if option != nil {
			option(&deps)
		}
	}
	var serverOptions []hostapi.ServerOption
	var cleanup []func() error
	metadataConfig := metadata.RuntimeConfig{
		Root: config.MetadataRoot, Configured: config.Metadata.Configured, Enabled: config.Metadata.Enabled,
		ProviderName: metadata.ProviderName(config.Metadata.Provider), ClientID: config.Metadata.ClientID,
		ClientSecret: config.Metadata.ClientSecret,
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
		var err error
		if deps.newCapture == nil {
			_ = closeComposition(cleanup)
			return nil, nil, errors.New("fogcast-api: media capture source is unavailable")
		}
		sender := &managedSenderComponent{
			media:        config.Media,
			token:        config.Token,
			newSources:   makeMediaSourcesFactory(deps.newCapture, deps.newAudio),
			options:      deps.senderOptions,
			audioOptions: deps.audioOptions,
		}
		target := deps.targetCast
		if target == nil {
			if castService, ok := service.(targetCast); ok {
				target = castService
			} else {
				target, err = newTargetCast(config)
			}
			if err != nil {
				_ = closeComposition(cleanup)
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
				_ = closeComposition(cleanup)
				return nil, nil, errors.New("fogcast-api: media configuration failed")
			}
		}
		mediaOwner := newCompositionMediaSession(hostapi.NewMediaSessionAdapter(mediasession.New(sender, receiver)), target, config.Media.Session, config.Token, config.Media.Generation)
		if config.Media.Audio.Enabled {
			mediaOwner.mediaSet = &protocol.CastMediaSet{Version: protocol.CastMediaSetVersion, Video: true, Audio: true}
		}
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
	media        fogcast.MediaConfig
	token        string
	newSources   mediaSourcesFactory
	options      []remotemedia.ManagedSenderOption
	audioOptions []remotemedia.ManagedAudioSenderOption
}

func (s *managedSenderComponent) Start(ctx context.Context, gameID string) (mediasession.ComponentHandle, error) {
	if s == nil || s.newSources == nil {
		return nil, errors.New("media sources could not be started")
	}
	capture, audio, err := s.newSources(s.media)
	if err != nil || capture == nil {
		if capture != nil || audio != nil {
			return cleanupUnownedMediaSources(capture, audio, "media sources could not be started")
		}
		if err != nil {
			return nil, fmt.Errorf("media sources could not be started: %w", err)
		}
		return nil, errors.New("media sources could not be started")
	}
	video, err := remotemedia.NewManagedSender(remotemedia.ManagedSenderConfig{
		Session: s.media.Session, Generation: s.media.Generation, Token: s.token,
		SSRC: s.media.SSRC, RTPAddress: s.media.RTPDestination, ControlAddress: s.media.ControlAddress,
		Bitrate: s.media.Bitrate, GOP: s.media.GOP, MTU: s.media.MTU,
	}, capture, s.options...)
	if err != nil {
		return cleanupUnownedMediaSources(capture, audio, "media sender could not be configured")
	}
	var managedAudio *remotemedia.ManagedAudioSender
	if audio != nil {
		config := s.media.Audio
		managedAudio, err = remotemedia.NewManagedAudioSender(remotemedia.AudioSenderConfig{
			RTPAddress: config.Transport.RTPDestination, ControlAddress: config.Transport.ControlAddress,
			Session: s.media.Session, Generation: s.media.Generation, Token: s.token, SSRC: config.Transport.SSRC,
			MTU: config.Transport.MTU, SampleRate: config.Source.SampleRate, Channels: config.Source.Channels,
			FrameSamples: config.Source.FrameSamples, FormatCapabilityVersion: config.Transport.FormatCapabilityVersion,
		}, audio, s.audioOptions...)
		if err != nil {
			return cleanupUnownedMediaSources(capture, audio, "audio sender could not be configured")
		}
	}
	sender, err := remotemedia.NewManagedMediaSender(video, managedAudio)
	if err != nil {
		return cleanupUnownedMediaSources(capture, audio, "media sender could not be configured")
	}
	// Ownership transfers to the managed sender before Start. From this point
	// onward its startup rollback owns every source, including an unstarted
	// sibling, so the caller must never close the raw sources a second time.
	capture, audio = nil, nil
	handle, err := sender.Start(ctx, gameID)
	if err != nil || handle == nil {
		if handle == nil {
			return nil, errors.New("media sender could not be started")
		}
		return handle, errors.New("media sender could not be started")
	}
	return handle, nil
}

type mediaSourcesCleanupHandle struct {
	capture *mediaSourceCleanupSlot
	audio   *mediaSourceCleanupSlot
}

type mediaSourceCleanupSlot struct {
	source interface{ Close() error }

	mu       sync.Mutex
	closed   bool
	inFlight bool
}

func (s *mediaSourceCleanupSlot) closeWithin(ctx context.Context, timeout time.Duration) error {
	if s == nil || s.source == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	if s.inFlight {
		s.mu.Unlock()
		return errors.New("media source cleanup is already in progress")
	}
	s.inFlight = true
	s.mu.Unlock()

	result := make(chan error, 1)
	go func() {
		err := s.source.Close()
		s.mu.Lock()
		s.inFlight = false
		if err == nil {
			s.closed = true
		}
		s.mu.Unlock()
		result <- err
	}()

	if timeout <= 0 {
		timeout = targetCleanupTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return errors.New("media source cleanup timed out")
	}
}

func (h *mediaSourcesCleanupHandle) Stop(ctx context.Context) error {
	if h == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	type cleanupResult struct{ err error }
	results := make(chan cleanupResult, 2)
	count := 0
	if h.audio != nil {
		count++
		go func() { results <- cleanupResult{err: h.audio.closeWithin(ctx, targetCleanupTimeout)} }()
	}
	if h.capture != nil {
		count++
		go func() { results <- cleanupResult{err: h.capture.closeWithin(ctx, targetCleanupTimeout)} }()
	}
	var first error
	for index := 0; index < count; index++ {
		if result := <-results; result.err != nil && first == nil {
			first = result.err
		}
	}
	return first
}

func cleanupUnownedMediaSources(capture remotemedia.CaptureSource, audio remotemedia.AudioSource, message string) (mediasession.ComponentHandle, error) {
	handle := &mediaSourcesCleanupHandle{}
	if capture != nil {
		handle.capture = &mediaSourceCleanupSlot{source: capture}
	}
	if audio != nil {
		handle.audio = &mediaSourceCleanupSlot{source: audio}
	}
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
	mediaSet     *protocol.CastMediaSet
	targetActive bool
	handle       hostapi.MediaHandle
}

func newCompositionMediaSession(media hostapi.MediaSession, target targetCast, session, token string, generation uint64) *compositionMediaSession {
	return &compositionMediaSession{media: media, target: target, session: session, token: token, generation: generation}
}

func (s *compositionMediaSession) Start(ctx context.Context, gameID string) (hostapi.MediaHandle, error) {
	if s.target != nil {
		var status host.CastStatus
		var err error
		if s.mediaSet == nil {
			status, err = s.target.CastStart(ctx, s.session, s.token, s.generation)
		} else {
			mediaTarget, ok := s.target.(targetMediaCast)
			if !ok {
				return nil, errors.New("target does not support media admission")
			}
			status, err = mediaTarget.CastStartWithMedia(ctx, s.session, s.token, s.generation, *s.mediaSet)
			if err == nil {
				err = protocol.ValidateCastMediaAcknowledgement(*s.mediaSet, status.Media)
			}
		}
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
	config.MetadataRoot = paths.MetadataRoot
	fogcastService, err := open(ctx, paths)
	if err != nil || fogcastService == nil {
		fmt.Fprintln(stderr, "fogcast-api: service load failed")
		return 1
	}
	closers = append(closers, runCloser{label: "service cleanup", close: fogcastService.Close})
	handler, closeRemoteInput, err := compose(fogcastService, config, defaultBridgeStarter)
	if err != nil {
		fmt.Fprintln(stderr, "fogcast-api: API composition failed")
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
