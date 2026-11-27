package rooms

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/release"
	lua "github.com/yuin/gopher-lua"
)

// ErrBudget marks a room failure caused by time rather than by the script's
// logic: a callback deadline that expired, a callback that finished past its
// hard limit, or a room retired after consecutive soft overruns. Test it with
// errors.Is on Instance.Err. Lua errors, load and compile errors and API
// misuse (including the draw-op cap) are not ErrBudget.
var ErrBudget = errors.New("room script exceeded its time budget")

// budgetError is a deadline or hard-limit failure. It is ErrBudget.
type budgetError struct{ msg string }

func (e budgetError) Error() string { return e.msg }

func (e budgetError) Is(target error) bool { return target == ErrBudget }

func budgetErrorf(format string, args ...any) error {
	return budgetError{msg: fmt.Sprintf(format, args...)}
}

var errSoftOverrun = errors.New("script exceeded soft callback budget")

// softOverrun is a callback that finished inside its hard deadline but
// over its soft budget. It is errSoftOverrun and ErrBudget, and also
// carries the timing.
type softOverrun struct {
	budget time.Duration
	timing callTiming
}

func (e softOverrun) Error() string {
	return fmt.Sprintf("script exceeded %s soft callback budget (%s)", e.budget, e.timing)
}

func (e softOverrun) Is(target error) bool { return target == errSoftOverrun || target == ErrBudget }

func (r *Instance) overrun(err error) {
	r.recordFrameOverrun(err)
}

func (r *Instance) recordFrameOverrun(err error) {
	r.frameOverruns++
	r.frameStreak.add(r, err, r.frameOverruns)
	r.retireAfterOverruns(err, r.frameOverruns, &r.frameStreak)
}

func (r *Instance) overrunInput(err error) {
	r.inputOverruns++
	r.inputStreak.add(r, err, r.inputOverruns)
	r.retireAfterOverruns(err, r.inputOverruns, &r.inputStreak)
}

func (r *Instance) retireAfterOverruns(err error, count int, streak *overrunStreak) {
	// Cortex-A9 GC and scheduler pauses can produce occasional slow frames;
	// tolerate several consecutive soft misses while the 4x deadline still
	// terminates genuinely runaway callbacks.
	if count >= maxConsecutiveOverruns {
		r.fail(fmt.Errorf("%w (after %d consecutive overruns; %s)", err, count, streak.summary(r)))
	}
}

func (r *Instance) overrunFrame(err error) Frame {
	r.recordFrameOverrun(err)
	return r.frame
}

func (r *Instance) handleInputCallbackError(err error) {
	if errors.Is(err, errSoftOverrun) {
		r.overrunInput(err)
		return
	}
	r.fail(err)
}

func (r *Instance) callGlobalSoft(name string, budget time.Duration, args ...lua.LValue) error {
	fn, ok := r.L.GetGlobal(name).(*lua.LFunction)
	if !ok {
		return nil
	}
	r.noteFrameBaseline()
	r.workKind(name)
	return r.callSoft(name, fn, budget, 0, args...)
}

// callRetSoft runs the callback called name with one return value. Every
// error it returns, soft or hard, names that callback.
func (r *Instance) callRetSoft(name string, fn *lua.LFunction, budget time.Duration, args ...lua.LValue) (lua.LValue, error) {
	top := r.L.GetTop()
	defer r.L.SetTop(top)
	hardBudget := budget * callbackHardLimitFactor
	timing, err := r.callBudgets(fn, budget, hardBudget, 1, args...)
	if err != nil {
		return lua.LNil, callbackError(name, err)
	}
	v := r.L.Get(-1)
	r.L.Pop(1)
	if err := r.checkElapsed(budget, timing); err != nil {
		if !errors.Is(err, errSoftOverrun) {
			v = lua.LNil
		}
		return v, callbackError(name, err)
	}
	return v, nil
}

// callSoft runs the callback called name. Every error it returns, soft or
// hard, names that callback.
func (r *Instance) callSoft(name string, fn *lua.LFunction, budget time.Duration, nret int, args ...lua.LValue) error {
	timing, err := r.callBudgets(fn, budget, budget*callbackHardLimitFactor, nret, args...)
	if err == nil {
		err = r.checkElapsed(budget, timing)
	}
	if err != nil {
		return callbackError(name, err)
	}
	return nil
}

