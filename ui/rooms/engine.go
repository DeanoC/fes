package rooms

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"image"
	"io"
	"io/fs"
	"os"
	"strings"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/theme"
)

//go:embed stdlib
var stdlibFS embed.FS

// Services is what a room may ask the launcher's host for. Calls run on
// engine goroutines; results are delivered back on the launcher thread by Step.
type Services interface {
	QueryGames(ctx context.Context, q hostclient.GameListQuery, max int) ([]hostclient.Game, error)
	Platforms(ctx context.Context) ([]hostclient.Platform, error)
	Collections(ctx context.Context) ([]hostclient.Collection, error)
	Game(ctx context.Context, id string) (hostclient.Game, error)
}

// Budget bounds one room so a broken script cannot wedge the launcher.
type Budget struct {
	Load           time.Duration
	Frame          time.Duration
	Input          time.Duration
	MaxOps         int
	MaxImagePixels int
	MaxQueryGames  int
}

// DefaultBudget is tuned for a 60 Hz sofa loop.
func DefaultBudget() Budget {
	return Budget{
		Load:           250 * time.Millisecond,
		Frame:          6 * time.Millisecond,
		Input:          4 * time.Millisecond,
		MaxOps:         4096,
		MaxImagePixels: 16 << 20,
		MaxQueryGames:  2000,
	}
}

func (b Budget) complete() Budget {
	d := DefaultBudget()
	if b.Load <= 0 {
		b.Load = d.Load
	}
	if b.Frame <= 0 {
		b.Frame = d.Frame
	}
	if b.Input <= 0 {
		b.Input = d.Input
	}
	if b.MaxOps <= 0 {
		b.MaxOps = d.MaxOps
	}
	if b.MaxImagePixels <= 0 {
		b.MaxImagePixels = d.MaxImagePixels
	}
	if b.MaxQueryGames <= 0 {
		b.MaxQueryGames = d.MaxQueryGames
	}
	return b
}

// Options configure one room instance.
type Options struct {
	Width, Height int
	Services      Services
	Index         *Index
	Theme         theme.Theme
	// StorePath is the per-room JSON persistence file; empty disables store.
	StorePath string
	Budget    Budget
	Now       func() time.Time
	Stderr    io.Writer
}

// Instance is one running room. It is not safe for concurrent use: the
// launcher calls every method while holding its own state lock. Only
// DeliverCover may be called from another goroutine.
type Instance struct {
	pack   Pack
	opts   Options
	budget Budget
	L      *lua.LState
	ctx    context.Context
	cancel context.CancelFunc

	// pending holds async results until the launcher thread drains them in
	// Step. It is a growable queue, not a channel, so delivery never blocks:
	// a suspended parent room (nested room open) must not stall the cover
	// workers it shares with the visible room.
	pendingMu sync.Mutex
	pending   []asyncResult
	// pixels is the decoded image budget already reserved, including
	// bitmaps still queued in pending. Guarded by pendingMu.
	pixels  int
	modules map[string]lua.LValue
	loading map[string]bool

	frame    Frame
	building *Frame
	inDraw   bool
	actions  []Action
	games    map[string]hostclient.Game
	images   map[string]*Image
	store    *store
	logs     []string

	started      time.Time
	last         time.Time
	elapsed      float64
	fatal        error
	loaded       bool
	sessionState string
	roomTable    *lua.LTable
}

type asyncResult struct {
	cb          *lua.LFunction
	err         error
	games       []hostclient.Game
	platforms   []hostclient.Platform
	collections []hostclient.Collection
	game        *hostclient.Game
	imageKey    string
	img         *image.RGBA
}

