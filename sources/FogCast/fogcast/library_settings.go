package fogcast

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/DeanoC/FogCast/protocol"
)

func (s *Service) AttractIdleSeconds() int {
	return s.LibrarySettings().AttractIdleSeconds
}

func (s *Service) LibrarySettings() LibraryConfig {
	s.libraryMu.RLock()
	defer s.libraryMu.RUnlock()
	return s.librarySettingsSnapshot()
}

func (s *Service) librarySettingsSnapshot() LibraryConfig {
	seconds := s.attractIdle
	if seconds <= 0 {
		seconds = 60
	}
	regions := append([]string(nil), s.preferredRegions...)
	if len(regions) == 0 {
		regions = append([]string(nil), catalog.DefaultPreferredRegions...)
	}
	return LibraryConfig{
		AttractIdleSeconds: seconds,
		PreferredRegions:   regions,
		VideoProfile:       defaultVideoProfile(s.videoProfile),
		Libraries:          append([]catalog.Root(nil), s.roots...),
		Targets:            append([]TargetConfig(nil), s.targets...),
		SelectedTarget:     s.selectedTarget,
		WatchRoot:          s.watchRoot,
	}
}

func (s *Service) currentPreferredRegions() []string {
	return append([]string(nil), s.LibrarySettings().PreferredRegions...)
}

func (s *Service) applyPersistedLibraryOverlay() {
	overlay, ok, err := loadLibraryOverlay(s.libraryOverlayPath)
	if err != nil || !ok {
		return
	}
	s.attractIdle = overlay.AttractIdleSeconds
	s.preferredRegions = append([]string(nil), overlay.PreferredRegions...)
	s.videoProfile = overlay.VideoProfile
}

func (s *Service) SetLibrarySettings(ctx context.Context, next LibraryConfig) error {
	s.cancelTargetLookup()
	if err := ctx.Err(); err != nil {
		return err
	}
	releaseSettings, err := s.acquireLibrarySettings(ctx)
	if err != nil {
		return err
	}
	defer releaseSettings()
	if err := ctx.Err(); err != nil {
		return err
	}
	s.targetMu.Lock()
	s.libraryMu.Lock()
	rootsChanged, err := s.setLibrarySettingsLocked(next)
	hook := s.settingsTargetHook
	s.settingsTargetHook = nil
	s.libraryMu.Unlock()
	s.targetMu.Unlock()
	if hook != nil {
		hook()
	}
	if err != nil {
		return err
	}
	if rootsChanged {
		_, err = s.scanLocked(ctx)
	}
	return err
}

var librarySettingsPatchStartHook func()

func (s *Service) PatchLibrarySettings(ctx context.Context, patch LibraryConfigPatch) error {
	s.cancelTargetLookup()
	if err := ctx.Err(); err != nil {
		return err
	}
	if librarySettingsPatchStartHook != nil {
		librarySettingsPatchStartHook()
	}
	releaseSettings, err := s.acquireLibrarySettings(ctx)
	if err != nil {
		return err
	}
	defer releaseSettings()
	if err := ctx.Err(); err != nil {
		return err
	}
	s.targetMu.Lock()
	s.libraryMu.Lock()
	next := s.librarySettingsSnapshot()
	if patch.AttractIdleSeconds != nil {
		next.AttractIdleSeconds = *patch.AttractIdleSeconds
	}
	if patch.PreferredRegions != nil {
		next.PreferredRegions = append([]string(nil), *patch.PreferredRegions...)
	}
	if patch.VideoProfile != nil {
		next.VideoProfile = *patch.VideoProfile
	}
	if patch.Libraries != nil {
		next.Libraries = append([]catalog.Root(nil), (*patch.Libraries)...)
	}
	if patch.Targets != nil {
		next.Targets = append([]TargetConfig(nil), (*patch.Targets)...)
	}
	if patch.SelectedTarget != nil {
		next.SelectedTarget = *patch.SelectedTarget
	}
	if patch.PrepareTarget != nil {
		found := false
		for i := range next.Targets {
			if next.Targets[i].Name != *patch.PrepareTarget {
				continue
			}
			found = true
			if next.Targets[i].TargetID == "" {
				next.Targets[i].TargetID, err = discovery.NewID()
			}
		}
		if !found || s.configPath == "" || err != nil {
			s.libraryMu.Unlock()
			s.targetMu.Unlock()
			return canonicalError(protocol.CodeBadRequest, nil)
		}
	}
	rootsChanged, err := s.setLibrarySettingsLocked(next)
	hook := s.settingsTargetHook
	s.settingsTargetHook = nil
	s.libraryMu.Unlock()
	s.targetMu.Unlock()
	if hook != nil {
		hook()
	}
	if err != nil {
		return err
	}
	if rootsChanged {
		_, err = s.scanLocked(ctx)
	}
	return err
}