// checkElapsed compares a callback that returned against its soft budget and
// its hard deadline. gopher-lua checks cancellation only between Lua
// instructions, so a Go call can return after the deadline has passed.
func (r *Instance) checkElapsed(budget time.Duration, timing callTiming) error {
	hard := budget * callbackHardLimitFactor
	if timing.elapsed > hard {
		return r.deadlineMiss(budgetErrorf("script exceeded %s hard callback budget (%s)", hard, timing), timing)
	}
	if timing.elapsed > budget {
		return softOverrun{budget: budget, timing: timing}
	}
	return nil
}

func callbackError(name string, err error) error {
	return fmt.Errorf("%s callback: %w", name, err)
}

// callOptionalInput runs a named input callback when the script defines it.
// An absent callback is not a success and must not clear inputOverruns.
func (r *Instance) callOptionalInput(name string, args ...lua.LValue) {
	fn, ok := r.L.GetGlobal(name).(*lua.LFunction)
	if !ok {
		return
	}
	r.noteInputBaseline()
	r.workKind(name)
	if err := r.callSoft(name, fn, r.budget.Input, 0, args...); err != nil {
		r.handleInputCallbackError(err)
		return
	}
	r.inputOverruns = 0
}

// Hover reports pointer hover over a registered hit region.
func (r *Instance) Hover(id string) {
	if r.fatal != nil || !r.loaded {
		return
	}
	r.callOptionalInput("on_hover", lua.LString(id))
}

// Activate reports a pointer click on a registered hit region.
func (r *Instance) Activate(id string) {
	if r.fatal != nil || !r.loaded {
		return
	}
	r.callOptionalInput("on_activate", lua.LString(id))
}

// Resume is called when the launcher returns to this room after a play
// session or a nested room.
func (r *Instance) Resume() {
	if r.fatal != nil || !r.loaded {
		return
	}
	r.callOptionalInput("on_resume")
}

// Resize updates room.width/room.height after the launcher's content box
// changes (safe-area nudge) and calls on_resize(w, h) when the script has one.
func (r *Instance) Resize(width, height int) {
	if r.L == nil || width <= 0 || height <= 0 {
		return
	}
	if width == r.opts.Width && height == r.opts.Height {
		return
	}
	r.opts.Width, r.opts.Height = width, height
	r.seg.drop()
	r.roomTable.RawSetString("width", lua.LNumber(width))
	r.roomTable.RawSetString("height", lua.LNumber(height))
	r.setHeroTable()
	if r.fatal != nil || !r.loaded {
		return
	}
	r.callOptionalInput("on_resize", lua.LNumber(width), lua.LNumber(height))
}

// SetSessionState is the launcher's current play-session state string.
func (r *Instance) SetSessionState(state string) { r.sessionState = state }

// SetLiveControls projects the launcher's observed display capability into a
// retained room when the active package changes.
func (r *Instance) SetLiveControls(on bool) {
	if r != nil && r.roomTable != nil {
		r.roomTable.RawSetString("live_controls", lua.LBool(on))
	}
}

// SetOverlay sets room.overlay: the room is drawn over a running machine on
// the kit's session plane, so its clear may be translucent and the machine
// shows through it. Host windows never set it.
func (r *Instance) SetOverlay(on bool) {
	if r != nil && r.roomTable != nil {
		r.roomTable.RawSetString("overlay", lua.LBool(on))
	}
}

// coreShown reports whether the room lists a core: every core unless
// ReleaseOnly, then a planned release core or the core the room was opened for, such as a
// running held-back machine's setup room.
func (r *Instance) coreShown(coreID string) bool {
	return !r.opts.ReleaseOnly || release.Shown(coreID, false) || coreID != "" && coreID == r.opts.InitialCoreID
}

// ReleaseOnly reports whether the room was opened hiding held-back cores.
func (r *Instance) ReleaseOnly() bool { return r.opts.ReleaseOnly }

// SetReducedMotion updates room.reduced_motion on a live instance (settings).
func (r *Instance) SetReducedMotion(on bool) {
	if r == nil || r.roomTable == nil {
		return
	}
	r.opts.ReducedMotion = on
	r.roomTable.RawSetString("reduced_motion", lua.LBool(on))
}

// ReducedMotion reports the launcher preference currently exposed to the script.
func (r *Instance) ReducedMotion() bool {
	return r != nil && r.opts.ReducedMotion
}

// TakeActions returns and clears launcher requests made by the script.
func (r *Instance) TakeActions() []Action {
	out := r.actions
	r.actions = nil
	return out
}

// TakeFirmwareInfo returns and clears the latest first-use firmware request.
func (r *Instance) TakeFirmwareInfo() (Action, bool) {
	if r.firmwareInfo == nil {
		return Action{}, false
	}
	out := *r.firmwareInfo
	r.firmwareInfo = nil
	return out, true
}