// New creates the VM and installs the API. Load runs the script.
func New(pack Pack, opts Options) (*Instance, error) {
	if pack.Err != nil {
		return nil, pack.Err
	}
	if pack.FS == nil {
		return nil, errors.New("rooms: pack has no filesystem")
	}
	if opts.Width <= 0 || opts.Height <= 0 {
		return nil, errors.New("rooms: width and height are required")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	opts.Theme = opts.Theme.Complete()
	ctx, cancel := context.WithCancel(context.Background())
	r := &Instance{
		pack:    pack,
		opts:    opts,
		budget:  opts.Budget.complete(),
		ctx:     ctx,
		cancel:  cancel,
		modules: map[string]lua.LValue{},
		loading: map[string]bool{},
		games:   map[string]hostclient.Game{},
		images:  map[string]*Image{},
		store:   newStore(opts.StorePath),
	}
	r.L = lua.NewState(lua.Options{
		SkipOpenLibs:        true,
		RegistrySize:        4096,
		RegistryMaxSize:     1 << 18,
		CallStackSize:       256,
		IncludeGoStackTrace: false,
	})
	r.installSandbox()
	r.installAPI()
	return r, nil
}

// ID is the pack id.
func (r *Instance) ID() string { return r.pack.ID }

// Title is the pack title.
func (r *Instance) Title() string { return r.pack.Title }

// Err is the fatal script error, if the room has failed.
func (r *Instance) Err() error { return r.fatal }

// Frame is the last completed display list.
func (r *Instance) Frame() Frame { return r.frame }

// Logs are the most recent log() lines.
func (r *Instance) Logs() []string { return append([]string(nil), r.logs...) }

// Close stops async work and frees the VM.
func (r *Instance) Close() {
	if r == nil || r.L == nil {
		return
	}
	if r.loaded && r.fatal == nil {
		r.callGlobal("unload", r.budget.Input)
	}
	r.cancel()
	r.store.flush()
	r.L.Close()
	r.L = nil
}

func (r *Instance) installSandbox() {
	L := r.L
	for _, lib := range []struct {
		name string
		fn   lua.LGFunction
	}{
		{lua.BaseLibName, lua.OpenBase},
		{lua.TabLibName, lua.OpenTable},
		{lua.StringLibName, lua.OpenString},
		{lua.MathLibName, lua.OpenMath},
	} {
		L.Push(L.NewFunction(lib.fn))
		L.Push(lua.LString(lib.name))
		L.Call(1, 0)
	}
	for _, name := range []string{"dofile", "loadfile", "load", "loadstring", "collectgarbage", "module", "newproxy", "getfenv", "setfenv"} {
		L.SetGlobal(name, lua.LNil)
	}
	L.SetGlobal("print", L.NewFunction(r.luaLog))
	L.SetGlobal("log", L.NewFunction(r.luaLog))
	L.SetGlobal("require", L.NewFunction(r.luaRequire))
	if str, ok := L.GetGlobal("string").(*lua.LTable); ok {
		str.RawSetString("dump", lua.LNil)
	}
}

// Load compiles main and runs the script body then load().
func (r *Instance) Load() error {
	if r.fatal != nil {
		return r.fatal
	}
	src, err := readPackFile(r.pack.FS, r.pack.Main, maxSourceBytes)
	if err != nil {
		return r.fail(fmt.Errorf("read %s: %w", r.pack.Main, err))
	}
	fn, err := r.L.Load(bytes.NewReader(src), r.pack.Main)
	if err != nil {
		return r.fail(fmt.Errorf("compile: %w", err))
	}
	r.started = r.opts.Now()
	r.last = r.started
	if err := r.call(fn, r.budget.Load, 0); err != nil {
		return r.fail(err)
	}
	r.loaded = true
	if err := r.callGlobal("load", r.budget.Load); err != nil {
		return r.fail(err)
	}
	return nil
}

// Step delivers async results, runs update(dt) and draw(), and returns the
// new frame. Steps after a fatal error return an empty frame.
func (r *Instance) Step(now time.Time) Frame {
	if r.fatal != nil || !r.loaded {
		return Frame{}
	}
	dt := now.Sub(r.last).Seconds()
	if dt < 0 {
		dt = 0
	}
	if dt > 0.1 {
		dt = 0.1
	}
	r.last = now
	r.elapsed += dt
	r.roomTable.RawSetString("time", lua.LNumber(r.elapsed))
	r.drainResults()
	if r.fatal != nil {
		return Frame{}
	}
	if err := r.callGlobal("update", r.budget.Frame, lua.LNumber(dt)); err != nil {
		r.fail(err)
		return Frame{}
	}
	frame := Frame{Clear: r.opts.Theme.SofaBackground}
	r.building = &frame
	r.inDraw = true
	err := r.callGlobal("draw", r.budget.Frame)
	r.inDraw = false
	r.building = nil
	if err != nil {
		r.fail(err)
		return Frame{}
	}
	r.frame = frame
	r.store.maybeFlush(now)
	return frame
}

// Input forwards a launcher command name. It reports whether the script
// consumed it.
func (r *Instance) Input(cmd string) bool {
	if r.fatal != nil || !r.loaded {
		return false
	}
	fn, ok := r.L.GetGlobal("on_input").(*lua.LFunction)
	if !ok {
		return false
	}
	handled, err := r.callRet(fn, r.budget.Input, lua.LString(cmd))
	if err != nil {
		r.fail(err)
		return true
	}
	return lua.LVAsBool(handled)
}

// Hover reports pointer hover over a registered hit region.
func (r *Instance) Hover(id string) {
	if r.fatal != nil || !r.loaded {
		return
	}
	if err := r.callGlobal("on_hover", r.budget.Input, lua.LString(id)); err != nil {
		r.fail(err)
	}
}

// Activate reports a pointer click on a registered hit region.
func (r *Instance) Activate(id string) {
	if r.fatal != nil || !r.loaded {
		return
	}
	if err := r.callGlobal("on_activate", r.budget.Input, lua.LString(id)); err != nil {
		r.fail(err)
	}
}

// Resume is called when the launcher returns to this room after a play
// session or a nested room.
func (r *Instance) Resume() {
	if r.fatal != nil || !r.loaded {
		return
	}
	if err := r.callGlobal("on_resume", r.budget.Input); err != nil {
		r.fail(err)
	}
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
	r.roomTable.RawSetString("width", lua.LNumber(width))
	r.roomTable.RawSetString("height", lua.LNumber(height))
	if r.fatal != nil || !r.loaded {
		return
	}
	if err := r.callGlobal("on_resize", r.budget.Input, lua.LNumber(width), lua.LNumber(height)); err != nil {
		r.fail(err)
	}
}

// SetSessionState is the launcher's current play-session state string.
func (r *Instance) SetSessionState(state string) { r.sessionState = state }

// TakeActions returns and clears launcher requests made by the script.
func (r *Instance) TakeActions() []Action {
	out := r.actions
	r.actions = nil
	return out
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
	return r.call(fn, budget, 0, args...)
}

func (r *Instance) callRet(fn *lua.LFunction, budget time.Duration, args ...lua.LValue) (lua.LValue, error) {
	if err := r.call(fn, budget, 1, args...); err != nil {
		return lua.LNil, err
	}
	v := r.L.Get(-1)
	r.L.Pop(1)
	return v, nil
}

func (r *Instance) call(fn *lua.LFunction, budget time.Duration, nret int, args ...lua.LValue) error {
	L := r.L
	ctx, cancel := context.WithTimeout(r.ctx, budget)
	defer cancel()
	L.SetContext(ctx)
	defer L.RemoveContext()
	L.Push(fn)
	for _, a := range args {
		L.Push(a)
	}
	if err := L.PCall(len(args), nret, nil); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("script exceeded %s budget", budget)
		}
		return errors.New(strings.TrimSpace(err.Error()))
	}
	return nil
}

