package rooms

import (
	"fmt"
	"math"
	"runtime/metrics"
	"strings"
	"time"
)

// threadUsage is one OS thread's CPU time, context switches, page faults and
// block reads: what one clock_gettime(CLOCK_THREAD_CPUTIME_ID) and one
// getrusage(RUSAGE_THREAD) return. It is the only sample taken inside a
// callback's elapsed window, two per callback, so the cost a callback pays
// against its budget is those four syscalls. A difference of two samples
// (since) covers the script run alone.
type threadUsage struct {
	cpu         time.Duration
	voluntary   int64
	involuntary int64
	// majorFaults, minorFaults and blocksRead are the thread's ru_majflt,
	// ru_minflt and ru_inblock. A major fault or block read waited for
	// storage. blocksRead is in 512-byte units, whatever the device's block
	// size; String prints it as KiB.
	majorFaults int64
	minorFaults int64
	blocksRead  int64
}

// sampleThread reads the calling OS thread's usage inside the elapsed window.
func (r *Instance) sampleThread() (threadUsage, bool) {
	if r.threadSample != nil {
		return r.threadSample()
	}
	return readThreadUsage()
}

func (u threadUsage) since(before threadUsage) threadUsage {
	return threadUsage{
		cpu:         u.cpu - before.cpu,
		voluntary:   u.voluntary - before.voluntary,
		involuntary: u.involuntary - before.involuntary,
		majorFaults: u.majorFaults - before.majorFaults,
		minorFaults: u.minorFaults - before.minorFaults,
		blocksRead:  u.blocksRead - before.blocksRead,
	}
}

// String always gives CPU time and switches. Faults and block reads appear
// only when non-zero, so a run that did neither keeps the short form.
func (u threadUsage) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "thread cpu %s, %d voluntary/%d involuntary switches",
		u.cpu.Round(10*time.Microsecond), u.voluntary, u.involuntary)
	if u.majorFaults != 0 || u.minorFaults != 0 {
		fmt.Fprintf(&b, ", %d major/%d minor faults", u.majorFaults, u.minorFaults)
	}
	if u.blocksRead != 0 {
		// ru_inblock counts 512-byte units; a partial KiB rounds up.
		fmt.Fprintf(&b, ", %d KiB read", (u.blocksRead+1)/2)
	}
	return b.String()
}

// usagePoint is one read of the counters too costly to take inside a
// callback's elapsed window: the thread's run-queue wait from schedstat and
// the Go runtime's GC and scheduler metrics, with the wall clock, thread id
// and thread CPU time they are compared against. A baseline point is taken
// once per Step (or before a callback outside a Step), and an after point
// only when a callback's timing will be reported, once its elapsed time is
// known. Neither counts against the callback's budget.
type usagePoint struct {
	wall time.Time
	// tid and cpu are the reading thread and its CPU time. A window describes
	// one thread only when both ends read the same one.
	tid     int
	cpu     time.Duration
	haveCPU bool
	// runDelay is the thread's time runnable on a run queue but not running
	// (/proc/thread-self/schedstat field 2). A wakeup that waits for a core
	// raises it without an involuntary switch. haveRunDelay is false where
	// the kernel does not provide it.
	runDelay     time.Duration
	haveRunDelay bool
	// gcCycles and gcPause are process-wide: completed Go GC cycles and GC
	// stop-the-world wall time (the runtime's GC pause CPU-seconds, which it
	// counts as each pause times GOMAXPROCS, divided by GOMAXPROCS).
	gcCycles uint64
	gcPause  time.Duration
	// goSchedWait is set on an after point only: the longest time a Go
	// goroutine, any in the process, was runnable before it ran, among waits
	// that ended since the baseline. It is the lower bound of the highest
	// /sched/latencies:seconds bucket that grew. The runtime records about one
	// in eight goroutine wakeups, so a long wait of the script goroutine
	// itself (a locked-thread handoff) can be missed.
	goSchedWait time.Duration
}

// usageWindow holds the baseline that an overrunning callback's after point
// is compared with, and the readers for both. One Instance's callbacks run
// one at a time, so it is not shared.
type usageWindow struct {
	schedstat schedstatReader
	runtime   runtimeUsage
	base      usagePoint
	haveBase  bool
	// inStep is set while Step runs, so its callbacks share the baseline
	// Step took instead of each reading its own.
	inStep bool
	// baseLabel names when the baseline was taken, for the message.
	baseLabel string
}