// sameTargets compares the persisted target fields and the selection.
func sameTargets(next, current []TargetConfig, nextSelected, currentSelected string) bool {
	if nextSelected != currentSelected || len(next) != len(current) {
		return false
	}
	for i := range next {
		a, b := next[i], current[i]
		if a.Name != b.Name || a.Enabled != b.Enabled || a.Address != b.Address || a.Agent != b.Agent || a.TargetID != b.TargetID {
			return false
		}
	}
	return true
}

func (s *Service) setLibrarySettingsLocked(next LibraryConfig) (bool, error) {
	if next.Libraries == nil {
		next.Libraries = append([]catalog.Root(nil), s.roots...)
	}
	if next.Targets == nil {
		next.Targets = append([]TargetConfig(nil), s.targets...)
		if strings.TrimSpace(next.SelectedTarget) == "" {
			next.SelectedTarget = s.selectedTarget
		}
	}
	if len(next.Targets) == 0 {
		return false, canonicalError(protocol.CodeBadRequest, nil)
	}
	next.Libraries = assignLibraryRootIDs(next.Libraries)
	mergedTargets, err := s.mergeTargetAgentsLocked(next.Targets)
	if err != nil {
		return false, canonicalError(protocol.CodeBadRequest, nil)
	}
	next.Targets = mergedTargets
	selectedWrite := targetByName(next.Targets, next.SelectedTarget)
	normalized, err := NormalizeLibraryConfig(next)
	if err != nil {
		return false, canonicalError(protocol.CodeBadRequest, nil)
	}
	if s.kitHost && !sameTargets(normalized.Targets, s.targets, normalized.SelectedTarget, s.selectedTarget) {
		// Kit mode owns the one loopback target; only libraries and
		// library settings change here.
		return false, canonicalError(protocol.CodeBadRequest, nil)
	}
	selected := targetByName(normalized.Targets, normalized.SelectedTarget)
	currentSelected := targetByName(s.targets, s.selectedTarget)
	s.executionMu.Lock()
	play := s.plays[s.selectedTarget]
	active := play.execution != "" || play.packageRejection != nil
	reconciled := s.selectedTargetReconciled
	repairAllowed := s.selectedTargetRepairAllowed
	s.executionMu.Unlock()
	selectedNameChanged := normalized.SelectedTarget != s.selectedTarget
	selectedRenamesCurrent := selectedNameChanged && strings.TrimSpace(selectedWrite.PreviousName) == s.selectedTarget
	selectedConnectionChanged := selected.Enabled != currentSelected.Enabled || selected.Address != currentSelected.Address || selected.Agent != currentSelected.Agent || (currentSelected.TargetID != "" && selected.TargetID != currentSelected.TargetID)
	selectedIdentityChanged := (!selectedRenamesCurrent && selectedNameChanged) || selectedConnectionChanged
	sameEnabledTargetRepair := repairAllowed && !selectedNameChanged && selected.Enabled && currentSelected.Enabled &&
		(selected.Address != currentSelected.Address || selected.Agent != currentSelected.Agent)
	offlineUnowned := !active && s.TargetConnection().State == "disconnected"
	if existing, ok := s.targetClients[s.selectedTarget].(interface{ HasKitGrant() bool }); ok {
		offlineUnowned = offlineUnowned && !existing.HasKitGrant()
	} else {
		// Legacy clients cannot establish absence of local lease authority.
		offlineUnowned = false
	}
	if selectedIdentityChanged && !reconciled && !sameEnabledTargetRepair && !offlineUnowned {
		return false, canonicalError(protocol.CodeBadRequest, nil)
	}
	if active && selected.TargetID != currentSelected.TargetID {
		return false, canonicalError(protocol.CodeBusy, nil)
	}
	if active && (selectedNameChanged || selectedConnectionChanged) {
		return false, canonicalError(protocol.CodeBadRequest, nil)
	}
	var selectedClient serviceClient
	if selected.Enabled {
		if !selectedConnectionChanged && (!selectedNameChanged || selectedRenamesCurrent) {
			selectedClient = s.targetClients[s.selectedTarget]
		} else if s.targetClientFactory == nil {
			if normalized.SelectedTarget == s.selectedTarget || selectedRenamesCurrent {
				selectedClient = s.targetClients[s.selectedTarget]
			}
			if selectedClient == nil {
				return false, canonicalError(protocol.CodeInternal, nil)
			}
		} else {
			selectedClient, err = s.targetClientFactory(selected)
			if err != nil {
				return false, canonicalError(protocol.CodeBadRequest, nil)
			}
		}
	}
	rootsChanged := !sameLibraryRoots(s.roots, normalized.Libraries)
	if err := s.persistAndPublishLibrarySettingsLocked(normalized, selectedClient, selectedIdentityChanged); err != nil {
		return false, err
	}
	return rootsChanged, nil
}

