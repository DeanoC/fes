package rooms

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// A hard deadline miss says what else wanted a CPU while the callback ran.
// Kit reports (#1770) show callbacks that used 13-17ms of thread CPU inside
// 50-85ms of wall time, so the next report must name the competing work.
//
// Reading every process's CPU time around each callback would cost the
// launcher thread far more than the callbacks it measures. Instead Step asks
// the shared sampler for a baseline at most once per contentionInterval, and a
// background goroutine reads it at normal priority: a niced thread can hold a
// Go P while starved, which would stall stop-the-world GC and the callbacks
// waiting on it. Only a hard miss reads the system again, on the launcher
// thread with its own reader so it never waits on the background read, and
// diffs that against a baseline finished before the callback started. The
// miss is already fatal, so that read cannot change the outcome. Platforms
// without a /proc reader report nothing.

const (
	contentionInterval = time.Second
	// contentionMinWindow is the shortest window a report prefers. Clock
	// ticks are 10ms, so a baseline taken just before the callback would
	// show next to nothing.
	contentionMinWindow = contentionInterval / 2
	// contentionPanelTop and contentionLogTop are how many processes the
	// room error and the log line name.
	contentionPanelTop = 3
	contentionLogTop   = 8
)

// procTicks is one process's cumulative user+system CPU and its start time
// since boot, both in clock ticks. A process is its pid and start together:
// comm is only a label, since a kernel worker renames itself for each
// workqueue it runs, and a pid can be reused by a later process.
type procTicks struct {
	comm  string
	ticks uint64
	start uint64
}

// contentionSnapshot is the system's load and per-process CPU, read between
// at and done.
type contentionSnapshot struct {
	// at is when the read started and done when it finished; done is zero
	// for a snapshot that was not timed (tests), meaning at.
	at, done time.Time
	loadavg  string // /proc/loadavg: 1, 5 and 15 minute averages, runnable/total
	// cpuTotal and cpuIdle are all CPUs' ticks from /proc/stat; idle
	// includes iowait. haveCPU is false when /proc/stat was unreadable.
	cpuTotal, cpuIdle uint64
	haveCPU           bool
	cpus              int
	// uptime is /proc/uptime, read when the snapshot started, so a process
	// that started after it is new in a later window. haveUptime is false
	// when it was unreadable.
	uptime     time.Duration
	haveUptime bool
	procs      map[int]procTicks
}

// end is when the read finished.
func (s contentionSnapshot) end() time.Time {
	if s.done.IsZero() {
		return s.at
	}
	return s.done
}

// mid is the read's midpoint, the best estimate of when its counters were
// sampled. Windows run between midpoints.
func (s contentionSnapshot) mid() time.Time {
	return s.at.Add(s.end().Sub(s.at) / 2)
}

// contentionSampler keeps the two newest baselines, at least
// contentionInterval apart. A miss diffs against the newest one finished before
// the callback started and at least contentionMinWindow before the report, so
// the window covers the callback and is long enough to measure.
type contentionSampler struct {
	// read takes a snapshot; nil means this platform has none.
	read func() (contentionSnapshot, bool)
	// clockTick is the length of one /proc clock tick (USER_HZ).
	clockTick time.Duration

	mu         sync.Mutex
	prev, last contentionSnapshot
	requested  time.Time
	refreshing bool
}

// hostContention is the launcher process's sampler, shared by every room.
var hostContention = newContentionSampler(readHostContention)

func newContentionSampler(read func() (contentionSnapshot, bool)) *contentionSampler {
	return &contentionSampler{read: read, clockTick: 10 * time.Millisecond}
}

// tick starts a background baseline read when the last one is older than
// contentionInterval. It costs the caller one lock and a time comparison.
func (s *contentionSampler) tick(now time.Time) {
	if s == nil || s.read == nil {
		return
	}
	s.mu.Lock()
	if s.refreshing || now.Sub(s.requested) < contentionInterval {
		s.mu.Unlock()
		return
	}
	s.refreshing, s.requested = true, now
	s.mu.Unlock()
	go s.refresh()
}

// refresh runs on its own goroutine at normal priority.
func (s *contentionSampler) refresh() {
	snap, ok := s.read()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshing = false
	if ok {
		s.prev, s.last = s.last, snap
	}
}