// CheckGlobal evaluates a Lua boolean expression in the room (tests/diagnostics).
func (r *Instance) CheckGlobal(expr string) error {
	if r.L == nil {
		return errors.New("room is closed")
	}
	fn, err := r.L.LoadString("return (" + expr + ")")
	if err != nil {
		return err
	}
	r.workKind("check")
	v, err := r.callRet(fn, r.budget.Input)
	if err != nil {
		return err
	}
	if !lua.LVAsBool(v) {
		return fmt.Errorf("%s is false", expr)
	}
	return nil
}

// GlobalNumber reads a numeric script global (debug HUD and tests).
func (r *Instance) GlobalNumber(name string) (float64, bool) {
	if r.L == nil {
		return 0, false
	}
	n, ok := r.L.GetGlobal(name).(lua.LNumber)
	return float64(n), ok
}

// CachedGame returns a game the script has seen through library results.
func (r *Instance) CachedGame(id string) (hostclient.Game, bool) {
	g, ok := r.games[id]
	return g, ok
}

// Record cache writes so an older query being converted across frames cannot
// replace a newer refresh or detail result for the same entry.
func (r *Instance) cacheGame(id string, game hostclient.Game) {
	if r.games == nil {
		r.games = make(map[string]hostclient.Game)
	}
	if r.gameVersion == nil {
		r.gameVersion = make(map[string]uint64)
	}
	r.gameEpoch++
	r.games[id] = game
	r.gameVersion[id] = r.gameEpoch
}

