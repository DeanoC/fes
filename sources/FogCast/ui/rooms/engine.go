package rooms

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/ui/theme"
	lua "github.com/yuin/gopher-lua"
)

//go:embed stdlib
var stdlibFS embed.FS

// Setup is the first-run setup state a room sees. Step is the open setup
// room's 1-based position among Steps (0 for a room outside setup).
// Unfinished is true after setup was skipped and before it is finished;
// Skipped lists the steps not yet done.
type Setup struct {
	Step, Steps int
	Unfinished  bool
	Skipped     []string
}

// Pad is an attached kit pad as room.pads() lists it.
type Pad struct {
	// Player is the pad's controller port from 1, or 0 for a menu-only pad.
	Player int
	Name   string
	// Family is whose markings the pad's buttons carry: "xbox",
	// "playstation", "nintendo" or "generic".
	Family     string
	Recognised bool
	Bluetooth  bool
	// Tested is a pad verified on FES hardware (#1348).
	Tested bool
	// SwapAB is a pad whose right-hand face button confirms.
	SwapAB bool
	// Mapped is a pad read through a mapping its player made step by step.
	Mapped bool
	// Glyphs are the pad's prompt glyphs by control: south, east, west,
	// north, l, r, select, start and dpad.
	Glyphs map[string]string
	// Pressed are the controls held down: the Glyphs names, and up, down,
	// left and right for the D-pad.
	Pressed []string
	// Sends is what FES reads from each control in Glyphs, such as "A".
	Sends map[string]string
}

// Services is what a room may ask the launcher's host for. Calls run on
// engine goroutines; results are delivered back on the launcher thread by Step.
type Services interface {
	QueryGames(ctx context.Context, q hostclient.GameListQuery, max int) ([]hostclient.Game, bool, error)
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
		Load: 250 * time.Millisecond,
		// The DE10-Nano's Cortex-A9 has much less headroom than the desktop
		// used to tune rooms. Keep frame work below a 60 Hz frame while
		// allowing for normal GC/scheduler jitter on the kit.
		Frame:          12 * time.Millisecond,
		Input:          8 * time.Millisecond,
		MaxOps:         4096,
		MaxImagePixels: 16 << 20,
		MaxQueryGames:  2000,
	}
}

const (
	callbackHardLimitFactor = 4
	maxConsecutiveOverruns  = 8
)

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
	// ReducedMotion skips decorative room animation (focus pulse, tweens,
	// drifting art). The launcher owns the preference; scripts read
	// room.reduced_motion.
	ReducedMotion bool
	// Setup is the first-run setup position the room is opened at, shown to
	// scripts as room.setup.
	Setup Setup
	// ReleaseOnly leaves out held-back cores (release.Shown).
	// The launcher sets it unless its Show all cores preference is on; a
	// change applies to rooms opened after it.
	ReleaseOnly bool
	// InitialGameID carries the selected library entry into a nested setup
	// room. Direct room opens leave it empty and use saved room navigation.
	InitialGameID string
	InitialCoreID string
	// SessionDisplayRequired means this physical display needs an observed
	// session-display contract before it can show controls during play.
	SessionDisplayRequired bool
	// StorePath is the per-room JSON persistence file; empty disables store.
	StorePath string
	// Local is the kit-local control socket. Nil on a host launcher: kit
	// is absent and inventory is unavailable.
	Local  LocalCores
	Budget Budget
	Now    func() time.Time
	Stderr io.Writer
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

	// pads is room.pads(); padsGen changes with every SetPads.
	pads    []Pad
	padsGen uint64

	frame         Frame
	building      *Frame
	inDraw        bool
	frameReady    bool
	reuseFrame    bool
	keepSlowFrame bool
	// extendCopied is set once gfx.extend has copied the presented frame
	// into this draw. A later extend appends without copying it again.
	extendCopied bool
	// seg holds gfx.segment runs of the presented and the building frame.
	seg     segments
	actions []Action
	// firmwareInfo is the latest first-use firmware request, kept apart from
	// actions: a workbench repeats it on every load.
	firmwareInfo *Action
	games        map[string]hostclient.Game
	gameVersion  map[string]uint64
	gameEpoch    uint64
	images       map[string]*Image
	store        *store
	logs         []string

	started       time.Time
	last          time.Time
	elapsed       float64
	fatal         error
	frameOverruns int
	inputOverruns int
	// lastStep is the Lua time of the most recent Step, for frame traces.
	lastStep StepTiming
	// threadSample, when set, replaces the in-window read of the callback
	// thread's CPU time, switches and faults (readThreadUsage). Tests use it.
	// Production leaves it nil.
	threadSample func() (threadUsage, bool)
	// windowSample, when set, replaces the out-of-window usage point read
	// (readWindow). Tests use it. Production leaves it nil.
	windowSample func(base bool) usagePoint
	// usage holds the baseline usage point and its readers. Close closes its
	// schedstat file.
	usage usageWindow
	// work, when set, counts each callback's Lua instructions and draw
	// ops. Tests use it. Production leaves it nil.
	work *workMeter
	// contention summarises system load on a hard deadline miss. New sets
	// the launcher's shared sampler; tests replace it.
	contention *contentionSampler
	// frameStreak and inputStreak describe the current run of consecutive
	// soft overruns, reported when that run retires the room.
	frameStreak     overrunStreak
	inputStreak     overrunStreak
	measures        int
	loaded          bool
	sessionState    string
	roomTable       *lua.LTable
	dest            Destination
	hardwareReading bool
	hardwareSaving  bool
}

