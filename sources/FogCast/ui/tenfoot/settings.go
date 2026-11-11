package tenfoot

import (
	"context"
	"fmt"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/shared"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Settings sections, in the v1 order (docs/ux/references/v1/README.md,
// Settings map). Settings opens on the section list; a section is listed
// only when it has rows, so Controllers waits for #980's page. Library &
// rooms holds the rows outside v1 and is listed with Show all cores.
const (
	settingsSectionList        = ""
	settingsSectionFolders     = "folders"
	settingsSectionFirmware    = "firmware"
	settingsSectionDisplay     = "display"
	settingsSectionNetwork     = "network"
	settingsSectionUpdates     = "updates"
	settingsSectionAbout       = "about"
	settingsSectionDiagnostics = "diagnostics"
	settingsSectionBrowse      = "browse"
)

var settingsSectionTitles = map[string]string{
	settingsSectionFolders:     "Games folders",
	settingsSectionFirmware:    "Firmware",
	settingsSectionDisplay:     "Display & sound",
	settingsSectionNetwork:     "Network",
	settingsSectionUpdates:     "Updates",
	settingsSectionAbout:       "About & help",
	settingsSectionDiagnostics: "Diagnostics",
	settingsSectionBrowse:      "Library & rooms",
}

// settingsSectionParent is where Back goes from a section.
func settingsSectionParent(id string) string {
	if id == settingsSectionDiagnostics {
		return settingsSectionAbout
	}
	return settingsSectionList
}

// settingsSectionRowPrefix starts the ID of a row that opens a section.
const settingsSectionRowPrefix = "section-"

const (
	settingsTargetFieldName = iota
	settingsTargetFieldAddress
	settingsTargetFieldEnabled
	settingsTargetFieldAgent
	settingsTargetFieldCount
)

var settingsTargetFieldIDs = [settingsTargetFieldCount]string{"name", "address", "enabled", "agent"}

type settingsOSKKind int

const (
	settingsOSKNone settingsOSKKind = iota
	settingsOSKLibraryPath
	settingsOSKTargetName
	settingsOSKTargetAddress
	settingsOSKTargetAgent
	settingsOSKDevelopmentPath
	settingsOSKFirmwarePath
	settingsOSKTapePath
	settingsOSKCorePath
	settingsOSKCoreTitle
	settingsOSKDriveField
)

type settingsTargetDraft struct {
	OriginalName    string
	Name            string
	Address         string
	Enabled         bool
	AgentConfigured bool
	AgentDirty      bool
	AgentClear      bool
	AgentDraft      string
}

const (
	minSettingsIdleSeconds = 1       // host library settings allow 1s (ui_shell min, config defaults only <=0)
	maxSettingsIdleSeconds = 2147483 // host MaxAttractIdleSeconds
	settingsIdleStep       = 15
)

var settingsKnownRegions = []string{"usa", "world", "europe", "japan"}

// SettingsRow is one sofa settings overlay line.
type SettingsRow struct {
	ID    string
	Label string
	Value string
}

// SettingsSnapshot is the settings overlay shown by the renderer.
type SettingsSnapshot struct {
	Open bool
	// Title is "Settings" on the section list, else the section's name.
	Title        string
	Index        int
	Rows         []SettingsRow
	Loading      bool
	Busy         bool
	Status       string
	Hint         string
	LibraryCount int
	TargetCount  int
	SystemCount  int
}

func (a *App) SettingsOpen() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settingsOpen
}

func (a *App) ConfigureAttract(disabled, forced bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.attractForcedOff = forced
	a.attractDisabled = disabled || forced
	if forced {
		prefs, err := loadTenfootPrefs(a.prefsPath)
		if err != nil {
			a.attractPrefEnabled = true
		} else {
			a.attractPrefEnabled = prefsAttractEnabled(prefs)
		}
	} else {
		a.attractPrefEnabled = !disabled
	}
	if a.attractDisabled {
		a.hideAttractLocked()
	}
}

func (a *App) settingsSnapshotLocked() SettingsSnapshot {
	if !a.settingsOpen {
		return SettingsSnapshot{}
	}
	rows := a.settingsRowsLocked()
	if a.settingsIndex < 0 {
		a.settingsIndex = 0
	}
	if n := len(rows); n > 0 && a.settingsIndex >= n {
		a.settingsIndex = n - 1
	}
	return SettingsSnapshot{
		Open:         true,
		Title:        a.settingsTitleLocked(),
		Index:        a.settingsIndex,
		Rows:         rows,
		Loading:      a.settingsLoading,
		Busy:         a.settingsBusy,
		Status:       a.settingsStatus,
		Hint:         a.settingsHintLocked(),
		LibraryCount: len(a.settingsDraftLibraries),
		TargetCount:  len(a.settingsDraftTargets),
		SystemCount:  len(a.hostSettings.Systems),
	}
}

func (a *App) settingsTitleLocked() string {
	if title := settingsSectionTitles[a.settingsSection]; title != "" {
		return title
	}
	return "Settings"
}

func (a *App) settingsRowsLocked() []SettingsRow {
	switch a.settingsSection {
	case settingsSectionFolders:
		return a.settingsFolderRowsLocked()
	case settingsSectionFirmware:
		return a.settingsFirmwareRowsLocked()
	case settingsSectionDisplay:
		return a.settingsDisplayRowsLocked()
	case settingsSectionNetwork:
		return a.settingsNetworkRowsLocked()
	case settingsSectionUpdates:
		return []SettingsRow{{ID: "systems", Label: "Cores", Value: "Install cores and add games"}}
	case settingsSectionAbout:
		return a.settingsAboutRowsLocked()
	case settingsSectionDiagnostics:
		return a.settingsDiagnosticRowsLocked()
	case settingsSectionBrowse:
		return a.settingsBrowseRowsLocked()
	}
	return a.settingsSectionListRowsLocked()
}

// settingsSectionsLocked lists the sections in order.
func (a *App) settingsSectionsLocked() []string {
	ids := []string{settingsSectionFolders, settingsSectionFirmware, settingsSectionDisplay}
	if len(a.settingsNetworkRowsLocked()) > 0 {
		ids = append(ids, settingsSectionNetwork)
	}
	ids = append(ids, settingsSectionUpdates, settingsSectionAbout)
	if a.allCores {
		ids = append(ids, settingsSectionBrowse)
	}
	return ids
}

func (a *App) settingsSectionListRowsLocked() []SettingsRow {
	ids := a.settingsSectionsLocked()
	rows := make([]SettingsRow, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, SettingsRow{ID: settingsSectionRowPrefix + id, Label: settingsSectionTitles[id], Value: a.settingsSectionSummaryLocked(id)})
	}
	return rows
}

func (a *App) settingsSectionSummaryLocked(id string) string {
	switch id {
	case settingsSectionFolders:
		if n := len(a.settingsDraftLibraries); n > 0 {
			return fmt.Sprintf("BIOS & media drives  ·  %d libraries", n)
		}
		return "BIOS & media drives"
	case settingsSectionFirmware:
		return a.settingsFirmwareSummaryLocked()
	case settingsSectionDisplay:
		return fmt.Sprintf("%s  ·  %s safe area", a.settingsResolutionLocked(), settingsSafeAreaValue(a.safeAreaPct))
	case settingsSectionNetwork:
		if a.kitDisplay {
			return "Wired  ·  " + a.settingsWired
		}
		if !a.settingsHydrated {
			return "—"
		}
		if n := len(a.settingsDraftTargets); n != 1 {
			return fmt.Sprintf("%d kits", n)
		}
		return "1 kit"
	case settingsSectionUpdates:
		return "Install cores"
	case settingsSectionAbout:
		return "Version, setup, diagnostics, reset"
	case settingsSectionBrowse:
		return "Layout, Home, attract, regions"
	}
	return ""
}

// settingsHostRowsLocked reports whether this launcher edits the host's
// library settings. A paired kit's listener serves none, so those rows would
// do nothing there and are left out.
func (a *App) settingsHostRowsLocked() bool {
	return a.client == nil || !a.client.paired
}

func (a *App) settingsFolderRowsLocked() []SettingsRow {
	rows := []SettingsRow{{ID: "drives", Label: "BIOS & media drives", Value: "Browse USB and SMB · kit or host"}}
	if !a.settingsHostRowsLocked() {
		return rows
	}
	for i, library := range a.settingsDraftLibraries {
		root := strings.TrimSpace(library.Root)
		if root == "" {
			root = "(empty)"
		}
		rows = append(rows, SettingsRow{
			ID:    fmt.Sprintf("library-%d", i),
			Label: a.settingsSystemLabelLocked(library.System),
			Value: root,
		})
	}
	save := "saved"
	if a.settingsLibrariesDirty {
		save = "A save"
	}
	return append(rows,
		SettingsRow{ID: "add-library", Label: "Add library", Value: "A add"},
		SettingsRow{ID: "save-libraries", Label: "Save libraries", Value: save},
	)
}

