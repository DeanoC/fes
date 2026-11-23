package tenfoot

import (
	"fmt"
	"strings"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/shared"
)

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
	case settingsOSKShelfName:
		a.submitDiskShelfNameLocked()
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
