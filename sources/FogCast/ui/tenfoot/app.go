package tenfoot

import (
	"context"
	"errors"
	"fmt"
	"github.com/DeanoC/FogCast/hostclient"
	"image"
	"strings"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/internal/playhid"
	"github.com/DeanoC/FogCast/kitlease"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/inputmap"
	"github.com/DeanoC/FogCast/ui/rooms"
	"github.com/DeanoC/FogCast/ui/shared"
	"github.com/DeanoC/FogCast/ui/theme"
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
	workScreenshot
)

type pendingWork struct {
	item workItem
	key  string
}

type workItem struct {
	kind    workKind
	gameID  string
	handle  string
	gen     int
	shotGen int
}

type workResult struct {
	kind          workKind
	gameID        string
	handle        string
	image         *image.RGBA
	err           error
	missing       bool
	state         string
	year          string
	genre         string
	summary       string
	studio        string
	players       string
	screenshotIDs []string
	attribution   string
	gen           int
	shotGen       int
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
	State             string
	Chrome            string
	GameID            string
	Title             string
	System            string
	Execution         string
	Media             string
	InputState        string
	InputHint         string
	InputBusy         bool
	Progress          string
	Stopping          bool
	RetryStop         bool
	RetryHint         string
	RetryCode         string
	LaunchLocked      bool
	Events            []string
	DevelopmentActive bool
	DevelopmentState  string
	Diagnostic        bool
}

// KitLeaseSnapshot is status-only kit ownership from GET /v1/kit/lease.
type KitLeaseSnapshot struct {
	Line        string
	State       string
	Owner       string
	Purpose     string
	Generation  string
	Expires     string
	Reason      string
	Unreachable bool
}

// HealthSnapshot is kit/host reachability from GET /api/v1/health.
type HealthSnapshot struct {
	HostUnreachable bool
	Ready           bool
	TargetReachable bool
	TargetReady     bool
	Line            string
	Connection      hostclient.TargetConnection
}

const detailMetaGap = 12

// layoutDetailMeta gives attribution its own reserved width so fitLabel cannot
// clip provenance off a shared facts line.
func layoutDetailMeta(d shared.FocusDetail, x, maxWidth, sizePx int) (facts string, factsX, factsW int, attr string, attrX, attrW int) {
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
	Games           []hostclient.Game
	Grid            Grid
	Status          string
	LoadErr         string
	Loading         bool
	Covers          map[string]*image.RGBA
	Launch          LaunchSnapshot
	Gamepads        int
	Keyboards       int
	Mice            int
	Affinity        InputKind
	AffinityID      int
	CoverHits       int
	Platforms       []hostclient.Platform
	PlatformID      string
	Sort            string
	Query           string
	SearchOpen      bool
	FocusDetail     shared.FocusDetail
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
	Filters         FilterSnapshot
	Genre           string
	Year            string
	Region          string
	HidePrerelease  bool
	HideHacks       bool
	OSK             shared.OSKSnapshot
	Health          HealthSnapshot
	KitLease        KitLeaseSnapshot
	Detail          DetailSnapshot
	Screenshots     map[string]*image.RGBA
	Preview         PreviewSnapshot
	Theme           theme.Theme
	DebugHUD        DebugHUDSnapshot
	Room            RoomSnapshot
	RoomPicker      RoomPickerSnapshot
	ReducedMotion   bool
}

// DebugHUDSnapshot is the optional corner overlay (off by default).
type DebugHUDSnapshot struct {
	Enabled bool
	Lines   []string
}

// App owns catalog, focus, async covers, and host launch. SDL stays out.
type App struct {
	client *Client
	ctx    context.Context
	cancel context.CancelFunc

	mu        sync.Mutex
	games     []hostclient.Game
	grid      Grid
	covers    map[string]*coverSlot
	inflight  map[string]workKind
	status    string
	loadErr   string
	loading   bool
	launch    LaunchSnapshot
	gamepads  int
	keyboards int
	mice      int
	affinity  affinityTracker
	repeat    Repeater
	remap     *inputmap.Remapper
	theme     theme.Theme
	jobs      chan workItem
	results   chan workResult
	maxGames  int
	pageLimit int

	platforms              []hostclient.Platform
	platformID             string
	sort                   string
	searchField            shared.TextField
	searchOpen             bool
	searchPending          bool
	searchDue              time.Time
	details                map[string]shared.FocusDetail
	shots                  map[string]*shotSlot
	shotIDs                map[string][]string
	detailOpen             bool
	carouselIndex          int
	shotGen                int
	shotCtx                context.Context
	shotCancel             context.CancelFunc
	loadGen                int
	keepFocusID            string
	keepFocusIndex         int
	navDirty               bool
	platformErr            string
	platformKick           chan struct{}
	loadCancel             context.CancelFunc
	jobCtx                 context.Context
	collectionID           string
	collections            []hostclient.Collection
	collectionsErr         string
	collectionsLoaded      bool
	collectionsKick        chan struct{}
	viewPickerOpen         bool
	viewPickerIndex        int
	nameEntry              nameEntryKind
	nameEntryID            string
	nameField              shared.TextField
	collectionBusy         bool
	membershipBusy         bool
	collectionManageOpen   bool
	collectionManageIndex  int
	collectionManageID     string
	collectionManageName   string
	collectionConfirmOpen  bool
	filtersOpen            bool
	filterPane             int
	filterIndex            int
	filterGen              int
	filtersLoading         bool
	filterStatus           string
	filterGenre            string
	filterYear             string
	filterRegion           string
	hidePrerelease         bool
	hideHacks              bool
	facets                 hostclient.FacetValues
	settingsOpen           bool
	settingsIndex          int
	settingsGen            int
	settingsWriteGen       int
	settingsPatchSeq       int
	settingsAppliedSeq     int
	settingsLoading        bool
	settingsBusy           bool
	settingsHydrated       bool
	settingsStatus         string
	settingsDraftIdle      int
	settingsDraftRegions   []string
	settingsDraftTarget    string
	settingsDraftTargets   []settingsTargetDraft
	settingsTargetsDirty   bool
	settingsDraftLibraries []hostclient.LibraryRoot
	settingsLibrariesDirty bool
	settingsRegionIndex    int
	settingsOSKKind        settingsOSKKind
	settingsOSKIndex       int
	settingsOSKIsAdd       bool
	settingsOSKField       shared.TextField
	hostSettings           hostclient.LibrarySettings
	attractPrefEnabled     bool
	attractForcedOff       bool
	favoriteBusy           bool
	hold                   HoldGate
	session                hostclient.SessionResult
	sessionTitle           string
	sessionGen             int
	stopPhase              string
	stopMessage            string
	gpuParked              bool
	sessionKick            chan struct{}
	health                 hostclient.HealthResult
	healthHave             bool
	hostUnreachable        bool
	inputBusy              bool
	inputAction            string
	inputMessage           string
	stopQueued             bool
	retryStopLock          bool
	retryStopHint          string
	retryStopCode          string
	sessionEvents          []hostclient.SessionEvent
	sessionEventAfter      uint64
	flightID               string
	lastFocusKey           string
	lastNavKey             string
	debugHUD               bool
	kitLease               KitLeaseStatus
	kitLeaseHave           bool
	developmentRBFPath     string
	devLoadPhase           string
	devLoadMessage         string

	roomsIndex        *rooms.Index
	roomsDir          string
	homeRooms         bool
	pinnedRooms       []string
	reducedMotion     bool
	homeRecents       []hostclient.Game
	homeRecentsErr    string
	homeRecentsLoaded bool
	homeRecentsGen    int
	room              *rooms.Instance
	roomStack         []*rooms.Instance
	roomFrame         rooms.Frame
	roomErr           string
	roomWasParked     bool
	roomPickerOpen    bool
	roomPickerIndex   int
	roomCoverSem      chan struct{}
	roomDetail        hostclient.Game
	roomChoiceOpen    bool
	roomChoiceIndex   int
	roomChoice        []hostclient.Game
	roomPicks         map[string]string

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
	attractItems       []hostclient.AttractItem
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

	previewGen         int
	previewCancel      context.CancelFunc
	previewLive        bool
	previewUnavailable bool
	previewImage       *image.RGBA
	previewSeq         int
	previewFails       int
	previewNext        time.Time
}

