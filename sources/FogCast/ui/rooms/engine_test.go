package rooms

import (
	"bytes"
	"context"
	"errors"
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
	mu        sync.Mutex
	games     []hostclient.Game
	platforms []hostclient.Platform
	queries   []hostclient.GameListQuery
	err       error
}

func (f *fakeServices) QueryGames(_ context.Context, q hostclient.GameListQuery, _ int) ([]hostclient.Game, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, q)
	return f.games, f.err
}

func (f *fakeServices) recorded() []hostclient.GameListQuery {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]hostclient.GameListQuery(nil), f.queries...)
}
func (f *fakeServices) Platforms(context.Context) ([]hostclient.Platform, error) {
	return f.platforms, f.err
}
func (f *fakeServices) Collections(context.Context) ([]hostclient.Collection, error) {
	return []hostclient.Collection{{ID: "strategy", Name: "Strategy"}}, f.err
}
func (f *fakeServices) Game(_ context.Context, id string) (hostclient.Game, error) {
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
	if len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != "snes-mario-abc123" {
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
	r.overrun(errSoftOverrun)
	if r.Err() != nil {
		t.Fatalf("second transient overrun failed the room: %v", r.Err())
	}
	r.overrun(errSoftOverrun)
	if r.Err() == nil || !strings.Contains(r.Err().Error(), "overruns") {
		t.Fatalf("sustained overruns should fail the room, got %v", r.Err())
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
	if !r.Input("left") {
		t.Fatal("overrunning input callback should remain handled")
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
	src := "function on_input(cmd)\n if cmd == 'right' then rooms.open('other') elseif cmd == 'left' then rooms.open('missing') elseif cmd == 'back' then rooms.back() end\n return true\nend\nfunction draw() end"
	self := memPack(t, "self", src, nil)
	r := newRoom(t, self, Options{Index: NewIndex([]Pack{self, other})})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	r.Input("right")
	r.Input("back")
	acts := r.TakeActions()
	if len(acts) != 2 || acts[0].Kind != ActionOpenRoom || acts[0].RoomID != "other" || acts[1].Kind != ActionBack {
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

func (f *fakeLocalCores) Launch(_ context.Context, packageID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.launched = append(f.launched, packageID)
	return f.launchErr
}

func (f *fakeLocalCores) Stop(context.Context) error { return nil }

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