// RefreshCachedGames updates host catalog rows the script already saw and
// reclassifies the published destination from its retained candidate set.
// A blocked sibling stays in that set, so a row that becomes viable is
// found on the next refresh. It does not move focus or call the script.
func (r *Instance) RefreshCachedGames(games []hostclient.Game) {
	if r == nil {
		return
	}
	if r.games == nil {
		r.games = map[string]hostclient.Game{}
	}
	for _, g := range games {
		id := strings.TrimSpace(g.ID)
		if id == "" {
			continue
		}
		r.cacheGame(id, g)
	}
	if r.dest.Kind != KindGame {
		return
	}
	source := r.dest.Candidates
	if len(source) == 0 {
		source = r.dest.Matches
	}
	if len(source) == 0 {
		return
	}
	next := append([]hostclient.Game(nil), source...)
	for i, m := range next {
		if g, ok := r.games[m.ID]; ok {
			next[i] = g
		}
	}
	if id := strings.TrimSpace(r.dest.GameID); id != "" {
		if g, ok := r.games[id]; ok {
			r.dest.Label = firstNonEmpty(r.dest.Label, g.Title)
			r.dest.System = firstNonEmpty(r.dest.System, g.System)
		}
	}
	r.dest.storePlay(next, true)
	r.dest.FillCopy()
	r.dest.FillHistory()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func (r *Instance) fail(err error) error {
	if r.fatal == nil {
		r.fatal = fmt.Errorf("room %s: %w", r.pack.ID, err)
		fmt.Fprintf(r.opts.Stderr, "rooms: %v\n", r.fatal)
		// A failed room never runs script again, so stop its async work and
		// let deliver drop anything still in flight.
		r.cancel()
	}
	r.inDraw = false
	r.building = nil
	return r.fatal
}

func (r *Instance) callGlobal(name string, budget time.Duration, args ...lua.LValue) error {
	fn, ok := r.L.GetGlobal(name).(*lua.LFunction)
	if !ok {
		return nil
	}
	r.workKind(name)
	if _, err := r.call(fn, budget, 0, args...); err != nil {
		return callbackError(name, err)
	}
	return nil
}

func (r *Instance) callRet(fn *lua.LFunction, budget time.Duration, args ...lua.LValue) (lua.LValue, error) {
	top := r.L.GetTop()
	defer r.L.SetTop(top)
	if _, err := r.call(fn, budget, 1, args...); err != nil {
		return lua.LNil, err
	}
	v := r.L.Get(-1)
	r.L.Pop(1)
	return v, nil
}

// callTiming is one callback's timing, so a kit overrun shows whether the
// script ran slowly or waited, and for what.
//
// elapsed is the wall time of the whole call: pinning the OS thread, both
// thread samples, the script and the deferred cleanup. The callback deadline
// starts with it and the soft and hard checks compare it, so a stall
// anywhere in the call counts against the room. The only usage reads inside
// it are the two thread samples (one clock_gettime and one getrusage each).
//
// Where the OS reports it (measured), thread covers the script run alone,
// between samples taken just before and just after the script: the script
// thread's own CPU time, context switches, page faults and block reads.
// script is the wall time of that run, from before the first sample to after
// the second, so thread cpu <= script <= elapsed and elapsed minus script was
// spent outside the script run. Voluntary switches are the thread parking (Go
// descheduling, GC, a blocking call); involuntary switches are the kernel
// giving its core to other work. Major faults and blocks read are waits for
// storage.
//
// window (haveWindow) is read only for a callback whose timing is reported:
// one whose deadline expired, or one that returned without error past a soft
// budget its caller reports (callBudgets). It runs from the baseline taken
// before the callback's elapsed window (at step start, or at callback start
// outside a Step) to after elapsed was taken, and gives that window's
// run-queue wait, process-wide GC and Go scheduler wait, with the same
// thread's CPU time over it and what those do not explain.
type callTiming struct {
	elapsed    time.Duration
	script     time.Duration
	thread     threadUsage
	measured   bool
	window     sinceBaseline
	haveWindow bool
}

func (t callTiming) String() string {
	s := "elapsed " + t.elapsed.Round(10*time.Microsecond).String()
	if d := t.detail(); d != "" {
		s += "; " + d
	}
	return s
}

// detail is the script run's usage and the window since the baseline, each
// labelled with what it covers, or "" when neither was read.
func (t callTiming) detail() string {
	var parts []string
	if t.measured {
		parts = append(parts, t.scriptRun())
	}
	if t.haveWindow {
		parts = append(parts, t.window.String())
	}
	return strings.Join(parts, "; ")
}

// scriptRun labels the thread usage with the run it covers.
func (t callTiming) scriptRun() string {
	return "script run " + t.script.Round(10*time.Microsecond).String() + ": " + t.thread.String()
}

// call runs fn under a deadline budget after the call starts. Its timing is
// reported only when the deadline expires, so it passes no soft budget.
func (r *Instance) call(fn *lua.LFunction, budget time.Duration, nret int, args ...lua.LValue) (callTiming, error) {
	return r.callBudgets(fn, 0, budget, nret, args...)
}

// callBudgets runs fn with its deadline at hard after the call starts.
// elapsed is read after runPinned's deferred cleanup has finished. soft is
// the elapsed time past which the caller reports a call that returned
// without error, or 0 when the caller reports timing only for an expired
// deadline. A callback outside a Step takes its usage baseline before
// elapsed starts. The usage after point is read once elapsed is taken, and
// only when the timing will be reported: the deadline expired, or the script
// returned without error past a non-zero soft. A script error before the
// deadline reports the error text alone and reads no after point. The OS
// thread stays locked across all three, so both usage points read the thread
// that ran the script.
func (r *Instance) callBudgets(fn *lua.LFunction, soft, hard time.Duration, nret int, args ...lua.LValue) (callTiming, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r.noteCallbackBaseline()
	start := time.Now()
	timing, expired, err := r.runPinned(fn, start.Add(hard), nret, args...)
	timing.elapsed = time.Since(start)
	if expired || (err == nil && soft > 0 && timing.elapsed > soft) {
		r.noteOverrunWindow(&timing)
	}
	if err != nil {
		if expired {
			return timing, r.deadlineMiss(budgetErrorf("script exceeded %s budget (%s)", hard, timing), timing)
		}
		return timing, errors.New(strings.TrimSpace(err.Error()))
	}
	return timing, nil
}

// runPinned runs fn locked to its OS thread, so both thread samples read the
// thread that ran the script. expired reports that fn failed because the
// deadline passed.
func (r *Instance) runPinned(fn *lua.LFunction, deadline time.Time, nret int, args ...lua.LValue) (timing callTiming, expired bool, err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	L := r.L
	top := L.GetTop()
	ctx, cancel := context.WithDeadline(r.ctx, deadline)
	defer cancel()
	if r.work != nil {
		L.SetContext(r.work.count(ctx))
		defer r.work.finish()
	} else {
		L.SetContext(ctx)
	}
	defer L.RemoveContext()
	L.Push(fn)
	for _, a := range args {
		L.Push(a)
	}
	scriptStart := time.Now()
	before, measured := r.sampleThread()
	err = L.PCall(len(args), nret, nil)
	after, ok := r.sampleThread()
	timing.script = time.Since(scriptStart)
	if ok && measured {
		timing.thread, timing.measured = after.since(before), true
	}
	if err != nil {
		L.SetTop(top)
		return timing, errors.Is(ctx.Err(), context.DeadlineExceeded), err
	}
	return timing, false, nil
}
