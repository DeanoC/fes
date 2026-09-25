package hostclient

import (
	"fmt"
	"strings"
)

// Game is one catalog row from GET /api/v1/games.
type Game struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	System           string   `json:"system"`
	Cover            string   `json:"cover,omitempty"`
	Genre            string   `json:"genre,omitempty"`
	Year             string   `json:"year,omitempty"`
	Region           string   `json:"region,omitempty"`
	State            string   `json:"state"`
	RootOnline       bool     `json:"root_online"`
	Launchable       bool     `json:"launchable"`
	ROMCached        *bool    `json:"rom_cached,omitempty"`
	Favorite         bool     `json:"favorite,omitempty"`
	PlayCount        int64    `json:"play_count,omitempty"`
	LastPlayedAt     int64    `json:"last_played_at,omitempty"`
	Collections      []string `json:"collections,omitempty"`
	Series           string   `json:"series,omitempty"`
	Variants         []Game   `json:"variants,omitempty"`
	FirmwareRequired bool     `json:"firmware_required,omitempty"`
	// Execution is the service policy for this row (fpga_native, fpga_development, host_only).
	Execution      string `json:"execution,omitempty"`
	ExpansionID    string `json:"expansion_id,omitempty"`
	ROMRequired    bool   `json:"rom_required,omitempty"`
	ROMReady       bool   `json:"rom_ready,omitempty"`
	ROMID          string `json:"rom_id,omitempty"`
	ROMMediaID     string `json:"rom_media_id,omitempty"`
	ExpansionReady bool   `json:"expansion_ready,omitempty"`
	FirmwareReady  bool   `json:"firmware_ready,omitempty"`
	// ReadyHere is Phase 2 Ready for this shell. Nil means the mesh
	// ensure seam is off and LaunchBlock stays composition. A false
	// pointer is Unavailable (or Checking while a slot is mid-pull).
	ReadyHere  *bool  `json:"ready_here,omitempty"`
	ReadyBlock string `json:"ready_block,omitempty"`
	NextAction string `json:"next_action,omitempty"`
	// Placement is the host predicate rooms read when this row asked
	// Place. Empty means the row is not asking. selected keeps Play on
	// the existing launch and does not ask which machine. unresolved
	// and fail_closed are not Ready. Codes match meshplace.Outcome.
	// They are not sofa copy.
	Placement string `json:"placement,omitempty"`
}

// Placement outcomes rooms read from the games row. They match
// meshplace.Outcome. Empty means this row is not asking.
const (
	PlacementSelected   = "selected"
	PlacementUnresolved = "unresolved"
	PlacementFailClosed = "fail_closed"
)

// LaunchBlock is why a catalog row is ineligible for POST /api/v1/session/launch.
// Empty means eligible. Values are stable codes, not UI copy.
type LaunchBlock string

const (
	LaunchBrowseOnly       LaunchBlock = "browse_only"
	LaunchSourceOffline    LaunchBlock = "source_offline"
	LaunchUnreadable       LaunchBlock = "unreadable"
	LaunchNotReady         LaunchBlock = "not_ready"
	LaunchMissingFirmware  LaunchBlock = "missing_firmware"
	LaunchMissingExpansion LaunchBlock = "missing_expansion"
	LaunchMissingROM       LaunchBlock = "missing_rom"
	// Mesh blocks match meshcontent.Block. They are set only when
	// ReadyHere is non-nil and false.
	LaunchDistant        LaunchBlock = "distant"
	LaunchLeaseHeld      LaunchBlock = "lease_held"
	LaunchVersionSkew    LaunchBlock = "version_skew"
	LaunchNoExecutor     LaunchBlock = "no_capable_executor"
	LaunchContentMissing LaunchBlock = "content_missing"
	LaunchEnsureProgress LaunchBlock = "ensure_in_progress"
	LaunchMeshInvalid    LaunchBlock = "invalid"
	// Placement blocks are set when Place cleared Ready. They are not
	// version skew, a held lease, or an edition choice.
	LaunchPlacementUnresolved LaunchBlock = "placement_unresolved"
	LaunchPlacementFailClosed LaunchBlock = "placement_fail_closed"
)

