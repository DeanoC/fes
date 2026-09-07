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
	settingsRowPrepareTarget
	settingsRowFixedCount
)

const (
	settingsTargetFieldName = iota
	settingsTargetFieldAddress
	settingsTargetFieldEnabled
	settingsTargetFieldAgent
	settingsTargetFieldCount
)

const (
	settingsRowAddTarget = iota
	settingsRowSaveTargets
	settingsTargetTrailingCount
)

const (
	settingsRowAddLibrary = iota
	settingsRowSaveLibraries
	settingsRowDevelopmentRBF
	settingsRowClose
	settingsTrailingCount
)

type settingsOSKKind int

const (
	settingsOSKNone settingsOSKKind = iota
	settingsOSKLibraryPath
	settingsOSKTargetName
	settingsOSKTargetAddress
	settingsOSKTargetAgent
	settingsOSKDevelopmentPath
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
	Open         bool
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
		{ID: "prepare-target", Label: "Prepare target", Value: "A generate identity"},
	}
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
			SettingsRow{ID: fmt.Sprintf("target-%d-name", i), Label: label + " · name", Value: name},
			SettingsRow{ID: fmt.Sprintf("target-%d-address", i), Label: label + " · address", Value: address},
			SettingsRow{ID: fmt.Sprintf("target-%d-enabled", i), Label: label + " · enabled", Value: enabled},
			SettingsRow{ID: fmt.Sprintf("target-%d-agent", i), Label: label + " · agent", Value: settingsAgentValue(draft)},
		)
	}
	saveTargets := "saved"
	if a.settingsTargetsDirty {
		saveTargets = "A save"
	}
	rows = append(rows,
		SettingsRow{ID: "add-target", Label: "Add target", Value: "A add"},
		SettingsRow{ID: "save-targets", Label: "Save targets", Value: saveTargets},
	)
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
	devValue := "A type path · not a game"
	if path := strings.TrimSpace(a.developmentRBFPath); path != "" {
		devValue = path
	}
	rows = append(rows,
		SettingsRow{ID: "add-library", Label: "Add library", Value: "A add"},
		SettingsRow{ID: "save-libraries", Label: "Save libraries", Value: save},
		SettingsRow{ID: "development-rbf", Label: "DIAGNOSTIC RBF", Value: devValue},
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
	if a.gpuParked || a.session.State == "active" || a.session.State == "launching" || a.stopPhase == "stopping" || a.developmentLoadingLocked() {
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
	a.closeSettingsOSKLocked()
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
	a.closeSettingsOSKLocked()
	a.settingsGen++
	a.discardSettingsDraftsLocked()
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
		case settingsRowIdle, settingsRowRegions, settingsRowTarget, settingsRowPrepareTarget:
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
			a.patchSettingsLocked(LibrarySettingsPatch{SelectedTarget: strPtr(name)})
		}
	case settingsRowPrepareTarget:
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
			a.patchSettingsLocked(LibrarySettingsPatch{PrepareTarget: strPtr(name)})
		}
	default:
		if a.settingsIndex < a.settingsLibraryStartLocked() {
			a.handleSettingsTargetRowsLocked(cmd)
			return
		}
		a.handleSettingsLibraryRowsLocked(cmd)
	}
}

func (a *App) settingsTargetBlockCountLocked() int {
	return len(a.settingsDraftTargets)*settingsTargetFieldCount + settingsTargetTrailingCount
}

func (a *App) settingsLibraryStartLocked() int {
	return settingsRowFixedCount + a.settingsTargetBlockCountLocked()
}

func (a *App) settingsRowCountLocked() int {
	return a.settingsLibraryStartLocked() + len(a.settingsDraftLibraries) + settingsTrailingCount
}

func (a *App) settingsTargetRowLocked() (int, int, int) {
	rel := a.settingsIndex - settingsRowFixedCount
	count := len(a.settingsDraftTargets)
	if rel < 0 {
		return -1, -1, -1
	}
	fields := count * settingsTargetFieldCount
	if rel < fields {
		return -1, rel / settingsTargetFieldCount, rel % settingsTargetFieldCount
	}
	return rel - fields, -1, -1
}

