package remotemedia

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/internal/mediasession"
)

var (
	ErrManagedAudioSenderStart = errors.New("managed audio sender failed to start")
	ErrManagedAudioSenderStop  = errors.New("managed audio sender failed to stop")
	ErrManagedMediaSenderStart = errors.New("managed media sender failed to start")
	ErrManagedMediaSenderStop  = errors.New("managed media sender failed to stop")
)

// ManagedAudioSenderRunner is the bounded worker lifecycle used by
// ManagedAudioSender. AudioSender implements it directly.
type ManagedAudioSenderRunner interface {
	Run(context.Context) error
	Close() error
}

// ManagedAudioSenderReadyRunner reports startup failure before its long-lived
// loop begins. AudioSender implements this interface through RunReady.
type ManagedAudioSenderReadyRunner interface {
	ManagedAudioSenderRunner
	RunReady(context.Context, func(error)) error
}

type managedAudioSenderFactory func(AudioSenderConfig, AudioSource) (ManagedAudioSenderRunner, error)
type managedAudioSenderFactoryWithOwnership func(AudioSenderConfig, AudioSource) (ManagedAudioSenderRunner, bool, error)

type ManagedAudioSenderOption func(*ManagedAudioSender)

func WithManagedAudioSenderFactory(factory func(AudioSenderConfig, AudioSource) (ManagedAudioSenderRunner, error)) ManagedAudioSenderOption {
	return func(s *ManagedAudioSender) {
		if factory != nil {
			s.newSender = factory
			s.newSenderWithOwnership = nil
		}
	}
}

// WithManagedAudioSenderFactoryOwnership is the explicit factory seam for a
// runner that can take audio-source ownership even when construction also
// returns an error. Legacy factories remain caller-owned on error.
func WithManagedAudioSenderFactoryOwnership(factory func(AudioSenderConfig, AudioSource) (ManagedAudioSenderRunner, bool, error)) ManagedAudioSenderOption {
	return func(s *ManagedAudioSender) {
		if factory != nil {
			s.newSenderWithOwnership = factory
			s.newSender = nil
		}
	}
}

func WithManagedAudioSenderStopTimeout(timeout time.Duration) ManagedAudioSenderOption {
	return func(s *ManagedAudioSender) {
		if timeout > 0 {
			s.stopTimeout = timeout
		}
	}
}

// ManagedAudioSender owns one audio source and its AudioSender lifecycle.
// It has no video dependencies and can therefore be stopped independently.
type ManagedAudioSender struct {
	config                 AudioSenderConfig
	source                 AudioSource
	newSender              managedAudioSenderFactory
	newSenderWithOwnership managedAudioSenderFactoryWithOwnership
	stopTimeout            time.Duration
	sourceMu               sync.Mutex
	sourceOwned            bool
}

func NewManagedAudioSender(config AudioSenderConfig, source AudioSource, options ...ManagedAudioSenderOption) (*ManagedAudioSender, error) {
	if source == nil || config.RTPAddress == "" || config.ControlAddress == "" || config.Session == "" || config.Generation == 0 || config.Token == "" || config.SSRC == 0 || config.FormatCapabilityVersion == 0 {
		return nil, ErrManagedAudioSenderStart
	}
	format := AudioFormat{SampleRate: config.SampleRate, Channels: config.Channels, Encoding: AudioEncodingPCM16LE, FrameSamples: config.FrameSamples}
	if err := ValidateAudioFormat(format); err != nil {
		return nil, ErrManagedAudioSenderStart
	}
	if config.MTU != 0 {
		if _, err := NewAudioRTPPacketizer(config.MTU, config.SSRC, config.InitialSequence, config.RTPBaseTimestamp, format); err != nil {
			return nil, ErrManagedAudioSenderStart
		}
	}
	s := &ManagedAudioSender{config: config, source: source, newSender: defaultManagedAudioSenderFactory, newSenderWithOwnership: defaultManagedAudioSenderFactoryWithOwnership, stopTimeout: defaultManagedSenderStopTimeout, sourceOwned: true}
	for _, option := range options {
		if option != nil {
			option(s)
		}
	}
	return s, nil
}

