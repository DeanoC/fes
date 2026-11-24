package hostapi

import (
	"context"
	"errors"
	"time"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/protocol"
)

func (s *sessionCoordinator) status(ctx context.Context) (sessionResult, error) {
	if s.target != "" {
		ctx = fogcast.WithSessionTarget(ctx, s.target)
	}
	// Serialize the service observation itself with launch, stop, and terminal
	// media observation. Otherwise a status call can begin against one host
	// generation, block while launch installs the next generation, then apply
	// its stale idle result to the replacement media handle.
	s.observationMu.Lock()
	defer s.observationMu.Unlock()

	st, err := s.service.Status(ctx)
	if err != nil {
		result := s.publicSession(st, nil)
		s.mu.Lock()
		if s.execution != "" {
			result.Execution = s.execution
		}
		if s.mediaState != "" {
			result.Media = s.mediaState
		}
		s.mu.Unlock()
		return result, err
	}
	// A kit host may observe a game owned by another host. Skip only its
	// remote-input reconciliation after a fresh foreign-held lease observation.
	// The target still authorizes every local keyboard edge.
	foreignRead := false
	if st.State == protocol.StateActive && st.Recovery == "" && st.LastError == nil && s.target == fogcast.KitHostTargetName {
		if kit, ok := s.service.(interface{ KitHostMode() bool }); ok && kit.KitHostMode() {
			if observer, ok := s.service.(interface {
				KitLocalForeignLease(context.Context, string) (bool, error)
			}); ok {
				var observeErr error
				foreignRead, observeErr = observer.KitLocalForeignLease(ctx, s.target)
				if observeErr != nil {
					return s.publicSession(st, nil), observeErr
				}
			}
		}
	}
	if !foreignRead {
		if err := s.reconcileInput(ctx, st); err != nil {
			return s.publicSession(st, nil), err
		}
	}
	projectInput := func(result *sessionResult) {
		if foreignRead {
			result.Input = &host.RemoteInputStatus{State: host.RemoteInputDetached}
		}
	}

	s.mu.Lock()
	execution, mediaHandle, mediaState := s.execution, s.mediaHandle, s.mediaState
	s.mu.Unlock()
	if mediaHandle != nil && mediaState == "active" && mediaHandleDone(mediaHandle) {
		mediaErr := s.stopMediaBounded(execution)
		result := s.publicSession(st, nil)
		if !foreignRead {
			result.Execution = execution
			result.Media = s.currentMediaState()
		}
		projectInput(&result)
		if mediaErr != nil {
			s.record("session.media.exit_failed", result, nil)
			return result, nil
		}
		s.record("session.media.exit", result, nil)
		return result, nil
	}
	if st.State == protocol.StateIdle && mediaHandle != nil && mediaState == "active" {
		if err := s.detachInputBounded("session_exit"); err != nil {
			return s.publicSession(st, nil), err
		}
		if err := s.stopMediaBounded(execution); err != nil {
			result := s.publicSession(st, nil)
			result.Execution = execution
			result.Media = s.currentMediaState()
			return result, err
		}
		result := s.publicSession(st, nil)
		result.Execution = execution
		result.Media = "stopped"
		s.record("session.exit", result, nil)
		return result, nil
	}

	result := s.publicSession(st, nil)
	projectInput(&result)
	s.mu.Lock()
	if s.execution == "" && !foreignRead {
		if execution, packageOwned := reconstructedSessionExecution(st); execution != "" {
			s.execution = execution
			s.packageOwned = packageOwned
			s.bindExecutionPackageLocked(st)
			s.nativeStoppedIdle = false
		}
	}
	if st.State == protocol.StateIdle && (s.execution == fogcast.ExecutionFPGADevelopment || s.packageOwned) {
		s.execution = ""
		s.packageOwned = false
		s.nativeStoppedIdle = false
	}
	// A package may have been replaced by another actor since this
	// coordinator last owned it. Do not relabel that replacement with the
	// previous package's execution type.
	ownedPackageChanged := st.State == protocol.StateActive && s.executionPackage.packageID != "" && !s.matchesExecutionPackageLocked(st)
	if s.execution != "" && !ownedPackageChanged && !foreignRead {
		result.Execution = s.execution
	}
	if s.mediaState != "" && !foreignRead {
		result.Media = s.mediaState
	}
	s.mu.Unlock()
	s.record("session.status", result, nil)
	return result, nil
}

