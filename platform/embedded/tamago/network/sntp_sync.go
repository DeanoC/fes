// SPDX-License-Identifier: GPL-3.0-or-later

package network

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultSNTPPollInterval  = 15 * time.Minute
	defaultSNTPRetryInterval = 30 * time.Second
	defaultSNTPMaxStep       = 5 * time.Minute
	defaultSNTPCallbackQueue = 8
)

var (
	defaultSNTPMinValidTime = time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	defaultSNTPMaxValidTime = time.Date(2100, time.January, 1, 0, 0, 0, 0, time.UTC)

	// ErrSNTPSyncerStarted is returned when SNTPSyncer.Start is called more than once.
	ErrSNTPSyncerStarted = errors.New("network: SNTP syncer already started")
	// ErrSNTPSyncerStopped is returned when SNTPSyncer.Start is called after Stop.
	ErrSNTPSyncerStopped = errors.New("network: SNTP syncer stopped")
	// ErrSNTPStepTooLarge is returned when a post-initial SNTP sample deviates
	// from the synchronized local UTC expectation by more than MaxStep.
	ErrSNTPStepTooLarge = errors.New("network: SNTP sample step exceeds MaxStep")
	// ErrSNTPImplausibleTime is returned when an SNTP sample falls outside the
	// configured [MinValidTime, MaxValidTime] plausibility window.
	ErrSNTPImplausibleTime = errors.New("network: SNTP sample outside plausible UTC range")
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
	MaxStep          time.Duration    // Max clock step allowed after initial sync (0 defaults to 5m unless DisableMaxStep)
	DisableMaxStep   bool             // Disables post-initial-sync MaxStep gating
	MinValidTime     time.Time        // Earliest accepted UTC sample (zero defaults to 2025-01-01 UTC)
	MaxValidTime     time.Time        // Latest accepted UTC sample (zero defaults to 2100-01-01 UTC)
	Query            SNTPQueryFunc    // Optional query override; nil uses QuerySNTP over port
	Now              func() time.Time // Optional monotonic reference for MaxStep drift checks
	OnSync           func(SNTPResult) // Optional callback dispatched sequentially after Sink.Sync
	OnError          func(error)      // Optional callback dispatched sequentially on non-paused failure
}

// SNTPSyncStatus summarizes SNTPSyncer synchronization state and counters.
type SNTPSyncStatus struct {
	Synced           bool
	LastSync         time.Time
	LastResult       SNTPResult
	SyncCount        uint64
	ErrorCount       uint64
	DroppedCallbacks uint64
	LastErr          error
}

// SNTPSyncer periodically queries RFC 4330 SNTPv4 servers and disciplines a
// monotonic-anchored wall clock via WallClockSink.Sync without modifying
// runtime monotonic time. Concurrent SyncOnce calls are serialized so delayed
// older responses cannot overwrite newer epochs, and callbacks run on a single
// bounded worker goroutine.
type SNTPSyncer struct {
	port             RawFramePort
	cfg              SNTPSyncConfig
	queryFn          SNTPQueryFunc
	nowFn            func() time.Time
	pollInterval     time.Duration
	retryInterval    time.Duration
	maxRetryInterval time.Duration
	maxStep          time.Duration
	minValidTime     time.Time
	maxValidTime     time.Time

	syncSem chan struct{}
	cbCh    chan func()
	cbDone  chan struct{}
	sinkWg  sync.WaitGroup
	cbWg    sync.WaitGroup
	sinkGID atomic.Uint64
	cbGID   atomic.Uint64

	mu               sync.Mutex
	started          bool
	stopped          bool
	cbStarted        bool
	stopCh           chan struct{}
	wg               sync.WaitGroup
	synced           bool
	lastSync         time.Time
	lastSyncMono     time.Time
	lastResult       SNTPResult
	syncCount        uint64
	errorCount       uint64
	droppedCallbacks uint64
	lastErr          error
}