// baseline is the newest snapshot finished no later than start whose
// midpoint is at least contentionMinWindow before now's, or failing that the
// newest finished no later than start.
func (s *contentionSampler) baseline(start time.Time, now contentionSnapshot) (contentionSnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var found contentionSnapshot
	for _, snap := range []contentionSnapshot{s.last, s.prev} {
		if snap.at.IsZero() || snap.end().After(start) {
			continue
		}
		if now.mid().Sub(snap.mid()) >= contentionMinWindow {
			return snap, true
		}
		if found.at.IsZero() {
			found = snap
		}
	}
	return found, !found.at.IsZero()
}

// report reads the system now and summarises it against the baseline taken
// before start: short for the room error, detail for the log. ok is false
// when this platform has no reader.
func (s *contentionSampler) report(start time.Time) (short, detail string, ok bool) {
	if s == nil || s.read == nil {
		return "", "", false
	}
	now, ok := s.read()
	if !ok {
		return "", "", false
	}
	base, haveBase := s.baseline(start, now)
	if !haveBase {
		return contentionText(now, nil, s.clockTick, contentionPanelTop),
			contentionText(now, nil, s.clockTick, contentionLogTop), true
	}
	return contentionText(now, &base, s.clockTick, contentionPanelTop),
		contentionText(now, &base, s.clockTick, contentionLogTop), true
}

// procUsage is one process's CPU over a contention window.
type procUsage struct {
	pid  int
	comm string
	cpu  time.Duration
}

// contentionText formats load, system CPU and the top processes by CPU over
// the window since base. Process CPU is a percentage of one CPU, like top.
func contentionText(now contentionSnapshot, base *contentionSnapshot, tick time.Duration, top int) string {
	var b strings.Builder
	b.WriteString("load " + now.loadavg)
	if base == nil {
		b.WriteString("; no cpu baseline")
		return b.String()
	}
	window := now.mid().Sub(base.mid())
	if window <= 0 {
		b.WriteString("; no cpu baseline")
		return b.String()
	}
	fmt.Fprintf(&b, "; over %s", window.Round(time.Millisecond))
	if now.haveCPU && base.haveCPU && now.cpuTotal > base.cpuTotal {
		total := now.cpuTotal - base.cpuTotal
		idle := min(now.cpuIdle-base.cpuIdle, total)
		fmt.Fprintf(&b, " cpu %d%% busy", (total-idle)*100/total)
		if now.cpus > 0 {
			fmt.Fprintf(&b, " of %d", now.cpus)
		}
	}
	used := topProcs(now, *base, tick)
	if len(used) == 0 {
		return b.String()
	}
	b.WriteString("; top")
	for i, p := range used[:min(top, len(used))] {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, " %s %d%%", p.comm, int64(p.cpu*100/window))
		if top > contentionPanelTop {
			fmt.Fprintf(&b, " (pid %d, %s)", p.pid, p.cpu)
		}
	}
	return b.String()
}

// topProcs lists processes that used CPU since base, busiest first. A
// process in base, the same pid with the same start time, counts the CPU it
// used since. A process not in base counts all its CPU only if it started
// after base was read; otherwise base missed it (a scan race) and its
// lifetime CPU would be charged to the window, so it is skipped.
func topProcs(now, base contentionSnapshot, tick time.Duration) []procUsage {
	var used []procUsage
	for pid, p := range now.procs {
		ticks := p.ticks
		if old, ok := base.procs[pid]; ok && old.start == p.start {
			ticks -= min(old.ticks, ticks)
		} else if !base.haveUptime || time.Duration(p.start)*tick < base.uptime {
			continue
		}
		if ticks == 0 {
			continue
		}
		used = append(used, procUsage{pid: pid, comm: p.comm, cpu: time.Duration(ticks) * tick})
	}
	slices.SortFunc(used, func(a, b procUsage) int {
		if c := cmp.Compare(b.cpu, a.cpu); c != 0 {
			return c
		}
		return cmp.Compare(a.pid, b.pid)
	})
	return used
}

// deadlineMiss appends what else was running to a hard deadline error. The
// room error carries a short summary for the failure panel; the log gets the
// longer list. err stays wrapped.
func (r *Instance) deadlineMiss(err error, timing callTiming) error {
	short, detail, ok := r.contention.report(time.Now().Add(-timing.elapsed))
	if !ok {
		return err
	}
	fmt.Fprintf(r.opts.Stderr, "rooms: room %s deadline miss contention: %s\n", r.pack.ID, detail)
	return fmt.Errorf("%w; %s", err, short)
}
