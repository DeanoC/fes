//go:build linux

package rooms

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// writeFakeProc writes a /proc tree: loadavg, stat with two CPUs, and one
// stat per process with the given utime and stime.
func writeFakeProc(t *testing.T, root, loadavg string, user, idle uint64, procs map[int][3]string) {
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
	u, i := strconv.FormatUint(user, 10), strconv.FormatUint(idle, 10)
	write("loadavg", loadavg+"\n")
	write("stat", "cpu  "+u+" 0 0 "+i+" 0 0 0 0 0 0\ncpu0 1 0 0 1 0 0 0 0 0 0\ncpu1 1 0 0 1 0 0 0 0 0 0\nintr 12345\nctxt 999\n")
	for pid, p := range procs {
		// comm, utime, stime: fields 3..13 are filler, then utime and stime.
		write(strconv.Itoa(pid)+"/stat", strconv.Itoa(pid)+" ("+p[0]+") S 1 1 1 0 -1 4194560 100 0 0 0 "+p[1]+" "+p[2]+" 0 0 20 0 3 0 100 1000 50\n")
	}
}

func TestProcReaderSummarisesFakeProc(t *testing.T) {
	root := t.TempDir()
	t0 := time.Unix(5000, 0)
	writeFakeProc(t, root, "0.20 0.10 0.05 1/70 123", 600, 400, map[int][3]string{
		1:   {"init", "3", "2"},
		210: {"fogcast-api", "100", "20"},
		211: {"mister-runtime", "40", "10"},
		212: {"odd) (name", "1", "0"},
	})
	// Entries that are not processes, or whose stat is gone, are skipped.
	if err := os.MkdirAll(filepath.Join(root, "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "999"), 0o755); err != nil {
		t.Fatal(err)
	}
	base, ok := (&procReader{root: root}).read(t0)
	if !ok || !base.haveCPU || base.cpus != 2 || base.cpuTotal != 1000 || base.cpuIdle != 400 || len(base.procs) != 4 {
		t.Fatalf("baseline %+v", base)
	}
	if got := base.procs[212]; got.comm != "odd) (name" || got.ticks != 1 {
		t.Fatalf("comm with parentheses: %+v", got)
	}
	if got := base.procs[210]; got.comm != "fogcast-api" || got.ticks != 120 {
		t.Fatalf("utime+stime: %+v", got)
	}
	writeFakeProc(t, root, "2.75 1.10 0.40 4/72 130", 790, 410, map[int][3]string{
		1:   {"init", "3", "2"},
		210: {"fogcast-api", "160", "30"},   // +70 ticks
		211: {"mister-runtime", "60", "15"}, // +25
		212: {"odd) (name", "1", "0"},
		300: {"kitLibraryScan", "30", "10"}, // new: 40
	})
	now, ok := (&procReader{root: root}).read(t0.Add(time.Second))
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

func TestProcReaderNeedsLoadavg(t *testing.T) {
	if _, ok := (&procReader{root: t.TempDir()}).read(time.Now()); ok {
		t.Fatal("read succeeded without loadavg")
	}
}

func TestParseProcStatRejectsMalformed(t *testing.T) {
	for _, data := range []string{"cpu 1 2 3\n", "cpu 1 2 3 x 5 6\n", "intr 1\n"} {
		if _, _, _, ok := parseProcStat([]byte(data)); ok {
			t.Fatalf("accepted %q", data)
		}
	}
	for _, data := range []string{"1 (x) S 1\n", "no parens\n", "1 (x) S 1 1 1 0 -1 0 0 0 0 0 a b\n"} {
		if _, _, ok := parsePidStat([]byte(data)); ok {
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
