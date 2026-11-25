package tenfoot

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
)

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
	hint := a.settingsHintLocked()
	if store, ok := a.preferenceStore.(PreferenceStatus); ok {
		state := store.PreferenceSaveState()
		if state.Failed {
			hint = "Settings not saved · " + hint
		} else if state.Pending {
			hint = "Settings waiting to save · " + hint
		}
	}
	if a.settingsSection == settingsSectionBluetooth && a.bluetooth != nil {
		st := a.bluetoothStatusLocked()
		if st.BondsSaveFailed {
			hint = "Bluetooth bonds not saved · " + hint
		} else if st.BondsPending {
			hint = "Bluetooth bonds waiting to save · " + hint
		}
	}
	return SettingsSnapshot{
		Open:         true,
		Title:        a.settingsTitleLocked(),
		Index:        a.settingsIndex,
		Rows:         rows,
		Loading:      a.settingsLoading,
		Busy:         a.settingsBusy,
		Status:       a.settingsStatus,
		Hint:         hint,
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
	case settingsSectionBluetooth:
		return a.settingsBluetoothRowsLocked()
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
	if len(a.pads) > 0 {
		ids = append(ids, settingsSectionControllers)
	}
	if a.bluetooth != nil {
		ids = append(ids, settingsSectionBluetooth)
	}
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
	case settingsSectionControllers:
		ids := a.padIDsLocked()
		if len(ids) == 1 {
			return a.pads[ids[0]].Name
		}
		return fmt.Sprintf("%d pads", len(ids))
	case settingsSectionBluetooth:
		return a.settingsBluetoothSummaryLocked()
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
	if _, ok := parseSettingsBluetoothDeviceRowID(id); ok {
		if a.settingsConfirm == id {
			return settingsActionHint(kind, "forget controller", "Up/Down cancel", leave)
		}
		return settingsActionHint(kind, "forget", "", leave)
	}
	switch id {
	case "home":
		return settingsActionHint(kind, "go home", "Left/Right start screen", leave)
	case "reduced-motion", "surround", "all-cores":
		return settingsActionHint(kind, "toggle", "Left/Right", leave)
	case "resolution", "versions", "kit-id", "wired", "bt-adapter", "bt-none":
		return backWord(kind) + " " + leave
	case "bt-pair":
		st := a.bluetoothStatusLocked()
		if st.PairingActive {
			return settingsActionHint(kind, "stop pairing", "", leave)
		}
		if st.DongleConnected && a.settingsConfirm == "bt-pair" {
			return settingsActionHint(kind, "confirm pairing", "Up/Down cancel", leave)
		}
		if st.DongleConnected {
			return settingsActionHint(kind, "pair controller", "", leave)
		}
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