// SetRemapper installs a shared input profile. A nil remapper is identity.
func (a *App) SetRemapper(r *inputmap.Remapper) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.remap = r
}

// SetTheme installs the sofa look. Zero Theme is Default.
func (a *App) SetTheme(th theme.Theme) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.theme = th.Complete()
}

func (a *App) remapper() *inputmap.Remapper {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.remap
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
		games:              []hostclient.Game{},
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
		facets:             hostclient.FacetValues{Genres: []string{}, Years: []string{}},
		details:            map[string]shared.FocusDetail{},
		shots:              map[string]*shotSlot{},
		shotIDs:            map[string][]string{},
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
	a.loadPinnedRoomsLocked()
	a.showHomeLocked()
	a.mu.Unlock()
	for i := 0; i < coverWorkers; i++ {
		go a.worker(ctx)
	}
	go a.loadPlatforms(ctx)
	go a.loadCollections(ctx)
	go a.loadEditionPreferences(ctx)
	go a.pollSession(ctx)
	go a.loadLibrary(loadCtx, gen)
	go a.hydrateAttractIdle(ctx)
}

// Stop cancels background work.
func (a *App) Stop() {
	a.mu.Lock()
	a.hideAttractLocked()
	a.stopPreviewLocked()
	a.closeAllRoomsLocked()
	a.attractClosed = true
	cancel := a.cancel
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	a.drainAttractResults()
}

// HandleCommand applies a gamepad, USB-keyboard, or pointer-mapped command.
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
	if a.searchOpen || a.nameEntryOpenLocked() || a.settingsOSKOpenLocked() {
		switch cmd {
		case CmdSafeAreaIn, CmdSafeAreaOut, CmdLayoutCycle:
			return
		}
	}
	roomOwnsInput := a.room != nil && !a.roomPickerOpen && !a.settingsOpen && !a.settingsOSKOpenLocked()
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
		if roomOwnsInput {
			a.handleRoomLocked(cmd)
			return
		}
		a.cycleLayoutLocked()
		return
	case CmdHome:
		if a.settingsOSKOpenLocked() || a.settingsOpen || a.searchOpen || a.nameEntryOpenLocked() {
			return
		}
		if a.sessionStopOfferedLocked() {
			return
		}
		a.toggleRoomPickerLocked()
		return
	case CmdSettings:
		if a.settingsOpen {
			a.closeSettingsLocked()
		} else {
			a.openSettingsLocked()
		}
		return
	case CmdFilters:
		if roomOwnsInput {
			a.handleRoomLocked(cmd)
			return
		}
		if a.filtersOpen {
			a.closeFiltersLocked()
		} else {
			a.openFiltersLocked()
		}
		return
	}
	if a.settingsOSKOpenLocked() {
		a.handleSettingsOSKLocked(cmd)
		return
	}
	if a.settingsOpen {
		a.handleSettingsLocked(cmd)
		return
	}
	if a.roomPickerOpen && !a.sessionStopOfferedLocked() {
		a.handleRoomPickerLocked(cmd)
		return
	}
	if a.room != nil && !a.sessionStopOfferedLocked() {
		a.handleRoomLocked(cmd)
		return
	}
	if a.filtersOpen {
		a.handleFiltersLocked(cmd)
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
		case CmdSortCycle:
			a.startInputToggleLocked()
			return
		case CmdUp, CmdDown, CmdLeft, CmdRight, CmdFilterPrev, CmdFilterNext, CmdSearch, CmdDetails, CmdViewPrev, CmdViewNext, CmdViewPicker, CmdFavorite, CmdFilters, CmdSafeAreaIn, CmdSafeAreaOut, CmdTab, CmdTabPrev:
			return
		}
	}
	if a.handleDetailLocked(cmd) {
		return
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
	case CmdSearch, CmdTab:
		a.openSearchLocked()
	case CmdDetails:
		a.openDetailLocked()
	case CmdViewPrev:
		a.cycleViewLocked(-1)
	case CmdViewNext:
		a.cycleViewLocked(1)
	case CmdViewPicker:
		a.openViewPickerLocked()
	case CmdFavorite:
		a.toggleFavoriteLocked()
	case CmdTabPrev:
		a.openFiltersLocked()
	}
}