func (s *Service) mergeTargetAgentsLocked(next []TargetConfig) ([]TargetConfig, error) {
	current := make(map[string]string, len(s.targets))
	for _, target := range s.targets {
		current[target.Name] = target.Agent
	}
	merged := append([]TargetConfig(nil), next...)
	claimedCurrentNames := make(map[string]struct{}, len(merged))
	for index := range merged {
		previous := strings.TrimSpace(merged[index].PreviousName)
		currentName := previous
		if currentName == "" {
			if _, exists := current[merged[index].Name]; exists {
				currentName = merged[index].Name
			}
		}
		if currentName == "" {
			continue
		}
		agent, ok := current[currentName]
		if !ok {
			return nil, errors.New("previous target name is unknown")
		}
		if _, duplicate := claimedCurrentNames[currentName]; duplicate {
			return nil, errors.New("current target identity is claimed more than once")
		}
		claimedCurrentNames[currentName] = struct{}{}
		if merged[index].TargetID == "" {
			merged[index].TargetID = targetByName(s.targets, currentName).TargetID
		}
		if !merged[index].AgentSet {
			merged[index].Agent = agent
		}
	}
	return merged, nil
}

func assignLibraryRootIDs(roots []catalog.Root) []catalog.Root {
	assigned := append([]catalog.Root(nil), roots...)
	for index := range assigned {
		if strings.TrimSpace(assigned[index].ID) != "" {
			continue
		}
		path, err := normalizeRoot(assigned[index].Path)
		if err != nil {
			continue
		}
		digest := sha256.Sum256([]byte(string(assigned[index].System) + "\x00" + path))
		assigned[index].ID = "library-" + string(assigned[index].System) + "-" + hex.EncodeToString(digest[:6])
	}
	return assigned
}

func sameLibraryRoots(left, right []catalog.Root) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func sameTargetConnectionConfig(left, right TargetConfig) bool {
	return left.Name != "" && right.Name != "" &&
		left.Name == right.Name && left.TargetID == right.TargetID &&
		left.Enabled == right.Enabled && left.Address == right.Address && left.Agent == right.Agent
}

func changedTargetConnectionNames(previous, next []TargetConfig) []string {
	var changed []string
	seen := make(map[string]struct{}, len(previous)+len(next))
	add := func(name string) {
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		changed = append(changed, name)
	}
	for _, target := range previous {
		if !sameTargetConnectionConfig(target, targetByName(next, target.Name)) {
			add(target.Name)
		}
	}
	for _, target := range next {
		if !sameTargetConnectionConfig(targetByName(previous, target.Name), target) {
			add(target.Name)
		}
	}
	return changed
}

func (s *Service) invalidateChangedTargetConnections(previous, next []TargetConfig, currentName string, clearCurrent bool) {
	s.connectionMu.Lock()
	defer s.connectionMu.Unlock()
	for name := range s.targetConnections {
		if !sameTargetConnectionConfig(targetByName(previous, name), targetByName(next, name)) {
			delete(s.targetConnections, name)
		}
	}
	if clearCurrent || !sameTargetConnectionConfig(targetByName(previous, currentName), targetByName(next, currentName)) {
		s.connection = TargetConnection{}
		s.nextLookup = time.Time{}
		s.lookupFailures = 0
	}
}

