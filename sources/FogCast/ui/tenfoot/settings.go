package tenfoot

import (
	"context"
	"fmt"
	"strings"

	"github.com/DeanoC/FogCast/hostclient"
)

// Settings sections, in the v1 order (docs/ux/references/v1/README.md,
// Settings map). Settings opens on the section list; a section is listed
// only when it has rows, and Controllers only while a pad is attached. Library &
// rooms holds the rows outside v1 and is listed with Show all cores.
const (
	settingsSectionList        = ""
	settingsSectionFolders     = "folders"
	settingsSectionFirmware    = "firmware"
	settingsSectionDisplay     = "display"
	settingsSectionControllers = "controllers"
	settingsSectionBluetooth   = "bluetooth"
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
	settingsSectionControllers: "Controllers",
	settingsSectionBluetooth:   "Bluetooth",
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
	settingsOSKShelfName
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
		prefs, err := a.readPreferencesLocked()
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
	a.closeControllersLocked()
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
	if a.controllers.open {
		a.handleControllersLocked(cmd)
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
	// Controllers is a page of its own, not a list of rows.
	if id == settingsSectionControllers {
		a.openControllersLocked()
		return
	}
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
	case "bt-pair":
		if cmd == CmdSelect {
			a.toggleSettingsBluetoothPairingLocked()
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
		} else if addr, ok := parseSettingsBluetoothDeviceRowID(id); ok && cmd == CmdSelect {
			a.handleSettingsBluetoothDeviceLocked(id, addr)
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
	a.displaySurroundOverride = nil
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

// setAllCoresLocked lists or hides held-back cores. The core
// catalog view and Home recents follow it at once. A room keeps the list it
// was opened with, so a change from Settings rebuilds Home when Settings closes.
func (a *App) setAllCoresLocked(on bool, persist bool) {
	a.allCores = on
	if !persist {
		return
	}
	a.persistPrefsLocked("all-cores")
	status := "supported and PC cores shown"
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
