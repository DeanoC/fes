package hostapi

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/protocol"
)

func (s *sessionCoordinator) launch(ctx context.Context, id, target string, stamp clientStamp) (sessionResult, error) {
	if !s.begin() {
		return sessionResult{}, busyError()
	}
	defer s.end()
	s.observationMu.Lock()
	defer s.observationMu.Unlock()
	return s.launchLocked(ctx, id, target, stamp)
}

// launchLocked performs a launch while the caller holds coordinator
// admission and observationMu. Cartridge replacement uses the same guarded
// path so a Stop or another launch cannot replace the admitted machine after
// its status check and before this launch begins.
func (s *sessionCoordinator) launchLocked(ctx context.Context, id, target string, stamp clientStamp) (sessionResult, error) {
	ctx = s.scoped(ctx)
	if target != "" {
		ctx = fogcast.WithSessionTarget(ctx, s.target)
	}
	// Package admission must run before retiring the previous input/media owner.
	execution := fogcast.ExecutionFPGANative
	if resolver, ok := s.service.(sessionExecutionService); ok {
		resolved, err := resolver.SessionExecution(ctx, id)
		if err != nil {
			return sessionResult{}, err
		}
		if resolved != "" {
			execution = resolved
		}
	}
	packageLaunch := execution == fogcast.ExecutionFPGADevelopment
	if game, err := s.service.Game(ctx, id); err == nil && game.Kind == catalog.SourceKindCorePackage {
		packageLaunch = true
	}
	if packageLaunch {
		response, err := s.launchGame(ctx, id, target, nil)
		placed := response.Status.State == protocol.StateActive || response.Status.State == protocol.StateLaunching
		owner := s
		if target == "" {
			resolvedTarget := ""
			if named, ok := s.service.(interface{ SessionTargetName() string }); ok {
				resolvedTarget = named.SessionTargetName()
			} else if bound, ok := s.service.(interface{ SessionTarget() (string, string) }); ok {
				resolvedTarget, _ = bound.SessionTarget()
			}
			if resolvedTarget != "" && s.root != nil && resolvedTarget != s.target {
				owner = s.root.forTarget(resolvedTarget)
			}
		}
		var result sessionResult
		if owner != s && placed {
			result, err = s.adoptPlacedPlay(ctx, owner, response.Status, func() (sessionResult, error) {
				owner.mu.Lock()
				owner.execution, owner.packageOwned = "", false
				owner.nativeStoppedIdle = false
				owner.mediaHandle, owner.mediaState, owner.inputBinding = nil, "", sessionInputBinding{}
				owner.mu.Unlock()
				return owner.finishCoreLoad(owner.scoped(ctx), response.Status, err, "session.launch", stamp, execution)
			})
		} else if owner != s {
			if err == nil {
				err = &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "placed package launch did not start", Phase: "admission"}
			}
		} else {
			result, err = owner.finishCoreLoad(ctx, response.Status, err, "session.launch", stamp, execution)
		}
		if err == nil && result.State == protocol.StateActive {
			if recorder, ok := s.service.(interface {
				RecordPlay(context.Context, string) error
			}); ok {
				_ = recorder.RecordPlay(owner.scoped(ctx), id)
			}
		}
		return result, err
	}
	development, err := s.developmentActive(ctx)
	if err != nil {
		return sessionResult{}, err
	}
	if development {
		return sessionResult{}, developmentMustStopError()
	}
	if err := s.stopPackageOwnedForReplacement(ctx, target); err != nil {
		return sessionResult{}, err
	}

	// Keep the selected coordinator's visible state so an omitted-target
	// placement can hand the new play to its actual executor afterwards.
	s.mu.Lock()
	previousExecution := s.execution
	previousNativeStoppedIdle := s.nativeStoppedIdle
	previousFlight := s.flightID
	previousPackageOwned := s.packageOwned
	s.mu.Unlock()
	if s.remoteInput != nil {
		if err := s.detachInputNow(ctx, "session_replace"); err != nil {
			return sessionResult{}, remoteInputError()
		}
	}
	if err := s.stopMediaBounded(previousExecution); err != nil {
		return sessionResult{}, err
	}

	s.beginFlight()

	s.mu.Lock()
	s.execution = execution
	s.nativeStoppedIdle = false
	s.mediaHandle = nil
	s.mediaState = ""
	s.mu.Unlock()

	var progress sessionProgress
	resp, err := s.launchGame(ctx, id, target, func(value fogcast.Progress) {
		progress = sessionProgress{Stage: value.Stage, Message: value.Message}
	})
	if err != nil {
		_ = s.stopMediaBounded(execution)
		s.restoreExecution(previousExecution)
		s.restoreNativeStoppedIdle(previousNativeStoppedIdle)
		s.restoreFlight(previousFlight)
		return sessionResult{}, err
	}
	owner := s
	resolvedTarget := ""
	if named, ok := s.service.(interface{ SessionTargetName() string }); ok {
		resolvedTarget = named.SessionTargetName()
	} else if respTarget, ok := s.service.(interface{ SessionTarget() (string, string) }); ok {
		resolvedTarget, _ = respTarget.SessionTarget()
	}
	if target == "" && s.root != nil && resolvedTarget != "" && resolvedTarget != s.target {
		owner = s.root.forTarget(resolvedTarget)
		return s.adoptPlacedPlay(ctx, owner, resp.Status, func() (sessionResult, error) {
			s.handoffLaunchState(owner, previousExecution, previousNativeStoppedIdle, previousFlight, previousPackageOwned)
			return s.finishOrdinaryLaunch(owner.scoped(ctx), owner, id, execution, resp.Status, &progress, stamp)
		})
	}
	return s.finishOrdinaryLaunch(ctx, owner, id, execution, resp.Status, &progress, stamp)
}

