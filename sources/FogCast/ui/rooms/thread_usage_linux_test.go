//go:build linux

package rooms

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	lua "github.com/yuin/gopher-lua"
	"golang.org/x/sys/unix"
)

// A callback that waits instead of running reports thread CPU well below its
// script run's wall time, which is how a kit overrun tells starvation from
// slow script work. The script run sits inside the callback's elapsed time.
func TestCallTimingSeparatesWaitFromThreadCPU(t *testing.T) {
	r := newRoom(t, memPack(t, "thread-cpu", "function draw() end", nil), Options{Budget: Budget{Load: time.Second, Frame: time.Second, Input: time.Second}})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	wait := r.L.NewFunction(func(*lua.LState) int { time.Sleep(30 * time.Millisecond); return 0 })
	timing, err := r.call(wait, time.Second, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !timing.measured {
		t.Fatal("linux callback timing has no thread usage")
	}
	if timing.script < 30*time.Millisecond || timing.script > timing.elapsed || timing.thread.cpu >= timing.script/2 {
		t.Fatalf("waiting callback: elapsed %s, script run %s, thread cpu %s", timing.elapsed, timing.script, timing.thread.cpu)
	}
	if timing.thread.voluntary < 1 {
		t.Fatalf("waiting callback parked its thread without a voluntary switch: %s", timing)
	}
	if s := timing.String(); !strings.Contains(s, "; script run ") || !strings.Contains(s, ": thread cpu ") {
		t.Fatalf("timing text: %s", timing)
	}
}

// A callback that maps a file and touches every page reports page faults. The
// file's cache is dropped first, so on a disk-backed filesystem the touches
// can be major faults and the reread can count KiB read; tmpfs, or a cache
// the kernel keeps, leaves those at zero, so they are logged, not required.
func TestCallTimingReportsFaultsAndBlockReads(t *testing.T) {
	r := newRoom(t, memPack(t, "faults", "function draw() end", nil), Options{Budget: Budget{Load: time.Second, Frame: time.Second, Input: time.Second}})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	const size = 1 << 20
	path := filepath.Join(t.TempDir(), "pages")
	if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_ = f.Sync()
	_ = unix.Fadvise(int(f.Fd()), 0, size, unix.FADV_DONTNEED)
	page := os.Getpagesize()
	touch := r.L.NewFunction(func(*lua.LState) int {
		mem, err := unix.Mmap(int(f.Fd()), 0, size, unix.PROT_READ, unix.MAP_SHARED)
		if err != nil {
			t.Error(err)
			return 0
		}
		var sum byte
		for i := 0; i < size; i += page {
			sum += mem[i]
		}
		_ = unix.Munmap(mem)
		_ = unix.Fadvise(int(f.Fd()), 0, size, unix.FADV_DONTNEED)
		buf := make([]byte, size)
		if _, err := f.ReadAt(buf, 0); err != nil {
			t.Error(err)
		}
		return int(sum)
	})
	timing, err := r.call(touch, time.Second, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !timing.measured {
		t.Fatal("linux callback timing has no thread usage")
	}
	if u := timing.thread; u.minorFaults+u.majorFaults < 1 {
		t.Fatalf("touching %d mapped bytes reported no page faults: %s", size, timing)
	}
	if !strings.Contains(timing.String(), " minor faults") {
		t.Fatalf("timing text omits faults: %s", timing)
	}
	t.Logf("mapped-file callback: %s", timing)
}

// A callback that runs a GC reports at least that cycle and its pause.
func TestCallTimingReportsGC(t *testing.T) {
	r := newRoom(t, memPack(t, "gc", "function draw() end", nil), Options{Budget: Budget{Load: time.Second, Frame: time.Second, Input: time.Second}})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	gc := r.L.NewFunction(func(*lua.LState) int { runtime.GC(); return 0 })
	timing, err := r.call(gc, time.Second, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !timing.measured || timing.thread.gcCycles < 1 {
		t.Fatalf("runtime.GC in a callback reported no GC cycle: %s", timing)
	}
	if timing.thread.gcPause <= 0 {
		t.Fatalf("runtime.GC in a callback reported no stop-the-world pause: %s", timing)
	}
	if !strings.Contains(timing.String(), " GC cycles, GC stop-the-world ") {
		t.Fatalf("timing text omits GC: %s", timing)
	}
}

// Kernels with CONFIG_SCHED_INFO report the thread's run-queue wait. Without
// it the field is omitted rather than shown as zero.
func TestReadRunDelay(t *testing.T) {
	if d, ok := parseRunDelay([]byte("123456 7890 12\n")); !ok || d != 7890 {
		t.Fatalf("parseRunDelay = %s, %v", d, ok)
	}
	for _, bad := range []string{"", "123456", "123456 x 1"} {
		if _, ok := parseRunDelay([]byte(bad)); ok {
			t.Fatalf("parseRunDelay(%q) accepted", bad)
		}
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var s schedstatReader
	defer s.close()
	_, statErr := os.Stat("/proc/thread-self/schedstat")
	if _, ok := s.runDelay(); ok != (statErr == nil) {
		t.Fatalf("run delay read %v, schedstat stat error %v", ok, statErr)
	}
	if statErr != nil {
		return
	}
	// A file left from another thread is replaced, not read.
	first := s.f
	s.tid = -1
	if _, ok := s.runDelay(); !ok || s.f == first || s.tid != unix.Gettid() {
		t.Fatalf("reader kept the other thread's file: tid %d", s.tid)
	}
	// A file that can no longer be read is reopened.
	s.f.Close()
	if _, ok := s.runDelay(); !ok {
		t.Fatal("reader did not reopen an unreadable schedstat file")
	}
}

// BenchmarkReadUsage is the whole per-sample cost; a callback takes two.
func BenchmarkReadUsage(b *testing.B) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var r Instance
	defer r.schedstat.close()
	b.ReportAllocs()
	for b.Loop() {
		r.readUsage(true)
	}
}

// BenchmarkReadRunDelay is the schedstat part of readUsage.
func BenchmarkReadRunDelay(b *testing.B) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var s schedstatReader
	defer s.close()
	b.ReportAllocs()
	for b.Loop() {
		s.runDelay()
	}
}