func (a *App) settingsDisplayRowsLocked() []SettingsRow {
	motion := "Off"
	if a.reducedMotion {
		motion = "On"
	}
	surround := "Off"
	if a.surround {
		surround = "On"
	}
	rows := []SettingsRow{
		{ID: "resolution", Label: "Resolution", Value: a.settingsResolutionLocked()},
		{ID: "safe-area", Label: "Safe area", Value: settingsSafeAreaValue(a.safeAreaPct)},
		{ID: "reduced-motion", Label: "Reduced motion", Value: motion},
		{ID: "surround", Label: "TV surround · kit HDMI", Value: surround},
	}
	if a.settingsHostRowsLocked() {
		rows = append(rows, SettingsRow{ID: "video-profile", Label: "Video preference · next launch", Value: a.settingsVideoValueLocked()})
	}
	return rows
}

// settingsResolutionLocked is the menu's output size. The kit's menu display
// is fixed at 1280×720, so there is nothing to choose yet.
func (a *App) settingsResolutionLocked() string {
	return fmt.Sprintf("%d×%d", a.grid.Width, a.grid.Height)
}

func settingsSafeAreaValue(pct float64) string {
	return strings.TrimSuffix(fmt.Sprintf("%.1f", pct*100), ".0") + "%"
}

// settingsNetworkRowsLocked is the kit's wired link on the kit, and the
// host's kit targets where this launcher edits them. The image has no Wi-Fi.
func (a *App) settingsNetworkRowsLocked() []SettingsRow {
	var rows []SettingsRow
	if a.kitDisplay {
		rows = append(rows, SettingsRow{ID: "wired", Label: "Wired", Value: a.settingsWired})
	}
	if !a.settingsHostRowsLocked() {
		return rows
	}
	target := "—"
	if a.settingsHydrated {
		if name := strings.TrimSpace(a.settingsDraftTarget); name != "" {
			target = name
		} else if names := a.settingsTargetNamesLocked(); len(names) > 0 {
			target = names[0]
		} else {
			target = "(none)"
		}
	}
	rows = append(rows,
		SettingsRow{ID: "target", Label: "Target", Value: target},
		SettingsRow{ID: "prepare-target", Label: "Prepare target", Value: "A generate identity"},
	)
	for i, draft := range a.settingsDraftTargets {
		name := strings.TrimSpace(draft.Name)
		if name == "" {
			name = "(new)"
		}
		label := name
		if strings.TrimSpace(draft.Name) != "" && strings.TrimSpace(draft.Name) == strings.TrimSpace(a.settingsDraftTarget) {
			label = "*" + name
		}
		address := strings.TrimSpace(draft.Address)
		if address == "" {
			address = "(empty)"
		}
		enabled := "Off"
		if draft.Enabled {
			enabled = "On"
		}
		rows = append(rows,
			SettingsRow{ID: settingsTargetRowID(i, settingsTargetFieldName), Label: label + " · name", Value: name},
			SettingsRow{ID: settingsTargetRowID(i, settingsTargetFieldAddress), Label: label + " · address", Value: address},
			SettingsRow{ID: settingsTargetRowID(i, settingsTargetFieldEnabled), Label: label + " · enabled", Value: enabled},
			SettingsRow{ID: settingsTargetRowID(i, settingsTargetFieldAgent), Label: label + " · agent", Value: settingsAgentValue(draft)},
		)
	}
	saveTargets := "saved"
	if a.settingsTargetsDirty {
		saveTargets = "A save"
	}
	return append(rows,
		SettingsRow{ID: "add-target", Label: "Add target", Value: "A add"},
		SettingsRow{ID: "save-targets", Label: "Save targets", Value: saveTargets},
	)
}

func (a *App) settingsAboutRowsLocked() []SettingsRow {
	var rows []SettingsRow
	if v := settingsVersionsValue(); v != "" {
		rows = append(rows, SettingsRow{ID: "versions", Label: "Versions", Value: v})
	}
	if a.kitID != "" {
		rows = append(rows, SettingsRow{ID: "kit-id", Label: "Kit ID", Value: a.kitID})
	}
	reset := "Erases settings, not your files"
	if a.settingsConfirm == "factory-reset" {
		reset = "A again to erase"
	}
	return append(rows,
		SettingsRow{ID: "setup-again", Label: "Run setup again", Value: "A start from the welcome screen"},
		SettingsRow{ID: settingsSectionRowPrefix + settingsSectionDiagnostics, Label: "Diagnostics", Value: "Diagnostic RBF, release kit, all cores"},
		SettingsRow{ID: "factory-reset", Label: "Factory reset", Value: reset},
	)
}

func (a *App) settingsDiagnosticRowsLocked() []SettingsRow {
	allCores := "Off"
	if a.allCores {
		allCores = "On"
	}
	devValue := "A type path · not a game"
	if path := strings.TrimSpace(a.developmentRBFPath); path != "" {
		devValue = path
	}
	release := "free"
	if a.retainedIdleLease || a.stopResponseLost {
		release = "A release"
	}
	return []SettingsRow{
		{ID: "all-cores", Label: "Show all cores · not in this release", Value: allCores},
		{ID: "development-rbf", Label: "DIAGNOSTIC RBF", Value: devValue},
		{ID: "release-kit", Label: "Release kit", Value: release},
	}
}

func (a *App) settingsBrowseRowsLocked() []SettingsRow {
	attract := "On"
	switch {
	case a.attractForcedOff && a.attractPrefEnabled:
		attract = "Off (flag)"
	case !a.attractPrefEnabled || a.attractDisabled:
		attract = "Off"
	}
	idle := "—"
	regions := "—"
	if a.settingsHydrated {
		idle = fmt.Sprintf("%ds", a.settingsDraftIdle)
		regions = formatSettingsRegions(a.settingsDraftRegions, a.settingsRegionIndex)
	}
	rows := []SettingsRow{
		{ID: "layout", Label: "Layout", Value: a.grid.Mode.Label()},
		{ID: "home", Label: "Home", Value: a.settingsHomeValueLocked()},
		{ID: "attract", Label: "Attract", Value: attract},
	}
	if a.settingsHostRowsLocked() {
		rows = append(rows,
			SettingsRow{ID: "idle", Label: "Idle", Value: idle},
			SettingsRow{ID: "regions", Label: "Regions", Value: regions},
		)
	}
	return rows
}

// settingsVersionsValue names the kit software bundle and base image the
// kit-software supervisor started tenfoot with. A host launcher has neither.
func settingsVersionsValue() string {
	var parts []string
	if id := shortSettingsID(os.Getenv("FES_SOFTWARE_BUNDLE_ID")); id != "" {
		parts = append(parts, "Software "+id)
	}
	if id := shortSettingsID(os.Getenv("FES_SOFTWARE_BASE_IDENTITY")); id != "" {
		parts = append(parts, "image "+id)
	}
	return strings.Join(parts, "  ·  ")
}

func shortSettingsID(id string) string {
	id = strings.TrimSpace(id)
	if i := strings.LastIndexAny(id, ":/"); i >= 0 {
		id = id[i+1:]
	}
	if len(id) > 12 {
		id = id[:12]
	}
	return id
}

func settingsTargetRowID(index, field int) string {
	return fmt.Sprintf("target-%d-%s", index, settingsTargetFieldIDs[field])
}

// parseSettingsTargetRowID reads a row ID from settingsTargetRowID.
func parseSettingsTargetRowID(id string) (index, field int, ok bool) {
	rest, ok := strings.CutPrefix(id, "target-")
	if !ok {
		return 0, 0, false
	}
	num, name, ok := strings.Cut(rest, "-")
	if !ok {
		return 0, 0, false
	}
	index, err := strconv.Atoi(num)
	if err != nil {
		return 0, 0, false
	}
	field = slices.Index(settingsTargetFieldIDs[:], name)
	return index, field, field >= 0
}

func parseSettingsFirmwareRowID(id string) (int, bool) {
	rest, ok := strings.CutPrefix(id, "firmware-")
	if !ok {
		return 0, false
	}
	index, err := strconv.Atoi(rest)
	return index, err == nil
}

func parseSettingsLibraryRowID(id string) (int, bool) {
	rest, ok := strings.CutPrefix(id, "library-")
	if !ok {
		return 0, false
	}
	index, err := strconv.Atoi(rest)
	return index, err == nil
}
func (a *App) settingsVideoValueLocked() string {
	if !a.settingsHydrated {
		return "—"
	}
	switch a.hostSettings.VideoProfile {
	case "", "direct":
		return "Direct"
	case "scanlines":
		return "Scanlines"
	default:
		return "Unavailable"
	}
}