func (s *sessionCoordinator) finishOrdinaryLaunch(ctx context.Context, owner *sessionCoordinator, id, execution string, status protocol.Status, progress *sessionProgress, stamp clientStamp) (sessionResult, error) {
	owner.mu.Lock()
	owner.bindExecutionPackageLocked(status)
	owner.mu.Unlock()
	result := owner.publicSession(status, progress)
	result.Execution = execution
	if status.State == protocol.StateActive {
		if err := owner.startMedia(ctx, id, execution); err != nil {
			_ = owner.stopMediaBounded(execution)
			owner.mu.Lock()
			if owner.mediaHandle == nil {
				owner.mediaState = "failed"
			}
			owner.mu.Unlock()
			result.Media = owner.currentMediaState()
			if result.Media == "" {
				result.Media = "failed"
			}
		} else {
			result.Media = owner.currentMediaState()
		}
	}
	if owner.remoteInput != nil && status.State == protocol.StateActive {
		core := sessionCore(status)
		if core == "" {
			return sessionResult{}, owner.stopAfterFailedAttach(execution)
		}
		if err := owner.attachInputForStatus(ctx, status); err != nil {
			return sessionResult{}, owner.stopAfterFailedAttach(execution)
		}
		result = owner.publicSession(status, progress)
		result.Execution = execution
		result.Media = owner.currentMediaState()
	}
	owner.recordStamp("session.launch", result, progress, stamp)
	if status.State == protocol.StateActive {
		if recorder, ok := s.service.(interface {
			RecordPlay(context.Context, string) error
		}); ok {
			_ = recorder.RecordPlay(ctx, id)
		}
	}
	return result, nil
}

