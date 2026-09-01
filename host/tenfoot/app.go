package tenfoot

import (
	"context"
	"fmt"
	"image"
	"sync"
	"time"
)

const (
	coverWorkers   = 6
	prefetchRows   = 2
	maxInflight    = 8
	coverJobBuffer = 128
)

type coverPhase int

const (
	coverIdle coverPhase = iota
	coverPresentation
	coverArtwork
	coverReady
	coverMissing
	coverFailed
)

type coverSlot struct {
	phase   coverPhase
	handle  string
	image   *image.RGBA
	message string
}

type workKind int

const (
	workPresentation workKind = iota
	workArtwork
)

type workItem struct {
	kind   workKind
	gameID string
	handle string
}

type workResult struct {
	kind    workKind
	gameID  string
	handle  string
	image   *image.RGBA
	err     error
	missing bool
}

// LaunchSnapshot is the current host launch attempt.
type LaunchSnapshot struct {
	GameID       string
	Phase        string
	Message      string
	HTTPStatus   int
	State        string
	ErrorCode    string
	ErrorMessage string
}

// Snapshot is a frame-loop readable copy of launcher state.
type Snapshot struct {
	Games     []Game
	Grid      Grid
	Status    string
	LoadErr   string
	Loading   bool
	Covers    map[string]*image.RGBA
	Launch    LaunchSnapshot
	Gamepads  int
	CoverHits int
}

// App owns catalog, focus, async covers, and host launch. SDL stays out.
type App struct {
	client *Client
	ctx    context.Context
	cancel context.CancelFunc

	mu        sync.Mutex
	games     []Game
	grid      Grid
	covers    map[string]*coverSlot
	inflight  map[string]workKind
	status    string
	loadErr   string
	loading   bool
	launch    LaunchSnapshot
	gamepads  int
	repeat    Repeater
	jobs      chan workItem
	results   chan workResult
	maxGames  int
	pageLimit int
}

// NewApp builds a launcher model bound to the host API client.
func NewApp(client *Client, width, height, maxGames int) *App {
	if client == nil {
		client = NewClient("", nil)
	}
	if maxGames <= 0 {
		maxGames = defaultMaxGames
	}
	grid := Grid{}
	grid.Layout(width, height)
	return &App{
		client:    client,
		games:     []Game{},
		grid:      grid,
		covers:    map[string]*coverSlot{},
		inflight:  map[string]workKind{},
		status:    "connecting to host API",
		launch:    LaunchSnapshot{Phase: "idle"},
		jobs:      make(chan workItem, coverJobBuffer),
		results:   make(chan workResult, coverJobBuffer),
		maxGames:  maxGames,
		pageLimit: defaultPageLimit,
	}
}

