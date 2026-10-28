package rooms

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/ui/gfx"
)

func manifest(id string) []byte {
	return []byte("id = \"" + id + "\"\ntitle = \"Test " + id + "\"\nmain = \"main.lua\"\n")
}

func memPack(t *testing.T, id, mainLua string, extra map[string]string) Pack {
	t.Helper()
	fsys := fstest.MapFS{
		ManifestName: {Data: manifest(id)},
		"main.lua":   {Data: []byte(mainLua)},
	}
	for name, body := range extra {
		fsys[name] = &fstest.MapFile{Data: []byte(body)}
	}
	p := LoadPackFS(fsys, "mem:"+id)
	if p.Err != nil {
		t.Fatalf("pack %s: %v", id, p.Err)
	}
	return p
}

type fakeServices struct {
	mu          sync.Mutex
	games       []hostclient.Game
	details     map[string]hostclient.Game
	platforms   []hostclient.Platform
	queries     []hostclient.GameListQuery
	queryLimits []int
	err         error
}

func (f *fakeServices) QueryGames(_ context.Context, q hostclient.GameListQuery, max int) ([]hostclient.Game, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, q)
	f.queryLimits = append(f.queryLimits, max)
	games := f.games
	if q.Platform != "" || len(q.CoreIDs) > 0 {
		wanted := make(map[string]struct{}, len(q.CoreIDs))
		for _, coreID := range q.CoreIDs {
			wanted[coreID] = struct{}{}
		}
		games = make([]hostclient.Game, 0)
		for _, game := range f.games {
			if q.Platform != "" && game.System != q.Platform {
				continue
			}
			if len(wanted) > 0 {
				if _, ok := wanted[game.CoreID]; !ok {
					continue
				}
			}
			games = append(games, game)
		}
	}
	if max > 0 && len(games) > max {
		games = games[:max]
	}
	return games, f.err
}

func (f *fakeServices) recorded() []hostclient.GameListQuery {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]hostclient.GameListQuery(nil), f.queries...)
}
func (f *fakeServices) recordedQueryLimits() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.queryLimits...)
}
func (f *fakeServices) Platforms(context.Context) ([]hostclient.Platform, error) {
	return f.platforms, f.err
}
func (f *fakeServices) Collections(context.Context) ([]hostclient.Collection, error) {
	return []hostclient.Collection{{ID: "strategy", Name: "Strategy"}}, f.err
}
func (f *fakeServices) Game(_ context.Context, id string) (hostclient.Game, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.details != nil {
		if g, ok := f.details[id]; ok {
			return g, nil
		}
	}
	for _, g := range f.games {
		if g.ID == id {
			return g, nil
		}
	}
	return hostclient.Game{}, errors.New("not found")
}