func defaultManagedAudioSenderFactory(config AudioSenderConfig, source AudioSource) (ManagedAudioSenderRunner, error) {
	return NewAudioSender(config, source)
}

var _ mediasession.Component = (*ManagedAudioSender)(nil)

func (s *ManagedAudioSender) Start(ctx context.Context, _ string) (mediasession.ComponentHandle, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if s == nil || s.source == nil || (s.newSender == nil && s.newSenderWithOwnership == nil) || ctx.Err() != nil {
		return nil, ErrManagedAudioSenderStart
	}
	var runner ManagedAudioSenderRunner
	var err error
	ownsSource := false
	if s.newSenderWithOwnership != nil {
		runner, ownsSource, err = s.newSenderWithOwnership(s.config, s.source)
	} else {
		runner, err = s.newSender(s.config, s.source)
		ownsSource = runner != nil && err == nil
	}
	// Ownership is valid only when a concrete runner exists. A successful
	// runner that declines ownership is malformed: returning it would leave the
	// source without a component responsible for its lifetime, so force the
	// caller-owned failure cleanup path instead.
	if runner == nil {
		ownsSource = false
	} else if err == nil && !ownsSource {
		err = errors.New("managed audio sender factory did not transfer source ownership")
	}
	if runner != nil && ownsSource {
		s.sourceMu.Lock()
		s.sourceOwned = false
		s.sourceMu.Unlock()
	}
	if err != nil || runner == nil {
		if !ownsSource {
			source := s.takeSourceCleanup(s.stopTimeout)
			if runner != nil {
				runnerHandle := newManagedAudioPartialHandle(runner, s.stopTimeout)
				if source != nil {
					return cleanupManagedRunnerAndSource(runnerHandle, source, s.stopTimeout, ErrManagedAudioSenderStart, ErrManagedAudioSenderStop)
				}
				return cleanupManagedStartChildrenWithStopError([]mediasession.ComponentHandle{runnerHandle}, s.stopTimeout, ErrManagedAudioSenderStart, ErrManagedAudioSenderStop)
			}
			if source != nil {
				return cleanupManagedStartChildrenWithStopError([]mediasession.ComponentHandle{source}, s.stopTimeout, ErrManagedAudioSenderStart, ErrManagedAudioSenderStop)
			}
			return nil, ErrManagedAudioSenderStart
		}
		if runner != nil {
			return cleanupManagedAudioStart(newManagedAudioPartialHandle(runner, s.stopTimeout))
		}
		return nil, ErrManagedAudioSenderStart
	}
	if ctx.Err() != nil {
		return cleanupManagedAudioStart(newManagedAudioPartialHandle(runner, s.stopTimeout))
	}
	if readyRunner, ok := runner.(ManagedAudioSenderReadyRunner); ok {
		ready := make(chan error, 1)
		runDone := make(chan struct{})
		runCtx, cancel := context.WithCancel(context.Background())
		go func() {
			defer close(runDone)
			_ = readyRunner.RunReady(runCtx, func(err error) { ready <- err })
		}()
		handle := newManagedAudioHandleWithDone(runCtx, cancel, runner, runDone, s.stopTimeout)
		select {
		case err := <-ready:
			if err != nil {
				return cleanupManagedAudioStart(handle)
			}
			return handle, nil
		case <-runDone:
			return cleanupManagedAudioStart(handle)
		case <-ctx.Done():
			return cleanupManagedAudioStart(handle)
		}
	}
	return newManagedAudioHandle(context.Background(), runner, s.stopTimeout), nil
}

func defaultManagedAudioSenderFactoryWithOwnership(config AudioSenderConfig, source AudioSource) (ManagedAudioSenderRunner, bool, error) {
	runner, err := defaultManagedAudioSenderFactory(config, source)
	return runner, runner != nil && err == nil, err
}