// adoptPlacedPlay serializes host ownership changes with every operation on the
// destination kit. The status check closes the gap between placement and
// admission: a Stop that won that race leaves the kit idle and nothing is
// installed. The callback runs while destination admission and observation are
// held, so a later Stop waits until all new resources are attached.
func (s *sessionCoordinator) adoptPlacedPlay(ctx context.Context, dst *sessionCoordinator, placed protocol.Status, install func() (sessionResult, error)) (sessionResult, error) {
	if !dst.beginWait(ctx, 500*time.Millisecond) {
		dst.rollbackPlacedIfCurrent(ctx, placed)
		return sessionResult{}, busyError()
	}
	defer dst.end()
	dst.observationMu.Lock()
	defer dst.observationMu.Unlock()
	check := dst.scoped(ctx)
	current, err := dst.service.Status(check)
	if err != nil {
		dst.rollbackPlacedIfCurrent(ctx, placed)
		return sessionResult{}, err
	}
	if !samePlacedPlay(placed, current) {
		return sessionResult{}, &protocol.APIError{Code: protocol.CodeBusy, Message: "placed play ended before session adoption", Phase: "admission"}
	}
	dst.mu.Lock()
	previous := adoptionSnapshot{
		execution: dst.execution, packageOwned: dst.packageOwned,
		executionPackage:  dst.executionPackage,
		nativeStoppedIdle: dst.nativeStoppedIdle, mediaState: dst.mediaState,
		mediaHandle: dst.mediaHandle, mediaGeneration: dst.mediaGeneration,
		inputBinding: dst.inputBinding,
		flightID:     dst.flightID,
	}
	dst.mu.Unlock()
	oldExecution := previous.execution
	if dst.remoteInput != nil {
		if err := dst.detachInputBounded("session_replace"); err != nil {
			dst.restoreAdoptionSnapshot(previous)
			dst.rollbackPlacedIfCurrent(ctx, placed)
			return sessionResult{}, err
		}
	}
	if err := dst.stopMediaBounded(oldExecution); err != nil {
		dst.restoreAdoptionSnapshot(previous)
		dst.rollbackPlacedIfCurrent(ctx, placed)
		return sessionResult{}, err
	}
	dst.mu.Lock()
	dst.mediaHandle, dst.mediaState, dst.mediaGeneration = nil, "", 0
	dst.inputBinding = sessionInputBinding{}
	dst.mu.Unlock()
	result, err := install()
	if err != nil {
		dst.restoreAdoptionSnapshot(previous)
		dst.rollbackPlacedIfCurrent(ctx, placed)
	}
	return result, err
}

func (s *sessionCoordinator) restoreAdoptionSnapshot(previous adoptionSnapshot) {
	s.mu.Lock()
	s.execution, s.packageOwned = previous.execution, previous.packageOwned
	s.executionPackage = previous.executionPackage
	s.nativeStoppedIdle, s.mediaState = previous.nativeStoppedIdle, previous.mediaState
	s.mediaHandle, s.mediaGeneration = previous.mediaHandle, previous.mediaGeneration
	s.inputBinding = previous.inputBinding
	s.flightID = previous.flightID
	s.mu.Unlock()
}

func samePlacedPlay(placed, current protocol.Status) bool {
	if (current.State != protocol.StateActive && current.State != protocol.StateLaunching) || placed.GameID == nil || current.GameID == nil || *placed.GameID != *current.GameID {
		return false
	}
	// A described package's runtime generation distinguishes a relaunch of the
	// same library game from the play that placement just started.
	if placed.CorePackage != nil {
		return current.CorePackage != nil && placed.CorePackage.PackageID == current.CorePackage.PackageID && placed.CorePackage.Generation == current.CorePackage.Generation
	}
	return true
}

func (s *sessionCoordinator) rollbackPlacedIfCurrent(_ context.Context, placed protocol.Status) {
	check, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	check = s.scoped(check)
	current, err := s.service.Status(check)
	if err == nil && samePlacedPlay(placed, current) {
		s.stopPlacedPlay()
	}
}

// handoffLaunchState moves the just-started play to the coordinator for the
// executor FogCast selected. Lock ordering is stable even when two launches
// resolve in opposite directions.
func (s *sessionCoordinator) handoffLaunchState(dst *sessionCoordinator, previousExecution string, previousNativeStoppedIdle bool, previousFlight string, previousPackageOwned bool) {
	first, second := s, dst
	if first.target > second.target {
		first, second = second, first
	}
	first.mu.Lock()
	second.mu.Lock()
	dst.execution, dst.packageOwned = s.execution, s.packageOwned
	dst.nativeStoppedIdle, dst.mediaState = s.nativeStoppedIdle, s.mediaState
	dst.mediaHandle = s.mediaHandle
	dst.mediaGeneration, dst.inputBinding = s.mediaGeneration, s.inputBinding
	dst.flightID = s.flightID
	s.execution, s.packageOwned = previousExecution, previousPackageOwned
	s.nativeStoppedIdle = previousNativeStoppedIdle
	s.flightID = previousFlight
	s.mediaHandle, s.mediaState, s.inputBinding = nil, "", sessionInputBinding{}
	s.mediaGeneration = 0
	second.mu.Unlock()
	first.mu.Unlock()
}

