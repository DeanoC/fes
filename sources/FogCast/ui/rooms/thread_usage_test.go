package rooms

import (
	"errors"
	"math"
	"runtime/metrics"
	"strings"
	"testing"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// since subtracts every in-window counter, and the text names faults and
// reads only when they occurred.
func TestThreadUsageSinceAndString(t *testing.T) {
	before := threadUsage{cpu: time.Millisecond, voluntary: 2, involuntary: 1, majorFaults: 1, minorFaults: 10, blocksRead: 8}
	after := threadUsage{cpu: 4110 * time.Microsecond, voluntary: 17, involuntary: 3, majorFaults: 13, minorFaults: 350, blocksRead: 104}
	d := after.since(before)
	want := threadUsage{cpu: 3110 * time.Microsecond, voluntary: 15, involuntary: 2, majorFaults: 12, minorFaults: 340, blocksRead: 96}
	if d != want {
		t.Fatalf("since = %+v, want %+v", d, want)
	}
	if got, w := d.String(), "thread cpu 3.11ms, 15 voluntary/2 involuntary switches, 12 major/340 minor faults, 48 KiB read"; got != w {
		t.Fatalf("String = %q, want %q", got, w)
	}
	quiet := threadUsage{cpu: 2 * time.Millisecond, voluntary: 1, involuntary: 9}
	if got, w := quiet.String(), "thread cpu 2ms, 1 voluntary/9 involuntary switches"; got != w {
		t.Fatalf("quiet String = %q, want %q", got, w)
	}
}

// The window since the baseline is labelled with its baseline and span, and
// its unaccounted rest uses only quantities over that same span.
func TestSinceBaselineString(t *testing.T) {
	t0 := time.Unix(100, 0)
	base := usagePoint{wall: t0, tid: 7, cpu: time.Millisecond, haveCPU: true, runDelay: time.Millisecond, haveRunDelay: true,
		gcCycles: 40, gcPause: 3 * time.Millisecond}
	after := usagePoint{wall: t0.Add(20 * time.Millisecond), tid: 7, cpu: 5 * time.Millisecond, haveCPU: true,
		runDelay: 3500 * time.Microsecond, haveRunDelay: true, gcCycles: 41, gcPause: 3500 * time.Microsecond, goSchedWait: 2 * time.Millisecond}
	w := after.since(base, baseStepStart)
	// 20ms - 4ms cpu - 2.5ms runnable. GC stop-the-world is context only:
	// it can overlap the thread's own CPU or run-queue time.
	if got, want := w.String(), "since step start 20ms: thread cpu 4ms, runnable 2.5ms, 1 GC cycles, GC stop-the-world 500µs, Go sched wait >=2ms, unaccounted 13.5ms"; got != want {
		t.Fatalf("String = %q, want %q", got, want)
	}
	// Over-explained windows floor at zero.
	over := after
	over.runDelay = 30 * time.Millisecond
	if got := over.since(base, baseStepStart).String(); !strings.HasSuffix(got, ", unaccounted 0s") {
		t.Fatalf("over-explained window: %q", got)
	}
	// Another thread at the after point: no thread CPU, runnable or
	// unaccounted, only the process-wide fields.
	moved := after
	moved.tid = 8
	got := moved.since(base, baseCallbackStart).String()
	wantMoved := "since callback start 20ms: 1 GC cycles, GC stop-the-world 500µs, Go sched wait >=2ms"
	if threadCPUSupported {
		wantMoved = "since callback start 20ms: thread changed, 1 GC cycles, GC stop-the-world 500µs, Go sched wait >=2ms"
	}
	if got != wantMoved {
		t.Fatalf("moved thread = %q, want %q", got, wantMoved)
	}
	// No run-queue source: thread CPU but no unaccounted.
	noDelay := after
	noDelay.haveRunDelay = false
	if got := noDelay.since(base, baseStepStart).String(); strings.Contains(got, "runnable") || strings.Contains(got, "unaccounted") {
		t.Fatalf("window without run-queue wait: %q", got)
	}
	// A quiet window keeps the short form; short Go waits are not named.
	quiet := usagePoint{wall: t0.Add(time.Millisecond), tid: 7, cpu: time.Millisecond, haveCPU: true, goSchedWait: goSchedWaitShown - 1}
	if got, want := quiet.since(usagePoint{wall: t0, tid: 7, haveCPU: true}, baseStepStart).String(), "since step start 1ms: thread cpu 1ms"; got != want {
		t.Fatalf("quiet window = %q, want %q", got, want)
	}
}

// An overrun message carries the injected samples' differences, so its
// causes read the same on any host: the script run's own usage, then the
// window from the baseline to the end of the callback.
func TestOverrunMessageNamesCauses(t *testing.T) {
	r := newRoom(t, memPack(t, "causes", "function spin() while true do end end", nil), Options{Budget: Budget{Load: time.Second, Frame: time.Second, Input: time.Second}})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	// A live hostContention sampler would append /proc load after the
	// closing paren and make this message host-dependent.
	r.contention = nil
	samples := []threadUsage{
		{cpu: time.Millisecond, voluntary: 4, minorFaults: 100},
		{cpu: 2 * time.Millisecond, voluntary: 9, involuntary: 1, majorFaults: 3, minorFaults: 120, blocksRead: 24},
	}
	r.threadSample = func() (threadUsage, bool) {
		u := samples[0]
		samples = samples[1:]
		return u, true
	}
	t0 := time.Unix(100, 0)
	r.windowSample = func(base bool) usagePoint {
		if base {
			return usagePoint{wall: t0, tid: 3, cpu: time.Millisecond, haveCPU: true, runDelay: time.Millisecond, haveRunDelay: true, gcCycles: 7}
		}
		return usagePoint{wall: t0.Add(50 * time.Millisecond), tid: 3, cpu: 3 * time.Millisecond, haveCPU: true,
			runDelay: 41 * time.Millisecond, haveRunDelay: true, gcCycles: 8, gcPause: 350 * time.Microsecond, goSchedWait: 1500 * time.Microsecond}
	}
	spin, ok := r.L.GetGlobal("spin").(*lua.LFunction)
	if !ok {
		t.Fatal("spin is not a function")
	}
	_, err := r.call(spin, time.Millisecond, 0)
	if err == nil {
		t.Fatal("callback past its budget did not fail")
	}
	want := ": thread cpu 1ms, 5 voluntary/1 involuntary switches, 3 major/20 minor faults, 12 KiB read; " +
		"since callback start 50ms: thread cpu 2ms, runnable 40ms, 1 GC cycles, GC stop-the-world 350µs, Go sched wait >=1.5ms, unaccounted 8ms)"
	if msg := err.Error(); !strings.HasPrefix(msg, "script exceeded 1ms budget (elapsed ") || !strings.HasSuffix(msg, want) {
		t.Fatalf("overrun %q lacks %q", msg, want)
	}
}

// windowCounter wraps the real usage-point reader, counting baselines and
// after points and failing if one runs between a callback's two in-window
// thread samples.
type windowCounter struct {
	base, after int
	inScript    bool
}

func countWindows(t *testing.T, r *Instance) *windowCounter {
	t.Helper()
	c := &windowCounter{}
	r.threadSample = func() (threadUsage, bool) {
		c.inScript = !c.inScript
		return readThreadUsage()
	}
	r.windowSample = func(base bool) usagePoint {
		if c.inScript {
			t.Error("usage point read inside the script run")
		}
		if base {
			c.base++
		} else {
			c.after++
		}
		hook := r.windowSample
		r.windowSample = nil
		defer func() { r.windowSample = hook }()
		return r.readWindow(base)
	}
	return c
}

// A Step reads one baseline that its callbacks share, and a callback within
// budget reads no after point. Only an overrun reads one. A callback outside
// a Step reads its own baseline.
func TestUsageWindowReadsOncePerStepAndAfterOnlyOnOverrun(t *testing.T) {
	const budget = 20 * time.Millisecond
	r := newRoom(t, memPack(t, "window-count", "function update() end\nfunction draw() end\nfunction on_input() return true end", nil),
		Options{Budget: Budget{Load: time.Second, Frame: budget, Input: budget}})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	c := countWindows(t, r)
	r.Step(time.Now())
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	if c.base != 1 || c.after != 0 {
		t.Fatalf("quiet Step read %d baselines and %d after points, want 1 and 0", c.base, c.after)
	}
	r.Input("confirm")
	if c.base != 2 || c.after != 0 {
		t.Fatalf("quiet Input read %d baselines and %d after points in all, want 2 and 0", c.base, c.after)
	}
	r.L.SetGlobal("draw", r.L.NewFunction(func(*lua.LState) int { time.Sleep(budget + 10*time.Millisecond); return 0 }))
	r.Step(time.Now())
	if r.frameOverruns != 1 {
		t.Fatalf("slow draw was not a soft overrun: %d overruns, %v", r.frameOverruns, r.Err())
	}
	if c.base != 3 || c.after != 1 {
		t.Fatalf("overrunning Step read %d baselines and %d after points in all, want 3 and 1", c.base, c.after)
	}
	got := r.frameStreak.timings[0]
	if !got.haveWindow || got.window.label != baseStepStart || got.window.span < got.elapsed {
		t.Fatalf("overrun window: %+v", got)
	}
	if s := got.String(); !strings.Contains(s, "; since step start ") {
		t.Fatalf("overrun text lacks its window: %s", s)
	}
}

// The baseline and after-point reads sit outside the elapsed window, so a
// stall in either does not count against the callback's budget.
func TestUsageWindowStallDoesNotCountTowardBudget(t *testing.T) {
	const budget, stall = 40 * time.Millisecond, 60 * time.Millisecond
	r := newRoom(t, memPack(t, "window-stall", "function draw() end", nil), Options{Budget: Budget{Load: time.Second, Frame: budget, Input: budget}})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	r.windowSample = func(bool) usagePoint {
		time.Sleep(stall)
		return usagePoint{wall: time.Now()}
	}
	r.Step(time.Now())
	if err := r.Err(); err != nil || r.frameOverruns != 0 {
		t.Fatalf("baseline stall counted against draw: %d overruns, %v", r.frameOverruns, err)
	}
	// An overrun's after point stalls past the hard limit; the recorded
	// elapsed time stays a soft overrun.
	r.L.SetGlobal("draw", r.L.NewFunction(func(*lua.LState) int { time.Sleep(budget + 5*time.Millisecond); return 0 }))
	r.windowSample = func(base bool) usagePoint {
		if !base {
			time.Sleep(budget * callbackHardLimitFactor)
		}
		return usagePoint{wall: time.Now()}
	}
	r.Step(time.Now())
	if err := r.Err(); err != nil || r.frameOverruns != 1 {
		t.Fatalf("after-point stall turned a soft overrun into %d overruns, %v", r.frameOverruns, err)
	}
	if got := r.frameStreak.timings[0]; got.elapsed >= budget*callbackHardLimitFactor || got.window.span < budget*callbackHardLimitFactor {
		t.Fatalf("elapsed %s, window %s", got.elapsed, got.window.span)
	}
}

// The runtime metrics exist in the toolchain with the kinds runtimeUsage
// reads.
func TestRuntimeUsageMetricsExist(t *testing.T) {
	type want struct {
		kind       metrics.ValueKind
		cumulative bool
	}
	kinds := map[string]want{gcCyclesMetric: {metrics.KindUint64, true}, gcPauseCPUMetric: {metrics.KindFloat64, true},
		gomaxprocsMetric: {metrics.KindUint64, false}, goSchedMetric: {metrics.KindFloat64Histogram, true}}
	for _, d := range metrics.All() {
		if k, ok := kinds[d.Name]; ok {
			if d.Kind != k.kind || d.Cumulative != k.cumulative {
				t.Fatalf("%s: kind %v cumulative %v", d.Name, d.Kind, d.Cumulative)
			}
			delete(kinds, d.Name)
		}
	}
	if len(kinds) != 0 {
		t.Fatalf("missing runtime metrics: %v", kinds)
	}
}

// A partial KiB rounds up, so a single 512-byte read still shows.
func TestBlocksReadText(t *testing.T) {
	if got := (threadUsage{blocksRead: 1}).String(); !strings.HasSuffix(got, ", 1 KiB read") {
		t.Fatalf("one 512-byte block: %q", got)
	}
	if got := (threadUsage{blocksRead: 4096}).String(); !strings.HasSuffix(got, ", 2048 KiB read") {
		t.Fatalf("2 MiB: %q", got)
	}
}

// The longest new scheduler wait is the lower bound of the highest bucket
// that grew; nothing new, an open lower bucket or a changed layout give 0.
func TestLongestNewWait(t *testing.T) {
	h := &metrics.Float64Histogram{
		Counts:  []uint64{5, 3, 2, 7},
		Buckets: []float64{math.Inf(-1), 0.001, 0.004, 0.016, math.Inf(1)},
	}
	if got, want := longestNewWait(h, []uint64{5, 2, 2, 7}), time.Millisecond; got != want {
		t.Fatalf("one new 1-4ms wait = %s, want %s", got, want)
	}
	if got, want := longestNewWait(h, []uint64{4, 3, 1, 6}), 16*time.Millisecond; got != want {
		t.Fatalf("new waits up to the open bucket = %s, want %s", got, want)
	}
	for _, before := range [][]uint64{{5, 3, 2, 7}, {4, 3, 2, 7}, {5, 3, 2}} {
		if got := longestNewWait(h, before); got != 0 {
			t.Fatalf("longestNewWait(%v) = %s, want 0", before, got)
		}
	}
}

// BenchmarkRuntimeUsageBase is the Go runtime part of a baseline read, once
// per Step. It allocates nothing after the first read.
func BenchmarkRuntimeUsageBase(b *testing.B) {
	var g runtimeUsage
	var p usagePoint
	g.read(&p, true)
	b.ReportAllocs()
	for b.Loop() {
		g.read(&p, true)
	}
}

// BenchmarkRuntimeUsageAfter is the Go runtime part of an after-point read,
// taken only for a reported overrun.
func BenchmarkRuntimeUsageAfter(b *testing.B) {
	var g runtimeUsage
	var p usagePoint
	g.read(&p, true)
	g.read(&p, false)
	b.ReportAllocs()
	for b.Loop() {
		g.read(&p, false)
	}
}

// The after point is read only when the timing will be printed. call()
// reports only an expired deadline, and a script error before its deadline
// reports the error text alone, so neither reads one even past its budget.
func TestUsageWindowAfterOnlyWhenTimingIsReported(t *testing.T) {
	const budget = 10 * time.Millisecond
	r := newRoom(t, memPack(t, "window-report", "function ok() end\nfunction boom() error('boom') end", nil),
		Options{Budget: Budget{Load: time.Second, Frame: budget, Input: budget}})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	c := countWindows(t, r)
	// The sample after the script stalls past the budget, so elapsed
	// overruns while the script itself returned before its deadline.
	counted := r.threadSample
	r.threadSample = func() (threadUsage, bool) {
		u, ok := counted()
		if !c.inScript {
			time.Sleep(2 * budget)
		}
		return u, ok
	}
	fn := func(name string) *lua.LFunction {
		f, ok := r.L.GetGlobal(name).(*lua.LFunction)
		if !ok {
			t.Fatalf("%s is not a function", name)
		}
		return f
	}
	timing, err := r.call(fn("ok"), budget, 0)
	if err != nil || timing.elapsed <= budget {
		t.Fatalf("call: elapsed %s, %v", timing.elapsed, err)
	}
	if c.after != 0 || timing.haveWindow {
		t.Fatalf("call past its unreported budget read %d after points", c.after)
	}
	if err := r.callSoft("boom", fn("boom"), budget, 0); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("script error: %v", err)
	}
	if c.after != 0 {
		t.Fatalf("script error past soft read %d after points", c.after)
	}
	var over softOverrun
	if err := r.callSoft("ok", fn("ok"), budget, 0); !errors.As(err, &over) {
		t.Fatalf("soft overrun: %v", err)
	}
	if c.after != 1 || !over.timing.haveWindow {
		t.Fatalf("reported soft overrun read %d after points, window %v", c.after, over.timing.haveWindow)
	}
}