func (s *sessionCoordinator) stop(ctx context.Context, stamp clientStamp, retainLease, releaseIdle bool, expected *sessionStopExpectation) (sessionResult, error) {
	if s.target != "" {
		ctx = fogcast.WithSessionTarget(ctx, s.target)
	}
	if releaseIdle {
		return s.releaseIdleGrants(ctx, stamp)
	}
	if !s.beginWait(ctx, 5*time.Second) {
		return sessionResult{}, busyError()
	}
	defer s.end()
	s.observationMu.Lock()
	defer s.observationMu.Unlock()
	if expected != nil {
		// This observation is inside the launch/stop admission guard. A stale
		// room cannot detach input, stop media, or release a different play's
		// lease before discovering that its displayed identity has changed.
		status, err := s.service.Status(ctx)
		if err != nil {
			return sessionResult{}, err
		}
		if !expected.matches(s.publicSession(status, nil)) {
			return sessionResult{}, fogcast.ErrSessionChanged
		}
	}
	// Capture host idle before probing the currently selected target. After
	// Soft-stop, idle settings may select an unrelated kit; that probe must
	// not hide retained grants from an explicit Stop. A Soft-stopped legacy
	// fpga_native label is idle as well: cleanup leaves that marker set.
	// Soft-stop of foreground B can still leave A playing: the coordinator
	// only sees B's idle status, so the release fallback also checks plays.
	alreadyIdle := s.idleWithoutPlay()
	if _, err := s.developmentActive(ctx); err != nil {
		if relErr := s.releaseKitLeaseAfterIdleExplicitStop(alreadyIdle, retainLease); relErr != nil {
			return sessionResult{}, relErr
		}
		return sessionResult{}, fogcast.WithStopStage(err, "development_probe")
	}
	s.ensureFlight()

	s.mu.Lock()
	priorBinding := s.inputBinding
	s.mu.Unlock()
	var inputErr, mediaErr error
	s.mu.Lock()
	hadMedia := s.mediaHandle != nil
	execution := s.execution
	packageOwned := s.packageOwned
	failedWithoutHandle := !hadMedia && s.mediaState == "failed"
	s.mu.Unlock()
	prepare := func(stopCtx context.Context) {
		if s.remoteInput != nil {
			inputErr = s.detachInputNow(stopCtx, "session_stop")
		}
		if hadMedia {
			mediaErr = s.stopMediaBounded(execution)
		}
	}
	var st protocol.Status
	var serviceErr error
	if bound, ok := s.service.(interface {
		StopExpectedWithPreparation(context.Context, fogcast.SessionStopBinding, func(context.Context)) (protocol.Status, error)
	}); expected != nil && ok {
		// The service checks its foreground binding under lifecycle admission
		// before any coordinator teardown, and holds it through physical Stop.
		if s.target != "" {
			if scoped, ok := s.service.(interface {
				StopExpectedTargetWithPreparation(context.Context, fogcast.SessionStopBinding, func(context.Context), string) (protocol.Status, error)
			}); ok {
				st, serviceErr = scoped.StopExpectedTargetWithPreparation(ctx, expected.SessionStopBinding, prepare, s.target)
			} else {
				st, serviceErr = bound.StopExpectedWithPreparation(ctx, expected.SessionStopBinding, prepare)
			}
		} else {
			st, serviceErr = bound.StopExpectedWithPreparation(ctx, expected.SessionStopBinding, prepare)
		}
	} else {
		prepare(ctx)
		if execution == fogcast.ExecutionFPGADevelopment || packageOwned {
			st, serviceErr = s.service.Stop(ctx)
		} else {
			st, serviceErr = s.stopServiceBounded()
		}
	}
	if mediaErr != nil {
		return sessionResult{}, fogcast.WithStopStage(mediaErr, "media_stop")
	}
	if serviceErr != nil {
		var failure *protocol.APIError
		if errors.As(serviceErr, &failure) && failure.Code == protocol.CodeSaveFailed && inputErr == nil && s.remoteInput != nil && priorBinding.packageID != "" {
			observation, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			observation = s.scoped(observation)
			status, statusErr := s.service.Status(observation)
			if statusErr == nil && resumedCoreDataStatus(status) && inputBindingForStatus(status) == priorBinding {
				if attachErr := s.attachInputForStatus(observation, status); attachErr != nil {
					cancel()
					return sessionResult{}, remoteInputError()
				}
			}
			cancel()
		}
		if relErr := s.releaseKitLeaseAfterIdleExplicitStop(alreadyIdle, retainLease); relErr != nil {
			return sessionResult{}, relErr
		}
		return sessionResult{}, serviceErr
	}
	if inputErr != nil {
		return sessionResult{}, fogcast.WithStopStage(remoteInputError(), "input_detach")
	}
	result := s.publicSession(st, nil)
	if hadMedia {
		result.Execution = execution
		result.Media = "stopped"
	} else if failedWithoutHandle {
		s.mu.Lock()
		if s.mediaHandle == nil && s.mediaState == "failed" {
			s.mediaState = "stopped"
		}
		s.mu.Unlock()
		result.Execution = execution
		result.Media = "stopped"
	}
	if execution == fogcast.ExecutionFPGADevelopment || packageOwned {
		s.mu.Lock()
		if s.execution == execution {
			s.execution = ""
			s.packageOwned = false
			s.nativeStoppedIdle = false
		}
		s.mu.Unlock()
	} else if st.State == protocol.StateIdle && execution == fogcast.ExecutionFPGANative {
		// Leave the legacy label in place. It is already idle for a later
		// explicit Stop, including when the newly selected target cannot be probed.
		s.mu.Lock()
		if s.execution == execution && s.mediaHandle == nil && !s.packageOwned {
			s.nativeStoppedIdle = true
		}
		s.mu.Unlock()
	}
	// Idle explicit Stop releases every kit grant retained by prior Stops,
	// including one whose client was dropped when idle settings changed
	// selected_target. Sofa Soft-stop (retain_lease) keeps those grants.
	// A stop that does not reach idle, including reboot_required, never releases.
	if st.State == protocol.StateIdle && !retainLease {
		if err := s.releaseKitLeaseNow(); err != nil {
			return sessionResult{}, err
		}
	}
	s.recordStamp("session.stop", result, nil, stamp)
	return result, nil
}