func (r *Instance) drainResults() {
	r.pendingMu.Lock()
	queued := r.pending
	r.pending = nil
	r.pendingMu.Unlock()
	for _, res := range queued {
		r.applyResult(res)
		if r.fatal != nil {
			return
		}
	}
}

func (r *Instance) applyResult(res asyncResult) {
	if res.imageKey != "" {
		r.applyImage(res)
		return
	}
	if res.cb == nil {
		return
	}
	var payload lua.LValue = lua.LNil
	switch {
	case res.err != nil:
	case res.games != nil:
		payload = r.gamesTable(res.games)
	case res.platforms != nil:
		payload = r.platformsTable(res.platforms)
	case res.collections != nil:
		payload = r.collectionsTable(res.collections)
	case res.game != nil:
		r.games[res.game.ID] = *res.game
		payload = r.gameTable(*res.game)
	}
	var errVal lua.LValue = lua.LNil
	if res.err != nil {
		errVal = lua.LString(res.err.Error())
	}
	if err := r.call(res.cb, r.budget.Frame, 0, payload, errVal); err != nil {
		r.fail(err)
	}
}

var errImageBudget = errors.New("room image budget exceeded")

// deliver queues an async result for the next Step. Safe from any
// goroutine and never blocks; results for a closed or failed room are
// dropped. Decoded bitmaps reserve the image budget here, at delivery, so a
// room that is not being stepped (parked, nested, failed) cannot accumulate
// more pixels than MaxImagePixels while its results wait.
func (r *Instance) deliver(res asyncResult) {
	if r.ctx.Err() != nil {
		return
	}
	r.pendingMu.Lock()
	defer r.pendingMu.Unlock()
	if res.img != nil {
		px := res.img.Bounds().Dx() * res.img.Bounds().Dy()
		if r.pixels+px > r.budget.MaxImagePixels {
			res.img = nil
			res.err = errImageBudget
		} else {
			r.pixels += px
		}
	}
	r.pending = append(r.pending, res)
}