func (a *App) openSearchLocked() {
	a.closeDetailLocked()
	a.closeFiltersLocked()
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
	case CmdTab:
		a.closeSearchApplyLocked()
	case CmdTabPrev, CmdFilterPrev:
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
	text = shared.SanitizeFieldText(text)
	if text == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.noteActivityLocked(now)
	if a.consumeAttractLocked(CmdNone, now) {
		return
	}
	if a.settingsOSKOpenLocked() {
		a.settingsOSKField.Insert(text)
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
	if a.settingsOSKOpenLocked() {
		if a.settingsOSKField.Buffer == "" {
			return
		}
		a.settingsOSKField.Backspace()
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
	if a.settingsOSKOpenLocked() {
		a.submitSettingsOSKLocked()
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
	a.stampNavLocked("platform")
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
	a.stampNavLocked("collection")
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
	a.closeDetailLocked()
	a.closeFiltersLocked()
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
	games := append([]hostclient.Game{}, a.games...)
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
	if a.collectionID == "favorites" {
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
	a.closeDetailLocked()
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
	a.cancelScreenshotContextLocked()
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
	a.syncPreviewLocked()
	if !a.attractActive {
		a.tickRoomLocked(now)
	}
	a.mu.Unlock()
	a.queueVisibleWork(now)
	if a.ForwardsPlayHID() {
		a.repeat.Clear()
	}
	if a.browseHoldEnabled() {
		if cmd := a.hold.Tick(now); cmd != CmdNone {
			a.HandleCommand(cmd, now)
			return cmd
		}
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
	} else if a.retryStopLock && strings.TrimSpace(a.retryStopHint) != "" {
		status = a.retryStopHint
	} else if a.inputBusy && strings.TrimSpace(a.inputMessage) != "" {
		status = a.inputMessage
	} else if a.developmentLoadingLocked() && strings.TrimSpace(a.devLoadMessage) != "" {
		status = a.devLoadMessage
	} else if (a.devLoadPhase == "error" || a.devLoadPhase == "host") && a.session.State != "active" && strings.TrimSpace(a.devLoadMessage) != "" {
		status = a.devLoadMessage
	} else if a.launch.Phase != "idle" && a.launch.Phase != "ok" && a.launch.Message != "" {
		status = a.launch.Message
	} else if line := a.nowPlayingStatusLocked(); line != "" {
		status = line
	} else if a.launch.Phase == "ok" && a.launch.Message != "" {
		status = a.launch.Message
	} else if line := strings.TrimSpace(a.inputMessage); line != "" {
		status = line
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
	health := a.healthSnapshotLocked()
	if health.Line != "" && (status == "" || status == "connecting to host API") {
		status = health.Line
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
		Keyboards:       a.keyboards,
		Mice:            a.mice,
		Affinity:        a.affinity.current.Kind,
		AffinityID:      a.affinity.current.ID,
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
		Filters:         a.filtersSnapshotLocked(),
		Genre:           a.filterGenre,
		Year:            a.filterYear,
		Region:          a.filterRegion,
		HidePrerelease:  a.hidePrerelease,
		HideHacks:       a.hideHacks,
		OSK:             a.oskSnapshotLocked(),
		Health:          health,
		KitLease:        a.kitLeaseSnapshotLocked(),
		Detail:          a.detailSnapshotLocked(),
		Screenshots:     a.screenshotImagesLocked(),
		Preview:         a.previewSnapshotLocked(),
		Theme:           a.theme.Complete(),
		DebugHUD:        a.debugHUDSnapshotLocked(),
		Room:            a.roomSnapshotLocked(true),
		RoomPicker:      a.roomPickerSnapshotLocked(),
		ReducedMotion:   a.reducedMotion,
	}
}

// SetDebugHUD enables the optional corner overlay (gen, lease ttl, last error).
func (a *App) SetDebugHUD(on bool) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.debugHUD = on
}

// SetGamepads records how many gamepads SDL currently has open.
func (a *App) SetGamepads(n int) {
	a.mu.Lock()
	a.gamepads = n
	a.mu.Unlock()
}

func (a *App) syncInputCountsLocked() {
	a.keyboards = a.affinity.count(InputKeyboard)
	a.mice = a.affinity.count(InputMouse)
	a.gamepads = a.affinity.count(InputGamepad)
}

func (a *App) noteInputLocked(kind InputKind, id int) {
	a.affinity.Note(kind, id)
	a.syncInputCountsLocked()
}

// SeedInput records a device present at start without claiming affinity.
func (a *App) SeedInput(kind InputKind, id int) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.affinity.Seed(kind, id)
	a.syncInputCountsLocked()
}

// AttachInput records a hotplug. A newly seen keyboard, mouse, or gamepad
// claims hint and focus ownership without moving catalog focus.
func (a *App) AttachInput(kind InputKind, id int) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.affinity.Attach(kind, id)
	a.syncInputCountsLocked()
}

// DetachInput drops a device. Unplugging the owner restores a remaining one.
func (a *App) DetachInput(kind InputKind, id int) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.affinity.Detach(kind, id)
	a.syncInputCountsLocked()
}

// NoteInput marks last-used input so hints follow that device.
func (a *App) NoteInput(kind InputKind, id int) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.noteInputLocked(kind, id)
}

// FinishInputSeed picks a startup owner: gamepad if any, else keyboard, else mouse.
func (a *App) FinishInputSeed() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.affinity.preferStartup()
	a.syncInputCountsLocked()
}

