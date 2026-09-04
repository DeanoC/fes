package tenfoot

import (
	"context"
	"fmt"
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
	settingsRowClose
	settingsRowCount
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
	return SettingsSnapshot{
		Open:         true,
		Index:        a.settingsIndex,
		Rows:         a.settingsRowsLocked(),
		Loading:      a.settingsLoading,
		Busy:         a.settingsBusy,
		Status:       a.settingsStatus,
		LibraryCount: len(a.hostSettings.Libraries),
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
	return []SettingsRow{
		{ID: "layout", Label: "Layout", Value: a.grid.Mode.Label()},
		{ID: "safe-area", Label: "Safe area", Value: fmt.Sprintf("%.1f%%", a.safeAreaPct*100)},
		{ID: "attract", Label: "Attract", Value: attract},
		{ID: "idle", Label: "Idle", Value: idle},
		{ID: "regions", Label: "Regions", Value: regions},
		{ID: "target", Label: "Target", Value: target},
		{ID: "close", Label: "Close", Value: "B back"},
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

func (a *App) openSettingsLocked() {
	if a.gpuParked || a.session.State == "active" || a.session.State == "launching" || a.stopPhase == "stopping" {
		return
	}
	a.searchOpen = false
	a.closeCollectionOverlaysLocked()
	a.settingsOpen = true
	a.settingsIndex = 0
	a.settingsStatus = ""
	a.settingsBusy = false
	a.settingsLoading = true
	a.settingsHydrated = false
	a.settingsRegionIndex = 0
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
	a.settingsGen++
	a.discardSettingsDraftsLocked()
}

func (a *App) discardSettingsDraftsLocked() {
	if !a.settingsHydrated {
		a.settingsDraftIdle = 0
		a.settingsDraftRegions = nil
		a.settingsDraftTarget = ""
		return
	}
	a.settingsDraftIdle = a.hostSettings.AttractIdleSeconds
	a.settingsDraftRegions = append([]string(nil), a.hostSettings.PreferredRegions...)
	a.settingsDraftTarget = a.hostSettings.SelectedTarget
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
		a.closeSettingsLocked()
		return
	case CmdUp:
		a.settingsIndex = (a.settingsIndex - 1 + settingsRowCount) % settingsRowCount
		return
	case CmdDown:
		a.settingsIndex = (a.settingsIndex + 1) % settingsRowCount
		return
	}
	if a.settingsBusy {
		switch a.settingsIndex {
		case settingsRowIdle, settingsRowRegions, settingsRowTarget:
			return
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
	case settingsRowClose:
		if cmd == CmdSelect {
			a.closeSettingsLocked()
		}
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

func (a *App) applyHostSettingsLocked(settings LibrarySettings) {
	a.hostSettings = settings
	a.settingsDraftIdle = settings.AttractIdleSeconds
	if a.settingsDraftIdle <= 0 {
		a.settingsDraftIdle = defaultAttractIdleSeconds
	}
	a.settingsDraftRegions = append([]string(nil), settings.PreferredRegions...)
	a.settingsDraftTarget = strings.TrimSpace(settings.SelectedTarget)
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
		a.discardSettingsDraftsLocked()
		return
	}
	if seq > a.settingsAppliedSeq {
		a.settingsAppliedSeq = seq
		a.hostSettings = settings
		if patch.AttractIdleSeconds != nil {
			a.applyAttractIdleFromSettingsLocked(settings.AttractIdleSeconds)
		}
		if patch.PreferredRegions != nil {
			a.reloadLocked()
		}
		a.settingsWriteGen++
		a.status = a.settingsSavedStatusLocked(patch)
		if !current {
			a.settingsLoading = false
			a.hydrateSettingsFromAppliedLocked()
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
	default:
		return "settings saved"
	}
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