type asyncResult struct {
	cb           *lua.LFunction
	err          error
	libraryQuery bool
	transient    bool
	more         bool
	games        []hostclient.Game
	homeFamily   bool
	cancelled    *atomic.Bool
	gameTable    *lua.LTable
	gameIndex    int
	partialGame  *lua.LTable
	partialList  *lua.LTable
	variantIndex int
	queryEpoch   uint64
	variants     []hostclient.Game
	platforms    []hostclient.Platform
	collections  []hostclient.Collection
	game         *hostclient.Game
	imageKey     string
	img          *image.RGBA
	hardware     *hostclient.HardwareSnapshot
	selection    *hostclient.CoreEntryExpansion
	cores        []localcores.Core
	hardwareRead bool
	hardwareSave bool
	// api is the room API whose result this is, such as library.platforms,
	// named in callback errors. library.query results carry libraryQuery.
	api string
}

// source names the API that produced an async result for callback errors.
func (res asyncResult) source() string {
	switch {
	case res.api != "":
		return res.api
	case res.libraryQuery:
		return "library.query"
	default:
		return "result"
	}
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
		pack:       pack,
		opts:       opts,
		budget:     opts.Budget.complete(),
		ctx:        ctx,
		cancel:     cancel,
		modules:    map[string]lua.LValue{},
		loading:    map[string]bool{},
		games:      map[string]hostclient.Game{},
		images:     map[string]*Image{},
		store:      newStore(opts.StorePath, opts.Stderr),
		contention: hostContention,
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

// Revision identifies the installed snapshot this instance keeps reading.
func (r *Instance) Revision() string { return r.pack.Revision }

// FlushState saves current room state before a replacement reads it.
func (r *Instance) FlushState() { r.store.flush() }

// StageState prevents trial load/draw callbacks from publishing state.
func (r *Instance) StageState() { r.store.staged = true }

// AdoptSavedState incorporates outgoing unload saves and commits trial writes.
func (r *Instance) AdoptSavedState() {
	r.store.adoptSaved()
	r.store.staged = false
	r.store.flush()
}

// Discard closes an unsuccessful candidate without persisting trial state.
func (r *Instance) Discard() { r.store.path = ""; r.Close() }

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
		if err := r.callGlobal("unload", r.budget.Input); err != nil {
			fmt.Fprintf(r.opts.Stderr, "rooms: room %s: %v\n", r.pack.ID, err)
		}
	}
	r.cancel()
	r.store.flush()
	r.L.Close()
	r.L = nil
	r.usage.schedstat.close()
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
	r.workKind("main")
	if _, err := r.call(fn, r.budget.Load, 0); err != nil {
		return r.fail(fmt.Errorf("%s chunk: %w", r.pack.Main, err))
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
		r.lastStep = StepTiming{}
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
	// One baseline usage read per Step, outside every callback's elapsed
	// window; an overrunning callback in this Step compares with it.
	r.beginStep()
	defer r.endStep()
	r.contention.tick(time.Now())
	r.roomTable.RawSetString("time", lua.LNumber(r.elapsed))
	r.lastStep = StepTiming{}
	asyncStart := time.Now()
	r.workKind("async")
	r.drainResults()
	updateStart := time.Now()
	r.lastStep.Async = updateStart.Sub(asyncStart)
	defer func() { r.lastStep.Total = time.Since(asyncStart) }()
	if r.fatal != nil {
		return Frame{}
	}
	err := r.callGlobalSoft("update", r.budget.Frame, lua.LNumber(dt))
	r.lastStep.Update = time.Since(updateStart)
	if err != nil {
		if errors.Is(err, errSoftOverrun) {
			return r.overrunFrame(err)
		}
		r.fail(err)
		return Frame{}
	}
	frame := Frame{Clear: r.opts.Theme.SofaBackground}
	r.building = &frame
	r.inDraw = true
	r.reuseFrame = false
	r.keepSlowFrame = false
	r.extendCopied = false
	r.seg.begin()
	drawStart := time.Now()
	err = r.callGlobalSoft("draw", r.budget.Frame)
	r.lastStep.Draw = time.Since(drawStart)
	r.inDraw = false
	built := r.building
	r.building = nil
	if r.work != nil {
		r.work.drew(built, r.seg.copied, r.seg.replayed)
	}
	// gfx.reuse() asks to keep the frame already presented. A slow check
	// still counts, and the next unchanged draw can try again.
	if r.reuseFrame {
		r.reuseFrame = false
		r.keepSlowFrame = false
		if err != nil && !errors.Is(err, errSoftOverrun) {
			r.fail(err)
			return Frame{}
		}
		r.presentFrame(true)
		if err != nil {
			return r.overrunFrame(err)
		}
		r.frameOverruns = 0
		r.store.maybeFlush(now)
		return r.frame
	}
	// A finished gfx.paint is the picture the script built. Present it
	// even when it missed the soft budget, so the next unchanged draw can
	// reuse it instead of painting the same scene until the room is retired.
	if err != nil && errors.Is(err, errSoftOverrun) && r.keepSlowFrame && built != nil && built.HasClear {
		r.keepSlowFrame = false
		r.frame = *built
		r.frameReady = true
		r.seg.present()
		r.presentFrame(true)
		r.recordFrameOverrun(err)
		if r.fatal != nil {
			return Frame{}
		}
		r.store.maybeFlush(now)
		return r.frame
	}
	r.keepSlowFrame = false
	if err != nil {
		if errors.Is(err, errSoftOverrun) {
			r.presentFrame(false)
			return r.overrunFrame(err)
		}
		r.fail(err)
		return Frame{}
	}
	r.frame = frame
	r.frameReady = true
	r.frameOverruns = 0
	r.seg.present()
	r.presentFrame(true)
	r.store.maybeFlush(now)
	return frame
}