func (s *sessionCoordinator) loadDevelopmentRBF(ctx context.Context, size int64, content io.Reader, stamp clientStamp) (sessionResult, error) {
	ctx = s.scoped(ctx)
	if !s.begin() {
		return sessionResult{}, busyError()
	}
	defer s.end()
	s.observationMu.Lock()
	defer s.observationMu.Unlock()
	development, err := s.developmentActive(ctx)
	if err != nil {
		return sessionResult{}, err
	}
	if development {
		return sessionResult{}, developmentMustStopError()
	}
	s.mu.Lock()
	previousExecution := s.execution
	s.mu.Unlock()
	nativeAlreadyIdle := false
	if previousExecution == fogcast.ExecutionFPGANative {
		observed, err := s.service.Status(ctx)
		if err != nil {
			return sessionResult{}, err
		}
		if observed.Development && observed.State != protocol.StateIdle {
			return sessionResult{}, developmentMustStopError()
		}
		nativeAlreadyIdle = exactIdleStatus(observed)
	}

	if s.remoteInput != nil {
		if err := s.detachInputNow(ctx, "session_replace"); err != nil {
			return sessionResult{}, remoteInputError()
		}
	}
	if err := s.stopMediaBounded(previousExecution); err != nil {
		return sessionResult{}, err
	}
	if previousExecution == fogcast.ExecutionFPGANative && !nativeAlreadyIdle {
		stopped, err := s.stopServiceForReplacement()
		if err != nil {
			return sessionResult{}, err
		}
		if !exactIdleStatus(stopped) {
			return sessionResult{}, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "native game did not stop to idle"}
		}
	}
	if previousExecution == fogcast.ExecutionFPGANative {
		s.restoreExecution("")
	}
	if err := ctx.Err(); err != nil {
		return sessionResult{}, err
	}

	status, err := s.service.LoadDevelopmentRBF(ctx, size, content)
	if err != nil {
		if observed, observeErr := s.service.Status(ctx); observeErr == nil {
			s.mu.Lock()
			switch {
			case observed.Development && observed.State != protocol.StateIdle:
				s.execution = fogcast.ExecutionFPGADevelopment
				s.packageOwned = false
			case observed.State == protocol.StateIdle:
				s.execution = ""
				s.packageOwned = false
			}
			s.mu.Unlock()
		}
		return sessionResult{}, err
	}
	s.beginFlight()
	s.mu.Lock()
	s.execution = fogcast.ExecutionFPGADevelopment
	s.packageOwned = false
	s.mediaHandle = nil
	s.mediaState = ""
	s.mu.Unlock()
	result := s.publicSession(status, nil)
	result.Execution = fogcast.ExecutionFPGADevelopment
	s.recordStamp("session.development_rbf", result, nil, stamp)
	return result, nil
}

func (s *sessionCoordinator) loadDevelopmentCore(ctx context.Context, size int64, content io.Reader, stamp clientStamp) (sessionResult, error) {
	ctx = s.scoped(ctx)
	if !s.begin() {
		return sessionResult{}, busyError()
	}
	defer s.end()
	s.observationMu.Lock()
	defer s.observationMu.Unlock()
	loader, ok := s.service.(sessionCoreService)
	if !ok {
		return sessionResult{}, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"}
	}
	status, err := loader.LoadCore(ctx, size, content)
	return s.finishCoreLoad(ctx, status, err, "session.development_core", stamp, fogcast.ExecutionFPGADevelopment)
}