func formatSettingsRegions(selected []string, cursor int) string {
	enabled := map[string]bool{}
	for _, region := range selected {
		enabled[strings.ToLower(strings.TrimSpace(region))] = true
	}
	parts := make([]string, 0, len(settingsKnownRegions))
	for i, region := range settingsKnownRegions {
		label := region
		if enabled[region] {
			label += "*"
		}
		if i == cursor {
			label = "[" + label + "]"
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, "  ")
}

// settingsBlockedLocked is the silent refuse used by the settings key.
// A room action entry uses the same conditions but must say so.
func (a *App) settingsBlockedLocked() bool {
	return a.gpuParked || a.session.State == "active" || a.session.State == "launching" || a.stopPhase == "stopping" || a.developmentLoadingLocked()
}

func (a *App) openSettingsLocked() {
	if a.settingsBlockedLocked() {
		return
	}
	a.searchOpen = false
	a.closeCollectionOverlaysLocked()
	a.closeFiltersLocked()
	a.closeDetailLocked()
	a.settingsOpen = true
	a.settingsScopeChanged = false
	a.settingsSection = settingsSectionList
	a.settingsIndex = 0
	a.settingsConfirm = ""
	a.settingsStatus = ""
	a.refreshSettingsWiredLocked()
	a.settingsBusy = false
	a.settingsLoading = true
	a.settingsHydrated = false
	a.settingsRegionIndex = 0
	a.closeSettingsOSKLocked()
	a.settingsGen++
	a.fetchSettingsFirmwareLocked()
	if !a.settingsHostRowsLocked() {
		// The kit launcher listener serves no library settings (hostapi
		// launcherOperation), so Settings leaves the host rows out.
		a.settingsLoading = false
		return
	}
	gen := a.settingsGen
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go a.fetchLibrarySettings(ctx, gen)
}

func (a *App) closeSettingsLocked() {
	if !a.settingsOpen {
		return
	}
	refreshHome := a.settingsScopeChanged
	a.settingsScopeChanged = false
	a.settingsOpen = false
	// A firmware card still loading for Settings › Firmware is dropped.
	if a.firmwareCard.Loading {
		a.closeFirmwareCardLocked()
	}
	a.settingsBusy = false
	a.settingsLoading = false
	a.settingsStatus = ""
	a.closeSettingsOSKLocked()
	a.settingsGen++
	a.discardSettingsDraftsLocked()
	if refreshHome && !a.sessionStopOfferedLocked() && a.launch.Phase != "launching" {
		a.closeAllRoomsLocked()
		a.discardLibraryOriginLocked()
		a.goHomeNowLocked()
	}
}

func (a *App) discardSettingsDraftsLocked() {
	a.settingsLibrariesDirty = false
	a.settingsTargetsDirty = false
	a.closeSettingsOSKLocked()
	if !a.settingsHydrated {
		a.settingsDraftIdle = 0
		a.settingsDraftRegions = nil
		a.settingsDraftTarget = ""
		a.settingsDraftLibraries = nil
		a.settingsDraftTargets = nil
		return
	}
	a.settingsDraftIdle = a.hostSettings.AttractIdleSeconds
	a.settingsDraftRegions = append([]string(nil), a.hostSettings.PreferredRegions...)
	a.settingsDraftTarget = a.hostSettings.SelectedTarget
	a.settingsDraftLibraries = cloneLibraryRoots(a.hostSettings.Libraries)
	a.settingsDraftTargets = cloneSettingsTargetsFromHost(a.hostSettings.Targets)
}

func (a *App) revertSettingsPatchDraftsLocked(patch hostclient.LibrarySettingsPatch) {
	if !a.settingsHydrated {
		return
	}
	if patch.AttractIdleSeconds != nil {
		idle := a.hostSettings.AttractIdleSeconds
		if idle <= 0 {
			idle = defaultAttractIdleSeconds
		}
		a.settingsDraftIdle = idle
	}
	if patch.PreferredRegions != nil {
		a.settingsDraftRegions = append([]string(nil), a.hostSettings.PreferredRegions...)
	}
	if patch.SelectedTarget != nil {
		a.settingsDraftTarget = a.hostSettings.SelectedTarget
	}
	if patch.Targets != nil {
		a.settingsTargetsDirty = false
		a.settingsDraftTargets = cloneSettingsTargetsFromHost(a.hostSettings.Targets)
		if a.settingsOSKKind == settingsOSKTargetName || a.settingsOSKKind == settingsOSKTargetAddress || a.settingsOSKKind == settingsOSKTargetAgent {
			a.closeSettingsOSKLocked()
		}
	}
	if patch.Libraries != nil {
		a.settingsLibrariesDirty = false
		a.settingsDraftLibraries = cloneLibraryRoots(a.hostSettings.Libraries)
		if a.settingsOSKKind == settingsOSKLibraryPath {
			a.closeSettingsOSKLocked()
		}
	}
}

func (a *App) handleSettingsLocked(cmd Command) {
	if !a.settingsOpen {
		return
	}
	switch cmd {
	case CmdSettings:
		a.closeSettingsLocked()
		return
	case CmdBack:
		a.settingsBackLocked()
		return
	}
	rows := a.settingsRowsLocked()
	if len(rows) == 0 {
		return
	}
	switch cmd {
	case CmdUp, CmdTabPrev:
		a.settingsConfirm = ""
		a.settingsIndex = (a.settingsIndex - 1 + len(rows)) % len(rows)
		return
	case CmdDown, CmdTab:
		a.settingsConfirm = ""
		a.settingsIndex = (a.settingsIndex + 1) % len(rows)
		return
	}
	a.clampSettingsIndexLocked()
	id := rows[a.settingsIndex].ID
	if a.settingsBusy && settingsRowWaitsOnHost(id) {
		return
	}
	a.handleSettingsRowLocked(cmd, id)
}

// settingsRowWaitsOnHost reports whether a row is held while a host settings
// write is in flight.
func settingsRowWaitsOnHost(id string) bool {
	switch id {
	case "idle", "regions", "target", "prepare-target", "add-target", "save-targets",
		"add-library", "save-libraries", "development-rbf", "systems", "release-kit":
		return true
	}
	if _, _, ok := parseSettingsTargetRowID(id); ok {
		return true
	}
	_, ok := parseSettingsLibraryRowID(id)
	return ok
}

// openSettingsSectionLocked shows a section's rows from the top.
func (a *App) openSettingsSectionLocked(id string) {
	if _, ok := settingsSectionTitles[id]; !ok {
		return
	}
	a.settingsSection = id
	a.settingsIndex = 0
	a.settingsConfirm = ""
	a.settingsStatus = ""
	if id == settingsSectionNetwork {
		a.refreshSettingsWiredLocked()
	}
}

// settingsBackLocked goes up one level, focusing the section it left, and
// closes Settings from the section list.
func (a *App) settingsBackLocked() {
	if a.settingsSection == settingsSectionList {
		a.closeSettingsLocked()
		return
	}
	left := a.settingsSection
	a.settingsSection = settingsSectionParent(left)
	a.settingsConfirm = ""
	a.settingsStatus = ""
	a.settingsIndex = 0
	a.focusSettingsRowLocked(settingsSectionRowPrefix + left)
}

func (a *App) focusedSettingsRowIDLocked() string {
	rows := a.settingsRowsLocked()
	if a.settingsIndex < 0 || a.settingsIndex >= len(rows) {
		return ""
	}
	return rows[a.settingsIndex].ID
}

// focusSettingsRowLocked moves focus to a row of the open section.
func (a *App) focusSettingsRowLocked(id string) {
	for i, row := range a.settingsRowsLocked() {
		if row.ID == id {
			a.settingsIndex = i
			return
		}
	}
}

func (a *App) handleSettingsRowLocked(cmd Command, id string) {
	switch id {
	case "layout":
		switch cmd {
		case CmdLeft:
			a.setLayoutLocked(prevLayout(a.grid.Mode), true)
			a.status = "layout " + a.grid.Mode.Label()
		case CmdRight, CmdSelect:
			a.cycleLayoutLocked()
		}
	case "safe-area":
		switch cmd {
		case CmdLeft, CmdSafeAreaOut:
			a.setSafeAreaPctLocked(a.safeAreaPct-safeAreaNudge, true)
			a.status = fmt.Sprintf("safe-area %.1f%%", a.safeAreaPct*100)
		case CmdRight, CmdSelect, CmdSafeAreaIn:
			a.setSafeAreaPctLocked(a.safeAreaPct+safeAreaNudge, true)
			a.status = fmt.Sprintf("safe-area %.1f%%", a.safeAreaPct*100)
		}
	case "attract":
		switch cmd {
		case CmdLeft, CmdRight, CmdSelect:
			a.toggleAttractPrefLocked()
		}
	case "home":
		switch cmd {
		case CmdLeft, CmdRight:
			a.toggleHomePrefLocked()
		case CmdSelect:
			a.goHomeNowLocked()
		}
	case "reduced-motion":
		switch cmd {
		case CmdLeft, CmdRight, CmdSelect:
			a.toggleReducedMotionLocked()
		}
	case "surround":
		switch cmd {
		case CmdLeft, CmdRight, CmdSelect:
			a.setSurroundLocked(!a.surround, true)
		}
	case "all-cores":
		switch cmd {
		case CmdLeft, CmdRight, CmdSelect:
			a.setAllCoresLocked(!a.allCores, true)
		}
	case "drives":
		if cmd == CmdSelect {
			a.openDriveSettingsLocked()
		}
	case "setup-again":
		if cmd == CmdSelect {
			a.runSetupAgainLocked()
		}
	case "idle":
		if !a.settingsHydrated {
			return
		}
		switch cmd {
		case CmdLeft:
			a.settingsDraftIdle = clampSettingsIdle(a.settingsDraftIdle - settingsIdleStep)
		case CmdRight:
			a.settingsDraftIdle = clampSettingsIdle(a.settingsDraftIdle + settingsIdleStep)
		case CmdSelect:
			a.patchSettingsLocked(hostclient.LibrarySettingsPatch{AttractIdleSeconds: intPtr(a.settingsDraftIdle)})
		}
	case "regions":
		if !a.settingsHydrated {
			return
		}
		n := len(settingsKnownRegions)
		switch cmd {
		case CmdLeft:
			a.settingsRegionIndex = (a.settingsRegionIndex - 1 + n) % n
		case CmdRight:
			a.settingsRegionIndex = (a.settingsRegionIndex + 1) % n
		case CmdSelect:
			next, ok := togglePreferredRegion(a.settingsDraftRegions, settingsKnownRegions[a.settingsRegionIndex])
			if !ok {
				a.settingsStatus = "keep at least one region"
				a.status = a.settingsStatus
				return
			}
			a.settingsDraftRegions = next
			regions := append([]string(nil), next...)
			a.patchSettingsLocked(hostclient.LibrarySettingsPatch{PreferredRegions: &regions})
		}
	case "video-profile":
		if !a.settingsHydrated || cmd != CmdLeft && cmd != CmdRight && cmd != CmdSelect {
			return
		}
		profile := "scanlines"
		if a.hostSettings.VideoProfile == "scanlines" {
			profile = "direct"
		} else if a.hostSettings.VideoProfile != "" && a.hostSettings.VideoProfile != "direct" {
			a.settingsStatus = "refresh unavailable video preference"
			return
		}
		a.patchSettingsLocked(hostclient.LibrarySettingsPatch{VideoProfile: &profile})
	case "target":
		if !a.settingsHydrated {
			return
		}
		names := a.settingsTargetNamesLocked()
		if len(names) == 0 {
			a.settingsStatus = "no targets"
			a.status = a.settingsStatus
			return
		}
		idx := indexOfString(names, a.settingsDraftTarget)
		if idx < 0 {
			idx = 0
		}
		switch cmd {
		case CmdLeft:
			idx = (idx - 1 + len(names)) % len(names)
			a.settingsDraftTarget = names[idx]
		case CmdRight:
			idx = (idx + 1) % len(names)
			a.settingsDraftTarget = names[idx]
		case CmdSelect:
			name := a.settingsDraftTarget
			if name == "" {
				name = names[0]
				a.settingsDraftTarget = name
			}
			if a.settingsSessionBlocksSelectedLocked() {
				a.settingsStatus = "cannot change target while a session is active"
				a.status = a.settingsStatus
				a.settingsDraftTarget = a.hostSettings.SelectedTarget
				return
			}
			if a.settingsTargetsDirty {
				a.saveSettingsTargetsLocked()
				return
			}
			a.patchSettingsLocked(hostclient.LibrarySettingsPatch{SelectedTarget: strPtr(name)})
		}
	case "prepare-target":
		if !a.settingsHydrated {
			return
		}
		if cmd == CmdSelect {
			if a.settingsTargetsDirty {
				a.settingsStatus = "save target edits before preparing identity"
				a.status = a.settingsStatus
				return
			}
			name := strings.TrimSpace(a.settingsDraftTarget)
			if name == "" {
				a.settingsStatus = "select a target first"
				a.status = a.settingsStatus
				return
			}
			a.patchSettingsLocked(hostclient.LibrarySettingsPatch{PrepareTarget: strPtr(name)})
		}
	case "factory-reset":
		if cmd == CmdSelect {
			if a.settingsConfirm != "factory-reset" {
				a.settingsConfirm = "factory-reset"
				return
			}
			a.factoryResetLocked()
		}
	case "add-target":
		if cmd == CmdSelect {
			a.addSettingsTargetLocked()
		}
	case "save-targets":
		if cmd == CmdSelect {
			a.saveSettingsTargetsLocked()
		}
	case "add-library":
		if cmd == CmdSelect {
			a.addSettingsLibraryLocked()
		}
	case "save-libraries":
		if cmd == CmdSelect {
			a.saveSettingsLibrariesLocked()
		}
	case "development-rbf":
		if cmd == CmdSelect {
			a.openDevelopmentPathOSKLocked()
		}
	case "systems":
		if cmd == CmdSelect {
			a.openCoreLibraryLocked()
		}
	case "release-kit":
		if cmd == CmdSelect {
			a.startReleaseIdleLeaseLocked()
		}
	default:
		if section, ok := strings.CutPrefix(id, settingsSectionRowPrefix); ok {
			if cmd == CmdSelect || cmd == CmdRight {
				a.openSettingsSectionLocked(section)
			}
		} else if index, field, ok := parseSettingsTargetRowID(id); ok {
			a.handleSettingsTargetEntryLocked(cmd, index, field)
		} else if index, ok := parseSettingsLibraryRowID(id); ok {
			a.handleSettingsLibraryEntryLocked(cmd, index)
		} else if index, ok := parseSettingsFirmwareRowID(id); ok && cmd == CmdSelect {
			a.openSettingsFirmwareCardLocked(index)
		}
	}
}

func (a *App) clampSettingsIndexLocked() {
	n := len(a.settingsRowsLocked())
	if n <= 0 || a.settingsIndex < 0 {
		a.settingsIndex = 0
		return
	}
	if a.settingsIndex >= n {
		a.settingsIndex = n - 1
	}
}

func (a *App) handleSettingsTargetEntryLocked(cmd Command, index, field int) {
	if !a.settingsHydrated || index < 0 || index >= len(a.settingsDraftTargets) {
		return
	}
	switch cmd {
	case CmdSortCycle:
		a.removeSettingsTargetLocked(index)
		return
	}
	switch field {
	case settingsTargetFieldEnabled:
		switch cmd {
		case CmdLeft, CmdRight, CmdSelect:
			a.toggleSettingsTargetEnabledLocked(index)
		}
	case settingsTargetFieldAgent:
		switch cmd {
		case CmdLeft, CmdRight:
			a.toggleSettingsTargetAgentClearLocked(index)
		case CmdSelect:
			a.openTargetOSKLocked(index, settingsOSKTargetAgent, false)
		}
	case settingsTargetFieldName:
		if cmd == CmdSelect {
			a.openTargetOSKLocked(index, settingsOSKTargetName, false)
		}
	case settingsTargetFieldAddress:
		if cmd == CmdSelect {
			a.openTargetOSKLocked(index, settingsOSKTargetAddress, false)
		}
	}
}

func (a *App) toggleSettingsTargetEnabledLocked(index int) {
	draft := a.settingsDraftTargets[index]
	next := !draft.Enabled
	if !next && a.settingsSessionBlocksSelectedLocked() && a.settingsDraftIsSelectedLocked(draft) {
		a.settingsStatus = "cannot change target while a session is active"
		a.status = a.settingsStatus
		return
	}
	a.settingsDraftTargets[index].Enabled = next
	a.settingsTargetsDirty = true
	a.settingsStatus = ""
}

func (a *App) toggleSettingsTargetAgentClearLocked(index int) {
	draft := &a.settingsDraftTargets[index]
	if !draft.AgentConfigured && !draft.AgentClear {
		return
	}
	if a.settingsSessionBlocksSelectedLocked() && a.settingsDraftIsSelectedLocked(*draft) {
		a.settingsStatus = "cannot change target while a session is active"
		a.status = a.settingsStatus
		return
	}
	if draft.AgentClear {
		draft.AgentClear = false
		draft.AgentDirty = false
		draft.AgentDraft = ""
	} else {
		draft.AgentClear = true
		draft.AgentDirty = true
		draft.AgentDraft = ""
	}
	a.settingsTargetsDirty = true
	a.settingsStatus = ""
}

func (a *App) addSettingsTargetLocked() {
	if !a.settingsHydrated {
		return
	}
	name := a.settingsUniqueTargetNameLocked()
	a.settingsDraftTargets = append(a.settingsDraftTargets, settingsTargetDraft{Name: name})
	a.focusSettingsRowLocked(settingsTargetRowID(len(a.settingsDraftTargets)-1, settingsTargetFieldName))
	a.openTargetOSKLocked(len(a.settingsDraftTargets)-1, settingsOSKTargetName, true)
}

func (a *App) removeSettingsTargetLocked(index int) {
	if index < 0 || index >= len(a.settingsDraftTargets) {
		return
	}
	if len(a.settingsDraftTargets) <= 1 {
		a.settingsStatus = "keep at least one target"
		a.status = a.settingsStatus
		return
	}
	draft := a.settingsDraftTargets[index]
	if a.settingsSessionBlocksSelectedLocked() && a.settingsDraftIsSelectedLocked(draft) {
		a.settingsStatus = "cannot change target while a session is active"
		a.status = a.settingsStatus
		return
	}
	a.settingsDraftTargets = append(a.settingsDraftTargets[:index], a.settingsDraftTargets[index+1:]...)
	a.settingsTargetsDirty = true
	a.settingsPickSelectedLocked()
	a.clampSettingsIndexLocked()
}

func (a *App) saveSettingsTargetsLocked() {
	if !a.settingsHydrated {
		return
	}
	if err := a.validateSettingsTargetsLocked(); err != nil {
		a.settingsStatus = err.Error()
		a.status = a.settingsStatus
		return
	}
	if a.settingsSessionBlocksSelectedLocked() && a.settingsSelectedIdentityDirtyLocked() {
		a.settingsStatus = "cannot change target while a session is active"
		a.status = a.settingsStatus
		return
	}
	writes := make([]hostclient.LibraryTargetWrite, 0, len(a.settingsDraftTargets))
	for _, draft := range a.settingsDraftTargets {
		writes = append(writes, draft.write())
	}
	patch := hostclient.LibrarySettingsPatch{Targets: &writes}
	if a.settingsNeedSelectedPatchLocked() {
		name := strings.TrimSpace(a.settingsDraftTarget)
		patch.SelectedTarget = strPtr(name)
	}
	a.patchSettingsLocked(patch)
}

func (d settingsTargetDraft) write() hostclient.LibraryTargetWrite {
	out := hostclient.LibraryTargetWrite{
		Name:    strings.TrimSpace(d.Name),
		Address: strings.TrimSpace(d.Address),
		Enabled: d.Enabled,
	}
	orig := strings.TrimSpace(d.OriginalName)
	if orig != "" && orig != out.Name {
		out.OriginalName = orig
	}
	if d.AgentClear {
		empty := ""
		out.Agent = &empty
	} else if d.AgentDirty {
		out.Agent = strPtr(d.AgentDraft)
	}
	return out
}

func (a *App) validateSettingsTargetsLocked() error {
	if len(a.settingsDraftTargets) == 0 {
		return fmt.Errorf("keep at least one target")
	}
	seen := map[string]struct{}{}
	for _, draft := range a.settingsDraftTargets {
		name := strings.TrimSpace(draft.Name)
		if name == "" {
			return fmt.Errorf("target name is required")
		}
		if err := validateTargetName(name); err != nil {
			return err
		}
		if _, dup := seen[name]; dup {
			return fmt.Errorf("duplicate target name")
		}
		seen[name] = struct{}{}
		address := strings.TrimSpace(draft.Address)
		hasAgent := (draft.AgentConfigured && !draft.AgentClear) || (draft.AgentDirty && strings.TrimSpace(draft.AgentDraft) != "")
		if address != "" && !hasAgent {
			return fmt.Errorf("address and agent must both be set")
		}
		if hasAgent && address == "" {
			return fmt.Errorf("address and agent must both be set")
		}
		if draft.Enabled && address == "" {
			return fmt.Errorf("enabled target needs address and agent")
		}
	}
	a.settingsPickSelectedLocked()
	if strings.TrimSpace(a.settingsDraftTarget) == "" || indexOfString(a.settingsTargetNamesLocked(), a.settingsDraftTarget) < 0 {
		return fmt.Errorf("selected target is required")
	}
	return nil
}

func validateTargetName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("target name is required")
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			continue
		}
		return fmt.Errorf("target name must be a lowercase slug")
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") || strings.Contains(name, "--") {
		return fmt.Errorf("target name must be a lowercase slug")
	}
	return nil
}

