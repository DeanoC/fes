package remotemedia

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/DeanoC/FogCast-POC/internal/mediasession"
)

var (
	ErrManagedSenderStart = errors.New("managed sender failed to start")
	ErrManagedSenderStop  = errors.New("managed sender failed to stop")
)

const defaultManagedSenderStopTimeout = 2 * time.Second

// ManagedSenderRunner is the small lifecycle surface used by ManagedSender.
// Sender implements it; the interface also permits deterministic tests.
type ManagedSenderRunner interface {
	Run(context.Context) error
	Close() error
}

// ManagedSenderReadyRunner can report errors encountered before Run enters its
// long-running loop. It is optional so existing injected runners remain valid.
type ManagedSenderReadyRunner interface {
	ManagedSenderRunner
	RunReady(context.Context, func(error)) error
}

// ManagedSenderConfig contains the authenticated sender and transport
// settings. Capture is supplied separately so physical capture remains an
// injectable concern and this component does not open hardware itself.
type ManagedSenderConfig struct {
	Session           string
	Generation        uint64
	Token             string
	RTPAddress        string
	ControlAddress    string
	SSRC              uint32
	InitialSequence   uint16
	RTPBaseTimestamp  uint32
	MTU               int
	Bitrate           int
	GOP               int
	PeriodicKeyframes int
	KeyframeInterval  time.Duration
}

type ManagedSenderOption func(*ManagedSender)
type managedSenderFactory func(SenderConfig, CaptureSource) (ManagedSenderRunner, error)

// WithManagedSenderFactory injects sender construction for deterministic tests.
func WithManagedSenderFactory(factory func(SenderConfig, CaptureSource) (ManagedSenderRunner, error)) ManagedSenderOption {
	return func(s *ManagedSender) {
		if factory != nil {
			s.newSender = factory
		}
	}
}

// WithManagedSenderStopTimeout bounds cleanup performed by a handle.
func WithManagedSenderStopTimeout(timeout time.Duration) ManagedSenderOption {
	return func(s *ManagedSender) {
		if timeout > 0 {
			s.stopTimeout = timeout
		}
	}
}

type ManagedSender struct {
	config      ManagedSenderConfig
	source      CaptureSource
	newSender   managedSenderFactory
	stopTimeout time.Duration
}

// NewManagedSender creates a sender component without opening capture hardware
// or network sockets. Configuration is validated again by Start because the
// component may be constructed before a session is selected.
func NewManagedSender(config ManagedSenderConfig, source CaptureSource, options ...ManagedSenderOption) (*ManagedSender, error) {
	if source == nil {
		return nil, errors.New("capture source is required")
	}
	s := &ManagedSender{config: config, source: source, newSender: defaultManagedSenderFactory, stopTimeout: defaultManagedSenderStopTimeout}
	for _, option := range options {
		if option != nil {
			option(s)
		}
	}
	return s, nil
}

func defaultManagedSenderFactory(config SenderConfig, source CaptureSource) (ManagedSenderRunner, error) {
	return NewSender(config, source, nil)
}

func (s *ManagedSender) senderConfig() SenderConfig {
	periodic := s.config.PeriodicKeyframes
	if periodic == 0 && s.config.GOP > 0 {
		periodic = s.config.GOP
	}
	return SenderConfig{
		RTPAddress: s.config.RTPAddress, ControlAddress: s.config.ControlAddress,
		Session: s.config.Session, Generation: s.config.Generation, Token: s.config.Token,
		SSRC: s.config.SSRC, InitialSequence: s.config.InitialSequence,
		RTPBaseTimestamp: s.config.RTPBaseTimestamp, MTU: s.config.MTU,
		Bitrate: s.config.Bitrate, PeriodicKeyframes: periodic,
		KeyframeInterval: s.config.KeyframeInterval,
	}
}

