package rooms

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/testwait"
	lua "github.com/yuin/gopher-lua"
)

// fakeContention is a sampler whose baseline predates every callback and
// whose current read is fixed, so a deadline miss appends a known summary.
func fakeContention() *contentionSampler {
	s := newContentionSampler(func() (contentionSnapshot, bool) {
		return contentionSnapshot{at: time.Now(), loadavg: "3.10 1.20 0.40 5/90",
			cpuTotal: 300, cpuIdle: 100, haveCPU: true, cpus: 2,
			procs: map[int]procTicks{7: {comm: "fogcast-api", ticks: 160}}}, true
	})
	s.last = contentionSnapshot{at: time.Now().Add(-time.Hour), loadavg: "0 0 0 1/1",
		cpuTotal: 100, cpuIdle: 50, haveCPU: true, cpus: 2,
		procs: map[int]procTicks{7: {comm: "fogcast-api", ticks: 100}}}
	return s
}

// Every room callback's deadline error names the callback, for both the
// context deadline that stops a Lua loop and the check after a Go call that
// returned late, and keeps the budget error wrapped. load, unload and the
// main chunk have no soft budget, so a late Go call there is caught by the
// next Lua instruction's context check.
func TestDeadlineErrorsNameTheirCallback(t *testing.T) {
	const budget = 5 * time.Millisecond
	hard := budget * callbackHardLimitFactor
	type path struct {
		label string // expected callback label
		body  string // Lua installing CB as the slow callback
		soft  bool   // the callback has a soft budget and a late-return check
		run   func(r *Instance)
	}
	step := func(r *Instance) { r.Step(time.Now()) }
	// stepUntilFailed steps until a result delivered from a goroutine has
	// run its callback.
	stepUntilFailed := func(r *Instance) {
		deadline := time.Now().Add(testwait.Timeout)
		for r.Err() == nil && time.Now().Before(deadline) {
			r.Step(time.Now())
			time.Sleep(time.Millisecond)
		}
	}
	paths := []path{
		{"main.lua chunk", "(CB)()", false, nil},
		{"load callback", "function load() (CB)() end", false, nil},
		{"unload callback", "function unload() (CB)() end", false, func(r *Instance) { r.Close() }},
		{"update callback", "update = CB", true, step},
		{"draw callback", "draw = CB", true, step},
		{"on_input callback", "on_input = CB", true, func(r *Instance) { r.Input("confirm") }},
		{"on_hover callback", "on_hover = CB", true, func(r *Instance) { r.Hover("a") }},
		{"on_activate callback", "on_activate = CB", true, func(r *Instance) { r.Activate("a") }},
		{"on_resume callback", "on_resume = CB", true, func(r *Instance) { r.Resume() }},
		{"on_resize callback", "on_resize = CB", true, func(r *Instance) { r.Resize(640, 360) }},
		{"async library.query callback", "function load() library.query({}, CB) end", true, step},
		{"async library.platforms callback", "function load() library.platforms(CB) end", true, step},
		{"async library.collections callback", "function load() library.collections(CB) end", true, step},
		{"async library.game callback", "function load() library.game('g', CB) end", true, step},
		{"async hardware.read callback", "function load() hardware.read(CB) end", true, step},
		{"async hardware.select_expansion callback", "function load() hardware.select_expansion({}, CB) end", true, step},
		{"async kit.cores callback", "function load() kit.cores(CB) end", true, stepUntilFailed},
	}
	for _, kind := range []struct {
		name, cb string
		late     bool
	}{
		{"lua loop stopped at the deadline", "function() while true do end end", false},
		{"go call returned late", "stall", true},
	} {
		for _, p := range paths {
			t.Run(kind.name+"/"+p.label, func(t *testing.T) {
				want := "script exceeded "
				if kind.late && p.soft {
					want += hard.String() + " hard callback budget ("
				}
				src := strings.ReplaceAll(p.body, "CB", kind.cb)
				r := newRoom(t, memPack(t, "names", src, nil), Options{Budget: Budget{Load: hard, Frame: budget, Input: budget}, Local: &fakeLocalCores{}})
				r.contention = fakeContention()
				r.L.SetGlobal("stall", r.L.NewFunction(func(*lua.LState) int { time.Sleep(hard + 40*time.Millisecond); return 0 }))
				err := r.Load()
				if p.run != nil {
					if err != nil {
						t.Fatalf("load: %v", err)
					}
					p.run(r)
					err = r.Err()
				}
				stderr := r.opts.Stderr.(*bytes.Buffer).String()
				if p.label == "unload callback" {
					err = errors.New(stderr)
				}
				if err == nil {
					t.Fatal("no deadline error")
				}
				msg := err.Error()
				if !strings.Contains(msg, p.label+": "+want) {
					t.Fatalf("error does not name %q: %v", p.label, err)
				}
				if !strings.Contains(msg, "; load 3.10 1.20 0.40 5/90; over ") || !strings.Contains(msg, "top fogcast-api ") {
					t.Fatalf("error lacks the contention summary: %v", err)
				}
				if !strings.Contains(stderr, "rooms: room names deadline miss contention: load 3.10") {
					t.Fatalf("log lacks the contention detail: %q", stderr)
				}
				if p.label != "unload callback" {
					assertWrapsBudgetError(t, err)
				}
			})
		}
	}
}

