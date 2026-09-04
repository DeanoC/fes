package tenfoot

import (
	"context"
	"errors"
	"fmt"
	"image"
	"strings"
	"sync"
	"time"
)

const (
	coverWorkers         = 6
	prefetchRows         = 2
	maxInflight          = 8
	coverJobBuffer       = 128
	searchDebounce       = 280 * time.Millisecond
	presentationRetryMin = 400 * time.Millisecond
	presentationRetryMax = 8 * time.Second
	sessionPollInterval  = time.Second
)

var catalogSorts = []string{"title", "recently_added", "platform"}

// recentsSorts are the orders queryRecents actually applies: last-played,
// title, and platform. recently_added is omitted because the host ignores it.
var recentsSorts = []string{"", "title", "platform"}

// recentlyAddedSorts match QueryGames rewriting empty/title to added-date order
// while still honoring an explicit platform sort.
var recentlyAddedSorts = []string{"recently_added", "platform"}

// LibraryView is one All / smart-rail / custom collection choice.
type LibraryView struct {
	ID     string
	Label  string
	Create bool
	Custom bool
	Member bool
}

var smartLibraryViews = []LibraryView{
	{ID: "", Label: "All"},
	{ID: "continue", Label: "Continue"},
	{ID: "favorites", Label: "Favorites"},
	{ID: "recents", Label: "Recent"},
	{ID: "unplayed", Label: "Unplayed"},
	{ID: "recently_added", Label: "Recently added"},
}

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
	phase      coverPhase
	handle     string
	image      *image.RGBA
	message    string
	detailTry  int
	detailNext time.Time
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
	gen    int
}

