package tenfoot

import (
	"context"
	"errors"
	"fmt"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/storage"
	"image"
	"io/fs"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/playhid"
	"github.com/DeanoC/FogCast/kitlease"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/inputmap"
	"github.com/DeanoC/FogCast/ui/rooms"
	"github.com/DeanoC/FogCast/ui/shared"
	"github.com/DeanoC/FogCast/ui/theme"
)

const (
	coverWorkers              = 6
	prefetchRows              = 2
	maxInflight               = 8
	coverJobBuffer            = 128
	searchDebounce            = 280 * time.Millisecond
	presentationRetryMin      = 400 * time.Millisecond
	presentationRetryMax      = 8 * time.Second
	sessionPollInterval       = time.Second
	shellLeaseReleaseAttempts = 3
)

const sessionDisplayUnavailableNotice = "HDMI controls unavailable. Back returns to play."

var errLibrarySuperseded = errors.New("library load superseded")

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
	StartedAt    time.Time
}

// SessionSnapshot is the live host session from GET /api/v1/session (and launch/stop).
type SessionSnapshot struct {
	State             string
	Chrome            string
	GameID            string
	Title             string
	System            string
	Coleco            bool // System or active Coleco bus identifies this session.
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
	LoadTape          bool // Live removable-media support, separate from ROM-linked cartridge swaps.
	ControlsNotice    string
	ControlsOpen      bool
	MediaSelector     bool // The kit session plane is available for direct media controls.
	// KeypadSelect keeps Select with a keypad machine; Guide opens controls.
	KeypadSelect bool
	HardwareRoom bool
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
	Games       []hostclient.Game
	Grid        Grid
	Status      string
	LoadErr     string
	Loading     bool
	Covers      map[string]*image.RGBA
	Launch      LaunchSnapshot
	Gamepads    int
	Keyboards   int
	Mice        int
	Affinity    InputKind
	AffinityID  int
	CoverHits   int
	Platforms   []hostclient.Platform
	PlatformID  string
	Sort        string
	Query       string
	SearchOpen  bool
	FocusDetail shared.FocusDetail
	// DetailPlay is the focused title's classified play destination: the
	// same readiness, reason and Confirm the launcher acts on.
	DetailPlay rooms.Destination
	// KitPlay is the kit state Library rows, cover cells and the header
	// classify with, the same as Details and Confirm.
	KitPlay kitPlayState
	// LibraryBackTo names the room Back returns to from the Library.
	LibraryBackTo   string
	Collection      string
	ViewLabel       string
	ViewPicker      bool
	ViewPickerIndex int
	Views           []LibraryView
	PickerRows      []LibraryView
	CollectionMenu  CollectionMenuSnapshot
	Session         SessionSnapshot
	GPUParked       bool
	Surround        SurroundSnapshot
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
	BrowseActions   BrowseActionsSnapshot
	Screenshots     map[string]*image.RGBA
	Preview         PreviewSnapshot
	Theme           theme.Theme
	DebugHUD        DebugHUDSnapshot
	Room            RoomSnapshot
	RoomPicker      RoomPickerSnapshot
	Storage         FirmwarePickerSnapshot
	CoreLibrary     FirmwarePickerSnapshot
	FirmwarePicker  FirmwarePickerSnapshot
	TapePicker      TapePickerSnapshot
	ReducedMotion   bool
	LocalCorePhase  string
	LocalCoreTitle  string
	// LocalCoreStatus is the latest kit-local launch or failure sentence.
	LocalCoreStatus         string
	LocalCoreStartedAt      time.Time
	LocalCorePresentsPaused bool
	LocalCoreLateAdopt      bool
	LocalCoreRedraw         uint64
}

// DebugHUDSnapshot is the optional corner overlay (off by default).
type DebugHUDSnapshot struct {
	Enabled bool
	Lines   []string
}