// LaunchBlock classifies catalog-side launch ineligibility. ListGames variant
// selection and sofa admission share this rule. A true ReadyHere still
// applies the catalog gates: a host-only row whose root is offline is
// not Play. A false ReadyHere keeps the mesh block.
func (g Game) LaunchBlock() LaunchBlock {
	if g.ReadyHere != nil && !*g.ReadyHere {
		if g.ReadyBlock == "" {
			return LaunchNotReady
		}
		return LaunchBlock(g.ReadyBlock)
	}
	if block := g.placementBlock(); block != "" {
		return block
	}
	return g.catalogLaunchBlock()
}

// placementBlock is the Ready gate for an unresolved or fail-closed
// placement. Selected and an empty predicate do not block. A false
// ReadyHere keeps its own block, so version skew and in use stay
// distinct when those already explain the row.
func (g Game) placementBlock() LaunchBlock {
	switch strings.TrimSpace(g.Placement) {
	case PlacementUnresolved:
		return LaunchPlacementUnresolved
	case PlacementFailClosed:
		return LaunchPlacementFailClosed
	default:
		return ""
	}
}

func (g Game) catalogLaunchBlock() LaunchBlock {
	if !g.Launchable {
		return LaunchBrowseOnly
	}
	if g.State == "missing" || !g.RootOnline {
		return LaunchSourceOffline
	}
	if g.State == "invalid" {
		return LaunchUnreadable
	}
	if g.State != "available" {
		return LaunchNotReady
	}
	if g.ROMRequired && !g.ROMReady {
		return LaunchMissingROM
	}
	if g.ExpansionID != "" && !g.ExpansionReady {
		return LaunchMissingExpansion
	}
	if g.FirmwareRequired && !g.FirmwareReady {
		return LaunchMissingFirmware
	}
	return ""
}

// LaunchEligible reports whether game may be POSTed to /api/v1/session/launch
// from catalog state.
func (g Game) LaunchEligible() bool {
	return g.LaunchBlock() == ""
}

// ExecutionHostOnly is a title that plays on the host executor and does not
// claim a kit lease.
const ExecutionHostOnly = "host_only"

// HostOnly reports a catalog row whose service execution is the host executor.
func (g Game) HostOnly() bool {
	return strings.TrimSpace(g.Execution) == ExecutionHostOnly
}

// CoreEntry is the stable library mapping for one FPGA-core game.
type CoreEntry struct {
	GameID    string `json:"game_id"`
	Title     string `json:"title"`
	CoreID    string `json:"core_id"`
	PackageID string `json:"package_id"`
}

// CorePackage is the public identity projection of one installed FPGA package.
type CorePackage struct {
	PackageID     string
	CoreID        string
	Name          string
	Version       string
	Compatibility string
}

// CoreLibrary is the read-only core-entry and installed-package inventory.
type CoreLibrary struct {
	Entries  []CoreEntry
	Packages []CorePackage
}

// CoreAvailability is the deterministic join between a selected entry and
// the installed package inventory.
type CoreAvailability struct {
	GameID         string
	Title          string
	CoreID         string
	PackageID      string
	PackageCoreID  string
	PackageName    string
	PackageVersion string
	Compatibility  string
	State          string
}