func (a *App) settingsUniqueTargetNameLocked() string {
	used := map[string]struct{}{}
	for _, draft := range a.settingsDraftTargets {
		used[strings.TrimSpace(draft.Name)] = struct{}{}
	}
	for i := 1; i < 1000; i++ {
		name := fmt.Sprintf("target-%d", i)
		if _, ok := used[name]; !ok {
			return name
		}
	}
	return "target-new"
}

func (a *App) settingsDraftIsSelectedLocked(draft settingsTargetDraft) bool {
	selected := strings.TrimSpace(a.settingsDraftTarget)
	if selected == "" {
		selected = strings.TrimSpace(a.hostSettings.SelectedTarget)
	}
	name := strings.TrimSpace(draft.Name)
	orig := strings.TrimSpace(draft.OriginalName)
	return selected != "" && (selected == name || selected == orig)
}

func (a *App) settingsPickSelectedLocked() {
	names := a.settingsTargetNamesLocked()
	if indexOfString(names, a.settingsDraftTarget) >= 0 {
		return
	}
	for _, draft := range a.settingsDraftTargets {
		if draft.Enabled && strings.TrimSpace(draft.Name) != "" {
			a.settingsDraftTarget = strings.TrimSpace(draft.Name)
			return
		}
	}
	if len(names) > 0 {
		a.settingsDraftTarget = names[0]
	}
}