// App owns catalog, focus, async covers, and host launch. SDL stays out.
type App struct {
	driveManager            *storage.Manager
	localDrives, hostDrives []storage.Drive
	driveRootsGen           int
	driveSettings           driveSettingsState

	coreLibrary coreLibraryState
	client      *Client
	ctx         context.Context
	cancel      context.CancelFunc

	mu                      sync.Mutex
	games                   []hostclient.Game
	grid                    Grid
	covers                  map[string]*coverSlot
	inflight                map[string]workKind
	status                  string
	statusLeaseRefusal      bool
	launchLeaseRefusal      bool
	loadErr                 string
	loading                 bool
	libraryRetrying         bool // catalog retry is in flight; independent of the Select gate
	launch                  LaunchSnapshot
	launchPackageID         string // pending Workshop package selection retained for retry
	launchExpectedPackageID string
	gamepads                int
	keyboards               int
	mice                    int
	affinity                affinityTracker
	repeat                  Repeater
	theme                   theme.Theme
	jobs                    chan workItem
	results                 chan workResult
	maxGames                int
	pageLimit               int

	platforms              []hostclient.Platform
	platformID             string
	libraryCoreIDs         []string
	shelfQuery             hostclient.GameListQuery
	shelfQuerySet          bool
	shelfGames             []hostclient.Game
	sort                   string
	searchField            shared.TextField
	searchOpen             bool
	searchPending          bool
	searchDue              time.Time
	details                map[string]shared.FocusDetail
	shots                  map[string]*shotSlot
	shotIDs                map[string][]string
	detailOpen             bool
	libraryDetail          hostclient.Game
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
	playingSetupSession    hostclient.SessionResult
	playingSetupCoreID     string
	playingSetupPending    bool
	playingSetupRequest    uint64
	playingSetupCancel     context.CancelFunc
	playingSetupRoom       *rooms.Instance
	sessionTitle           string
	// retainedIdleLease is set when this shell's Soft-stop kept an idle kit
	// grant, and when a kit Select+Start release failed while that grant may
	// still be renewing. A later active or relaunched play does not clear it:
	// another target's grant can still be retained. Shell exit and Release
	// kit are the global idle release. A bound Select+Start drops only the
	// captured target, and a host-only stop drops none, so those exits leave
	// every other retained grant tracked. B/Back does not release.
	retainedIdleLease    bool
	retainedIdleTargets  map[string]struct{}
	retainedIdleUnscoped bool
	leaseReleaseBusy     bool
	// stopResponseLost is a Soft-stop whose transport failed after the host
	// may already have retained the grant. Shell exit reconciles it.
	stopResponseLost bool
	// stopWait is closed when the in-flight Soft-stop goroutine returns.
	stopWait     chan struct{}
	sessionGen   int
	stopPhase    string
	stopExpected hostclient.SessionResult
	// stopReleasesLease is the kit Select+Start completed exit. A retry of
	// that stop keeps releasing. Rooms B/Back leaves it false. Session
	// change clears it so the next stop captures the new play on its own.
	stopReleasesLease bool
	// stopRetryIdleRelease is a Select+Start whose cleanup reached idle and
	// then failed to release the grant. The host rejects the captured play
	// once idle, so Retry Stop posts the idle-grant release instead.
	stopRetryIdleRelease bool
	// stopCaptured is a Select+Start or Soft-stop queued while input was
	// busy. stopExpected is the play from chord time, not from the flush.
	stopCaptured       bool
	stopMessage        string
	gpuParked          bool
	sessionKick        chan struct{}
	health             hostclient.HealthResult
	healthHave         bool
	hostUnreachable    bool
	inputBusy          bool
	inputAction        string
	inputMessage       string
	stopQueued         bool
	retryStopLock      bool
	retryStopHint      string
	retryStopCode      string
	sessionEvents      []hostclient.SessionEvent
	sessionEventAfter  uint64
	flightID           string
	lastFocusKey       string
	lastNavKey         string
	debugHUD           bool
	kitLease           KitLeaseStatus
	kitLeaseHave       bool
	developmentRBFPath string
	devLoadPhase       string
	devLoadMessage     string

	roomsIndex          *rooms.Index
	roomsDir            string
	homeRooms           bool
	homeRoom            string
	gfxBackend          string
	pinnedRooms         []string
	reducedMotion       bool
	homeRecents         []hostclient.Game
	homeRecentsErr      string
	homeRecentsLoaded   bool
	homeRecentsGen      int
	room                *rooms.Instance
	roomStack           []*rooms.Instance
	roomFrame           rooms.Frame
	roomErr             string
	roomWasParked       bool
	roomDuringPlay      bool
	mediaControlsDirect bool
	roomSessionNotice   string
	// roomWaitAck is a Confirm received while the selected destination was
	// still Checking. It is shown, never queued, and clears once the
	// destination is no longer Checking.
	roomWaitAck bool
	// frameTrace records frame timings on the kit display loop (nil elsewhere).
	frameTrace *frameTrace
	// frameLua is the room Step from the Tick in progress. Tick clears it,
	// and only a Step that actually ran this iteration fills it again.
	frameLua rooms.StepTiming
	// roomCoreDetail is the package whose Details the hero is showing.
	roomCoreDetail string
	browseActions  browseActionState
	// libraryReturn is the suspended room the Library was opened from.
	libraryReturn *libraryOrigin
	// firmwarePickerFromDetail reopens Library Details when the picker is
	// cancelled, so the requirement stays in front of the player.
	firmwarePickerFromDetail bool
	menuDisplay              *gfx.MenuDisplay
	sessionDisplayBusy       bool
	sessionDisplay           protocol.SessionDisplay
	sessionDisplayUncertain  bool
	surround, surroundFailed bool
	sessionDisplayEpoch      uint64
	sessionDisplayBinding    hostclient.SessionResult
	sessionDisplayWait       chan struct{}
	playKeyHeld              map[string]bool
	playKeySuppressed        map[string]bool
	playPadHeld              map[padKey]remoteinput.Event
	playPadSuppressed        map[padKey]bool
	playPadPair              chordPairs
	playPadChordSince        time.Time
	playPadChordFired        bool
	playHIDHeld              map[playHIDKey]remoteinput.Event
	playMice                 map[int]*playMouse
	playHIDTail              chan struct{}
	playHIDContext           context.Context
	playHIDCancel            context.CancelFunc
	playHIDEpoch             uint64
	playHIDPending           int
	roomPickerOpen           bool
	roomPickerIndex          int
	roomCoverSem             chan struct{}
	roomDetail               hostclient.Game
	roomChoiceOpen           bool
	roomChoiceIndex          int
	roomChoice               []hostclient.Game
	roomPicks                map[string]string
	firmwarePickerOpen       bool
	firmwarePickerAtRoots    bool
	firmwarePickerBusy       bool
	firmwarePickerIndex      int
	firmwarePickerGen        int
	firmwarePickerPath       string
	firmwarePickerStatus     string
	firmwarePickerRows       []FirmwarePickerRow
	firmwarePickerGame       hostclient.Game
	tapePickerOpen           bool
	tapePickerAtRoots        bool
	tapePickerBusy           bool
	tapePickerIndex          int
	tapePickerGen            int
	tapePickerPath           string
	tapePickerStatus         string
	tapePickerRows           []TapePickerRow
	tapePickerSession        hostclient.SessionResult
	tapePickerStarters       []hostclient.HardwareTape

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

	localCores                 rooms.LocalCores
	localFeed                  localPadSender
	localInstallKnown          bool
	localInstalled             map[string]struct{}
	localInstallGen            uint64
	localInstallCancel         context.CancelFunc
	localCatalogClose          func() error
	localContent               func(context.Context, string) (string, string, error)
	localPhase                 string
	localReconcileAfterFailure bool
	localReconcileDeadline     time.Time
	localUnownedRun            bool
	localTitle                 string
	localStartedAt             time.Time
	localStatus                string
	localGen                   uint64
	localLeaseGeneration       string
	localPackageID             string
	localPresentsPaused        bool
	localLateAdopt             bool
	localRedraw                uint64
	localPair                  chordPairs
	localChordSince            time.Time
	localChordFired            bool
	localStopAfterStart        bool
	localSent                  map[padKey]bool
	localStatusBusy            bool
	localStatusNext            time.Time
	localStatusEpoch           uint64
	// localStartupStatusChecked is set after the first successful local
	// status read. A later idle menu does not keep polling.
	localStartupStatusChecked bool
	// localWatchStartup keeps polling a launching or stopping generation
	// seen at startup until that generation is running or generation-free idle.
	localWatchStartup  bool
	localLaunchPending func()
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

// padRemap compiles the profile tenfoot.json saves for a pad's GUID, or
// returns nil when there is none. It is read as the pad attaches, so an
// edit applies when the pad is next plugged in.
func (a *App) padRemap(guid string) (*inputmap.Remapper, error) {
	a.mu.Lock()
	path := a.prefsPath
	a.mu.Unlock()
	if path == "" {
		return nil, nil
	}
	prefs, err := loadTenfootPrefs(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	profile, ok := prefs.Controllers[guid]
	if !ok {
		return nil, nil
	}
	return inputmap.NewRemapper(profile)
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

// Stop waits for an in-flight Soft-stop, releases an idle lease this shell
// retained, then cancels background work.
func (a *App) Stop() {
	if a == nil {
		return
	}
	a.mu.Lock()
	done := a.stopWait
	displayDone := a.sessionDisplayWait
	a.mu.Unlock()
	if done != nil {
		<-done
	}
	if displayDone != nil {
		<-displayDone
	}
	a.returnSessionDisplayOnExit()
	a.releaseOwnedIdleLease()
	a.mu.Lock()
	a.hideAttractLocked()
	a.stopPreviewLocked()
	a.cancelPlayHIDLocked()
	a.closeAllRoomsLocked()
	a.discardLibraryOriginLocked()
	a.attractClosed = true
	feed := a.localFeed
	a.localFeed = nil
	closeCatalog := a.localCatalogClose
	a.localCatalogClose = nil
	installCancel := a.localInstallCancel
	a.localInstallCancel = nil
	cancel := a.cancel
	a.mu.Unlock()
	if installCancel != nil {
		installCancel()
	}
	if feed != nil {
		feed.Close()
	}
	if closeCatalog != nil {
		_ = closeCatalog()
	}
	if cancel != nil {
		cancel()
	}
	a.drainAttractResults()
}

// releaseOwnedIdleLease posts the shell-exit release when this shell still
// owns a retained idle grant or a Soft-stop response was lost. The local
// session may already show another play; that does not skip the release.
// A surviving play is not stopped: that path posts release_idle. A confirmed
// idle service posts an empty-body Stop. The pending flag clears only after
// that release succeeds.
func (a *App) releaseOwnedIdleLease() {
	if a == nil {
		return
	}
	a.mu.Lock()
	client := a.client
	release := a.retainedIdleLease || a.stopResponseLost
	var stamp ClientStamp
	if release {
		stamp = a.clientStampLocked()
	}
	a.mu.Unlock()
	if !release || client == nil {
		return
	}
	if releaseIdleLeaseNow(client, stamp) {
		a.mu.Lock()
		a.clearRetainedLeasesLocked()
		a.stopResponseLost = false
		a.mu.Unlock()
	}
}

// startReleaseIdleLeaseLocked releases a kit this shell kept after returning
// to the menu. The menu stays open. A running game is left owned.
// The idle read and the Stop are separate steps. Launches stay closed until
// the goroutine finishes, including after settings closes, so that Stop
// cannot end a game that started in between.
func (a *App) startReleaseIdleLeaseLocked() {
	if a.leaseReleaseBusy {
		return
	}
	if a.session.State == "active" || a.session.State == "launching" || a.launch.Phase == "launching" || a.stopPhase == "stopping" {
		a.settingsStatus = "A running game keeps the kit"
		a.status = a.settingsStatus
		return
	}
	if !a.retainedIdleLease && !a.stopResponseLost {
		a.settingsStatus = "The kit is already free"
		a.status = a.settingsStatus
		return
	}
	client := a.client
	if client == nil {
		a.settingsStatus = "The kit is already free"
		a.status = a.settingsStatus
		return
	}
	stamp := a.clientStampLocked()
	a.leaseReleaseBusy = true
	a.settingsStatus = "Releasing the kit"
	a.status = a.settingsStatus
	go func() {
		ok := releaseIdleLeaseNow(client, stamp)
		a.mu.Lock()
		defer a.mu.Unlock()
		a.leaseReleaseBusy = false
		if !ok {
			if a.settingsOpen {
				a.settingsStatus = "Could not release the kit"
				a.status = a.settingsStatus
			}
			return
		}
		a.clearRetainedLeasesLocked()
		a.stopResponseLost = false
		if a.settingsOpen {
			a.settingsStatus = "Kit released"
			a.status = "Kit released. The menu stays open."
		}
	}()
}

func releaseIdleLeaseNow(client *Client, stamp ClientStamp) bool {
	if client == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	view := observeLeaseExit(ctx, client)
	cancel()
	var releaseFn func(*Client, context.Context, ClientStamp) (hostclient.SessionResult, error)
	switch view {
	case leaseExitIdle:
		releaseFn = (*Client).ReleaseIdleLease
	default:
		// A surviving play, or a service we could not read. release_idle does
		// not stop the foreground. An empty-body Stop would.
		releaseFn = (*Client).ReleaseIdleGrants
	}
	return postLeaseRelease(client, stamp, releaseFn)
}

type leaseExitView int

const (
	leaseExitUnknown leaseExitView = iota
	leaseExitIdle
	leaseExitPlaySurvives
)

func observeLeaseExit(ctx context.Context, client *Client) leaseExitView {
	if client == nil {
		return leaseExitUnknown
	}
	session, err := client.Session(ctx)
	if err != nil || session.ErrorCode != "" {
		return leaseExitUnknown
	}
	count, err := client.survivingPlayCount(ctx)
	switch {
	case err == nil && count > 0:
		return leaseExitPlaySurvives
	case err != nil && !errors.Is(err, errSessionsUnsupported):
		return leaseExitUnknown
	}
	if !idleRetainedSession(session.State) {
		return leaseExitPlaySurvives
	}
	return leaseExitIdle
}

func postLeaseRelease(client *Client, stamp ClientStamp, release func(*Client, context.Context, ClientStamp) (hostclient.SessionResult, error)) bool {
	if client == nil || release == nil {
		return false
	}
	for attempt := 0; attempt < shellLeaseReleaseAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		result, err := release(client, ctx, stamp)
		cancel()
		if err == nil && strings.TrimSpace(result.ErrorCode) == "" {
			return true
		}
	}
	return false
}

func idleRetainedSession(state string) bool {
	if hostclient.SessionKitLocal(state) {
		return false
	}
	switch strings.TrimSpace(state) {
	case "", "idle":
		return true
	default:
		return false
	}
}

// HandleCommand applies a gamepad, USB-keyboard, or pointer-mapped command.
func (a *App) HandleCommand(cmd Command, now time.Time) {
	if cmd == CmdNone {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.traceInputLocked(cmd, now)
	a.noteActivityLocked(now)
	if a.localCoreBusyLocked() {
		return
	}
	if a.sessionDisplayBusy {
		if cmd == CmdStop {
			a.startStopLocked()
		}
		return
	}
	if cmd == CmdBack && a.sessionDisplayUncertain {
		a.beginSessionDisplayLocked(a.sessionReturnDisplayLocked(), false)
		return
	}
	if a.mediaControlsDirect && a.roomDuringPlay {
		if cmd == CmdSettings {
			return
		}
		if !a.tapePickerOpen {
			switch cmd {
			case CmdBack, CmdHome, CmdLayoutCycle:
				a.resumeRoomSessionLocked()
			case CmdStop:
				a.startStopLocked()
			}
			return
		}
	}
	a.localStatus = ""
	if a.driveSettings.Open {
		if a.settingsOSKOpenLocked() {
			a.handleSettingsOSKLocked(cmd)
		} else {
			a.handleDriveSettingsLocked(cmd)
		}
		return
	}
	if a.coreLibrary.Open {
		if a.settingsOSKOpenLocked() {
			a.handleSettingsOSKLocked(cmd)
		} else {
			a.handleCoreLibraryLocked(cmd)
		}
		return
	}
	if a.consumeAttractLocked(cmd, now) {
		return
	}
	if a.searchOpen || a.nameEntryOpenLocked() || a.settingsOSKOpenLocked() {
		switch cmd {
		case CmdSafeAreaIn, CmdSafeAreaOut, CmdLayoutCycle:
			return
		}
	}
	roomOwnsInput := a.room != nil && !a.roomPickerOpen && !a.settingsOpen && !a.settingsOSKOpenLocked() && !a.firmwarePickerOpen && !a.tapePickerOpen
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
		if a.firmwarePickerOpen || a.tapePickerOpen {
			return
		}
		if a.sessionStopOfferedLocked() {
			if a.roomDuringPlay {
				a.resumeRoomSessionLocked()
			} else {
				a.openPlayingHardwareRoomLocked()
			}
			return
		}
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
		if a.tapePickerOpen {
			a.dismissMediaControlsLocked()
			return
		}
		if a.sessionStopOfferedLocked() {
			if a.roomDuringPlay {
				a.toggleRoomPickerLocked()
			} else {
				a.openPlayingHardwareRoomLocked()
			}
			return
		}
		if a.firmwarePickerOpen {
			a.closeFirmwarePickerLocked()
		}
		if !a.openHomeRoomAsRootLocked() {
			a.toggleRoomPickerLocked()
		}
		return
	case CmdSettings:
		if a.tapePickerOpen {
			a.closeTapePickerLocked()
		}
		if a.firmwarePickerOpen {
			a.closeFirmwarePickerLocked()
		}
		if a.settingsOpen {
			a.closeSettingsLocked()
		} else {
			a.openSettingsLocked()
		}
		return
	case CmdFilters:
		if a.firmwarePickerOpen || a.tapePickerOpen {
			return
		}
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
	if a.tapePickerOpen {
		a.handleTapePickerLocked(cmd)
		return
	}
	if a.firmwarePickerOpen {
		a.handleFirmwarePickerLocked(cmd)
		return
	}
	if a.roomPickerOpen && (!a.sessionStopOfferedLocked() || a.roomDuringPlay) {
		a.handleRoomPickerLocked(cmd)
		return
	}
	if a.room != nil && (!a.sessionStopOfferedLocked() || a.roomDuringPlay) {
		if cmd == CmdStop {
			a.startStopLocked()
			return
		}
		if cmd == CmdBack && a.roomDuringPlay && !a.roomChoiceOpen && !a.detailOpen {
			a.resumeRoomSessionLocked()
			return
		}
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
			if a.room != nil {
				if dest := a.roomDestinationLocked(); dest.Confirm() == rooms.ConfirmLauncherAction {
					a.runLauncherActionLocked(dest.LauncherAction)
				}
			}
			return
		case CmdSearch, CmdDetails:
			if a.sessionLiveMediaOfferedLocked() || (!a.roomDuringPlay && a.colecoCartridgeOfferedLocked()) {
				a.openTapePickerLocked()
			}
			return
		case CmdSortCycle:
			a.startInputToggleLocked()
			return
		case CmdUp, CmdDown, CmdLeft, CmdRight, CmdFilterPrev, CmdFilterNext, CmdViewPrev, CmdViewNext, CmdViewPicker, CmdFavorite, CmdFilters, CmdSafeAreaIn, CmdSafeAreaOut, CmdTab, CmdTabPrev:
			return
		}
	}
	if a.handleBrowseActionsLocked(cmd) {
		return
	}
	if cmd == CmdSearch && a.affinity.current.Kind == InputGamepad {
		if d, _, ok := a.browseActionsDestinationLocked(); ok {
			if _, hasSecondary := browseActionLabels(d); hasSecondary {
				a.openDetailLocked()
				return
			}
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
		} else {
			a.returnFromLibraryLocked()
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
	a.libraryCoreIDs = nil
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
	game, ok := a.favoriteTargetLocked()
	if !ok {
		a.status = "no title selected"
		return
	}
	want := !game.Favorite
	a.setGameFavoriteLocked(game.ID, want)
	a.favoriteBusy = true
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go a.doFavorite(ctx, game.ID, want)
}

// favoriteTargetLocked is the title Favorite changes. Open Details for a
// title that is not the focused row, such as a Home recent, owns the action.
func (a *App) favoriteTargetLocked() (hostclient.Game, bool) {
	if a.room == nil && strings.TrimSpace(a.libraryDetail.ID) != "" {
		return a.libraryDetail, true
	}
	if a.grid.Focus < 0 || a.grid.Focus >= len(a.games) {
		return hostclient.Game{}, false
	}
	return a.games[a.grid.Focus], true
}

func (a *App) setGameFavoriteLocked(gameID string, favorite bool) {
	if a.libraryDetail.ID == gameID {
		a.libraryDetail.Favorite = favorite
	}
	for i := range a.homeRecents {
		if a.homeRecents[i].ID == gameID {
			a.homeRecents[i].Favorite = favorite
		}
	}
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
	a.frameLua = rooms.StepTiming{}
	a.flushSearchLocked(now)
	a.tickAttractLocked(now)
	a.syncPreviewLocked()
	a.tickLocalCoreLocked(now)
	a.tickSessionPadLocked(now)
	a.syncSurroundLocked()
	// Stop drains a shown plane, so its frames fail without a display fault.
	if a.sessionDisplay.Visible && !a.sessionDisplayBusy && a.stopPhase != "stopping" && a.menuDisplay != nil {
		if err := a.menuDisplay.LastError(); err != nil {
			a.roomSessionNotice = sessionDisplayUnavailableNotice
		} else if a.roomSessionNotice == sessionDisplayUnavailableNotice {
			a.roomSessionNotice = ""
		}
	}
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
// replaces it rather than mutating elements. Visible labels project prepared
// names while drawing. Cover images are shared; the cover map is copied.
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
	if strings.TrimSpace(a.localStatus) != "" {
		status = a.localStatus
	} else if a.tapePickerOpen && strings.TrimSpace(a.tapePickerStatus) != "" {
		status = a.tapePickerStatus
	} else if line := a.stopStatusLocked(); line != "" {
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
	} else if a.session.State == "active" && strings.HasPrefix(strings.TrimSpace(a.status), "Tape ") {
		status = strings.TrimSpace(a.status)
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
	snap := Snapshot{
		Games:                   games,
		Grid:                    a.grid,
		Status:                  status,
		LoadErr:                 a.loadErr,
		Loading:                 a.loading,
		Covers:                  covers,
		Launch:                  a.launch,
		Gamepads:                a.gamepads,
		Keyboards:               a.keyboards,
		Mice:                    a.mice,
		Affinity:                a.affinity.current.Kind,
		AffinityID:              a.affinity.current.ID,
		CoverHits:               hits,
		Platforms:               a.platforms,
		PlatformID:              a.platformID,
		Sort:                    a.sort,
		Query:                   a.searchField.Buffer,
		SearchOpen:              a.searchOpen,
		FocusDetail:             a.focusDetailLocked(),
		DetailPlay:              a.detailPlayLocked(),
		KitPlay:                 a.kitPlayStateLocked(),
		LibraryBackTo:           a.libraryBackToLocked(),
		Collection:              a.collectionID,
		ViewLabel:               a.viewLabelLocked(),
		ViewPicker:              a.viewPickerOpen,
		ViewPickerIndex:         a.viewPickerIndex,
		Views:                   a.viewChoicesLocked(),
		PickerRows:              a.pickerRowsLocked(),
		CollectionMenu:          a.collectionMenuSnapshotLocked(),
		Session:                 a.sessionSnapshotLocked(),
		GPUParked:               a.gpuParked,
		Surround:                SurroundSnapshot{Active: a.surroundShownLocked()},
		Attract:                 a.attractSnapshotLocked(),
		SafeAreaPct:             a.safeAreaPct,
		Settings:                a.settingsSnapshotLocked(),
		Filters:                 a.filtersSnapshotLocked(),
		Genre:                   a.filterGenre,
		Year:                    a.filterYear,
		Region:                  a.filterRegion,
		HidePrerelease:          a.hidePrerelease,
		HideHacks:               a.hideHacks,
		OSK:                     a.oskSnapshotLocked(),
		Health:                  health,
		KitLease:                a.kitLeaseSnapshotLocked(),
		Detail:                  a.detailSnapshotLocked(),
		Screenshots:             a.screenshotImagesLocked(),
		Preview:                 a.previewSnapshotLocked(),
		Theme:                   a.theme.Complete(),
		DebugHUD:                a.debugHUDSnapshotLocked(),
		Room:                    a.roomSnapshotLocked(true),
		RoomPicker:              a.roomPickerSnapshotLocked(),
		Storage:                 a.driveSettingsSnapshotLocked(),
		CoreLibrary:             a.coreLibrarySnapshotLocked(),
		FirmwarePicker:          a.firmwarePickerSnapshotLocked(),
		TapePicker:              a.tapePickerSnapshotLocked(),
		ReducedMotion:           a.reducedMotion,
		LocalCorePhase:          a.localPhase,
		LocalCoreTitle:          a.localTitle,
		LocalCoreStatus:         a.localStatus,
		LocalCoreStartedAt:      a.localStartedAt,
		LocalCorePresentsPaused: a.localPresentsPaused,
		LocalCoreLateAdopt:      a.localLateAdopt,
		LocalCoreRedraw:         a.localRedraw,
	}
	destination := snap.DetailPlay
	if snap.Room.Open {
		destination = snap.Room.Destination
	}
	snap.BrowseActions = a.browseActionsSnapshotLocked(destination)
	return snap

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
		case settingsOSKDriveField:
			snap.Prompt = "SMB " + a.driveSettings.Status
			if a.driveSettings.Status == "password" {
				snap.Masked = true
				snap.Buffer = shared.MaskSecret(a.settingsOSKField.Buffer)
			}
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
		case settingsOSKCoreTitle:
			snap.Prompt = "Game title"
		case settingsOSKCorePath:
			snap.Prompt = "ROM or media path"
		case settingsOSKFirmwarePath:
			if a.entryFirmwarePickerLocked() {
				snap.Prompt = "Firmware ROM path"
			} else {
				snap.Prompt = "Coleco BIOS path"
			}
		case settingsOSKTapePath:
			snap.Prompt = "Media path"
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
	if a.coreLibrary.Open || a.gpuParked || a.stopPhase == "stopping" || a.launch.Phase == "launching" || a.retryStopLock || a.developmentLoadingLocked() {
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
	if s.Grid.Focus >= 0 && s.Grid.Focus < len(s.Games) {
		play := classifyLibraryGame(s.Games[s.Grid.Focus], s.KitPlay)
		if label := play.Availability.Label(); label != "" {
			parts = append(parts, label)
		}
	}
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

// detailPlayLocked classifies the title Details describes. In a room it is
// the room's own destination. In the Library it is the refused launch when
// that title is not the focused row, and otherwise the focused row, with the
// kit lease applied as Confirm would see it.
func (a *App) detailPlayLocked() rooms.Destination {
	var game hostclient.Game
	switch {
	case a.room != nil && strings.TrimSpace(a.roomDetail.ID) != "":
		// Details for one chosen edition describes that edition.
		game = a.roomDetail
		d := a.gamePlayDestinationLocked(game)
		if a.session.State == "active" && a.roomDuringPlay && a.room.ID() == colecoArcadeRoomID && a.colecoSessionLocked() && d.Confirm() == rooms.ConfirmLaunch {
			d.Status = colecoRunningDestinationStatus
			d.Action = colecoRunningDestinationAction
		}
		return d
	case a.room != nil:
		return a.roomDestinationLocked()
	case strings.TrimSpace(a.libraryDetail.ID) != "":
		game = a.libraryDetail
	case a.grid.Focus < 0 || a.grid.Focus >= len(a.games):
		return rooms.Destination{}
	default:
		game = a.games[a.grid.Focus]
	}
	return a.gamePlayDestinationLocked(game)
}

// gamePlayDestinationLocked classifies one title as Confirm will treat it.
func (a *App) gamePlayDestinationLocked(game hostclient.Game) rooms.Destination {
	return classifyLibraryGame(game, a.kitPlayStateLocked())
}

// kitPlayStateLocked is the kit state every Library classification shares.
func (a *App) kitPlayStateLocked() kitPlayState {
	paired := a.client != nil && a.client.paired
	return kitPlayState{
		Paired:  paired,
		Unknown: paired && a.pairedKitStatusUnavailableLocked(),
		Foreign: a.foreignKitLeaseLocked(),
	}
}

func (a *App) focusDetailLocked() shared.FocusDetail {
	if a.room != nil && strings.TrimSpace(a.roomDetail.ID) != "" {
		return a.detailForGameLocked(a.roomDetail)
	}
	if a.room == nil && strings.TrimSpace(a.libraryDetail.ID) != "" {
		return a.detailForGameLocked(a.libraryDetail)
	}
	if a.grid.Focus < 0 || a.grid.Focus >= len(a.games) {
		return shared.FocusDetail{}
	}
	return a.detailForGameLocked(a.games[a.grid.Focus])
}

func (a *App) detailForGameLocked(game hostclient.Game) shared.FocusDetail {
	detail := shared.FocusDetail{
		Title:    displayGameTitle(game),
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
	if dy > 0 && a.detailEnterFromBrowseLocked() && a.enterBrowseActionsLocked(0) {
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
	if dy > 0 {
		a.enterBrowseActionsLocked(0)
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
	if a.localLaunchBlockedLocked() {
		a.status = localLaunchCheckingCopy
		return
	}
	if a.launch.Phase == "launching" || hostclient.SessionKitLocal(a.session.State) || a.sessionStopOfferedLocked() || a.developmentLoadingLocked() {
		if a.session.State == "active" && a.colecoSessionLocked() && a.roomDuringPlay {
			a.status = colecoCartridgeSessionStatus
		}
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
		a.launchLeaseRefusal = false
		a.launch = LaunchSnapshot{Phase: "error", Message: "no title selected"}
		return
	}
	game := a.games[a.grid.Focus]
	if libraryPlayDestination(game).Confirm() == rooms.ConfirmWait {
		a.status = rooms.CheckingStatus
		return
	}
	a.startLaunchGameLocked(game)
}

func (a *App) startLaunchGameLocked(game hostclient.Game) {
	a.startLaunchGameWithPackageLocked(game, "", "")
}

func (a *App) startLaunchGameWithPackageLocked(game hostclient.Game, packageID, expectedPackageID string) {
	// Release kit may already have read an idle session and still be about
	// to post an empty-body Stop. That Stop would end this game.
	if a.leaseReleaseBusy {
		a.status = "Releasing the kit"
		return
	}
	if a.localLaunchBlockedLocked() {
		a.status = localLaunchCheckingCopy
		return
	}
	if a.launch.Phase == "launching" || hostclient.SessionKitLocal(a.session.State) || a.sessionStopOfferedLocked() || a.developmentLoadingLocked() {
		if a.session.State == "active" && a.colecoSessionLocked() && a.roomDuringPlay {
			a.status = colecoCartridgeSessionStatus
		}
		return
	}
	// Firmware import and other catalog blocks do not claim the kit.
	// A foreign lease applies only when Confirm would launch.
	a.clearStaleDevelopmentLoadLocked()
	if missingFirmwareGame(game) {
		a.openFirmwarePickerLocked(game)
		return
	}
	if a.client != nil && a.client.paired && a.kitMutationBlockedLocked() {
		message := a.status
		a.launch = LaunchSnapshot{GameID: game.ID, Phase: "error", Message: message}
		a.launchLeaseRefusal = a.statusLeaseRefusal
		if a.room != nil {
			a.closeRoomOverlaysLocked()
		} else {
			a.openLibraryDetailLocked(game)
		}
		return
	}
	if reason := launchBlockReason(game); reason != "" {
		a.launchLeaseRefusal = false
		a.launch = LaunchSnapshot{GameID: game.ID, Phase: "error", Message: reason}
		if a.room != nil {
			a.closeRoomOverlaysLocked()
		} else {
			// The reason and its remedy are in Details, not only the header.
			a.openLibraryDetailLocked(game)
		}
		return
	}
	if a.foreignKitLeaseLocked() && !game.HostOnly() {
		a.launch = LaunchSnapshot{GameID: game.ID, Phase: "error", Message: localInUseCopy}
		a.launchLeaseRefusal = true
		if a.room != nil {
			a.closeRoomOverlaysLocked()
		} else {
			a.openLibraryDetailLocked(game)
		}
		return
	}
	a.launchPackageID = packageID
	a.launchExpectedPackageID = expectedPackageID
	a.sessionTitle = displayGameTitle(game)
	a.cancelPlayHIDLocked()
	a.launchLeaseRefusal = false
	a.launch = LaunchSnapshot{
		GameID:    game.ID,
		Phase:     "launching",
		Message:   "launching " + displayGameTitle(game),
		StartedAt: time.Now(),
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
	go a.doLaunch(ctx, game, stamp, packageID, expectedPackageID)
}

// launchBlockReason maps hostclient catalog ineligibility to sofa copy.
// Admission rules live on hostclient.Game; unavailable titles must not
// POST /api/v1/session/launch.
func launchBlockReason(game hostclient.Game) string {
	if game.LaunchBlock() == "" {
		return ""
	}
	return rooms.LaunchBlockCopy(game)
}

func libraryPlayDestination(game hostclient.Game) rooms.Destination {
	state, matches := rooms.ClassifyGames([]hostclient.Game{game}, "")
	d := rooms.Destination{Kind: rooms.KindGame, Availability: state, Matches: matches, GameID: game.ID, Label: displayGameTitle(game), System: game.System}
	d.FillCopy()
	return d
}

func (a *App) doLaunch(ctx context.Context, game hostclient.Game, stamp ClientStamp, packageID, expectedPackageID string) {
	if packageID != "" {
		_, err := a.client.SelectCoreEntryPackage(ctx, game.ID, expectedPackageID, packageID)
		if err != nil {
			// The host may commit the compare-and-swap and lose its response.
			// Confirm the saved entry before deciding that retry must remain
			// pending or that another selection should stop this launch.
			if current, readErr := a.client.CoreEntry(ctx, game.ID); readErr == nil {
				switch {
				case current.PackageID == packageID:
					err = nil
				case current.PackageID != expectedPackageID:
					err = errors.New("saved package selection changed while launch was pending")
				}
			}
		}
		if err != nil {
			a.mu.Lock()
			defer a.mu.Unlock()
			if a.launch.GameID != game.ID {
				return
			}
			a.bumpSessionGenLocked()
			a.launch.Phase = "error"
			a.launchLeaseRefusal = false
			a.launch.Message = "package selection failed: " + err.Error()
			a.launch.ErrorMessage = err.Error()
			return
		}
		a.mu.Lock()
		if a.launch.GameID == game.ID && a.launch.Phase == "launching" {
			a.launchPackageID = ""
			a.launchExpectedPackageID = ""
		}
		a.mu.Unlock()
	}
	result, err := a.client.LaunchStamped(ctx, game.ID, stamp)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.launch.GameID != game.ID {
		return
	}
	a.bumpSessionGenLocked()
	if err != nil {
		a.launch.Phase = "error"
		a.launchLeaseRefusal = false
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
		a.launchLeaseRefusal = false
		a.launch.Message = fmt.Sprintf("host launch %d %s: %s", result.HTTPStatus, result.ErrorCode, result.ErrorMessage)
		return
	}
	a.launch.Phase = "ok"
	a.launchLeaseRefusal = false
	a.launch.Message = fmt.Sprintf("host accepted launch for %s", game.ID)
	a.applySessionLocked(result)
	if a.session.State == "active" && a.session.GameID == "" {
		a.session.GameID = game.ID
	}
	activePackage := ""
	if a.session.CorePackage != nil {
		activePackage = a.session.CorePackage.PackageID
	}
	expectedPackage := game.PackageID
	if packageID != "" {
		expectedPackage = packageID
	}
	if a.session.State == "active" && a.session.GameID == game.ID && game.CoreID != "" && activePackage == expectedPackage {
		a.rememberPlayingSetupLocked(game.CoreID)
	}
	a.kickSessionPollLocked()
}

func (a *App) sessionStopOfferedLocked() bool {
	return a.retryStopLock || a.session.State == "active" || a.stopPhase == "stopping"
}

func (a *App) startStopLocked() {
	a.startStopModeLocked(false)
}

// startReleaseStopLocked is the kit Select+Start completed exit. Idle cleanup
// releases the captured play's kit grant. Rooms B/Back stays on startStopLocked
// and retains the lease for the unpaired sofa Soft-stop.
func (a *App) startReleaseStopLocked() {
	a.startStopModeLocked(true)
}

func (a *App) startStopModeLocked(releaseLease bool) {
	if a.stopPhase == "stopping" {
		return
	}
	if !a.retryStopLock && a.session.State != "active" {
		if a.stopCaptured {
			a.discardCapturedStopLocked()
		}
		return
	}
	if a.inputBusy {
		a.stopQueued = true
		if !a.retryStopLock {
			a.stopExpected = a.session
			a.stopReleasesLease = releaseLease
			a.stopCaptured = true
		}
		return
	}
	if !a.retryStopLock && a.stopCaptured && !samePlayHIDSession(a.stopExpected, a.session) {
		// The chord was for the play on screen while input was in flight.
		// Flushing after a replacement must not capture that replacement.
		a.discardCapturedStopLocked()
		return
	}
	if !a.retryStopLock {
		if !a.stopCaptured {
			a.stopExpected = a.session
			a.stopReleasesLease = releaseLease
		}
		a.stopCaptured = false
	}
	a.roomDuringPlay = false
	a.roomPickerOpen = false
	// Stop can fail while the core still owns this display generation. Keep
	// its binding and any pending display reply until idle is confirmed;
	// otherwise Back and shell exit could no longer return the HDMI plane.
	a.sessionDisplayUncertain = a.sessionDisplayUncertain || a.launcherDisplayFocusedLocked() || a.sessionDisplayBusy
	a.suppressHeldPlayKeysLocked()
	a.suppressHeldSessionPadLocked()
	a.cancelPlayHIDLocked()
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
	expected := a.stopExpected
	release := a.stopReleasesLease
	idleRetry := a.stopRetryIdleRelease
	done := make(chan struct{})
	a.stopWait = done
	go func() {
		if idleRetry {
			a.doIdleLeaseRetry(stamp)
		} else {
			a.doStop(ctx, stamp, expected, release)
		}
		a.mu.Lock()
		if a.stopWait == done {
			a.stopWait = nil
		}
		a.mu.Unlock()
		close(done)
	}()
}

func (a *App) doStop(ctx context.Context, stamp ClientStamp, expected hostclient.SessionResult, releaseLease bool) {
	var result hostclient.SessionResult
	var err error
	switch {
	case packageStopIdentity(expected):
		if releaseLease {
			result, err = a.client.StopReleaseExpectedStamped(ctx, expected, stamp)
		} else {
			result, err = a.client.StopExpectedStamped(ctx, expected, stamp)
		}
	case releaseLease && flightStopIdentity(expected):
		result, err = a.client.StopReleaseIdentityStamped(ctx, expected, stamp)
	case releaseLease:
		// No package generation and no launch flight. An empty body would
		// stop whichever play is foreground when the host admits it.
		result = hostclient.SessionResult{ErrorCode: "SESSION_CHANGED", ErrorMessage: "the displayed session identity is unavailable; refresh before stopping"}
	default:
		result, err = a.client.StopStamped(ctx, stamp)
	}
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
		if result.ErrorCode == "" {
			a.stopResponseLost = true
		}
		if releaseLease {
			// The exit asked to drop the grant. Failure can leave it renewing
			// after the machine is already idle, so Release kit can retry.
			a.noteRetainedLeaseLocked(expected)
			a.stopRetryIdleRelease = result.StopStage == "lease_release"
		}
		a.syncGPUParkLocked()
		return
	}
	if result.ErrorCode == "SESSION_CHANGED" {
		// Admission rejected this identity without stopping it. A later explicit
		// Stop must capture refreshed play, rather than retry the stale request.
		a.retryStopLock = false
		a.retryStopHint = ""
		a.retryStopCode = ""
		a.stopExpected = hostclient.SessionResult{}
		a.stopReleasesLease = false
		a.stopRetryIdleRelease = false
		a.stopCaptured = false
		a.stopPhase = "host"
		a.stopMessage = "The running machine changed. Review it before stopping."
		a.status = a.stopMessage
		if a.room != nil {
			a.room.Resume()
			a.dropRoomNavActionsLocked()
		}
		a.kickSessionPollLocked()
		a.syncGPUParkLocked()
		return
	}
	if result.ErrorCode != "" {
		a.stopPhase = "host"
		a.stopMessage = fmt.Sprintf("host stop %d %s: %s", result.HTTPStatus, result.ErrorCode, result.ErrorMessage)
		a.lockRetryStopLocked(result.ErrorCode, result.ErrorMessage)
		if releaseLease {
			a.noteRetainedLeaseLocked(expected)
			a.stopRetryIdleRelease = result.StopStage == "lease_release"
		}
		a.syncGPUParkLocked()
		return
	}
	a.retryStopLock = false
	a.retryStopHint = ""
	a.retryStopCode = ""
	a.stopRetryIdleRelease = false
	a.stopPhase = "ok"
	a.stopMessage = ""
	a.applySessionLocked(result)
	if result.State == "" || result.State == "idle" {
		if releaseLease {
			a.noteExplicitReleaseLocked(expected)
		} else {
			a.noteRetainedLeaseLocked(expected)
		}
		a.stopResponseLost = false
	}
	a.kickSessionPollLocked()
}

// doIdleLeaseRetry posts the shell's idle-grant release. The captured play
// is already idle, so replaying expected_session is rejected before the
// grant can be released.
func (a *App) doIdleLeaseRetry(stamp ClientStamp) {
	client := a.client
	ok := releaseIdleLeaseNow(client, stamp)
	var observed hostclient.SessionResult
	if ok && client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		observed, _ = client.Session(ctx)
		cancel()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopPhase != "stopping" {
		return
	}
	a.bumpSessionGenLocked()
	if !ok {
		a.stopPhase = "error"
		a.stopMessage = "stop failed: lease release failed"
		a.lockRetryStopLocked("", "lease release failed")
		a.stopRetryIdleRelease = true
		a.retainedIdleLease = true
		a.syncGPUParkLocked()
		return
	}
	a.stopRetryIdleRelease = false
	a.retryStopLock = false
	a.retryStopHint = ""
	a.retryStopCode = ""
	a.stopReleasesLease = false
	a.stopExpected = hostclient.SessionResult{}
	a.stopPhase = "ok"
	a.stopMessage = ""
	a.clearRetainedLeasesLocked()
	a.stopResponseLost = false
	if observed.ErrorCode == "" && observed.State != "" {
		a.applySessionLocked(observed)
	} else {
		a.session.State = "idle"
		a.session.GameID = ""
		a.session.System = ""
	}
	a.kickSessionPollLocked()
}

func packageStopIdentity(expected hostclient.SessionResult) bool {
	return expected.CorePackage != nil && expected.GameID != "" &&
		expected.CorePackage.PackageID != "" && expected.CorePackage.Generation != 0
}

func flightStopIdentity(expected hostclient.SessionResult) bool {
	return strings.TrimSpace(expected.ID) != "" && strings.TrimSpace(expected.FlightID) != "" &&
		strings.TrimSpace(expected.GameID) != "" && !packageStopIdentity(expected)
}

func (a *App) discardCapturedStopLocked() {
	a.stopCaptured = false
	a.stopReleasesLease = false
	a.stopExpected = hostclient.SessionResult{}
	a.stopPhase = "host"
	a.stopMessage = "The running machine changed. Review it before stopping."
	a.status = a.stopMessage
}

func (a *App) clearRetainedLeasesLocked() {
	a.retainedIdleLease = false
	a.retainedIdleTargets = nil
	a.retainedIdleUnscoped = false
}

// noteRetainedLeaseLocked records a grant this stop did not release.
// A later bound release of one target must not forget the others.
func (a *App) noteRetainedLeaseLocked(play hostclient.SessionResult) {
	if play.Execution == hostclient.ExecutionHostOnly || strings.TrimSpace(play.Target) == "" {
		a.retainedIdleUnscoped = true
	} else {
		if a.retainedIdleTargets == nil {
			a.retainedIdleTargets = make(map[string]struct{})
		}
		a.retainedIdleTargets[strings.TrimSpace(play.Target)] = struct{}{}
	}
	a.retainedIdleLease = true
}

// noteExplicitReleaseLocked drops the grant a bound explicit stop released.
// Host-only cleanup releases no kit grant. Any other retained target, and
// any grant whose target was not named, stays until the global idle release.
func (a *App) noteExplicitReleaseLocked(play hostclient.SessionResult) {
	if play.Execution == hostclient.ExecutionHostOnly {
		return
	}
	target := strings.TrimSpace(play.Target)
	if target == "" {
		return
	}
	delete(a.retainedIdleTargets, target)
	if len(a.retainedIdleTargets) == 0 && !a.retainedIdleUnscoped {
		a.retainedIdleLease = false
	}
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
	if !a.session.CoreKeyboard && !a.session.CoreKeyboardHID {
		return false
	}
	return a.forwardsPlayHIDLocked()
}

// forwardsKeyboardHIDLocked reports an attached fes.keyboard.hid session.
// Every physical key, including Esc and Backspace, goes to the core; Stop
// stays on the session chrome and the controller Select+Start chord.
func (a *App) forwardsKeyboardHIDLocked() bool {
	return a.session.CoreKeyboardHID && a.forwardsPlayHIDLocked()
}

// HandlePlayHIDScancode consumes one USB key with its HID usage (an SDL
// scancode is the Keyboard/Keypad usage). HID sessions forward the usage
// without chrome keys or a character map; other sessions keep the named-key
// path of HandlePlayHIDKey.
// hidKeyboardF12 is the USB HID Keyboard/Keypad usage for F12.
const hidKeyboardF12 = 0x45

func mediaSelectorKey(name string, usage uint8) bool {
	if usage == hidKeyboardF12 {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(name), "f12")
}

// openRunningMediaSelector opens the running machine's media selector. An
// empty tape and a folder with no .p files still open: Eject and a typed path
// stay available. F12 opens a ROM-linked Coleco session's title selector;
// Guide opens its cartridge picker. A kit-local core keeps F12 as Stop, so
// this returns false there.
func (a *App) openRunningMediaSelector(down bool, now time.Time) bool {
	a.mu.Lock()
	colecoSession := a.colecoSessionLocked()
	available := !a.localCoreBusyLocked() && a.playingHardwareRoomAvailableLocked()
	mediaOffer := available && a.sessionLiveMediaOfferedLocked()
	colecoOffer := available && colecoSession && !a.sessionLiveMediaOfferedLocked() && !a.roomDuringPlay
	a.mu.Unlock()
	if !mediaOffer && !colecoOffer {
		if down && colecoSession {
			a.mu.Lock()
			if !a.roomDuringPlay {
				a.status = colecoSelectorUnavailableStatus
			}
			a.mu.Unlock()
		}
		return false
	}
	if down {
		a.Press(CmdHome, now)
	}
	return true
}

func (a *App) HandlePlayHIDScancode(name string, usage uint8, down bool, now time.Time) bool {
	if a == nil {
		return false
	}
	if a.consumeSuppressedPlayKey(name, usage, down) {
		return true
	}
	if mediaSelectorKey(name, usage) {
		// F12 is the running selector. A kit-local core keeps it as Stop.
		// Every other session consumes the key, including an empty tape and
		// a folder that has no .p files, so it is never forwarded. With no
		// media selector to open, F12 opens the running room as Home does,
		// which the surround hint advertises (#1132).
		if a.localCoreOwnsInput() {
			if down {
				a.RequestLocalStop()
			}
			return true
		}
		if !a.openRunningMediaSelector(down, now) {
			a.handlePlayingRoomKey("home", down, now)
		}
		return true
	}
	if a.handlePlayingRoomKey(name, down, now) {
		return true
	}
	a.mu.Lock()
	hid := a.forwardsKeyboardHIDLocked()
	a.mu.Unlock()
	if !hid {
		return a.handlePlayHIDKey(name, down, now)
	}
	if !a.ConsumePlayHID() {
		return false
	}
	if event, ok := hidkeys.Event(usage, down); ok {
		a.SendPlayHID(event)
	}
	return true
}

func (a *App) ForwardsPlayHID() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.forwardsPlayHIDLocked()
}

func (a *App) forwardsPlayHIDLocked() bool {
	if a.session.State != "active" || a.roomDuringPlay || a.sessionDisplayBusy || a.sessionDisplayUncertain || a.tapePickerOpen || a.firmwarePickerOpen {
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
	if !a.forwardsPlayHIDLocked() || a.tapePickerOpen || a.firmwarePickerOpen {
		return false
	}
	a.repeat.Clear()
	return true
}

// HandlePlayHIDKey consumes one USB key while play HID is attached. Esc and
// Backspace stop the session without NoteInput; "/" opens Load-tape when the
// active package can arm a mailbox. Other keys encode for the attached core.
// Letter s is a ZX81/core key, not chrome stop.
func (a *App) HandlePlayHIDKey(name string, down bool, now time.Time) bool {
	if a.consumeSuppressedPlayKey(name, 0, down) {
		return true
	}
	return a.handlePlayHIDKey(name, down, now)
}

func (a *App) handlePlayHIDKey(name string, down bool, now time.Time) bool {
	if a.handlePlayingRoomKey(name, down, now) {
		return true
	}
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
	if loadTapeChromeKey(name) {
		a.mu.Lock()
		open := down && a.sessionMediaOfferedLocked()
		a.mu.Unlock()
		if open {
			a.Press(CmdSearch, now)
		}
		return true
	}
	if event, ok := playhid.Event(name, down, a.ForwardsCoreKeyboard()); ok {
		a.SendPlayHID(event)
	}
	return true
}

// loadTapeChromeKey is the keyboard/mouse Load-tape binding advertised by
// sessionChromeHint (northWord → "/"). Gamepad North stays CmdSearch.
// ZX81 letters, including s and f, stay core keys.
func loadTapeChromeKey(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "/", "slash":
		return true
	default:
		return false
	}
}

func (a *App) playHIDFailClosedLocked() bool {
	if (a.client == nil || !a.client.paired) && a.healthHave && kitlease.ForeignHID(kitlease.Status{
		State: a.health.Connection.State,
		Owner: a.health.Connection.Owner,
	}) {
		return true
	}
	if a.client != nil && a.client.paired && (!a.kitLeaseHave || a.kitLease.Unavailable) {
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
	defer a.mu.Unlock()
	ready := a.forwardsPlayHIDLocked()
	foreign := a.playHIDFailClosedLocked()
	if !ready || foreign || a.client == nil {
		return false
	}
	if (event.Action == remoteinput.ActionPress || event.Kind == remoteinput.KindAxis && event.Value != 0 || event.Device == remoteinput.DeviceMouse && (event.Value != 0 || event.Code != 0)) && a.playHIDPending >= maxPendingPlayHID {
		return false
	}
	a.rememberPlayHIDLocked(event)
	a.queuePlayHIDLocked([]remoteinput.Event{event})
	return true
}

func (a *App) pollSession(ctx context.Context) {
	a.fetchSession(ctx)
	a.fetchSessionEvents(ctx)
	a.fetchHealth(ctx)
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
		case <-a.sessionKick:
			a.fetchSession(ctx)
			a.fetchSessionEvents(ctx)
			a.fetchHealth(ctx)
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
	if a.launch.Phase == "launching" || a.stopPhase == "stopping" || a.inputBusy || a.sessionDisplayBusy || a.developmentLoadingLocked() {
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
	var paired KitLeaseStatus
	var pairedErr error
	pairedHave := a.client != nil && a.client.paired
	if pairedHave {
		paired, pairedErr = a.client.PairedKitLease(ctx)
		if ctx.Err() != nil {
			return
		}
	}
	result, err := a.client.Health(ctx)
	if ctx.Err() != nil {
		return
	}
	if err == nil {
		lease := kitLeaseFromConnection(result.Connection)
		if pairedHave {
			if pairedErr == nil {
				lease = paired
				result.TargetReachable = paired.TargetReachable
				result.TargetReady = paired.TargetReady
				result.Connection = hostclient.TargetConnection{}
			} else {
				lease = KitLeaseStatus{Unavailable: true, Reason: "paired kit lease status unavailable"}
				result.TargetReachable = false
				result.TargetReady = false
			}
		}
		a.mu.Lock()
		wasForeign := a.foreignKitLeaseLocked()
		a.hostUnreachable = false
		a.healthHave = true
		a.health = result
		a.kitLeaseHave = true
		a.kitLease = lease
		a.clearLeaseRefusalAfterTransitionLocked(wasForeign)
		a.mu.Unlock()
		return
	}
	if pairedHave && pairedErr == nil {
		// Paired target status remains useful when the host's selected target
		// cannot be observed. The authenticated projection belongs to this kit.
		a.mu.Lock()
		wasForeign := a.foreignKitLeaseLocked()
		a.hostUnreachable = false
		a.healthHave = true
		a.health = hostclient.HealthResult{Ready: true, TargetReachable: paired.TargetReachable, TargetReady: paired.TargetReady}
		a.kitLeaseHave = true
		a.kitLease = paired
		a.clearLeaseRefusalAfterTransitionLocked(wasForeign)
		a.mu.Unlock()
		return
	}
	if pairedHave && pairedErr != nil {
		// Keep the scoped status failure visible even when the selected host's
		// health endpoint is also unreachable.
		a.mu.Lock()
		a.kitLeaseHave = true
		a.kitLease = KitLeaseStatus{Unavailable: true, Reason: "paired kit lease status unavailable"}
		a.mu.Unlock()
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

func (a *App) clearLeaseRefusalAfterTransitionLocked(wasForeign bool) {
	if !wasForeign || a.foreignKitLeaseLocked() {
		return
	}
	if a.statusLeaseRefusal && a.status == localInUseCopy {
		a.status = ""
	}
	a.statusLeaseRefusal = false
	if a.launchLeaseRefusal {
		a.launch.Message = ""
		a.launch.Phase = "idle"
		a.launchLeaseRefusal = false
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
	a.startStopModeLocked(a.stopReleasesLease)
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
	if !samePlayHIDSession(a.session, result) {
		a.clearPlayingSetupLocked()
		a.cancelPlayHIDLocked()
		a.invalidateSessionDisplayLocked()
	}
	a.session = result
	if hostclient.SessionKitLocal(a.session.State) {
		a.session.GameID = ""
		a.session.System = ""
		a.session.Input = nil
	}
	a.rememberFlightLocked(result.FlightID)
	// Keep retainedIdleLease. Soft-stop of foreground B can promote A, and
	// a later launch can go active, while B's idle grant is still retained.
	// Shell exit releases that grant; this poll does not.
	if result.State != "active" {
		a.session.GameID = ""
		a.session.System = ""
	}
	if a.session.State == "active" && !a.developmentLoadingLocked() && !sessionDevelopmentActive(a.session) {
		a.clearStaleDevelopmentLoadLocked()
	}
	if a.session.State != "active" && a.stopPhase != "stopping" && !a.retryStopLock {
		a.roomDuringPlay = false
		a.playHIDHeld = nil
		a.stopPhase = "idle"
		a.stopMessage = ""
		if a.launch.Phase == "ok" {
			a.launch.Phase = "idle"
			a.launch.Message = ""
			a.launch.GameID = ""
		}
		a.clearCompletedDevelopmentLoadLocked()
		a.closeTapePickerLocked()
	}
	a.syncGPUParkLocked()
}

func (a *App) syncGPUParkLocked() {
	if a.session.State == "active" || a.session.State == "launching" || a.stopPhase == "stopping" || a.retryStopLock || a.developmentLoadingLocked() {
		a.hold.Clear()
	}
	want := a.session.State == "active" && !a.roomDuringPlay || a.stopPhase == "stopping" || a.retryStopLock
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
	// A held retry has loading false so Select can launch the cached title.
	// Parking still cancels the in-flight generation; continue that retry
	// instead of treating it as a finished load. The new load reads the
	// current filter, so a superseded generation stays cancelled.
	resumeHeld := a.libraryRetrying && !a.loading
	restart := a.loading || a.libraryRetrying
	ctx := a.replaceLoadContextLocked()
	if restart {
		a.libraryRetrying = true
		go a.loadLibraryResume(ctx, a.loadGen, resumeHeld)
	} else {
		a.libraryRetrying = false
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
				title = displayGameTitle(game)
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
		Coleco:            a.colecoSessionLocked(),
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
		LoadTape:          a.sessionLiveMediaOfferedLocked(),
		ControlsNotice:    a.roomSessionNotice,
		ControlsOpen:      a.roomDuringPlay,
		MediaSelector:     a.needsSessionDisplayLocked() && (a.sessionLiveMediaOfferedLocked() || a.mediaControlsDirect),
		KeypadSelect:      a.sessionKeypadLocked(),
		HardwareRoom:      a.playingHardwareRoomAvailableLocked(),
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

func kitLeaseFromConnection(connection hostclient.TargetConnection) KitLeaseStatus {
	status := KitLeaseStatus{State: connection.State, Owner: connection.Owner, Reason: connection.Message}
	switch connection.State {
	case "busy":
		status.State = "held"
	case "recovery-required":
		status.State = "blocked"
	}
	return status
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
		CoreIDs:        append([]string(nil), a.libraryCoreIDs...),
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

func (a *App) applyCatalogPageLocked(games []hostclient.Game, keepID string, keepIndex int, pinned *bool, lastFocus *int, navID *string) {
	if navID == nil {
		empty := ""
		navID = &empty
	}
	// A move on the shelf wins over the id captured at reload. Remember that
	// title before SetCount clamps focus onto a shorter partial page.
	if *pinned && *lastFocus >= 0 && (a.grid.Focus != *lastFocus || a.navDirty) {
		*pinned = false
		if a.grid.Focus >= 0 && a.grid.Focus < len(a.games) {
			*navID = a.games[a.grid.Focus].ID
		}
	} else if !*pinned && *lastFocus >= 0 && a.grid.Focus != *lastFocus && a.grid.Focus >= 0 && a.grid.Focus < len(a.games) {
		*navID = a.games[a.grid.Focus].ID
	}
	a.games = games
	a.grid.SetCount(len(a.games))
	if !*pinned {
		if id := *navID; id != "" {
			for i, game := range a.games {
				if game.ID == id {
					a.grid.Focus = i
					a.grid.ensureVisible()
					*lastFocus = a.grid.Focus
					return
				}
			}
		}
		*lastFocus = a.grid.Focus
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
	a.loadLibraryResume(ctx, gen, false)
}

// loadLibraryResume loads one generation. resumeHeld continues a retry whose
// cached shelf is already on screen: the shelf stays up, and partial pages
// are not published until that attempt completes.
func (a *App) loadLibraryResume(ctx context.Context, gen int, resumeHeld bool) {
	a.mu.Lock()
	if gen != a.loadGen {
		a.mu.Unlock()
		return
	}
	if resumeHeld {
		a.libraryRetrying = true
	}
	a.captureCatalogFocusLocked()
	keepID := a.keepFocusID
	keepIndex := a.keepFocusIndex
	query := a.currentQueryLocked() // fixed for this generation; TypeText does not bump loadGen until debounce
	previous := append([]hostclient.Game(nil), a.games...)
	if !resumeHeld {
		a.games = nil
		a.grid.SetCount(0)
	}
	pinned := true
	lastFocus := -1
	navID := ""
	a.mu.Unlock()
	defer a.endLibraryLoad(gen)

	fails := 0
	held := resumeHeld
	for {
		if ctx.Err() != nil {
			return
		}
		all, notice, err := a.collectLibraryPages(ctx, gen, query, keepID, keepIndex, &pinned, &lastFocus, &navID, !held)
		if err == nil && len(all) == 0 && shelfQueryUnfiltered(query) && strings.TrimSpace(query.Availability) == "" {
			fallback := query
			fallback.Availability = "all"
			fallback.Cursor = ""
			saved, savedNotice, savedErr := a.collectLibraryPages(ctx, gen, fallback, keepID, keepIndex, &pinned, &lastFocus, &navID, !held)
			if savedNotice != "" {
				notice = savedNotice
			}
			all = saved
			err = savedErr
		}
		if err == nil && held {
			a.publishLibraryAttempt(gen, all, keepID, keepIndex, &pinned, &lastFocus, &navID)
		}
		if err == nil || !libraryNetRetry(err) {
			a.finishLibraryLoad(ctx, gen, query, previous, all, keepID, keepIndex, notice, err)
			return
		}
		fails++
		if a.holdLibraryForRetry(gen, query, previous, keepID, keepIndex, &pinned, &lastFocus, &navID) {
			held = true
		}
		if !a.libraryRetryWait(ctx, fails, gen, false) {
			return
		}
	}
}

func (a *App) endLibraryLoad(gen int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if gen != a.loadGen {
		return
	}
	a.libraryRetrying = false
}

// libraryNetRetry is a connect, a short read, or a truncated body. A host
// API status, an oversize body, a JSON decode error, and a superseded or
// cancelled load are not retried.
func libraryNetRetry(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, errLibrarySuperseded) {
		return false
	}
	// ReadResponseBody turns a mid-body disconnect into ErrResponseTruncated
	// without wrapping the original net error, so errors.As would miss it.
	if errors.Is(err, hostclient.ErrResponseTruncated) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

// holdLibraryForRetry puts the last same-filter shelf back while this
// generation keeps retrying. loading is only the launch gate, so Select can
// start a title that is already on screen. libraryRetrying stays set so GPU
// park restarts this generation. Another filter's rows stay off this shelf,
// matching finishLibraryLoad, and a later success replaces them there.
// The bool reports that the cached shelf is on screen; later attempts then
// skip partial pages until one completes.
func (a *App) holdLibraryForRetry(gen int, query hostclient.GameListQuery, previous []hostclient.Game, keepID string, keepIndex int, pinned *bool, lastFocus *int, navID *string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if gen != a.loadGen {
		return false
	}
	a.libraryRetrying = true
	saved, ok := sameFilterSavedShelf(a.shelfQuerySet, a.shelfQuery, a.shelfGames, previous, query)
	if !ok {
		a.loadErr = "Can't reach the library right now."
		a.status = a.loadErr
		return false
	}
	shown := append([]hostclient.Game(nil), saved...)
	a.applyCatalogPageLocked(shown, keepID, keepIndex, pinned, lastFocus, navID)
	a.loading = false
	a.loadErr = ""
	a.status = savedListOfflineCopy
	return true
}

// sameFilterSavedShelf is the last shelf for query. Rows saved for another
// filter are refused. previous is the shelf this generation cleared, used
// when a successful shelf has not been stored yet.
func sameFilterSavedShelf(shelfSet bool, shelfQuery hostclient.GameListQuery, shelfGames, previous []hostclient.Game, query hostclient.GameListQuery) ([]hostclient.Game, bool) {
	if shelfSet && !shelfFilterEqual(shelfQuery, query) {
		return nil, false
	}
	saved := previous
	if shelfSet && len(shelfGames) > 0 {
		saved = shelfGames
	}
	if len(saved) == 0 {
		return nil, false
	}
	return saved, true
}

func (a *App) libraryRetryWait(ctx context.Context, fails, gen int, recents bool) bool {
	timer := time.NewTimer(presentationRetryDelay(fails))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if ctx.Err() != nil {
		return false
	}
	if recents {
		return gen == a.homeRecentsGen
	}
	return gen == a.loadGen
}

func (a *App) collectLibraryPages(ctx context.Context, gen int, query hostclient.GameListQuery, keepID string, keepIndex int, pinned *bool, lastFocus *int, navID *string, publish bool) ([]hostclient.Game, string, error) {
	var (
		all    []hostclient.Game
		notice string
	)
	for {
		if err := ctx.Err(); err != nil {
			return all, notice, err
		}
		a.mu.Lock()
		if gen != a.loadGen {
			a.mu.Unlock()
			return all, notice, errLibrarySuperseded
		}
		a.mu.Unlock()
		page, err := a.client.ListGamePage(ctx, query)
		if err != nil {
			return all, notice, err
		}
		if notice == "" {
			notice = page.Notice
		}
		remain := a.maxGames - len(all)
		if remain <= 0 {
			break
		}
		chunk := page.Games
		if len(chunk) > remain {
			chunk = chunk[:remain]
		}
		all = append(all, chunk...)
		a.mu.Lock()
		if gen != a.loadGen {
			a.mu.Unlock()
			return all, notice, errLibrarySuperseded
		}
		// A held retry keeps the cached shelf up. Publishing this partial
		// page would clamp focus, and the next failure could not put it back
		// once navigation had unpinned the reload id.
		if publish {
			a.applyCatalogPageLocked(append([]hostclient.Game(nil), all...), keepID, keepIndex, pinned, lastFocus, navID)
			a.status = a.libraryStatusLocked()
		}
		a.mu.Unlock()
		if page.NextCursor == "" || len(all) >= a.maxGames || len(chunk) == 0 {
			break
		}
		query.Cursor = page.NextCursor
	}
	return all, notice, nil
}

func (a *App) publishLibraryAttempt(gen int, games []hostclient.Game, keepID string, keepIndex int, pinned *bool, lastFocus *int, navID *string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if gen != a.loadGen {
		return
	}
	a.applyCatalogPageLocked(append([]hostclient.Game(nil), games...), keepID, keepIndex, pinned, lastFocus, navID)
	a.status = a.libraryStatusLocked()
}

func (a *App) finishLibraryLoad(ctx context.Context, gen int, query hostclient.GameListQuery, previous, games []hostclient.Game, keepID string, keepIndex int, notice string, err error) {
	if ctx.Err() != nil || errors.Is(err, errLibrarySuperseded) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if gen != a.loadGen {
		return
	}
	a.loading = false
	a.libraryRetrying = false
	if err != nil {
		// A failed refresh may keep the last successful shelf only when that
		// shelf was produced by this same filter. Restoring SNES rows onto
		// an SMS shelf is the bug.
		if a.shelfQuerySet && !shelfFilterEqual(a.shelfQuery, query) {
			a.loadErr = ""
			if len(a.games) == 0 {
				a.grid.SetCount(0)
				a.status = "Can't reach the library right now."
			} else {
				a.status = a.libraryShelfStatusLocked(query, a.games, notice)
			}
			return
		}
		saved, ok := sameFilterSavedShelf(a.shelfQuerySet, a.shelfQuery, a.shelfGames, previous, query)
		if ok {
			pinned := true
			lastFocus := -1
			navID := ""
			a.applyCatalogPageLocked(saved, keepID, keepIndex, &pinned, &lastFocus, &navID)
			a.loadErr = ""
			a.status = savedListOfflineCopy
			return
		}
		a.loadErr = "Can't reach the library right now."
		a.status = a.loadErr
		return
	}
	a.shelfQuery = query
	a.shelfQuerySet = true
	a.shelfGames = append([]hostclient.Game(nil), games...)
	a.loadErr = ""
	a.status = a.libraryShelfStatusLocked(query, games, notice)
}

func (a *App) libraryShelfStatusLocked(query hostclient.GameListQuery, games []hostclient.Game, notice string) string {
	if !shelfQueryUnfiltered(query) {
		return a.libraryStatusLocked()
	}
	if len(games) == 0 {
		switch notice {
		case savedListOfflineCopy, shelfEmptyCopy, shelfMissingCopy:
			return notice
		}
		if notice != "" {
			return notice
		}
		return shelfEmptyCopy
	}
	if librarySavedOffline(games) {
		return savedListOfflineCopy
	}
	return a.libraryStatusLocked()
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
	queue = a.queueLibraryDetailWorkLocked(now, queue)
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
	if a.room == nil && a.detailOpen && a.libraryDetail.ID == gameID {
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
	if a.room == nil && a.detailOpen && strings.TrimSpace(a.libraryDetail.ID) != "" {
		keep[a.libraryDetail.ID] = struct{}{}
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
