//go:build linux

package rooms

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// fakeProc is one process in a fake /proc tree: CPU and start time in clock
// ticks.
type fakeProc struct {
	comm                string
	utime, stime, start uint64
	threads             int
}

// writeFakeProc writes a /proc tree: loadavg, uptime, stat with two CPUs,
// and one stat per process. Process directories not in procs are removed.
func writeFakeProc(t *testing.T, root, loadavg, uptime string, user, idle uint64, procs map[int]fakeProc) {
	t.Helper()
	write := func(name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if pid, err := strconv.Atoi(e.Name()); err == nil {
			if _, keep := procs[pid]; !keep {
				if err := os.RemoveAll(filepath.Join(root, e.Name())); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	u, i := strconv.FormatUint(user, 10), strconv.FormatUint(idle, 10)
	write("loadavg", loadavg+"\n")
	write("uptime", uptime+" 12.34\n")
	write("stat", "cpu  "+u+" 0 0 "+i+" 0 0 0 0 0 0\ncpu0 1 0 0 1 0 0 0 0 0 0\ncpu1 1 0 0 1 0 0 0 0 0 0\nintr 12345\nctxt 999\n")
	for pid, p := range procs {
		threads := max(p.threads, 1)
		// Fields 3..13 are filler, then utime (14), stime (15), filler,
		// num_threads (20), itrealvalue and starttime (22).
		write(strconv.Itoa(pid)+"/stat", fmt.Sprintf("%d (%s) S 1 1 1 0 -1 4194560 100 0 0 0 %d %d 0 0 20 0 %d 0 %d 1000 50\n",
			pid, p.comm, p.utime, p.stime, threads, p.start))
	}
}

func TestProcReaderSummarisesFakeProc(t *testing.T) {
	root := t.TempDir()
	t0 := time.Unix(5000, 0)
	writeFakeProc(t, root, "0.20 0.10 0.05 1/70 123", "50.00", 600, 400, map[int]fakeProc{
		1:   {comm: "init", utime: 3, stime: 2, start: 1},
		210: {comm: "fogcast-api", utime: 100, stime: 20, start: 100},
		211: {comm: "mister-runtime", utime: 40, stime: 10, start: 100},
		212: {comm: "odd) (name", utime: 1, start: 100},
	})
	// Entries that are not processes, or whose stat is gone, are skipped.
	if err := os.MkdirAll(filepath.Join(root, "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "999"), 0o755); err != nil {
		t.Fatal(err)
	}
	clock := t0
	next := func() time.Time { c := clock; clock = clock.Add(100 * time.Millisecond); return c }
	base, ok := (&procReader{root: root, now: next}).read()
	if !base.at.Equal(t0) || !base.done.Equal(t0.Add(100*time.Millisecond)) {
		t.Fatalf("read stamped %s..%s, want the scan's start and end", base.at.Sub(t0), base.done.Sub(t0))
	}
	if !ok || !base.haveCPU || base.cpus != 2 || base.cpuTotal != 1000 || base.cpuIdle != 400 || len(base.procs) != 4 ||
		!base.haveUptime || base.uptime != 50*time.Second {
		t.Fatalf("baseline %+v", base)
	}
	if got := base.procs[212]; got.comm != "odd) (name" || got.ticks != 1 {
		t.Fatalf("comm with parentheses: %+v", got)
	}
	if got := base.procs[210]; got.comm != "fogcast-api" || got.ticks != 120 || got.start != 100 {
		t.Fatalf("utime+stime: %+v", got)
	}
	writeFakeProc(t, root, "2.75 1.10 0.40 4/72 130", "51.00", 790, 410, map[int]fakeProc{
		1:   {comm: "init", utime: 3, stime: 2, start: 1},
		210: {comm: "fogcast-api", utime: 160, stime: 30, start: 100},   // +70 ticks
		211: {comm: "mister-runtime", utime: 60, stime: 15, start: 100}, // +25
		212: {comm: "odd) (name", utime: 1, start: 100},
		300: {comm: "kitLibraryScan", utime: 30, stime: 10, start: 5050}, // new: 40
	})
	clock = t0.Add(time.Second)
	now, ok := (&procReader{root: root, now: next}).read()
	if !ok {
		t.Fatal("second read failed")
	}
	s := newContentionSampler(nil)
	if got, want := contentionText(now, &base, s.clockTick, contentionPanelTop),
		"load 2.75 1.10 0.40 4/72; over 1s cpu 95% busy of 2; top fogcast-api 70%, kitLibraryScan 40%, mister-runtime 25%"; got != want {
		t.Fatalf("panel\n got %q\nwant %q", got, want)
	}
	if got, want := contentionText(now, &base, s.clockTick, contentionLogTop),
		"load 2.75 1.10 0.40 4/72; over 1s cpu 95% busy of 2; top fogcast-api 70% (pid 210, 700ms), kitLibraryScan 40% (pid 300, 400ms), mister-runtime 25% (pid 211, 250ms)"; got != want {
		t.Fatalf("log\n got %q\nwant %q", got, want)
	}
}

// Kit A reported a kworker at 275% of a 595ms window: the same 1.64s of
// lifetime CPU in two windows, because the kworker had renamed itself for
// another workqueue and was matched by comm. Processes are matched by pid and
// start time; one missing from the baseline counts only if it started in the
// window, so no process can use more than its threads for the whole window.
func TestProcReaderWindowMatchesProcessIdentity(t *testing.T) {
	root := t.TempDir()
	t0 := time.Unix(5000, 0)
	read := func(at time.Time) contentionSnapshot {
		t.Helper()
		snap, ok := (&procReader{root: root, now: func() time.Time { return at }}).read()
		if !ok {
			t.Fatal("read failed")
		}
		return snap
	}
	threads := map[int]int{}
	procs := func(m map[int]fakeProc) map[int]fakeProc {
		for pid, p := range m {
			threads[pid] = max(p.threads, 1)
		}
		return m
	}
	// Uptime 1000s is 100000 ticks.
	writeFakeProc(t, root, "3.09 2.20 1.86 5/120", "1000.00", 10000, 5000, procs(map[int]fakeProc{
		1250:  {comm: "fogcast-kit", utime: 900, stime: 100, start: 2000, threads: 4},
		26411: {comm: "kworker/0:1-events", utime: 0, stime: 160, start: 90000},
		32001: {comm: "old-helper", utime: 500, stime: 50, start: 3000},
	}))
	base := read(t0)
	writeFakeProc(t, root, "3.09 2.20 1.86 5/120", "1000.60", 10120, 5000, procs(map[int]fakeProc{
		1250:  {comm: "fogcast-kit", utime: 950, stime: 110, start: 2000, threads: 4}, // +60 ticks
		26411: {comm: "kworker/0:1-mm_percpu_wq", utime: 0, stime: 164, start: 90000}, // renamed: +4
		32001: {comm: "fes-scan", utime: 10, stime: 2, start: 100030},                 // pid reused in the window: 12
		32750: {comm: "kworker/u4:0-writeback", utime: 0, stime: 8, start: 100020},    // new in the window: 8
		26500: {comm: "kworker/1:2-events", utime: 0, stime: 164, start: 95000},       // missed by the baseline
	}))
	now := read(t0.Add(600 * time.Millisecond))
	tick := newContentionSampler(nil).clockTick
	window := now.mid().Sub(base.mid())
	used := topProcs(now, base, tick)
	got := map[int]time.Duration{}
	for _, p := range used {
		got[p.pid] = p.cpu
		if p.cpu > window*time.Duration(threads[p.pid]) {
			t.Errorf("pid %d %s used %s in a %s window with %d threads", p.pid, p.comm, p.cpu, window, threads[p.pid])
		}
		if threads[p.pid] == 1 && p.cpu > window {
			t.Errorf("single-threaded pid %d %s used %s in a %s window", p.pid, p.comm, p.cpu, window)
		}
	}
	want := map[int]time.Duration{
		1250:  600 * time.Millisecond,
		26411: 40 * time.Millisecond,
		32001: 120 * time.Millisecond,
		32750: 80 * time.Millisecond,
	}
	if len(got) != len(want) {
		t.Fatalf("used %v, want %v", got, want)
	}
	for pid, cpu := range want {
		if got[pid] != cpu {
			t.Fatalf("used %v, want %v", got, want)
		}
	}
	if got, want := contentionText(now, &base, tick, contentionLogTop),
		"load 3.09 2.20 1.86 5/120; over 600ms cpu 100% busy of 2; top fogcast-kit 100% (pid 1250, 600ms), "+
			"fes-scan 20% (pid 32001, 120ms), kworker/u4:0-writeback 13% (pid 32750, 80ms), "+
			"kworker/0:1-mm_percpu_wq 6% (pid 26411, 40ms)"; got != want {
		t.Fatalf("log\n got %q\nwant %q", got, want)
	}

	// Without the baseline's uptime nothing absent from it can be placed in
	// the window, so only processes in both reads count.
	base.haveUptime = false
	for _, p := range topProcs(now, base, tick) {
		if p.pid != 1250 && p.pid != 26411 {
			t.Fatalf("counted pid %d %s without a baseline uptime", p.pid, p.comm)
		}
	}
}

func TestParseUptime(t *testing.T) {
	for in, want := range map[string]time.Duration{"1000.60 5.00\n": 1000600 * time.Millisecond, "7": 7 * time.Second, "3.5": 3500 * time.Millisecond} {
		if got, ok := parseUptime([]byte(in)); !ok || got != want {
			t.Fatalf("parseUptime(%q) = %s %v, want %s", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "x.1", "1.x"} {
		if _, ok := parseUptime([]byte(in)); ok {
			t.Fatalf("accepted %q", in)
		}
	}
}

func TestProcReaderNeedsLoadavg(t *testing.T) {
	if _, ok := (&procReader{root: t.TempDir()}).read(); ok {
		t.Fatal("read succeeded without loadavg")
	}
}

func TestParseProcStatRejectsMalformed(t *testing.T) {
	for _, data := range []string{"cpu 1 2 3\n", "cpu 1 2 3 x 5 6\n", "intr 1\n"} {
		if _, _, _, ok := parseProcStat([]byte(data)); ok {
			t.Fatalf("accepted %q", data)
		}
	}
	for _, data := range []string{"1 (x) S 1\n", "no parens\n", "1 (x) S 1 1 1 0 -1 0 0 0 0 0 a b\n", "1 (x) S 1 1 1 0 -1 0 0 0 0 0 1 2 0 0 20 0 1 0\n", "1 (x) S 1 1 1 0 -1 0 0 0 0 0 1 2 0 0 20 0 1 0 z 1\n"} {
		if _, _, _, ok := parsePidStat([]byte(data)); ok {
			t.Fatalf("accepted %q", data)
		}
	}
}

// BenchmarkReadHostContention is the cost of one baseline read, paid once
// per second on a background goroutine while rooms are stepping, and once on
// the launcher thread after a hard miss.
func BenchmarkReadHostContention(b *testing.B) {
	for b.Loop() {
		if _, ok := readHostContention(); !ok {
			b.Fatal("no /proc")
		}
	}
}