// takeSourceCleanup transfers an unstarted audio source to a retryable cleanup
// handle. A successfully constructed runner has already taken source
// ownership, so this returns nil in that case and prevents a second Close.
func (s *ManagedAudioSender) takeSourceCleanup(timeout time.Duration) mediasession.ComponentHandle {
	if s == nil {
		return nil
	}
	s.sourceMu.Lock()
	if !s.sourceOwned || s.source == nil {
		s.sourceMu.Unlock()
		return nil
	}
	source := s.source
	s.source = nil
	s.sourceOwned = false
	s.sourceMu.Unlock()
	return newManagedSourceCleanupHandle(source, timeout)
}

func newManagedAudioPartialHandle(runner ManagedAudioSenderRunner, timeout time.Duration) *managedAudioHandle {
	_, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	close(runDone)
	done := make(chan struct{})
	close(done)
	return &managedAudioHandle{runner: runner, cancel: cancel, runDone: runDone, timeout: timeout, done: done, runReaped: true}
}

func cleanupManagedAudioStart(handle *managedAudioHandle) (mediasession.ComponentHandle, error) {
	if err := handle.Stop(context.Background()); err != nil {
		return handle, ErrManagedAudioSenderStart
	}
	return nil, ErrManagedAudioSenderStart
}

type managedAudioHandle struct {
	runner  ManagedAudioSenderRunner
	cancel  context.CancelFunc
	runDone chan struct{}
	timeout time.Duration

	cancelOnce   sync.Once
	doneOnce     sync.Once
	done         chan struct{}
	attemptMu    sync.Mutex
	mu           sync.Mutex
	runnerClosed bool
	runReaped    bool
}

func newManagedAudioHandle(parent context.Context, runner ManagedAudioSenderRunner, timeout time.Duration) *managedAudioHandle {
	ctx, cancel := context.WithCancel(parent)
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		_ = runner.Run(ctx)
	}()
	return newManagedAudioHandleWithDone(ctx, cancel, runner, runDone, timeout)
}

func newManagedAudioHandleWithDone(ctx context.Context, cancel context.CancelFunc, runner ManagedAudioSenderRunner, runDone chan struct{}, timeout time.Duration) *managedAudioHandle {
	h := &managedAudioHandle{runner: runner, cancel: cancel, runDone: runDone, done: make(chan struct{}), timeout: timeout}
	go func() {
		select {
		case <-ctx.Done():
		case <-runDone:
		}
		h.doneOnce.Do(func() { close(h.done) })
		_ = h.Stop(context.Background())
	}()
	return h
}

func (h *managedAudioHandle) Done() <-chan struct{} {
	if h == nil {
		return nil
	}
	return h.done
}

func (h *managedAudioHandle) Stop(ctx context.Context) error {
	if h == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	h.cancelOnce.Do(h.cancel)
	attemptDone := make(chan error, 1)
	go func() { attemptDone <- h.cleanupAttempt() }()
	timer := time.NewTimer(h.timeout)
	defer timer.Stop()
	select {
	case err := <-attemptDone:
		return err
	case <-ctx.Done():
		return ErrManagedAudioSenderStop
	case <-timer.C:
		return ErrManagedAudioSenderStop
	}
}

func (h *managedAudioHandle) cleanupAttempt() error {
	h.attemptMu.Lock()
	defer h.attemptMu.Unlock()
	h.mu.Lock()
	runnerClosed, runReaped := h.runnerClosed, h.runReaped
	h.mu.Unlock()
	failed := false
	if !runnerClosed {
		if err := h.runner.Close(); err != nil {
			failed = true
		} else {
			h.mu.Lock()
			h.runnerClosed = true
			h.mu.Unlock()
		}
	}
	if !runReaped {
		if !waitManagedSender(h.runDone, h.timeout) {
			failed = true
		} else {
			h.mu.Lock()
			h.runReaped = true
			h.mu.Unlock()
		}
	}
	if failed {
		return ErrManagedAudioSenderStop
	}
	return nil
}

// ManagedMediaSender joins a video sender and an optional, independent audio
// sender into the single mediasession.Component required by API composition.
type ManagedMediaSender struct {
	video            mediasession.Component
	audio            mediasession.Component
	videoSourceOwner *ManagedSender
	audioSourceOwner *ManagedAudioSender
	stopTimeout      time.Duration
}

