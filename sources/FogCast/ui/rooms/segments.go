package rooms

import (
	"slices"

	lua "github.com/yuin/gopher-lua"

	"github.com/DeanoC/FogCast/ui/gfx"
)

// segment is a run of a frame's ops and hits recorded by one
// gfx.segment call, with the clip stack and blend mode its first op draws
// under.
type segment struct {
	ops0, ops1   int
	hits0, hits1 int
	at           drawState
}

// drawState is the replayer's state before an op: the clip rectangles
// pushed and not yet popped, and the blend mode.
type drawState struct {
	clips []gfx.Rect
	blend gfx.BlendMode
}

func (s drawState) equal(o drawState) bool {
	return s.blend == o.blend && slices.Equal(s.clips, o.clips)
}

// apply advances s over one op, as tenfoot's drawRoom replays it. A pop with
// nothing pushed is ignored there too.
func (s *drawState) apply(op Op) {
	switch op.Kind {
	case OpClipPush:
		s.clips = append(s.clips, gfx.Rect{X: op.X, Y: op.Y, W: op.W, H: op.H})
	case OpClipPop:
		if len(s.clips) > 0 {
			s.clips = s.clips[:len(s.clips)-1]
		}
	case OpBlend:
		s.blend = op.Blend
	}
}

func (s drawState) clone() drawState {
	return drawState{clips: slices.Clone(s.clips), blend: s.blend}
}

// segments holds the gfx.segment runs of the presented frame and of the
// frame being drawn.
//
// A segment replays only from the frame on screen: presenting a frame adopts
// the segments drawn into it, a draw that keeps the presented frame
// (gfx.reuse) keeps them, and a frame that is not presented, a resize or a
// new instance drops them all. A replay also needs the same clip stack and
// blend mode as when the run was recorded.
type segments struct {
	presented map[string]segment
	building  map[string]segment
	// recording is set while a segment's function runs. A run that skipped
	// an image still loading is not kept (uncacheable): its replay would
	// keep the image missing.
	recording   bool
	uncacheable bool
	// scan is the draw state after building's first scanned ops.
	scan    drawState
	scanned int
	// copied and replayed count this draw's ops taken from the presented
	// frame by gfx.extend and by replays, for the work meter.
	copied, replayed int
	// noReplay makes every gfx.segment run its function. Tests use it to
	// compare replayed frames with fully drawn ones.
	noReplay bool
}

// begin starts a draw.
func (s *segments) begin() {
	clear(s.building)
	s.recording, s.uncacheable = false, false
	s.scan = drawState{blend: gfx.BlendAlpha}
	s.scanned = 0
	s.copied, s.replayed = 0, 0
}

// present adopts the segments drawn into the frame just presented.
func (s *segments) present() {
	s.presented, s.building = s.building, s.presented
	clear(s.building)
}

// drop forgets every segment: the frame on screen no longer matches what
// the script last drew, or the geometry changed.
func (s *segments) drop() {
	clear(s.presented)
	clear(s.building)
}

// stateAt is the draw state after all of ops, scanning only ops added since
// the last call.
func (s *segments) stateAt(ops []Op) drawState {
	for _, op := range ops[s.scanned:] {
		s.scan.apply(op)
	}
	s.scanned = len(ops)
	return s.scan
}

func (s *segments) add(key string, sg segment) {
	if s.building == nil {
		s.building = map[string]segment{}
	}
	s.building[key] = sg
}

// extended moves this draw's segments behind the presented frame gfx.extend
// copied in front of them, recomputes their draw state in the merged frame
// and adds the presented frame's own segments, which keep their places.
func (s *segments) extended(L *lua.LState, f *Frame, prefixOps, prefixHits int) {
	moved := make([]string, 0, len(s.building))
	for key, sg := range s.building {
		sg.ops0 += prefixOps
		sg.ops1 += prefixOps
		sg.hits0 += prefixHits
		sg.hits1 += prefixHits
		s.building[key] = sg
		moved = append(moved, key)
	}
	slices.SortFunc(moved, func(a, b string) int { return s.building[a].ops0 - s.building[b].ops0 })
	s.scan = drawState{blend: gfx.BlendAlpha}
	s.scanned = 0
	for _, key := range moved {
		sg := s.building[key]
		sg.at = s.stateAt(f.Ops[:sg.ops0]).clone()
		s.building[key] = sg
	}
	for key, sg := range s.presented {
		if _, dup := s.building[key]; dup {
			L.RaiseError("gfx.extend: segment %q is already in the presented frame", key)
		}
		s.add(key, sg)
	}
}

// gfx.segment(key, fn, ...) draws fn(...) as a named run of ops and hit
// regions. When the presented frame has a run under key, drawn under the
// same clips and blend mode, the run is copied from it and fn does not run.
// It returns true when it replayed.
func (r *Instance) gfxSegment(L *lua.LState) int {
	f := r.drawing(L, "segment")
	key := L.CheckString(1)
	fn := L.CheckFunction(2)
	if key == "" {
		L.ArgError(1, "segment key required")
	}
	s := &r.seg
	if s.recording {
		L.RaiseError("gfx.segment: segments do not nest")
	}
	if _, dup := s.building[key]; dup {
		L.RaiseError("gfx.segment: key %q drawn twice in one frame", key)
	}
	if len(s.building) >= r.budget.MaxOps {
		L.RaiseError("gfx.segment: frame exceeds %d segments", r.budget.MaxOps)
	}
	at := s.stateAt(f.Ops)
	if r.replaySegment(L, f, key, at) {
		L.Push(lua.LTrue)
		return 1
	}
	at = at.clone()
	ops0, hits0 := len(f.Ops), len(f.Hits)
	s.recording, s.uncacheable = true, false
	defer func() { s.recording = false }()
	top := L.GetTop()
	L.Push(fn)
	for i := 3; i <= top; i++ {
		L.Push(L.Get(i))
	}
	L.Call(top-2, 0)
	if !s.uncacheable {
		s.add(key, segment{ops0: ops0, ops1: len(f.Ops), hits0: hits0, hits1: len(f.Hits), at: at})
	}
	L.Push(lua.LFalse)
	return 1
}

func (r *Instance) replaySegment(L *lua.LState, f *Frame, key string, at drawState) bool {
	s := &r.seg
	sg, ok := s.presented[key]
	if !ok || s.noReplay || !r.frameReady || !sg.at.equal(at) ||
		sg.ops1 > len(r.frame.Ops) || sg.hits1 > len(r.frame.Hits) {
		return false
	}
	ops := r.frame.Ops[sg.ops0:sg.ops1]
	hits := r.frame.Hits[sg.hits0:sg.hits1]
	if len(f.Ops)+len(ops) > r.budget.MaxOps {
		L.RaiseError("gfx.segment: frame exceeds %d draw ops", r.budget.MaxOps)
	}
	if len(f.Hits)+len(hits) > r.budget.MaxOps {
		L.RaiseError("gfx.segment: frame exceeds %d hit regions", r.budget.MaxOps)
	}
	s.add(key, segment{ops0: len(f.Ops), ops1: len(f.Ops) + len(ops), hits0: len(f.Hits), hits1: len(f.Hits) + len(hits), at: sg.at})
	f.Ops = append(f.Ops, ops...)
	f.Hits = append(f.Hits, hits...)
	s.replayed += len(ops)
	return true
}

// outsideSegment rejects calls that change the frame outside a run of ops:
// a replay copies only the run's ops and hits.
func (r *Instance) outsideSegment(L *lua.LState, fn string) {
	if r.seg.recording {
		L.RaiseError("gfx.%s may not be called inside gfx.segment", fn)
	}
}
