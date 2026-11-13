package rooms

import (
	"fmt"
	"math"
	"runtime/metrics"
	"strings"
	"time"
)

// threadUsage is one OS thread's CPU time, context switches, page faults and
// block reads, plus the process-wide Go GC counters read with them. A
// difference of two samples (since) says why a script run took longer than
// its thread CPU: blocked on I/O (major faults, blocks read), runnable but
// waiting for a core (runDelay), or parked across a Go GC (gcCycles,
// gcPause).
type threadUsage struct {
	cpu         time.Duration
	voluntary   int64
	involuntary int64
	// majorFaults, minorFaults and blocksRead are the thread's ru_majflt,
	// ru_minflt and ru_inblock. A major fault or block read waited for
	// storage.
	majorFaults int64
	minorFaults int64
	blocksRead  int64
	// runDelay is the thread's time runnable on a run queue but not running
	// (/proc/thread-self/schedstat field 2). A wakeup that waits for a core
	// raises it without an involuntary switch. haveRunDelay is false where
	// the kernel does not provide it.
	runDelay     time.Duration
	haveRunDelay bool
	// gcCycles and gcPause are process-wide: completed Go GC cycles and the
	// summed stop-the-world pause time of GC, estimated from the runtime's
	// pause histogram (each pause counted at its bucket's midpoint).
	gcCycles uint64
	gcPause  time.Duration
}

// sampleThread reads the calling OS thread's usage. beforeScript says which
// side of the script run the sample is on (see readUsage).
func (r *Instance) sampleThread(beforeScript bool) (threadUsage, bool) {
	if r.threadSample != nil {
		return r.threadSample()
	}
	return r.readUsage(beforeScript)
}

// readUsage is the OS thread read plus the run-queue wait and the Go runtime
// counters. Thread CPU time and rusage are read innermost: after the other
// reads before the script, and before them after it, so the script run's CPU,
// switches and faults hold the script alone and not the sampling.
func (r *Instance) readUsage(beforeScript bool) (threadUsage, bool) {
	var u threadUsage
	if beforeScript {
		r.readOuter(&u)
	}
	if !readThreadUsage(&u) {
		return threadUsage{}, false
	}
	if !beforeScript {
		r.readOuter(&u)
	}
	return u, true
}

// readOuter fills the fields read around the thread's own counters.
func (r *Instance) readOuter(u *threadUsage) {
	u.runDelay, u.haveRunDelay = r.schedstat.runDelay()
	r.runtimeUsage.read(u)
}

const (
	gcCyclesMetric = "/gc/cycles/total:gc-cycles"
	gcPauseMetric  = "/sched/pauses/total/gc:seconds"
)

// runtimeUsage reads the Go GC counters into a sample slice it keeps, so a
// read allocates nothing after the first. metrics.Read does not stop the
// world. One Instance's callbacks run one at a time, so its reader is not
// shared.
type runtimeUsage struct {
	samples []metrics.Sample
}

func (g *runtimeUsage) read(u *threadUsage) {
	if g.samples == nil {
		g.samples = []metrics.Sample{{Name: gcCyclesMetric}, {Name: gcPauseMetric}}
	}
	metrics.Read(g.samples)
	if v := g.samples[0].Value; v.Kind() == metrics.KindUint64 {
		u.gcCycles = v.Uint64()
	}
	if v := g.samples[1].Value; v.Kind() == metrics.KindFloat64Histogram {
		u.gcPause = histogramSum(v.Float64Histogram())
	}
}

// histogramSum estimates the total of a seconds histogram, counting each
// sample at its bucket's midpoint, or at its finite bound for an open-ended
// bucket. The same estimate on both samples makes their difference the
// pauses that ended in between.
func histogramSum(h *metrics.Float64Histogram) time.Duration {
	var sum float64
	for i, n := range h.Counts {
		if n == 0 {
			continue
		}
		lo, hi := h.Buckets[i], h.Buckets[i+1]
		var mid float64
		switch {
		case math.IsInf(lo, -1):
			mid = max(hi, 0)
		case math.IsInf(hi, 1):
			mid = lo
		default:
			mid = (lo + hi) / 2
		}
		sum += float64(n) * mid
	}
	return time.Duration(sum * float64(time.Second))
}

func (u threadUsage) since(before threadUsage) threadUsage {
	return threadUsage{
		cpu:          u.cpu - before.cpu,
		voluntary:    u.voluntary - before.voluntary,
		involuntary:  u.involuntary - before.involuntary,
		majorFaults:  u.majorFaults - before.majorFaults,
		minorFaults:  u.minorFaults - before.minorFaults,
		blocksRead:   u.blocksRead - before.blocksRead,
		runDelay:     u.runDelay - before.runDelay,
		haveRunDelay: u.haveRunDelay && before.haveRunDelay,
		gcCycles:     u.gcCycles - before.gcCycles,
		gcPause:      u.gcPause - before.gcPause,
	}
}

// String always gives CPU time and switches. Runnable wait appears where the
// kernel reports it. Faults, block reads and GC appear only when non-zero, so
// a run that did none of them keeps the short form.
func (u threadUsage) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "thread cpu %s, %d voluntary/%d involuntary switches",
		u.cpu.Round(10*time.Microsecond), u.voluntary, u.involuntary)
	if u.haveRunDelay {
		fmt.Fprintf(&b, ", runnable %s", u.runDelay.Round(10*time.Microsecond))
	}
	if u.majorFaults != 0 || u.minorFaults != 0 {
		fmt.Fprintf(&b, ", %d major/%d minor faults", u.majorFaults, u.minorFaults)
	}
	if u.blocksRead != 0 {
		fmt.Fprintf(&b, ", %d blocks read", u.blocksRead)
	}
	if u.gcCycles != 0 || u.gcPause != 0 {
		fmt.Fprintf(&b, ", %d GC cycles, GC stop-the-world %s", u.gcCycles, u.gcPause.Round(10*time.Microsecond))
	}
	return b.String()
}
