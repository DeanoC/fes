package tenfoot

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io/fs"
	"strings"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/storage"
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
	kitMenuReadinessLogPeriod = 10 * time.Second
	shellLeaseReleaseAttempts = 3
)

const sessionDisplayUnavailableNotice = "HDMI controls unavailable. Back returns to play."

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
	KeyboardHID  bool // The running core receives physical Escape and Backspace.
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

// Snapshot is a frame-loop readable copy of launcher state.
type Snapshot struct {
	Games      []hostclient.Game
	Grid       Grid
	Status     string
	LoadErr    string
	Loading    bool
	Covers     map[string]*image.RGBA
	Launch     LaunchSnapshot
	Gamepads   int
	Keyboards  int
	Mice       int
	Affinity   InputKind
	AffinityID int
	// Prompts draws gamepad hints with the affinity pad's glyphs. Nil
	// keeps button words.
	Prompts     PadPrompts
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
	LibraryBackTo       string
	Collection          string
	ViewLabel           string
	ViewPicker          bool
	ViewPickerIndex     int
	Views               []LibraryView
	PickerRows          []LibraryView
	CollectionMenu      CollectionMenuSnapshot
	Controllers         ControllersSnapshot
	Session             SessionSnapshot
	GPUParked           bool
	Surround            SurroundSnapshot
	Attract             AttractSnapshot
	SafeAreaPct         float64
	Settings            SettingsSnapshot
	Filters             FilterSnapshot
	Genre               string
	Year                string
	Region              string
	HidePrerelease      bool
	HideHacks           bool
	OSK                 shared.OSKSnapshot
	Health              HealthSnapshot
	KitLease            KitLeaseSnapshot
	Detail              DetailSnapshot
	BrowseActions       BrowseActionsSnapshot
	Screenshots         map[string]*image.RGBA
	Preview             PreviewSnapshot
	Theme               theme.Theme
	DebugHUD            DebugHUDSnapshot
	Room                RoomSnapshot
	RoomPicker          RoomPickerSnapshot
	Storage             FirmwarePickerSnapshot
	CoreLibrary         FirmwarePickerSnapshot
	FirmwarePicker      FirmwarePickerSnapshot
	FirmwareCard        FirmwareCardSnapshot
	TapePicker          TapePickerSnapshot
	ReducedMotion       bool
	HostPresentsPaused  bool
	HostLateAdopt       bool
	sessionObservation  uint64
	displayFrameRequest uint64
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
	libraryStatus           string // the library's own line; an open room keeps status
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
	// padPrompts holds each attached pad's hint glyphs by input ID.
	padPrompts map[int]PadPrompts
	// pads is the kit's attached pads by input ID, and padDown the physical
	// buttons each is holding.
	pads    map[int]PadInfo
	padDown map[int]map[remoteinput.Code]bool
	// padProfiles is the saved profiles by GUID, read on first use.
	// padProfilesUnread is a preferences file that could not be read, so a
	// save would lose what it holds.
	padProfiles       map[string]inputmap.Profile
	padProfilesUnread bool
	padProfileGen     uint64
	padStateGen       uint64 // room.pads() changes: attach, leave, press, release, save
	controllers       controllersState
	repeat            Repeater
	theme             theme.Theme
	jobs              chan workItem
	results           chan workResult
	maxGames          int
	pageLimit         int

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
	detailRemoveID         string
	detailRemoveError      string
	detailRemoveBusy       bool
	detailRemoveGeneration uint64
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
	settingsScopeChanged   bool
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
	resolvedSessionTitle   string
	resolvedSessionGameID  string
	sessionTitlePending    bool
	sessionTitleRequest    uint64
	sessionTitleNext       time.Time
	// retainedIdleLease is set when this shell's Soft-stop kept an idle kit
	// grant, and when a kit Select+Start release failed while that grant may
	// still be renewing. A later active or relaunched play does not clear it:
	// another target's grant can still be retained. Shell exit and Release
	// kit are the global idle release. A bound Select+Start drops only the
	// captured target, so those exits leave
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
	allCores            bool // cores outside the v1 release are listed
	homeRecents         []hostclient.Game
	homeRecentsErr      string
	homeRecentsLoaded   bool
	homeRecentsGen      int
	room                *rooms.Instance
	roomStack           []*rooms.Instance
	roomFrame           rooms.Frame
	roomErr             string
	homeRecovery        homeRecovery
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
	// roomHeroDetail is heroDetailKey of the destination whose Details the
	// hero is showing: an installed package or a family.
	roomHeroDetail string
	browseActions  browseActionState
	// libraryReturn is the suspended room the Library was opened from.
	libraryReturn *libraryOrigin
	// firmwarePickerFromDetail reopens Library Details when the picker is
	// cancelled, so the requirement stays in front of the player.
	firmwarePickerFromDetail bool
	menuDisplay              *gfx.MenuDisplay
	sessionDisplayBusy       bool
	sessionDisplayNonHandoff bool
	sessionDisplay           protocol.SessionDisplay
	displayIPC               bool
	displayIPCFailure        <-chan error
	displayShuttingDown      bool
	displayShutdownClose     bool
	displayOutbound          bool
	displayOutboundWait      chan struct{}
	displaySurroundOverride  *bool
	displayFrameRequest      uint64
	displayFrameSeq          uint64
	displayStartupFenced     bool
	startupFrameVerify       func(context.Context) error
	startupFrameCheck        <-chan error
	startupFrameNextCheck    time.Time
	sessionDisplayUncertain  bool
	surround, surroundFailed bool
	sessionDisplayEpoch      uint64
	sessionDisplayBinding    hostclient.SessionResult
	sessionDisplayWait       chan struct{}
	playKeyHeld              map[physicalPlayKey]bool
	playKeySuppressed        map[physicalPlayKey]bool
	playKeyAdmitted          map[physicalPlayKey]playHIDKey
	playKeyPendingRelease    map[physicalPlayKey]playHIDKey
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
	firmwareCard             firmwareCardState
	firmwareCardSeen         map[string]bool
	setup                    setupState // first-run setup (setup.go)
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
	// diskChangeBusy is a core screen Next disk in flight.
	diskChangeBusy     bool
	tapePickerIndex    int
	tapePickerGen      int
	tapePickerPath     string
	tapePickerStatus   string
	tapePickerRows     []TapePickerRow
	tapePickerSession  hostclient.SessionResult
	tapePickerStarters []hostclient.HardwareTape
	// tapePickerSet is the running game's disk set as picker rows.
	tapePickerSet []TapePickerRow

	// A bounded, root-requested test observation may defer the surround for
	// this exact play session. It never changes the saved user preference.
	diagnosticSurroundSession hostclient.SessionResult
	diagnosticSurroundUntil   time.Time

	safeAreaPct        float64
	prefsPath          string
	preferenceStore    PreferenceStore
	prefsWriteWarned   bool
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

	localCores               rooms.LocalCores
	kitHost                  bool
	kitHostPreparing         bool
	hostLaunchPending        func()
	hostPresentsPaused       bool
	hostAwaitStatus          bool
	sessionObservation       uint64
	sessionObservedAt        time.Time
	sessionReadyWrittenAt    time.Time
	kitMenuStartedAt         time.Time
	kitMenuSessionSeen       bool
	kitMenuSessionState      string
	kitMenuSessionHTTPStatus int
	kitMenuSessionError      string
	kitMenuHealthSeen        bool
	kitMenuHealthError       string
	kitMenuReadyLogf         func(string, ...any)
	kitMenuReadyLogKey       string
	kitMenuReadyLogAt        time.Time
	localFeed                localPadSender
	localInstallKnown        bool
	localInstalled           map[string]struct{}
	localInstallGen          uint64
	localInstallCancel       context.CancelFunc

	// settingsSection is the open Settings section; "" is the section list.
	settingsSection string
	// settingsConfirm is the row waiting for a second Select (factory-reset).
	settingsConfirm string
	// settingsWired is the kit's wired link, read as Settings or Network opens.
	settingsWired string
	// settingsFirmware is the host's bundled firmware for Settings › Firmware.
	settingsFirmware settingsFirmware
	// kitDisplay is the kit's own menu display; kitID is its target ID.
	kitDisplay bool
	kitID      string
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

// padProfile reads the profile tenfoot.json saves for a pad's GUID: the
// zero Profile when there is none. The input layer reads it as the pad
// attaches, and again when Settings › Controllers changes a profile.
func (a *App) padProfile(guid string) (inputmap.Profile, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.preferenceStore == nil && a.prefsPath == "" {
		return inputmap.Profile{}, nil
	}
	prefs, err := a.readPreferencesLocked()
	if errors.Is(err, fs.ErrNotExist) {
		return inputmap.Profile{}, nil
	}
	if err != nil {
		return inputmap.Profile{}, fmt.Errorf("%s: %w", a.preferenceSourceLocked(), err)
	}
	return prefs.Controllers[guid], nil
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
		setup:              setupState{step: -1},
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
	if a.kitHost && a.kitMenuStartedAt.IsZero() {
		a.kitMenuStartedAt = time.Now()
	}
	a.loadGen++
	gen := a.loadGen
	loadCtx := a.replaceLoadContextLocked()
	a.loadPinnedRoomsLocked()
	if !a.roomDuringPlay {
		a.showHomeLocked()
	}
	a.mu.Unlock()
	for i := 0; i < coverWorkers; i++ {
		go a.worker(ctx)
	}
	go a.loadPlatforms(ctx)
	go a.loadCollections(ctx)
	go a.loadEditionPreferences(ctx)
	go a.pollSession(ctx)
	go a.pollKitMenuReadiness(ctx)
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
	a.displayShuttingDown = true
	done := a.stopWait
	displayDone := a.sessionDisplayWait
	outboundDone := a.displayOutboundWait
	a.mu.Unlock()
	if done != nil {
		<-done
	}
	if displayDone != nil {
		<-displayDone
	}
	if outboundDone != nil {
		<-outboundDone
	}
	a.mu.Lock()
	a.displayShutdownClose = true
	a.mu.Unlock()
	a.returnSessionDisplayOnExit()
	a.mu.Lock()
	a.displayShutdownClose = false
	displayDone = a.sessionDisplayWait
	a.mu.Unlock()
	if displayDone != nil {
		<-displayDone
	}
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
	if a.kitHost && (a.hostAwaitStatus || a.kitHostPreparing) {
		return
	}
	a.traceInputLocked(cmd, now)
	a.noteActivityLocked(now)
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
	roomOwnsInput := a.room != nil && !a.roomPickerOpen && !a.settingsOpen && !a.settingsOSKOpenLocked() && !a.firmwarePickerOpen && !a.tapePickerOpen && !a.firmwareCard.Open
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
		if a.firmwarePickerOpen || a.tapePickerOpen || a.firmwareCard.Open {
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
		a.closeFirmwareCardLocked()
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
		a.closeFirmwareCardLocked()
		if a.settingsOpen {
			a.closeSettingsLocked()
		} else {
			a.openSettingsLocked()
		}
		return
	case CmdFilters:
		if a.firmwarePickerOpen || a.tapePickerOpen || a.firmwareCard.Open {
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
	// Settings › Firmware opens a family's card over Settings.
	if a.firmwareCard.Open {
		a.handleFirmwareCardLocked(cmd)
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
	a.flushPendingPlayKeyReleasesLocked()
	a.frameLua = rooms.StepTiming{}
	a.flushSearchLocked(now)
	a.tickAttractLocked(now)
	a.syncPreviewLocked()
	a.tickSessionPadLocked(now)
	a.tickLearnLocked(now)
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
	if kind == InputGamepad {
		a.dropPadInfoLocked(id)
	}
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