// assertWrapsBudgetError checks the callback name and contention wrap the
// budget error with %w, so errors.Is and errors.As reach it.
func assertWrapsBudgetError(t *testing.T, err error) {
	t.Helper()
	for e := err; e != nil; e = errors.Unwrap(e) {
		if strings.HasPrefix(e.Error(), "script exceeded ") {
			return
		}
	}
	t.Fatalf("budget error is not wrapped: %v", err)
}

// A soft overrun names its callback and keeps errSoftOverrun reachable.
func TestSoftOverrunNamesItsCallback(t *testing.T) {
	r := newRoom(t, memPack(t, "soft-name", "function on_hover() stall() end", nil), Options{Budget: Budget{Load: time.Second, Frame: 10 * time.Millisecond, Input: 10 * time.Millisecond}})
	r.L.SetGlobal("stall", r.L.NewFunction(func(*lua.LState) int { time.Sleep(15 * time.Millisecond); return 0 }))
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	r.contention = nil
	r.Hover("a")
	if r.Err() != nil || r.inputOverruns != 1 {
		t.Fatalf("soft overrun: err %v, %d overruns", r.Err(), r.inputOverruns)
	}
	err := r.callSoft("on_hover", r.L.GetGlobal("on_hover").(*lua.LFunction), 10*time.Millisecond, 0)
	if !errors.Is(err, errSoftOverrun) || !strings.HasPrefix(err.Error(), "on_hover callback: script exceeded 10ms soft callback budget") {
		t.Fatalf("soft overrun error: %v", err)
	}
}

// A script error that is not a deadline also names the callback, and an
// async result names the API that produced it.
func TestScriptErrorsNameTheirCallback(t *testing.T) {
	for _, tc := range []struct {
		body, want string
		run        func(r *Instance)
	}{
		{"function on_input() error('boom') end", "on_input callback: ", func(r *Instance) { r.Input("confirm") }},
		{"function on_resume() error('boom') end", "on_resume callback: ", func(r *Instance) { r.Resume() }},
		{"function load() library.query({}, function() error('boom') end) end", "async library.query callback: ", func(r *Instance) { r.Step(time.Now()) }},
	} {
		r := newRoom(t, memPack(t, "script-error", tc.body, nil), Options{})
		if err := r.Load(); err != nil {
			t.Fatal(err)
		}
		tc.run(r)
		if err := r.Err(); err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("want %q: %v", tc.want, err)
		}
	}
}

func TestAsyncResultSource(t *testing.T) {
	for _, tc := range []struct {
		res  asyncResult
		want string
	}{
		{asyncResult{api: "kit.cores"}, "kit.cores"},
		{asyncResult{libraryQuery: true}, "library.query"},
		{asyncResult{}, "result"},
	} {
		if got := tc.res.source(); got != tc.want {
			t.Fatalf("source %+v = %q, want %q", tc.res, got, tc.want)
		}
	}
}
