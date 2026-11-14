package rooms

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// segmentRoom draws runs named by the globals a_key and b_key and counts
// how often each run's function runs.
const segmentRoom = `
a_key, b_key = "a1", "b1"
runs_a, runs_b, replayed = 0, 0, {}
local function draw_a(x)
  runs_a = runs_a + 1
  gfx.rect(x, 0, 10, 10, "#112233")
  gfx.hit("a", x, 0, 10, 10)
end
local function draw_b()
  runs_b = runs_b + 1
  gfx.text(b_key, 0, 20)
  gfx.hit("b", 0, 20, 10, 10)
end
function draw()
  gfx.clear("#000000")
  gfx.rect(0, 0, 1, 1, "#ffffff")
  replayed = { gfx.segment(a_key, draw_a, 5), gfx.segment(b_key, draw_b) }
  gfx.hit("after", 0, 0, 1, 1)
end
`

func loadSegmentRoom(t *testing.T) *Instance {
	t.Helper()
	r := newRoom(t, memPack(t, "segments", segmentRoom, nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	return r
}

func checkRuns(t *testing.T, r *Instance, expr string) {
	t.Helper()
	if err := r.CheckGlobal(expr); err != nil {
		t.Fatal(err)
	}
}

func TestGfxSegmentReplaysThePresentedRun(t *testing.T) {
	r := loadSegmentRoom(t)
	first := r.Step(time.Unix(1, 0))
	checkRuns(t, r, `runs_a == 1 and runs_b == 1 and replayed[1] == false`)
	second := r.Step(time.Unix(2, 0))
	checkRuns(t, r, `runs_a == 1 and runs_b == 1 and replayed[1] == true and replayed[2] == true`)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("replayed frame differs:\n%+v\n%+v", first, second)
	}
	if &first.Ops[0] == &second.Ops[0] {
		t.Fatal("a replay must build a new frame, not reuse the old one")
	}
	if err := r.CheckGlobal(`(function() b_key = "b2" return true end)()`); err != nil {
		t.Fatal(err)
	}
	third := r.Step(time.Unix(3, 0))
	checkRuns(t, r, `runs_a == 1 and runs_b == 2 and replayed[1] == true and replayed[2] == false`)
	if texts(third) != "b2" || hitIDs(third) != "a b after" {
		t.Fatalf("texts %q hits %q", texts(third), hitIDs(third))
	}
}

func TestGfxSegmentDropsRunsTheScreenDoesNotShow(t *testing.T) {
	r := loadSegmentRoom(t)
	r.Step(time.Unix(1, 0))
	r.Resize(800, 600)
	r.Step(time.Unix(2, 0))
	checkRuns(t, r, `runs_a == 2 and runs_b == 2`)
	r.presentFrame(false)
	r.Step(time.Unix(3, 0))
	checkRuns(t, r, `runs_a == 3 and runs_b == 3`)
	r.Step(time.Unix(4, 0))
	checkRuns(t, r, `runs_a == 3 and runs_b == 3`)
}

func TestGfxSegmentKeepsRunsAcrossReuse(t *testing.T) {
	r := newRoom(t, memPack(t, "segment-reuse", `
runs, n = 0, 0
local function draw_a() runs = runs + 1 gfx.rect(0, 0, 4, 4, "#ffffff") end
function draw()
  n = n + 1
  if n == 2 and gfx.reuse() then return end
  gfx.clear("#000000")
  gfx.segment("a", draw_a)
end
`, nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		r.Step(time.Unix(int64(i), 0))
	}
	checkRuns(t, r, `runs == 1`)
}

func TestGfxSegmentNeedsTheSameClipAndBlend(t *testing.T) {
	r := newRoom(t, memPack(t, "segment-state", `
runs, clipped, blend = 0, true, false
local function draw_a() runs = runs + 1 gfx.rect(0, 0, 4, 4, "#ffffff") end
function draw()
  gfx.clear("#000000")
  if blend then gfx.blend("none") end
  if clipped then gfx.clip(0, 0, 2, 2) end
  gfx.segment("a", draw_a)
  if clipped then gfx.unclip() end
end
`, nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	r.Step(time.Unix(1, 0))
	r.Step(time.Unix(2, 0))
	checkRuns(t, r, `runs == 1`)
	for i, change := range []string{`clipped = false`, `blend = true`} {
		if err := r.CheckGlobal(`(function() ` + change + ` return true end)()`); err != nil {
			t.Fatal(err)
		}
		r.Step(time.Unix(int64(3+i), 0))
		checkRuns(t, r, `runs == `+string(rune('2'+i)))
	}
}

func TestGfxSegmentFollowsExtend(t *testing.T) {
	r := newRoom(t, memPack(t, "segment-extend", `
stage, runs_a, runs_b = 1, 0, 0
local function draw_a() runs_a = runs_a + 1 gfx.text("a", 0, 0) gfx.hit("a", 0, 0, 1, 1) end
local function draw_b() runs_b = runs_b + 1 gfx.text("b", 0, 10) gfx.hit("b", 0, 10, 1, 1) end
function draw()
  if stage == 1 then
    gfx.segment("a", draw_a)
    gfx.paint({ { "c", "#000000" } })
  elseif stage == 2 then
    gfx.blend("none")
    gfx.segment("b", draw_b)
    gfx.extend({})
  else
    gfx.clear("#000000")
    gfx.segment("a", draw_a)
    gfx.blend("none")
    gfx.segment("b", draw_b)
  end
  stage = stage + 1
end
`, nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	r.Step(time.Unix(1, 0))
	extended := r.Step(time.Unix(2, 0))
	if texts(extended) != "a b" || hitIDs(extended) != "a b" {
		t.Fatalf("extended texts %q hits %q", texts(extended), hitIDs(extended))
	}
	whole := r.Step(time.Unix(3, 0))
	checkRuns(t, r, `runs_a == 1 and runs_b == 1`)
	if !reflect.DeepEqual(extended.Ops, whole.Ops) || !reflect.DeepEqual(extended.Hits, whole.Hits) {
		t.Fatalf("replays after extend differ:\n%+v\n%+v", extended, whole)
	}
}

func TestGfxSegmentDoesNotKeepAWaitingImage(t *testing.T) {
	r := newRoom(t, memPack(t, "segment-image", `
runs = 0
local function draw_a()
  runs = runs + 1
  gfx.image(image.cover("game"), 0, 0, 4, 4)
end
function draw()
  gfx.clear("#000000")
  gfx.segment("a", draw_a)
end
`, nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	r.Step(time.Unix(1, 0))
	r.Step(time.Unix(2, 0))
	checkRuns(t, r, `runs == 2`)
}

func TestGfxSegmentRejectsCallsOutsideARun(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"nest", `gfx.segment("y", function() end)`, "segments do not nest"},
		{"clear", `gfx.clear("#000000")`, "gfx.clear may not be called inside gfx.segment"},
		{"paint", `gfx.paint({})`, "gfx.paint may not be called inside gfx.segment"},
		{"extend", `gfx.extend({})`, "gfx.extend may not be called inside gfx.segment"},
		{"reuse", `gfx.reuse()`, "gfx.reuse may not be called inside gfx.segment"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRoom(t, memPack(t, "segment-"+tc.name, `
function draw()
  gfx.clear("#000000")
  ok, err = pcall(gfx.segment, "x", function() `+tc.body+` end)
  after = gfx.segment("z", function() gfx.rect(0, 0, 1, 1, "#ffffff") end) == false
end
`, nil), Options{})
			if err := r.Load(); err != nil {
				t.Fatal(err)
			}
			r.Step(time.Unix(1, 0))
			if err := r.CheckGlobal(`ok == false and after`); err != nil {
				t.Fatal(err)
			}
			if err := r.CheckGlobal(`string.find(tostring(err), "` + tc.want + `", 1, true) ~= nil`); err != nil {
				msg, _ := r.L.GetGlobal("err").(interface{ String() string })
				t.Fatalf("%v: %v", err, msg)
			}
		})
	}
}

func TestGfxSegmentKeyOncePerFrame(t *testing.T) {
	r := newRoom(t, memPack(t, "segment-dup", `
function draw()
  gfx.segment("x", function() end)
  gfx.segment("x", function() end)
end
`, nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	r.Step(time.Unix(1, 0))
	if r.Err() == nil || !strings.Contains(r.Err().Error(), `key "x" drawn twice`) {
		t.Fatalf("err = %v", r.Err())
	}
}

func TestGfxRectsMatchesRect(t *testing.T) {
	r := newRoom(t, memPack(t, "rects", `
mode = "rect"
local list = { 1.5, 2, 3, 4, 10, 20, 30.25, 40 }
function draw()
  if mode == "rect" then
    for i = 1, #list, 4 do gfx.rect(list[i], list[i + 1], list[i + 2], list[i + 3], "#abcdef") end
  else
    gfx.rects(list, "#abcdef")
  end
end
`, nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	one := r.Step(time.Unix(1, 0))
	if err := r.CheckGlobal(`(function() mode = "rects" return true end)()`); err != nil {
		t.Fatal(err)
	}
	batch := r.Step(time.Unix(2, 0))
	if len(one.Ops) != 2 || !reflect.DeepEqual(one.Ops, batch.Ops) {
		t.Fatalf("rects %+v, rect %+v", batch.Ops, one.Ops)
	}

	bad := newRoom(t, memPack(t, "rects-bad", `
function draw() gfx.rects({ 1, 2, 3 }, "#ffffff") end
`, nil), Options{})
	if err := bad.Load(); err != nil {
		t.Fatal(err)
	}
	bad.Step(time.Unix(1, 0))
	if bad.Err() == nil || !strings.Contains(bad.Err().Error(), "x, y, w and h") {
		t.Fatalf("err = %v", bad.Err())
	}
}

func TestWorkMeterCountsInstructions(t *testing.T) {
	r := newRoom(t, memPack(t, "work", `
n = 10
function draw()
  local s = 0
  for i = 1, n do s = s + i end
  gfx.rect(0, 0, 1, 1, "#ffffff")
end
`, nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	w := &workMeter{}
	r.work = w
	r.Step(time.Unix(1, 0))
	if err := r.CheckGlobal(`(function() n = 110 return true end)()`); err != nil {
		t.Fatal(err)
	}
	w.calls = nil
	r.Step(time.Unix(2, 0))
	small := w.calls
	if len(small) != 1 || small[0].kind != "draw" || small[0].ops != 1 {
		t.Fatalf("calls %+v", small)
	}
	w.calls = nil
	r.work = nil
	r.Step(time.Unix(3, 0))
	if len(w.calls) != 0 {
		t.Fatal("a room without a meter counted work")
	}
	r.work = w
	if err := r.CheckGlobal(`(function() n = 10 return true end)()`); err != nil {
		t.Fatal(err)
	}
	w.calls = nil
	r.Step(time.Unix(4, 0))
	// The loop body is a few instructions per iteration: 100 more
	// iterations cost a few hundred more.
	if d := small[0].instructions - w.calls[0].instructions; d < 200 || d > 600 {
		t.Fatalf("100 iterations cost %d instructions (%d vs %d)", d, small[0].instructions, w.calls[0].instructions)
	}
}
