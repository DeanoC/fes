package rooms

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/testwait"
)

func contentionPair() (base, now contentionSnapshot) {
	t0 := time.Unix(1000, 0)
	base = contentionSnapshot{at: t0, loadavg: "0.50 0.40 0.30 1/80",
		cpuTotal: 1000, cpuIdle: 800, haveCPU: true, cpus: 2,
		procs: map[int]procTicks{
			1:   {comm: "init", ticks: 5},
			40:  {comm: "fogcast-api", ticks: 300},
			41:  {comm: "mister-runtime", ticks: 50},
			42:  {comm: "fogcast-tenfoot", ticks: 70},
			43:  {comm: "old-name", ticks: 900},
			44:  {comm: "idle-daemon", ticks: 12},
			400: {comm: "exited", ticks: 99},
		}}
	now = contentionSnapshot{at: t0.Add(1250 * time.Millisecond), loadavg: "3.52 2.10 0.95 6/83",
		cpuTotal: 1250, cpuIdle: 810, haveCPU: true, cpus: 2,
		procs: map[int]procTicks{
			1:  {comm: "init", ticks: 5},
			40: {comm: "fogcast-api", ticks: 400},    // 1s
			41: {comm: "mister-runtime", ticks: 90},  // 400ms
			42: {comm: "fogcast-tenfoot", ticks: 95}, // 250ms
			43: {comm: "fogcast-kit", ticks: 20},     // pid reused: all 200ms
			44: {comm: "idle-daemon", ticks: 12},
			50: {comm: "fes-scan", ticks: 10}, // new: all 100ms
		}}
	return base, now
}

func TestContentionTextFormats(t *testing.T) {
	base, now := contentionPair()
	tick := 10 * time.Millisecond
	if got, want := contentionText(now, &base, tick, contentionPanelTop),
		"load 3.52 2.10 0.95 6/83; over 1.25s cpu 96% busy of 2; top fogcast-api 80%, mister-runtime 32%, fogcast-tenfoot 20%"; got != want {
		t.Fatalf("panel text\n got %q\nwant %q", got, want)
	}
	if got, want := contentionText(now, &base, tick, contentionLogTop),
		"load 3.52 2.10 0.95 6/83; over 1.25s cpu 96% busy of 2; top fogcast-api 80% (pid 40, 1s), mister-runtime 32% (pid 41, 400ms), "+
			"fogcast-tenfoot 20% (pid 42, 250ms), fogcast-kit 16% (pid 43, 200ms), fes-scan 8% (pid 50, 100ms)"; got != want {
		t.Fatalf("log text\n got %q\nwant %q", got, want)
	}
	if got, want := contentionText(now, nil, tick, contentionPanelTop), "load 3.52 2.10 0.95 6/83; no cpu baseline"; got != want {
		t.Fatalf("no baseline %q", got)
	}
	now.haveCPU = false
	now.procs = nil
	if got, want := contentionText(now, &base, tick, contentionPanelTop), "load 3.52 2.10 0.95 6/83; over 1.25s"; got != want {
		t.Fatalf("no cpu %q", got)
	}
}

// A report diffs against a baseline from before the callback that leaves a
// measurable window, and falls back to the newest earlier one.
func TestContentionBaselineChoice(t *testing.T) {
	t0 := time.Unix(1000, 0)
	s := newContentionSampler(nil)
	s.prev = contentionSnapshot{at: t0}
	s.last = contentionSnapshot{at: t0.Add(time.Second)}
	for _, tc := range []struct {
		start, now time.Time
		want       time.Time
		ok         bool
	}{
		{t0.Add(1900 * time.Millisecond), t0.Add(2 * time.Second), t0.Add(time.Second), true},
		{t0.Add(1100 * time.Millisecond), t0.Add(1200 * time.Millisecond), t0, true},
		{t0.Add(500 * time.Millisecond), t0.Add(1100 * time.Millisecond), t0, true},
		{t0.Add(-time.Millisecond), t0.Add(10 * time.Millisecond), time.Time{}, false},
	} {
		got, ok := s.baseline(tc.start, tc.now)
		if ok != tc.ok || !got.at.Equal(tc.want) {
			t.Fatalf("baseline(%s, %s) = %s %v, want %s %v", tc.start.Sub(t0), tc.now.Sub(t0), got.at, ok, tc.want, tc.ok)
		}
	}
	s.prev = contentionSnapshot{}
	if got, ok := s.baseline(t0.Add(1100*time.Millisecond), t0.Add(1200*time.Millisecond)); !ok || !got.at.Equal(t0.Add(time.Second)) {
		t.Fatalf("short-window fallback = %s %v", got.at, ok)
	}
}

// tick reads at most once per interval, never on the caller's goroutine
// twice at once, and keeps the previous snapshot.
func TestContentionTickRateLimitsReads(t *testing.T) {
	var reads atomic.Int32
	release := make(chan struct{})
	t0 := time.Unix(1000, 0)
	s := newContentionSampler(func() (contentionSnapshot, bool) {
		n := reads.Add(1)
		<-release
		return contentionSnapshot{at: t0.Add(time.Duration(n) * time.Second)}, true
	})
	s.tick(t0)
	s.tick(t0.Add(10 * time.Millisecond))
	s.tick(t0.Add(2 * time.Second)) // still refreshing
	release <- struct{}{}
	waitRefreshed(t, s)
	s.tick(t0.Add(2100 * time.Millisecond))
	release <- struct{}{}
	waitRefreshed(t, s)
	s.tick(t0.Add(2500 * time.Millisecond))
	if n := reads.Load(); n != 2 {
		t.Fatalf("%d reads, want 2", n)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.prev.at.Equal(t0.Add(time.Second)) || !s.last.at.Equal(t0.Add(2*time.Second)) {
		t.Fatalf("snapshots %s, %s", s.prev.at, s.last.at)
	}
}

func waitRefreshed(t *testing.T, s *contentionSampler) {
	t.Helper()
	deadline := time.Now().Add(testwait.Timeout)
	for {
		s.mu.Lock()
		done := !s.refreshing
		s.mu.Unlock()
		if done {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("refresh did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}

// Without a reader a deadline error is unchanged and nothing is logged.
func TestDeadlineMissWithoutReader(t *testing.T) {
	r := newRoom(t, memPack(t, "no-proc", "function draw() end", nil), Options{})
	for _, s := range []*contentionSampler{nil, newContentionSampler(nil)} {
		r.contention = s
		base := errSoftOverrun
		if got := r.deadlineMiss(base, callTiming{elapsed: time.Millisecond}); got != base {
			t.Fatalf("deadlineMiss changed the error: %v", got)
		}
		s.tick(time.Now())
	}
}

func BenchmarkContentionTick(b *testing.B) {
	s := newContentionSampler(func() (contentionSnapshot, bool) { return contentionSnapshot{}, false })
	now := time.Now()
	s.tick(now)
	for b.Loop() {
		s.tick(now)
	}
}