func NewManagedMediaSender(video *ManagedSender, audio *ManagedAudioSender) (*ManagedMediaSender, error) {
	if video == nil {
		return nil, ErrManagedMediaSenderStart
	}
	var audioComponent mediasession.Component
	if audio != nil {
		if video.config.Session == "" || video.config.Generation == 0 || video.config.Token == "" ||
			video.config.Session != audio.config.Session || video.config.Generation != audio.config.Generation || video.config.Token != audio.config.Token ||
			video.config.SSRC == 0 || audio.config.SSRC == 0 || video.config.SSRC == audio.config.SSRC {
			return nil, ErrManagedMediaSenderStart
		}
		endpoints := []string{video.config.RTPAddress, video.config.ControlAddress, audio.config.RTPAddress, audio.config.ControlAddress}
		for index, endpoint := range endpoints {
			if endpoint == "" {
				continue
			}
			canonical, err := canonicalManagedEndpoint(endpoint)
			if err != nil {
				return nil, ErrManagedMediaSenderStart
			}
			endpoints[index] = canonical
		}
		for index, endpoint := range endpoints {
			if endpoint == "" {
				continue
			}
			for _, other := range endpoints[index+1:] {
				if endpoint == other {
					return nil, ErrManagedMediaSenderStart
				}
			}
		}
		audioComponent = audio
	}
	return &ManagedMediaSender{
		video:            video,
		audio:            audioComponent,
		videoSourceOwner: video,
		audioSourceOwner: audio,
		stopTimeout:      defaultManagedSenderStopTimeout,
	}, nil
}