// Start loads the library in the background and runs cover workers.
func (a *App) Start(parent context.Context) {
	a.mu.Lock()
	if a.cancel != nil {
		a.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	a.ctx = ctx
	a.cancel = cancel
	a.loading = true
	a.mu.Unlock()
	for i := 0; i < coverWorkers; i++ {
		go a.worker(ctx)
	}
	go a.loadLibrary(ctx)
}

// Stop cancels background work.
func (a *App) Stop() {
	a.mu.Lock()
	cancel := a.cancel
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// HandleCommand applies a gamepad or debug-keyboard command.
func (a *App) HandleCommand(cmd Command, now time.Time) {
	if cmd == CmdNone {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	switch cmd {
	case CmdUp:
		a.grid.Move(0, -1)
	case CmdDown:
		a.grid.Move(0, 1)
	case CmdLeft:
		a.grid.Move(-1, 0)
	case CmdRight:
		a.grid.Move(1, 0)
	case CmdSelect:
		a.startLaunchLocked()
	case CmdBack:
		if a.launch.Phase == "launching" {
			a.status = "launch in progress"
		}
	}
}

// Press records a button down, including hold-repeat bookkeeping.
func (a *App) Press(cmd Command, now time.Time) Command {
	fired := a.repeat.Down(cmd, now)
	if fired != CmdNone && fired != CmdQuit {
		a.HandleCommand(fired, now)
	}
	return fired
}

// Release records a button up.
func (a *App) Release(cmd Command) {
	a.repeat.Up(cmd)
}

// Tick drains async work and queues visible covers. It must not block.
func (a *App) Tick(now time.Time) Command {
	a.drainResults()
	a.queueVisibleWork()
	if cmd := a.repeat.Tick(now); cmd != CmdNone {
		a.HandleCommand(cmd, now)
		return cmd
	}
	return CmdNone
}

// Snapshot copies state for rendering. Cover images are shared, not cloned.
func (a *App) Snapshot() Snapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	games := append([]Game(nil), a.games...)
	covers := make(map[string]*image.RGBA, len(a.covers))
	hits := 0
	for id, slot := range a.covers {
		if slot != nil && slot.image != nil {
			covers[id] = slot.image
			hits++
		}
	}
	status := a.status
	if a.launch.Phase != "idle" && a.launch.Message != "" {
		status = a.launch.Message
	}
	return Snapshot{
		Games:     games,
		Grid:      a.grid,
		Status:    status,
		LoadErr:   a.loadErr,
		Loading:   a.loading,
		Covers:    covers,
		Launch:    a.launch,
		Gamepads:  a.gamepads,
		CoverHits: hits,
	}
}

// SetGamepads records how many gamepads SDL currently has open.
func (a *App) SetGamepads(n int) {
	a.mu.Lock()
	a.gamepads = n
	a.mu.Unlock()
}

// Selected returns the focused game, if any.
func (a *App) Selected() (Game, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.grid.Focus < 0 || a.grid.Focus >= len(a.games) {
		return Game{}, false
	}
	return a.games[a.grid.Focus], true
}

func (a *App) startLaunchLocked() {
	if a.launch.Phase == "launching" {
		return
	}
	if a.grid.Focus < 0 || a.grid.Focus >= len(a.games) {
		a.launch = LaunchSnapshot{Phase: "error", Message: "no title selected"}
		return
	}
	game := a.games[a.grid.Focus]
	if reason := launchBlockReason(game); reason != "" {
		a.launch = LaunchSnapshot{GameID: game.ID, Phase: "error", Message: reason}
		return
	}
	a.launch = LaunchSnapshot{
		GameID:  game.ID,
		Phase:   "launching",
		Message: "launching " + game.Title,
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go a.doLaunch(ctx, game)
}

// launchBlockReason mirrors ui_shell launchBlockReason: unavailable titles
// must not POST /api/v1/session/launch.
func launchBlockReason(game Game) string {
	if !game.Launchable {
		return "This platform is browse-only on this host."
	}
	if game.State == "missing" || !game.RootOnline {
		return "This game's source is offline."
	}
	if game.State == "invalid" {
		return "This ROM can't be read."
	}
	if game.State != "available" {
		return "This game isn't ready to launch."
	}
	return ""
}

func (a *App) doLaunch(ctx context.Context, game Game) {
	result, err := a.client.Launch(ctx, game.ID)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.launch.GameID != game.ID {
		return
	}
	if err != nil {
		a.launch.Phase = "error"
		a.launch.Message = "launch failed: " + err.Error()
		return
	}
	a.launch.HTTPStatus = result.HTTPStatus
	a.launch.ErrorCode = result.ErrorCode
	a.launch.ErrorMessage = result.ErrorMessage
	a.launch.State = result.State
	if result.ErrorCode != "" {
		a.launch.Phase = "host"
		a.launch.Message = fmt.Sprintf("host launch %d %s: %s", result.HTTPStatus, result.ErrorCode, result.ErrorMessage)
		return
	}
	a.launch.Phase = "ok"
	a.launch.Message = fmt.Sprintf("host accepted launch for %s", game.ID)
}

func (a *App) loadLibrary(ctx context.Context) {
	var (
		all    []Game
		cursor string
	)
	for {
		if err := ctx.Err(); err != nil {
			a.setLoadError(err.Error())
			return
		}
		page, next, err := a.client.ListGames(ctx, cursor, a.pageLimit)
		if err != nil {
			a.setLoadError(err.Error())
			return
		}
		remain := a.maxGames - len(all)
		if remain <= 0 {
			break
		}
		if len(page) > remain {
			page = page[:remain]
		}
		all = append(all, page...)
		a.mu.Lock()
		a.games = append([]Game(nil), all...)
		a.grid.SetCount(len(a.games))
		a.status = fmt.Sprintf("%d titles from host API", len(a.games))
		a.mu.Unlock()
		if next == "" || len(all) >= a.maxGames {
			break
		}
		cursor = next
	}
	a.mu.Lock()
	a.loading = false
	if len(a.games) == 0 {
		a.status = "host API returned no titles"
	}
	a.mu.Unlock()
}

func (a *App) setLoadError(message string) {
	a.mu.Lock()
	a.loading = false
	a.loadErr = message
	a.status = "library load failed"
	a.mu.Unlock()
}

func (a *App) drainResults() {
	for {
		select {
		case result := <-a.results:
			a.applyResult(result)
		default:
			return
		}
	}
}

func (a *App) applyResult(result workResult) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.inflight, result.gameID)
	if !a.inPrefetchLocked(result.gameID) {
		delete(a.covers, result.gameID)
		return
	}
	slot := a.covers[result.gameID]
	if slot == nil {
		slot = &coverSlot{}
		a.covers[result.gameID] = slot
	}
	if result.err != nil {
		slot.phase = coverFailed
		slot.message = result.err.Error()
		return
	}
	switch result.kind {
	case workPresentation:
		if result.missing || result.handle == "" {
			slot.phase = coverMissing
			return
		}
		slot.handle = result.handle
		slot.phase = coverArtwork
	case workArtwork:
		slot.image = result.image
		slot.phase = coverReady
	}
}

func (a *App) queueVisibleWork() {
	a.mu.Lock()
	start, end := a.prefetchSpanLocked()
	type pending struct {
		item workItem
		key  string
	}
	var queue []pending
	for i := start; i < end; i++ {
		if len(a.inflight) >= maxInflight {
			break
		}
		game := a.games[i]
		slot := a.covers[game.ID]
		if slot == nil {
			slot = &coverSlot{phase: coverIdle}
			if handle := normalizeHandle(game.Cover); handle != "" {
				slot.handle = handle
				slot.phase = coverArtwork
			}
			a.covers[game.ID] = slot
		}
		if _, busy := a.inflight[game.ID]; busy {
			continue
		}
		switch slot.phase {
		case coverIdle:
			a.inflight[game.ID] = workPresentation
			queue = append(queue, pending{item: workItem{kind: workPresentation, gameID: game.ID}, key: game.ID})
		case coverArtwork:
			if slot.handle == "" {
				slot.phase = coverMissing
				continue
			}
			a.inflight[game.ID] = workArtwork
			queue = append(queue, pending{item: workItem{kind: workArtwork, gameID: game.ID, handle: slot.handle}, key: game.ID})
		}
		if len(a.inflight) >= maxInflight {
			break
		}
	}
	a.evictCoversLocked()
	a.mu.Unlock()
	for _, item := range queue {
		select {
		case a.jobs <- item.item:
		default:
			a.mu.Lock()
			delete(a.inflight, item.key)
			a.mu.Unlock()
		}
	}
}

func (a *App) prefetchSpanLocked() (int, int) {
	start, end := a.grid.PrefetchRange(prefetchRows)
	if start < 0 {
		start = 0
	}
	if end > len(a.games) {
		end = len(a.games)
	}
	return start, end
}

func (a *App) inPrefetchLocked(gameID string) bool {
	start, end := a.prefetchSpanLocked()
	for i := start; i < end; i++ {
		if a.games[i].ID == gameID {
			return true
		}
	}
	return false
}

func (a *App) evictCoversLocked() {
	start, end := a.prefetchSpanLocked()
	keep := make(map[string]struct{}, end-start)
	for i := start; i < end; i++ {
		keep[a.games[i].ID] = struct{}{}
	}
	for id := range a.covers {
		if _, ok := keep[id]; ok {
			continue
		}
		if _, busy := a.inflight[id]; busy {
			continue
		}
		delete(a.covers, id)
	}
}

func (a *App) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case item := <-a.jobs:
			result := a.doWork(ctx, item)
			select {
			case a.results <- result:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (a *App) doWork(ctx context.Context, item workItem) workResult {
	switch item.kind {
	case workPresentation:
		pres, err := a.client.GamePresentation(ctx, item.gameID)
		if err != nil {
			return workResult{kind: workPresentation, gameID: item.gameID, err: err}
		}
		handle := CoverHandle(Game{ID: item.gameID}, pres)
		return workResult{kind: workPresentation, gameID: item.gameID, handle: handle, missing: handle == ""}
	case workArtwork:
		data, _, err := a.client.Artwork(ctx, item.handle)
		if err != nil {
			return workResult{kind: workArtwork, gameID: item.gameID, err: err}
		}
		img, err := DecodeCover(data)
		if err != nil {
			return workResult{kind: workArtwork, gameID: item.gameID, err: err}
		}
		return workResult{kind: workArtwork, gameID: item.gameID, handle: item.handle, image: img}
	default:
		return workResult{kind: item.kind, gameID: item.gameID, err: fmt.Errorf("unknown work")}
	}
}
