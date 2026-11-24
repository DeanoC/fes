package fogcast

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"reflect"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

// SessionExecution resolves the service-owned execution policy for one catalog game.
func (s *Service) SessionExecution(ctx context.Context, gameID string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := protocol.ValidateGameID(gameID); err != nil {
		return "", canonicalError(protocol.CodeBadRequest, nil)
	}
	game, err := s.catalog.Game(ctx, gameID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", canonicalError(protocol.CodeROMNotFound, nil)
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	execution, err := s.resolveExecution(ctx, game)
	if err != nil {
		return "", err
	}
	return execution, nil
}

// DevelopmentActive reconstructs development ownership from the selected
// target after a host restart, when no local execution marker exists yet.
func (s *Service) DevelopmentActive(ctx context.Context) (bool, error) {
	development, _, err := s.DevelopmentSessionState(ctx)
	return development, err
}

func (s *Service) ActivePackageOwned() bool {
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	_, play := s.selectedKitPlayLocked()
	return play.packageID != ""
}

func (s *Service) ActivePackageOwnedForTarget(target string) bool {
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	return s.plays[target].packageID != ""
}

// DevelopmentSessionState returns both development admission and execution
// ownership reconstructed by the same authoritative target observation.
func (s *Service) DevelopmentSessionState(ctx context.Context) (bool, string, error) {
	scopedTarget := SessionTargetFromContext(ctx)
	s.executionMu.Lock()
	execution := s.activeExecution
	if scopedTarget != "" {
		execution = s.plays[scopedTarget].execution
	} else {
		_, play := s.selectedKitPlayLocked()
		execution = play.execution
	}
	s.executionMu.Unlock()
	if execution == ExecutionFPGADevelopment {
		return true, execution, nil
	}
	if execution != "" {
		return false, execution, nil
	}
	if _, err := s.Status(ctx); err != nil {
		return false, "", err
	}
	s.executionMu.Lock()
	if scopedTarget != "" {
		execution = s.plays[scopedTarget].execution
	} else {
		_, play := s.selectedKitPlayLocked()
		execution = play.execution
	}
	s.executionMu.Unlock()
	return execution == ExecutionFPGADevelopment, execution, nil
}

// RecognizedPlayABI reports whether a format-2 package ABI is a normal FES
// play profile. Unknown or empty ABIs stay on the Diagnostic development path.
func RecognizedPlayABI(id string, major, minor int64) bool {
	switch id {
	case "fes.computer":
		return protocol.ComputerMediaABI(id, major, minor)
	case "fes.simple-computer", "fes.simple-game", "fes.application":
		return major == 1 && minor == 0
	default:
		return false
	}
}

func recognizedPlayContract(abi protocol.RuntimeContract) bool {
	return RecognizedPlayABI(abi.ID, int64(abi.Major), int64(abi.Minor))
}

func (s *Service) resolveExecution(ctx context.Context, game catalog.Game) (string, error) {
	if game.Kind == catalog.SourceKindCorePackage {
		if s.corePackageHasRecognizedPlayABI(ctx, game.ID) {
			return ExecutionFPGANative, nil
		}
		return ExecutionFPGADevelopment, nil
	}
	return ExecutionFPGANative, nil
}

func (s *Service) corePackageHasRecognizedPlayABI(ctx context.Context, gameID string) bool {
	if s.corePackages == nil {
		return false
	}
	store, ok := s.catalog.(coreEntryCatalog)
	if !ok {
		return false
	}
	entry, err := store.CoreEntry(ctx, gameID)
	if err != nil {
		return false
	}
	inspection, err := s.corePackages.Inspect(ctx, entry.PackageID)
	if err != nil {
		return false
	}
	abi := inspection.Descriptor.ABI
	return RecognizedPlayABI(abi.ID, abi.Major, abi.Minor)
}

func (s *Service) Launch(ctx context.Context, gameID string, progress ProgressFunc) (protocol.CachedLaunchResponse, error) {
	return s.LaunchOn(ctx, gameID, "", progress)
}

func (s *Service) LaunchOn(ctx context.Context, gameID, target string, progress ProgressFunc) (protocol.CachedLaunchResponse, error) {
	// Admit before Ensure so a canceled or malformed request never
	// starts a pull. The same two checks run before the core-entry
	// lookup when the seam is off, so that request returns before
	// catalog access. With the seam off, bind still resolves the
	// selected target under targetMu at bind time. A nil executor
	// leaves Phase 0 and Phase 1 launch unchanged. Rooms and
	// GET /api/v1/games read ReadyHere only when this seam is installed.
	if err := ctx.Err(); err != nil {
		return protocol.CachedLaunchResponse{}, err
	}
	if err := protocol.ValidateGameID(gameID); err != nil {
		return protocol.CachedLaunchResponse{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	// Admit an actual package-backed entry before placement or Ensure can
	// claim a kit or pull content. Remote-source entries already exist before
	// their media is materialized.
	if s.catalog == nil {
		return protocol.CachedLaunchResponse{}, canonicalError(protocol.CodeInternal, nil)
	}
	store, ok := s.catalog.(coreEntryCatalog)
	if !ok {
		return protocol.CachedLaunchResponse{}, s.rejectUnpackagedGame(ctx, gameID)
	}
	entry, entryErr := store.CoreEntry(ctx, gameID)
	if errors.Is(entryErr, catalog.ErrCoreEntryNotFound) {
		return protocol.CachedLaunchResponse{}, s.rejectUnpackagedGame(ctx, gameID)
	}
	if entryErr != nil {
		if ctx.Err() != nil {
			return protocol.CachedLaunchResponse{}, ctx.Err()
		}
		return protocol.CachedLaunchResponse{}, mapCoreEntryError(entryErr)
	}
	if entry.GameID != gameID {
		return protocol.CachedLaunchResponse{}, canonicalError(protocol.CodeInternal, nil)
	}
	if err := catalog.ValidateCoreEntry(entry); err != nil {
		if errors.Is(err, catalog.ErrInvalidCoreMedia) {
			return protocol.CachedLaunchResponse{}, mapCoreMediaError(err)
		}
		return protocol.CachedLaunchResponse{}, canonicalError(protocol.CodeInternal, nil)
	}
	snap, err := s.captureLaunchSnapshot(gameID, target)
	if err != nil {
		return protocol.CachedLaunchResponse{}, err
	}
	if err := s.prepareRemoteLibraryLaunch(ctx, gameID, &snap); err != nil {
		return protocol.CachedLaunchResponse{}, err
	}
	// A placement request runs Place before Ensure and before bind. A
	// selection of the bound executor is recorded. A selection of
	// another FPGA kit claims that kit and rebinds this snapshot onto
	// it. Any other selection of a different node returns
	// ErrUnboundNode and does not move the session. No request leaves
	// this path unchanged.
	snap, err = s.applyMatchingPlacement(ctx, gameID, snap)
	// A kit lease claimed for this placement is held only while this
	// launch reaches execution. Ensure failure releases it and settles
	// the flag only when that release succeeds. A failed release stays
	// unsettled so this defer can retry it. A grant the session already
	// held is not placementClaimed. A rebind that does not start
	// execution restores the previous session unless a later launch has
	// already moved it, and leaves the previous kit's lease untouched.
	// A rebind that starts execution releases that previous lease through
	// the kit lease API (#281). The release runs after claim cleanup,
	// with no Service lock held.
	var placementSettled bool
	var placementKept bool
	if snap.placementClaimed || snap.placementUndo.installedName != "" {
		if snap.placementClaimed {
			snap.placementClaimSettled = &placementSettled
		}
		if snap.placementUndo.installedName != "" {
			snap.placementKept = &placementKept
		}
		defer func() {
			if snap.placementKept != nil && !placementKept {
				s.restorePlacementSession(snap.placementUndo)
			}
			if snap.placementClaimed && !placementSettled {
				if s.releaseClaimedContentLease(snap) {
					placementSettled = true
				}
			}
			if snap.placementKept != nil && placementKept {
				s.releaseLeftPlacementLease(snap.placementUndo)
			}
		}()
	}
	if err != nil {
		return protocol.CachedLaunchResponse{}, err
	}
	if err := s.meshEnsureBeforeExecute(ctx, snap); err != nil {
		return protocol.CachedLaunchResponse{}, err
	}
	if store, ok := s.catalog.(coreEntryCatalog); ok {
		if _, err := store.CoreEntry(ctx, gameID); err == nil {
			// A core entry executes on an FPGA kit. Deny only the kit whose
			// connection is already held elsewhere; another named target does
			// not use that lease. Generation takeover stays on the kit lease API.
			if s.launchUsesForeignKit(snap) {
				return protocol.CachedLaunchResponse{}, canonicalError(protocol.CodeKitLeaseDenied, nil)
			}
			return s.launchCoreEntry(ctx, gameID, snap)
		} else if !errors.Is(err, catalog.ErrCoreEntryNotFound) {
			return protocol.CachedLaunchResponse{}, mapCoreEntryError(err)
		}
	}

	return protocol.CachedLaunchResponse{}, s.rejectUnpackagedGame(ctx, gameID)
}

// rejectUnpackagedGame distinguishes a missing title from an existing row
// which cannot run without an installed FPGA package.
func (s *Service) rejectUnpackagedGame(ctx context.Context, gameID string) error {
	_, err := s.catalog.Game(ctx, gameID)
	if errors.Is(err, sql.ErrNoRows) {
		return canonicalError(protocol.CodeROMNotFound, nil)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "FPGA launch requires an installed core package", Phase: "admission"}
}

func (s *Service) LoadDevelopmentRBF(parent context.Context, size int64, content io.Reader) (protocol.Status, error) {
	if size <= 0 || size > protocol.MaxDevelopmentRBFBytes || content == nil {
		return protocol.Status{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	ctx, cancel := serviceTimeout(parent, s.uploadTimeout)
	defer cancel()
	releaseLifecycle, err := s.acquireLifecycle(ctx)
	if err != nil {
		return protocol.Status{}, err
	}
	defer releaseLifecycle()
	s.executionMu.Lock()
	kitTarget, kitPlay := s.selectedKitPlayLocked()
	s.executionMu.Unlock()
	if kitPlay.packageRejection != nil {
		if status, err := s.stopRejectedKitCore(ctx, kitTarget); err != nil {
			return status, err
		}
	}
	if s.protocolAdmissionEnabled() {
		if _, err := s.refreshTargetAdmission(ctx); err != nil {
			return protocol.Status{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
		}
	}
	if err := s.incompatibleTargetError(); err != nil {
		return protocol.Status{}, err
	}

	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	client, ok := s.selectedClientLocked()
	if !ok {
		return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	developmentClient, ok := client.(developmentRBFClient)
	if !ok {
		return protocol.Status{}, canonicalError(protocol.CodeInternal, nil)
	}
	status, err := developmentClient.LoadDevelopmentRBF(ctx, size, content)
	if err != nil {
		targetDeadlineExpired := errors.Is(ctx.Err(), context.DeadlineExceeded) && parent.Err() == nil
		if targetDeadlineExpired && ambiguousTargetMutationError(err) {
			status, err = s.reconcileLostDevelopmentLoad(parent, client, err)
			if err != nil {
				return protocol.Status{}, err
			}
		} else {
			return protocol.Status{}, canonicalRemoteError(err, protocol.CodeTransferFailed)
		}
	}
	if !validServiceDevelopmentStatus(status) {
		return protocol.Status{}, canonicalError(protocol.CodeInternal, nil)
	}
	playTarget := s.sessionTargetNameLocked()
	s.executionMu.Lock()
	if s.configuredTargetLocked(playTarget) {
		s.plays[playTarget] = targetPlay{execution: ExecutionFPGADevelopment}
	}
	s.clearForegroundLocked()
	s.activeTarget = playTarget
	s.executionMu.Unlock()
	return status, nil
}

// LoadCore sends one package mutation without tearing down the current session
// first. Target admission therefore preserves a prior game/input session on a
// stale lease, invalid archive, or compatibility rejection.
func (s *Service) LoadCore(parent context.Context, size int64, content io.Reader) (protocol.Status, error) {
	if size <= 0 || size > corepackage.MaxArchiveSize || content == nil {
		return protocol.Status{}, corePackageRequestFailure(canonicalError(protocol.CodeBadRequest, nil))
	}
	return s.loadCore(parent, func(context.Context) (coreLoadSource, error) { return coreLoadSource{size: size, body: content}, nil })
}

func (s *Service) loadCore(parent context.Context, source func(context.Context) (coreLoadSource, error)) (protocol.Status, error) {
	ctx, cancel := serviceTimeout(parent, s.uploadTimeout)
	defer cancel()
	releaseLifecycle, err := s.acquireLifecycle(ctx)
	if err != nil {
		return protocol.Status{}, corePackageRequestFailure(err)
	}
	defer releaseLifecycle()

	return s.loadCoreLocked(ctx, parent, source)
}

// Caller holds lifecycle admission through package and optional media delivery.
func (s *Service) loadCoreLocked(ctx, parent context.Context, source func(context.Context) (coreLoadSource, error)) (result protocol.Status, resultErr error) {
	// Resolve and validate the requested immutable package/media before even a
	// recovery Stop: an invalid next launch must preserve the retained owner.
	selected, err := source(ctx)
	if err != nil {
		return protocol.Status{}, corePackageRequestFailure(err)
	}
	if selected.activationBudget > 0 {
		// ROM-linked ST programming needs the same bounded activation budget
		// with an empty drive, selected parts, or an atomic initial disk.
		// Derive from the caller, not the shorter package-upload context;
		// caller cancellation and an earlier caller deadline still win.
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(parent, max(s.uploadTimeout, selected.activationBudget))
		defer cancel()
	}
	s.executionMu.Lock()
	kitTarget, kitPlay := s.selectedKitPlayLocked()
	pendingRejection := kitPlay.packageRejection != nil
	s.executionMu.Unlock()
	if pendingRejection {
		if status, err := s.stopRejectedKitCore(ctx, kitTarget); err != nil {
			return status, err
		}
	}
	size, content := selected.size, selected.body
	if s.protocolAdmissionEnabled() {
		if _, err := s.refreshTargetAdmission(ctx); err != nil {
			return protocol.Status{}, corePackageRequestFailure(canonicalRemoteError(err, protocol.CodeMiSTerUnavailable))
		}
	}
	if err := s.incompatibleTargetError(); err != nil {
		return protocol.Status{}, corePackageRequestFailure(err)
	}
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	client, ok := s.selectedClientLocked()
	if !ok {
		return protocol.Status{}, corePackageRequestFailure(canonicalError(protocol.CodeMiSTerUnavailable, nil))
	}
	loader, ok := client.(corePackageClient)
	if !ok {
		return protocol.Status{}, corePackageRequestFailure(canonicalError(protocol.CodeUnsupportedOperation, nil))
	}
	// Core mutations may claim the kit lease inside the target client. If
	// this operation created that grant and then failed (including a target
	// UPDATE_CONFLICT), release only that newly acquired grant. A grant that
	// existed before this launch may back another live play and must survive.
	lease := kitLeaseOf(client)
	leaseWasHeld := lease != nil && lease.Held()
	defer func() {
		resultErr = releaseFailedLaunchLease(resultErr, leaseWasHeld, lease, s.kitLeaseBacksPlayLocked(lease))
	}()
	prior, err := client.Status(ctx)
	if err != nil {
		return protocol.Status{}, corePackageRequestFailure(canonicalRemoteError(err, protocol.CodeMiSTerUnavailable))
	}
	var status protocol.Status
	if selected.entry != nil {
		library, ok := client.(libraryCoreClient)
		if !ok {
			return protocol.Status{}, corePackageRequestFailure(canonicalError(protocol.CodeUnsupportedOperation, nil))
		}
		if retainedIdleLaunchRecoveryCandidate(prior) {
			if err := s.validateLibraryCoreBeforeRecovery(ctx, selected.entry.PackageID, client); err != nil {
				return protocol.Status{}, err
			}
		}
		prior, err = s.recoverIdleLaunchError(ctx, client, prior)
		if err != nil {
			return protocol.Status{}, err
		}
		if selected.partsComposition != nil && selected.romID == "" {
			parts, ok := client.(interface {
				LoadLibraryPartsCore(context.Context, int64, io.Reader, string) (protocol.Status, error)
			})
			if !ok {
				return protocol.Status{}, corePackageRequestFailure(canonicalError(protocol.CodeUnsupportedOperation, nil))
			}
			status, err = parts.LoadLibraryPartsCore(ctx, size, content, selected.entry.PackageID)
		} else if selected.staticComposition != nil || selected.composition != nil || (selected.slotComposition != nil && selected.romID == "") {
			composed, ok := client.(interface {
				LoadComposedCore(context.Context, int64, io.Reader, string) (protocol.Status, error)
			})
			if !ok {
				return protocol.Status{}, corePackageRequestFailure(canonicalError(protocol.CodeUnsupportedOperation, nil))
			}
			status, err = composed.LoadComposedCore(ctx, size, content, selected.entry.PackageID)
		} else if selected.machine != nil {
			machine, ok := client.(machineCoreClient)
			if !ok {
				return protocol.Status{}, corePackageRequestFailure(canonicalError(protocol.CodeUnsupportedOperation, nil))
			}
			if selected.hardDisk != nil {
				if err := stageHardDisk(ctx, machine, selected.hardDisk); err != nil {
					return protocol.Status{}, corePackageRequestFailure(err)
				}
			}
			status, err = machine.LoadLibraryMachineCore(ctx, size, content, selected.entry.PackageID, *selected.machine)
		} else {
			status, err = library.LoadLibraryCore(ctx, size, content, selected.entry.PackageID)
		}
	} else {
		status, err = loader.LoadCore(ctx, size, content)
	}
	if err != nil {
		if corePackagePreMutationFailure(err) {
			return protocol.Status{}, preserveCorePackageError(retainedAdmissionDiagnostic(prior, err))
		}
		status, err = s.reconcileLostCoreLoad(parent, client, prior, err)
		if err != nil {
			if confirmedIdleCorePackageFailure(status, err) {
				return s.retireExecutionAfterConfirmedIdleCorePackageFailure(ctx, status, err)
			}
			return status, err
		}
	}
	if !validServiceCorePackageStatus(status) {
		return protocol.Status{}, canonicalError(protocol.CodeInternal, nil)
	}

	if selected.entry != nil && (status.CorePackage.PackageID != selected.entry.PackageID || !selected.matchesLoadedIdentity(status.CorePackage)) {
		rejection := &protocol.APIError{Code: protocol.CodeUnrecognizedCore, Message: "activated package differs from selected library package", Phase: "identity", Expected: selected.entry.PackageID, Observed: status.CorePackage.PackageID}
		recoveryCtx, recoveryCancel := serviceTimeout(parent, s.coreLoadReconcileTimeout)
		recovered, stopErr := client.Stop(recoveryCtx)
		if stopErr != nil || !validRecoveredDevelopmentStatus(recovered) || recovered.CorePackage != nil {
			recovered, stopErr = client.Status(recoveryCtx)
		}
		recoveryCancel()
		if stopErr == nil && validRecoveredDevelopmentStatus(recovered) && recovered.CorePackage == nil {
			recovered.LastError = rejection
			return s.retireExecutionAfterConfirmedIdleCorePackageFailure(ctx, recovered, rejection)
		}

		keepTarget := s.kitBindToKeepLocked()
		if keepTarget == "" {
			keepTarget = s.selectedTarget
		}
		s.executionMu.Lock()
		s.plays[keepTarget] = targetPlay{execution: ExecutionFPGADevelopment, packageRejection: rejection}
		s.activeTarget = keepTarget
		s.executionMu.Unlock()
		status.LastError = rejection
		return status, rejection
	}
	keepTarget := s.kitBindToKeepLocked()
	if keepTarget == "" {
		keepTarget = s.selectedTarget
	}
	s.executionMu.Lock()
	// Kit ownership is always per target.
	play := targetPlay{execution: ExecutionFPGADevelopment}
	if selected.entry != nil && recognizedPlayContract(status.CorePackage.ABI) {
		play.execution = ExecutionFPGANative
		play.gameID, play.system, play.coreID = selected.entry.GameID, catalog.CorePlatform, selected.entry.CoreID
		play.packageID, play.packageGeneration = selected.entry.PackageID, status.CorePackage.Generation
	}
	status = attachSavedCoreID(status, play)
	s.plays[keepTarget] = play
	s.clearForegroundLocked()
	s.activeTarget = keepTarget
	if selected.entry != nil && status.CorePackage.PackageID == selected.entry.PackageID {
		status.GameID = stringPtr(selected.entry.GameID)
		status.System = systemPtr(catalog.CorePlatform)
	}
	s.executionMu.Unlock()
	return status, nil
}

type failedLaunchLease interface {
	Held() bool
	Release(context.Context) error
}

func releaseFailedLaunchLease(launchErr error, wasHeld bool, lease failedLaunchLease, backsLivePlay bool) error {
	if launchErr == nil || wasHeld || lease == nil || !lease.Held() || backsLivePlay {
		return launchErr
	}
	releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return errors.Join(launchErr, lease.Release(releaseCtx))
}

func (s *Service) retireExecutionAfterConfirmedIdleCorePackageFailure(ctx context.Context, status protocol.Status, loadErr error) (protocol.Status, error) {
	s.executionMu.Lock()
	kitTarget, _ := s.selectedKitPlayLocked()
	delete(s.plays, kitTarget)
	if s.activeTarget == kitTarget || s.activeTarget == "" {
		s.clearForegroundLocked()
		s.activeTarget = ""
	}
	if s.boundKitTarget == kitTarget {
		s.boundKitTarget = ""
	}
	if kitTarget == s.selectedTarget {
		s.selectedTargetReconciled = true
		s.selectedTargetRepairAllowed = false
	}
	s.executionMu.Unlock()
	return status, loadErr
}

func corePackageRequestFailure(err error) error {
	var apiErr *protocol.APIError
	if errors.As(err, &apiErr) {
		copy := *apiErr
		copy.Phase = "request"
		return &copy
	}
	return &protocol.APIError{Code: protocol.CodeInternal, Message: "core package request failed before dispatch", Phase: "request"}
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

func (s *Service) reconcileLostCoreLoad(parent context.Context, client serviceClient, prior protocol.Status, loadErr error) (protocol.Status, error) {
	reconcileContext, cancelReconcile := context.WithTimeout(parent, s.coreLoadReconcileTimeout)
	defer cancelReconcile()
	for {
		if err := reconcileContext.Err(); err != nil {
			return protocol.Status{}, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "core package activation outcome is unavailable", Phase: "recovery"}
		}
		statusContext, cancel := serviceTimeout(reconcileContext, s.requestTimeout)
		status, err := client.Status(statusContext)
		cancel()
		if err != nil {
			return protocol.Status{}, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "core package activation outcome is unavailable", Phase: "recovery"}
		}
		if errors.Is(loadErr, targetclient.ErrKitLeaseLost) {
			// Expiry cleanup can follow a completed load. Observing that load
			// does not restore authority or justify another activation.
			if validRecoveredDevelopmentStatus(status) && status.CorePackage == nil {
				return status, loadErr
			}
			return protocol.Status{}, loadErr
		}
		if validServiceCorePackageStatus(status) && !reflect.DeepEqual(status, prior) {
			return status, nil
		}
		if confirmedIdleCorePackageFailure(status, loadErr) {
			return status, preserveCorePackageError(loadErr)
		}
		if reflect.DeepEqual(status, prior) {
			preserved := preserveCorePackageError(loadErr)
			var apiErr *protocol.APIError
			if errors.As(preserved, &apiErr) {
				apiErr.Phase = "request"
			}
			return protocol.Status{}, preserved
		}
		if status.State != protocol.StateLaunching || !status.Development || status.LastError != nil || status.Recovery != "" {
			return protocol.Status{}, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "core package activation outcome is inconsistent", Phase: "recovery"}
		}
		timer := time.NewTimer(lostLaunchPollInterval)
		select {
		case <-reconcileContext.Done():
			timer.Stop()
			return protocol.Status{}, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "core package activation outcome is unavailable", Phase: "recovery"}
		case <-timer.C:
		}
	}
}

func confirmedIdleCorePackageFailure(status protocol.Status, loadErr error) bool {
	if errors.Is(loadErr, targetclient.ErrKitLeaseLost) {
		return validRecoveredDevelopmentStatus(status) && status.CorePackage == nil
	}
	if ambiguousTargetMutationError(loadErr) {
		return false
	}
	var dispatched *protocol.APIError
	if !errors.As(loadErr, &dispatched) || status.LastError == nil {
		return false
	}
	return status.State == protocol.StateIdle && status.GameID == nil && status.System == nil &&
		status.ExpectedCore == nil && status.ObservedCore == nil && !status.Development &&
		status.Recovery == "" && status.CorePackage == nil &&
		status.LastError.Code == dispatched.Code && status.LastError.Phase == dispatched.Phase &&
		status.LastError.Expected == dispatched.Expected && status.LastError.Observed == dispatched.Observed
}

func preserveCorePackageError(err error) error {
	var apiErr *protocol.APIError
	if errors.As(err, &apiErr) {
		copy := *apiErr
		return &copy
	}
	return canonicalRemoteError(err, protocol.CodeTransferFailed)
}

func validServiceDevelopmentStatus(status protocol.Status) bool {
	return status.State == protocol.StateActive && status.Development && status.GameID == nil && status.System == nil &&
		status.ExpectedCore == nil && (status.ObservedCore == nil || *status.ObservedCore != "") && status.LastError == nil && status.Recovery == ""
}

func validServiceCorePackageStatus(status protocol.Status) bool {
	return status.State == protocol.StateActive && status.Development && status.GameID == nil &&
		status.System == nil && status.ExpectedCore == nil && status.LastError == nil &&
		status.Recovery == "" && status.CorePackage != nil &&
		status.CorePackage.PackageID != "" && status.CorePackage.Generation != 0 &&
		status.CorePackage.ABI.ID != "" && status.CorePackage.ABI.Major != 0 &&
		status.CorePackage.BuildID != ""
}

func (s *Service) reconcileLostDevelopmentLoad(parent context.Context, client serviceClient, loadErr error) (protocol.Status, error) {
	for {
		if err := parent.Err(); err != nil {
			return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, safeContextError(err))
		}
		statusContext, cancelStatus := serviceTimeout(parent, s.requestTimeout)
		status, err := client.Status(statusContext)
		cancelStatus()
		if err != nil {
			return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, safeContextError(err))
		}
		if validServiceDevelopmentStatus(status) {
			return status, nil
		}
		if !provisionalLostDevelopmentLoad(status) {
			if status.LastError != nil {
				return protocol.Status{}, canonicalRemoteError(errors.Join(status.LastError, loadErr), protocol.CodeMiSTerUnavailable)
			}
			return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, safeContextError(loadErr))
		}
		timer := time.NewTimer(lostLaunchPollInterval)
		select {
		case <-parent.Done():
			timer.Stop()
			return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, safeContextError(parent.Err()))
		case <-timer.C:
		}
	}
}

func provisionalLostDevelopmentLoad(status protocol.Status) bool {
	if validRecoveredDevelopmentStatus(status) {
		return true
	}
	return status.State == protocol.StateLaunching && status.Development && status.GameID == nil && status.System == nil &&
		status.ExpectedCore == nil && status.ObservedCore == nil && status.LastError == nil && status.Recovery == ""
}