func (s *ManagedSender) validate() error {
	if s == nil || s.source == nil || s.newSender == nil || s.config.Session == "" || s.config.Token == "" || s.config.RTPAddress == "" || s.config.SSRC == 0 {
		return ErrManagedSenderStart
	}
	if s.config.MTU != 0 && s.config.MTU < RTPHeaderSize+3 {
		return ErrManagedSenderStart
	}
	if s.config.Bitrate < 0 || s.config.GOP < 0 || s.config.PeriodicKeyframes < 0 || s.config.KeyframeInterval < 0 {
		return ErrManagedSenderStart
	}
	return nil
}

var _ mediasession.Component = (*ManagedSender)(nil)

func (s *ManagedSender) Start(ctx context.Context, _ string) (mediasession.ComponentHandle, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil || s.validate() != nil {
		return nil, ErrManagedSenderStart
	}
	runner, err := s.newSender(s.senderConfig(), s.source)
	if err != nil || runner == nil {
		if runner != nil {
			_ = runner.Close()
		}
		return nil, ErrManagedSenderStart
	}
	if ctx.Err() != nil {
		_ = runner.Close()
		return nil, ErrManagedSenderStart
	}
	if readyRunner, ok := runner.(ManagedSenderReadyRunner); ok {
		ready := make(chan error, 1)
		runDone := make(chan struct{})
		runCtx, cancel := context.WithCancel(ctx)
		go func() {
			defer close(runDone)
			_ = readyRunner.RunReady(runCtx, func(err error) { ready <- err })
		}()
		select {
		case err := <-ready:
			if err != nil {
				cancel()
				_ = runner.Close()
				return nil, ErrManagedSenderStart
			}
			h := newManagedSenderHandleWithDone(runCtx, cancel, runner, runDone, s.stopTimeout)
			return h, nil
		case <-runDone:
			cancel()
			_ = runner.Close()
			return nil, ErrManagedSenderStart
		case <-ctx.Done():
			cancel()
			_ = runner.Close()
			return nil, ErrManagedSenderStart
		}
	}
	h := newManagedSenderHandle(ctx, runner, s.stopTimeout)
	return h, nil
}

type managedSenderHandle struct {
	runner  ManagedSenderRunner
	cancel  context.CancelFunc
	runDone chan struct{}
	timeout time.Duration

	once sync.Once
	done chan struct{}
	mu   sync.Mutex
	err  error
}

func newManagedSenderHandle(parent context.Context, runner ManagedSenderRunner, timeout time.Duration) *managedSenderHandle {
	ctx, cancel := context.WithCancel(parent)
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		_ = runner.Run(ctx)
	}()
	return newManagedSenderHandleWithDone(ctx, cancel, runner, runDone, timeout)
}

func newManagedSenderHandleWithDone(ctx context.Context, cancel context.CancelFunc, runner ManagedSenderRunner, runDone chan struct{}, timeout time.Duration) *managedSenderHandle {
	h := &managedSenderHandle{runner: runner, cancel: cancel, runDone: runDone, done: make(chan struct{}), timeout: timeout}
	go func() {
		<-ctx.Done()
		_ = h.Stop(context.Background())
	}()
	return h
}

func (h *managedSenderHandle) Stop(ctx context.Context) error {
	if h == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	h.once.Do(func() { go h.cleanup() })
	timer := time.NewTimer(h.timeout)
	defer timer.Stop()
	select {
	case <-h.done:
		h.mu.Lock()
		err := h.err
		h.mu.Unlock()
		return err
	case <-ctx.Done():
		return ErrManagedSenderStop
	case <-timer.C:
		return ErrManagedSenderStop
	}
}

func (h *managedSenderHandle) cleanup() {
	defer close(h.done)
	h.cancel()
	if err := h.runner.Close(); err != nil {
		h.setError(ErrManagedSenderStop)
	}
	if !waitManagedSender(h.runDone, h.timeout) {
		h.setError(ErrManagedSenderStop)
	}
}

func (h *managedSenderHandle) setError(err error) {
	h.mu.Lock()
	if h.err == nil {
		h.err = err
	}
	h.mu.Unlock()
}
func waitManagedSender(done <-chan struct{}, timeout time.Duration) bool {
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-done:
		return true
	case <-t.C:
		return false
	}
}
