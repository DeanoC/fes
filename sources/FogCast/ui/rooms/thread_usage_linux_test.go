//go:build linux

package rooms

import (
	"strings"
	"testing"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// A callback that waits instead of running reports thread CPU well below its
// wall time, which is how a kit overrun tells starvation from slow script work.
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
	if timing.elapsed < 30*time.Millisecond || timing.thread.cpu >= timing.elapsed/2 {
		t.Fatalf("waiting callback: elapsed %s, thread cpu %s", timing.elapsed, timing.thread.cpu)
	}
	if timing.thread.voluntary < 1 {
		t.Fatalf("waiting callback parked its thread without a voluntary switch: %s", timing)
	}
	if !strings.Contains(timing.String(), "thread cpu ") {
		t.Fatalf("timing text: %s", timing)
	}
}