// releaseIdleGrants drops retained grants that no longer back a play.
// It does not call Service.Stop, so a promoted play keeps running.
func (s *sessionCoordinator) releaseIdleGrants(ctx context.Context, stamp clientStamp) (sessionResult, error) {
	if !s.begin() {
		return sessionResult{}, busyError()
	}
	defer s.end()
	if err := s.releaseKitLeaseNow(); err != nil {
		return sessionResult{}, err
	}
	st, err := s.service.Status(ctx)
	if err != nil {
		return sessionResult{}, err
	}
	result := s.publicSession(st, nil)
	s.recordStamp("session.lease_release", result, nil, stamp)
	return result, nil
}

func (s *sessionCoordinator) idleWithoutPlay() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mediaHandle != nil || s.packageOwned {
		return false
	}
	if s.execution == "" {
		return true
	}
	// Package Soft-stop clears its execution. Legacy non-package fpga_native
	// keeps the label after idle cleanup; that leftover marker is idle too.
	return s.nativeStoppedIdle && s.execution == fogcast.ExecutionFPGANative
}

func (s *sessionCoordinator) releaseKitLeaseNow() error {
	owner, ok := s.service.(interface{ ReleaseKitLease(context.Context) error })
	if !ok {
		return nil
	}
	releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var err error
	if s.target != "" {
		if scoped, ok := s.service.(interface {
			ReleaseKitLeaseTarget(context.Context, string) error
		}); ok {
			err = scoped.ReleaseKitLeaseTarget(releaseCtx, s.target)
		} else {
			err = owner.ReleaseKitLease(releaseCtx)
		}
	} else {
		err = owner.ReleaseKitLease(releaseCtx)
	}
	if err != nil {
		return fogcast.WithStopStage(err, "lease_release")
	}
	return nil
}

func (s *sessionCoordinator) releaseKitLeaseAfterIdleExplicitStop(alreadyIdle, retainLease bool) error {
	// alreadyIdle is the coordinator marker. Soft-stop of foreground B can
	// promote still-playing A and return only B's idle status, so the marker
	// is set while another play remains. ReleaseKitLease keeps a grant that
	// still backs a play and releases idle grants from earlier Soft-stops.
	// Skipping the call because any play remains would leave those idle
	// grants renewing after a failed explicit Stop of the survivor.
	if retainLease || !alreadyIdle {
		return nil
	}
	return s.releaseKitLeaseNow()
}