func (a *App) settingsNeedSelectedPatchLocked() bool {
	return strings.TrimSpace(a.settingsDraftTarget) != strings.TrimSpace(a.hostSettings.SelectedTarget)
}

func (a *App) settingsSessionBlocksSelectedLocked() bool {
	return a.session.State == "active" || a.session.State == "launching" || a.stopPhase == "stopping"
}

func (a *App) settingsSelectedIdentityDirtyLocked() bool {
	hostSelected := strings.TrimSpace(a.hostSettings.SelectedTarget)
	draftSelected := strings.TrimSpace(a.settingsDraftTarget)
	if draftSelected != hostSelected {
		var renamedCurrent bool
		for _, draft := range a.settingsDraftTargets {
			if strings.TrimSpace(draft.OriginalName) == hostSelected && strings.TrimSpace(draft.Name) == draftSelected {
				renamedCurrent = true
				break
			}
		}
		if !renamedCurrent {
			return true
		}
	}
	want := draftSelected
	if want == "" {
		want = hostSelected
	}
	var host hostclient.LibraryTarget
	for _, target := range a.hostSettings.Targets {
		if strings.TrimSpace(target.Name) == hostSelected {
			host = target
			break
		}
	}
	for _, draft := range a.settingsDraftTargets {
		name := strings.TrimSpace(draft.Name)
		orig := strings.TrimSpace(draft.OriginalName)
		if name != want && orig != hostSelected {
			continue
		}
		if draft.Enabled != host.Enabled || strings.TrimSpace(draft.Address) != strings.TrimSpace(host.Address) {
			return true
		}
		if draft.AgentDirty || draft.AgentClear {
			return true
		}
		return false
	}
	return true
}

func settingsAgentValue(draft settingsTargetDraft) string {
	switch {
	case draft.AgentClear:
		return "will clear"
	case draft.AgentDirty && strings.TrimSpace(draft.AgentDraft) != "":
		return "will set"
	case draft.AgentConfigured:
		return "stored"
	default:
		return "not set"
	}
}

func (a *App) handleSettingsLibraryEntryLocked(cmd Command, index int) {
	if !a.settingsHydrated || index < 0 || index >= len(a.settingsDraftLibraries) {
		return
	}
	switch cmd {
	case CmdLeft, CmdRight:
		ids := a.settingsSystemChoicesLocked(a.settingsDraftLibraries[index].System)
		if len(ids) == 0 {
			a.settingsStatus = "no systems"
			a.status = a.settingsStatus
			return
		}
		idx := indexOfString(ids, a.settingsDraftLibraries[index].System)
		if idx < 0 {
			idx = 0
		}
		if cmd == CmdLeft {
			idx = (idx - 1 + len(ids)) % len(ids)
		} else {
			idx = (idx + 1) % len(ids)
		}
		next := ids[idx]
		if next == a.settingsDraftLibraries[index].System {
			return
		}
		a.settingsDraftLibraries[index].System = next
		a.settingsLibrariesDirty = true
	case CmdSelect:
		a.openLibraryPathOSKLocked(index, false)
	case CmdSortCycle:
		a.removeSettingsLibraryLocked(index)
	}
}

func (a *App) addSettingsLibraryLocked() {
	if !a.settingsHydrated {
		return
	}
	ids := a.settingsSystemIDsLocked()
	if len(ids) == 0 {
		a.settingsStatus = "no systems"
		a.status = a.settingsStatus
		return
	}
	a.settingsDraftLibraries = append(a.settingsDraftLibraries, hostclient.LibraryRoot{System: ids[0]})
	a.focusSettingsRowLocked(fmt.Sprintf("library-%d", len(a.settingsDraftLibraries)-1))
	a.openLibraryPathOSKLocked(len(a.settingsDraftLibraries)-1, true)
}

func (a *App) removeSettingsLibraryLocked(index int) {
	if index < 0 || index >= len(a.settingsDraftLibraries) {
		return
	}
	a.settingsDraftLibraries = append(a.settingsDraftLibraries[:index], a.settingsDraftLibraries[index+1:]...)
	a.settingsLibrariesDirty = true
	a.clampSettingsIndexLocked()
}