func newRoom(t *testing.T, p Pack, opts Options) *Instance {
	t.Helper()
	if opts.Width == 0 {
		opts.Width, opts.Height = 1280, 720
	}
	opts.Stderr = &bytes.Buffer{}
	if opts.Budget.Frame == 0 {
		opts.Budget = Budget{Load: 2 * time.Second, Frame: 2 * time.Second, Input: 2 * time.Second}
	}
	r, err := New(p, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(r.Close)
	return r
}

// stepUntil ticks until pred is true or the deadline passes, giving async
// results time to land.
func stepUntil(t *testing.T, r *Instance, pred func(Frame) bool) Frame {
	t.Helper()
	now := time.Unix(1000, 0)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		now = now.Add(16 * time.Millisecond)
		f := r.Step(now)
		if r.Err() != nil {
			t.Fatalf("room failed: %v", r.Err())
		}
		if pred(f) {
			return f
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("condition not met; last err %v", r.Err())
	return Frame{}
}

func TestDecodeManifest(t *testing.T) {
	m, err := DecodeManifest([]byte("id = \"mario-world\"\ntitle = \"Mushroom Kingdom\"\n[theme]\naccent = \"#ffb830\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Main != "main.lua" || m.Title != "Mushroom Kingdom" || m.Theme.Accent != "#ffb830" {
		t.Fatalf("unexpected manifest %+v", m)
	}
	for name, body := range map[string]string{
		"bad id":        "id = \"Mario World\"\n",
		"unknown field": "id = \"x\"\nbogus = 1\n",
		"escape main":   "id = \"x\"\nmain = \"../evil.lua\"\n",
		"bad colour":    "id = \"x\"\n[theme]\nbackground = \"blue\"\n",
	} {
		if _, err := DecodeManifest([]byte(body)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestLoadDirListsInvalidPacks(t *testing.T) {
	root := t.TempDir()
	good := filepath.Join(root, "good")
	bad := filepath.Join(root, "bad")
	os.MkdirAll(good, 0o755)
	os.MkdirAll(bad, 0o755)
	os.MkdirAll(filepath.Join(root, "notaroom"), 0o755)
	os.WriteFile(filepath.Join(good, ManifestName), manifest("good"), 0o644)
	os.WriteFile(filepath.Join(good, "main.lua"), []byte("function draw() end"), 0o644)
	os.WriteFile(filepath.Join(bad, ManifestName), []byte("id = \"bad\"\nmain = \"missing.lua\"\n"), 0o644)
	packs, err := LoadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(packs) != 2 {
		t.Fatalf("got %d packs, want 2", len(packs))
	}
	idx := NewIndex(packs)
	if idx.ValidCount() != 1 {
		t.Fatalf("valid count %d", idx.ValidCount())
	}
	if p, ok := idx.Find("bad"); !ok || p.Valid() {
		t.Fatalf("bad pack should be listed as invalid: %+v", p)
	}
	if packs, err := LoadDir(filepath.Join(root, "nope")); err != nil || len(packs) != 0 {
		t.Fatalf("missing dir: %v %d", err, len(packs))
	}
}

const goodRoom = `
local color = require "util.color"
local lib = require "lib.helper"
ticks = 0
function load()
  store.set("visits", (store.get("visits", 0)) + 1)
end
function update(dt) ticks = ticks + 1 end
function draw()
  gfx.clear(room.theme.background)
  gfx.rect(10, 20, 100, 50, "#ff0000")
  gfx.rect(0, 0, 10, 10, color.with_alpha("#00ff00", 128))
  gfx.text(lib.greeting(room.title), 40, 40, {size = 24, bold = true, align = "center", color = {255, 255, 255}})
  gfx.clip(0, 0, 640, 360)
  gfx.hit("node:1", 10, 20, 100, 50)
  gfx.unclip()
end
function on_input(cmd)
  if cmd == "select" then session.launch("snes-mario-abc123") return true end
  return false
end
function on_activate(id) rooms.open_library({platform = "snes"}) end
function on_hover(id) hovered = id end
function on_resume() resumed = true end
`

func TestGoodRoomFrameAndActions(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "state.json")
	p := memPack(t, "good", goodRoom, map[string]string{
		"lib/helper.lua": "local M = {}\nfunction M.greeting(s) return 'Hello ' .. s end\nreturn M\n",
	})
	r := newRoom(t, p, Options{StorePath: storePath})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	f := r.Step(time.Unix(1, 0))
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	if !f.HasClear {
		t.Error("clear not recorded")
	}
	kinds := []OpKind{}
	for _, op := range f.Ops {
		kinds = append(kinds, op.Kind)
	}
	want := []OpKind{OpRect, OpRect, OpText, OpClipPush, OpClipPop}
	if len(kinds) != len(want) {
		t.Fatalf("ops %v want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("ops %v want %v", kinds, want)
		}
	}
	if f.Ops[0].Color != gfx.RGB(255, 0, 0) || f.Ops[1].Color != gfx.RGBA(0, 255, 0, 128) {
		t.Errorf("colors %+v %+v", f.Ops[0].Color, f.Ops[1].Color)
	}
	text := f.Ops[2]
	if text.Text != "Hello Test good" || text.Size != 24 || !text.Bold || text.Align != "center" {
		t.Errorf("text op %+v", text)
	}
	if hit, ok := f.HitAt(50, 40); !ok || hit.ID != "node:1" {
		t.Errorf("hit %+v %v", hit, ok)
	}
	if _, ok := f.HitAt(5, 5); ok {
		t.Error("unexpected hit outside region")
	}

	if !r.Input("select") {
		t.Error("select should be handled")
	}
	if r.Input("back") {
		t.Error("back should not be handled")
	}
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != "snes-mario-abc123" || acts[0].PackageID != "" || acts[0].ExpectedPackageID != "" {
		t.Fatalf("actions %+v", acts)
	}
	r.Activate("node:1")
	acts = r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionOpenLibrary || acts[0].Platform != "snes" {
		t.Fatalf("actions %+v", acts)
	}
	r.Hover("node:1")
	if got := r.L.GetGlobal("hovered").String(); got != "node:1" {
		t.Errorf("hovered %q", got)
	}
	r.Resume()
	if r.L.GetGlobal("resumed") != lua_true() {
		t.Error("on_resume not called")
	}
	r.Close()

	r2 := newRoom(t, p, Options{StorePath: storePath})
	if err := r2.Load(); err != nil {
		t.Fatal(err)
	}
	r2.Close()
	data, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\"visits\": 2") {
		t.Errorf("store contents %s", data)
	}
}

func TestRoomLaunchCanSelectPackageWithCompareAndSwap(t *testing.T) {
	current, next := strings.Repeat("a", 64), strings.Repeat("b", 64)
	script := fmt.Sprintf(`function on_input(cmd)
  if cmd == "select" then session.launch("zx81-workshop", %q, %q) return true end
  return false
end`, next, current)
	r := newRoom(t, memPack(t, "package-launch", script, nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	if !r.Input("select") {
		t.Fatal("select was not handled")
	}
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != "zx81-workshop" || acts[0].PackageID != next || acts[0].ExpectedPackageID != current {
		t.Fatalf("package launch action %+v", acts)
	}
	r.Close()
}

func TestStoreWriteFailureLogsOnceAndKeepsRoom(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parent, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "example.fes-cores.state.json")
	r := newRoom(t, memPack(t, "persist", `
function load()
  store.set("package_id", "abc")
end
function draw() end
`, nil), Options{StorePath: path})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	r.Step(time.Unix(10, 0))
	r.Step(time.Unix(12, 0))
	r.Close()
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	text := r.opts.Stderr.(*bytes.Buffer).String()
	if strings.Count(text, "could not save room state") != 1 {
		t.Fatalf("log count:\n%s", text)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("state file was created under an unwritable parent")
	}
}

func TestDeadlineStopsRunawayScript(t *testing.T) {
	for _, src := range []string{
		"function update(dt) while true do end end\nfunction draw() end",
		"function update(dt) end\nfunction draw() while true do end end",
	} {
		r := newRoom(t, memPack(t, "loop", src, nil), Options{Budget: Budget{Load: time.Second, Frame: 30 * time.Millisecond, Input: 30 * time.Millisecond}})
		if err := r.Load(); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		f := r.Step(time.Unix(1, 0))
		if time.Since(start) > 2*time.Second {
			t.Fatalf("deadline not enforced: %v", time.Since(start))
		}
		if len(f.Ops) != 0 || r.Err() == nil || !strings.Contains(r.Err().Error(), "budget") {
			t.Fatalf("frame %+v err %v", f, r.Err())
		}
		if got := r.Step(time.Unix(2, 0)); len(got.Ops) != 0 {
			t.Error("failed room must not draw")
		}
	}
}

func TestTransientFrameOverrunsKeepPreviousFrame(t *testing.T) {
	r := newRoom(t, memPack(t, "transient", "function draw() gfx.clear('#123456') end", nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	previous := r.Step(time.Unix(1, 0))
	if !previous.HasClear {
		t.Fatal("initial frame was not completed")
	}
	got := r.overrunFrame(errSoftOverrun)
	if r.Err() != nil {
		t.Fatalf("one transient overrun failed the room: %v", r.Err())
	}
	if len(got.Ops) != len(previous.Ops) || got.Clear != previous.Clear || !got.HasClear {
		t.Fatalf("overrun did not retain previous frame: got %+v, want %+v", got, previous)
	}
	for i := 1; i < maxConsecutiveOverruns-1; i++ {
		r.overrun(errSoftOverrun)
	}
	if r.Err() != nil {
		t.Fatalf("soft overruns failed too early: %v", r.Err())
	}
	r.overrun(errSoftOverrun)
	if r.Err() == nil || !strings.Contains(r.Err().Error(), "overruns") {
		t.Fatalf("sustained overruns should fail the room, got %v", r.Err())
	}
}

func TestGfxReuseKeepsThePresentedFrame(t *testing.T) {
	r := newRoom(t, memPack(t, "reuse", `
function draw()
  if gfx.reuse() then
    gfx.clear("#ffffff")
    return
  end
  gfx.clear("#123456")
end
`, nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	first := r.Step(time.Unix(1, 0))
	if got := FormatHexColor(first.Clear); got != "#123456" || !first.HasClear {
		t.Fatalf("first draw did not paint: %+v", first.Clear)
	}
	second := r.Step(time.Unix(2, 0))
	if got := FormatHexColor(second.Clear); got != "#123456" {
		t.Fatalf("reuse replaced the presented frame with %s", got)
	}
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
}

func TestGfxExtendAppendsToThePresentedFrame(t *testing.T) {
	r := newRoom(t, memPack(t, "extend", `
stage = 1
function draw()
  if stage == 1 then
    gfx.paint({{"c", "#112233"}, {"t", "upper", 1, 1, 18, "#ffffff", 0, false, "left"}})
    stage = 2
    return
  end
  gfx.extend({{"t", "lower", 1, 30, 18, "#ffffff", 0, false, "left"}})
end
`, nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	upper := r.Step(time.Unix(1, 0))
	if FormatHexColor(upper.Clear) != "#112233" || !frameHas(upper, "upper") || frameHas(upper, "lower") {
		t.Fatalf("upper frame = %+v", upper)
	}
	both := r.Step(time.Unix(2, 0))
	if FormatHexColor(both.Clear) != "#112233" || !frameHas(both, "upper") || !frameHas(both, "lower") {
		t.Fatalf("extend dropped a half: clear %s upper %v lower %v", FormatHexColor(both.Clear), frameHas(both, "upper"), frameHas(both, "lower"))
	}
}

func TestGfxExtendKeepsDrawingRecordedBeforeIt(t *testing.T) {
	r := newRoom(t, memPack(t, "extend-after-draw", `
stage = 1
function draw()
  if stage == 1 then
    gfx.paint({{"c", "#112233"}, {"t", "kept", 1, 1, 18, "#ffffff", 0, false, "left"}, {"h", "old", 1, 1, 8, 8}})
    stage = 2
    return
  end
  if stage == 2 then
    gfx.text("before", 1, 20)
    gfx.hit("before", 1, 20, 8, 8)
    gfx.extend({{"t", "added", 1, 40, 18, "#ffffff", 0, false, "left"}, {"h", "added", 1, 40, 8, 8}})
    gfx.extend({{"t", "again", 1, 60, 18, "#ffffff", 0, false, "left"}})
    stage = 3
    return
  end
  gfx.clear("#abcdef")
  gfx.rect(2, 2, 3, 4, "#ffffff")
  gfx.extend({{"c", "#445566"}, {"t", "late", 1, 80, 18, "#ffffff", 0, false, "left"}})
end
`, nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	first := r.Step(time.Unix(1, 0))
	if FormatHexColor(first.Clear) != "#112233" || texts(first) != "kept" || hitIDs(first) != "old" {
		t.Fatalf("presented frame = clear %s texts %q hits %q", FormatHexColor(first.Clear), texts(first), hitIDs(first))
	}
	appended := r.Step(time.Unix(2, 0))
	if FormatHexColor(appended.Clear) != "#112233" {
		t.Fatalf("clear = %s, want the presented clear", FormatHexColor(appended.Clear))
	}
	if got := texts(appended); got != "kept before added again" {
		t.Fatalf("texts = %q", got)
	}
	if got := hitIDs(appended); got != "old before added" {
		t.Fatalf("hits = %q", got)
	}
	replaced := r.Step(time.Unix(3, 0))
	if FormatHexColor(replaced.Clear) != "#445566" {
		t.Fatalf("clear = %s, want the clear inside extend", FormatHexColor(replaced.Clear))
	}
	if got := texts(replaced); got != "kept before added again late" {
		t.Fatalf("texts = %q", got)
	}
	var rect bool
	for _, op := range replaced.Ops {
		if op.Kind == OpRect && op.X == 2 && op.Y == 2 && op.W == 3 && op.H == 4 {
			rect = true
		}
	}
	if !rect {
		t.Fatal("rect drawn before extend was dropped")
	}
}

func texts(f Frame) string {
	var parts []string
	for _, op := range f.Ops {
		if op.Kind == OpText {
			parts = append(parts, op.Text)
		}
	}
	return strings.Join(parts, " ")
}

func hitIDs(f Frame) string {
	var parts []string
	for _, hit := range f.Hits {
		parts = append(parts, hit.ID)
	}
	return strings.Join(parts, " ")
}

func TestSlowPaintIsPresentedAndReuseClearsTheStreak(t *testing.T) {
	r := newRoom(t, memPack(t, "slow-paint", `
function draw()
  if mode == "paint" then
    gfx.paint({{"c", "#333333"}, {"t", "open", 1, 1, 18, "#ffffff", 0, false, "left"}})
    wait()
    return
  end
  if mode == "reuse" then
    if gfx.reuse() then return end
  end
  if mode == "slow" then wait() end
  gfx.clear("#111111")
end
`, nil), Options{Budget: Budget{Load: time.Second, Frame: 20 * time.Millisecond, Input: 20 * time.Millisecond}})
	r.L.SetGlobal("wait", r.L.NewFunction(func(*lua.LState) int { time.Sleep(30 * time.Millisecond); return 0 }))
	r.L.SetGlobal("mode", lua.LString("fast"))
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	fast := r.Step(time.Unix(1, 0))
	if FormatHexColor(fast.Clear) != "#111111" {
		t.Fatalf("fast clear %s", FormatHexColor(fast.Clear))
	}
	r.L.SetGlobal("mode", lua.LString("slow"))
	kept := r.Step(time.Unix(2, 0))
	if r.Err() != nil || FormatHexColor(kept.Clear) != "#111111" || r.frameOverruns != 1 {
		t.Fatalf("non-paint slow draw presented a new frame: clear %s overruns %d err %v", FormatHexColor(kept.Clear), r.frameOverruns, r.Err())
	}
	r.L.SetGlobal("mode", lua.LString("paint"))
	painted := r.Step(time.Unix(3, 0))
	if r.Err() != nil || FormatHexColor(painted.Clear) != "#333333" || !frameHas(painted, "open") || r.frameOverruns != 2 {
		t.Fatalf("slow paint was discarded: clear %s overruns %d err %v text %v", FormatHexColor(painted.Clear), r.frameOverruns, r.Err(), frameHas(painted, "open"))
	}
	r.L.SetGlobal("mode", lua.LString("reuse"))
	reused := r.Step(time.Unix(4, 0))
	if r.Err() != nil || FormatHexColor(reused.Clear) != "#333333" || r.frameOverruns != 0 {
		t.Fatalf("reuse did not keep the paint or clear the streak: clear %s overruns %d err %v", FormatHexColor(reused.Clear), r.frameOverruns, r.Err())
	}
}

func TestEightSlowPaintsRetireTheRoom(t *testing.T) {
	r := newRoom(t, memPack(t, "eight-paints", `
n = 0
function draw()
  n = n + 1
  gfx.paint({{"c", "#445566"}, {"t", tostring(n), 1, 1, 18, "#ffffff", 0, false, "left"}})
  wait()
end
`, nil), Options{Budget: Budget{Load: time.Second, Frame: 20 * time.Millisecond, Input: 20 * time.Millisecond}})
	r.L.SetGlobal("wait", r.L.NewFunction(func(*lua.LState) int { time.Sleep(30 * time.Millisecond); return 0 }))
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxConsecutiveOverruns-1; i++ {
		f := r.Step(time.Unix(int64(i+1), 0))
		if r.Err() != nil || !frameHas(f, fmt.Sprintf("%d", i+1)) {
			t.Fatalf("slow paint %d retired early: err %v", i+1, r.Err())
		}
	}
	r.Step(time.Unix(int64(maxConsecutiveOverruns), 0))
	if r.Err() == nil || !strings.Contains(r.Err().Error(), "overruns") {
		t.Fatalf("eight slow paints left the room running: %v", r.Err())
	}
}

func TestLiveSlowDrawsAreToleratedThenFail(t *testing.T) {
	r := newRoom(t, memPack(t, "live-slow", "function draw() if slow then wait() end end", nil), Options{
		Budget: Budget{Load: time.Second, Frame: 8 * time.Millisecond, Input: 8 * time.Millisecond},
	})
	r.L.SetGlobal("wait", r.L.NewFunction(func(*lua.LState) int { time.Sleep(12 * time.Millisecond); return 0 }))
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	r.L.SetGlobal("slow", lua.LTrue)
	for i := 0; i < maxConsecutiveOverruns-1; i++ {
		r.Step(time.Unix(int64(i+1), 0))
		if r.Err() != nil {
			t.Fatalf("isolated soft overrun failed room at %d: %v", i, r.Err())
		}
	}
	r.L.SetGlobal("slow", lua.LFalse)
	r.Step(time.Unix(20, 0))
	r.L.SetGlobal("slow", lua.LTrue)
	for i := 0; i < maxConsecutiveOverruns; i++ {
		r.Step(time.Unix(int64(21+i), 0))
	}
	if r.Err() == nil || !strings.Contains(r.Err().Error(), "overruns") {
		t.Fatalf("sustained live slow draws did not fail room: %v", r.Err())
	}
}

func TestLiveSlowInputsFailDespiteSuccessfulSteps(t *testing.T) {
	r := newRoom(t, memPack(t, "live-slow-input", "function on_input(cmd) wait(); return false end\nfunction draw() end", nil), Options{
		Budget: Budget{Load: time.Second, Frame: 8 * time.Millisecond, Input: 8 * time.Millisecond},
	})
	r.L.SetGlobal("wait", r.L.NewFunction(func(*lua.LState) int { time.Sleep(12 * time.Millisecond); return 0 }))
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxConsecutiveOverruns; i++ {
		if r.Input("left") {
			t.Fatalf("slow input %d lost its false result", i)
		}
		if i < maxConsecutiveOverruns-1 {
			r.Step(time.Unix(int64(i+1), 0))
			if r.Err() != nil {
				t.Fatalf("successful step retired room after input %d: %v", i, r.Err())
			}
		}
	}
	if r.Err() == nil || !strings.Contains(r.Err().Error(), "overruns") {
		t.Fatalf("repeated slow inputs did not fail room: %v", r.Err())
	}
}

func TestAbsentHoverDoesNotResetInputOverruns(t *testing.T) {
	r := newRoom(t, memPack(t, "slow-input-no-hover", "function on_input(cmd) wait(); return false end\nfunction draw() end", nil), Options{
		Budget: Budget{Load: time.Second, Frame: 8 * time.Millisecond, Input: 8 * time.Millisecond},
	})
	r.L.SetGlobal("wait", r.L.NewFunction(func(*lua.LState) int { time.Sleep(12 * time.Millisecond); return 0 }))
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxConsecutiveOverruns; i++ {
		if r.Input("left") {
			t.Fatalf("slow input %d lost its false result", i)
		}
		r.Hover("gap")
		r.Activate("gap")
		r.Resume()
	}
	if r.Err() == nil || !strings.Contains(r.Err().Error(), "overruns") {
		t.Fatalf("absent optional callbacks reset input overruns: %v", r.Err())
	}

	reset := newRoom(t, memPack(t, "slow-input-hover-resets", "function on_input(cmd) wait(); return false end\nfunction on_hover(id) end\nfunction draw() end", nil), Options{
		Budget: Budget{Load: time.Second, Frame: 8 * time.Millisecond, Input: 8 * time.Millisecond},
	})
	reset.L.SetGlobal("wait", reset.L.NewFunction(func(*lua.LState) int { time.Sleep(12 * time.Millisecond); return 0 }))
	if err := reset.Load(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxConsecutiveOverruns; i++ {
		if reset.Input("left") {
			t.Fatalf("slow input %d lost its false result", i)
		}
		reset.Hover("kept")
		if reset.Err() != nil {
			t.Fatalf("successful hover failed room at %d: %v", i, reset.Err())
		}
	}
}

func TestSoftCallbacksRestoreLuaStack(t *testing.T) {
	r := newRoom(t, memPack(t, "stack", "function on_input(cmd) if slow_input then wait() end; return true end\nfunction draw() if slow_draw then wait() end end\nfunction result_cb(v,e) if slow_result then wait() end end", nil), Options{
		Budget: Budget{Load: time.Second, Frame: 8 * time.Millisecond, Input: 8 * time.Millisecond},
	})
	r.L.SetGlobal("wait", r.L.NewFunction(func(*lua.LState) int { time.Sleep(12 * time.Millisecond); return 0 }))
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	cb := r.L.GetGlobal("result_cb").(*lua.LFunction)
	for i := 0; i < 2; i++ {
		base := r.L.GetTop()
		r.L.SetGlobal("slow_input", lua.LTrue)
		r.Input("left")
		r.L.SetGlobal("slow_input", lua.LFalse)
		if got := r.L.GetTop(); got != base {
			t.Fatalf("input stack top %d, want %d", got, base)
		}
		r.L.SetGlobal("slow_draw", lua.LTrue)
		r.Step(time.Unix(int64(i+1), 0))
		r.L.SetGlobal("slow_draw", lua.LFalse)
		r.Input("reset")
		if got := r.L.GetTop(); got != base {
			t.Fatalf("draw stack top %d, want %d", got, base)
		}
		r.L.SetGlobal("slow_result", lua.LTrue)
		r.deliver(asyncResult{cb: cb})
		r.Step(time.Unix(int64(i+10), 0))
		r.L.SetGlobal("slow_result", lua.LFalse)
		r.Input("reset")
		if got := r.L.GetTop(); got != base {
			t.Fatalf("result stack top %d, want %d", got, base)
		}
	}
	if r.Err() != nil {
		t.Fatalf("soft callback overruns unexpectedly failed: %v", r.Err())
	}
}

func TestCancelledPCallLeavesRoomUsable(t *testing.T) {
	r := newRoom(t, memPack(t, "cancel-call", "function draw() gfx.clear('#123456') end", nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	base := r.L.GetTop()
	canceled, cancel := context.WithCancel(r.ctx)
	cancel()
	r.ctx = canceled
	if err := r.callGlobalSoft("draw", time.Second); err == nil {
		t.Fatal("expected canceled PCall error")
	}
	r.ctx = context.Background()
	if got := r.L.GetTop(); got != base {
		t.Fatalf("stack top after cancellation %d, want %d", got, base)
	}
	if f := r.Step(time.Unix(1, 0)); !f.HasClear || r.Err() != nil {
		t.Fatalf("room did not recover after canceled PCall: frame=%+v err=%v", f, r.Err())
	}
}

func TestSoftFrameOverrunKeepsRoomAlive(t *testing.T) {
	r := newRoom(t, memPack(t, "slow-frame", "function draw() if slow then wait() end; gfx.clear('#123456') end\nfunction on_input(cmd) if slow then wait() end; return false end", nil), Options{
		Budget: Budget{Load: time.Second, Frame: 10 * time.Millisecond, Input: 10 * time.Millisecond},
	})
	r.L.SetGlobal("wait", r.L.NewFunction(func(*lua.LState) int {
		time.Sleep(15 * time.Millisecond)
		return 0
	}))
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	previous := r.Step(time.Unix(1, 0))
	r.L.SetGlobal("slow", lua.LBool(true))
	got := r.Step(time.Unix(2, 0))
	if r.Err() != nil {
		t.Fatalf("soft frame overrun failed the room: %v", r.Err())
	}
	if !got.HasClear || got.Clear != previous.Clear {
		t.Fatalf("soft overrun did not return previous frame: %+v", got)
	}
	if r.Input("left") {
		t.Fatal("overrunning input callback should preserve its false result")
	}
	if r.Err() != nil {
		t.Fatalf("soft input overrun failed the room: %v", r.Err())
	}
}

func TestDefaultRoomBudgets(t *testing.T) {
	b := DefaultBudget()
	if b.Frame != 12*time.Millisecond || b.Input != 8*time.Millisecond || b.Load != 250*time.Millisecond {
		t.Fatalf("unexpected default budgets: %+v", b)
	}
}

func TestSyntaxAndRuntimeErrors(t *testing.T) {
	p := memPack(t, "syntax", "function draw( end", nil)
	r := newRoom(t, p, Options{})
	if err := r.Load(); err == nil || !strings.Contains(err.Error(), "compile") {
		t.Fatalf("expected compile error, got %v", err)
	}
	p = memPack(t, "runtime", "function draw() local x = nil; x.y = 1 end", nil)
	r = newRoom(t, p, Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	r.Step(time.Unix(1, 0))
	if r.Err() == nil {
		t.Fatal("expected runtime error")
	}
}

func TestSandboxRemovesDangerousGlobals(t *testing.T) {
	src := `
assert(os == nil, "os leaked")
assert(io == nil, "io leaked")
assert(dofile == nil, "dofile leaked")
assert(loadstring == nil, "loadstring leaked")
assert(load == nil, "load leaked")
assert(debug == nil, "debug leaked")
assert(package == nil, "package leaked")
assert(string.dump == nil, "string.dump leaked")
assert(type(require) == "function")
function draw() end`
	r := newRoom(t, memPack(t, "sandbox", src, nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"require escape": `require "../secret"`,
		"require slash":  `require "lib/x"`,
		"image escape":   `image.load("../../etc/passwd")`,
		"image absolute": `image.load("/etc/passwd")`,
	} {
		r := newRoom(t, memPack(t, "esc", body+"\nfunction draw() end", nil), Options{})
		if err := r.Load(); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestGfxOutsideDrawAndOpCap(t *testing.T) {
	r := newRoom(t, memPack(t, "outside", `gfx.rect(0,0,1,1,"#fff")`+"\nfunction draw() end", nil), Options{})
	if err := r.Load(); err == nil || !strings.Contains(err.Error(), "inside draw") {
		t.Fatalf("expected draw-only error, got %v", err)
	}
	r = newRoom(t, memPack(t, "cap", "function draw() for i=1,100 do gfx.rect(i,0,1,1,'#fff') end end", nil), Options{Budget: Budget{Load: time.Second, Frame: time.Second, Input: time.Second, MaxOps: 50}})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	r.Step(time.Unix(1, 0))
	if r.Err() == nil || !strings.Contains(r.Err().Error(), "draw ops") {
		t.Fatalf("expected op cap error, got %v", r.Err())
	}
}

func TestLibraryQueryAndCovers(t *testing.T) {
	svc := &fakeServices{
		games: []hostclient.Game{
			{ID: "snes-mario", Title: "Super Mario World", System: "snes", State: "available", RootOnline: true, Launchable: true, Collections: []string{"fav"}},
			{ID: "snes-zelda", Title: "Zelda", System: "snes", State: "missing", RootOnline: true, Launchable: true},
		},
		platforms: []hostclient.Platform{{ID: "colecovision", Label: "ColecoVision", Tags: []string{"vdp:tms9918"}}},
	}
	src := `
games = nil
plats = nil
cover = nil
function load()
  library.query({platform = "snes", sort = "title", limit = 10}, function(list, err)
    assert(err == nil, err)
    games = list
    cover = image.cover(list[1].id)
  end)
  library.platforms(function(list) plats = list end)
end
function draw()
  if cover then gfx.image(cover, 0, 0, 200, 300) end
end`
	r := newRoom(t, memPack(t, "lib", src, nil), Options{Services: svc})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(Frame) bool {
		return r.L.GetGlobal("games").Type().String() == "table" && r.L.GetGlobal("plats").Type().String() == "table"
	})
	if qs := svc.recorded(); len(qs) != 1 || qs[0].Platform != "snes" || qs[0].Sort != "title" {
		t.Fatalf("queries %+v", qs)
	}
	if g, ok := r.CachedGame("snes-zelda"); !ok || g.Title != "Zelda" {
		t.Fatalf("cached game %+v %v", g, ok)
	}
	f := r.Step(time.Unix(5, 0))
	if len(f.Ops) != 0 {
		t.Fatalf("cover must not draw before it is ready: %+v", f.Ops)
	}
	ids := r.TakeCoverRequests()
	if len(ids) != 1 || ids[0] != "snes-mario" {
		t.Fatalf("cover requests %v", ids)
	}
	if again := r.TakeCoverRequests(); len(again) != 0 {
		t.Fatalf("cover requested twice: %v", again)
	}
	img := image.NewRGBA(image.Rect(0, 0, 4, 6))
	r.DeliverCover("snes-mario", img, nil)
	f = stepUntil(t, r, func(f Frame) bool { return len(f.Ops) == 1 })
	if f.Ops[0].Kind != OpImage || f.Ops[0].Image != "cover:snes-mario" || f.Ops[0].W != 200 {
		t.Fatalf("image op %+v", f.Ops[0])
	}
	if r.Images()["cover:snes-mario"] != img {
		t.Error("decoded image not exposed for upload")
	}
	// The Lua view of a game carries launch admission.
	L := r.L
	L.SetContext(context.Background())
	if err := L.DoString(`assert(games[1].launchable == true); assert(games[2].launchable == false); assert(games[2].launch_block == "source_offline"); assert(games[1].collections[1] == "fav"); assert(plats[1].tags[1] == "vdp:tms9918")`); err != nil {
		t.Fatal(err)
	}
}

func TestDestinationClassifyAndPublish(t *testing.T) {
	svc := &fakeServices{
		games: []hostclient.Game{
			{ID: "nes-smb-usa", Title: "Super Mario Bros.", System: "nes", State: "available", RootOnline: true, Launchable: true},
			{ID: "nes-smb-jp", Title: "Super Mario Bros.", System: "nes", State: "available", RootOnline: true, Launchable: true},
		},
	}
	src := `
result = nil
function load()
  library.query({ q = "Super Mario Bros." }, function(games, err)
    result = destination.classify(games, { q = "Super Mario Bros." })
    destination.set{ kind = "game", label = "Super Mario Bros.", query = "Super Mario Bros.", matches = games }
  end)
end
function draw() end`
	r := newRoom(t, memPack(t, "dest", src, nil), Options{Services: svc})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(Frame) bool { return r.L.GetGlobal("result").Type().String() == "table" })
	d := r.Destination()
	if d.Availability != AvailNeedsChoice || d.Confirm() != ConfirmChoose {
		t.Fatalf("dest %+v", d)
	}
	if d.Status != "Several editions match. Choose one." || d.Action != "Choose an edition." {
		t.Fatalf("copy %q %q", d.Status, d.Action)
	}
	if err := r.CheckGlobal("result.state == 'needs_choice' and #result.matches == 2"); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshCachedGamesMakesFroggerReadyWithoutMovingFocus(t *testing.T) {
	svc := &fakeServices{
		games: []hostclient.Game{
			{ID: "fpga-frogger", Title: "Frogger", System: "fpga", State: "available", RootOnline: true, Launchable: true, FirmwareRequired: true},
		},
	}
	src := `
function load()
  library.query({ q = "Frogger" }, function(games, err)
    destination.set{ kind = "game", label = "Frogger", query = "Frogger", platform = "fpga", matches = games }
  end)
end
function draw() gfx.rect(0,0,10,10,'#fff') end`
	r := newRoom(t, memPack(t, "frogger", src, nil), Options{Services: svc})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(Frame) bool { return r.Destination().Availability == AvailUnavailable })
	d := r.Destination()
	if d.Confirm() != ConfirmImportFirmware || d.Action != "Import Coleco BIOS." {
		t.Fatalf("unavailable dest %+v", d)
	}
	label := d.Label
	ready := svc.games[0]
	ready.FirmwareReady = true
	r.RefreshCachedGames([]hostclient.Game{ready})
	got := r.Destination()
	if got.Availability != AvailReady || got.Confirm() != ConfirmLaunch || got.Action != "Play" || got.Label != label {
		t.Fatalf("refreshed dest %+v", got)
	}
}

func TestBlockedSiblingRefreshBecomesReadyOrAChoice(t *testing.T) {
	src := `
result = nil
function load()
  local games = {
    { id = "fpga-data-storm", title = "Data Storm", system = "sms", launchable = true, state = "available", execution = "fpga_native", ready_here = false, ready_block = "version_skew" },
    { id = "sms-data-storm", title = "Data Storm", system = "sms", launchable = true, state = "available", execution = "host_only" },
  }
  result = destination.classify(games, { q = "Data Storm" })
  destination.set{ kind = "game", label = "Data Storm", query = "Data Storm", matches = result.candidates or result.matches }
end
function draw() end`
	r := newRoom(t, memPack(t, "storm", src, nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	if err := r.CheckGlobal(`result.state == 'ready' and #result.matches == 1 and result.matches[1].id == 'sms-data-storm' and #result.candidates == 2 and result.candidates[1].availability == 'unavailable' and result.candidates[1].reason == "Can't play here yet."`); err != nil {
		t.Fatal(err)
	}
	d := r.Destination()
	if d.Availability != AvailReady || d.Confirm() != ConfirmLaunch || len(d.Matches) != 1 || d.Matches[0].ID != "sms-data-storm" || len(d.Candidates) != 2 {
		t.Fatalf("published %+v", d)
	}
	if PlayChoices(d) != nil {
		t.Fatal("blocked sibling was offered")
	}

	yes := true
	fpga := hostclient.Game{ID: "fpga-data-storm", Title: "Data Storm", System: "sms", State: "available", RootOnline: true, Launchable: true, Execution: "fpga_native", ReadyHere: &yes}
	r.RefreshCachedGames([]hostclient.Game{fpga})
	d = r.Destination()
	if d.Availability != AvailNeedsChoice || d.Confirm() != ConfirmChoose || d.Choice != ChoiceBackend || len(PlayChoices(d)) != 2 {
		t.Fatalf("both viable %+v choices=%d", d, len(PlayChoices(d)))
	}

	no := false
	emu := hostclient.Game{ID: "sms-data-storm", Title: "Data Storm", System: "sms", State: "available", RootOnline: true, Launchable: true, Execution: hostclient.ExecutionHostOnly, ReadyHere: &no, ReadyBlock: string(hostclient.LaunchLeaseHeld)}
	r.RefreshCachedGames([]hostclient.Game{emu})
	d = r.Destination()
	if d.Availability != AvailReady || d.Confirm() != ConfirmLaunch || d.GameID != "fpga-data-storm" || len(d.Matches) != 1 || len(d.Candidates) != 2 {
		t.Fatalf("fpga should be the sole ready row %+v", d)
	}
	for _, g := range d.Matches {
		if g.ID == "sms-data-storm" {
			t.Fatal("blocked emulator was offered")
		}
	}
}

func TestPlayHistoryLuaDoesNotConflatePlayedAndCompleted(t *testing.T) {
	svc := &fakeServices{
		games: []hostclient.Game{
			{ID: "nes-smb", Title: "Super Mario Bros.", System: "nes", State: "available", RootOnline: true, Launchable: true, PlayCount: 2, LastPlayedAt: 99},
			{ID: "nes-smb3", Title: "Super Mario Bros. 3", System: "nes", State: "available", RootOnline: true, Launchable: true},
		},
	}
	src := `
played = nil
unplayed = nil
resume = nil
explicit = nil
function load()
  library.query({ q = "Super Mario" }, function(games, err)
    for _, g in ipairs(games) do
      if g.id == "nes-smb" then
        played = destination.play_history(g)
        destination.set{ kind = "game", label = g.title, game_id = g.id, matches = { g } }
      elseif g.id == "nes-smb3" then
        unplayed = destination.play_history(g)
      end
    end
    resume = destination.play_history({ id = "nes-smb3", play_count = 0, last_played_at = 0 })
    explicit = destination.play_history({ play_count = 1, completed = true })
  end)
end
function draw() end`
	r := newRoom(t, memPack(t, "hist", src, nil), Options{Services: svc})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(Frame) bool { return r.L.GetGlobal("played").Type().String() == "table" })
	d := r.Destination()
	if !d.History.Played || d.History.Completed || d.History.Line() != "Played" {
		t.Fatalf("published dest %+v", d.History)
	}
	if err := r.CheckGlobal("played.played == true and played.completed == false and played.line == 'Played'"); err != nil {
		t.Fatal(err)
	}
	if err := r.CheckGlobal("unplayed.played == false and unplayed.completed == false and unplayed.line == ''"); err != nil {
		t.Fatal(err)
	}
	if err := r.CheckGlobal("resume.played == false and resume.completed == false"); err != nil {
		t.Fatal(err)
	}
	if err := r.CheckGlobal("explicit.played == true and explicit.completed == true and explicit.line == 'Played  ·  Completed'"); err != nil {
		t.Fatal(err)
	}
	if err := r.CheckGlobal("destination.get().played == true and destination.get().completed == false"); err != nil {
		t.Fatal(err)
	}
}

func TestLuaMatchRepublishKeepsHostPlayFacts(t *testing.T) {
	host := hostclient.Game{ID: "nes-smb", Title: "Super Mario Bros.", System: "nes", State: "available", RootOnline: true, Launchable: true, PlayCount: 2, LastPlayedAt: 50}
	svc := &fakeServices{games: []hostclient.Game{host}}
	src := `
snap = nil
function load()
  library.query({ q = "Super Mario" }, function(games, err)
    snap = games[1]
    destination.set{ kind = "game", label = snap.title, game_id = snap.id, matches = { snap } }
  end)
end
function draw() end
function republish_stale()
  snap.play_count = 0
  snap.last_played_at = 0
  destination.set{ kind = "game", label = snap.title, game_id = snap.id, matches = { snap } }
end`
	r := newRoom(t, memPack(t, "hist-cache", src, nil), Options{Services: svc})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(Frame) bool { return r.L.GetGlobal("snap").Type().String() == "table" })

	host.PlayCount = 7
	host.LastPlayedAt = 200
	r.games[host.ID] = host
	if err := r.callGlobal("republish_stale", r.budget.Input); err != nil {
		t.Fatal(err)
	}

	g, ok := r.CachedGame(host.ID)
	if !ok || g.PlayCount != 7 || g.LastPlayedAt != 200 {
		t.Fatalf("cached play facts %+v want count=7 last=200", g)
	}
	d := r.Destination()
	if len(d.Matches) != 1 || d.Matches[0].PlayCount != 7 || d.Matches[0].LastPlayedAt != 200 {
		t.Fatalf("published matches %+v", d.Matches)
	}
	if !d.History.Played || d.History.Completed || d.History.Line() != "Played" {
		t.Fatalf("published dest %+v", d.History)
	}
}

func TestAssetImageDecodeAndBudget(t *testing.T) {
	var buf bytes.Buffer
	im := image.NewRGBA(image.Rect(0, 0, 8, 4))
	im.Set(0, 0, color.RGBA{255, 0, 0, 255})
	if err := png.Encode(&buf, im); err != nil {
		t.Fatal(err)
	}
	fsys := fstest.MapFS{
		ManifestName:    {Data: manifest("asset")},
		"main.lua":      {Data: []byte("bg = image.load('assets/bg.png')\nbad = image.load('assets/missing.png')\nfunction draw() if bg.ready then gfx.image(bg, 1, 2) end end")},
		"assets/bg.png": {Data: buf.Bytes()},
	}
	p := LoadPackFS(fsys, "mem:asset")
	r := newRoom(t, p, Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	f := stepUntil(t, r, func(f Frame) bool { return len(f.Ops) == 1 })
	if f.Ops[0].Image != "asset:assets/bg.png" || f.Ops[0].W != 8 || f.Ops[0].H != 4 {
		t.Fatalf("op %+v", f.Ops[0])
	}
	stepUntil(t, r, func(Frame) bool { return r.images["asset:assets/missing.png"].Ready })
	if r.images["asset:assets/missing.png"].Err == "" {
		t.Error("missing asset should carry an error")
	}

	r2 := newRoom(t, p, Options{Budget: Budget{Load: time.Second, Frame: time.Second, Input: time.Second, MaxImagePixels: 8}})
	if err := r2.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r2, func(Frame) bool { return r2.images["asset:assets/bg.png"].Ready })
	if !strings.Contains(r2.images["asset:assets/bg.png"].Err, "budget") {
		t.Errorf("expected budget error, got %q", r2.images["asset:assets/bg.png"].Err)
	}
}

func TestDestinationActionAllowlist(t *testing.T) {
	src := `
function load()
  destination.set{ kind = "action", action = "settings", label = "Settings" }
end
function draw() end`
	r := newRoom(t, memPack(t, "actions", src, nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	d := r.Destination()
	if d.Kind != KindAction || d.LauncherAction != "settings" || d.Availability != AvailReady {
		t.Fatalf("dest %+v", d)
	}
	if d.Confirm() != ConfirmLauncherAction {
		t.Fatalf("confirm %v", d.Confirm())
	}
	if d.Status != "Open settings." || d.Action != "Open settings." {
		t.Fatalf("copy %+v", d)
	}
	if err := r.CheckGlobal(`destination.get().kind == "action" and destination.get().launcher_action == "settings" and destination.get().availability == "ready" and destination.get().action == "Open settings."`); err != nil {
		t.Fatal(err)
	}
}

func TestDestinationRejectsUnknownAndMissingAction(t *testing.T) {
	keep := `
function load()
  destination.set{ kind = "room", room_id = "other", label = "Other" }
end
function on_input(cmd)
  if cmd == "right" then destination.set{ kind = "action", action = "shell" }
  elseif cmd == "left" then destination.set{ kind = "action" }
  elseif cmd == "up" then destination.set{ kind = "action", action = "  " }
  elseif cmd == "down" then destination.set{ kind = "nope", action = "settings", label = "Nope" }
  end
  return true
end
function draw() end`
	r := newRoom(t, memPack(t, "reject", keep, nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	if got := r.Destination(); got.Kind != KindRoom || got.RoomID != "other" {
		t.Fatalf("setup dest %+v", got)
	}
	r.Input("right")
	if r.Err() == nil || !strings.Contains(r.Err().Error(), `destination.set: unknown launcher action "shell"`) {
		t.Fatalf("unknown action error %v", r.Err())
	}
	if got := r.Destination(); got.Kind == KindAction || got.LauncherAction != "" {
		t.Fatalf("unknown action stored %+v", got)
	}

	r2 := newRoom(t, memPack(t, "missing-action", keep, nil), Options{})
	if err := r2.Load(); err != nil {
		t.Fatal(err)
	}
	r2.Input("left")
	if r2.Err() == nil || !strings.Contains(r2.Err().Error(), "destination.set: launcher action required") {
		t.Fatalf("missing action error %v", r2.Err())
	}
	if got := r2.Destination(); got.Kind == KindAction || got.LauncherAction != "" {
		t.Fatalf("missing action stored %+v", got)
	}

	r3 := newRoom(t, memPack(t, "blank-action", keep, nil), Options{})
	if err := r3.Load(); err != nil {
		t.Fatal(err)
	}
	r3.Input("up")
	if r3.Err() == nil || !strings.Contains(r3.Err().Error(), "destination.set: launcher action required") {
		t.Fatalf("blank action error %v", r3.Err())
	}

	r4 := newRoom(t, memPack(t, "unknown-kind", keep, nil), Options{})
	if err := r4.Load(); err != nil {
		t.Fatal(err)
	}
	r4.Input("down")
	if r4.Err() != nil {
		t.Fatal(r4.Err())
	}
	if got := r4.Destination(); got.Kind == KindAction || got.LauncherAction != "" {
		t.Fatalf("unknown kind became an action %+v", got)
	}
	if got := r4.Destination(); got.Kind != KindUnresolved {
		t.Fatalf("unknown kind dest %+v", got)
	}
}

func TestRoomsOpenValidatesAgainstIndex(t *testing.T) {
	other := memPack(t, "other", "function draw() end", nil)
	src := "function on_input(cmd)\n if cmd == 'right' then rooms.open('other', {game_id = 'selected-game'}) elseif cmd == 'left' then rooms.open('missing') elseif cmd == 'back' then rooms.back() end\n return true\nend\nfunction draw() end"
	self := memPack(t, "self", src, nil)
	r := newRoom(t, self, Options{Index: NewIndex([]Pack{self, other})})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	r.Input("right")
	r.Input("back")
	acts := r.TakeActions()
	if len(acts) != 2 || acts[0].Kind != ActionOpenRoom || acts[0].RoomID != "other" || acts[0].InitialGameID != "selected-game" || acts[1].Kind != ActionBack {
		t.Fatalf("actions %+v", acts)
	}
	r.Input("left")
	if r.Err() == nil || !strings.Contains(r.Err().Error(), "unknown room") {
		t.Fatalf("expected unknown room error, got %v", r.Err())
	}
}

const (
	corePongID   = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	coreColecoID = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
	coreZXID     = "1111111111111111111111111111111111111111111111111111111111111111"
)

type fakeLocalCores struct {
	mu        sync.Mutex
	cores     []localcores.Core
	listErr   error
	launchErr error
	launched  []string
}

func (f *fakeLocalCores) List(context.Context) ([]localcores.Core, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	return append([]localcores.Core(nil), f.cores...), nil
}

func (f *fakeLocalCores) Launch(_ context.Context, packageID string) (localcores.LaunchResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.launched = append(f.launched, packageID)
	return localcores.LaunchResult{Generation: strings.Repeat("a", 64)}, f.launchErr
}

func (f *fakeLocalCores) Stop(context.Context, string) error { return nil }

func (f *fakeLocalCores) Status(context.Context) (localcores.RunStatus, error) {
	return localcores.RunStatus{}, nil
}

func (f *fakeLocalCores) launchedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.launched...)
}

func TestKitCoresAbsentWithoutClient(t *testing.T) {
	src := `
function load()
  has_kit = kit ~= nil
  has_cores = kit ~= nil and kit.cores ~= nil
end
function draw() end`
	r := newRoom(t, memPack(t, "no-kit", src, nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	if err := r.CheckGlobal(`has_kit == false and has_cores == false`); err != nil {
		t.Fatal(err)
	}
	if err := r.ActivateDestination(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestKitCoresRowsAndLaunch(t *testing.T) {
	fake := &fakeLocalCores{cores: []localcores.Core{{
		PackageID: corePongID, CoreID: "fes.pong", Name: "FES Pong", Needs: "none", Launchable: true,
	}}}
	src := `
function load()
  kit.cores(function(rows, err)
    got_err = err
    got = rows
    local row = rows and rows[1]
    if row then
      destination.set{
        kind = "core",
        package_id = row.package_id,
        core_id = row.core_id,
        label = row.name,
        launchable = row.launchable,
        block = row.block or "",
      }
    end
  end)
end
function draw() end`
	r := newRoom(t, memPack(t, "kit-cores", src, nil), Options{Local: fake})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(Frame) bool { return r.Destination().Kind == KindCore })
	if err := r.CheckGlobal(`got_err == nil and got[1].package_id == "` + corePongID + `" and got[1].core_id == "fes.pong" and got[1].name == "FES Pong" and got[1].needs == "none" and got[1].launchable == true and (got[1].block == nil or got[1].block == "")`); err != nil {
		t.Fatal(err)
	}
	d := r.Destination()
	if d.PackageID != corePongID || d.CoreID != "fes.pong" || !d.CoreLaunchable || d.Confirm() != ConfirmLaunchCore {
		t.Fatalf("dest %+v confirm %v", d, d.Confirm())
	}
	if err := r.ActivateDestination(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fake.launchedIDs(); len(got) != 1 || got[0] != corePongID {
		t.Fatalf("launched %v", got)
	}
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
}

type warmThenCores struct {
	firstError error
	mu         sync.Mutex
	calls      int
	cores      []localcores.Core
}

func (f *warmThenCores) List(context.Context) ([]localcores.Core, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls == 1 {
		if f.firstError != nil {
			return nil, f.firstError
		}
		return nil, localcores.ErrWarming
	}
	return append([]localcores.Core(nil), f.cores...), nil
}

func (f *warmThenCores) Launch(context.Context, string) (localcores.LaunchResult, error) {
	return localcores.LaunchResult{Generation: strings.Repeat("a", 64)}, nil
}

func (f *warmThenCores) Stop(context.Context, string) error { return nil }

func (f *warmThenCores) Status(context.Context) (localcores.RunStatus, error) {
	return localcores.RunStatus{}, nil
}

func TestKitCoresRetriesWarming(t *testing.T) {
	fake := &warmThenCores{cores: []localcores.Core{{
		PackageID: corePongID, CoreID: "fes.pong", Name: "FES Pong", Needs: "none", Launchable: true,
	}}}
	src := `
function load()
  kit.cores(function(rows, err)
    got_err = err
    got = rows
  end)
end
function draw() end`
	r := newRoom(t, memPack(t, "kit-warming", src, nil), Options{Local: fake})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(Frame) bool {
		return r.CheckGlobal(`got_err == nil and type(got) == "table" and got[1] ~= nil and got[1].core_id == "fes.pong"`) == nil
	})
	fake.mu.Lock()
	calls := fake.calls
	fake.mu.Unlock()
	if calls < 2 {
		t.Fatalf("warming was not retried: %d calls", calls)
	}
}

func TestCoreActivationErrorsDoNotCrash(t *testing.T) {
	src := `
function load()
  destination.set{
    kind = "core",
    package_id = "` + corePongID + `",
    core_id = "fes.pong",
    label = "FES Pong",
    launchable = true,
  }
end
function on_input(cmd)
  if cmd == "right" then destination.set{ kind = "core", label = "Nope" }
  elseif cmd == "left" then destination.set{ kind = "core", package_id = "../etc", core_id = "fes.pong", label = "Bad" }
  elseif cmd == "up" then destination.set{ kind = "nope", package_id = "` + corePongID + `", label = "Unknown" }
  end
  return true
end
function draw() end`

	bare := newRoom(t, memPack(t, "bare-core", src, nil), Options{})
	if err := bare.Load(); err != nil {
		t.Fatal(err)
	}
	if got := bare.Destination(); got.Kind != KindCore || got.PackageID != corePongID || !got.CoreLaunchable {
		t.Fatalf("setup %+v", got)
	}
	if err := bare.ActivateDestination(context.Background()); !errors.Is(err, ErrNoLocalCores) {
		t.Fatalf("missing client %v", err)
	}
	if bare.Err() != nil {
		t.Fatal(bare.Err())
	}

	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "in_use", err: localcores.ErrInUse},
		{name: "blocked", err: localcores.ErrBlocked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeLocalCores{launchErr: tc.err}
			r := newRoom(t, memPack(t, "core-"+tc.name, src, nil), Options{Local: fake})
			if err := r.Load(); err != nil {
				t.Fatal(err)
			}
			err := r.ActivateDestination(context.Background())
			if !errors.Is(err, tc.err) {
				t.Fatalf("activate %v", err)
			}
			if r.Err() != nil {
				t.Fatal(r.Err())
			}
			if got := r.Destination(); got.Status != tc.err.Error() || got.Availability != AvailUnavailable {
				t.Fatalf("status %+v", got)
			}
			if ids := fake.launchedIDs(); len(ids) != 1 || ids[0] != corePongID {
				t.Fatalf("launched %v", ids)
			}
		})
	}

	reject := newRoom(t, memPack(t, "bad-core", src, nil), Options{})
	if err := reject.Load(); err != nil {
		t.Fatal(err)
	}
	reject.Input("right")
	if reject.Err() == nil || !strings.Contains(reject.Err().Error(), "destination.set: package_id required") {
		t.Fatalf("missing package id %v", reject.Err())
	}
	if got := reject.Destination(); got.PackageID != corePongID {
		t.Fatalf("missing id stored %+v", got)
	}

	bad := newRoom(t, memPack(t, "bad-id", src, nil), Options{})
	if err := bad.Load(); err != nil {
		t.Fatal(err)
	}
	bad.Input("left")
	if bad.Err() == nil || !strings.Contains(bad.Err().Error(), "is not a package id") {
		t.Fatalf("bad package id %v", bad.Err())
	}
	if got := bad.Destination(); got.PackageID != corePongID || got.Kind != KindCore {
		t.Fatalf("bad id stored %+v", got)
	}

	unknown := newRoom(t, memPack(t, "unknown-kind-core", src, nil), Options{})
	if err := unknown.Load(); err != nil {
		t.Fatal(err)
	}
	unknown.Input("up")
	if unknown.Err() != nil {
		t.Fatal(unknown.Err())
	}
	if got := unknown.Destination(); got.Kind != KindUnresolved || got.PackageID != "" {
		t.Fatalf("unknown kind %+v", got)
	}
}

func TestBlockedCoreDestinationDoesNotLaunch(t *testing.T) {
	fake := &fakeLocalCores{}
	src := `
function load()
  destination.set{
    kind = "core",
    package_id = "` + coreColecoID + `",
    core_id = "fes.coleco",
    label = "ColecoVision",
    launchable = false,
    block = "Needs a cartridge",
  }
end
function draw() end`
	r := newRoom(t, memPack(t, "blocked-core", src, nil), Options{Local: fake})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	d := r.Destination()
	if d.Kind != KindCore || d.CoreLaunchable || d.Status != "Needs a cartridge" || d.Confirm() != ConfirmExplain {
		t.Fatalf("dest %+v confirm %v", d, d.Confirm())
	}
	if err := r.ActivateDestination(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ids := fake.launchedIDs(); len(ids) != 0 {
		t.Fatalf("blocked launched %v", ids)
	}
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
}

func lua_true() lua.LValue { return lua.LTrue }

func TestRequireFailureIsNotStickyAndResizeUpdatesRoom(t *testing.T) {
	src := `
local ok1, err1 = pcall(require, "lib.broken")
local ok2, err2 = pcall(require, "lib.broken")
assert(not ok1 and not ok2, "broken module must fail both times")
assert(not tostring(err2):find("circular"), "second failure must not be reported as circular: " .. tostring(err2))
assert(tostring(err2):find("kaboom"), "module error must surface: " .. tostring(err2))
sizes = {}
function on_resize(w, h) sizes[#sizes + 1] = w .. "x" .. h end
function draw() end`
	r := newRoom(t, memPack(t, "req", src, map[string]string{"lib/broken.lua": "error('kaboom')"}), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	r.Resize(640, 360)
	r.Resize(640, 360) // no-op when unchanged
	if err := r.CheckGlobal(`room.width == 640 and room.height == 360 and #sizes == 1 and sizes[1] == "640x360"`); err != nil {
		t.Fatal(err)
	}
}

// A suspended room (not being stepped) must never block delivery, or the
// cover workers it shares with the visible room stall.
func TestDeliverNeverBlocksWhileSuspended(t *testing.T) {
	src := `
covers = {}
function load()
  for i = 1, 400 do covers[i] = image.cover("g" .. i) end
end
function draw() end`
	r := newRoom(t, memPack(t, "suspend", src, nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	ids := r.TakeCoverRequests()
	if len(ids) != 400 {
		t.Fatalf("cover requests %d", len(ids))
	}
	done := make(chan struct{})
	go func() {
		for _, id := range ids {
			r.DeliverCover(id, image.NewRGBA(image.Rect(0, 0, 1, 1)), nil)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("DeliverCover blocked while the room was suspended")
	}
	r.Step(time.Unix(1, 0))
	if got := len(r.Images()); got != 400 {
		t.Fatalf("images after resume %d want 400", got)
	}
	r.Close()
	r.DeliverCover("late", nil, nil) // dropped, must not panic
}

// The image budget is reserved when a bitmap is delivered, not when the room
// next steps, so an un-stepped room cannot hold unbounded decoded covers.
func TestImageBudgetAppliesToQueuedResults(t *testing.T) {
	src := `
covers = {}
function load()
  for i = 1, 50 do covers[i] = image.cover("g" .. i) end
end
function draw() end`
	r := newRoom(t, memPack(t, "budget", src, nil), Options{Budget: Budget{Load: time.Second, Frame: time.Second, Input: time.Second, MaxImagePixels: 10}})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	ids := r.TakeCoverRequests()
	for _, id := range ids {
		r.DeliverCover(id, image.NewRGBA(image.Rect(0, 0, 1, 1)), nil)
	}
	if got := r.reservedPixels(); got != 10 {
		t.Fatalf("reserved %d pixels before Step, want the 10-pixel cap", got)
	}
	r.pendingMu.Lock()
	retained := 0
	for _, res := range r.pending {
		if res.img != nil {
			retained++
		}
	}
	r.pendingMu.Unlock()
	if retained != 10 {
		t.Fatalf("queued bitmaps %d, want 10 (over-budget bitmaps must be dropped at delivery)", retained)
	}
	r.Step(time.Unix(1, 0))
	ready, over := 0, 0
	for _, im := range r.images {
		switch {
		case im.Img != nil:
			ready++
		case strings.Contains(im.Err, "budget"):
			over++
		}
	}
	if ready != 10 || over != 40 {
		t.Fatalf("ready %d over-budget %d, want 10/40", ready, over)
	}
}

// A failed room stops accepting async results.
func TestFailedRoomDropsDeliveries(t *testing.T) {
	r := newRoom(t, memPack(t, "failing", "c = image.cover('x')\nfunction draw() error('boom') end", nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	r.Step(time.Unix(1, 0))
	if r.Err() == nil {
		t.Fatal("expected failure")
	}
	r.DeliverCover("x", image.NewRGBA(image.Rect(0, 0, 4, 4)), nil)
	if r.reservedPixels() != 0 || len(r.pending) != 0 {
		t.Fatal("failed room must drop deliveries")
	}
}

func TestReservedCommandsAreNotDeliveredToScripts(t *testing.T) {
	src := `
saw = {}
function on_input(cmd)
  saw[#saw + 1] = cmd
  return true
end
function draw() end`
	r := newRoom(t, memPack(t, "reserved", src, nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	if r.Input("settings") || r.Input("home") {
		t.Fatal("settings/home must not be consumed by a room")
	}
	if !r.Input("back") {
		t.Fatal("back is still delivered so rooms.back() can run")
	}
	if err := r.CheckGlobal("#saw == 1 and saw[1] == 'back'"); err != nil {
		t.Fatal(err)
	}
}

func TestReducedMotionFlagRoundTrip(t *testing.T) {
	src := `function draw() end`
	r := newRoom(t, memPack(t, "motion", src, nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	if err := r.CheckGlobal("room.reduced_motion == false"); err != nil {
		t.Fatal(err)
	}
	r.SetReducedMotion(true)
	if !r.ReducedMotion() {
		t.Fatal("SetReducedMotion")
	}
	if err := r.CheckGlobal("room.reduced_motion == true"); err != nil {
		t.Fatal(err)
	}
}

func TestKitCoresStartupReadFailures(t *testing.T) {
	for _, first := range []error{localcores.ErrUnavailable, context.DeadlineExceeded} {
		t.Run(first.Error(), func(t *testing.T) {
			fake := &warmThenCores{firstError: first, cores: []localcores.Core{{PackageID: corePongID, CoreID: "fes.pong", Name: "FES Pong", Launchable: true}}}
			r := newRoom(t, memPack(t, "startup-cores", `function load() kit.cores(function(rows, err) got=rows got_err=err end) end function draw() end`, nil), Options{Local: fake})
			if err := r.Load(); err != nil {
				t.Fatal(err)
			}
			stepUntil(t, r, func(Frame) bool {
				return r.CheckGlobal(`got_err == nil and type(got) == "table" and got[1].core_id == "fes.pong"`) == nil
			})
		})
	}
}

func TestKitCoresStartupRetryBoundAndPermanentError(t *testing.T) {
	permanent := errors.New("invalid catalog response")
	fake := &warmThenCores{firstError: permanent}
	_, err := listLocalCores(t.Context(), fake, time.Second)
	if !errors.Is(err, permanent) || fake.calls != 1 {
		t.Fatalf("err=%v calls=%d", err, fake.calls)
	}
	fake = &warmThenCores{firstError: localcores.ErrUnavailable}
	_, err = listLocalCores(t.Context(), fake, 10*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) || fake.calls != 1 {
		t.Fatalf("err=%v calls=%d", err, fake.calls)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	fake = &warmThenCores{firstError: localcores.ErrUnavailable}
	_, err = listLocalCores(ctx, fake, time.Minute)
	if !errors.Is(err, context.Canceled) || fake.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, fake.calls)
	}
}

func TestKitCoresStartupRetryCancelsDuringWait(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fake := &warmThenCores{firstError: localcores.ErrUnavailable}
	done := make(chan error, 1)
	go func() { _, err := listLocalCores(ctx, fake, time.Minute); done <- err }()
	deadline := time.Now().Add(time.Second)
	for {
		fake.mu.Lock()
		calls := fake.calls
		fake.mu.Unlock()
		if calls > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("listing did not start")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("retry wait ignored cancellation")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.calls != 1 {
		t.Fatalf("listed again after cancellation: %d", fake.calls)
	}
}

// A delayed Go callback demonstrates the existing wall-clock watchdog: even
// without a busy Lua loop, elapsed scheduling/blocking time consumes the budget.
func TestCallbackDeadlineReportsNameAndElapsed(t *testing.T) {
	r := newRoom(t, memPack(t, "deadline-label", "function draw() wait() gfx.clear('#123456') end", nil), Options{Budget: Budget{Load: time.Second, Frame: time.Millisecond, Input: time.Second}})
	r.L.SetGlobal("wait", r.L.NewFunction(func(*lua.LState) int { time.Sleep(20 * time.Millisecond); return 0 }))
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	r.Step(time.Now())
	if err := r.Err(); err == nil || !strings.Contains(err.Error(), "draw callback:") || !strings.Contains(err.Error(), "elapsed") {
		t.Fatalf("deadline diagnostic: %v", err)
	}
	if got := r.Step(time.Now()); len(got.Ops) != 0 {
		t.Fatal("fatal room resumed")
	}
}

// gopher-lua checks cancellation between Lua instructions, so a Go callback can
// return successfully after its context deadline. The post-return watchdog must
// still report elapsed time, matching the deadline error.
func TestHardOverrunReportsElapsed(t *testing.T) {
	budget := 5 * time.Millisecond
	hard := budget * callbackHardLimitFactor
	r := newRoom(t, memPack(t, "hard-overrun", "function draw() end", nil), Options{Budget: Budget{Load: time.Second, Frame: budget, Input: budget}})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	block := r.L.NewFunction(func(L *lua.LState) int {
		time.Sleep(hard + 40*time.Millisecond)
		L.Push(lua.LTrue)
		return 1
	})
	assertHardOverrunElapsed(t, r.callSoft(block, budget, 0), hard)
	value, err := r.callRetSoft(block, budget)
	if value != lua.LNil {
		t.Fatalf("hard return overrun value: %v", value)
	}
	assertHardOverrunElapsed(t, err, hard)
}

func assertHardOverrunElapsed(t *testing.T, err error, hard time.Duration) {
	t.Helper()
	if err == nil || errors.Is(err, errSoftOverrun) {
		t.Fatalf("hard overrun: %v", err)
	}
	prefix := "script exceeded " + hard.String() + " hard callback budget (elapsed "
	msg := err.Error()
	i := strings.Index(msg, prefix)
	if i < 0 || !strings.HasSuffix(msg, ")") {
		t.Fatalf("hard overrun diagnostic: %v", err)
	}
	rest := strings.TrimSuffix(msg[i+len(prefix):], ")")
	if end := strings.Index(rest, ";"); end >= 0 {
		rest = rest[:end]
	}
	elapsed, parseErr := time.ParseDuration(rest)
	if parseErr != nil {
		t.Fatalf("elapsed in %q: %v", msg, parseErr)
	}
	if elapsed <= hard {
		t.Fatalf("elapsed %s did not exceed hard budget %s (%s)", elapsed, hard, msg)
	}
}

// The deadline and the soft and hard checks time the whole call, so the
// launcher thread stalling while it samples itself around a quick script
// still counts against the room.
func TestThreadSampleStallCountsTowardCallbackBudget(t *testing.T) {
	for _, tc := range []struct {
		name   string
		sample int
	}{{"before the script", 0}, {"after the script", 1}} {
		t.Run(tc.name, func(t *testing.T) {
			const budget, stall = 40 * time.Millisecond, 50 * time.Millisecond
			r := sampleStallRoom(t, budget, tc.sample, stall)
			r.Step(time.Now())
			if err := r.Err(); err != nil {
				t.Fatalf("soft overrun failed the room: %v", err)
			}
			if r.frameOverruns != 1 || len(r.frameStreak.timings) != 1 {
				t.Fatalf("stalled draw was not a soft overrun: %d overruns", r.frameOverruns)
			}
			if got := r.frameStreak.timings[0]; got.elapsed < stall {
				t.Fatalf("elapsed %s left out the %s stall", got.elapsed, stall)
			}
		})
	}
	t.Run("past the hard limit", func(t *testing.T) {
		const budget = 5 * time.Millisecond
		hard := budget * callbackHardLimitFactor
		r := sampleStallRoom(t, budget, 0, hard+40*time.Millisecond)
		r.Step(time.Now())
		err := r.Err()
		if err == nil || errors.Is(err, errSoftOverrun) || !strings.Contains(err.Error(), "draw callback: script exceeded "+hard.String()) {
			t.Fatalf("stall past the hard limit: %v", err)
		}
	})
}

// sampleStallRoom is a loaded room with a no-op draw. Its next callback's
// thread sample which (0 before the script, 1 after it) sleeps for stall.
func sampleStallRoom(t *testing.T, budget time.Duration, which int, stall time.Duration) *Instance {
	t.Helper()
	r := newRoom(t, memPack(t, "sample-stall", "function draw() end", nil), Options{Budget: Budget{Load: time.Second, Frame: budget, Input: budget}})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	samples := 0
	r.threadSample = func() (threadUsage, bool) {
		if samples == which {
			time.Sleep(stall)
		}
		samples++
		return readThreadUsage()
	}
	return r
}

// A room retired by consecutive soft overruns reports each callback's time
// and the GC and image-loading context, so a kit failure is diagnosable.
func TestSoftOverrunRetirementReportsTimings(t *testing.T) {
	r := newRoom(t, memPack(t, "slow", "function draw() gfx.clear('#000000') end", nil), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	// Only a cover the launcher has been asked for counts. An unrequested
	// handle, a ready image and a failed image do not.
	r.images["cover:a"] = &Image{Key: "cover:a", requested: true}
	r.images["cover:unsent"] = &Image{Key: "cover:unsent"}
	r.images["cover:b"] = &Image{Key: "cover:b", Ready: true, requested: true}
	r.images["cover:bad"] = &Image{Key: "cover:bad", Err: "no artwork", requested: true}
	for i := 0; i < maxConsecutiveOverruns; i++ {
		timing := callTiming{elapsed: time.Duration(13+i) * time.Millisecond}
		if i == 0 {
			timing.script = 12500 * time.Microsecond
			timing.thread, timing.measured = threadUsage{cpu: 2 * time.Millisecond, voluntary: 1, involuntary: 9}, true
		}
		r.recordFrameOverrun(fmt.Errorf("draw callback: %w", softOverrun{budget: 12 * time.Millisecond, timing: timing}))
	}
	err := r.Err()
	if err == nil {
		t.Fatal("soft overruns did not retire the room")
	}
	msg := err.Error()
	for _, want := range []string{"draw callback", "12ms soft callback budget", "8 consecutive overruns",
		"elapsed 13ms (script run 12.5ms: thread cpu 2ms, 1 voluntary/9 involuntary switches), 14ms, 15ms, 16ms, 17ms, 18ms, 19ms, 20ms",
		"process-wide GC cycles", "up to 1 images loading"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("retirement %q lacks %q", msg, want)
		}
	}
	if !errors.Is(softOverrun{}, errSoftOverrun) {
		t.Fatal("softOverrun is not errSoftOverrun")
	}
}
