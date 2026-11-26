package tenfoot

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io/fs"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/storage"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/inputmap"
	"github.com/DeanoC/FogCast/ui/localfeed"
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
	loadFinishedGen        int
	loadFinishedOK         bool
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
	stopWait              chan struct{}
	sessionGen            int
	sessionFetchSequence  uint64
	sessionFetchApplied   uint64
	sessionFetchExhausted bool
	healthFetchSequence   uint64
	healthFetchApplied    uint64
	healthFetchExhausted  bool
	healthObservedAt      time.Time
	stopPhase             string
	stopExpected          hostclient.SessionResult
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
	stopCaptured           bool
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
	localKeyboardUncertain bool
	developmentRBFPath     string
	devLoadPhase           string
	devLoadMessage         string

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

	localCores         rooms.LocalCores
	kitHost            bool
	managedKitSoftware bool
	kitV2InputDisabled bool
	// SDL cannot obtain authoritative held-key state per physical keyboard.
	// This gates only the kit-local delegated HID keyboard route.
	kitLocalKeyboardUnavailable bool
	// The v2 latch is set only by the eventual acknowledged authority path.
	// It stays set through quarantine; zero-value App does not enable v2.
	delegatedV2Active         bool
	nativeKeyboardQuarantined bool
	nativeKeyboardClosing     bool
	nativeGuest               nativeGuestLifecycle
	nativeGuestBinding        nativeGuestBinding
	nativeGuestQueueEpoch     uint64
	// True from native pad-only promotion until a correlated recovery proves
	// that exact connection's local non-HID source has drained.
	nativePadRetiring                 bool
	nativePadRetirementEpoch          uint64
	nativePadRetirementBinding        padOnlyAuthority
	nativeMappedPad                   nativeMappedPadLedger
	nativePadConnectionFence          *localfeed.PadWriteFence
	nativePadMappingFence             *localfeed.PadWriteFence
	nativePadDeviceFences             map[localhid.DeviceID]*localfeed.PadWriteFence
	nativePadDeviceBlocked            map[localhid.DeviceID]bool
	nativePadMappingFenced            bool
	nativeGuestSnapshotAllowed        bool
	nativeGuestBeginRequest           *localhid.Request
	nativeKeyboardGeneration          uint64
	nativeKeyboardGenerationExhausted bool
	nativeKeyboardWake                chan struct{}
	nativeUIOnlyInFlight              uint32
	nativeUIRaw                       nativeKeyboardUIRawLedger
	nativeUIActionNext                uint64
	nativeUIActions                   map[uint64]nativeKeyboardUIAction
	kitHostPreparing                  bool
	hostLaunchPending                 func()
	hostPresentsPaused                bool
	hostAwaitStatus                   bool
	sessionObservation                uint64
	sessionObservedAt                 time.Time
	sessionRuntimeInputRetained       bool      // session.RuntimeInput kept from an earlier read of the same play
	sessionRuntimeInputReportedAt     time.Time // request start of the last session read that reported it
	sessionReadyWrittenAt             time.Time
	kitMenuStartedAt                  time.Time
	kitMenuSessionSeen                bool
	kitMenuSessionState               string
	kitMenuSessionHTTPStatus          int
	kitMenuSessionError               string
	kitMenuHealthSeen                 bool
	kitMenuHealthError                string
	kitMenuReadyLogf                  func(string, ...any)
	kitMenuReadyLogKey                string
	kitMenuReadyLogAt                 time.Time
	localFeed                         localPadSender
	localInstallKnown                 bool
	localInstalled                    map[string]struct{}
	localInstallGen                   uint64
	localInstallCancel                context.CancelFunc

	// settingsSection is the open Settings section; "" is the section list.
	settingsSection string
	// settingsConfirm is the row waiting for a second Select (factory-reset).
	settingsConfirm string
	// settingsWired is the kit's wired link, read as Settings or Network opens.
	settingsWired string
	// settingsFirmware is the host's bundled firmware for Settings › Firmware.
	settingsFirmware settingsFirmware
	// bluetooth is the optional local Bluetooth stack for Settings › Bluetooth.
	bluetooth BluetoothController
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
		nativeKeyboardWake: make(chan struct{}, 1),
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

// Affinity reports the device that currently owns sofa hints and focus.
func (a *App) Affinity() Affinity {
	if a == nil {
		return Affinity{}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.affinity.current
}
