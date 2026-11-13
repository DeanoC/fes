package rooms

import (
	"math"
	"runtime/metrics"
	"strings"
	"testing"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// since subtracts every counter, and the text names each cause that occurred
// while a run with none of them keeps the short form.
func TestThreadUsageSinceAndString(t *testing.T) {
	before := threadUsage{cpu: time.Millisecond, voluntary: 2, involuntary: 1, majorFaults: 1, minorFaults: 10, blocksRead: 8,
		runDelay: time.Millisecond, haveRunDelay: true, gcCycles: 40, gcPause: 3 * time.Millisecond}
	after := threadUsage{cpu: 4110 * time.Microsecond, voluntary: 17, involuntary: 3, majorFaults: 13, minorFaults: 350, blocksRead: 104,
		runDelay: 2500 * time.Microsecond, haveRunDelay: true, gcCycles: 41, gcPause: 5100 * time.Microsecond}
	d := after.since(before)
	want := threadUsage{cpu: 3110 * time.Microsecond, voluntary: 15, involuntary: 2, majorFaults: 12, minorFaults: 340, blocksRead: 96,
		runDelay: 1500 * time.Microsecond, haveRunDelay: true, gcCycles: 1, gcPause: 2100 * time.Microsecond}
	if d != want {
		t.Fatalf("since = %+v, want %+v", d, want)
	}
	if got, w := d.String(), "thread cpu 3.11ms, 15 voluntary/2 involuntary switches, runnable 1.5ms, 12 major/340 minor faults, 48 KiB read, 1 GC cycles, GC stop-the-world 2.1ms"; got != w {
		t.Fatalf("String = %q, want %q", got, w)
	}
	quiet := threadUsage{cpu: 2 * time.Millisecond, voluntary: 1, involuntary: 9}
	if got, w := quiet.String(), "thread cpu 2ms, 1 voluntary/9 involuntary switches"; got != w {
		t.Fatalf("quiet String = %q, want %q", got, w)
	}
	// Run delay is reported, even as zero, only when both samples had it.
	noDelay := after
	noDelay.haveRunDelay = false
	if d := noDelay.since(before); d.haveRunDelay || strings.Contains(d.String(), "runnable") {
		t.Fatalf("run delay without a kernel source: %s", d)
	}
	zero := threadUsage{haveRunDelay: true}
	if got := zero.String(); !strings.Contains(got, ", runnable 0s") || strings.Contains(got, "faults") || strings.Contains(got, "GC") {
		t.Fatalf("zero run delay text: %q", got)
	}
}

// An overrun message carries the injected samples' difference, so its causes
// read the same on any host.
func TestOverrunMessageNamesCauses(t *testing.T) {
	r := newRoom(t, memPack(t, "causes", "function spin() while true do end end", nil), Options{Budget: Budget{Load: time.Second, Frame: time.Second, Input: time.Second}})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	samples := []threadUsage{
		{cpu: time.Millisecond, voluntary: 4, minorFaults: 100, runDelay: time.Millisecond, haveRunDelay: true, gcCycles: 7},
		{cpu: 2 * time.Millisecond, voluntary: 9, involuntary: 1, majorFaults: 3, minorFaults: 120, blocksRead: 24,
			runDelay: 41 * time.Millisecond, haveRunDelay: true, gcCycles: 8, gcPause: 350 * time.Microsecond},
	}
	r.threadSample = func() (threadUsage, bool) {
		u := samples[0]
		samples = samples[1:]
		return u, true
	}
	spin, ok := r.L.GetGlobal("spin").(*lua.LFunction)
	if !ok {
		t.Fatal("spin is not a function")
	}
	_, err := r.call(spin, time.Millisecond, 0)
	if err == nil {
		t.Fatal("callback past its budget did not fail")
	}
	want := ": thread cpu 1ms, 5 voluntary/1 involuntary switches, runnable 40ms, 3 major/20 minor faults, 12 KiB read, 1 GC cycles, GC stop-the-world 350µs)"
	if msg := err.Error(); !strings.HasPrefix(msg, "script exceeded 1ms budget (elapsed ") || !strings.HasSuffix(msg, want) {
		t.Fatalf("overrun %q lacks %q", msg, want)
	}
}

// The GC metrics exist in the toolchain with the kinds runtimeUsage reads.
func TestRuntimeUsageMetricsExist(t *testing.T) {
	kinds := map[string]metrics.ValueKind{gcCyclesMetric: metrics.KindUint64, gcPauseMetric: metrics.KindFloat64Histogram}
	for _, d := range metrics.All() {
		if k, ok := kinds[d.Name]; ok {
			if d.Kind != k || !d.Cumulative {
				t.Fatalf("%s: kind %v cumulative %v", d.Name, d.Kind, d.Cumulative)
			}
			delete(kinds, d.Name)
		}
	}
	if len(kinds) != 0 {
		t.Fatalf("missing runtime metrics: %v", kinds)
	}
}

func TestHistogramSum(t *testing.T) {
	h := &metrics.Float64Histogram{
		Counts:  []uint64{1, 2, 0, 1},
		Buckets: []float64{math.Inf(-1), 0.001, 0.003, 0.004, math.Inf(1)},
	}
	// 1*0.001 + 2*0.002 + 1*0.004
	if got, want := histogramSum(h), 9*time.Millisecond; got != want {
		t.Fatalf("histogramSum = %s, want %s", got, want)
	}
}

// The GC read allocates nothing after its first.
func BenchmarkRuntimeUsageRead(b *testing.B) {
	var g runtimeUsage
	var u threadUsage
	g.read(&u)
	b.ReportAllocs()
	for b.Loop() {
		g.read(&u)
	}
}