func (a *App) saveSettingsLibrariesLocked() {
	if !a.settingsHydrated {
		return
	}
	for _, library := range a.settingsDraftLibraries {
		if strings.TrimSpace(library.System) == "" || strings.TrimSpace(library.Root) == "" {
			a.settingsStatus = "library system and path are required"
			a.status = a.settingsStatus
			return
		}
	}
	libraries := cloneLibraryRoots(a.settingsDraftLibraries)
	a.patchSettingsLocked(hostclient.LibrarySettingsPatch{Libraries: &libraries})
}

func (a *App) settingsOSKOpenLocked() bool {
	return a.settingsOSKKind != settingsOSKNone
}

func (a *App) openLibraryPathOSKLocked(index int, isAdd bool) {
	if !a.settingsHydrated || index < 0 || index >= len(a.settingsDraftLibraries) {
		return
	}
	a.settingsOSKKind = settingsOSKLibraryPath
	a.settingsOSKIndex = index
	a.settingsOSKIsAdd = isAdd
	a.settingsOSKField = shared.TextField{Buffer: a.settingsDraftLibraries[index].Root}
	a.settingsOSKField.OSK.Reset()
	a.settingsOSKField.OSK.CyclePage(1)
}

func (a *App) openTargetOSKLocked(index int, kind settingsOSKKind, isAdd bool) {
	if !a.settingsHydrated || index < 0 || index >= len(a.settingsDraftTargets) {
		return
	}
	if a.settingsSessionBlocksSelectedLocked() && a.settingsDraftIsSelectedLocked(a.settingsDraftTargets[index]) {
		a.settingsStatus = "cannot change target while a session is active"
		a.status = a.settingsStatus
		return
	}
	buffer := ""
	switch kind {
	case settingsOSKTargetName:
		buffer = a.settingsDraftTargets[index].Name
	case settingsOSKTargetAddress:
		buffer = a.settingsDraftTargets[index].Address
	case settingsOSKTargetAgent:
		buffer = ""
	default:
		return
	}
	a.settingsOSKKind = kind
	a.settingsOSKIndex = index
	a.settingsOSKIsAdd = isAdd
	a.settingsOSKField = shared.TextField{Buffer: buffer}
	a.settingsOSKField.OSK.Reset()
	if kind == settingsOSKTargetAddress {
		a.settingsOSKField.OSK.CyclePage(1)
	}
}

func (a *App) closeSettingsOSKLocked() {
	a.settingsOSKKind = settingsOSKNone
	a.settingsOSKIndex = 0
	a.settingsOSKIsAdd = false
	a.settingsOSKField = shared.TextField{}
}

func (a *App) handleSettingsOSKLocked(cmd Command) {
	switch cmd {
	case CmdUp:
		a.settingsOSKField.Move(0, -1)
	case CmdDown:
		a.settingsOSKField.Move(0, 1)
	case CmdLeft:
		a.settingsOSKField.Move(-1, 0)
	case CmdRight:
		a.settingsOSKField.Move(1, 0)
	case CmdSelect:
		result := a.settingsOSKField.Activate()
		if result.Done {
			a.submitSettingsOSKLocked()
		}
	case CmdBack:
		if strings.TrimSpace(a.settingsOSKField.Buffer) != "" {
			a.settingsOSKField.Clear()
			return
		}
		a.cancelSettingsOSKLocked()
	case CmdSearch:
		a.cancelSettingsOSKLocked()
	case CmdTab:
		a.submitSettingsOSKLocked()
	case CmdTabPrev, CmdFilterPrev:
		a.settingsOSKField.CyclePage(-1)
	case CmdFilterNext:
		a.settingsOSKField.CyclePage(1)
	}
}

func (a *App) submitSettingsOSKLocked() {
	switch a.settingsOSKKind {
	case settingsOSKDriveField:
		a.submitDriveFieldLocked()
	case settingsOSKCorePath, settingsOSKCoreTitle:
		a.submitCoreLibraryOSKLocked()
	case settingsOSKLibraryPath:
		a.submitLibraryPathOSKLocked()
	case settingsOSKTargetName:
		a.submitTargetNameOSKLocked()
	case settingsOSKTargetAddress:
		a.submitTargetAddressOSKLocked()
	case settingsOSKTargetAgent:
		a.submitTargetAgentOSKLocked()
	case settingsOSKDevelopmentPath:
		a.submitDevelopmentPathOSKLocked()
	case settingsOSKFirmwarePath:
		a.submitFirmwarePathOSKLocked()
	case settingsOSKTapePath:
		a.submitTapePathOSKLocked()
	default:
		a.closeSettingsOSKLocked()
	}
}

func (a *App) submitLibraryPathOSKLocked() {
	index := a.settingsOSKIndex
	if index < 0 || index >= len(a.settingsDraftLibraries) {
		a.closeSettingsOSKLocked()
		return
	}
	path := strings.TrimSpace(a.settingsOSKField.Buffer)
	if path == "" {
		a.settingsStatus = "library path is required"
		a.status = a.settingsStatus
		return
	}
	a.settingsDraftLibraries[index].Root = path
	a.settingsLibrariesDirty = true
	a.closeSettingsOSKLocked()
	a.settingsStatus = ""
}

func (a *App) submitTargetNameOSKLocked() {
	index := a.settingsOSKIndex
	if index < 0 || index >= len(a.settingsDraftTargets) {
		a.closeSettingsOSKLocked()
		return
	}
	name := strings.TrimSpace(a.settingsOSKField.Buffer)
	if err := validateTargetName(name); err != nil {
		a.settingsStatus = err.Error()
		a.status = a.settingsStatus
		return
	}
	for i, draft := range a.settingsDraftTargets {
		if i != index && strings.TrimSpace(draft.Name) == name {
			a.settingsStatus = "duplicate target name"
			a.status = a.settingsStatus
			return
		}
	}
	prev := strings.TrimSpace(a.settingsDraftTargets[index].Name)
	if a.settingsSessionBlocksSelectedLocked() && a.settingsDraftIsSelectedLocked(a.settingsDraftTargets[index]) && name != prev {
		a.settingsStatus = "cannot change target while a session is active"
		a.status = a.settingsStatus
		return
	}
	if strings.TrimSpace(a.settingsDraftTarget) == prev {
		a.settingsDraftTarget = name
	}
	a.settingsDraftTargets[index].Name = name
	a.settingsTargetsDirty = true
	a.closeSettingsOSKLocked()
	a.settingsStatus = ""
}

func (a *App) submitTargetAddressOSKLocked() {
	index := a.settingsOSKIndex
	if index < 0 || index >= len(a.settingsDraftTargets) {
		a.closeSettingsOSKLocked()
		return
	}
	if a.settingsSessionBlocksSelectedLocked() && a.settingsDraftIsSelectedLocked(a.settingsDraftTargets[index]) {
		a.settingsStatus = "cannot change target while a session is active"
		a.status = a.settingsStatus
		return
	}
	a.settingsDraftTargets[index].Address = strings.TrimSpace(a.settingsOSKField.Buffer)
	a.settingsTargetsDirty = true
	a.closeSettingsOSKLocked()
	a.settingsStatus = ""
}

func (a *App) submitTargetAgentOSKLocked() {
	index := a.settingsOSKIndex
	if index < 0 || index >= len(a.settingsDraftTargets) {
		a.closeSettingsOSKLocked()
		return
	}
	if a.settingsSessionBlocksSelectedLocked() && a.settingsDraftIsSelectedLocked(a.settingsDraftTargets[index]) {
		a.settingsStatus = "cannot change target while a session is active"
		a.status = a.settingsStatus
		return
	}
	secret := a.settingsOSKField.Buffer
	if strings.TrimSpace(secret) == "" {
		a.closeSettingsOSKLocked()
		a.settingsStatus = ""
		return
	}
	a.settingsDraftTargets[index].AgentDraft = secret
	a.settingsDraftTargets[index].AgentDirty = true
	a.settingsDraftTargets[index].AgentClear = false
	a.settingsTargetsDirty = true
	a.closeSettingsOSKLocked()
	a.settingsStatus = ""
}

func (a *App) cancelSettingsOSKLocked() {
	kind := a.settingsOSKKind
	index := a.settingsOSKIndex
	isAdd := a.settingsOSKIsAdd
	a.closeSettingsOSKLocked()
	if !isAdd {
		return
	}
	switch kind {
	case settingsOSKLibraryPath:
		if index < 0 || index >= len(a.settingsDraftLibraries) {
			return
		}
		if strings.TrimSpace(a.settingsDraftLibraries[index].Root) != "" {
			return
		}
		a.settingsDraftLibraries = append(a.settingsDraftLibraries[:index], a.settingsDraftLibraries[index+1:]...)
		a.clampSettingsIndexLocked()
	case settingsOSKTargetName:
		if index < 0 || index >= len(a.settingsDraftTargets) {
			return
		}
		if len(a.settingsDraftTargets) <= 1 {
			return
		}
		a.settingsDraftTargets = append(a.settingsDraftTargets[:index], a.settingsDraftTargets[index+1:]...)
		a.settingsPickSelectedLocked()
		a.settingsTargetsDirty = !settingsTargetsMatchHost(a.settingsDraftTargets, a.hostSettings.Targets)
		a.clampSettingsIndexLocked()
	}
}