// NewSNTPSyncer validates cfg and creates an idle SNTPSyncer over port (or cfg.Query).
func NewSNTPSyncer(port RawFramePort, cfg SNTPSyncConfig) (*SNTPSyncer, error) {
	if cfg.Query == nil && port == nil {
		return nil, fmt.Errorf("%w: nil frame port", ErrSNTPInvalidConfig)
	}
	if cfg.PollInterval < 0 || cfg.RetryInterval < 0 || cfg.MaxRetryInterval < 0 || cfg.MaxStep < 0 {
		return nil, fmt.Errorf("%w: negative duration in sync config", ErrSNTPInvalidConfig)
	}
	minValid := cfg.MinValidTime.UTC()
	if minValid.IsZero() {
		minValid = defaultSNTPMinValidTime
	}
	maxValid := cfg.MaxValidTime.UTC()
	if maxValid.IsZero() {
		maxValid = defaultSNTPMaxValidTime
	}
	if !minValid.Before(maxValid) {
		return nil, fmt.Errorf("%w: invalid plausibility time window", ErrSNTPInvalidConfig)
	}
	if cfg.Query == nil {
		parsed, err := parseSNTPConfig(cfg.SNTP)
		if err != nil {
			return nil, err
		}
		if !parsed.gateway.IsValid() {
			reachable := false
			for _, srv := range parsed.servers {
				if parsed.localPrefix.Contains(srv) {
					reachable = true
					break
				}
			}
			if !reachable {
				return nil, fmt.Errorf("%w: off-subnet SNTP server requires gateway", ErrSNTPInvalidConfig)
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
	maxStep := cfg.MaxStep
	if !cfg.DisableMaxStep && maxStep == 0 {
		maxStep = defaultSNTPMaxStep
	}
	if cfg.DisableMaxStep {
		maxStep = 0
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
	sem := make(chan struct{}, 1)
	sem <- struct{}{}
	return &SNTPSyncer{
		port:             port,
		cfg:              cfg,
		queryFn:          queryFn,
		nowFn:            nowFn,
		pollInterval:     poll,
		retryInterval:    retry,
		maxRetryInterval: maxRetry,
		maxStep:          maxStep,
		minValidTime:     minValid,
		maxValidTime:     maxValid,
		syncSem:          sem,
		cbCh:             make(chan func(), defaultSNTPCallbackQueue),
		cbDone:           make(chan struct{}),
		stopCh:           make(chan struct{}),
	}, nil
}

func currentGoroutineID() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	line := buf[:n]
	const prefix = "goroutine "
	if !bytes.HasPrefix(line, []byte(prefix)) {
		return 0
	}
	line = line[len(prefix):]
	end := bytes.IndexByte(line, ' ')
	if end <= 0 {
		return 0
	}
	id, err := strconv.ParseUint(string(line[:end]), 10, 64)
	if err != nil {
		return 0
	}
	return id
}

func (s *SNTPSyncer) isStopped() bool {
	s.mu.Lock()
	stopped := s.stopped
	s.mu.Unlock()
	return stopped
}

func (s *SNTPSyncer) enqueueCallbackLocked(fn func()) {
	if s.stopped || fn == nil {
		return
	}
	if !s.cbStarted {
		s.cbStarted = true
		s.cbWg.Add(1)
		go s.runCallbacks()
	}
	select {
	case s.cbCh <- fn:
	default:
		s.droppedCallbacks++
	}
}

func (s *SNTPSyncer) drainCallbacks() {
	for {
		select {
		case fn := <-s.cbCh:
			fn()
		default:
			return
		}
	}
}

func (s *SNTPSyncer) runCallbacks() {
	s.cbGID.Store(currentGoroutineID())
	defer func() {
		s.cbGID.Store(0)
		close(s.cbDone)
		s.cbWg.Done()
	}()
	for {
		select {
		case <-s.stopCh:
			s.drainCallbacks()
			return
		case fn := <-s.cbCh:
			fn()
		}
	}
}

// SyncOnce performs a single serialized SNTPv4 query, validates sample
// plausibility (and post-initial-sync MaxStep), updates Sink (if non-nil), and
// records Status metrics. Calls participate in Stop's lifecycle barrier;
// invoking Stop from OnSync or OnError never deadlocks.
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

	select {
	case <-s.syncSem:
		defer func() { s.syncSem <- struct{}{} }()
	case <-opCtx.Done():
		if s.isStopped() {
			return SNTPResult{}, ErrSNTPSyncerStopped
		}
		return SNTPResult{}, opCtx.Err()
	}

	if s.isStopped() {
		return SNTPResult{}, ErrSNTPSyncerStopped
	}

	res, err := s.queryFn(opCtx, s.cfg.SNTP)
	if err != nil {
		s.mu.Lock()
		if s.stopped {
			s.mu.Unlock()
			return SNTPResult{}, ErrSNTPSyncerStopped
		}
		if !errors.Is(err, ErrPaused) {
			s.errorCount++
			s.lastErr = err
			if s.cfg.OnError != nil {
				s.enqueueCallbackLocked(func() { s.cfg.OnError(err) })
			}
		} else {
			s.lastErr = err
		}
		s.mu.Unlock()
		return SNTPResult{}, err
	}

	nowMono := s.nowFn()
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return SNTPResult{}, ErrSNTPSyncerStopped
	}
	sampleUTC := res.Time.UTC()
	if res.Time.IsZero() || sampleUTC.Before(s.minValidTime) || sampleUTC.After(s.maxValidTime) {
		plausErr := fmt.Errorf("%w: %v outside [%s, %s]", ErrSNTPImplausibleTime, res.Time, s.minValidTime.Format(time.RFC3339), s.maxValidTime.Format(time.RFC3339))
		s.errorCount++
		s.lastErr = plausErr
		if s.cfg.OnError != nil {
			s.enqueueCallbackLocked(func() { s.cfg.OnError(plausErr) })
		}
		s.mu.Unlock()
		return SNTPResult{}, plausErr
	}
	if s.synced && s.maxStep > 0 {
		elapsed := nowMono.Sub(s.lastSyncMono)
		if elapsed < 0 {
			elapsed = 0
		}
		expected := s.lastSync.Add(elapsed)
		step := sampleUTC.Sub(expected)
		if step < 0 {
			step = -step
		}
		if step > s.maxStep {
			stepErr := fmt.Errorf("%w: offset=%v max=%v", ErrSNTPStepTooLarge, step, s.maxStep)
			s.errorCount++
			s.lastErr = stepErr
			if s.cfg.OnError != nil {
				s.enqueueCallbackLocked(func() { s.cfg.OnError(stepErr) })
			}
			s.mu.Unlock()
			return SNTPResult{}, stepErr
		}
	}
	res.Time = sampleUTC
	s.synced = true
	s.lastSync = sampleUTC
	s.lastSyncMono = nowMono
	s.lastResult = res
	s.syncCount++
	s.lastErr = nil
	sink := s.cfg.Sink
	if sink != nil {
		s.sinkWg.Add(1)
	}
	hasOnSync := s.cfg.OnSync != nil
	s.mu.Unlock()

	if sink != nil {
		// Executed synchronously on SyncOnce's goroutine outside s.mu with
		// syncSem + sinkWg held so external Stop() waits for active sinks while
		// re-entrant Stop() from Sink.Sync escapes without self-deadlocking.
		func() {
			s.sinkGID.Store(currentGoroutineID())
			defer func() {
				s.sinkGID.Store(0)
				s.sinkWg.Done()
			}()
			sink.Sync(sampleUTC)
		}()
	}
	if hasOnSync {
		s.mu.Lock()
		s.enqueueCallbackLocked(func() { s.cfg.OnSync(res) })
		s.mu.Unlock()
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
// queries, active WallClockSink.Sync calls, and queued callback worker work to
// finish before returning. When called synchronously from an OnSync/OnError
// callback, Stop waits for queries/sinks and drains all already-queued
// callbacks before returning; when called synchronously from WallClockSink.Sync,
// Stop signals shutdown without waiting on itself to avoid deadlock. Safe to
// call multiple times or without Start.
func (s *SNTPSyncer) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if !s.stopped {
		s.stopped = true
		close(s.stopCh)
	}
	s.mu.Unlock()

	gid := currentGoroutineID()
	if gid != 0 && gid == s.sinkGID.Load() {
		return
	}
	s.wg.Wait()
	s.sinkWg.Wait()
	if gid == 0 || gid != s.cbGID.Load() {
		s.cbWg.Wait()
		return
	}
	s.drainCallbacks()
}

// Status returns a snapshot of the syncer's synchronization state and counters.
func (s *SNTPSyncer) Status() SNTPSyncStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return SNTPSyncStatus{
		Synced:           s.synced,
		LastSync:         s.lastSync,
		LastResult:       s.lastResult,
		SyncCount:        s.syncCount,
		ErrorCount:       s.errorCount,
		DroppedCallbacks: s.droppedCallbacks,
		LastErr:          s.lastErr,
	}
}
