package tenfoot

import (
	"image"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/rooms"
	"github.com/DeanoC/FogCast/ui/shared"
	"github.com/DeanoC/FogCast/ui/theme"
)

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
	KeypadSelect        bool
	KeyboardHID         bool // The running core receives physical Escape and Backspace.
	KeyboardUnavailable bool // This kit window cannot safely delegate a physical keyboard.
	HardwareRoom        bool
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
