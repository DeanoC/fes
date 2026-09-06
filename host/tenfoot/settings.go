package tenfoot

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

const (
	settingsRowLayout = iota
	settingsRowSafeArea
	settingsRowAttract
	settingsRowIdle
	settingsRowRegions
	settingsRowTarget
	settingsRowFixedCount
)

const (
	settingsRowAddLibrary = iota
	settingsRowSaveLibraries
	settingsRowClose
	settingsTrailingCount
)

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
	Open         bool
	Index        int
	Rows         []SettingsRow
	Loading      bool
	Busy         bool
	Status       string
	Hint         string
	LibraryCount int
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
		Index:        a.settingsIndex,
		Rows:         rows,
		Loading:      a.settingsLoading,
		Busy:         a.settingsBusy,
		Status:       a.settingsStatus,
		Hint:         a.settingsHintLocked(),
		LibraryCount: len(a.settingsDraftLibraries),
		SystemCount:  len(a.hostSettings.Systems),
	}
}

func (a *App) settingsRowsLocked() []SettingsRow {
	attract := "On"
	switch {
	case a.attractForcedOff && a.attractPrefEnabled:
		attract = "Off (flag)"
	case !a.attractPrefEnabled || a.attractDisabled:
		attract = "Off"
	}
	idle := "—"
	if a.settingsHydrated {
		idle = fmt.Sprintf("%ds", a.settingsDraftIdle)
	}
	regions := "—"
	if a.settingsHydrated {
		regions = formatSettingsRegions(a.settingsDraftRegions, a.settingsRegionIndex)
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
	rows := []SettingsRow{
		{ID: "layout", Label: "Layout", Value: a.grid.Mode.Label()},
		{ID: "safe-area", Label: "Safe area", Value: fmt.Sprintf("%.1f%%", a.safeAreaPct*100)},
		{ID: "attract", Label: "Attract", Value: attract},
		{ID: "idle", Label: "Idle", Value: idle},
		{ID: "regions", Label: "Regions", Value: regions},
		{ID: "target", Label: "Target", Value: target},
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
	rows = append(rows,
		SettingsRow{ID: "add-library", Label: "Add library", Value: "A add"},
		SettingsRow{ID: "save-libraries", Label: "Save libraries", Value: save},
		SettingsRow{ID: "close", Label: "Close", Value: "B back"},
	)
	return rows
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

func (a *App) openSettingsLocked() {
	if a.gpuParked || a.session.State == "active" || a.session.State == "launching" || a.stopPhase == "stopping" {
		return
	}
	a.searchOpen = false
	a.closeCollectionOverlaysLocked()
	a.closeFiltersLocked()
	a.closeDetailLocked()
	a.settingsOpen = true
	a.settingsIndex = 0
	a.settingsStatus = ""
	a.settingsBusy = false
	a.settingsLoading = true
	a.settingsHydrated = false
	a.settingsRegionIndex = 0
	a.closeLibraryPathOSKLocked()
	a.settingsGen++
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
	a.settingsOpen = false
	a.settingsBusy = false
	a.settingsLoading = false
	a.settingsStatus = ""
	a.closeLibraryPathOSKLocked()
	a.settingsGen++
	a.discardSettingsDraftsLocked()
}

func (a *App) discardSettingsDraftsLocked() {
	a.settingsLibrariesDirty = false
	a.closeLibraryPathOSKLocked()
	if !a.settingsHydrated {
		a.settingsDraftIdle = 0
		a.settingsDraftRegions = nil
		a.settingsDraftTarget = ""
		a.settingsDraftLibraries = nil
		return
	}
	a.settingsDraftIdle = a.hostSettings.AttractIdleSeconds
	a.settingsDraftRegions = append([]string(nil), a.hostSettings.PreferredRegions...)
	a.settingsDraftTarget = a.hostSettings.SelectedTarget
	a.settingsDraftLibraries = cloneLibraryRoots(a.hostSettings.Libraries)
}

func (a *App) revertSettingsPatchDraftsLocked(patch LibrarySettingsPatch) {
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
	if patch.Libraries != nil {
		a.settingsLibrariesDirty = false
		a.settingsDraftLibraries = cloneLibraryRoots(a.hostSettings.Libraries)
		a.closeLibraryPathOSKLocked()
	}
}

func (a *App) handleSettingsLocked(cmd Command) {
	if !a.settingsOpen {
		return
	}
	rows := a.settingsRowCountLocked()
	if rows <= 0 {
		rows = 1
	}
	switch cmd {
	case CmdSettings:
		a.closeSettingsLocked()
		return
	case CmdBack:
		a.closeSettingsLocked()
		return
	case CmdUp:
		a.settingsIndex = (a.settingsIndex - 1 + rows) % rows
		return
	case CmdDown:
		a.settingsIndex = (a.settingsIndex + 1) % rows
		return
	}
	if a.settingsBusy {
		switch a.settingsIndex {
		case settingsRowIdle, settingsRowRegions, settingsRowTarget:
			return
		}
		if a.settingsIndex >= settingsRowFixedCount {
			kind, _ := a.settingsLibraryRowLocked()
			if kind != settingsRowClose {
				return
			}
		}
	}
	switch a.settingsIndex {
	case settingsRowLayout:
		switch cmd {
		case CmdLeft:
			a.setLayoutLocked(prevLayout(a.grid.Mode), true)
			a.status = "layout " + a.grid.Mode.Label()
		case CmdRight, CmdSelect:
			a.cycleLayoutLocked()
		}
	case settingsRowSafeArea:
		switch cmd {
		case CmdLeft, CmdSafeAreaOut:
			a.setSafeAreaPctLocked(a.safeAreaPct-safeAreaNudge, true)
			a.status = fmt.Sprintf("safe-area %.1f%%", a.safeAreaPct*100)
		case CmdRight, CmdSelect, CmdSafeAreaIn:
			a.setSafeAreaPctLocked(a.safeAreaPct+safeAreaNudge, true)
			a.status = fmt.Sprintf("safe-area %.1f%%", a.safeAreaPct*100)
		}
	case settingsRowAttract:
		switch cmd {
		case CmdLeft, CmdRight, CmdSelect:
			a.toggleAttractPrefLocked()
		}
	case settingsRowIdle:
		if !a.settingsHydrated {
			return
		}
		switch cmd {
		case CmdLeft:
			a.settingsDraftIdle = clampSettingsIdle(a.settingsDraftIdle - settingsIdleStep)
		case CmdRight:
			a.settingsDraftIdle = clampSettingsIdle(a.settingsDraftIdle + settingsIdleStep)
		case CmdSelect:
			a.patchSettingsLocked(LibrarySettingsPatch{AttractIdleSeconds: intPtr(a.settingsDraftIdle)})
		}
	case settingsRowRegions:
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
			a.patchSettingsLocked(LibrarySettingsPatch{PreferredRegions: &regions})
		}
	case settingsRowTarget:
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
			a.patchSettingsLocked(LibrarySettingsPatch{SelectedTarget: strPtr(name)})
		}
	default:
		a.handleSettingsLibraryRowsLocked(cmd)
	}
}

func (a *App) settingsRowCountLocked() int {
	return settingsRowFixedCount + len(a.settingsDraftLibraries) + settingsTrailingCount
}

func (a *App) settingsLibraryRowLocked() (int, int) {
	rel := a.settingsIndex - settingsRowFixedCount
	libCount := len(a.settingsDraftLibraries)
	if rel < 0 {
		return -1, -1
	}
	if rel < libCount {
		return -1, rel
	}
	return rel - libCount, -1
}

func (a *App) clampSettingsIndexLocked() {
	n := a.settingsRowCountLocked()
	if n <= 0 {
		a.settingsIndex = 0
		return
	}
	if a.settingsIndex < 0 {
		a.settingsIndex = 0
	}
	if a.settingsIndex >= n {
		a.settingsIndex = n - 1
	}
}

func (a *App) handleSettingsLibraryRowsLocked(cmd Command) {
	kind, libIndex := a.settingsLibraryRowLocked()
	switch {
	case libIndex >= 0:
		a.handleSettingsLibraryEntryLocked(cmd, libIndex)
	case kind == settingsRowAddLibrary:
		if cmd == CmdSelect {
			a.addSettingsLibraryLocked()
		}
	case kind == settingsRowSaveLibraries:
		if cmd == CmdSelect {
			a.saveSettingsLibrariesLocked()
		}
	case kind == settingsRowClose:
		if cmd == CmdSelect {
			a.closeSettingsLocked()
		}
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
	a.settingsDraftLibraries = append(a.settingsDraftLibraries, LibraryRoot{System: ids[0]})
	a.settingsIndex = settingsRowFixedCount + len(a.settingsDraftLibraries) - 1
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
	a.patchSettingsLocked(LibrarySettingsPatch{Libraries: &libraries})
}

func (a *App) openLibraryPathOSKLocked(index int, isAdd bool) {
	if !a.settingsHydrated || index < 0 || index >= len(a.settingsDraftLibraries) {
		return
	}
	a.settingsPathOpen = true
	a.settingsPathIndex = index
	a.settingsPathIsAdd = isAdd
	a.settingsPathField = TextField{Buffer: a.settingsDraftLibraries[index].Root}
	a.settingsPathField.OSK.Reset()
	a.settingsPathField.OSK.CyclePage(1)
}

func (a *App) closeLibraryPathOSKLocked() {
	a.settingsPathOpen = false
	a.settingsPathIndex = 0
	a.settingsPathIsAdd = false
	a.settingsPathField = TextField{}
}

func (a *App) handleLibraryPathOSKLocked(cmd Command) {
	switch cmd {
	case CmdUp:
		a.settingsPathField.Move(0, -1)
	case CmdDown:
		a.settingsPathField.Move(0, 1)
	case CmdLeft:
		a.settingsPathField.Move(-1, 0)
	case CmdRight:
		a.settingsPathField.Move(1, 0)
	case CmdSelect:
		result := a.settingsPathField.Activate()
		if result.Done {
			a.submitLibraryPathOSKLocked()
		}
	case CmdBack:
		if strings.TrimSpace(a.settingsPathField.Buffer) != "" {
			a.settingsPathField.Clear()
			return
		}
		a.cancelLibraryPathOSKLocked()
	case CmdSearch:
		a.cancelLibraryPathOSKLocked()
	case CmdFilterPrev:
		a.settingsPathField.CyclePage(-1)
	case CmdFilterNext:
		a.settingsPathField.CyclePage(1)
	}
}

func (a *App) submitLibraryPathOSKLocked() {
	index := a.settingsPathIndex
	if index < 0 || index >= len(a.settingsDraftLibraries) {
		a.closeLibraryPathOSKLocked()
		return
	}
	path := strings.TrimSpace(a.settingsPathField.Buffer)
	if path == "" {
		a.settingsStatus = "library path is required"
		a.status = a.settingsStatus
		return
	}
	a.settingsDraftLibraries[index].Root = path
	a.settingsLibrariesDirty = true
	a.closeLibraryPathOSKLocked()
	a.settingsStatus = ""
}

func (a *App) cancelLibraryPathOSKLocked() {
	index := a.settingsPathIndex
	isAdd := a.settingsPathIsAdd
	a.closeLibraryPathOSKLocked()
	if !isAdd || index < 0 || index >= len(a.settingsDraftLibraries) {
		return
	}
	if strings.TrimSpace(a.settingsDraftLibraries[index].Root) != "" {
		return
	}
	a.settingsDraftLibraries = append(a.settingsDraftLibraries[:index], a.settingsDraftLibraries[index+1:]...)
	a.clampSettingsIndexLocked()
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
	names := make([]string, 0, len(a.hostSettings.Targets)+1)
	seen := map[string]struct{}{}
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

func (a *App) refreshSettingsDraftsFromAppliedLocked(prev LibrarySettings, seq int) {
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

func (a *App) settingsDraftsMatchLocked(settings LibrarySettings) bool {
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
	if !slices.Equal(a.settingsDraftLibraries, settings.Libraries) {
		return false
	}
	return slices.Equal(a.settingsDraftRegions, settings.PreferredRegions)
}

func (a *App) applyHostSettingsLocked(settings LibrarySettings) {
	a.hostSettings = settings
	a.settingsDraftIdle = settings.AttractIdleSeconds
	if a.settingsDraftIdle <= 0 {
		a.settingsDraftIdle = defaultAttractIdleSeconds
	}
	a.settingsDraftRegions = append([]string(nil), settings.PreferredRegions...)
	a.settingsDraftTarget = strings.TrimSpace(settings.SelectedTarget)
	if !a.settingsLibrariesDirty {
		a.settingsDraftLibraries = cloneLibraryRoots(settings.Libraries)
	}
	a.settingsHydrated = true
}

func (a *App) patchSettingsLocked(patch LibrarySettingsPatch) {
	if a.settingsBusy {
		return
	}
	if _, err := patch.payload(); err != nil {
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

func (a *App) commitLibrarySettings(ctx context.Context, gen, seq int, patch LibrarySettingsPatch) {
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

func (a *App) settingsSavedStatusLocked(patch LibrarySettingsPatch) string {
	switch {
	case patch.AttractIdleSeconds != nil:
		return fmt.Sprintf("idle %ds", a.hostSettings.AttractIdleSeconds)
	case patch.PreferredRegions != nil:
		return "regions " + strings.Join(a.hostSettings.PreferredRegions, ", ")
	case patch.SelectedTarget != nil:
		return "target " + a.hostSettings.SelectedTarget
	case patch.Libraries != nil:
		return fmt.Sprintf("libraries %d", len(a.hostSettings.Libraries))
	default:
		return "settings saved"
	}
}

func (a *App) settingsHintLocked() string {
	if a.settingsPathOpen {
		return ""
	}
	kind, libIndex := a.settingsLibraryRowLocked()
	if libIndex >= 0 {
		return "A path  X remove  Left/Right system  B close"
	}
	switch kind {
	case settingsRowAddLibrary:
		return "A add library  B close"
	case settingsRowSaveLibraries:
		return "A save libraries  B close"
	default:
		return "A confirm  B close  Left/Right change"
	}
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

func cloneLibraryRoots(in []LibraryRoot) []LibraryRoot {
	if len(in) == 0 {
		return []LibraryRoot{}
	}
	return append([]LibraryRoot(nil), in...)
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
