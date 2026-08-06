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

// WithStopTimeout bounds the complete reverse-order component cleanup. The
// default is two seconds. A caller context deadline, when earlier, remains
// authoritative.
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
	once       sync.Once
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
	start := func(role string, component Component) error {
		handle, err := component.Start(ctx, gameID)
		if handle != nil {
			started = append(started, handle)
		}
		if err != nil {
			s.cleanup(ctx, started)
			return fmt.Errorf("%w: %s", ErrComponentStart, role)
		}
		if handle == nil {
			s.cleanup(ctx, started)
			return fmt.Errorf("%w: %s", ErrComponentStart, role)
		}
		return nil
	}

	if err := start("receiver", s.receiver); err != nil {
		return nil, err
	}
	if err := start("sender", s.sender); err != nil {
		return nil, err
	}
	return &sessionHandle{components: started, timeout: s.stopTimeout}, nil
}

func boundedContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if parent == nil || parent.Err() != nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, timeout)
}

func (s *Session) cleanup(parent context.Context, components []ComponentHandle) {
	cleanupCtx, cancel := boundedContext(parent, s.stopTimeout)
	defer cancel()
	for i := len(components) - 1; i >= 0; i-- {
		_ = components[i].Stop(cleanupCtx)
	}
}

func (h *sessionHandle) Stop(ctx context.Context) error {
	if h == nil {
		return nil
	}
	h.once.Do(func() {
		if ctx == nil {
			ctx = context.Background()
		}
		stopCtx, cancel := boundedContext(ctx, h.timeout)
		defer cancel()
		for i := len(h.components) - 1; i >= 0; i-- {
			if err := h.components[i].Stop(stopCtx); err != nil && h.err == nil {
				h.err = fmt.Errorf("%w: component %d", ErrComponentStop, i)
			}
		}
	})
	return h.err
}
