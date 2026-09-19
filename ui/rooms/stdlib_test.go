package rooms

import (
	"strings"
	"testing"
	"time"

	lua "github.com/yuin/gopher-lua"
)

func countKinds(f Frame) map[OpKind]int {
	out := map[OpKind]int{}
	for _, op := range f.Ops {
		out[op.Kind]++
	}
	return out
}

func focusedHit(f Frame, prefix string) []string {
	var ids []string
	for _, h := range f.Hits {
		if strings.HasPrefix(h.ID, prefix) {
			ids = append(ids, h.ID)
		}
	}
	return ids
}

func TestListWidget(t *testing.T) {
	src := `
local List = require "widgets.list"
items = {}
for i = 1, 30 do items[i] = { title = "Game " .. i } end
list = List.new{ id = "l", x = 100, y = 50, w = 400, h = 200, row_h = 50, items = items }
function on_input(cmd) return list:input(cmd) end
function on_activate(id) list:on_activate(id) end
function draw() list:draw{} end`
	r := newRoom(t, memPack(t, "list", src, nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	f := r.Step(time.Unix(1, 0))
	hits := focusedHit(f, "l:")
	if len(hits) != 5 { // 4 visible rows + one partial overscan row
		t.Fatalf("hits %v", hits)
	}
	if hit, ok := f.HitAt(200, 60); !ok || hit.ID != "l:1" {
		t.Fatalf("hit at first row %+v %v", hit, ok)
	}
	for i := 0; i < 10; i++ {
		if !r.Input("down") {
			t.Fatal("down not consumed")
		}
	}
	if r.Input("select") {
		t.Fatal("select must bubble to the room")
	}
	f = r.Step(time.Unix(2, 0))
	if got := r.L.GetGlobal("list").(*lua.LTable).RawGetString("focus"); got != lua.LNumber(11) {
		t.Fatalf("focus %v", got)
	}
	if _, ok := f.HitAt(200, 60); !ok {
		t.Fatal("scrolled list should still fill the top row")
	}
	if hit, ok := f.HitAt(200, 60); ok && hit.ID != "l:8" {
		t.Fatalf("scroll: top row is %s, want l:8", hit.ID)
	}
	r.Activate("l:9")
	if got := r.L.GetGlobal("list").(*lua.LTable).RawGetString("focus"); got != lua.LNumber(9) {
		t.Fatalf("activate focus %v", got)
	}
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
}

func TestGridWidget(t *testing.T) {
	src := `
local Grid = require "widgets.grid"
items = {}
for i = 1, 18 do items[i] = { title = "Title " .. i } end
grid = Grid.new{ id = "g", x = 0, y = 0, w = 800, h = 480, cell_w = 180, cell_h = 220, gap = 20, items = items }
function on_input(cmd) return grid:input(cmd) end
function draw() grid:draw{} end`
	r := newRoom(t, memPack(t, "grid", src, nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	f := r.Step(time.Unix(1, 0))
	g := r.L.GetGlobal("grid").(*lua.LTable)
	if g.RawGetString("cols") != lua.LNumber(4) || g.RawGetString("rows_visible") != lua.LNumber(2) {
		t.Fatalf("layout cols=%v rows=%v", g.RawGetString("cols"), g.RawGetString("rows_visible"))
	}
	if n := len(focusedHit(f, "g:")); n != 8 {
		t.Fatalf("visible cells %d want 8", n)
	}
	kinds := countKinds(f)
	if kinds[OpText] < 8 || kinds[OpRect] < 9 {
		t.Fatalf("ops %v", kinds)
	}
	r.Input("right")
	r.Input("right")
	r.Input("down")
	r.Input("down")
	r.Input("down")
	f = r.Step(time.Unix(2, 0))
	if g.RawGetString("focus") != lua.LNumber(15) {
		t.Fatalf("focus %v want 15", g.RawGetString("focus"))
	}
	if g.RawGetString("scroll_row") != lua.LNumber(2) {
		t.Fatalf("scroll_row %v want 2", g.RawGetString("scroll_row"))
	}
	if hit, ok := f.HitAt(10, 10); !ok || hit.ID != "g:9" {
		t.Fatalf("top-left after scroll %+v %v", hit, ok)
	}
	// Down into a shorter last row clamps to the final item, then stays.
	r.Input("down")
	r.Input("down")
	if g.RawGetString("focus") != lua.LNumber(18) {
		t.Fatalf("focus %v want 18", g.RawGetString("focus"))
	}
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
}

func TestNodeMapWidget(t *testing.T) {
	src := `
local Map = require "widgets.nodemap"
map = Map.new{
  id = "m",
  nodes = {
    { id = "a", x = 100, y = 300, label = "A" },
    { id = "b", x = 300, y = 300, label = "B" },
    { id = "c", x = 300, y = 100, label = "C" },
    { id = "island", x = 700, y = 100, label = "Island" },
  },
  edges = { {"a", "b"}, {"b", "c"} },
}
function on_input(cmd) return map:input(cmd) end
function on_hover(id) map:on_hover(id) end
function draw() map:draw{} end`
	r := newRoom(t, memPack(t, "map", src, nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	f := r.Step(time.Unix(1, 0))
	if n := len(focusedHit(f, "m:")); n != 4 {
		t.Fatalf("hits %d", n)
	}
	m := r.L.GetGlobal("map").(*lua.LTable)
	focus := func() string { return m.RawGetString("focus").String() }
	if focus() != "a" {
		t.Fatalf("initial focus %s", focus())
	}
	r.Input("right")
	if focus() != "b" {
		t.Fatalf("right from a -> %s", focus())
	}
	r.Input("up")
	if focus() != "c" {
		t.Fatalf("up from b -> %s", focus())
	}
	r.Input("right") // no edge: falls back to nearest node in that direction
	if focus() != "island" {
		t.Fatalf("right from c -> %s", focus())
	}
	r.Input("up") // nothing above: stays
	if focus() != "island" {
		t.Fatalf("up from island -> %s", focus())
	}
	r.Hover("m:a")
	if focus() != "a" {
		t.Fatalf("hover -> %s", focus())
	}
	f = r.Step(time.Unix(2, 0))
	if hit, ok := f.HitAt(100, 300); !ok || hit.ID != "m:a" {
		t.Fatalf("hit at node a %+v %v", hit, ok)
	}
	if kinds := countKinds(f); kinds[OpRect] < 20 {
		t.Fatalf("expected path squares + nodes, ops %v", kinds)
	}
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
}

func TestNodeMapPlayedAndCompletedUseDistinctFills(t *testing.T) {
	src := `
local Map = require "widgets.nodemap"
map = Map.new{
  id = "m",
  nodes = {
    { id = "u", x = 40, y = 40, label = "U" },
    { id = "p", x = 120, y = 40, label = "P", played = true },
    { id = "c", x = 200, y = 40, label = "C", done = true },
    { id = "b", x = 280, y = 40, label = "B", played = true, done = true },
  },
  edges = {},
  radius = 10,
}
function draw()
  map:draw{ node_color = "#ff0000", played_color = "#d4a017", done_color = "#2fb457" }
end`
	r := newRoom(t, memPack(t, "map-hist", src, nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	f := r.Step(time.Unix(1, 0))
	want := map[string]string{
		"u": "#ff0000",
		"p": "#d4a017",
		"c": "#2fb457",
		"b": "#2fb457",
	}
	for id, hex := range want {
		wantC, err := ParseHexColor(hex)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, op := range f.Ops {
			if op.Kind != OpRect || op.W != 20 || op.H != 20 {
				continue
			}
			if FormatHexColor(op.Color) != FormatHexColor(wantC) {
				continue
			}
			hit, ok := f.HitAt(op.X+10, op.Y+10)
			if ok && hit.ID == "m:"+id {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("node %s missing fill %s", id, hex)
		}
	}
	for _, op := range f.Ops {
		if op.Kind != OpRect || op.W != 20 || op.H != 20 {
			continue
		}
		hit, ok := f.HitAt(op.X+10, op.Y+10)
		if !ok || hit.ID != "m:p" {
			continue
		}
		if FormatHexColor(op.Color) == "#2fb457" {
			t.Fatal("played node used completed fill")
		}
	}
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
}
