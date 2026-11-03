package fogcast

// This opt-in diagnostic records fixed stage names and monotonic offsets only.
// It is not an audit log: events can be dropped, and it never changes a request.
import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"
)

const timingQueueSize = 4096
const timingMaxRecords = 32768

type timingStage uint8

const (
	timingHealth timingStage = iota + 1
	timingSession
	timingLifecycle
	timingTargetLock
	timingProbe
	timingProbeHealth
	timingDiscovery
	timingOwnership
	timingReconcileStatus
	timingFinalStatus
)

var timingNames = [...]string{"", "health", "session", "lifecycle", "target_lock", "probe", "probe_health", "discovery", "ownership", "reconcile_status", "final_status"}

type timingEvent struct {
	id      uint64
	stage   timingStage
	start   bool
	ns      int64
	dropped uint64
}
type observationTiming struct {
	epoch   time.Time
	next    atomic.Uint64
	dropped atomic.Uint64
	events  chan timingEvent
	stop    chan struct{}
	done    chan struct{}
	active  atomic.Int64
}
type timingContextKey struct{}
type timingContext struct {
	recorder *observationTiming
	id       uint64
}

// EnableObservationTiming creates one private diagnostic file. It does not
// open an existing file and is intended only for an isolated diagnostic host.
// Once enabled, recording cannot be disabled during this service's lifetime.
func (s *Service) EnableObservationTiming(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("timing output path must be clean and absolute")
	}
	s.timingEnableMu.Lock()
	if s.timingClosed || s.timing.Load() != nil {
		s.timingEnableMu.Unlock()
		return fmt.Errorf("timing unavailable")
	}
	s.timingEnableMu.Unlock()
	open := openObservationOutput
	if s.timingOpen != nil {
		open = s.timingOpen
	}
	f, err := open(path)
	if err != nil {
		return err
	}
	s.timingEnableMu.Lock()
	if s.timingClosed || s.timing.Load() != nil {
		s.timingEnableMu.Unlock()
		// Do not remove a pathname whose identity may have changed.
		_ = f.Close()
		return fmt.Errorf("timing unavailable")
	}
	t := &observationTiming{epoch: time.Now(), events: make(chan timingEvent, timingQueueSize), stop: make(chan struct{}), done: make(chan struct{})}
	s.timing.Store(t)
	go t.write(f)
	s.timingEnableMu.Unlock()
	return nil
}

func openObservationOutput(path string) (*os.File, error) {
	dir := filepath.Dir(path)
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil || resolved != dir {
		return nil, fmt.Errorf("timing directory must have no symlinks")
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode().Perm() != 0700 || stat.Uid != uint32(os.Geteuid()) {
		return nil, fmt.Errorf("timing directory must be private and owned by this user")
	}
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
}

// write owns the file. The queue and total output are capped; producers never
// wait for disk. Abrupt termination, full queue, and I/O errors may lose data.
func (t *observationTiming) write(f *os.File) {
	defer close(t.done)
	defer f.Close()
	truncated := false
	writeFailed := false
	defer func() {
		_, _ = fmt.Fprintf(f, "{\"summary\":true,\"dropped\":%d,\"truncated\":%t,\"active\":%d,\"write_error\":%t}\n", t.dropped.Load(), truncated, t.active.Load(), writeFailed)
	}()
	write := func(e timingEvent) bool {
		phase := "end"
		if e.start {
			phase = "start"
		}
		_, err := fmt.Fprintf(f, "{\"id\":%d,\"stage\":%q,\"phase\":%q,\"monotonic_ns\":%d,\"dropped\":%d}\n", e.id, timingNames[e.stage], phase, e.ns, e.dropped)
		return err == nil
	}
	for n := 0; n < timingMaxRecords; n++ {
		select {
		case e := <-t.events:
			if !write(e) {
				writeFailed = true
				return
			}
		case <-t.stop:
			for {
				select {
				case e := <-t.events:
					if !write(e) {
						writeFailed = true
						return
					}
					n++
					if n >= timingMaxRecords {
						truncated = true
						return
					}
				default:
					return
				}
			}
		}
	}
	truncated = true
}
func (t *observationTiming) emit(id uint64, stage timingStage, start bool) {
	event := timingEvent{id: id, stage: stage, start: start, ns: time.Since(t.epoch).Nanoseconds(), dropped: t.dropped.Load()}
	select {
	case t.events <- event:
	default:
		t.dropped.Add(1)
	}
}
func (s *Service) beginObservation(ctx context.Context, stage timingStage) (context.Context, func()) {
	t := s.timing.Load()
	if t == nil {
		return ctx, func() {}
	}
	s.timingEnableMu.Lock()
	if s.timingClosed {
		s.timingEnableMu.Unlock()
		return ctx, func() {}
	}
	t.active.Add(1)
	s.timingEnableMu.Unlock()
	id := t.next.Add(1)
	trace := timingContext{t, id}
	t.emit(id, stage, true)
	return context.WithValue(ctx, timingContextKey{}, trace), func() { t.emit(id, stage, false); t.active.Add(-1) }
}

// A missing summary or a nonzero active count, dropped count, or truncation
// means incomplete evidence. Never let a blocked diagnostic delay shutdown
// indefinitely or affect target cleanup.
func (s *Service) stopObservationTiming() {
	s.timingEnableMu.Lock()
	s.timingClosed = true
	t := s.timing.Load()
	s.timingEnableMu.Unlock()
	if t == nil {
		return
	}
	deadline := time.NewTimer(200 * time.Millisecond)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for t.active.Load() != 0 {
		select {
		case <-ticker.C:
		case <-deadline.C:
			close(t.stop)
			return
		}
	}
	close(t.stop)
	select {
	case <-t.done:
	case <-deadline.C:
	}
}
func observationStage(ctx context.Context, stage timingStage) func() {
	trace, ok := ctx.Value(timingContextKey{}).(timingContext)
	if !ok {
		return func() {}
	}
	trace.recorder.emit(trace.id, stage, true)
	return func() { trace.recorder.emit(trace.id, stage, false) }
}