func (s *sessionCoordinator) developmentActive(ctx context.Context) (bool, error) {
	ctx = s.scoped(ctx)
	s.mu.Lock()
	execution := s.execution
	s.mu.Unlock()
	if execution == fogcast.ExecutionFPGADevelopment {
		return true, nil
	}
	if execution != "" {
		return false, nil
	}
	var development bool
	var reconstructedExecution string
	var err error
	if probe, ok := s.service.(sessionDevelopmentStateService); ok {
		development, reconstructedExecution, err = probe.DevelopmentSessionState(ctx)
	} else if probe, ok := s.service.(sessionDevelopmentService); ok {
		development, err = probe.DevelopmentActive(ctx)
	} else {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	packageOwned := false
	if s.target != "" {
		if owner, ok := s.service.(interface{ ActivePackageOwnedForTarget(string) bool }); ok {
			packageOwned = owner.ActivePackageOwnedForTarget(s.target)
		} else if owner, ok := s.service.(sessionPackageOwnerService); ok {
			packageOwned = owner.ActivePackageOwned()
		}
	} else if owner, ok := s.service.(sessionPackageOwnerService); ok {
		packageOwned = owner.ActivePackageOwned()
	}
	s.mu.Lock()
	if s.execution == "" {
		switch {
		case reconstructedExecution == fogcast.ExecutionFPGANative:
			s.execution = fogcast.ExecutionFPGANative
			s.packageOwned = packageOwned
			s.nativeStoppedIdle = false
		case development:
			s.execution = fogcast.ExecutionFPGADevelopment
			s.packageOwned = packageOwned
			s.nativeStoppedIdle = false
		}
	}
	development = s.execution == fogcast.ExecutionFPGADevelopment
	s.mu.Unlock()
	return development, nil
}

func (s *sessionCoordinator) restoreExecution(execution string) {
	s.mu.Lock()
	s.execution = execution
	if execution == "" {
		s.packageOwned = false
		s.nativeStoppedIdle = false
	}
	s.mu.Unlock()
}

func (s *sessionCoordinator) restoreNativeStoppedIdle(nativeStoppedIdle bool) {
	if !nativeStoppedIdle {
		return
	}
	s.mu.Lock()
	if s.execution == fogcast.ExecutionFPGANative && !s.packageOwned && s.mediaHandle == nil {
		s.nativeStoppedIdle = true
	}
	s.mu.Unlock()
}

func (s *sessionCoordinator) stopAfterFailedAttach(execution string) error {
	_ = s.detachInputBounded("input_attach_failed")
	mediaErr := s.stopMediaBounded(execution)
	_, stopErr := s.stopServiceBounded()
	if mediaErr != nil {
		return mediaErr
	}
	if stopErr != nil {
		return stopErr
	}
	return remoteInputError()
}

func (s *sessionCoordinator) detachInputBounded(reason string) error {
	if s.remoteInput == nil {
		return nil
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.detachInputNow(cleanupCtx, reason); err != nil {
		return remoteInputError()
	}
	return nil
}

func (s *sessionCoordinator) stopServiceBounded() (protocol.Status, error) {
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	if s.target != "" {
		stopCtx = fogcast.WithSessionTarget(stopCtx, s.target)
	}
	status, err := s.service.Stop(stopCtx)
	cancel()
	if err == nil || !ambiguousBoundedStopError(err) {
		return status, err
	}
	reconcileCtx, reconcileCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer reconcileCancel()
	if s.target != "" {
		reconcileCtx = fogcast.WithSessionTarget(reconcileCtx, s.target)
	}
	for {
		status, statusErr := s.service.Status(reconcileCtx)
		if statusErr != nil {
			return protocol.Status{}, err
		}
		if exactIdleStatus(status) {
			return status, nil
		}
		if !provisionalStopStatus(status) {
			return protocol.Status{}, err
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-reconcileCtx.Done():
			timer.Stop()
			return protocol.Status{}, err
		case <-timer.C:
		}
	}
}

func ambiguousBoundedStopError(err error) bool {
	if !errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var apiErr *protocol.APIError
	return !errors.As(err, &apiErr) || apiErr.Code == protocol.CodeMiSTerUnavailable
}

func provisionalStopStatus(status protocol.Status) bool {
	return (status.State == protocol.StateActive || status.State == protocol.StateStopping) &&
		status.LastError == nil && !status.Development && status.Recovery == ""
}

func exactIdleStatus(status protocol.Status) bool {
	return status.State == protocol.StateIdle && status.GameID == nil && status.System == nil &&
		status.ExpectedCore == nil && status.ObservedCore == nil && status.LastError == nil &&
		!status.Development && status.Recovery == ""
}

func reconstructedSessionExecution(st protocol.Status) (execution string, packageOwned bool) {
	if st.Development && st.State != protocol.StateIdle {
		if (st.State == protocol.StateActive || st.State == protocol.StateStopping) &&
			st.CorePackage != nil && fogcast.RecognizedPlayABI(st.CorePackage.ABI.ID, int64(st.CorePackage.ABI.Major), int64(st.CorePackage.ABI.Minor)) {
			return fogcast.ExecutionFPGANative, true
		}
		return fogcast.ExecutionFPGADevelopment, st.CorePackage != nil
	}
	if st.State == protocol.StateActive {
		return fogcast.ExecutionFPGANative, false
	}
	return "", false
}

// stopPlacedPlay rolls back a service launch that reached this coordinator's
// target before host-side ownership admission or handoff completed.
func (s *sessionCoordinator) stopPlacedPlay() {
	_, _ = s.stopServiceBounded()
}