func prevLayout(mode LayoutKind) LayoutKind {
	switch mode {
	case LayoutGrid:
		return LayoutList
	case LayoutShelf:
		return LayoutGrid
	default:
		return LayoutShelf
	}
}

func clampSettingsIdle(seconds int) int {
	if seconds < minSettingsIdleSeconds {
		return minSettingsIdleSeconds
	}
	if seconds > maxSettingsIdleSeconds {
		return maxSettingsIdleSeconds
	}
	return seconds
}

func (a *App) toggleAttractPrefLocked() {
	a.setAttractPrefLocked(!a.attractPrefEnabled, true)
}

func (a *App) setAttractPrefLocked(enabled bool, persist bool) {
	a.attractPrefEnabled = enabled
	if persist {
		a.persistPrefsLocked("attract")
	}
	if a.attractForcedOff {
		a.attractDisabled = true
		a.hideAttractLocked()
		if enabled {
			a.status = "attract off (flag)"
		} else {
			a.status = "attract off"
		}
		return
	}
	a.attractDisabled = !enabled
	if a.attractDisabled {
		a.hideAttractLocked()
		a.status = "attract off"
		return
	}
	a.status = "attract on"
}

func (a *App) settingsTargetNamesLocked() []string {
	names := make([]string, 0, len(a.settingsDraftTargets))
	seen := map[string]struct{}{}
	for _, target := range a.settingsDraftTargets {
		name := strings.TrimSpace(target.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	if len(names) == 0 {
		if selected := strings.TrimSpace(a.hostSettings.SelectedTarget); selected != "" {
			names = append(names, selected)
			seen[selected] = struct{}{}
		}
		for _, target := range a.hostSettings.Targets {
			name := strings.TrimSpace(target.Name)
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
		return names
	}
	selected := strings.TrimSpace(a.settingsDraftTarget)
	if idx := indexOfString(names, selected); idx > 0 {
		names = append([]string{selected}, append(names[:idx], names[idx+1:]...)...)
	}
	return names
}

func (a *App) fetchLibrarySettings(ctx context.Context, gen int) {
	a.mu.Lock()
	writeGen := a.settingsWriteGen
	a.mu.Unlock()
	settings, err := a.client.LibrarySettings(ctx)
	a.mu.Lock()
	defer a.mu.Unlock()
	if gen != a.settingsGen {
		return
	}
	if writeGen != a.settingsWriteGen {
		a.settingsLoading = false
		a.hydrateSettingsFromAppliedLocked()
		return
	}
	a.settingsLoading = false
	if err != nil {
		a.settingsStatus = "settings failed"
		a.status = a.settingsStatus
		return
	}
	a.applyHostSettingsLocked(settings)
	a.settingsHydrated = true
	a.settingsStatus = ""
}

func (a *App) hydrateSettingsFromAppliedLocked() {
	if !a.settingsOpen || a.settingsHydrated {
		return
	}
	a.applyHostSettingsLocked(a.hostSettings)
	a.settingsStatus = ""
}

func (a *App) refreshSettingsDraftsFromAppliedLocked(prev hostclient.LibrarySettings, seq int) {
	if !a.settingsOpen {
		return
	}
	if seq < a.settingsPatchSeq {
		return
	}
	if a.settingsHydrated && !a.settingsDraftsMatchLocked(prev) {
		return
	}
	a.applyHostSettingsLocked(a.hostSettings)
	a.settingsStatus = ""
}

func (a *App) settingsDraftsMatchLocked(settings hostclient.LibrarySettings) bool {
	idle := settings.AttractIdleSeconds
	if idle <= 0 {
		idle = defaultAttractIdleSeconds
	}
	if a.settingsDraftIdle != idle {
		return false
	}
	if strings.TrimSpace(a.settingsDraftTarget) != strings.TrimSpace(settings.SelectedTarget) {
		return false
	}
	if a.settingsTargetsDirty || !settingsTargetsMatchHost(a.settingsDraftTargets, settings.Targets) {
		return false
	}
	if !slices.Equal(a.settingsDraftLibraries, settings.Libraries) {
		return false
	}
	return slices.Equal(a.settingsDraftRegions, settings.PreferredRegions)
}

func (a *App) applyHostSettingsLocked(settings hostclient.LibrarySettings) {
	// Host rows come and go as drafts load, so keep focus on the same row.
	focused := a.focusedSettingsRowIDLocked()
	defer func() {
		if focused != "" {
			a.focusSettingsRowLocked(focused)
		}
		a.clampSettingsIndexLocked()
	}()
	a.hostSettings = settings
	a.settingsDraftIdle = settings.AttractIdleSeconds
	if a.settingsDraftIdle <= 0 {
		a.settingsDraftIdle = defaultAttractIdleSeconds
	}
	a.settingsDraftRegions = append([]string(nil), settings.PreferredRegions...)
	a.settingsDraftTarget = strings.TrimSpace(settings.SelectedTarget)
	if !a.settingsTargetsDirty {
		a.settingsDraftTargets = cloneSettingsTargetsFromHost(settings.Targets)
	}
	if !a.settingsLibrariesDirty {
		a.settingsDraftLibraries = cloneLibraryRoots(settings.Libraries)
	}
	a.settingsHydrated = true
}

func (a *App) patchSettingsLocked(patch hostclient.LibrarySettingsPatch) {
	if a.settingsBusy {
		return
	}
	a.settingsBusy = true
	a.settingsStatus = "saving"
	a.status = a.settingsStatus
	a.settingsPatchSeq++
	gen := a.settingsGen
	seq := a.settingsPatchSeq
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go a.commitLibrarySettings(ctx, gen, seq, patch)
}

func (a *App) commitLibrarySettings(ctx context.Context, gen, seq int, patch hostclient.LibrarySettingsPatch) {
	settings, err := a.client.PatchLibrarySettings(ctx, patch)
	a.mu.Lock()
	defer a.mu.Unlock()
	current := gen == a.settingsGen && seq == a.settingsPatchSeq
	if err != nil {
		if !current {
			return
		}
		a.settingsBusy = false
		a.settingsStatus = settingsStatusError(err)
		a.status = a.settingsStatus
		a.revertSettingsPatchDraftsLocked(patch)
		return
	}
	if seq > a.settingsAppliedSeq {
		prev := a.hostSettings
		a.settingsAppliedSeq = seq
		a.hostSettings = settings
		if patch.AttractIdleSeconds != nil {
			a.applyAttractIdleFromSettingsLocked(settings.AttractIdleSeconds)
		}
		if patch.PreferredRegions != nil {
			a.reloadLocked()
		}
		if patch.Targets != nil {
			if current {
				a.settingsTargetsDirty = false
			}
		}
		if patch.Libraries != nil {
			if current {
				a.settingsLibrariesDirty = false
			}
			a.reloadLocked()
		}
		a.settingsWriteGen++
		a.status = a.settingsSavedStatusLocked(patch)
		if !current {
			a.settingsLoading = false
			a.refreshSettingsDraftsFromAppliedLocked(prev, seq)
		}
	}
	if !current {
		return
	}
	a.applyHostSettingsLocked(settings)
	a.settingsBusy = false
	a.settingsStatus = ""
}

func (a *App) applyAttractIdleFromSettingsLocked(seconds int) {
	if seconds <= 0 {
		seconds = defaultAttractIdleSeconds
	}
	a.attractIdle = attractIdleDuration(seconds)
	a.attractIdleSeconds = seconds
	a.attractIdleReady = true
	a.attractIdleAt = time.Now()
	a.attractGen++
	a.attractLoading = false
}

func (a *App) settingsSavedStatusLocked(patch hostclient.LibrarySettingsPatch) string {
	switch {
	case patch.VideoProfile != nil:
		return "Video: " + a.settingsVideoValueLocked() + " · next launch"
	case patch.AttractIdleSeconds != nil:
		return fmt.Sprintf("idle %ds", a.hostSettings.AttractIdleSeconds)
	case patch.PreferredRegions != nil:
		return "regions " + strings.Join(a.hostSettings.PreferredRegions, ", ")
	case patch.SelectedTarget != nil && patch.Targets == nil:
		return "target " + a.hostSettings.SelectedTarget
	case patch.PrepareTarget != nil:
		return "target identity prepared"
	case patch.Targets != nil:
		return fmt.Sprintf("targets %d", len(a.hostSettings.Targets))
	case patch.Libraries != nil:
		return fmt.Sprintf("libraries %d", len(a.hostSettings.Libraries))
	default:
		return "settings saved"
	}
}

func (a *App) settingsHintLocked() string {
	if a.settingsOSKOpenLocked() {
		return ""
	}
	kind := a.affinity.current.Kind
	hint := a.settingsRowHintLocked(kind)
	if a.settingsSection != settingsSectionList {
		hint = settingsSectionTitles[a.settingsSection] + "  ·  " + hint
	}
	return hint
}

func (a *App) settingsRowHintLocked(kind InputKind) string {
	rows := a.settingsRowsLocked()
	leave := "back"
	if a.settingsSection == settingsSectionList {
		leave = "close"
	}
	if a.settingsIndex < 0 || a.settingsIndex >= len(rows) {
		return settingsDefaultHint(kind, leave)
	}
	id := rows[a.settingsIndex].ID
	if strings.HasPrefix(id, settingsSectionRowPrefix) {
		return settingsActionHint(kind, "open", "", leave)
	}
	if _, field, ok := parseSettingsTargetRowID(id); ok {
		switch field {
		case settingsTargetFieldName:
			return settingsRemoveHint(kind, "name", "", leave)
		case settingsTargetFieldAddress:
			return settingsRemoveHint(kind, "address", "", leave)
		case settingsTargetFieldEnabled:
			return settingsRemoveHint(kind, "enabled", "Left/Right", leave)
		case settingsTargetFieldAgent:
			return settingsRemoveHint(kind, "set agent", "Left/Right clear", leave)
		}
	}
	if _, ok := parseSettingsLibraryRowID(id); ok {
		return settingsRemoveHint(kind, "path", "Left/Right system", leave)
	}
	if _, ok := parseSettingsFirmwareRowID(id); ok {
		return settingsActionHint(kind, "details", "", leave)
	}
	switch id {
	case "home":
		return settingsActionHint(kind, "go home", "Left/Right start screen", leave)
	case "reduced-motion", "surround", "all-cores":
		return settingsActionHint(kind, "toggle", "Left/Right", leave)
	case "resolution", "versions", "kit-id", "wired":
		return backWord(kind) + " " + leave
	case "setup-again":
		return settingsActionHint(kind, "start setup", "", leave)
	case "factory-reset":
		if a.settingsConfirm == "factory-reset" {
			return settingsActionHint(kind, "erase settings", "Up/Down cancel", leave)
		}
		return settingsActionHint(kind, "reset", "", leave)
	case "add-target":
		return settingsActionHint(kind, "add target", "", leave)
	case "save-targets":
		return settingsActionHint(kind, "save targets", "", leave)
	case "systems":
		return settingsActionHint(kind, "browse systems", "", leave)
	case "add-library":
		return settingsActionHint(kind, "add library", "", leave)
	case "save-libraries":
		return settingsActionHint(kind, "save libraries", "", leave)
	case "development-rbf":
		return settingsActionHint(kind, "type path", "", leave) + " · not a game session · HDMI/input may be down"
	case "release-kit":
		return settingsActionHint(kind, "release kit", "Returning to the menu keeps it", leave)
	}
	return settingsDefaultHint(kind, leave)
}

func (a *App) settingsSystemIDsLocked() []string {
	ids := make([]string, 0, len(a.hostSettings.Systems))
	seen := map[string]struct{}{}
	for _, system := range a.hostSettings.Systems {
		id := strings.TrimSpace(system.ID)
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

func (a *App) settingsSystemChoicesLocked(current string) []string {
	ids := a.settingsSystemIDsLocked()
	current = strings.TrimSpace(current)
	if current != "" && indexOfString(ids, current) < 0 {
		ids = append([]string{current}, ids...)
	}
	return ids
}

func (a *App) settingsSystemLabelLocked(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "(system)"
	}
	for _, system := range a.hostSettings.Systems {
		if strings.TrimSpace(system.ID) != id {
			continue
		}
		if label := strings.TrimSpace(system.Label); label != "" {
			return label
		}
		break
	}
	return id
}

func cloneLibraryRoots(in []hostclient.LibraryRoot) []hostclient.LibraryRoot {
	if len(in) == 0 {
		return []hostclient.LibraryRoot{}
	}
	return append([]hostclient.LibraryRoot(nil), in...)
}

func cloneSettingsTargetsFromHost(in []hostclient.LibraryTarget) []settingsTargetDraft {
	out := make([]settingsTargetDraft, 0, len(in))
	for _, target := range in {
		name := strings.TrimSpace(target.Name)
		out = append(out, settingsTargetDraft{
			OriginalName:    name,
			Name:            name,
			Address:         strings.TrimSpace(target.Address),
			Enabled:         target.Enabled,
			AgentConfigured: target.AgentConfigured,
		})
	}
	return out
}

func settingsTargetsMatchHost(drafts []settingsTargetDraft, host []hostclient.LibraryTarget) bool {
	if len(drafts) != len(host) {
		return false
	}
	for i, draft := range drafts {
		if draft.AgentDirty || draft.AgentClear {
			return false
		}
		if strings.TrimSpace(draft.Name) != strings.TrimSpace(host[i].Name) {
			return false
		}
		if strings.TrimSpace(draft.Address) != strings.TrimSpace(host[i].Address) {
			return false
		}
		if draft.Enabled != host[i].Enabled || draft.AgentConfigured != host[i].AgentConfigured {
			return false
		}
	}
	return true
}

func settingsStatusError(err error) string {
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		return "settings failed"
	}
	if strings.HasPrefix(msg, "host API ") {
		if i := strings.Index(msg, ": "); i >= 0 && i+2 < len(msg) {
			msg = strings.TrimSpace(msg[i+2:])
		}
	}
	if len(msg) > 48 {
		msg = msg[:48]
	}
	return msg
}

func togglePreferredRegion(current []string, region string) ([]string, bool) {
	region = strings.ToLower(strings.TrimSpace(region))
	out := make([]string, 0, len(current)+1)
	found := false
	for _, item := range current {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		if item == region {
			found = true
			continue
		}
		out = append(out, item)
	}
	if found {
		if len(out) == 0 {
			return append([]string(nil), current...), false
		}
		return out, true
	}
	return append(out, region), true
}

func indexOfString(items []string, want string) int {
	want = strings.TrimSpace(want)
	for i, item := range items {
		if item == want {
			return i
		}
	}
	return -1
}

func intPtr(v int) *int       { return &v }
func strPtr(v string) *string { return &v }
func boolPtr(v bool) *bool    { return &v }

func prefsAttractEnabled(prefs tenfootPrefs) bool {
	if prefs.AttractEnabled == nil {
		return true
	}
	return *prefs.AttractEnabled
}

func (a *App) settingsHomeValueLocked() string {
	if !a.roomsAvailableLocked() {
		return "Library (no rooms installed)"
	}
	if a.homeRooms {
		return fmt.Sprintf("Rooms (%d)", a.publicRoomCountLocked())
	}
	return "Library"
}

func (a *App) toggleHomePrefLocked() {
	if !a.roomsAvailableLocked() {
		a.settingsStatus = "no rooms installed"
		if dir := strings.TrimSpace(a.roomsDir); dir != "" {
			a.settingsStatus = "no rooms in " + dir
		}
		a.status = a.settingsStatus
		return
	}
	a.homeRooms = !a.homeRooms
	if a.homeRooms {
		a.homeRoom = menuDisplayInstalledHome(a.gfxBackend, homePrefValue(true), a.homeRoom)
	}
	a.persistPrefsLocked("home")
	a.status = "home: " + homePrefValue(a.homeRooms)
}

func (a *App) toggleReducedMotionLocked() {
	a.setReducedMotionLocked(!a.reducedMotion, true)
}

func (a *App) setReducedMotionLocked(on bool, persist bool) {
	a.reducedMotion = on
	if persist {
		a.persistPrefsLocked("reduced-motion")
	}
	if a.room != nil {
		a.room.SetReducedMotion(on)
	}
	for _, parent := range a.roomStack {
		if parent != nil {
			parent.SetReducedMotion(on)
		}
	}
	a.setSuspendedReducedMotionLocked(on)
	if on {
		a.status = "reduced motion on"
		return
	}
	a.status = "reduced motion off"
}

// setSurroundLocked changes the TV surround drawn around a running machine
// on kit HDMI. A running session follows it once the machine has input.
func (a *App) setSurroundLocked(on bool, persist bool) {
	a.surround = on
	if !persist {
		return
	}
	a.persistPrefsLocked("surround")
	if on {
		a.status = "TV surround on"
		return
	}
	a.status = "TV surround off"
}

// setAllCoresLocked lists or hides cores outside the v1 release. The core
// catalog view and Home recents follow it at once. A room keeps the list it
// was opened with, so a change from Settings rebuilds Home when Settings closes.
func (a *App) setAllCoresLocked(on bool, persist bool) {
	a.allCores = on
	if !persist {
		return
	}
	a.persistPrefsLocked("all-cores")
	status := "v1 cores only"
	if on {
		status = "all cores shown"
	}
	if a.settingsOpen && !a.sessionStopOfferedLocked() && a.launch.Phase != "launching" {
		a.settingsScopeChanged = true
	}
	a.status = status
}

func (a *App) SetAllCores(on bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.setAllCoresLocked(on, false)
}

func (a *App) SetSurround(on bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.setSurroundLocked(on, false)
}

func (a *App) SetReducedMotion(on bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.setReducedMotionLocked(on, false)
}

func (a *App) ReducedMotion() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.reducedMotion
}