func (a *App) settingsLibraryRowLocked() (int, int) {
	rel := a.settingsIndex - a.settingsLibraryStartLocked()
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

func (a *App) handleSettingsTargetRowsLocked(cmd Command) {
	kind, targetIndex, field := a.settingsTargetRowLocked()
	switch {
	case targetIndex >= 0:
		a.handleSettingsTargetEntryLocked(cmd, targetIndex, field)
	case kind == settingsRowAddTarget:
		if cmd == CmdSelect {
			a.addSettingsTargetLocked()
		}
	case kind == settingsRowSaveTargets:
		if cmd == CmdSelect {
			a.saveSettingsTargetsLocked()
		}
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
	a.settingsIndex = settingsRowFixedCount + (len(a.settingsDraftTargets)-1)*settingsTargetFieldCount
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
	writes := make([]LibraryTargetWrite, 0, len(a.settingsDraftTargets))
	for _, draft := range a.settingsDraftTargets {
		writes = append(writes, draft.write())
	}
	patch := LibrarySettingsPatch{Targets: &writes}
	if a.settingsNeedSelectedPatchLocked() {
		name := strings.TrimSpace(a.settingsDraftTarget)
		patch.SelectedTarget = strPtr(name)
	}
	a.patchSettingsLocked(patch)
}

func (d settingsTargetDraft) write() LibraryTargetWrite {
	out := LibraryTargetWrite{
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
	var host LibraryTarget
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
	case kind == settingsRowDevelopmentRBF:
		if cmd == CmdSelect {
			a.openDevelopmentPathOSKLocked()
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
	a.settingsIndex = a.settingsLibraryStartLocked() + len(a.settingsDraftLibraries) - 1
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
	a.settingsOSKField = TextField{Buffer: a.settingsDraftLibraries[index].Root}
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
	a.settingsOSKField = TextField{Buffer: buffer}
	a.settingsOSKField.OSK.Reset()
	if kind == settingsOSKTargetAddress {
		a.settingsOSKField.OSK.CyclePage(1)
	}
}

func (a *App) closeSettingsOSKLocked() {
	a.settingsOSKKind = settingsOSKNone
	a.settingsOSKIndex = 0
	a.settingsOSKIsAdd = false
	a.settingsOSKField = TextField{}
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
	case CmdFilterPrev:
		a.settingsOSKField.CyclePage(-1)
	case CmdFilterNext:
		a.settingsOSKField.CyclePage(1)
	}
}

func (a *App) submitSettingsOSKLocked() {
	switch a.settingsOSKKind {
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
	if a.settingsTargetsDirty || !settingsTargetsMatchHost(a.settingsDraftTargets, settings.Targets) {
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
	if !a.settingsTargetsDirty {
		a.settingsDraftTargets = cloneSettingsTargetsFromHost(settings.Targets)
	}
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

func (a *App) settingsSavedStatusLocked(patch LibrarySettingsPatch) string {
	switch {
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
	targetKind, targetIndex, field := a.settingsTargetRowLocked()
	if targetIndex >= 0 {
		switch field {
		case settingsTargetFieldName:
			return "A name  X remove  B close"
		case settingsTargetFieldAddress:
			return "A address  X remove  B close"
		case settingsTargetFieldEnabled:
			return "A/Left/Right enabled  X remove  B close"
		case settingsTargetFieldAgent:
			return "A set agent  Left/Right clear  X remove  B close"
		}
	}
	switch targetKind {
	case settingsRowAddTarget:
		return "A add target  B close"
	case settingsRowSaveTargets:
		return "A save targets  B close"
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
	case settingsRowDevelopmentRBF:
		return "A type path  B close · not a game session · HDMI/input may be down"
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

func cloneSettingsTargetsFromHost(in []LibraryTarget) []settingsTargetDraft {
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

func settingsTargetsMatchHost(drafts []settingsTargetDraft, host []LibraryTarget) bool {
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
