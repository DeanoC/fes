// SPDX-License-Identifier: GPL-3.0-or-later

package network

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"time"
)

const (
	defaultSNTPPollInterval  = 15 * time.Minute
	defaultSNTPRetryInterval = 30 * time.Second
)

var (
	// ErrSNTPSyncerStarted is returned when SNTPSyncer.Start is called more than once.
	ErrSNTPSyncerStarted = errors.New("network: SNTP syncer already started")
	// ErrSNTPSyncerStopped is returned when SNTPSyncer.Start is called after Stop.
	ErrSNTPSyncerStopped = errors.New("network: SNTP syncer stopped")
	// ErrSNTPStepTooLarge is returned when an SNTP sample deviates from the
	// already-synchronized local UTC expectation by more than MaxStep.
	ErrSNTPStepTooLarge = errors.New("network: SNTP sample step exceeds MaxStep")
)

// WallClockSink receives validated UTC epochs from SNTPSyncer (satisfied by *rtc.Clock).
type WallClockSink interface {
	Sync(time.Time)
}

// SNTPQueryFunc overrides the raw-frame SNTP query function for unit tests or custom bindings.
type SNTPQueryFunc func(ctx context.Context, cfg SNTPConfig) (SNTPResult, error)

// SNTPSyncConfig configures periodic RFC 4330 SNTPv4 wall-clock discipline.
type SNTPSyncConfig struct {
	SNTP             SNTPConfig
	Sink             WallClockSink
	PollInterval     time.Duration    // Interval between successful syncs (default 15m)
	RetryInterval    time.Duration    // Initial backoff after failed sync (default 30s)
	MaxRetryInterval time.Duration    // Maximum exponential retry backoff (default max(PollInterval, RetryInterval))
	MaxStep          time.Duration    // Optional maximum clock step allowed after initial sync (0 disables)
	Query            SNTPQueryFunc    // Optional query override; nil uses QuerySNTP over port
	Now              func() time.Time // Optional monotonic reference for MaxStep drift checks
	OnSync           func(SNTPResult) // Optional callback invoked outside mutex after Sink.Sync
	OnError          func(error)      // Optional callback invoked outside mutex on non-paused failure
}

// SNTPSyncStatus summarizes SNTPSyncer synchronization state and counters.
type SNTPSyncStatus struct {
	Synced     bool
	LastSync   time.Time
	LastResult SNTPResult
	SyncCount  uint64
	ErrorCount uint64
	LastErr    error
}

// SNTPSyncer periodically queries RFC 4330 SNTPv4 servers and disciplines a
// monotonic-anchored wall clock via WallClockSink.Sync without modifying
// runtime monotonic time.
type SNTPSyncer struct {
	port             RawFramePort
	cfg              SNTPSyncConfig
	queryFn          SNTPQueryFunc
	nowFn            func() time.Time
	pollInterval     time.Duration
	retryInterval    time.Duration
	maxRetryInterval time.Duration

	mu           sync.Mutex
	started      bool
	stopped      bool
	stopCh       chan struct{}
	wg           sync.WaitGroup
	synced       bool
	lastSync     time.Time
	lastSyncMono time.Time
	lastResult   SNTPResult
	syncCount    uint64
	errorCount   uint64
	lastErr      error
}

// NewSNTPSyncer validates cfg and creates an idle SNTPSyncer over port (or cfg.Query).
func NewSNTPSyncer(port RawFramePort, cfg SNTPSyncConfig) (*SNTPSyncer, error) {
	if cfg.Query == nil && port == nil {
		return nil, fmt.Errorf("%w: nil frame port", ErrSNTPInvalidConfig)
	}
	if cfg.PollInterval < 0 || cfg.RetryInterval < 0 || cfg.MaxRetryInterval < 0 || cfg.MaxStep < 0 {
		return nil, fmt.Errorf("%w: negative duration in sync config", ErrSNTPInvalidConfig)
	}
	if cfg.Query == nil {
		if _, err := parseUnicastMAC(cfg.SNTP.LocalMAC); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrSNTPInvalidConfig, err)
		}
		localPrefix, err := netip.ParsePrefix(cfg.SNTP.LocalPrefix)
		if err != nil || !localPrefix.Addr().Is4() || localPrefix.Bits() < 1 || localPrefix.Bits() > 30 || !usableHost(localPrefix, localPrefix.Addr()) {
			return nil, fmt.Errorf("%w: invalid local IPv4 prefix", ErrSNTPInvalidConfig)
		}
		if cfg.SNTP.Gateway != "" {
			if _, err := staticConfig(cfg.SNTP.LocalPrefix, cfg.SNTP.Gateway); err != nil {
				return nil, fmt.Errorf("%w: %v", ErrSNTPInvalidConfig, err)
			}
		}
		if len(cfg.SNTP.Servers) == 0 {
			return nil, fmt.Errorf("%w: no SNTP servers configured", ErrSNTPInvalidConfig)
		}
		for _, s := range cfg.SNTP.Servers {
			a, err := netip.ParseAddr(strings.TrimSpace(s))
			if err != nil || !a.Is4() || a.IsUnspecified() || a.IsMulticast() {
				return nil, fmt.Errorf("%w: invalid SNTP server %q", ErrSNTPInvalidConfig, s)
			}
		}
	}
	poll := cfg.PollInterval
	if poll == 0 {
		poll = defaultSNTPPollInterval
	}
	retry := cfg.RetryInterval
	if retry == 0 {
		retry = defaultSNTPRetryInterval
	}
	maxRetry := cfg.MaxRetryInterval
	if maxRetry == 0 {
		maxRetry = max(poll, retry)
	}
	if maxRetry < retry {
		maxRetry = retry
	}
	queryFn := cfg.Query
	if queryFn == nil {
		queryFn = func(ctx context.Context, qcfg SNTPConfig) (SNTPResult, error) {
			return QuerySNTP(ctx, port, qcfg)
		}
	}
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	return &SNTPSyncer{
		port:             port,
		cfg:              cfg,
		queryFn:          queryFn,
		nowFn:            nowFn,
		pollInterval:     poll,
		retryInterval:    retry,
		maxRetryInterval: maxRetry,
		stopCh:           make(chan struct{}),
	}, nil
}