// finishCoreLoad shares the confirmed-package ownership transition between
// explicit development loading and package-backed library entries.
// execution is the host play label: recognized-ABI library titles use
// FPGANative; no-ABI and LoadDevelopmentCore stay FPGADevelopment.
// The caller holds observationMu and the coordinator operation admission.
func (s *sessionCoordinator) finishCoreLoad(ctx context.Context, status protocol.Status, err error, event string, stamp clientStamp, execution string) (sessionResult, error) {
	var identityErr *protocol.APIError
	if errors.As(err, &identityErr) && identityErr.Phase == "identity" {
		status.LastError = identityErr
	}

	if err != nil && corePackagePreMutationFailure(err) {
		// These failures are proven to precede target mutation, so the current
		// host-owned input and media session still describe the active target.
		return sessionResult{}, err
	}
	if !corePackageInputStatus(status) {
		// The target result is ambiguous or post-mutation. Retire stale target
		// input ownership.
		_ = s.detachInputBounded("session_replace_ambiguous")
		s.mu.Lock()
		previousExecution := s.execution
		s.execution = ""
		s.packageOwned = false
		s.mu.Unlock()
		_ = s.stopMediaBounded(previousExecution)
		if err != nil {
			return sessionResult{}, err
		}
		return sessionResult{}, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "target package state is unavailable", Phase: "recovery"}
	}
	if execution == "" {
		execution = fogcast.ExecutionFPGADevelopment
	}
	s.mu.Lock()
	previousExecution := s.execution
	s.execution = execution
	s.packageOwned = true
	s.mu.Unlock()
	// Publish the confirmed target owner before cleaning up the retired host
	// session. Cleanup failures must not make subsequent status calls report
	// that the old game still owns the target.
	var cleanupErr error
	detachErr := s.detachInputBounded("session_replace")
	if detachErr != nil {
		s.clearInputBinding()
		cleanupErr = detachErr
	}
	if mediaErr := s.stopMediaBounded(previousExecution); mediaErr != nil && cleanupErr == nil {
		cleanupErr = mediaErr
	}
	s.mu.Lock()
	if s.mediaHandle == nil {
		s.mediaState = ""
	}
	s.mu.Unlock()
	s.beginFlight()
	s.mu.Lock()
	s.bindExecutionPackageLocked(status)
	s.mu.Unlock()
	if detachErr == nil && s.remoteInput != nil && inputEligibleStatus(status) {
		core := sessionCore(status)
		if core == "" || s.attachInputForStatus(ctx, status) != nil {
			return sessionResult{}, s.stopAfterFailedAttach(execution)
		}
	}
	result := s.publicSession(status, nil)
	result.Execution = execution
	s.recordStamp(event, result, nil, stamp)
	if err != nil {
		return result, err
	}
	if cleanupErr != nil {
		return result, &protocol.APIError{Code: protocol.CodeInternal, Message: "retired host session cleanup failed", Phase: "recovery"}
	}
	return result, nil
}

func corePackagePreMutationFailure(err error) bool {
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.Phase == "recovery" {
		return false
	}
	if apiErr.Code == protocol.CodeBusy {
		return true
	}
	switch apiErr.Phase {
	case "request", "admission", "compatibility", "save":
		return true
	default:
		return false
	}
}

func (s *sessionCoordinator) stopServiceForReplacement() (protocol.Status, error) {
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stopCtx = s.scoped(stopCtx)
	return s.service.Stop(stopCtx)
}

func packageReplacementApplies(requestedTarget, packageOwnerTarget string) bool {
	requestedTarget = strings.TrimSpace(requestedTarget)
	packageOwnerTarget = strings.TrimSpace(packageOwnerTarget)
	if requestedTarget == "" || packageOwnerTarget == "" {
		return true
	}
	return requestedTarget == packageOwnerTarget
}

func (s *sessionCoordinator) stopPackageOwnedForReplacement(ctx context.Context, requestedTarget string) error {
	s.mu.Lock()
	packageOwned := s.packageOwned
	previousExecution := s.execution
	s.mu.Unlock()
	if !packageOwned {
		return nil
	}
	packageOwnerTarget := ""
	if binder, ok := s.service.(interface{ SessionTarget() (string, string) }); ok {
		packageOwnerTarget, _ = binder.SessionTarget()
	}
	if !packageReplacementApplies(requestedTarget, packageOwnerTarget) {
		return nil
	}
	if s.remoteInput != nil {
		if err := s.detachInputNow(ctx, "session_replace"); err != nil {
			return remoteInputError()
		}
	}
	if err := s.stopMediaBounded(previousExecution); err != nil {
		return err
	}
	stopped, err := s.service.Stop(ctx)
	if err != nil {
		return err
	}
	if !exactIdleStatus(stopped) {
		return &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "package session did not stop to idle"}
	}
	s.restoreExecution("")
	return nil
}

func (s *sessionCoordinator) launchGame(ctx context.Context, id, target string, progress fogcast.ProgressFunc) (protocol.CachedLaunchResponse, error) {
	if launcher, ok := s.service.(sessionLaunchOnService); ok {
		return launcher.LaunchOn(ctx, id, target, progress)
	}
	return s.service.Launch(ctx, id, progress)
}