// Affinity reports the device that currently owns sofa hints and focus.
func (a *App) Affinity() Affinity {
	if a == nil {
		return Affinity{}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.affinity.current
}

func (a *App) oskSnapshotLocked() shared.OSKSnapshot {
	if a.settingsOSKOpenLocked() {
		snap := a.settingsOSKField.Snapshot()
		snap.Open = true
		switch a.settingsOSKKind {
		case settingsOSKLibraryPath:
			snap.Prompt = "Library path"
		case settingsOSKTargetName:
			snap.Prompt = "Target name"
		case settingsOSKTargetAddress:
			snap.Prompt = "Target address"
		case settingsOSKTargetAgent:
			snap.Prompt = "Agent password"
			snap.Masked = true
			snap.Buffer = shared.MaskSecret(a.settingsOSKField.Buffer)
		case settingsOSKDevelopmentPath:
			snap.Prompt = "DIAGNOSTIC RBF path"
		}
		return a.withOSKHintLocked(snap)
	}
	if a.nameEntryOpenLocked() {
		snap := a.nameField.Snapshot()
		snap.Open = true
		snap.Prompt = "Collection name"
		return a.withOSKHintLocked(snap)
	}
	if !a.searchOpen {
		return shared.OSKSnapshot{}
	}
	snap := a.searchField.Snapshot()
	snap.Open = true
	snap.Prompt = "Search"
	return a.withOSKHintLocked(snap)
}

func (a *App) withOSKHintLocked(snap shared.OSKSnapshot) shared.OSKSnapshot {
	snap.Hint = oskHintFor(a.affinity.current.Kind, snap.Page)
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

func (a *App) browseHoldEnabled() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.browseHoldEnabledLocked()
}

func (a *App) browseHoldEnabledLocked() bool {
	if a.gpuParked || a.stopPhase == "stopping" || a.launch.Phase == "launching" || a.retryStopLock || a.developmentLoadingLocked() {
		return false
	}
	switch a.session.State {
	case "active", "launching":
		return false
	default:
		return true
	}
}

// ChromeLine is the header label: now-playing while a session is active, otherwise browse chrome.
func (s Snapshot) ChromeLine() string {
	line := s.chromeBody()
	if health := strings.TrimSpace(s.Health.Line); health != "" && !strings.Contains(line, health) {
		line = health + "  ·  " + line
	}
	if lease := strings.TrimSpace(s.KitLease.Line); lease != "" && !strings.Contains(line, lease) {
		line = line + "  ·  " + lease
	}
	return line
}

func (s Snapshot) chromeBody() string {
	if s.GPUParked || s.Session.State == "active" {
		line := s.NowPlayingLine()
		if strings.TrimSpace(s.Status) != "" && s.Status != line && s.Status != s.Health.Line {
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
	parts := []string{view, platform, sortLabel(s.Collection, s.Sort)}
	parts = append(parts, filterSummaryParts(s.Genre, s.Year, s.Region, s.HidePrerelease, s.HideHacks)...)
	parts = append(parts, search)
	status := s.Status
	if health := strings.TrimSpace(s.Health.Line); health != "" && status == health {
		return strings.Join(parts, "  ·  ")
	}
	return strings.Join(append(parts, status), "  ·  ")
}

// NowPlayingLine is the compact active-session chrome.
func (s Snapshot) NowPlayingLine() string {
	if s.Session.Diagnostic {
		return diagnosticNowPlayingLine(s.Session)
	}
	heading := nowPlayingHeading(s)
	parts := make([]string, 0, 6)
	parts = append(parts, heading)
	title := strings.TrimSpace(s.Session.Title)
	if title == "" {
		title = strings.TrimSpace(s.Session.GameID)
	}
	if title != "" {
		parts = append(parts, title)
	}
	if chrome := strings.TrimSpace(s.Session.Chrome); chrome != "" {
		parts = append(parts, chrome)
	} else if state := strings.TrimSpace(s.Session.State); state != "" {
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
	if hint := strings.TrimSpace(s.Session.InputHint); hint != "" {
		parts = append(parts, hint)
	}
	if s.Session.Stopping && strings.TrimSpace(s.Session.Chrome) != sessionChromeStopping {
		parts = append(parts, "stopping")
	}
	if s.Session.RetryStop {
		hint := strings.TrimSpace(s.Session.RetryHint)
		if hint == "" {
			hint = "retry Stop"
		}
		parts = append(parts, hint)
	}
	return strings.Join(parts, "  ·  ")
}

const (
	nowPlayingHeadingPlay = "Now playing"
	nowPlayingHeadingSave = "Save failed"
	nowPlayingHeadingStop = "Stop failed"
)

// nowPlayingHeading is the living-room label for the parked session surface.
// A failed save must not read as Now playing or Completed.
func nowPlayingHeading(s Snapshot) string {
	if s.Session.Diagnostic {
		return diagnosticLabel
	}
	if s.Session.RetryStop || strings.TrimSpace(s.Session.Chrome) == sessionChromeFailed {
		if sessionSaveFailedVisible(s) {
			return nowPlayingHeadingSave
		}
		return nowPlayingHeadingStop
	}
	return nowPlayingHeadingPlay
}

func sessionSaveFailedVisible(s Snapshot) bool {
	blob := strings.ToLower(strings.Join([]string{
		s.Session.RetryCode,
		s.Session.RetryHint,
		s.Status,
		s.Session.Progress,
	}, " "))
	return strings.Contains(blob, "save_failed") ||
		strings.Contains(blob, "save could not") ||
		strings.Contains(blob, "save fail")
}

// Selected returns the focused game, if any.
func (a *App) Selected() (hostclient.Game, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.grid.Focus < 0 || a.grid.Focus >= len(a.games) {
		return hostclient.Game{}, false
	}
	return a.games[a.grid.Focus], true
}

func (a *App) focusDetailLocked() shared.FocusDetail {
	if a.room != nil && strings.TrimSpace(a.roomDetail.ID) != "" {
		return a.detailForGameLocked(a.roomDetail)
	}
	if a.grid.Focus < 0 || a.grid.Focus >= len(a.games) {
		return shared.FocusDetail{}
	}
	return a.detailForGameLocked(a.games[a.grid.Focus])
}

func (a *App) detailForGameLocked(game hostclient.Game) shared.FocusDetail {
	detail := shared.FocusDetail{
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
		detail.Studio = strings.TrimSpace(cached.Studio)
		detail.Players = strings.TrimSpace(cached.Players)
		detail.Attribution = strings.TrimSpace(cached.Attribution)
		if len(cached.ScreenshotIDs) > 0 {
			detail.ScreenshotIDs = append([]string(nil), cached.ScreenshotIDs...)
		}
	}
	if ids := a.shotIDs[game.ID]; len(ids) > 0 {
		detail.ScreenshotIDs = append([]string(nil), ids...)
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
	if dy > 0 && a.detailEnterFromBrowseLocked() {
		a.openDetailLocked()
		return
	}
	before := a.grid.Focus
	a.grid.Move(dx, dy)
	if a.grid.Focus != before {
		a.navDirty = true
		a.carouselIndex = 0
		a.stampFocusLocked()
		return
	}
	if dy > 0 && len(a.games) > 0 {
		a.openDetailLocked()
	}
}

func (a *App) focusIndex(i int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.focusIndexLocked(i)
}

func (a *App) focusIndexLocked(i int) bool {
	if i < 0 || i >= len(a.games) {
		return false
	}
	if a.grid.Focus != i {
		a.navDirty = true
		a.carouselIndex = 0
		a.grid.Focus = i
		a.grid.ensureVisible()
		a.stampFocusLocked()
		return true
	}
	a.grid.Focus = i
	a.grid.ensureVisible()
	return true
}

func (a *App) startLaunchLocked() {
	if a.launch.Phase == "launching" || a.sessionStopOfferedLocked() || a.developmentLoadingLocked() {
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

func (a *App) startLaunchGameLocked(game hostclient.Game) {
	if a.launch.Phase == "launching" || a.sessionStopOfferedLocked() || a.developmentLoadingLocked() {
		return
	}
	a.clearStaleDevelopmentLoadLocked()
	if reason := launchBlockReason(game); reason != "" {
		a.launch = LaunchSnapshot{GameID: game.ID, Phase: "error", Message: reason}
		if a.room != nil {
			a.closeRoomOverlaysLocked()
		}
		return
	}
	a.sessionTitle = game.Title
	a.launch = LaunchSnapshot{
		GameID:  game.ID,
		Phase:   "launching",
		Message: "launching " + game.Title,
	}
	if a.room != nil {
		a.closeRoomOverlaysLocked()
	}
	a.hold.Clear()
	a.closeFiltersLocked()
	a.bumpSessionGenLocked()
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	stamp := a.clientStampLocked()
	a.postUIEventLocked("ui.launch", map[string]string{
		"game_id": game.ID,
		"title":   game.Title,
	})
	go a.doLaunch(ctx, game, stamp)
}

// launchBlockReason maps hostclient catalog ineligibility to sofa copy.
// Admission rules live on hostclient.Game; unavailable titles must not
// POST /api/v1/session/launch.
func launchBlockReason(game hostclient.Game) string {
	switch game.LaunchBlock() {
	case hostclient.LaunchBrowseOnly:
		return "This platform is browse-only on this host."
	case hostclient.LaunchSourceOffline:
		return "This game's source is offline."
	case hostclient.LaunchUnreadable:
		return "This ROM can't be read."
	case hostclient.LaunchNotReady:
		return "This game isn't ready to launch."
	case hostclient.LaunchMissingFirmware:
		return "Coleco BIOS required. Import household firmware before Play."
	case "":
		return ""
	default:
		return "This game isn't ready to launch."
	}
}

func (a *App) doLaunch(ctx context.Context, game hostclient.Game, stamp ClientStamp) {
	result, err := a.client.LaunchStamped(ctx, game.ID, stamp)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.launch.GameID != game.ID {
		return
	}
	a.bumpSessionGenLocked()
	if err != nil {
		a.launch.Phase = "error"
		a.launch.Message = "launch failed: " + err.Error()
		a.launch.ErrorMessage = err.Error()
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
	a.applySessionLocked(result)
	if a.session.State == "active" && a.session.GameID == "" {
		a.session.GameID = game.ID
	}
	a.kickSessionPollLocked()
}

func (a *App) sessionStopOfferedLocked() bool {
	return a.retryStopLock || a.session.State == "active" || a.stopPhase == "stopping"
}

func (a *App) startStopLocked() {
	if a.stopPhase == "stopping" {
		return
	}
	if !a.retryStopLock && a.session.State != "active" {
		return
	}
	if a.inputBusy {
		a.stopQueued = true
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
	stamp := a.clientStampLocked()
	a.postUIEventLocked("ui.stop", map[string]string{
		"game_id": a.session.GameID,
		"state":   a.session.State,
	})
	go a.doStop(ctx, stamp)
}

func (a *App) doStop(ctx context.Context, stamp ClientStamp) {
	result, err := a.client.StopStamped(ctx, stamp)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopPhase != "stopping" {
		return
	}
	a.bumpSessionGenLocked()
	if err != nil {
		a.stopPhase = "error"
		a.stopMessage = "stop failed: " + err.Error()
		a.lockRetryStopLocked("", err.Error())
		a.syncGPUParkLocked()
		return
	}
	if result.ErrorCode != "" {
		a.stopPhase = "host"
		a.stopMessage = fmt.Sprintf("host stop %d %s: %s", result.HTTPStatus, result.ErrorCode, result.ErrorMessage)
		a.lockRetryStopLocked(result.ErrorCode, result.ErrorMessage)
		a.syncGPUParkLocked()
		return
	}
	a.retryStopLock = false
	a.retryStopHint = ""
	a.retryStopCode = ""
	a.stopPhase = "ok"
	a.stopMessage = ""
	a.applySessionLocked(result)
	a.kickSessionPollLocked()
}

func (a *App) lockRetryStopLocked(code, message string) {
	a.retryStopLock = true
	a.retryStopHint = retryStopStatus(code, message)
	a.retryStopCode = strings.TrimSpace(code)
}

func (a *App) ForwardsCoreKeyboard() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.forwardsCoreKeyboardLocked()
}

func (a *App) forwardsCoreKeyboardLocked() bool {
	if !a.session.CoreKeyboard {
		return false
	}
	return a.forwardsPlayHIDLocked()
}

func (a *App) ForwardsPlayHID() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.forwardsPlayHIDLocked()
}

func (a *App) forwardsPlayHIDLocked() bool {
	if a.session.State != "active" {
		return false
	}
	if a.session.Input == nil {
		return false
	}
	return a.session.Input.Ready || a.session.Input.State == "attached" ||
		a.session.Input.State == "reconnecting"
}

// ConsumePlayHID keeps USB keys on the play-session path: no sofa browse and
// no affinity steal. False means browse/nav may handle the key.
func (a *App) ConsumePlayHID() bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.forwardsPlayHIDLocked() {
		return false
	}
	a.repeat.Clear()
	return true
}

// HandlePlayHIDKey consumes one USB key while play HID is attached. Esc and
// Backspace stop the session without NoteInput; other keys encode for the
// attached core. Letter s is a ZX81/core key, not chrome stop.
func (a *App) HandlePlayHIDKey(name string, down bool, now time.Time) bool {
	if !a.ConsumePlayHID() {
		return false
	}
	if playhid.ChromeStop(name) {
		cmd := CommandFromKey(strings.ToLower(strings.TrimSpace(name)))
		if down {
			a.Press(cmd, now)
		} else {
			a.Release(cmd)
		}
		return true
	}
	if event, ok := playhid.Event(name, down, a.ForwardsCoreKeyboard()); ok {
		a.SendPlayHID(event)
	}
	return true
}

func (a *App) playHIDFailClosedLocked() bool {
	if a.healthHave && kitlease.ForeignHID(kitlease.Status{
		State: a.health.Connection.State,
		Owner: a.health.Connection.Owner,
	}) {
		return true
	}
	if !a.kitLeaseHave || a.kitLease.Unavailable {
		return false
	}
	switch strings.TrimSpace(a.kitLease.ErrorCode) {
	case "KIT_LEASE_DENIED", "KIT_LEASE_BUSY":
		return true
	}
	return kitlease.ForeignHID(kitlease.Status{
		State:   a.kitLease.State,
		Owner:   a.kitLease.Owner,
		Purpose: a.kitLease.Purpose,
	})
}

func (a *App) SendCoreKey(event remoteinput.Event) {
	a.SendPlayHID(event)
}

// SendPlayHID posts one play-session HID event. Foreign kit leases fail closed.
func (a *App) SendPlayHID(event remoteinput.Event) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	ready := a.forwardsPlayHIDLocked()
	foreign := a.playHIDFailClosedLocked()
	client := a.client
	a.mu.Unlock()
	if !ready || foreign || client == nil {
		return false
	}
	go func() { _ = client.SendCoreKey(context.Background(), event) }()
	return true
}

func (a *App) pollSession(ctx context.Context) {
	a.fetchSession(ctx)
	a.fetchSessionEvents(ctx)
	a.fetchHealth(ctx)
	a.fetchKitLease(ctx)
	ticker := time.NewTicker(sessionPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.fetchSession(ctx)
			a.fetchSessionEvents(ctx)
			a.fetchHealth(ctx)
			a.fetchKitLease(ctx)
		case <-a.sessionKick:
			a.fetchSession(ctx)
			a.fetchSessionEvents(ctx)
			a.fetchHealth(ctx)
			a.fetchKitLease(ctx)
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
	if a.launch.Phase == "launching" || a.stopPhase == "stopping" || a.inputBusy || a.developmentLoadingLocked() {
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

func (a *App) fetchHealth(ctx context.Context) {
	result, err := a.client.Health(ctx)
	if ctx.Err() != nil {
		return
	}
	if err == nil {
		a.mu.Lock()
		a.hostUnreachable = false
		a.healthHave = true
		a.health = result
		a.mu.Unlock()
		return
	}
	if isHostTransportError(err) {
		a.mu.Lock()
		a.hostUnreachable = true
		a.mu.Unlock()
		return
	}
	status, statusErr := a.client.Status(ctx)
	if ctx.Err() != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if statusErr == nil && status.Unavailable {
		a.hostUnreachable = false
		a.healthHave = true
		a.health = hostclient.HealthResult{Ready: true, TargetReachable: false, TargetReady: false}
	}
}

func (a *App) healthSnapshotLocked() HealthSnapshot {
	line := kitHealthLine(a.hostUnreachable, a.healthHave, a.health)
	return HealthSnapshot{
		HostUnreachable: a.hostUnreachable,
		Ready:           a.health.Ready,
		TargetReachable: a.health.TargetReachable,
		TargetReady:     a.health.TargetReady,
		Line:            line,
		Connection:      a.health.Connection,
	}
}

func kitHealthLine(hostUnreachable, have bool, health hostclient.HealthResult) string {
	if hostUnreachable {
		return "host unreachable"
	}
	if !have {
		return ""
	}
	if !health.Ready {
		return "host not ready"
	}
	if health.Connection.State != "" {
		return connectionLine(health.Connection)
	}
	if !health.TargetReachable {
		return "kit unreachable"
	}
	if !health.TargetReady {
		return "kit not ready"
	}
	return ""
}

func connectionLine(connection hostclient.TargetConnection) string {
	state := strings.TrimSpace(connection.State)
	label := strings.ReplaceAll(state, "-", " ")
	parts := []string{label}
	if state == "busy" && strings.TrimSpace(connection.Owner) != "" {
		parts = append(parts, "owned by "+strings.TrimSpace(connection.Owner))
	} else if strings.TrimSpace(connection.Message) != "" {
		parts = append(parts, strings.TrimSpace(connection.Message))
	}
	if (state == "ready" || state == "active") && strings.TrimSpace(connection.Address) != "" {
		parts = append(parts, strings.TrimSpace(connection.Address))
	}
	return strings.Join(parts, " · ")
}

func remoteInputCanAttach(session hostclient.SessionResult) bool {
	state := remoteInputState(session)
	if !remoteInputOffered(session) || state == "" {
		return false
	}
	if remoteInputTransitioning(state) {
		return false
	}
	return state != "attached"
}

func remoteInputCanDetach(session hostclient.SessionResult) bool {
	return remoteInputOffered(session) && remoteInputState(session) == "attached"
}

func remoteInputOffered(session hostclient.SessionResult) bool {
	return session.State == "active" && session.Execution == "fpga_native" && session.Input != nil
}

func remoteInputState(session hostclient.SessionResult) string {
	if session.Input == nil {
		return ""
	}
	return strings.TrimSpace(session.Input.State)
}

func remoteInputTransitioning(state string) bool {
	return state == "starting" || state == "reconnecting"
}

func remoteInputHint(session hostclient.SessionResult, busy bool, action string, kind InputKind) string {
	west := westWord(kind)
	if busy {
		switch action {
		case "detach":
			return west + " detaching"
		default:
			return west + " attaching"
		}
	}
	if remoteInputCanDetach(session) {
		return west + " detach"
	}
	if remoteInputCanAttach(session) {
		return west + " attach"
	}
	return ""
}

func (a *App) startInputToggleLocked() {
	if a.inputBusy || a.stopPhase == "stopping" {
		return
	}
	if !remoteInputOffered(a.session) {
		return
	}
	state := remoteInputState(a.session)
	if remoteInputTransitioning(state) {
		a.inputMessage = "input busy"
		return
	}
	action := "attach"
	if remoteInputCanDetach(a.session) {
		action = "detach"
	} else if !remoteInputCanAttach(a.session) {
		a.inputMessage = "input busy"
		return
	}
	a.inputBusy = true
	a.inputAction = action
	if action == "detach" {
		a.inputMessage = "detaching remote input"
	} else {
		a.inputMessage = "attaching remote input"
	}
	a.bumpSessionGenLocked()
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go a.doInputMutation(ctx, action)
}

func (a *App) doInputMutation(ctx context.Context, action string) {
	var result hostclient.SessionResult
	var err error
	if action == "detach" {
		result, err = a.client.DetachInput(ctx)
	} else {
		result, err = a.client.AttachInput(ctx)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.inputAction != action || !a.inputBusy {
		return
	}
	a.bumpSessionGenLocked()
	a.inputBusy = false
	a.inputAction = ""
	if err != nil || result.ErrorCode != "" {
		a.inputMessage = inputFailureStatus(action)
		a.kickSessionPollLocked()
		a.flushQueuedStopLocked()
		return
	}
	a.applySessionLocked(result)
	if action == "detach" {
		a.inputMessage = "input detached"
	} else {
		a.inputMessage = "input attached"
	}
	a.kickSessionPollLocked()
	a.flushQueuedStopLocked()
}

func (a *App) flushQueuedStopLocked() {
	if !a.stopQueued {
		return
	}
	a.stopQueued = false
	a.startStopLocked()
}

func inputFailureStatus(action string) string {
	if action == "detach" {
		return "input detach failed"
	}
	return "input attach failed"
}

func (a *App) applySessionLocked(result hostclient.SessionResult) {
	if result.ErrorCode != "" {
		return
	}
	a.session = result
	a.rememberFlightLocked(result.FlightID)
	if result.State != "active" {
		a.session.GameID = ""
		a.session.System = ""
	}
	if a.session.State == "active" && !a.developmentLoadingLocked() && !sessionDevelopmentActive(a.session) {
		a.clearStaleDevelopmentLoadLocked()
	}
	if a.session.State != "active" && a.stopPhase != "stopping" && !a.retryStopLock {
		a.stopPhase = "idle"
		a.stopMessage = ""
		if a.launch.Phase == "ok" {
			a.launch.Phase = "idle"
			a.launch.Message = ""
			a.launch.GameID = ""
		}
		a.clearCompletedDevelopmentLoadLocked()
	}
	a.syncGPUParkLocked()
}

func (a *App) syncGPUParkLocked() {
	if a.session.State == "active" || a.session.State == "launching" || a.stopPhase == "stopping" || a.retryStopLock || a.developmentLoadingLocked() {
		a.hold.Clear()
	}
	want := a.session.State == "active" || a.stopPhase == "stopping" || a.retryStopLock
	if want == a.gpuParked {
		if want {
			a.closeCollectionOverlaysLocked()
			a.searchOpen = false
			a.closeSettingsLocked()
			a.closeFiltersLocked()
			a.closeDetailLocked()
		}
		a.syncPreviewLocked()
		return
	}
	a.gpuParked = want
	a.stopPreviewLocked()
	if !want {
		a.syncPreviewLocked()
		return
	}
	a.hideAttractLocked()
	a.closeCollectionOverlaysLocked()
	a.searchOpen = false
	a.closeSettingsLocked()
	a.closeFiltersLocked()
	a.closeDetailLocked()
	a.loadGen++
	wasLoading := a.loading
	ctx := a.replaceLoadContextLocked()
	if wasLoading {
		go a.loadLibrary(ctx, a.loadGen)
	}
	a.covers = map[string]*coverSlot{}
	a.shots = map[string]*shotSlot{}
	a.shotIDs = map[string][]string{}
	a.syncPreviewLocked()
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
	inputState := remoteInputState(a.session)
	events := make([]string, 0, len(a.sessionEvents))
	for _, ev := range a.sessionEvents {
		if line := formatSessionEvent(ev); line != "" {
			events = append(events, line)
		}
	}
	retry := a.retryStopLock
	devActive := sessionDevelopmentActive(a.session)
	devState := sessionDevelopmentState(a.session)
	chrome := sessionChromeState(a.session.State, a.session.Execution, a.stopPhase == "stopping", retry)
	diagnostic := chrome == sessionChromeDevelopment || (devActive && (a.session.State == "active" || a.session.State == "launching"))
	return SessionSnapshot{
		State:             a.session.State,
		Chrome:            chrome,
		GameID:            a.session.GameID,
		Title:             title,
		System:            a.session.System,
		Execution:         a.session.Execution,
		Media:             a.session.Media,
		InputState:        inputState,
		InputHint:         remoteInputHint(a.session, a.inputBusy, a.inputAction, a.affinity.current.Kind),
		InputBusy:         a.inputBusy || remoteInputTransitioning(inputState),
		Progress:          progress,
		Stopping:          a.stopPhase == "stopping",
		RetryStop:         retry,
		RetryHint:         strings.TrimSpace(a.retryStopHint),
		RetryCode:         strings.TrimSpace(a.retryStopCode),
		LaunchLocked:      a.sessionStopOfferedLocked() || a.launch.Phase == "launching" || a.developmentLoadingLocked(),
		Events:            events,
		DevelopmentActive: devActive,
		DevelopmentState:  devState,
		Diagnostic:        diagnostic,
	}
}

func (a *App) kitLeaseSnapshotLocked() KitLeaseSnapshot {
	if !a.kitLeaseHave {
		return KitLeaseSnapshot{}
	}
	status := a.kitLease
	return KitLeaseSnapshot{
		Line:        formatKitLeaseLine(status),
		State:       status.State,
		Owner:       status.Owner,
		Purpose:     status.Purpose,
		Generation:  status.Generation,
		Expires:     formatLeaseExpiry(status),
		Reason:      status.Reason,
		Unreachable: status.Unavailable,
	}
}

func (a *App) selectedTargetAddressLocked() string {
	selected := strings.TrimSpace(a.hostSettings.SelectedTarget)
	for _, target := range a.hostSettings.Targets {
		if strings.TrimSpace(target.Name) != selected {
			continue
		}
		return strings.TrimSpace(target.Address)
	}
	return ""
}

func (a *App) sessionLiveLocked() bool {
	switch a.session.State {
	case "active", "failed", "stopping", "launching":
		return true
	}
	switch a.stopPhase {
	case "stopping", "error", "host":
		return true
	}
	return a.retryStopLock
}

func (a *App) fetchSessionEvents(ctx context.Context) {
	a.mu.Lock()
	after := a.sessionEventAfter
	a.mu.Unlock()
	events, err := a.client.SessionEvents(ctx, after)
	if err != nil || ctx.Err() != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sessionEvents, a.sessionEventAfter = mergeSessionEvents(a.sessionEvents, events, after)
	a.rememberFlightsFromEventsLocked(events)
	retain, ev := eventsRetainRetryStop(a.sessionEvents)
	if !retain || !a.sessionLiveLocked() {
		return
	}
	msg := ""
	if ev.Progress != nil {
		msg = ev.Progress.Message
	}
	a.lockRetryStopLocked(ev.Event, msg)
	a.syncGPUParkLocked()
}

func (a *App) refreshTargetAddress(ctx context.Context) string {
	a.mu.Lock()
	target := a.selectedTargetAddressLocked()
	open := a.settingsOpen
	writeGen := a.settingsWriteGen
	a.mu.Unlock()
	if target != "" || open {
		return target
	}
	settings, err := a.client.LibrarySettings(ctx)
	if err != nil || ctx.Err() != nil {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.settingsOpen || a.settingsWriteGen != writeGen {
		return a.selectedTargetAddressLocked()
	}
	a.hostSettings = settings
	return a.selectedTargetAddressLocked()
}

func (a *App) fetchKitLease(ctx context.Context) {
	target := a.refreshTargetAddress(ctx)
	if target == "" || ctx.Err() != nil {
		return
	}
	status, err := a.client.KitLease(ctx, target)
	if ctx.Err() != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.kitLeaseHave = true
	if err != nil {
		if isHostTransportError(err) {
			a.kitLease = KitLeaseStatus{Unavailable: true, ErrorMessage: "kit unreachable"}
			return
		}
		a.kitLease = KitLeaseStatus{ErrorMessage: "kit lease unavailable"}
		return
	}
	a.kitLease = status
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
			a.collectionsLoaded = true
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

func (a *App) currentQueryLocked() hostclient.GameListQuery {
	return hostclient.GameListQuery{
		Limit:          a.pageLimit,
		Platform:       a.platformID,
		Sort:           catalogQuerySort(a.collectionID, a.sort),
		Q:              strings.TrimSpace(a.searchField.Buffer),
		Collection:     a.collectionID,
		Genre:          a.filterGenre,
		Year:           a.filterYear,
		Region:         a.filterRegion,
		HidePrerelease: a.hidePrerelease,
		HideHacks:      a.hideHacks,
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
	filters := a.filterSummaryLocked()
	if q := strings.TrimSpace(a.searchField.Buffer); q != "" {
		if filters != "" {
			return fmt.Sprintf("%d titles · %s · %s · %s · %s · %q", n, view, platform, sort, filters, q)
		}
		return fmt.Sprintf("%d titles · %s · %s · %s · %q", n, view, platform, sort, q)
	}
	if filters != "" {
		return fmt.Sprintf("%d titles · %s · %s · %s · %s", n, view, platform, sort, filters)
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
func restoreCatalogFocus(games []hostclient.Game, keepID string, keepIndex int) int {
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

func (a *App) applyCatalogPageLocked(games []hostclient.Game, keepID string, keepIndex int, pinned *bool, lastFocus *int) {
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
		all    []hostclient.Game
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
		a.applyCatalogPageLocked(append([]hostclient.Game(nil), all...), keepID, keepIndex, &pinned, &lastFocus)
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
		if a.platformID != "" || strings.TrimSpace(a.searchField.Buffer) != "" || a.collectionID != "" || a.filtersActiveLocked() {
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
		delete(a.inflight, result.key())
		return
	}
	delete(a.inflight, result.key())
	if result.kind == workScreenshot {
		if result.shotGen != a.shotGen {
			return
		}
		if result.err != nil && errors.Is(result.err, context.Canceled) {
			return
		}
		a.applyScreenshotResultLocked(result)
		return
	}
	if result.err != nil && (errors.Is(result.err, context.Canceled) || errors.Is(result.err, context.DeadlineExceeded)) {
		return
	}
	if result.kind == workPresentation && result.err == nil {
		if shots := shared.ScreenshotHandles(result.screenshotIDs); len(shots) > 0 {
			a.shotIDs[result.gameID] = shots
		} else {
			delete(a.shotIDs, result.gameID)
		}
		if presentationComplete(result.state, result.attribution) {
			a.details[result.gameID] = shared.FocusDetail{
				Year:          strings.TrimSpace(result.year),
				Genre:         strings.TrimSpace(result.genre),
				Studio:        strings.TrimSpace(result.studio),
				Players:       strings.TrimSpace(result.players),
				Summary:       strings.TrimSpace(result.summary),
				Attribution:   strings.TrimSpace(result.attribution),
				ScreenshotIDs: shared.ScreenshotHandles(result.screenshotIDs),
			}
		}
		a.clampCarouselLocked()
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
	var queue []pendingWork
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
			if handle := hostclient.NormalizeHandle(game.Cover); handle != "" {
				slot.handle = handle
				slot.phase = coverArtwork
			}
			a.covers[game.ID] = slot
		}
		if _, have := a.details[game.ID]; !have && slot.phase != coverIdle && slot.phase != coverArtwork {
			if _, busy := a.inflight[game.ID]; !busy && i == a.grid.Focus && !now.Before(slot.detailNext) {
				a.inflight[game.ID] = workPresentation
				queue = append(queue, pendingWork{item: workItem{kind: workPresentation, gameID: game.ID, handle: slot.handle, gen: a.loadGen}, key: game.ID})
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
			queue = append(queue, pendingWork{item: workItem{kind: workPresentation, gameID: game.ID, handle: slot.handle, gen: a.loadGen}, key: game.ID})
		case coverArtwork:
			if slot.handle == "" {
				slot.phase = coverMissing
				continue
			}
			a.inflight[game.ID] = workArtwork
			queue = append(queue, pendingWork{item: workItem{kind: workArtwork, gameID: game.ID, handle: slot.handle, gen: a.loadGen}, key: game.ID})
		}
		if len(a.inflight) >= maxInflight {
			break
		}
	}
	queue = a.queueRoomDetailWorkLocked(now, queue)
	queue = a.queueFocusedScreenshotsLocked(queue)
	a.evictCoversLocked()
	a.evictShotsLocked()
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
	if a.room != nil && a.detailOpen && a.roomDetail.ID == gameID {
		return true
	}
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
	if a.room != nil && a.detailOpen && a.roomDetail.ID != "" {
		keep[a.roomDetail.ID] = struct{}{}
	}
	for id := range a.covers {
		if _, ok := keep[id]; ok {
			continue
		}
		// Drop the slot even when its request is still running. A later result
		// is discarded by applyResult if the game remains outside the window;
		// retaining the slot would expose stale artwork until that request ends.
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
			shotGen := a.shotGen
			shotCtx := a.shotCtx
			a.mu.Unlock()
			if jobCtx == nil {
				jobCtx = ctx
			}
			workCtx := jobCtx
			if item.kind == workScreenshot && shotCtx != nil {
				workCtx = shotCtx
			}
			if item.gen != gen || parked || (item.kind == workScreenshot && item.shotGen != shotGen) {
				a.dropStaleWork(item)
				continue
			}
			result := a.doWork(workCtx, item)
			if item.gen != gen || workCtx.Err() != nil {
				a.dropStaleWork(item)
				continue
			}
			if item.kind == workScreenshot {
				a.mu.Lock()
				staleShot := item.shotGen != a.shotGen
				a.mu.Unlock()
				if staleShot {
					a.dropStaleWork(item)
					continue
				}
			}
			select {
			case a.results <- result:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (a *App) dropStaleWork(item workItem) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.loadGen != item.gen {
		return
	}
	if item.kind == workScreenshot && a.shotGen != item.shotGen {
		return
	}
	delete(a.inflight, item.key())
}

func (a *App) doWork(ctx context.Context, item workItem) workResult {
	switch item.kind {
	case workPresentation:
		pres, err := a.client.GamePresentation(ctx, item.gameID)
		if err != nil {
			return workResult{kind: workPresentation, gameID: item.gameID, gen: item.gen, err: err}
		}
		handle := shared.CoverHandle(hostclient.Game{ID: item.gameID}, pres)
		result := workResult{kind: workPresentation, gameID: item.gameID, handle: handle, missing: handle == "", state: pres.State, gen: item.gen}
		if pres.Presentation != nil {
			result.year = pres.Presentation.Year
			result.genre = pres.Presentation.Genre
			result.summary = pres.Presentation.Summary
			result.studio = pres.Presentation.Studio
			result.players = pres.Presentation.Players
			result.screenshotIDs = shared.ScreenshotHandles(pres.Presentation.ScreenshotIDs)
		}
		result.attribution = pres.AttributionLabel()
		return result
	case workArtwork:
		data, _, err := a.client.Artwork(ctx, item.handle)
		if err != nil {
			return workResult{kind: workArtwork, gameID: item.gameID, gen: item.gen, err: err}
		}
		img, err := shared.DecodeCover(data)
		if err != nil {
			return workResult{kind: workArtwork, gameID: item.gameID, gen: item.gen, err: err}
		}
		return workResult{kind: workArtwork, gameID: item.gameID, handle: item.handle, image: img, gen: item.gen}
	case workScreenshot:
		data, _, err := a.client.Artwork(ctx, item.handle)
		if err != nil {
			return workResult{kind: workScreenshot, gameID: item.gameID, handle: item.handle, gen: item.gen, shotGen: item.shotGen, err: err}
		}
		img, err := shared.DecodeScreenshot(data)
		if err != nil {
			return workResult{kind: workScreenshot, gameID: item.gameID, handle: item.handle, gen: item.gen, shotGen: item.shotGen, err: err}
		}
		return workResult{kind: workScreenshot, gameID: item.gameID, handle: item.handle, image: img, gen: item.gen, shotGen: item.shotGen}
	default:
		return workResult{kind: item.kind, gameID: item.gameID, gen: item.gen, err: fmt.Errorf("unknown work")}
	}
}