type corePackageWire struct {
	PackageID  string `json:"package_id"`
	Descriptor struct {
		Core struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"core"`
	} `json:"descriptor"`
	Compatibility string `json:"compatibility"`
}

// Availability joins each selected core entry with its exact installed
// package. An installed package with compatibility "unknown" remains
// installed/unknown, never ready.
func (l CoreLibrary) Availability() []CoreAvailability {
	packages := make(map[string]CorePackage, len(l.Packages))
	for _, packageInfo := range l.Packages {
		packageInfo.PackageID = strings.TrimSpace(packageInfo.PackageID)
		packages[packageInfo.PackageID] = packageInfo
	}
	out := make([]CoreAvailability, 0, len(l.Entries))
	for _, entry := range l.Entries {
		status := CoreAvailability{
			GameID:    strings.TrimSpace(entry.GameID),
			Title:     strings.TrimSpace(entry.Title),
			CoreID:    strings.TrimSpace(entry.CoreID),
			PackageID: strings.TrimSpace(entry.PackageID),
		}
		packageInfo, ok := packages[status.PackageID]
		if !ok || status.PackageID == "" {
			status.State = "missing"
			out = append(out, status)
			continue
		}
		status.PackageCoreID = strings.TrimSpace(packageInfo.CoreID)
		status.PackageName = strings.TrimSpace(packageInfo.Name)
		status.PackageVersion = strings.TrimSpace(packageInfo.Version)
		status.Compatibility = strings.TrimSpace(packageInfo.Compatibility)
		switch {
		case status.PackageCoreID != status.CoreID:
			status.State = "mismatch"
		case strings.EqualFold(status.Compatibility, "incompatible"):
			status.State = "incompatible"
		default:
			status.State = "installed"
		}
		out = append(out, status)
	}
	return out
}

// Label is compact enough for tile/detail metadata while retaining the core
// identity and selected package prefix.
func (s CoreAvailability) Label() string {
	parts := []string{strings.TrimSpace(s.CoreID), strings.TrimSpace(s.State)}
	if parts[0] == "" {
		parts[0] = "core"
	}
	if parts[1] == "" {
		parts[1] = "unknown"
	}
	if version := strings.TrimSpace(s.PackageVersion); version != "" {
		parts = append(parts, version)
	}
	if packageID := shortPackageID(s.PackageID); packageID != "" {
		parts = append(parts, "pkg "+packageID)
	}
	if compatibility := strings.TrimSpace(s.Compatibility); compatibility != "" {
		parts = append(parts, "compat "+compatibility)
	}
	if packageCoreID := strings.TrimSpace(s.PackageCoreID); packageCoreID != "" && packageCoreID != strings.TrimSpace(s.CoreID) {
		parts = append(parts, "declares "+packageCoreID)
	}
	return strings.Join(parts, " | ")
}

func shortPackageID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// Presentation is GET /api/v1/presentation/games/{id}.
type Presentation struct {
	GameID       string                   `json:"game_id"`
	State        string                   `json:"state"`
	Presentation *PresentationInfo        `json:"presentation"`
	Attribution  *PresentationAttribution `json:"attribution,omitempty"`
}

// PresentationInfo is the nested presentation object on a games/{id} payload.
type PresentationInfo struct {
	CoverArtworkID    string   `json:"cover_artwork_id"`
	BackdropArtworkID string   `json:"backdrop_artwork_id,omitempty"`
	LogoID            string   `json:"logo_id,omitempty"`
	MarqueeID         string   `json:"marquee_id,omitempty"`
	Box3DID           string   `json:"box3d_id,omitempty"`
	VideoID           string   `json:"video_id,omitempty"`
	Summary           string   `json:"summary"`
	Year              string   `json:"year"`
	Genre             string   `json:"genre"`
	Studio            string   `json:"studio"`
	Players           string   `json:"players"`
	Rating            string   `json:"rating,omitempty"`
	Completion        string   `json:"completion,omitempty"`
	Portable          bool     `json:"portable,omitempty"`
	ScreenshotIDs     []string `json:"screenshot_ids,omitempty"`
	Series            string   `json:"series,omitempty"`
	Related           []string `json:"related,omitempty"`
	RelatedIDs        []string `json:"related_ids,omitempty"`
	Collection        string   `json:"collection,omitempty"`
}

// PresentationAttribution is the provider label the public API returns with ready metadata.
type PresentationAttribution struct {
	Provider string `json:"provider"`
	Label    string `json:"label"`
}

// AttributionLabel returns the validated IGDB or LaunchBox label, or empty.
func (p Presentation) AttributionLabel() string {
	if p.Attribution == nil {
		return ""
	}
	provider := strings.TrimSpace(p.Attribution.Provider)
	label := strings.TrimSpace(p.Attribution.Label)
	switch {
	case provider == "igdb" && label == "Data from IGDB.com":
		return label
	case provider == "launchbox" && label == "Data from LaunchBox Games Database":
		return label
	default:
		return ""
	}
}

// Platform is one row from GET /api/v1/platforms.
type Platform struct {
	ID         string   `json:"id"`
	Label      string   `json:"label"`
	GameCount  int      `json:"game_count"`
	Online     bool     `json:"online"`
	Launchable bool     `json:"launchable"`
	Tags       []string `json:"tags,omitempty"`
}

// Collection is one custom shelf from GET /api/v1/library/collections.
type Collection struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"created_at,omitempty"`
}

// EditionPreference is one household edition choice from
// GET /api/v1/library/edition-preferences.
type EditionPreference struct {
	Query    string `json:"query"`
	Platform string `json:"platform,omitempty"`
	GameID   string `json:"game_id"`
	ChosenAt int64  `json:"chosen_at,omitempty"`
}

const (
	defaultAttractLimit       = 24
	defaultAttractIdleSeconds = 60
)

// AttractItem is one row from GET /api/v1/library/attract.
type AttractItem struct {
	GameID     string `json:"game_id"`
	Title      string `json:"title"`
	Platform   string `json:"platform"`
	Video      string `json:"video,omitempty"`
	Cover      string `json:"cover,omitempty"`
	Backdrop   string `json:"backdrop,omitempty"`
	Marquee    string `json:"marquee,omitempty"`
	Launchable bool   `json:"launchable"`
}

// AttractPlaylist is the attract response, including host idle_seconds.
type AttractPlaylist struct {
	Items       []AttractItem `json:"items"`
	IdleSeconds int           `json:"idle_seconds"`
}

// LibraryCache is GET /api/v1/library/cache. Cover used/free are kit-local.
type LibraryCache struct {
	ROM        ROMCacheStatus `json:"rom"`
	SyncedUnix int64          `json:"synced_unix,omitempty"`
}

// ROMCacheStatus is the target ROM cache budget from a lease-free probe.
type ROMCacheStatus struct {
	UsedBytes int64 `json:"used_bytes"`
	MaxBytes  int64 `json:"max_bytes"`
	FreeBytes int64 `json:"free_bytes"`
	Reachable bool  `json:"reachable"`
}

// BackdropHandle is the 64-hex fanart/backdrop handle, or empty.
func (item AttractItem) BackdropHandle() string { return NormalizeHandle(item.Backdrop) }

// VideoHandle is the 64-hex library_media / attract video handle, or empty.
func (item AttractItem) VideoHandle() string { return NormalizeHandle(item.Video) }

// StillHandle prefers backdrop, then cover, then marquee. Video is ignored.
func (item AttractItem) StillHandle() string {
	handles := item.StillHandles()
	if len(handles) == 0 {
		return ""
	}
	return handles[0]
}

// StillHandles is backdrop, then cover, then marquee, de-duplicated. Video is ignored.
func (item AttractItem) StillHandles() []string {
	var out []string
	seen := map[string]bool{}
	for _, handle := range []string{item.Backdrop, item.Cover, item.Marquee} {
		got := NormalizeHandle(handle)
		if got == "" || seen[got] {
			continue
		}
		seen[got] = true
		out = append(out, got)
	}
	return out
}

// GameListQuery is GET /api/v1/games with the web UI's catalog params.
type GameListQuery struct {
	Cursor         string
	Limit          int
	Platform       string
	Sort           string
	Q              string
	Collection     string
	Genre          string
	Year           string
	Region         string
	HidePrerelease bool
	HideHacks      bool
}

// FacetValues is GET /api/v1/library/facets. The host does not return regions.
type FacetValues struct {
	Genres []string `json:"genres"`
	Years  []string `json:"years"`
}

// LibraryTarget is one target row from GET /api/v1/library/settings.
type LibraryTarget struct {
	Name            string `json:"name"`
	Address         string `json:"address"`
	Enabled         bool   `json:"enabled"`
	AgentConfigured bool   `json:"agent_configured"`
	TargetID        string `json:"target_id,omitempty"`
}

// LibraryTargetWrite is one target in a PATCH /api/v1/library/settings body.
type LibraryTargetWrite struct {
	Name         string  `json:"name"`
	OriginalName string  `json:"original_name,omitempty"`
	Address      string  `json:"address"`
	Enabled      bool    `json:"enabled"`
	Agent        *string `json:"agent,omitempty"`
}

// LibraryRoot is one library path row from GET /api/v1/library/settings.
type LibraryRoot struct {
	ID     string `json:"id"`
	System string `json:"system"`
	Root   string `json:"root"`
}

// LibrarySystem is one platform label from GET /api/v1/library/settings.
type LibrarySystem struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// LibrarySettings is GET /api/v1/library/settings.
type LibrarySettings struct {
	AttractIdleSeconds int             `json:"attract_idle_seconds"`
	PreferredRegions   []string        `json:"preferred_regions"`
	SelectedTarget     string          `json:"selected_target"`
	Targets            []LibraryTarget `json:"targets"`
	Libraries          []LibraryRoot   `json:"libraries"`
	Systems            []LibrarySystem `json:"systems"`
}

// LibrarySettingsPatch is a partial PATCH /api/v1/library/settings body.
type LibrarySettingsPatch struct {
	AttractIdleSeconds *int                  `json:"attract_idle_seconds,omitempty"`
	PreferredRegions   *[]string             `json:"preferred_regions,omitempty"`
	SelectedTarget     *string               `json:"selected_target,omitempty"`
	Targets            *[]LibraryTargetWrite `json:"targets,omitempty"`
	Libraries          *[]LibraryRoot        `json:"libraries,omitempty"`
	PrepareTarget      *string               `json:"prepare_target,omitempty"`
}

func (p LibrarySettingsPatch) payload() (map[string]any, error) {
	raw := map[string]any{}
	if p.AttractIdleSeconds != nil {
		raw["attract_idle_seconds"] = *p.AttractIdleSeconds
	}
	if p.PreferredRegions != nil {
		regions := append([]string(nil), *p.PreferredRegions...)
		if regions == nil {
			regions = []string{}
		}
		raw["preferred_regions"] = regions
	}
	if p.SelectedTarget != nil {
		raw["selected_target"] = *p.SelectedTarget
	}
	if p.Targets != nil {
		targets := append([]LibraryTargetWrite(nil), *p.Targets...)
		if targets == nil {
			targets = []LibraryTargetWrite{}
		}
		raw["targets"] = targets
	}
	if p.Libraries != nil {
		libraries := append([]LibraryRoot(nil), *p.Libraries...)
		if libraries == nil {
			libraries = []LibraryRoot{}
		}
		raw["libraries"] = libraries
	}
	if p.PrepareTarget != nil {
		raw["prepare_target"] = *p.PrepareTarget
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("settings patch is empty")
	}
	return raw, nil
}

// HealthResult is GET /api/v1/health. HTTP 200 while the host process is up.
type HealthResult struct {
	Ready           bool
	TargetReachable bool
	TargetReady     bool
	Connection      TargetConnection
}

// TargetConnection is discovery and reconciliation state, independent of game state.
type TargetConnection struct {
	State    string `json:"state"`
	Message  string `json:"message,omitempty"`
	Address  string `json:"address,omitempty"`
	TargetID string `json:"target_id,omitempty"`
	BootID   string `json:"boot_id,omitempty"`
	Owner    string `json:"owner,omitempty"`
}

// TargetStatus is GET /api/v1/status. HTTP 503 TARGET_UNAVAILABLE means the kit
// did not answer; that is not a host-process failure.
type TargetStatus struct {
	HTTPStatus   int
	State        string
	GameID       string
	System       string
	Core         string
	ErrorCode    string
	ErrorMessage string
	Unavailable  bool
	Connection   TargetConnection
}

// SessionEvent is one row from GET /api/v1/session/events.
type SessionEvent struct {
	Sequence     uint64           `json:"sequence"`
	FlightID     string           `json:"flight_id,omitempty"`
	TSUTC        string           `json:"ts_utc,omitempty"`
	MonoMS       int64            `json:"mono_ms,omitempty"`
	ClientTSUTC  string           `json:"client_ts_utc,omitempty"`
	ClientMonoMS *int64           `json:"client_mono_ms,omitempty"`
	Event        string           `json:"event"`
	State        string           `json:"state"`
	GameID       string           `json:"game_id,omitempty"`
	System       string           `json:"system,omitempty"`
	Media        string           `json:"media,omitempty"`
	Progress     *SessionProgress `json:"progress,omitempty"`
	Input        *SessionInput    `json:"input,omitempty"`
}