// SyncOnce performs a single SNTPv4 query, enforces MaxStep if already synced,
// updates Sink (if non-nil), and records Status metrics. Calls participate in
// Stop's lifecycle barrier; invoking Stop from OnSync or OnError never deadlocks.
func (s *SNTPSyncer) SyncOnce(ctx context.Context) (SNTPResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return SNTPResult{}, err
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return SNTPResult{}, ErrSNTPSyncerStopped
	}
	s.wg.Add(1)
	s.mu.Unlock()
	defer s.wg.Done()

	opCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	doneWatch := make(chan struct{})
	defer close(doneWatch)
	go func() {
		select {
		case <-s.stopCh:
			cancel()
		case <-doneWatch:
		}
	}()

	res, err := s.queryFn(opCtx, s.cfg.SNTP)
	if err != nil {
		if !errors.Is(err, ErrPaused) {
			s.mu.Lock()
			s.errorCount++
			s.lastErr = err
			s.mu.Unlock()
			if s.cfg.OnError != nil {
				go s.cfg.OnError(err)
			}
		} else {
			s.mu.Lock()
			s.lastErr = err
			s.mu.Unlock()
		}
		return SNTPResult{}, err
	}

	nowMono := s.nowFn()
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return SNTPResult{}, ErrSNTPSyncerStopped
	}
	if s.synced && s.cfg.MaxStep > 0 {
		elapsed := nowMono.Sub(s.lastSyncMono)
		if elapsed < 0 {
			elapsed = 0
		}
		expected := s.lastSync.Add(elapsed)
		step := res.Time.Sub(expected)
		if step < 0 {
			step = -step
		}
		if step > s.cfg.MaxStep {
			stepErr := fmt.Errorf("%w: offset=%v max=%v", ErrSNTPStepTooLarge, step, s.cfg.MaxStep)
			s.errorCount++
			s.lastErr = stepErr
			s.mu.Unlock()
			if s.cfg.OnError != nil {
				go s.cfg.OnError(stepErr)
			}
			return SNTPResult{}, stepErr
		}
	}
	s.synced = true
	s.lastSync = res.Time
	s.lastSyncMono = nowMono
	s.lastResult = res
	s.syncCount++
	s.lastErr = nil
	if s.cfg.Sink != nil {
		s.cfg.Sink.Sync(res.Time)
	}
	s.mu.Unlock()

	if s.cfg.OnSync != nil {
		go s.cfg.OnSync(res)
	}
	return res, nil
}

// Start launches the periodic SNTP discipline loop in a background goroutine.
func (s *SNTPSyncer) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return ErrSNTPSyncerStopped
	}
	if s.started {
		s.mu.Unlock()
		return ErrSNTPSyncerStarted
	}
	s.started = true
	s.wg.Add(1)
	s.mu.Unlock()

	go s.run(ctx)
	return nil
}

func (s *SNTPSyncer) run(ctx context.Context) {
	defer s.wg.Done()
	loopCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-s.stopCh:
			cancel()
		case <-loopCtx.Done():
		}
	}()

	backoff := s.retryInterval
	for {
		if loopCtx.Err() != nil {
			return
		}
		_, err := s.SyncOnce(loopCtx)
		wait := s.pollInterval
		if err == nil {
			backoff = s.retryInterval
		} else if errors.Is(err, ErrPaused) {
			wait = s.retryInterval
		} else {
			wait = backoff
			if backoff < s.maxRetryInterval {
				backoff = min(backoff*2, s.maxRetryInterval)
			}
		}

		timer := time.NewTimer(wait)
		select {
		case <-loopCtx.Done():
			timer.Stop()
			return
		case <-s.stopCh:
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// Stop halts the background SNTP discipline loop and waits for all in-flight
// queries and Sink.Sync updates (from both Start and direct SyncOnce calls) to
// complete before returning. Safe to call multiple times, without Start, or from
// OnSync/OnError callbacks.
func (s *SNTPSyncer) Stop() {
	s.mu.Lock()
	if !s.stopped {
		s.stopped = true
		close(s.stopCh)
	}
	s.mu.Unlock()
	s.wg.Wait()
}

// Status returns a snapshot of the syncer's synchronization state and counters.
func (s *SNTPSyncer) Status() SNTPSyncStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return SNTPSyncStatus{
		Synced:     s.synced,
		LastSync:   s.lastSync,
		LastResult: s.lastResult,
		SyncCount:  s.syncCount,
		ErrorCount: s.errorCount,
		LastErr:    s.lastErr,
	}
}
