package rooms

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/testwait"
)

func contentionPair() (base, now contentionSnapshot) {
	t0 := time.Unix(1000, 0)
	// Uptime 100s is 10000 ticks: processes in the baseline started at 100.
	base = contentionSnapshot{at: t0, loadavg: "0.50 0.40 0.30 1/80",
		cpuTotal: 1000, cpuIdle: 800, haveCPU: true, cpus: 2,
		uptime: 100 * time.Second, haveUptime: true,
		procs: map[int]procTicks{
			1:   {comm: "init", ticks: 5, start: 1},
			40:  {comm: "fogcast-api", ticks: 300, start: 100},
			41:  {comm: "mister-runtime", ticks: 50, start: 100},
			42:  {comm: "fogcast-tenfoot", ticks: 70, start: 100},
			43:  {comm: "old-name", ticks: 900, start: 100},
			44:  {comm: "idle-daemon", ticks: 12, start: 100},
			45:  {comm: "kworker/0:1-events", ticks: 160, start: 2},
			400: {comm: "exited", ticks: 99, start: 100},
		}}
	now = contentionSnapshot{at: t0.Add(1250 * time.Millisecond), loadavg: "3.52 2.10 0.95 6/83",
		cpuTotal: 1250, cpuIdle: 810, haveCPU: true, cpus: 2,
		uptime: 101250 * time.Millisecond, haveUptime: true,
		procs: map[int]procTicks{
			1:  {comm: "init", ticks: 5, start: 1},
			40: {comm: "fogcast-api", ticks: 400, start: 100},            // 1s
			41: {comm: "mister-runtime", ticks: 90, start: 100},          // 400ms
			42: {comm: "fogcast-tenfoot", ticks: 95, start: 100},         // 250ms
			43: {comm: "fogcast-kit", ticks: 20, start: 10050},           // pid reused in the window: all 200ms
			44: {comm: "idle-daemon", ticks: 12, start: 100},             // idle
			45: {comm: "kworker/0:1-mm_percpu_wq", ticks: 163, start: 2}, // renamed: 30ms
			50: {comm: "fes-scan", ticks: 10, start: 10100},              // new: all 100ms
			51: {comm: "unlisted", ticks: 164, start: 3},                 // missed by the baseline: skipped
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
			"fogcast-tenfoot 20% (pid 42, 250ms), fogcast-kit 16% (pid 43, 200ms), fes-scan 8% (pid 50, 100ms), kworker/0:1-mm_percpu_wq 2% (pid 45, 30ms)"; got != want {
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
		got, ok := s.baseline(tc.start, contentionSnapshot{at: tc.now})
		if ok != tc.ok || !got.at.Equal(tc.want) {
			t.Fatalf("baseline(%s, %s) = %s %v, want %s %v", tc.start.Sub(t0), tc.now.Sub(t0), got.at, ok, tc.want, tc.ok)
		}
	}
	s.prev = contentionSnapshot{}
	if got, ok := s.baseline(t0.Add(1100*time.Millisecond), contentionSnapshot{at: t0.Add(1200 * time.Millisecond)}); !ok || !got.at.Equal(t0.Add(time.Second)) {
		t.Fatalf("short-window fallback = %s %v", got.at, ok)
	}
}

// A baseline read that started before the callback but finished after it
// started counted part of the callback, so it is not a baseline. Windows
// run between read midpoints.
func TestContentionBaselineUsesReadEnd(t *testing.T) {
	t0 := time.Unix(1000, 0)
	s := newContentionSampler(nil)
	s.prev = contentionSnapshot{at: t0, done: t0.Add(200 * time.Millisecond)}
	s.last = contentionSnapshot{at: t0.Add(time.Second), done: t0.Add(1300 * time.Millisecond)}
	now := contentionSnapshot{at: t0.Add(2 * time.Second), done: t0.Add(2100 * time.Millisecond)}
	got, ok := s.baseline(t0.Add(1200*time.Millisecond), now)
	if !ok || !got.at.Equal(t0) {
		t.Fatalf("baseline = %s %v, want the read that finished first", got.at.Sub(t0), ok)
	}
	if got, want := contentionText(now, &got, 10*time.Millisecond, contentionPanelTop), "load ; over 1.95s"; got != want {
		t.Fatalf("window %q, want %q", got, want)
	}
	if got, ok := s.baseline(t0.Add(1400*time.Millisecond), now); !ok || !got.at.Equal(t0.Add(time.Second)) {
		t.Fatalf("baseline = %s %v, want the newer finished read", got.at.Sub(t0), ok)
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
