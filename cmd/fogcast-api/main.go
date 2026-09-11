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
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/internal/mediasession"
	"github.com/DeanoC/FogCast/internal/metadata"
	"github.com/DeanoC/FogCast/internal/remotemedia"
	"github.com/DeanoC/FogCast/protocol"
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
	targetCleanupTimeout   = 2 * time.Second
	targetStatusInterval   = 100 * time.Millisecond
	targetStatusFailures   = 3
	folderWatchStopTimeout = 2 * time.Second
	folderWatchLogInterval = time.Second
)

var errFolderWatchStopTimeout = errors.New("folder-watch stop timed out")

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
	newCapture      captureSourceFactory
	newPreview      remotemedia.PreviewDecoderFactory
	previewHandler  http.Handler
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

func withPreviewDecoderFactory(factory remotemedia.PreviewDecoderFactory) compositionOption {
	return func(deps *compositionDeps) { deps.newPreview = factory }
}

func withPreviewHandler(handler http.Handler) compositionOption {
	return func(deps *compositionDeps) { deps.previewHandler = handler }
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
		var err error
		if (config.Media.Decoder == "ffplay" || config.Media.Decoder == "mjpeg") && config.Media.Audio.Enabled {
			_ = closeComposition(cleanup)
			return nil, nil, errors.New("fogcast-api: local media playback is video-only")
		}
		if deps.newCapture == nil {
			_ = closeComposition(cleanup)
			return nil, nil, errors.New("fogcast-api: media capture source is unavailable")
		}
		if config.Media.Decoder == "mjpeg" {
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
			mediaOwner := newCompositionMediaSession(hostapi.NewMediaSessionAdapter(mediasession.New(component, noopMediaComponent{})), nil, "", "", 0)
			cleanup = append(cleanup, mediaOwner.Close)
			serverOptions = append(serverOptions, hostapi.WithMediaSession(mediaOwner), hostapi.WithMediaPreview(previewHandler))
		} else {
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
			mediaTarget := target
			if config.Media.Decoder == "ffplay" {
				// Local playback is the sofa-path preview: the existing capture
				// sender feeds the host receiver/ffplay window and must not claim
				// the target presentation surface from the active FPGA core.
				mediaTarget = nil
				receiver, err = remotemedia.NewManagedReceiver(remotemedia.ManagedReceiverConfig{
					Session: config.Media.Session, Generation: config.Media.Generation, Token: config.Token,
					SSRC: config.Media.SSRC, RTPAddress: config.Media.RTPListen, ControlAddress: config.Media.ControlAddress, Decoder: config.Media.Decoder,
				}, deps.receiverOptions...)
			} else if target != nil {
				receiver = noopMediaComponent{}
			} else {
				receiver, err = remotemedia.NewManagedReceiver(remotemedia.ManagedReceiverConfig{
					Session: config.Media.Session, Generation: config.Media.Generation, Token: config.Token,
					SSRC: config.Media.SSRC, RTPAddress: config.Media.RTPListen, ControlAddress: config.Media.ControlAddress, Decoder: config.Media.Decoder,
				}, deps.receiverOptions...)
			}
			if err != nil {
				_ = closeComposition(cleanup)
				return nil, nil, errors.New("fogcast-api: media configuration failed")
			}
			mediaOwner := newCompositionMediaSession(hostapi.NewMediaSessionAdapter(mediasession.New(sender, receiver)), mediaTarget, config.Media.Session, config.Token, config.Media.Generation)
			if config.Media.Audio.Enabled {
				mediaOwner.mediaSet = &protocol.CastMediaSet{Version: protocol.CastMediaSetVersion, Video: true, Audio: true}
			}
			cleanup = append(cleanup, mediaOwner.Close)
			serverOptions = append(serverOptions, hostapi.WithMediaSession(mediaOwner))
		}
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
	if provider, ok := service.(interface{ KitLease() *host.KitLease }); ok {
		if targetStarter, ok := starter.(*host.HTTPBridgeStarter); ok {
			targetStarter.WithKitLease(provider.KitLease())
		}
	}
	remoteInput, err := host.NewRemoteInput(host.RemoteInputConfig{Starter: starter})
	if err != nil {
		_ = closeComposition(cleanup)
		return nil, nil, errors.New("fogcast-api: remote input configuration failed")
	}
	if provider, ok := service.(interface{ SetTargetReset(func()) }); ok {
		provider.SetTargetReset(remoteInput.Invalidate)
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
	launcherPath := flags.String("launcher-config", "", "private JSON configuration for the optional paired-kit listener")
	metadataConfigPath := flags.String("metadata-config", "", "FogCast configuration path supplying the metadata section")
	previewCaptureDevice := flags.String("preview-capture-device", "", "local capture device shown in the FPGA Play surface")
	headless := flags.Bool("headless", false, "API-only: do not start local capture or session preview")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return 2
	}
	var launcherConfig *launcherListenerConfig
	if *launcherPath != "" {
		parsed, err := loadLauncherConfig(*launcherPath)
		if err != nil {
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
	paths, err := fogcast.DefaultPaths()
	if err != nil {
		fmt.Fprintln(stderr, "fogcast-api: cannot determine default paths")
		return 1
	}
	if strings.TrimSpace(*configPath) != "" {
		paths.Config = *configPath
	}
	config, err := loadAPIConfig(paths.Config, *metadataConfigPath)
	if err != nil {
		fmt.Fprintln(stderr, "fogcast-api: configuration load failed")
		return 1
	}
	if launcherConfig != nil {
		if launcherConfig.Token == config.Token {
			fmt.Fprintln(stderr, "fogcast-api: launcher requires a separate credential")
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
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ReadHeaderTimeout: 5 * time.Second,
		// Preview is a long-lived MJPEG stream; per-write deadlines live on
		// the handler. A global WriteTimeout would detach the in-page picture.
		WriteTimeout:   0,
		IdleTimeout:    30 * time.Second,
		MaxHeaderBytes: 16 << 10,
	}
	defer server.Close()
	serveErrors := make(chan error, 2)
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
		fmt.Fprintf(stdout, "FogCast launcher listening on http://%s\n", launcherListener.Addr())
	}
	go func() { serveErrors <- server.Serve(listener) }()
	fmt.Fprintf(stdout, "FogCast API listening on http://%s\n", listener.Addr())

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
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
