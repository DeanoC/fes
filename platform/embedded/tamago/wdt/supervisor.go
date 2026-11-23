// SPDX-License-Identifier: GPL-3.0-or-later

package wdt

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// ErrAlreadyStarted is returned when Supervisor.Start is called more than once.
	ErrAlreadyStarted = errors.New("wdt: supervisor already started")
	// ErrStopped is returned when Supervisor.Start is called after Supervisor.Stop.
	ErrStopped = errors.New("wdt: supervisor stopped")
)

// Kicker is the minimal watchdog interface driven by Supervisor (implemented by *DW).
type Kicker interface {
	Start(timeout time.Duration, mode ResponseMode) (time.Duration, error)
	Kick() error
}

// SupervisorConfig configures an opt-in watchdog keepalive Supervisor.
type SupervisorConfig struct {
	// Timeout is the requested hardware watchdog timeout passed to Kicker.Start.
	Timeout time.Duration
	// KickInterval is how often the supervisor kicks the watchdog. When zero,
	// it defaults to one third of the actual hardware period returned by Start.
	// If non-zero and >= the achieved hardware period, it is clamped to actual/3
	// so the supervisor never kicks slower than hardware expiry.
	KickInterval time.Duration
	// Mode selects ModeReset or ModePreTimeoutIRQ.
	Mode ResponseMode
	// Check is an optional health probe invoked before every kick. A non-nil
	// error halts further kicks so the hardware watchdog can recover a wedged
	// system, records the error in Err(), and notifies OnError.
	Check func() error
	// OnError is an optional hook invoked when Check or Kick returns an error.
	// It must not call Supervisor.Stop synchronously.
	OnError func(err error)
}

// Supervisor runs an opt-in periodic keepalive goroutine for a watchdog.
// Creating a Supervisor does not arm hardware; callers must invoke Start.
// Note: stopping a Supervisor stops keepalive kicks and waits for any in-flight
// Check/Kick/OnError call to finish, but does not clear DesignWare WDT_EN in
// hardware (which remains sticky until peripheral reset).
type Supervisor struct {
	dev Kicker
	cfg SupervisorConfig

	mu       sync.Mutex
	started  bool
	stopped  bool
	running  atomic.Bool
	interval time.Duration
	stopCh   chan struct{}
	wg       sync.WaitGroup

	kicks   atomic.Uint64
	errMu   sync.Mutex
	lastErr error
}

// NewSupervisor validates cfg and returns an unarmed Supervisor.
func NewSupervisor(dev Kicker, cfg SupervisorConfig) (*Supervisor, error) {
	if dev == nil {
		return nil, ErrNilAccess
	}
	if cfg.Timeout <= 0 {
		return nil, fmt.Errorf("%w: timeout must be > 0", ErrInvalidConfig)
	}
	if cfg.KickInterval < 0 {
		return nil, fmt.Errorf("%w: kick interval must be >= 0", ErrInvalidConfig)
	}
	if cfg.KickInterval >= cfg.Timeout {
		return nil, fmt.Errorf("%w: kick interval must be < timeout", ErrInvalidConfig)
	}
	if cfg.Mode != ModeReset && cfg.Mode != ModePreTimeoutIRQ {
		return nil, fmt.Errorf("%w: unknown response mode %d", ErrInvalidConfig, cfg.Mode)
	}
	return &Supervisor{
		dev:    dev,
		cfg:    cfg,
		stopCh: make(chan struct{}),
	}, nil
}

// Start arms the underlying watchdog and starts the keepalive goroutine.
// It returns the actual single-stage hardware period reported by Kicker.Start.
func (s *Supervisor) Start(ctx context.Context) (time.Duration, error) {
	if ctx == nil {
		return 0, fmt.Errorf("%w: nil context", ErrInvalidConfig)
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return 0, ErrStopped
	}
	if s.started {
		return 0, ErrAlreadyStarted
	}

	actual, err := s.dev.Start(s.cfg.Timeout, s.cfg.Mode)
	if err != nil {
		return 0, err
	}
	if actual <= 0 {
		return 0, fmt.Errorf("%w: watchdog reported non-positive period %v", ErrInvalidConfig, actual)
	}

	interval := s.cfg.KickInterval
	if interval == 0 || interval >= actual {
		interval = actual / 3
		if interval <= 0 {
			interval = time.Nanosecond
		}
	}

	s.started = true
	s.interval = interval
	s.running.Store(true)
	s.wg.Add(1)
	go s.loop(ctx, interval)
	return actual, nil
}

// Stop halts keepalive kicks and waits for the supervisor goroutine (including
// any in-flight Check, Kick, or OnError callback) to exit. Idempotent.
func (s *Supervisor) Stop() {
	s.mu.Lock()
	if !s.stopped {
		s.stopped = true
		close(s.stopCh)
	}
	s.mu.Unlock()
	s.wg.Wait()
}

// Kicks reports the number of successful periodic kicks performed since Start.
func (s *Supervisor) Kicks() uint64 { return s.kicks.Load() }

// Running reports whether the keepalive goroutine is currently active.
func (s *Supervisor) Running() bool { return s.running.Load() }

// Interval reports the effective kick interval selected by Start (0 before Start).
func (s *Supervisor) Interval() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.interval
}

// Err returns the first fatal Check or Kick error that stopped keepalive kicks.
func (s *Supervisor) Err() error {
	s.errMu.Lock()
	defer s.errMu.Unlock()
	return s.lastErr
}

func (s *Supervisor) recordErr(err error) {
	s.errMu.Lock()
	if s.lastErr == nil {
		s.lastErr = err
	}
	s.errMu.Unlock()
	if s.cfg.OnError != nil {
		s.cfg.OnError(err)
	}
}

func (s *Supervisor) loop(ctx context.Context, interval time.Duration) {
	defer func() {
		s.running.Store(false)
		s.wg.Done()
	}()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.cfg.Check != nil {
				if err := s.cfg.Check(); err != nil {
					s.recordErr(err)
					return
				}
			}
			if err := s.dev.Kick(); err != nil {
				s.recordErr(err)
				return
			}
			s.kicks.Add(1)
		}
	}
}