func (s *Service) persistAndPublishLibrarySettingsLocked(normalized LibraryConfig, selectedClient serviceClient, selectedIdentityChanged bool) error {
	s.configWriteMu.Lock()
	defer s.configWriteMu.Unlock()
	previousTargets := append([]TargetConfig(nil), s.targets...)
	previousCurrentTarget := s.sessionTargetNameLocked()
	resetTargets := changedTargetConnectionNames(previousTargets, normalized.Targets)
	var previousOverlay []byte
	previousOverlayExists := false
	var previousConfig []byte
	previousConfigExists := false
	if s.configPath != "" {
		var err error
		previousConfig, previousConfigExists, err = snapshotPrivateFile(s.configPath)
		if err != nil {
			return canonicalError(protocol.CodeInternal, safeContextError(err))
		}
	}
	if s.libraryOverlayPath != "" {
		var err error
		previousOverlay, previousOverlayExists, err = snapshotLibraryOverlay(s.libraryOverlayPath)
		if err != nil {
			return canonicalError(protocol.CodeInternal, safeContextError(err))
		}
		if err := saveLibraryOverlay(s.libraryOverlayPath, normalized); err != nil {
			if restoreErr := restoreLibraryOverlay(s.libraryOverlayPath, previousOverlay, previousOverlayExists); restoreErr != nil {
				return canonicalError(protocol.CodeInternal, safeContextError(errors.Join(err, restoreErr)))
			}
			return canonicalError(protocol.CodeInternal, safeContextError(err))
		}
	}
	if s.configPath != "" {
		if err := writeCanonicalConfig(s.configPath, normalized.Libraries, normalized.Targets, normalized.SelectedTarget); err != nil {
			restoreErrors := []error{err, restorePrivateFile(s.configPath, previousConfig, previousConfigExists)}
			if s.libraryOverlayPath != "" {
				restoreErrors = append(restoreErrors, restoreLibraryOverlay(s.libraryOverlayPath, previousOverlay, previousOverlayExists))
			}
			return canonicalError(protocol.CodeInternal, safeContextError(errors.Join(restoreErrors...)))
		}
	}
	s.attractIdle = normalized.AttractIdleSeconds
	s.preferredRegions = append([]string(nil), normalized.PreferredRegions...)
	s.videoProfile = normalized.VideoProfile
	s.roots = append([]catalog.Root(nil), normalized.Libraries...)
	s.rootsByID = make(map[string]catalog.Root, len(s.roots))
	for _, root := range s.roots {
		s.rootsByID[root.ID] = root
	}
	s.targets = append([]TargetConfig(nil), normalized.Targets...)
	s.selectedTarget = normalized.SelectedTarget
	s.invalidateChangedTargetConnections(previousTargets, s.targets, previousCurrentTarget, selectedIdentityChanged)
	previousClients := s.targetClients
	s.targetClients = make(map[string]serviceClient)
	if selectedClient != nil {
		s.targetClients[s.selectedTarget] = selectedClient
	}
	s.executionMu.Lock()
	playNames := make([]string, 0, len(s.plays)+1)
	if s.activeTarget != "" {
		playNames = append(playNames, s.activeTarget)
	}
	for name := range s.plays {
		playNames = append(playNames, name)
	}
	s.executionMu.Unlock()
	for _, name := range playNames {
		if name != "" && name != s.selectedTarget {
			if client := previousClients[name]; client != nil {
				s.targetClients[name] = client
			}
		}
	}
	s.pairedTargetMu.Lock()
	s.pairedTargetConfigs = append([]TargetConfig(nil), s.targets...)
	s.pairedTargetClients = make(map[string]serviceClient, len(s.targetClients))
	for name, client := range s.targetClients {
		s.pairedTargetClients[name] = client
	}
	s.pairedTargetMu.Unlock()
	s.settingsTargetHook = nil
	if selectedIdentityChanged {
		s.executionMu.Lock()
		s.selectedTargetReconciled = !targetByName(s.targets, s.selectedTarget).Enabled
		s.selectedTargetRepairAllowed = false
		s.executionMu.Unlock()
		currentTargetIncluded := false
		for _, name := range resetTargets {
			currentTargetIncluded = currentTargetIncluded || name == previousCurrentTarget
		}
		if previousCurrentTarget != "" && !currentTargetIncluded {
			resetTargets = append(resetTargets, previousCurrentTarget)
		}
	}
	if len(resetTargets) != 0 || selectedIdentityChanged {
		reset, origin := s.targetReset, s.targetOrigin
		cfg := targetByName(s.targets, s.selectedTarget)
		lease := kitLeaseOf(s.targetClients[s.selectedTarget])
		s.settingsTargetHook = func() {
			if reset != nil {
				for _, name := range resetTargets {
					reset(name)
				}
			}
			if selectedIdentityChanged && origin != nil {
				origin(cfg, lease)
			}
		}
	}
	return nil
}