// reservedPixels reports the decoded image budget in use (tests/HUD).
func (r *Instance) reservedPixels() int {
	r.pendingMu.Lock()
	defer r.pendingMu.Unlock()
	return r.pixels
}

func (r *Instance) luaLog(L *lua.LState) int {
	n := L.GetTop()
	parts := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		parts = append(parts, L.ToStringMeta(L.Get(i)).String())
	}
	line := strings.Join(parts, " ")
	r.logs = append(r.logs, line)
	if len(r.logs) > 32 {
		r.logs = r.logs[len(r.logs)-32:]
	}
	fmt.Fprintf(r.opts.Stderr, "room[%s]: %s\n", r.pack.ID, line)
	return 0
}

func (r *Instance) luaRequire(L *lua.LState) int {
	name := L.CheckString(1)
	if v, ok := r.modules[name]; ok {
		L.Push(v)
		return 1
	}
	if r.loading[name] {
		L.RaiseError("require %q: circular dependency", name)
	}
	src, chunk, err := r.resolveModule(name)
	if err != nil {
		L.RaiseError("require %q: %s", name, err.Error())
	}
	fn, err := L.Load(bytes.NewReader(src), chunk)
	if err != nil {
		L.RaiseError("require %q: %s", name, err.Error())
	}
	r.loading[name] = true
	L.Push(fn)
	err = L.PCall(0, 1, nil)
	delete(r.loading, name)
	if err != nil {
		L.RaiseError("require %q: %s", name, strings.TrimSpace(err.Error()))
	}
	v := L.Get(-1)
	L.Pop(1)
	if v == lua.LNil {
		v = lua.LTrue
	}
	r.modules[name] = v
	L.Push(v)
	return 1
}

// resolveModule maps "widgets.grid" to the embedded stdlib, else to
// <pack>/widgets/grid.lua. Module names never escape the pack.
func (r *Instance) resolveModule(name string) ([]byte, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
		return nil, "", errors.New("invalid module name")
	}
	rel := strings.ReplaceAll(name, ".", "/") + ".lua"
	if data, err := fs.ReadFile(stdlibFS, "stdlib/"+rel); err == nil {
		return data, "stdlib/" + rel, nil
	}
	data, err := readPackFile(r.pack.FS, rel, maxSourceBytes)
	if err != nil {
		return nil, "", fmt.Errorf("module not found in stdlib or room (%v)", err)
	}
	return data, r.pack.ID + "/" + rel, nil
}