// presentFrame tells the script whether its last draw is on screen. A draw
// that is not presented drops every gfx.segment run.
func (r *Instance) presentFrame(ok bool) {
	if !ok {
		r.seg.drop()
	}
	if r.roomTable != nil {
		r.roomTable.RawSetString("presented", lua.LBool(ok))
	}
}

// reservedRoomCommand is owned by FES. Scripts may style prompts; they must
// not swallow Back's host binding or the system menu.
func reservedRoomCommand(cmd string) bool {
	switch strings.TrimSpace(strings.ToLower(cmd)) {
	case "settings", "home":
		return true
	default:
		return false
	}
}

// Input forwards a launcher command name. It reports whether the script
// consumed it. Settings and Home are never delivered.
func (r *Instance) Input(cmd string) bool {
	if r.fatal != nil || !r.loaded {
		return false
	}
	if reservedRoomCommand(cmd) {
		return false
	}
	fn, ok := r.L.GetGlobal("on_input").(*lua.LFunction)
	if !ok {
		return false
	}
	r.noteInputBaseline()
	r.workKind("on_input")
	handled, err := r.callRetSoft("on_input", fn, r.budget.Input, lua.LString(cmd))
	if err != nil {
		if errors.Is(err, errSoftOverrun) {
			r.overrunInput(err)
		} else {
			r.fail(err)
			return true
		}
	} else {
		r.inputOverruns = 0
	}
	return lua.LVAsBool(handled)
}

// StepTiming is where one Step's time went: async result callbacks,
// update, draw, and the whole Step including frame bookkeeping.
type StepTiming struct {
	Async, Update, Draw, Total time.Duration
}

// LastStep reports the most recent Step's timing.
func (r *Instance) LastStep() StepTiming {
	if r == nil {
		return StepTiming{}
	}
	return r.lastStep
}