type workResult struct {
	kind        workKind
	gameID      string
	handle      string
	image       *image.RGBA
	err         error
	missing     bool
	state       string
	year        string
	genre       string
	summary     string
	attribution string
	gen         int
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

// SessionSnapshot is the live host session from GET /api/v1/session (and launch/stop).
type SessionSnapshot struct {
	State      string
	GameID     string
	Title      string
	System     string
	Execution  string
	Media      string
	InputState string
	Progress   string
	Stopping   bool
}

// FocusDetail is the focused title's metadata shown in the detail strip.
type FocusDetail struct {
	Title       string
	Platform    string
	Year        string
	Genre       string
	Summary     string
	Attribution string
	Favorite    bool
}

// MetaFacts joins platform, year, and genre for the detail strip.
func (d FocusDetail) MetaFacts() string {
	parts := make([]string, 0, 3)
	for _, part := range []string{d.Platform, d.Year, d.Genre} {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "  ·  ")
}

// MetaLine joins optional metadata and provider attribution.
func (d FocusDetail) MetaLine() string {
	parts := make([]string, 0, 2)
	for _, part := range []string{d.MetaFacts(), d.Attribution} {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "  ·  ")
}

const detailMetaGap = 12

// layoutDetailMeta gives attribution its own reserved width so fitLabel cannot
// clip provenance off a shared facts line.
func layoutDetailMeta(d FocusDetail, x, maxWidth, sizePx int) (facts string, factsX, factsW int, attr string, attrX, attrW int) {
	facts = d.MetaFacts()
	attr = strings.TrimSpace(d.Attribution)
	if maxWidth < 1 {
		return facts, x, 0, attr, x, 0
	}
	if attr == "" {
		return facts, x, maxWidth, "", x, 0
	}
	attrW = measureLabel(attr, sizePx)
	if attrW < 1 || attrW > maxWidth {
		attrW = maxWidth
	}
	if facts == "" || attrW+detailMetaGap >= maxWidth {
		return "", x, 0, attr, x, maxWidth
	}
	factsW = maxWidth - attrW - detailMetaGap
	return facts, x, factsW, attr, x + factsW + detailMetaGap, attrW
}

// Snapshot is a frame-loop readable copy of launcher state.
type Snapshot struct {
	Games           []Game
	Grid            Grid
	Status          string
	LoadErr         string
	Loading         bool
	Covers          map[string]*image.RGBA
	Launch          LaunchSnapshot
	Gamepads        int
	CoverHits       int
	Platforms       []Platform
	PlatformID      string
	Sort            string
	Query           string
	SearchOpen      bool
	FocusDetail     FocusDetail
	Collection      string
	ViewLabel       string
	ViewPicker      bool
	ViewPickerIndex int
	Views           []LibraryView
	PickerRows      []LibraryView
	CollectionMenu  CollectionMenuSnapshot
	Session         SessionSnapshot
	GPUParked       bool
	Attract         AttractSnapshot
	SafeAreaPct     float64
	Settings        SettingsSnapshot
	OSK             OSKSnapshot
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

	platforms             []Platform
	platformID            string
	sort                  string
	searchField           TextField
	searchOpen            bool
	searchPending         bool
	searchDue             time.Time
	details               map[string]FocusDetail
	loadGen               int
	keepFocusID           string
	keepFocusIndex        int
	navDirty              bool
	platformErr           string
	platformKick          chan struct{}
	loadCancel            context.CancelFunc
	jobCtx                context.Context
	collectionID          string
	collections           []Collection
	collectionsErr        string
	collectionsKick       chan struct{}
	viewPickerOpen        bool
	viewPickerIndex       int
	nameEntry             nameEntryKind
	nameEntryID           string
	nameField             TextField
	collectionBusy        bool
	membershipBusy        bool
	collectionManageOpen  bool
	collectionManageIndex int
	collectionManageID    string
	collectionManageName  string
	collectionConfirmOpen bool
	settingsOpen          bool
	settingsIndex         int
	settingsGen           int
	settingsWriteGen      int
	settingsPatchSeq      int
	settingsAppliedSeq    int
	settingsLoading       bool
	settingsBusy          bool
	settingsHydrated      bool
	settingsStatus        string
	settingsDraftIdle     int
	settingsDraftRegions  []string
	settingsDraftTarget   string
	settingsRegionIndex   int
	hostSettings          LibrarySettings
	attractPrefEnabled    bool
	attractForcedOff      bool
	favoriteBusy          bool
	hold                  HoldGate
	session               SessionResult
	sessionTitle          string
	sessionGen            int
	stopPhase             string
	stopMessage           string
	gpuParked             bool
	sessionKick           chan struct{}

	safeAreaPct        float64
	prefsPath          string
	attractDisabled    bool
	attractIdle        time.Duration
	attractCycle       time.Duration
	attractIdleSeconds int
	attractIdleReady   bool
	attractIdleAt      time.Time
	attractIdleRefresh time.Duration
	attractTried       map[string]bool
	lastInput          time.Time
	attractActive      bool
	attractItems       []AttractItem
	attractIndex       int
	attractImage       *image.RGBA
	attractHandle      string
	attractTitle       string
	attractLoading     bool
	attractGen         int
	attractCycleAt     time.Time
	attractResults     chan attractResult
	openAttractVideo   func(context.Context, *Client, string) (attractPlayer, error)
	openAttractCached  func(path string) (attractPlayer, error)
	attractPlayer      attractPlayer
	attractVideo       bool
	attractEnded       bool
	attractFrameSeq    int
	attractVideoPath   string
	attractClosed      bool
	attractMediaCancel context.CancelFunc
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
		client:             client,
		games:              []Game{},
		grid:               grid,
		covers:             map[string]*coverSlot{},
		inflight:           map[string]workKind{},
		status:             "connecting to host API",
		launch:             LaunchSnapshot{Phase: "idle"},
		jobs:               make(chan workItem, coverJobBuffer),
		results:            make(chan workResult, coverJobBuffer),
		maxGames:           maxGames,
		pageLimit:          defaultPageLimit,
		sort:               "title",
		details:            map[string]FocusDetail{},
		platformKick:       make(chan struct{}, 1),
		collectionsKick:    make(chan struct{}, 1),
		sessionKick:        make(chan struct{}, 1),
		stopPhase:          "idle",
		attractPrefEnabled: true,
		attractIdle:        attractIdleDuration(defaultAttractIdleSeconds),
		attractCycle:       defaultAttractCycle,
		attractResults:     make(chan attractResult, 4),
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
	a.lastInput = time.Now()
	a.loadGen++
	gen := a.loadGen
	loadCtx := a.replaceLoadContextLocked()
	a.mu.Unlock()
	for i := 0; i < coverWorkers; i++ {
		go a.worker(ctx)
	}
	go a.loadPlatforms(ctx)
	go a.loadCollections(ctx)
	go a.pollSession(ctx)
	go a.loadLibrary(loadCtx, gen)
	go a.hydrateAttractIdle(ctx)
}

// Stop cancels background work.
func (a *App) Stop() {
	a.mu.Lock()
	a.hideAttractLocked()
	a.attractClosed = true
	cancel := a.cancel
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	a.drainAttractResults()
}

// HandleCommand applies a gamepad or debug-keyboard command.
func (a *App) HandleCommand(cmd Command, now time.Time) {
	if cmd == CmdNone {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.noteActivityLocked(now)
	if a.consumeAttractLocked(cmd, now) {
		return
	}
	if a.searchOpen || a.nameEntryOpenLocked() {
		switch cmd {
		case CmdSafeAreaIn, CmdSafeAreaOut, CmdLayoutCycle:
			return
		}
	}
	switch cmd {
	case CmdSafeAreaIn:
		a.setSafeAreaPctLocked(a.safeAreaPct+safeAreaNudge, true)
		a.status = fmt.Sprintf("safe-area %.1f%%", a.safeAreaPct*100)
		return
	case CmdSafeAreaOut:
		a.setSafeAreaPctLocked(a.safeAreaPct-safeAreaNudge, true)
		a.status = fmt.Sprintf("safe-area %.1f%%", a.safeAreaPct*100)
		return
	case CmdLayoutCycle:
		a.cycleLayoutLocked()
		return
	case CmdSettings:
		if a.settingsOpen {
			a.closeSettingsLocked()
		} else {
			a.openSettingsLocked()
		}
		return
	}
	if a.settingsOpen {
		a.handleSettingsLocked(cmd)
		return
	}
	if a.nameEntryOpenLocked() {
		a.handleNameEntryLocked(cmd)
		return
	}
	if a.viewPickerOpen {
		a.handleViewPickerLocked(cmd)
		return
	}
	if a.searchOpen {
		a.handleSearchLocked(cmd, now)
		return
	}
	if a.sessionStopOfferedLocked() && !a.searchOpen {
		switch cmd {
		case CmdBack, CmdStop:
			a.startStopLocked()
			return
		case CmdSelect:
			return
		case CmdUp, CmdDown, CmdLeft, CmdRight, CmdFilterPrev, CmdFilterNext, CmdSortCycle, CmdSearch, CmdViewPrev, CmdViewNext, CmdViewPicker, CmdFavorite, CmdSafeAreaIn, CmdSafeAreaOut:
			return
		}
	}
	switch cmd {
	case CmdUp:
		a.moveFocusLocked(0, -1)
	case CmdDown:
		a.moveFocusLocked(0, 1)
	case CmdLeft:
		a.moveFocusLocked(-1, 0)
	case CmdRight:
		a.moveFocusLocked(1, 0)
	case CmdSelect:
		a.startLaunchLocked()
	case CmdStop:
		a.startStopLocked()
	case CmdBack:
		if a.launch.Phase == "launching" {
			a.status = "launch in progress"
		}
	case CmdFilterPrev:
		a.cyclePlatformLocked(-1)
	case CmdFilterNext:
		a.cyclePlatformLocked(1)
	case CmdSortCycle:
		a.cycleSortLocked()
	case CmdSearch:
		a.openSearchLocked()
	case CmdViewPrev:
		a.cycleViewLocked(-1)
	case CmdViewNext:
		a.cycleViewLocked(1)
	case CmdViewPicker:
		a.openViewPickerLocked()
	case CmdFavorite:
		a.toggleFavoriteLocked()
	}
}

func (a *App) openSearchLocked() {
	a.searchOpen = true
	a.searchField.OSK.Reset()
}

func (a *App) closeSearchApplyLocked() {
	a.searchOpen = false
	a.applyPendingSearchLocked()
}

func (a *App) handleSearchLocked(cmd Command, now time.Time) {
	switch cmd {
	case CmdUp:
		a.searchField.Move(0, -1)
	case CmdDown:
		a.searchField.Move(0, 1)
	case CmdLeft:
		a.searchField.Move(-1, 0)
	case CmdRight:
		a.searchField.Move(1, 0)
	case CmdSelect:
		result := a.searchField.Activate()
		if result.Changed {
			a.markSearchLocked(now)
		}
		if result.Done {
			a.closeSearchApplyLocked()
		}
	case CmdBack:
		if strings.TrimSpace(a.searchField.Buffer) != "" {
			a.searchField.Clear()
			a.searchPending = false
			a.reloadLocked()
			return
		}
		a.searchOpen = false
	case CmdSearch:
		a.searchOpen = false
	case CmdFilterPrev:
		a.searchField.CyclePage(-1)
	case CmdFilterNext:
		a.searchField.CyclePage(1)
	case CmdSortCycle:
		a.cycleSortLocked()
	case CmdStop:
		a.startStopLocked()
	}
}

// TypeText appends into the on-screen search field from a physical keyboard.
func (a *App) TypeText(text string, now time.Time) {
	text = sanitizeFieldText(text)
	if text == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.noteActivityLocked(now)
	if a.consumeAttractLocked(CmdNone, now) {
		return
	}
	if a.nameEntryOpenLocked() {
		a.nameField.Insert(text)
		return
	}
	if !a.searchOpen {
		return
	}
	a.searchField.Insert(text)
	a.markSearchLocked(now)
}

// SearchBackspace deletes the last search rune.
func (a *App) SearchBackspace(now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.noteActivityLocked(now)
	if a.consumeAttractLocked(CmdBack, now) {
		return
	}
	if a.nameEntryOpenLocked() {
		if a.nameField.Buffer == "" {
			return
		}
		a.nameField.Backspace()
		return
	}
	if !a.searchOpen || a.searchField.Buffer == "" {
		return
	}
	a.searchField.Backspace()
	a.markSearchLocked(now)
}

// ConfirmSearch closes the OSK and applies a pending query (physical Enter).
func (a *App) ConfirmSearch(now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.noteActivityLocked(now)
	if a.consumeAttractLocked(CmdSelect, now) {
		return
	}
	if a.nameEntryOpenLocked() {
		a.submitNameEntryLocked()
		return
	}
	if !a.searchOpen {
		return
	}
	a.closeSearchApplyLocked()
}

func (a *App) markSearchLocked(now time.Time) {
	a.searchPending = true
	a.searchDue = now.Add(searchDebounce)
}

func (a *App) flushSearchLocked(now time.Time) {
	if !a.searchPending {
		return
	}
	if !now.IsZero() && now.Before(a.searchDue) {
		return
	}
	a.applyPendingSearchLocked()
}

func (a *App) applyPendingSearchLocked() {
	if !a.searchPending {
		return
	}
	a.searchPending = false
	a.reloadLocked()
}

func (a *App) cyclePlatformLocked(delta int) {
	choices := a.platformChoicesLocked()
	if len(choices) < 2 {
		if a.platformErr != "" {
			a.requestPlatformReloadLocked()
		}
		return
	}
	idx := 0
	for i, id := range choices {
		if id == a.platformID {
			idx = i
			break
		}
	}
	idx = (idx + delta) % len(choices)
	if idx < 0 {
		idx += len(choices)
	}
	next := choices[idx]
	if next == a.platformID {
		return
	}
	a.platformID = next
	a.reloadLocked()
}

func (a *App) cycleSortLocked() {
	sorts := a.sortChoicesLocked()
	idx := 0
	for i, sort := range sorts {
		if sort == a.sort {
			idx = i
			break
		}
	}
	a.sort = sorts[(idx+1)%len(sorts)]
	a.reloadLocked()
}

func (a *App) sortChoicesLocked() []string {
	switch a.collectionID {
	case "recents":
		return recentsSorts
	case "recently_added":
		return recentlyAddedSorts
	default:
		return catalogSorts
	}
}

func (a *App) cycleViewLocked(delta int) {
	views := a.viewChoicesLocked()
	if len(views) == 0 {
		return
	}
	idx := a.currentViewIndexLocked()
	idx = (idx + delta) % len(views)
	if idx < 0 {
		idx += len(views)
	}
	a.setCollectionLocked(views[idx].ID)
}

func (a *App) setCollectionLocked(id string) {
	if id == a.collectionID {
		return
	}
	a.collectionID = id
	a.normalizeSortForCollectionLocked()
	a.reloadLocked()
}

func (a *App) normalizeSortForCollectionLocked() {
	switch a.collectionID {
	case "recents":
		if a.sort == "platform" || a.sort == "system" {
			a.sort = "platform"
			return
		}
		a.sort = ""
	case "recently_added":
		if a.sort == "platform" || a.sort == "system" {
			a.sort = "platform"
			return
		}
		a.sort = "recently_added"
	default:
		if a.sort == "" {
			a.sort = "title"
		}
	}
}

func (a *App) openViewPickerLocked() {
	a.searchOpen = false
	a.closeNameEntryLocked()
	a.closeCollectionManageLocked()
	a.viewPickerOpen = true
	a.viewPickerIndex = a.currentViewIndexLocked()
	if a.collectionsErr != "" {
		a.requestCollectionsReloadLocked()
	}
}

func (a *App) viewChoicesLocked() []LibraryView {
	views := make([]LibraryView, 0, len(smartLibraryViews)+len(a.collections))
	views = append(views, smartLibraryViews...)
	seen := map[string]struct{}{}
	for _, smart := range smartLibraryViews {
		seen[smart.ID] = struct{}{}
	}
	for _, collection := range a.collections {
		id := strings.TrimSpace(collection.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		label := strings.TrimSpace(collection.Name)
		if label == "" {
			label = id
		}
		views = append(views, LibraryView{ID: id, Label: label})
	}
	return views
}

func (a *App) currentViewIndexLocked() int {
	views := a.viewChoicesLocked()
	for i, view := range views {
		if view.ID == a.collectionID {
			return i
		}
	}
	return 0
}

func (a *App) viewLabelLocked() string {
	views := a.viewChoicesLocked()
	for _, view := range views {
		if view.ID == a.collectionID {
			if view.Label != "" {
				return view.Label
			}
			return view.ID
		}
	}
	if a.collectionID == "" {
		return "All"
	}
	return a.collectionID
}

func (a *App) toggleFavoriteLocked() {
	if a.favoriteBusy {
		return
	}
	if a.grid.Focus < 0 || a.grid.Focus >= len(a.games) {
		a.status = "no title selected"
		return
	}
	game := a.games[a.grid.Focus]
	want := !game.Favorite
	a.setGameFavoriteLocked(game.ID, want)
	a.favoriteBusy = true
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go a.doFavorite(ctx, game.ID, want)
}

func (a *App) setGameFavoriteLocked(gameID string, favorite bool) {
	idx := -1
	for i, game := range a.games {
		if game.ID == gameID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	games := append([]Game{}, a.games...)
	games[idx].Favorite = favorite
	a.games = games
}

func (a *App) doFavorite(ctx context.Context, gameID string, want bool) {
	err := a.client.SetFavorite(ctx, gameID, want)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.favoriteBusy = false
	if err != nil {
		a.setGameFavoriteLocked(gameID, !want)
		a.status = "favorite failed: " + err.Error()
		return
	}
	a.setGameFavoriteLocked(gameID, want)
	if !want && a.collectionID == "favorites" {
		a.reloadLocked()
		return
	}
	a.status = a.libraryStatusLocked()
}

func (a *App) platformChoicesLocked() []string {
	ids := make([]string, 0, len(a.platforms)+1)
	ids = append(ids, "")
	seen := map[string]struct{}{"": {}}
	for _, platform := range a.platforms {
		id := strings.TrimSpace(platform.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func (a *App) reloadLocked() {
	a.captureCatalogFocusLocked()
	a.searchPending = false
	a.loadGen++
	gen := a.loadGen
	a.loading = true
	a.loadErr = ""
	a.status = "loading library"
	ctx := a.replaceLoadContextLocked()
	go a.loadLibrary(ctx, gen)
}

func (a *App) replaceLoadContextLocked() context.Context {
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	if a.loadCancel != nil {
		a.loadCancel()
	}
	ctx, cancel := context.WithCancel(parent)
	a.loadCancel = cancel
	a.jobCtx = ctx
	a.inflight = map[string]workKind{}
	for {
		select {
		case <-a.jobs:
		default:
			return ctx
		}
	}
}

// NoteActivity records input so idle/attract timers reset.
func (a *App) NoteActivity(now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.noteActivityLocked(now)
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
	a.drainAttractResults()
	a.mu.Lock()
	a.flushSearchLocked(now)
	a.tickAttractLocked(now)
	a.mu.Unlock()
	a.queueVisibleWork(now)
	if cmd := a.hold.Tick(now); cmd != CmdNone {
		a.HandleCommand(cmd, now)
		return cmd
	}
	if cmd := a.repeat.Tick(now); cmd != CmdNone {
		a.HandleCommand(cmd, now)
		return cmd
	}
	return CmdNone
}

// Snapshot copies renderer-facing state. The games slice is shared: loadLibrary
// replaces it rather than mutating elements, so the frame loop does not clone
// the catalog. Cover images are shared, not cloned; the cover map is copied.
func (a *App) Snapshot() Snapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	games := a.games
	covers := make(map[string]*image.RGBA, len(a.covers))
	hits := 0
	for id, slot := range a.covers {
		if slot != nil && slot.image != nil {
			covers[id] = slot.image
			hits++
		}
	}
	status := a.status
	if line := a.stopStatusLocked(); line != "" {
		status = line
	} else if a.launch.Phase != "idle" && a.launch.Phase != "ok" && a.launch.Message != "" {
		status = a.launch.Message
	} else if line := a.nowPlayingStatusLocked(); line != "" {
		status = line
	} else if a.launch.Phase == "ok" && a.launch.Message != "" {
		status = a.launch.Message
	}
	if a.platformErr != "" {
		if status == "" {
			status = "platform list failed"
		} else {
			status = status + " · platform list failed"
		}
	}
	if a.collectionsErr != "" {
		if status == "" {
			status = "collection list failed"
		} else {
			status = status + " · collection list failed"
		}
	}
	return Snapshot{
		Games:           games,
		Grid:            a.grid,
		Status:          status,
		LoadErr:         a.loadErr,
		Loading:         a.loading,
		Covers:          covers,
		Launch:          a.launch,
		Gamepads:        a.gamepads,
		CoverHits:       hits,
		Platforms:       a.platforms,
		PlatformID:      a.platformID,
		Sort:            a.sort,
		Query:           a.searchField.Buffer,
		SearchOpen:      a.searchOpen,
		FocusDetail:     a.focusDetailLocked(),
		Collection:      a.collectionID,
		ViewLabel:       a.viewLabelLocked(),
		ViewPicker:      a.viewPickerOpen,
		ViewPickerIndex: a.viewPickerIndex,
		Views:           a.viewChoicesLocked(),
		PickerRows:      a.pickerRowsLocked(),
		CollectionMenu:  a.collectionMenuSnapshotLocked(),
		Session:         a.sessionSnapshotLocked(),
		GPUParked:       a.gpuParked,
		Attract:         a.attractSnapshotLocked(),
		SafeAreaPct:     a.safeAreaPct,
		Settings:        a.settingsSnapshotLocked(),
		OSK:             a.oskSnapshotLocked(),
	}
}

// SetGamepads records how many gamepads SDL currently has open.
func (a *App) SetGamepads(n int) {
	a.mu.Lock()
	a.gamepads = n
	a.mu.Unlock()
}

func (a *App) oskSnapshotLocked() OSKSnapshot {
	if a.nameEntryOpenLocked() {
		snap := a.nameField.Snapshot()
		snap.Open = true
		snap.Prompt = "Collection name"
		return snap
	}
	if !a.searchOpen {
		return OSKSnapshot{}
	}
	snap := a.searchField.Snapshot()
	snap.Open = true
	snap.Prompt = "Search"
	return snap
}

// SearchOpen reports whether the on-screen search field is active.
func (a *App) SearchOpen() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.searchOpen
}

// ViewPickerOpen reports whether the library view list is on screen.
func (a *App) ViewPickerOpen() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.viewPickerOpen
}

// ChromeLine is the header label: now-playing while a session is active, otherwise browse chrome.
func (s Snapshot) ChromeLine() string {
	if s.GPUParked || s.Session.State == "active" {
		line := s.NowPlayingLine()
		if strings.TrimSpace(s.Status) != "" && s.Status != line {
			return line + "  ·  " + s.Status
		}
		return line
	}
	view := strings.TrimSpace(s.ViewLabel)
	if view == "" {
		view = "All"
	}
	platform := "All"
	if s.PlatformID != "" {
		platform = s.PlatformID
		for _, row := range s.Platforms {
			if row.ID == s.PlatformID {
				if label := strings.TrimSpace(row.Label); label != "" {
					platform = label
				}
				break
			}
		}
	}
	search := "Search"
	if s.OSK.Open && s.OSK.Prompt != "" && s.OSK.Prompt != "Search" {
		search = s.OSK.Prompt + ": " + s.OSK.Buffer + "_"
	} else if s.SearchOpen {
		search = "Search: " + s.Query + "_"
	} else if strings.TrimSpace(s.Query) != "" {
		search = "Search: " + s.Query
	}
	return fmt.Sprintf("%s  ·  %s  ·  %s  ·  %s  ·  %s", view, platform, sortLabel(s.Collection, s.Sort), search, s.Status)
}

// NowPlayingLine is the compact active-session chrome.
func (s Snapshot) NowPlayingLine() string {
	parts := make([]string, 0, 6)
	parts = append(parts, "Now playing")
	title := strings.TrimSpace(s.Session.Title)
	if title == "" {
		title = strings.TrimSpace(s.Session.GameID)
	}
	if title != "" {
		parts = append(parts, title)
	}
	if state := strings.TrimSpace(s.Session.State); state != "" {
		parts = append(parts, state)
	}
	if exec := strings.TrimSpace(s.Session.Execution); exec != "" {
		parts = append(parts, exec)
	}
	if media := strings.TrimSpace(s.Session.Media); media != "" {
		parts = append(parts, "media "+media)
	}
	if input := strings.TrimSpace(s.Session.InputState); input != "" {
		parts = append(parts, "input "+input)
	}
	if s.Session.Stopping {
		parts = append(parts, "stopping")
	}
	return strings.Join(parts, "  ·  ")
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

func (a *App) focusDetailLocked() FocusDetail {
	if a.grid.Focus < 0 || a.grid.Focus >= len(a.games) {
		return FocusDetail{}
	}
	game := a.games[a.grid.Focus]
	detail := FocusDetail{
		Title:    game.Title,
		Platform: a.platformLabelLocked(game.System),
		Year:     strings.TrimSpace(game.Year),
		Genre:    strings.TrimSpace(game.Genre),
		Favorite: game.Favorite,
	}
	if cached, ok := a.details[game.ID]; ok {
		if strings.TrimSpace(cached.Year) != "" {
			detail.Year = cached.Year
		}
		if strings.TrimSpace(cached.Genre) != "" {
			detail.Genre = cached.Genre
		}
		detail.Summary = strings.TrimSpace(cached.Summary)
		detail.Attribution = strings.TrimSpace(cached.Attribution)
	}
	return detail
}

func (a *App) platformLabelLocked(system string) string {
	system = strings.TrimSpace(system)
	for _, platform := range a.platforms {
		if platform.ID == system {
			if label := strings.TrimSpace(platform.Label); label != "" {
				return label
			}
			return platform.ID
		}
	}
	if a.platformID != "" && a.platformID == system {
		return a.platformID
	}
	return system
}

func (a *App) moveFocusLocked(dx, dy int) {
	before := a.grid.Focus
	a.grid.Move(dx, dy)
	if a.grid.Focus != before {
		a.navDirty = true
	}
}

func (a *App) focusIndex(i int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if i < 0 || i >= len(a.games) {
		return false
	}
	if a.grid.Focus != i {
		a.navDirty = true
	}
	a.grid.Focus = i
	a.grid.ensureVisible()
	return true
}

func (a *App) startLaunchLocked() {
	if a.launch.Phase == "launching" || a.sessionStopOfferedLocked() {
		return
	}
	if a.searchPending {
		a.applyPendingSearchLocked()
		return
	}
	if a.loading {
		return
	}
	if a.grid.Focus < 0 || a.grid.Focus >= len(a.games) {
		a.launch = LaunchSnapshot{Phase: "error", Message: "no title selected"}
		return
	}
	a.startLaunchGameLocked(a.games[a.grid.Focus])
}

func (a *App) startLaunchGameLocked(game Game) {
	if a.launch.Phase == "launching" || a.sessionStopOfferedLocked() {
		return
	}
	if reason := launchBlockReason(game); reason != "" {
		a.launch = LaunchSnapshot{GameID: game.ID, Phase: "error", Message: reason}
		return
	}
	a.sessionTitle = game.Title
	a.launch = LaunchSnapshot{
		GameID:  game.ID,
		Phase:   "launching",
		Message: "launching " + game.Title,
	}
	a.bumpSessionGenLocked()
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
	a.bumpSessionGenLocked()
	if err != nil {
		a.launch.Phase = "error"
		a.launch.Message = "launch failed: " + err.Error()
		a.launch.GameID = ""
		return
	}
	a.launch.HTTPStatus = result.HTTPStatus
	a.launch.ErrorCode = result.ErrorCode
	a.launch.ErrorMessage = result.ErrorMessage
	a.launch.State = result.State
	if result.ErrorCode != "" {
		a.launch.Phase = "host"
		a.launch.Message = fmt.Sprintf("host launch %d %s: %s", result.HTTPStatus, result.ErrorCode, result.ErrorMessage)
		a.launch.GameID = ""
		return
	}
	a.launch.Phase = "ok"
	a.launch.Message = fmt.Sprintf("host accepted launch for %s", game.ID)
	a.applySessionLocked(result)
	if a.session.State == "active" && a.session.GameID == "" {
		a.session.GameID = game.ID
	}
	a.kickSessionPollLocked()
}

func (a *App) sessionStopOfferedLocked() bool {
	return a.session.State == "active" || a.stopPhase == "stopping"
}

func (a *App) startStopLocked() {
	if a.stopPhase == "stopping" {
		return
	}
	if a.session.State != "active" {
		return
	}
	a.stopPhase = "stopping"
	a.stopMessage = "stopping session"
	a.bumpSessionGenLocked()
	a.syncGPUParkLocked()
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go a.doStop(ctx)
}

func (a *App) doStop(ctx context.Context) {
	result, err := a.client.Stop(ctx)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopPhase != "stopping" {
		return
	}
	a.bumpSessionGenLocked()
	if err != nil {
		a.stopPhase = "error"
		a.stopMessage = "stop failed: " + err.Error()
		a.syncGPUParkLocked()
		return
	}
	if result.ErrorCode != "" {
		a.stopPhase = "host"
		a.stopMessage = fmt.Sprintf("host stop %d %s: %s", result.HTTPStatus, result.ErrorCode, result.ErrorMessage)
		a.syncGPUParkLocked()
		return
	}
	a.stopPhase = "ok"
	a.stopMessage = ""
	a.applySessionLocked(result)
	a.kickSessionPollLocked()
}

func (a *App) pollSession(ctx context.Context) {
	a.fetchSession(ctx)
	ticker := time.NewTicker(sessionPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.fetchSession(ctx)
		case <-a.sessionKick:
			a.fetchSession(ctx)
		}
	}
}

func (a *App) fetchSession(ctx context.Context) {
	a.mu.Lock()
	gen := a.sessionGen
	a.mu.Unlock()
	result, err := a.client.Session(ctx)
	if err != nil || ctx.Err() != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if gen != a.sessionGen {
		return
	}
	if a.launch.Phase == "launching" || a.stopPhase == "stopping" {
		return
	}
	a.applySessionLocked(result)
}

func (a *App) bumpSessionGenLocked() {
	a.sessionGen++
}

func (a *App) kickSessionPollLocked() {
	if a.sessionKick == nil {
		return
	}
	select {
	case a.sessionKick <- struct{}{}:
	default:
	}
}

func (a *App) applySessionLocked(result SessionResult) {
	if result.ErrorCode != "" {
		return
	}
	a.session = result
	if result.State != "active" {
		a.session.GameID = ""
		a.session.System = ""
	}
	if a.session.State != "active" && a.stopPhase != "stopping" {
		a.stopPhase = "idle"
		a.stopMessage = ""
		if a.launch.Phase == "ok" {
			a.launch.Phase = "idle"
			a.launch.Message = ""
			a.launch.GameID = ""
		}
	}
	a.syncGPUParkLocked()
}

func (a *App) syncGPUParkLocked() {
	want := a.session.State == "active" || a.stopPhase == "stopping"
	if want == a.gpuParked {
		if want {
			a.closeCollectionOverlaysLocked()
			a.searchOpen = false
			a.closeSettingsLocked()
		}
		return
	}
	a.gpuParked = want
	if !want {
		return
	}
	a.hideAttractLocked()
	a.closeCollectionOverlaysLocked()
	a.searchOpen = false
	a.closeSettingsLocked()
	a.inflight = map[string]workKind{}
	a.covers = map[string]*coverSlot{}
	for {
		select {
		case <-a.jobs:
		default:
			return
		}
	}
}

func (a *App) sessionSnapshotLocked() SessionSnapshot {
	title := strings.TrimSpace(a.sessionTitle)
	if a.session.GameID != "" {
		found := false
		for _, game := range a.games {
			if game.ID == a.session.GameID {
				title = game.Title
				found = true
				break
			}
		}
		if !found && a.launch.GameID != a.session.GameID {
			title = a.session.GameID
		}
		if title == "" {
			title = a.session.GameID
		}
	} else {
		title = ""
	}
	progress := ""
	if a.session.Progress != nil {
		progress = strings.TrimSpace(a.session.Progress.Message)
		if progress == "" {
			progress = strings.TrimSpace(a.session.Progress.Stage)
		}
	}
	inputState := ""
	if a.session.Input != nil {
		inputState = strings.TrimSpace(a.session.Input.State)
	}
	return SessionSnapshot{
		State:      a.session.State,
		GameID:     a.session.GameID,
		Title:      title,
		System:     a.session.System,
		Execution:  a.session.Execution,
		Media:      a.session.Media,
		InputState: inputState,
		Progress:   progress,
		Stopping:   a.stopPhase == "stopping",
	}
}

func (a *App) stopStatusLocked() string {
	if strings.TrimSpace(a.stopMessage) == "" {
		return ""
	}
	switch a.stopPhase {
	case "stopping", "error", "host":
		return a.stopMessage
	default:
		return ""
	}
}

func (a *App) nowPlayingStatusLocked() string {
	if a.session.State != "active" {
		return ""
	}
	if a.session.Progress != nil {
		if msg := strings.TrimSpace(a.session.Progress.Message); msg != "" {
			return msg
		}
		if stage := strings.TrimSpace(a.session.Progress.Stage); stage != "" {
			return stage
		}
	}
	return ""
}

func (a *App) requestPlatformReloadLocked() {
	if a.platformKick == nil {
		return
	}
	select {
	case a.platformKick <- struct{}{}:
	default:
	}
}

func (a *App) loadPlatforms(ctx context.Context) {
	fails := 0
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		platforms, err := a.client.Platforms(ctx)
		if ctx.Err() != nil {
			return
		}
		a.mu.Lock()
		if err == nil {
			a.platforms = platforms
			a.platformErr = ""
			a.mu.Unlock()
			return
		}
		fails++
		a.platformErr = err.Error()
		delay := presentationRetryDelay(fails)
		kick := a.platformKick
		a.mu.Unlock()
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		case <-kick:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
	}
}

func (a *App) requestCollectionsReloadLocked() {
	if a.collectionsKick == nil {
		return
	}
	select {
	case a.collectionsKick <- struct{}{}:
	default:
	}
}

func (a *App) loadCollections(ctx context.Context) {
	fails := 0
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		collections, err := a.client.Collections(ctx)
		if ctx.Err() != nil {
			return
		}
		a.mu.Lock()
		if err == nil {
			a.collections = collections
			a.collectionsErr = ""
			fails = 0
			if a.viewPickerOpen {
				n := len(a.pickerRowsLocked())
				if a.viewPickerIndex >= n {
					a.viewPickerIndex = a.currentViewIndexLocked()
				}
			}
			kick := a.collectionsKick
			a.mu.Unlock()
			select {
			case <-ctx.Done():
				return
			case <-kick:
			}
			continue
		}
		fails++
		a.collectionsErr = err.Error()
		delay := presentationRetryDelay(fails)
		kick := a.collectionsKick
		a.mu.Unlock()
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		case <-kick:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
	}
}

func (a *App) currentQueryLocked() GameListQuery {
	return GameListQuery{
		Limit:      a.pageLimit,
		Platform:   a.platformID,
		Sort:       catalogQuerySort(a.collectionID, a.sort),
		Q:          strings.TrimSpace(a.searchField.Buffer),
		Collection: a.collectionID,
	}
}

func catalogQuerySort(collection, sort string) string {
	switch collection {
	case "recents":
		switch sort {
		case "title":
			return "title"
		case "platform", "system":
			return "platform"
		default:
			return ""
		}
	case "recently_added":
		if sort == "platform" || sort == "system" {
			return "platform"
		}
		return "recently_added"
	default:
		return sort
	}
}

func (a *App) libraryStatusLocked() string {
	n := len(a.games)
	view := a.viewLabelLocked()
	platform := "All"
	if a.platformID != "" {
		platform = a.platformLabelLocked(a.platformID)
	}
	sort := sortLabel(a.collectionID, a.sort)
	if q := strings.TrimSpace(a.searchField.Buffer); q != "" {
		return fmt.Sprintf("%d titles · %s · %s · %s · %q", n, view, platform, sort, q)
	}
	return fmt.Sprintf("%d titles · %s · %s · %s", n, view, platform, sort)
}

func sortLabel(collection, sort string) string {
	if collection == "recents" && (sort == "" || sort == "recently_added") {
		return "Last played"
	}
	switch sort {
	case "recently_added":
		return "Recently added"
	case "platform", "system":
		return "System"
	default:
		return "Title"
	}
}

func wrapWords(text string, maxChars, maxLines int) []string {
	text = strings.TrimSpace(text)
	if text == "" || maxChars < 1 || maxLines < 1 {
		return nil
	}
	words := strings.Fields(text)
	lines := make([]string, 0, maxLines)
	var cur string
	flush := func() bool {
		if cur == "" {
			return len(lines) >= maxLines
		}
		lines = append(lines, cur)
		cur = ""
		return len(lines) >= maxLines
	}
	for _, word := range words {
		if len([]rune(word)) > maxChars {
			word = string([]rune(word)[:maxChars])
		}
		next := word
		if cur != "" {
			next = cur + " " + word
		}
		if len([]rune(next)) <= maxChars {
			cur = next
			continue
		}
		if flush() {
			return lines
		}
		cur = word
	}
	flush()
	return lines
}

// restoreCatalogFocus keeps the pre-reload game when it is still in the catalog.
// If that id is gone, the original index is clamped to the new length instead of
// resetting to 0.
func restoreCatalogFocus(games []Game, keepID string, keepIndex int) int {
	if keepID != "" {
		for i, game := range games {
			if game.ID == keepID {
				return i
			}
		}
	}
	if len(games) == 0 {
		return 0
	}
	if keepIndex >= len(games) {
		return len(games) - 1
	}
	if keepIndex < 0 {
		return 0
	}
	return keepIndex
}

func (a *App) applyCatalogPageLocked(games []Game, keepID string, keepIndex int, pinned *bool, lastFocus *int) {
	if *pinned && *lastFocus >= 0 && (a.grid.Focus != *lastFocus || a.navDirty) {
		*pinned = false
	}
	a.games = games
	a.grid.SetCount(len(a.games))
	if !*pinned {
		return
	}
	a.grid.Focus = restoreCatalogFocus(a.games, keepID, keepIndex)
	a.grid.ensureVisible()
	*lastFocus = a.grid.Focus
}

// captureCatalogFocusLocked records the current title so a superseded reload
// can restore it. An already-cleared catalog or a still-loading partial page
// must not overwrite that capture unless the user moved focus.
func (a *App) captureCatalogFocusLocked() {
	if len(a.games) == 0 {
		return
	}
	if a.loading && !a.navDirty {
		return
	}
	a.keepFocusIndex = a.grid.Focus
	a.keepFocusID = ""
	if a.keepFocusIndex >= 0 && a.keepFocusIndex < len(a.games) {
		a.keepFocusID = a.games[a.keepFocusIndex].ID
	}
	a.navDirty = false
}

func (a *App) loadLibrary(ctx context.Context, gen int) {
	var (
		all    []Game
		cursor string
	)
	a.mu.Lock()
	if gen != a.loadGen {
		a.mu.Unlock()
		return
	}
	a.captureCatalogFocusLocked()
	keepID := a.keepFocusID
	keepIndex := a.keepFocusIndex
	query := a.currentQueryLocked() // fixed for this generation; TypeText does not bump loadGen until debounce
	a.games = nil
	a.grid.SetCount(0)
	pinned := true
	lastFocus := -1
	a.mu.Unlock()
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		a.mu.Lock()
		if gen != a.loadGen {
			a.mu.Unlock()
			return
		}
		a.mu.Unlock()
		query.Cursor = cursor
		page, next, err := a.client.ListGames(ctx, query)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			a.setLoadError(gen, err.Error())
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
		if gen != a.loadGen {
			a.mu.Unlock()
			return
		}
		a.applyCatalogPageLocked(append([]Game(nil), all...), keepID, keepIndex, &pinned, &lastFocus)
		a.status = a.libraryStatusLocked()
		a.mu.Unlock()
		if next == "" || len(all) >= a.maxGames {
			break
		}
		cursor = next
	}
	a.mu.Lock()
	if gen != a.loadGen {
		a.mu.Unlock()
		return
	}
	a.loading = false
	if len(a.games) == 0 {
		a.status = "host API returned no titles"
		if a.platformID != "" || strings.TrimSpace(a.searchField.Buffer) != "" || a.collectionID != "" {
			a.status = a.libraryStatusLocked()
		}
	} else {
		a.status = a.libraryStatusLocked()
	}
	a.mu.Unlock()
}

func (a *App) setLoadError(gen int, message string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if gen != a.loadGen {
		return
	}
	a.loading = false
	a.loadErr = message
	a.status = "library load failed"
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
	if result.gen != a.loadGen {
		return
	}
	if a.gpuParked {
		delete(a.inflight, result.gameID)
		return
	}
	delete(a.inflight, result.gameID)
	if result.err != nil && (errors.Is(result.err, context.Canceled) || errors.Is(result.err, context.DeadlineExceeded)) {
		return
	}
	if result.kind == workPresentation && result.err == nil && presentationComplete(result.state, result.attribution) {
		a.details[result.gameID] = FocusDetail{
			Year:        strings.TrimSpace(result.year),
			Genre:       strings.TrimSpace(result.genre),
			Summary:     strings.TrimSpace(result.summary),
			Attribution: strings.TrimSpace(result.attribution),
		}
	}
	if !a.inPrefetchLocked(result.gameID) {
		delete(a.covers, result.gameID)
		return
	}
	slot := a.covers[result.gameID]
	if slot == nil {
		slot = &coverSlot{}
		a.covers[result.gameID] = slot
	}
	if result.kind == workPresentation {
		if result.err != nil || !presentationComplete(result.state, result.attribution) {
			schedulePresentationRetry(slot)
		} else {
			slot.detailTry = 0
			slot.detailNext = time.Time{}
		}
	}
	if result.err != nil {
		if slot.phase == coverReady {
			return
		}
		if result.kind == workPresentation && slot.handle != "" {
			if slot.phase != coverFailed {
				slot.phase = coverArtwork
			}
			return
		}
		slot.phase = coverFailed
		slot.message = result.err.Error()
		return
	}
	switch result.kind {
	case workPresentation:
		complete := presentationComplete(result.state, result.attribution)
		if slot.phase == coverReady && !complete {
			return
		}
		prevHandle := slot.handle
		if result.handle != "" || result.missing {
			slot.handle = result.handle
		}
		if slot.handle == "" {
			slot.image = nil
			slot.phase = coverMissing
			return
		}
		if slot.handle == prevHandle && (slot.phase == coverReady || slot.phase == coverFailed) {
			return
		}
		if slot.handle != prevHandle {
			slot.image = nil
		}
		slot.phase = coverArtwork
	case workArtwork:
		slot.image = result.image
		slot.phase = coverReady
	}
}

func presentationComplete(state, attribution string) bool {
	state = strings.TrimSpace(state)
	switch strings.ToLower(state) {
	case "offline":
		return false
	case "ready":
		return strings.TrimSpace(attribution) != ""
	case "disabled", "unconfigured", "no_match", "ambiguous":
		return true
	default:
		return true
	}
}

func presentationRetryDelay(fails int) time.Duration {
	if fails < 1 {
		fails = 1
	}
	delay := presentationRetryMin
	for i := 1; i < fails && delay < presentationRetryMax; i++ {
		delay *= 2
	}
	if delay > presentationRetryMax {
		return presentationRetryMax
	}
	return delay
}

func schedulePresentationRetry(slot *coverSlot) {
	if slot == nil {
		return
	}
	slot.detailTry++
	slot.detailNext = time.Now().Add(presentationRetryDelay(slot.detailTry))
}

func (a *App) queueVisibleWork(now time.Time) {
	a.mu.Lock()
	if a.gpuParked || a.attractActive {
		a.mu.Unlock()
		return
	}
	start, end := a.prefetchSpanLocked()
	type pending struct {
		item workItem
		key  string
	}
	var queue []pending
	order := make([]int, 0, end-start)
	if a.grid.Focus >= start && a.grid.Focus < end {
		order = append(order, a.grid.Focus)
	}
	for i := start; i < end; i++ {
		if i == a.grid.Focus {
			continue
		}
		order = append(order, i)
	}
	for _, i := range order {
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
		if _, have := a.details[game.ID]; !have && slot.phase != coverIdle && slot.phase != coverArtwork {
			if _, busy := a.inflight[game.ID]; !busy && i == a.grid.Focus && !now.Before(slot.detailNext) {
				a.inflight[game.ID] = workPresentation
				queue = append(queue, pending{item: workItem{kind: workPresentation, gameID: game.ID, handle: slot.handle, gen: a.loadGen}, key: game.ID})
				if len(a.inflight) >= maxInflight {
					break
				}
			}
		}
		if _, busy := a.inflight[game.ID]; busy {
			continue
		}
		switch slot.phase {
		case coverIdle:
			a.inflight[game.ID] = workPresentation
			queue = append(queue, pending{item: workItem{kind: workPresentation, gameID: game.ID, handle: slot.handle, gen: a.loadGen}, key: game.ID})
		case coverArtwork:
			if slot.handle == "" {
				slot.phase = coverMissing
				continue
			}
			a.inflight[game.ID] = workArtwork
			queue = append(queue, pending{item: workItem{kind: workArtwork, gameID: game.ID, handle: slot.handle, gen: a.loadGen}, key: game.ID})
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
			a.mu.Lock()
			jobCtx := a.jobCtx
			gen := a.loadGen
			parked := a.gpuParked
			a.mu.Unlock()
			if jobCtx == nil {
				jobCtx = ctx
			}
			if item.gen != gen || parked {
				a.mu.Lock()
				if a.loadGen == item.gen {
					delete(a.inflight, item.gameID)
				}
				a.mu.Unlock()
				continue
			}
			result := a.doWork(jobCtx, item)
			if item.gen != gen || jobCtx.Err() != nil {
				a.mu.Lock()
				if a.loadGen == item.gen {
					delete(a.inflight, item.gameID)
				}
				a.mu.Unlock()
				continue
			}
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
			return workResult{kind: workPresentation, gameID: item.gameID, gen: item.gen, err: err}
		}
		handle := CoverHandle(Game{ID: item.gameID}, pres)
		result := workResult{kind: workPresentation, gameID: item.gameID, handle: handle, missing: handle == "", state: pres.State, gen: item.gen}
		if pres.Presentation != nil {
			result.year = pres.Presentation.Year
			result.genre = pres.Presentation.Genre
			result.summary = pres.Presentation.Summary
		}
		result.attribution = pres.AttributionLabel()
		return result
	case workArtwork:
		data, _, err := a.client.Artwork(ctx, item.handle)
		if err != nil {
			return workResult{kind: workArtwork, gameID: item.gameID, gen: item.gen, err: err}
		}
		img, err := DecodeCover(data)
		if err != nil {
			return workResult{kind: workArtwork, gameID: item.gameID, gen: item.gen, err: err}
		}
		return workResult{kind: workArtwork, gameID: item.gameID, handle: item.handle, image: img, gen: item.gen}
	default:
		return workResult{kind: item.kind, gameID: item.gameID, gen: item.gen, err: fmt.Errorf("unknown work")}
	}
}
