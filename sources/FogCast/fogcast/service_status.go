package fogcast

import (
	"context"
	"errors"
	"strings"

	"github.com/DeanoC/FogCast/protocol"
)

func (s *Service) Health(parent context.Context) (protocol.Health, error) {
	if targetID, _ := parent.Value(pairedTargetContextKey{}).(string); targetID != "" {
		ctx, cancel := serviceTimeout(parent, s.requestTimeout)
		defer cancel()
		client, ok := s.pairedTargetClient(targetID)
		if !ok {
			return protocol.Health{}, errors.New("paired target unavailable")
		}
		return client.Health(ctx)
	}
	ctx, cancel := serviceTimeout(parent, s.requestTimeout)
	defer cancel()
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return protocol.Health{}, err
	}
	defer release()
	health, err := s.refreshTargetConnection(ctx)
	if err != nil {
		return protocol.Health{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
	}
	return health, nil
}

func (s *Service) Status(parent context.Context) (protocol.Status, error) {
	if targetID := PairedTargetFromContext(parent); targetID != "" {
		return s.statusPairedTarget(parent, targetID)
	}
	if name := SessionTargetFromContext(parent); name != "" {
		return s.StatusTarget(parent, name)
	}
	return s.statusTarget(parent, "")
}

func (s *Service) statusPairedTarget(parent context.Context, targetID string) (protocol.Status, error) {
	ctx, cancel := serviceTimeout(parent, s.requestTimeout)
	defer cancel()
	name := s.pairedTargetName(targetID)
	if name == "" {
		return protocol.Status{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	client, ok := s.pairedTargetClient(targetID)
	if !ok {
		return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	status, err := client.Status(ctx)
	if err != nil {
		return protocol.Status{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
	}
	s.executionMu.Lock()
	play, hasPlay := s.plays[name]
	matchingPackage := status.CorePackage == nil && play.packageID == "" && play.packageGeneration == 0
	if status.CorePackage != nil {
		matchingPackage = status.CorePackage.PackageID != "" && status.CorePackage.Generation != 0 &&
			status.CorePackage.PackageID == play.packageID && status.CorePackage.Generation == play.packageGeneration
	}
	if hasPlay && matchingPackage && (status.State == protocol.StateActive || status.State == protocol.StateStopping) {
		status = attachSavedCoreID(status, play)
		if status.GameID == nil && play.gameID != "" {
			status.GameID = stringPtr(play.gameID)
		}
		if status.System == nil && play.system != "" {
			status.System = systemPtr(play.system)
		}
		if status.LastError == nil {
			status.LastError = play.packageRejection
		}
	}
	s.executionMu.Unlock()
	return s.adoptObservedKit(name, status), nil
}

func (s *Service) pairedTargetName(targetID string) string {
	s.pairedTargetMu.Lock()
	defer s.pairedTargetMu.Unlock()
	for _, target := range s.pairedTargetConfigs {
		if target.Enabled && target.TargetID == targetID {
			return target.Name
		}
	}
	return ""
}

// StatusTarget observes one configured target without changing which target
// unscoped browser Status considers foreground.
func (s *Service) StatusTarget(parent context.Context, target string) (protocol.Status, error) {
	if strings.TrimSpace(target) == "" {
		return protocol.Status{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	return s.statusTarget(parent, target)
}

func (s *Service) statusTarget(parent context.Context, target string) (protocol.Status, error) {
	if target != "" {
		parent = WithSessionTarget(parent, target)
	}
	ctx, cancel := serviceTimeout(parent, s.requestTimeout)
	defer cancel()
	releaseLifecycle, err := s.acquireLifecycle(ctx)
	if err != nil {
		return protocol.Status{}, err
	}
	defer releaseLifecycle()
	if target != "" {
		s.targetMu.Lock()
		cfg := targetByName(s.targets, target)
		_, clientAvailable := s.targetClientLocked(target)
		s.targetMu.Unlock()
		if cfg.Name == "" || !cfg.Enabled {
			return protocol.Status{}, canonicalError(protocol.CodeBadRequest, nil)
		}
		if !clientAvailable {
			return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
		}
	}
	if s.discoveryEnabledForContext(ctx) {
		if _, err := s.refreshTargetConnection(ctx); err != nil {
			return protocol.Status{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
		}
	}
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	var client serviceClient
	var ok bool
	if target != "" {
		client, ok = s.targetClients[target]
		ok = client != nil
	} else {
		client, ok = s.selectedClientLocked()
	}
	if !ok {
		s.allowSelectedTargetRepair(parent)
		return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	status, err := client.Status(ctx)
	if err != nil {
		s.allowSelectedTargetRepair(parent)
		return protocol.Status{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
	}
	{
		// Project catalog identity from the target-owned record.
		s.executionMu.Lock()
		kitTarget := target
		if kitTarget == "" {
			kitTarget, _ = s.selectedKitPlayLocked()
		}
		play, hasPlay := s.plays[kitTarget]
		matchingPackage := status.CorePackage == nil && play.packageID == "" && play.packageGeneration == 0
		if status.CorePackage != nil {
			matchingPackage = status.CorePackage.PackageID != "" && status.CorePackage.Generation != 0 &&
				status.CorePackage.PackageID == play.packageID && status.CorePackage.Generation == play.packageGeneration
		}
		if hasPlay && matchingPackage && (status.State == protocol.StateActive || status.State == protocol.StateStopping) {
			status = attachSavedCoreID(status, play)
			if status.GameID == nil && play.gameID != "" {
				status.GameID = stringPtr(play.gameID)
			}
			if status.System == nil && play.system != "" {
				status.System = systemPtr(play.system)
			}
			if status.LastError == nil {
				status.LastError = play.packageRejection
			}
		}
		s.executionMu.Unlock()
	}
	kitStatusTarget := target
	if kitStatusTarget == "" {
		s.executionMu.Lock()
		kitStatusTarget, _ = s.selectedKitPlayLocked()
		s.executionMu.Unlock()
	}
	status = s.adoptObservedKit(kitStatusTarget, status)
	s.executionMu.Lock()
	if target == "" && status.State != protocol.StateIdle {
		s.activeTarget = kitStatusTarget
	}
	if rejection := s.plays[kitStatusTarget].packageRejection; rejection != nil {
		status.LastError = rejection
		status.GameID = nil
		status.System = nil
	}
	s.executionMu.Unlock()
	return status, nil
}

func attachSavedCoreID(status protocol.Status, play targetPlay) protocol.Status {
	if status.CorePackage == nil || status.CorePackage.CoreID != "" || play.coreID == "" {
		return status
	}
	corePackage := *status.CorePackage
	corePackage.CoreID = play.coreID
	status.CorePackage = &corePackage
	return status
}

// adoptObservedKit records the kit observation. A failed development status
// keeps the development execution and the kit's package and recovery evidence,
// and the returned status drops the game the kit was recovering from.
func (s *Service) adoptObservedKit(target string, status protocol.Status) protocol.Status {
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	play := s.plays[target]
	if status.State == protocol.StateIdle {
		if target == s.selectedTarget {
			s.selectedTargetReconciled = true
			s.selectedTargetRepairAllowed = false
		}
		if play.packageRejection != nil {
			s.plays[target] = targetPlay{packageRejection: play.packageRejection}
		} else {
			delete(s.plays, target)
			if s.activeTarget == target {
				s.activeTarget = ""
			}
			if s.boundKitTarget == target {
				s.boundKitTarget = ""
			}
		}
		return status
	}
	if status.State == protocol.StateFailed && !status.Development {
		if play.packageRejection != nil {
			s.plays[target] = targetPlay{packageRejection: play.packageRejection}
		} else {
			delete(s.plays, target)
			if s.activeTarget == target {
				s.activeTarget = ""
			}
			if s.boundKitTarget == target {
				s.boundKitTarget = ""
			}
		}
		if target == s.selectedTarget {
			s.selectedTargetReconciled = false
			s.selectedTargetRepairAllowed = false
		}
		return status
	}
	if target == s.selectedTarget {
		s.selectedTargetReconciled = false
		s.selectedTargetRepairAllowed = false
	}
	if status.Development {
		play.execution = ExecutionFPGADevelopment
		if status.CorePackage != nil {
			if play.packageID != status.CorePackage.PackageID || play.packageGeneration != status.CorePackage.Generation {
				play.gameID, play.system = "", ""
			}
			play.packageID = status.CorePackage.PackageID
			play.packageGeneration = status.CorePackage.Generation
			if recognizedPlayContract(status.CorePackage.ABI) {
				play.execution = ExecutionFPGANative
			}
		} else {
			play.packageID, play.packageGeneration = "", 0
		}
	} else if status.State == protocol.StateActive {
		play.execution = ExecutionFPGANative
	}
	if status.State == protocol.StateFailed {
		// Development recovery keeps the kit's execution, not the game it was running.
		play.gameID, play.system = "", ""
		s.plays[target] = play
		status.GameID, status.System = nil, nil
		return status
	}
	if status.GameID != nil {
		play.gameID = *status.GameID
	}
	if status.System != nil {
		play.system = *status.System
	}
	s.plays[target] = play
	return status
}

func (s *Service) allowSelectedTargetRepair(parent context.Context) {
	if parent.Err() != nil {
		return
	}
	s.executionMu.Lock()
	s.selectedTargetReconciled = false
	s.selectedTargetRepairAllowed = s.activeExecution == ""
	s.executionMu.Unlock()
}
