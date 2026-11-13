package rooms

import "context"

// workMeter counts the work of each script callback in units that do not
// depend on the machine: Lua instructions run and draw ops recorded. Tests
// set Instance.work to hold a room's worst callbacks under fixed ceilings
// without timing them. Production leaves it nil, so a callback pays one nil
// check.
type workMeter struct {
	kind  string
	steps uint64
	calls []callWork
}

// callWork is one callback's work. Ops are the draw ops the script recorded
// itself; Replayed are ops copied from the presented frame by gfx.segment
// replays and gfx.extend, which run no script.
type callWork struct {
	kind         string
	instructions uint64
	ops          int
	replayed     int
}

// countingContext counts calls to Done. gopher-lua's context main loop
// (mainLoopWithContext) checks Done once before every bytecode instruction,
// so the count is the number of Lua instructions the callback ran.
type countingContext struct {
	context.Context
	n *uint64
}

func (c countingContext) Done() <-chan struct{} {
	*c.n++
	return c.Context.Done()
}

// workKind names the callbacks that follow, for the work meter.
func (r *Instance) workKind(kind string) {
	if r.work != nil {
		r.work.kind = kind
	}
}

// count wraps a callback's context so its instructions are counted.
func (w *workMeter) count(ctx context.Context) context.Context {
	w.steps = 0
	return countingContext{Context: ctx, n: &w.steps}
}

func (w *workMeter) finish() {
	w.calls = append(w.calls, callWork{kind: w.kind, instructions: w.steps})
}

// drew attributes a finished draw's ops to the draw callback just recorded.
func (w *workMeter) drew(built *Frame, copied, replayed int) {
	if len(w.calls) == 0 || built == nil {
		return
	}
	c := &w.calls[len(w.calls)-1]
	c.ops = len(built.Ops) - copied - replayed
	c.replayed = replayed + copied
}
