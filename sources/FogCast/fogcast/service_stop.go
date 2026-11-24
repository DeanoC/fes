package fogcast

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

func (s *Service) Stop(parent context.Context) (protocol.Status, error) {
	if name := SessionTargetFromContext(parent); name != "" {
		return s.StopTarget(parent, name)
	}
	if targetID := PairedTargetFromContext(parent); targetID != "" {
		if name := s.targetNameForID(targetID); name != "" {
			return s.StopTarget(parent, name)
		}
	}
	return s.stopExpected(parent, nil, nil, "")
}

// StopTarget stops one configured target's play while preserving the default
// foreground selection used by unscoped Stop and Status calls.
func (s *Service) StopTarget(parent context.Context, target string) (protocol.Status, error) {
	if strings.TrimSpace(target) == "" {
		return protocol.Status{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	return s.stopExpected(parent, nil, nil, target)
}

// StopExpected preserves the ordinary Stop/save lifecycle while refusing to
// stop a different foreground play from the one the caller observed.
func (s *Service) StopExpected(parent context.Context, expected SessionStopBinding) (protocol.Status, error) {
	return s.stopExpected(parent, &expected, nil, "")
}

func (s *Service) StopExpectedTargetWithPreparation(parent context.Context, expected SessionStopBinding, prepare func(context.Context), target string) (protocol.Status, error) {
	return s.stopExpected(parent, &expected, prepare, target)
}

// StopExpectedWithPreparation admits the bound play before coordinator input
// and media teardown. Preparation must not call a lifecycle service method:
// lifecycle admission stays held through preparation and the physical Stop.
func (s *Service) StopExpectedWithPreparation(parent context.Context, expected SessionStopBinding, prepare func(context.Context)) (protocol.Status, error) {
	return s.stopExpected(parent, &expected, prepare, "")
}

func (s *Service) stopExpected(parent context.Context, expected *SessionStopBinding, prepare func(context.Context), target string) (protocol.Status, error) {
	s.executionMu.Lock()
	activeExecution := s.activeExecution
	kitTarget := target
	if kitTarget == "" {
		kitTarget, _ = s.selectedKitPlayLocked()
	}
	kitPlay := s.plays[kitTarget]
	pendingRejection := kitPlay.packageRejection != nil
	s.executionMu.Unlock()
	timeout := s.requestTimeout
	if activeExecution == ExecutionFPGANative || activeExecution == ExecutionFPGADevelopment || kitPlay.execution == ExecutionFPGANative || kitPlay.execution == ExecutionFPGADevelopment || pendingRejection {
		timeout = s.uploadTimeout
	}
	ctx, cancel := serviceTimeout(parent, timeout)
	defer cancel()
	releaseLifecycle, err := s.acquireLifecycle(ctx)
	if err != nil {
		return protocol.Status{}, WithStopStage(err, "lifecycle")
	}
	defer releaseLifecycle()
	if target != "" {
		s.targetMu.RLock()
		valid := targetByName(s.targets, target).Name != "" && targetByName(s.targets, target).Enabled
		s.targetMu.RUnlock()
		if !valid {
			return protocol.Status{}, canonicalError(protocol.CodeBadRequest, nil)
		}
	}
	if expected != nil && !s.matchesStopBinding(*expected) {
		return protocol.Status{}, ErrSessionChanged
	}
	if prepare != nil {
		prepare(ctx)
	}
	return s.stopLocked(ctx, parent, timeout, target)
}

// Caller holds lifecycle admission.
func (s *Service) stopLocked(ctx, parent context.Context, timeout time.Duration, scopedTarget string) (result protocol.Status, resultErr error) {
	stage := "admission"
	defer func() { resultErr = WithStopStage(resultErr, stage) }()
	s.executionMu.Lock()
	activeExecution := s.activeExecution
	kitTarget := scopedTarget
	if kitTarget == "" {
		kitTarget, _ = s.selectedKitPlayLocked()
	}
	kitPlay := s.plays[kitTarget]
	pendingRejection := kitPlay.packageRejection != nil
	describedPackage := kitPlay.packageID != ""
	s.executionMu.Unlock()
	if pendingRejection {
		stage = "development_recovery"
		status, err := s.stopRejectedKitCoreWithAdmission(ctx, true, kitTarget)
		if err == nil {
			s.executionMu.Lock()
			if s.activeTarget == kitTarget {
				s.activeTarget = ""
			}
			if s.boundKitTarget == kitTarget {
				s.boundKitTarget = ""
			}
			s.executionMu.Unlock()
		}
		return status, err
	}
	// Only foreground discovery admission supplies this target's cached Status.
	// A scoped sibling is admitted through its own client instead.
	scopedOtherKit := scopedTarget != "" && scopedTarget != s.SessionTargetName()
	discoveryAdmission := !scopedOtherKit && s.discoveryEnabled()
	protocolClient := s.protocolAdmissionEnabled()
	if scopedTarget != "" {
		s.targetMu.Lock()
		client, available := s.targetClientLocked(scopedTarget)
		_, protocolClient = client.(*targetclient.Client)
		protocolClient = protocolClient && available
		s.targetMu.Unlock()
	}
	if protocolClient {
		admit := s.refreshStopAdmission
		admitCtx := ctx
		if scopedOtherKit {
			admit = s.refreshStopAdmission
			admitCtx = WithSessionTarget(ctx, scopedTarget)
		}
		if _, err := admit(admitCtx); err != nil {
			return protocol.Status{}, stopAdmissionError(err)
		}
	}

	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	var client serviceClient
	var ok bool
	if scopedTarget != "" {
		client, ok = s.targetClients[scopedTarget]
		ok = client != nil
	} else {
		client, ok = s.selectedClientLocked()
	}
	if !ok {
		return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	// Preserve the exact ownership used for this Stop even when its response
	// is lost and the public session layer later reconciles idle via Status.
	// Keep earlier grants too: an idle selected-target change rebuilds
	// targetClients and would otherwise drop the only reference a later
	// explicit release uses.
	s.executionMu.Lock()
	if leased, ok := client.(interface{ KitLease() *targetclient.KitLease }); ok {
		lease := leased.KitLease()
		s.stoppedKitLeases = retainStoppedKitLease(s.stoppedKitLeases, lease)
		if lease != nil {
			if s.stoppedKitLeaseTargets == nil {
				s.stoppedKitLeaseTargets = make(map[*targetclient.KitLease]string)
			}
			s.stoppedKitLeaseTargets[lease] = kitTarget
		}
	}
	s.executionMu.Unlock()
	stage = "target_stop"
	var status protocol.Status
	var err error
	idleWithoutLease := false
	if leased, ok := client.(interface{ KitLease() *targetclient.KitLease }); ok {
		if lease := leased.KitLease(); lease != nil && !lease.Held() {
			// A launch rejected before dispatch has no grant to stop with.
			// Confirm clean idle without claiming the kit or mutating a peer's
			// session. Unreachable, active or recovery states still fail closed.
			status, err = client.Status(ctx)
			idleWithoutLease = err == nil && validRecoveredDevelopmentStatus(status) && status.CorePackage == nil
		}
	}
	if !idleWithoutLease {
		// Reuse discovery admission's existing Status. Legacy address-only
		// Stop never acquires a new status dependency; a remembered package
		// may be observed directly while lifecycle admission is held.
		boundDisk := discoveryAdmission && s.TargetConnection().persistentMediaBound
		saving, canSave := client.(interface {
			StopWithMediaSave(context.Context) (protocol.Status, error)
		})
		if describedPackage && !discoveryAdmission {
			observed, observeErr := client.Status(ctx)
			boundDisk = observeErr == nil && protocol.PersistentMediaBound(observed.CorePackage)
		}
		if boundDisk {
			if timeout < 150*time.Second {
				timeout = 150 * time.Second
			}
			var cancel context.CancelFunc
			ctx, cancel = serviceTimeout(parent, timeout)
			defer cancel()
		}
		if saveErr := s.rememberZX81Program(ctx, client, kitPlay); saveErr != nil {
			return protocol.Status{}, saveErr
		}
		if canSave && boundDisk {
			status, err = saving.StopWithMediaSave(ctx)
		} else {
			status, err = client.Stop(ctx)
		}
	}
	if err != nil {
		targetDeadlineExpired := errors.Is(ctx.Err(), context.DeadlineExceeded) && parent.Err() == nil
		if targetDeadlineExpired && ambiguousTargetMutationError(err) && activeExecution != ExecutionFPGADevelopment {
			stage = "reconcile_stop"
			status, err = s.reconcileLostStop(parent, client, err, timeout)
			if err != nil {
				return protocol.Status{}, err
			}
		} else {
			return protocol.Status{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
		}
	}
	if status.State == protocol.StateStopping && status.Development && status.Recovery == protocol.RecoveryRebootRequired {
		stage = "development_recovery"
		recoveryClient, ok := client.(developmentRecoveryClient)
		if !ok {
			return protocol.Status{}, canonicalError(protocol.CodeInternal, nil)
		}
		health, healthErr := client.Health(ctx)
		if healthErr != nil || health.BootID == "" {
			return protocol.Status{}, canonicalRemoteError(healthErr, protocol.CodeMiSTerUnavailable)
		}
		recovered, rebootErr := recoveryClient.RebootDevelopment(ctx)
		// An agent error means recover_idle failed closed and no reboot was
		// armed. Return it now. A lost transport reply can still mean a reboot
		// is in flight, so that case keeps waiting for a new boot id.
		var closed *protocol.APIError
		if rebootErr != nil && errors.As(rebootErr, &closed) {
			return protocol.Status{}, canonicalRemoteError(rebootErr, protocol.CodeMiSTerUnavailable)
		}
		if validRecoveredDevelopmentStatus(recovered) {
			status = recovered
		} else {
			s.targetMu.RUnlock()
			status, err = waitForDevelopmentRecovery(ctx, client, health.BootID, s.developmentRecoveryHealth(client, health.BootID))
			s.targetMu.RLock()
			if err != nil {
				return protocol.Status{}, err
			}
		}
	}
	s.executionMu.Lock()
	if status.State == protocol.StateIdle {
		stoppedTarget := kitTarget
		if stoppedTarget != "" {
			delete(s.plays, stoppedTarget)
		}
		if s.activeTarget == stoppedTarget || s.activeTarget == "" {
			s.clearForegroundLocked()
			s.activeTarget = ""
		}
		if s.boundKitTarget == stoppedTarget {
			s.boundKitTarget = ""
		}
	}
	if kitTarget == s.selectedTarget {
		s.selectedTargetReconciled = status.State == protocol.StateIdle
		s.selectedTargetRepairAllowed = false
	}
	s.executionMu.Unlock()
	if s.targetOrigin != nil {
		s.executionMu.Lock()
		stoppedTarget := scopedTarget
		s.executionMu.Unlock()
		if stoppedTarget == "" {
			stoppedTarget = s.selectedTarget
		}
		hook, cfg, lease := s.targetOrigin, targetByName(s.targets, stoppedTarget), kitLeaseOf(s.targetClients[stoppedTarget])
		s.targetMu.RUnlock()
		hook(cfg, lease)
		s.targetMu.RLock()
	}
	return status, nil
}

func (s *Service) reconcileLostStop(parent context.Context, client serviceClient, stopErr error, timeout time.Duration) (protocol.Status, error) {
	ctx, cancel := serviceTimeout(parent, timeout)
	defer cancel()
	for {
		status, err := client.Status(ctx)
		if err != nil {
			return protocol.Status{}, canonicalRemoteError(errors.Join(stopErr, err), protocol.CodeMiSTerUnavailable)
		}
		if validRecoveredDevelopmentStatus(status) {
			return status, nil
		}
		if !provisionalLostStop(status) {
			if status.LastError != nil {
				return protocol.Status{}, canonicalRemoteError(errors.Join(stopErr, status.LastError), protocol.CodeMiSTerUnavailable)
			}
			return protocol.Status{}, canonicalRemoteError(stopErr, protocol.CodeMiSTerUnavailable)
		}
		timer := time.NewTimer(lostLaunchPollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return protocol.Status{}, canonicalRemoteError(errors.Join(stopErr, ctx.Err()), protocol.CodeMiSTerUnavailable)
		case <-timer.C:
		}
	}
}

func provisionalLostStop(status protocol.Status) bool {
	return (status.State == protocol.StateActive || status.State == protocol.StateStopping) &&
		status.LastError == nil && !status.Development && status.Recovery == ""
}

func waitForDevelopmentRecovery(ctx context.Context, client serviceClient, previousBootID string, poll ...func(context.Context) (protocol.Health, error)) (protocol.Status, error) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	healthPoll := client.Health
	if len(poll) > 0 {
		healthPoll = poll[0]
	}
	for {
		health, healthErr := healthPoll(ctx)
		if healthErr == nil && health.Ready && health.BootID != "" && health.BootID != previousBootID {
			status, statusErr := client.Status(ctx)
			if statusErr == nil && validRecoveredDevelopmentStatus(status) {
				return status, nil
			}
		}
		select {
		case <-ctx.Done():
			return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, safeContextError(ctx.Err()))
		case <-ticker.C:
		}
	}
}

func validRecoveredDevelopmentStatus(status protocol.Status) bool {
	return status.State == protocol.StateIdle && !status.Development && status.Recovery == "" &&
		status.GameID == nil && status.System == nil && status.ExpectedCore == nil && status.ObservedCore == nil && status.LastError == nil
}