const (
	baseStepStart     = "step start"
	baseCallbackStart = "callback start"
)

// readWindow reads a usage point. A baseline reads the Go runtime first and
// the thread's own counters and wall clock last, an after point the
// reverse, so the window between them is as close as the reads allow to the
// callbacks it brackets.
func (r *Instance) readWindow(base bool) usagePoint {
	if r.windowSample != nil {
		return r.windowSample(base)
	}
	var p usagePoint
	if base {
		r.usage.runtime.read(&p, true)
	}
	p.runDelay, p.haveRunDelay = r.usage.schedstat.runDelay()
	p.tid, p.cpu, p.haveCPU = currentThread()
	p.wall = time.Now()
	if !base {
		r.usage.runtime.read(&p, false)
	}
	return p
}

// beginStep takes the one baseline a Step's callbacks share. endStep ends
// the sharing, so a callback outside a Step takes its own baseline.
func (r *Instance) beginStep() {
	r.usage.base = r.readWindow(true)
	r.usage.haveBase, r.usage.inStep, r.usage.baseLabel = true, true, baseStepStart
}

func (r *Instance) endStep() {
	r.usage.inStep, r.usage.haveBase = false, false
}

// noteCallbackBaseline gives a callback outside a Step its own baseline,
// taken before its elapsed window starts. Inside a Step it does nothing.
func (r *Instance) noteCallbackBaseline() {
	if r.usage.inStep {
		return
	}
	r.usage.base = r.readWindow(true)
	r.usage.haveBase, r.usage.baseLabel = true, baseCallbackStart
}

// noteOverrunWindow reads the after point for a callback whose timing will
// be reported, after its elapsed time is taken, and attaches the window
// since the baseline to timing.
func (r *Instance) noteOverrunWindow(timing *callTiming) {
	if !r.usage.haveBase {
		return
	}
	after := r.readWindow(false)
	timing.window, timing.haveWindow = after.since(r.usage.base, r.usage.baseLabel), true
}

// sinceBaseline is an after point less its baseline. It covers from the
// baseline (step start, or callback start outside a Step) to the end of the
// overrunning callback, which is wider than the script run.
type sinceBaseline struct {
	label string
	span  time.Duration
	// sameThread says both ends read the same OS thread, so cpu and runDelay
	// describe one thread over span.
	sameThread   bool
	cpu          time.Duration
	runDelay     time.Duration
	haveRunDelay bool
	gcCycles     uint64
	gcPause      time.Duration
	goSchedWait  time.Duration
}

func (u usagePoint) since(base usagePoint, label string) sinceBaseline {
	same := u.haveCPU && base.haveCPU && u.tid == base.tid
	return sinceBaseline{
		label:        label,
		span:         u.wall.Sub(base.wall),
		sameThread:   same,
		cpu:          u.cpu - base.cpu,
		runDelay:     u.runDelay - base.runDelay,
		haveRunDelay: same && u.haveRunDelay && base.haveRunDelay,
		gcCycles:     u.gcCycles - base.gcCycles,
		gcPause:      u.gcPause - base.gcPause,
		goSchedWait:  u.goSchedWait,
	}
}

// String labels the window with its baseline. Thread CPU, and runnable wait
// where the kernel reports it, appear when both ends read one thread;
// "thread changed" when they did not, and nothing where the OS gives no
// thread CPU. GC appears only when non-zero and a Go scheduler wait from
// goSchedWaitShown up. Unaccounted ends it where it can be computed.
func (w sinceBaseline) String() string {
	var parts []string
	if w.sameThread {
		parts = append(parts, "thread cpu "+w.cpu.Round(10*time.Microsecond).String())
		if w.haveRunDelay {
			parts = append(parts, "runnable "+w.runDelay.Round(10*time.Microsecond).String())
		}
	} else if w.haveCPUSource() {
		parts = append(parts, "thread changed")
	}
	if w.gcCycles != 0 || w.gcPause != 0 {
		parts = append(parts, fmt.Sprintf("%d GC cycles, GC stop-the-world %s", w.gcCycles, w.gcPause.Round(10*time.Microsecond)))
	}
	if w.goSchedWait >= goSchedWaitShown {
		parts = append(parts, "Go sched wait >="+w.goSchedWait.Round(10*time.Microsecond).String())
	}
	if left, ok := w.unaccounted(); ok {
		parts = append(parts, "unaccounted "+left.Round(10*time.Microsecond).String())
	}
	s := "since " + w.label + " " + w.span.Round(10*time.Microsecond).String()
	if len(parts) == 0 {
		return s
	}
	return s + ": " + strings.Join(parts, ", ")
}

