//go:build linux

package rooms

import (
	"strings"
	"testing"
	"time"

	lua "github.com/yuin/gopher-lua"
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
