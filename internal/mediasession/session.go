// Package mediasession provides the lifecycle boundary for host-only media.
//
// It intentionally knows nothing about capture devices, codecs, sockets, or
// displays. Those concerns are supplied as component implementations so this
// boundary can be tested and composed without claiming hardware readiness.
package mediasession

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const defaultStopTimeout = 2 * time.Second

var (
	ErrInvalidConfiguration = errors.New("media session configuration is invalid")
	ErrComponentStart       = errors.New("media session component failed to start")
	ErrComponentStop        = errors.New("media session component failed to stop")
)

// Component starts one part of a media session for a game and returns its
// owned lifecycle handle. Implementations must honor the context while
// starting and must make Handle.Stop safe to call with a deadline.
type Component interface {
	Start(context.Context, string) (ComponentHandle, error)
}

// ComponentHandle owns resources started by a Component.
type ComponentHandle interface {
	Stop(context.Context) error
}

// Handle is the lifecycle handle returned by Session.Start. Callers that
// expose this through another package's interface should use an explicit
// adapter at that package boundary; Go does not support covariant interface
// return types.
type Handle interface {
	Stop(context.Context) error
}

type Option func(*Session)

// WithStopTimeout bounds each reverse-order component cleanup. The default is
// two seconds per component. Each component receives a fresh cleanup budget so
// one blocked component cannot consume the next component's deadline.
func WithStopTimeout(timeout time.Duration) Option {
	return func(s *Session) {
		if timeout > 0 {
			s.stopTimeout = timeout
		}
	}
}

// Session starts the receiver before the sender, then stops them in reverse
// order. Receiver-first startup prevents a newly started sender from emitting
// into an unready receiver.
type Session struct {
	sender   Component
	receiver Component

	stopTimeout time.Duration
}

func New(sender, receiver Component, options ...Option) *Session {
	s := &Session{sender: sender, receiver: receiver, stopTimeout: defaultStopTimeout}
	for _, option := range options {
		if option != nil {
			option(s)
		}
	}
	return s
}

type sessionHandle struct {
	components []ComponentHandle
	timeout    time.Duration
	mu         sync.Mutex
	doneOnce   sync.Once
	done       chan struct{}
	err        error
}

func (s *Session) Start(ctx context.Context, gameID string) (Handle, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(gameID) == "" {
		return nil, fmt.Errorf("%w: game id is required", ErrInvalidConfiguration)
	}
	if s == nil || s.sender == nil || s.receiver == nil {
		return nil, fmt.Errorf("%w: sender and receiver are required", ErrInvalidConfiguration)
	}

	started := make([]ComponentHandle, 0, 2)
	start := func(role string, component Component) (Handle, error) {
		handle, err := component.Start(ctx, gameID)
		if handle != nil {
			started = append(started, handle)
		}
		if err != nil {
			return failedStartHandle(s.cleanup(ctx, started), s.stopTimeout, role)
		}
		if handle == nil {
			return failedStartHandle(s.cleanup(ctx, started), s.stopTimeout, role)
		}
		return nil, nil
	}

	if partial, err := start("receiver", s.receiver); err != nil {
		return partial, err
	}
	if partial, err := start("sender", s.sender); err != nil {
		return partial, err
	}
	handle := newSessionHandle(started, s.stopTimeout)
	for _, component := range started {
		if terminal, ok := component.(interface{ Done() <-chan struct{} }); ok && terminal.Done() != nil {
			go func(done <-chan struct{}) {
				<-done
				handle.doneOnce.Do(func() { close(handle.done) })
			}(terminal.Done())
		}
	}
	return handle, nil
}

func boundedContext(_ context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}

func (s *Session) cleanup(parent context.Context, components []ComponentHandle) []ComponentHandle {
	remaining := make([]ComponentHandle, 0, len(components))
	for i := len(components) - 1; i >= 0; i-- {
		cleanupCtx, cancel := boundedContext(parent, s.stopTimeout)
		err := components[i].Stop(cleanupCtx)
		cancel()
		if err != nil {
			remaining = append([]ComponentHandle{components[i]}, remaining...)
		}
	}
	return remaining
}

func failedStartHandle(remaining []ComponentHandle, timeout time.Duration, role string) (Handle, error) {
	err := fmt.Errorf("%w: %s", ErrComponentStart, role)
	if handle := newSessionHandle(remaining, timeout); handle != nil {
		return handle, err
	}
	return nil, err
}

func newSessionHandle(components []ComponentHandle, timeout time.Duration) *sessionHandle {
	if len(components) == 0 {
		return nil
	}
	return &sessionHandle{components: components, timeout: timeout, done: make(chan struct{})}
}

func (h *sessionHandle) Stop(ctx context.Context) error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	remaining := make([]ComponentHandle, 0, len(h.components))
	var first error
	for i := len(h.components) - 1; i >= 0; i-- {
		stopCtx, cancel := boundedContext(ctx, h.timeout)
		err := h.components[i].Stop(stopCtx)
		cancel()
		if err != nil {
			remaining = append([]ComponentHandle{h.components[i]}, remaining...)
			if first == nil {
				first = fmt.Errorf("%w: component %d", ErrComponentStop, i)
			}
		}
	}
	h.components = remaining
	h.err = first
	if len(remaining) == 0 {
		h.doneOnce.Do(func() { close(h.done) })
	}
	return first
}

func (h *sessionHandle) Done() <-chan struct{} {
	if h == nil {
		return nil
	}
	return h.done
}