func canonicalManagedEndpoint(address string) (string, error) {
	address = strings.TrimSpace(address)
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || port == "" {
		return "", errors.New("media endpoint must be a literal host and port")
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return "", errors.New("media endpoint host must be a literal IP")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 0 || portNumber > 65535 {
		return "", errors.New("media endpoint port is invalid")
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	return net.JoinHostPort(ip.String(), strconv.Itoa(portNumber)), nil
}

func newManagedMediaSenderForComponents(video, audio mediasession.Component, timeout time.Duration) *ManagedMediaSender {
	return &ManagedMediaSender{video: video, audio: audio, stopTimeout: timeout}
}

var _ mediasession.Component = (*ManagedMediaSender)(nil)

func (s *ManagedMediaSender) Start(ctx context.Context, gameID string) (mediasession.ComponentHandle, error) {
	if s == nil || s.video == nil {
		return nil, ErrManagedMediaSenderStart
	}
	if ctx == nil {
		ctx = context.Background()
	}
	video, err := s.video.Start(ctx, gameID)
	if err != nil || video == nil {
		if video != nil && err != nil {
			children := []mediasession.ComponentHandle{video}
			if source := s.audioSourceCleanup(); source != nil {
				if sourceErr := source.Stop(context.Background()); sourceErr != nil {
					children = append(children, source)
				}
			}
			return cleanupManagedStartChildrenWithoutAttempt(children, s.stopTimeout)
		}
		return s.startFailure(video, nil, s.videoSourceCleanup(), s.audioSourceCleanup())
	}
	if s.audio == nil {
		return newManagedMediaHandle([]mediasession.ComponentHandle{video}, s.stopTimeout), nil
	}
	audio, audioErr := s.audio.Start(ctx, gameID)
	if audioErr != nil || audio == nil {
		return s.startFailure(video, audio, s.audioSourceCleanup())
	}
	return newManagedMediaHandle([]mediasession.ComponentHandle{video, audio}, s.stopTimeout), nil
}

func (s *ManagedMediaSender) videoSourceCleanup() mediasession.ComponentHandle {
	if s == nil || s.videoSourceOwner == nil {
		return nil
	}
	return s.videoSourceOwner.takeSourceCleanup(s.stopTimeout)
}

func (s *ManagedMediaSender) audioSourceCleanup() mediasession.ComponentHandle {
	if s == nil || s.audioSourceOwner == nil {
		return nil
	}
	return s.audioSourceOwner.takeSourceCleanup(s.stopTimeout)
}

func (s *ManagedMediaSender) startFailure(video, audio mediasession.ComponentHandle, sources ...mediasession.ComponentHandle) (mediasession.ComponentHandle, error) {
	children := make([]mediasession.ComponentHandle, 0, 2+len(sources))
	if video != nil {
		children = append(children, video)
	}
	if audio != nil {
		children = append(children, audio)
	}
	for _, source := range sources {
		if source != nil {
			children = append(children, source)
		}
	}
	if len(children) == 0 {
		return nil, ErrManagedMediaSenderStart
	}
	handle := newManagedMediaPartialHandle(children, s.stopTimeout)
	if err := handle.Stop(context.Background()); err != nil {
		return handle, ErrManagedMediaSenderStart
	}
	return nil, ErrManagedMediaSenderStart
}

type managedSourceCloser interface {
	Close() error
}

func cleanupManagedStartChildren(children []mediasession.ComponentHandle, timeout time.Duration) (mediasession.ComponentHandle, error) {
	return cleanupManagedStartChildrenWithError(children, timeout, ErrManagedMediaSenderStart)
}

func cleanupManagedStartChildrenWithError(children []mediasession.ComponentHandle, timeout time.Duration, failure error) (mediasession.ComponentHandle, error) {
	return cleanupManagedStartChildrenWithStopError(children, timeout, failure, ErrManagedMediaSenderStop)
}

func cleanupManagedStartChildrenWithStopError(children []mediasession.ComponentHandle, timeout time.Duration, failure, stopErr error) (mediasession.ComponentHandle, error) {
	if len(children) == 0 {
		return nil, failure
	}
	handle := newManagedMediaPartialHandle(children, timeout)
	handle.stopErr = stopErr
	if err := handle.Stop(context.Background()); err != nil {
		return handle, failure
	}
	return nil, failure
}

func cleanupManagedStartChildrenWithoutAttempt(children []mediasession.ComponentHandle, timeout time.Duration) (mediasession.ComponentHandle, error) {
	if len(children) == 0 {
		return nil, ErrManagedMediaSenderStart
	}
	return newManagedMediaPartialHandle(children, timeout), ErrManagedMediaSenderStart
}

// managedSourceCleanupHandle owns a source that never reached a runner. Close
// is attempted independently and remains retryable after an error or timeout.
// The underlying Close call is deliberately isolated in a goroutine because
// the source interface has no context parameter.
type managedSourceCleanupHandle struct {
	source  managedSourceCloser
	timeout time.Duration

	mu       sync.Mutex
	closed   bool
	inFlight bool
}

// managedRunnerAndSourceCleanupHandle preserves the dependency between a
// partially constructed runner and the source passed to it. The runner is
// always stopped first; if that attempt fails or times out, the source remains
// open for a retry rather than being released underneath an in-flight worker.
type managedRunnerAndSourceCleanupHandle struct {
	mu      sync.Mutex
	attempt sync.Mutex
	runner  mediasession.ComponentHandle
	source  mediasession.ComponentHandle
	timeout time.Duration
	stopErr error
}

func cleanupManagedRunnerAndSource(runner, source mediasession.ComponentHandle, timeout time.Duration, failure, stopErr error) (mediasession.ComponentHandle, error) {
	handle := &managedRunnerAndSourceCleanupHandle{runner: runner, source: source, timeout: timeout, stopErr: stopErr}
	if err := handle.Stop(context.Background()); err != nil {
		return handle, failure
	}
	return nil, failure
}

func (h *managedRunnerAndSourceCleanupHandle) Stop(ctx context.Context) error {
	if h == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	h.attempt.Lock()
	defer h.attempt.Unlock()
	timeout := h.timeout
	if timeout <= 0 {
		timeout = defaultManagedSenderStopTimeout
	}
	h.mu.Lock()
	runner, source := h.runner, h.source
	h.mu.Unlock()
	if runner != nil {
		stopCtx, cancel := context.WithTimeout(ctx, timeout)
		err := runner.Stop(stopCtx)
		cancel()
		if err != nil {
			return h.stopError()
		}
		h.mu.Lock()
		h.runner = nil
		h.mu.Unlock()
	}
	if source != nil {
		stopCtx, cancel := context.WithTimeout(ctx, timeout)
		err := source.Stop(stopCtx)
		cancel()
		if err != nil {
			return h.stopError()
		}
		h.mu.Lock()
		h.source = nil
		h.mu.Unlock()
	}
	return nil
}

func (h *managedRunnerAndSourceCleanupHandle) stopError() error {
	if h != nil && h.stopErr != nil {
		return h.stopErr
	}
	return ErrManagedMediaSenderStop
}

func newManagedSourceCleanupHandle(source managedSourceCloser, timeout time.Duration) *managedSourceCleanupHandle {
	return &managedSourceCleanupHandle{source: source, timeout: timeout}
}

func (h *managedSourceCleanupHandle) Stop(ctx context.Context) error {
	if h == nil || h.source == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	if h.inFlight {
		h.mu.Unlock()
		return ErrManagedMediaSenderStop
	}
	h.inFlight = true
	h.mu.Unlock()

	result := make(chan error, 1)
	go func() {
		err := h.source.Close()
		h.mu.Lock()
		h.inFlight = false
		if err == nil {
			h.closed = true
		}
		h.mu.Unlock()
		result <- err
	}()

	timeout := h.timeout
	if timeout <= 0 {
		timeout = defaultManagedSenderStopTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-result:
		if err != nil {
			return ErrManagedMediaSenderStop
		}
		return nil
	case <-ctx.Done():
		return ErrManagedMediaSenderStop
	case <-timer.C:
		return ErrManagedMediaSenderStop
	}
}

type managedMediaHandle struct {
	mu       sync.Mutex
	children []mediasession.ComponentHandle
	timeout  time.Duration
	stopErr  error
	done     chan struct{}
	doneOnce sync.Once
}

func newManagedMediaHandle(children []mediasession.ComponentHandle, timeout time.Duration) *managedMediaHandle {
	h := &managedMediaHandle{children: children, timeout: timeout, stopErr: ErrManagedMediaSenderStop, done: make(chan struct{})}
	h.monitorChildren()
	return h
}

// A failed Start returns a retryable partial handle. It deliberately does not
// watch terminal children: the caller owns its retry and a terminal child must
// not race the first bounded rollback attempt.
func newManagedMediaPartialHandle(children []mediasession.ComponentHandle, timeout time.Duration) *managedMediaHandle {
	return &managedMediaHandle{children: children, timeout: timeout, stopErr: ErrManagedMediaSenderStop, done: make(chan struct{})}
}

func (h *managedMediaHandle) monitorChildren() {
	for _, child := range h.children {
		if terminal, ok := child.(interface{ Done() <-chan struct{} }); ok && terminal.Done() != nil {
			go func(done <-chan struct{}) {
				<-done
				h.doneOnce.Do(func() { close(h.done) })
				_ = h.Stop(context.Background())
			}(terminal.Done())
		}
	}
}

func (h *managedMediaHandle) Done() <-chan struct{} {
	if h == nil {
		return nil
	}
	return h.done
}

func (h *managedMediaHandle) Stop(ctx context.Context) error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	remaining := make([]mediasession.ComponentHandle, 0, len(h.children))
	var first error
	for index := len(h.children) - 1; index >= 0; index-- {
		stopCtx, cancel := context.WithTimeout(context.Background(), h.timeout)
		err := h.children[index].Stop(stopCtx)
		cancel()
		if err != nil {
			remaining = append([]mediasession.ComponentHandle{h.children[index]}, remaining...)
			if first == nil {
				first = h.stopError()
			}
		}
	}
	h.children = remaining
	if len(remaining) == 0 {
		h.doneOnce.Do(func() { close(h.done) })
	}
	return first
}

func (h *managedMediaHandle) stopError() error {
	if h != nil && h.stopErr != nil {
		return h.stopErr
	}
	return ErrManagedMediaSenderStop
}