// haveCPUSource reports whether this platform reads thread CPU at all, so a
// window without it is a changed thread rather than an unsupported OS.
func (sinceBaseline) haveCPUSource() bool { return threadCPUSupported }

// goSchedWaitShown is the shortest Go scheduler wait the text names. Shorter
// waits are routine and would lengthen every message.
const goSchedWaitShown = time.Millisecond

// unaccounted is the window's wall time not explained by the same thread's
// CPU time and run-queue wait or by GC stop-the-world over that same window,
// floored at zero: time the thread slept for another reason, such as
// storage, a lock, a Go mark assist or a locked-thread handoff. It is known
// only where one thread was read at both ends and run-queue wait is.
func (w sinceBaseline) unaccounted() (time.Duration, bool) {
	if !w.haveRunDelay {
		return 0, false
	}
	return max(w.span-w.cpu-w.runDelay-w.gcPause, 0), true
}

const (
	gcCyclesMetric   = "/gc/cycles/total:gc-cycles"
	gcPauseCPUMetric = "/cpu/classes/gc/pause:cpu-seconds"
	gomaxprocsMetric = "/sched/gomaxprocs:threads"
	goSchedMetric    = "/sched/latencies:seconds"
)

// runtimeUsage reads the Go runtime counters into sample slices it keeps, so
// a read allocates nothing after the first. A baseline and an after point
// read separate slices, so the after point compares its scheduler histogram
// with the baseline's own instead of a copy. metrics.Read does not stop the
// world.
//
// GC stop-the-world time comes from the scalar GC pause CPU-seconds rather
// than the /sched/pauses/total/gc histogram: the scalar is exact where a
// histogram sum is a bucket estimate, and it saves reading a second
// 160-bucket histogram. The scheduler latency histogram stays because it is
// the only source for goroutines waiting on the Go scheduler, which faults
// (storage) and run-queue wait (CPU) do not show.
type runtimeUsage struct {
	base, after []metrics.Sample
}

func newRuntimeSamples() []metrics.Sample {
	return []metrics.Sample{{Name: gcCyclesMetric}, {Name: gcPauseCPUMetric}, {Name: gomaxprocsMetric}, {Name: goSchedMetric}}
}

func (g *runtimeUsage) read(p *usagePoint, base bool) {
	if g.base == nil {
		g.base, g.after = newRuntimeSamples(), newRuntimeSamples()
	}
	s := g.after
	if base {
		s = g.base
	}
	metrics.Read(s)
	if v := s[0].Value; v.Kind() == metrics.KindUint64 {
		p.gcCycles = v.Uint64()
	}
	if v, procs := s[1].Value, s[2].Value; v.Kind() == metrics.KindFloat64 && procs.Kind() == metrics.KindUint64 && procs.Uint64() > 0 {
		p.gcPause = time.Duration(v.Float64() / float64(procs.Uint64()) * float64(time.Second))
	}
	if base {
		return
	}
	b, a := g.base[3].Value, s[3].Value
	if b.Kind() == metrics.KindFloat64Histogram && a.Kind() == metrics.KindFloat64Histogram {
		p.goSchedWait = longestNewWait(a.Float64Histogram(), b.Float64Histogram().Counts)
	}
}

// longestNewWait is the lower bound of the highest bucket of h whose count
// grew since before, or 0 when none grew or the layout changed.
func longestNewWait(h *metrics.Float64Histogram, before []uint64) time.Duration {
	if len(before) != len(h.Counts) {
		return 0
	}
	for i := len(h.Counts) - 1; i >= 0; i-- {
		if h.Counts[i] > before[i] {
			lo := h.Buckets[i]
			if math.IsInf(lo, -1) || lo <= 0 {
				return 0
			}
			return time.Duration(lo * float64(time.Second))
		}
	}
	return 0
}
