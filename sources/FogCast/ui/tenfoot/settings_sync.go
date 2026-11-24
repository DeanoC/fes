package tenfoot

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
)

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

func intPtr(v int) *int       { return &v }
func strPtr(v string) *string { return &v }
func boolPtr(v bool) *bool    { return &v }
