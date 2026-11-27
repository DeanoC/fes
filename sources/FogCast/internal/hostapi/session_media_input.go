package hostapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/kitlease"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

func (s *sessionCoordinator) reconcileInput(ctx context.Context, st protocol.Status) error {
	if s.remoteInput == nil {
		return nil
	}
	input := s.remoteInput.Status()
	if inputEligibleStatus(st) {
		desired := inputBindingForStatus(st)
		s.mu.Lock()
		current := s.inputBinding
		s.mu.Unlock()
		if st.LastError != nil && st.LastError.Code == protocol.CodeSaveFailed && current != desired {
			if input.State != host.RemoteInputDetached {
				return s.detachInputNow(ctx, "session_reconcile")
			}
			return nil
		}
		if current == desired && (input.State == host.RemoteInputAttached || input.State == host.RemoteInputReconnecting) {
			return nil
		}
		if input.State != host.RemoteInputDetached {
			if err := s.detachInputNow(ctx, "session_reconcile"); err != nil {
				return remoteInputReconcileError(err)
			}
		}
		if err := s.attachInputForStatus(ctx, st); err != nil {
			return remoteInputReconcileError(err)
		}
		return nil
	}
	if input.State == host.RemoteInputAttached || input.Ready || input.SessionID != "" {
		if err := s.detachInputNow(ctx, "session_reconcile"); err != nil {
			return remoteInputReconcileError(err)
		}
	}
	return nil
}

func mediaHandleDone(handle MediaHandle) bool {
	terminal, ok := handle.(interface{ Done() <-chan struct{} })
	if !ok || terminal.Done() == nil {
		return false
	}
	select {
	case <-terminal.Done():
		return true
	default:
		return false
	}
}

func (s *sessionCoordinator) watchMedia(handle MediaHandle, generation uint64, execution string) {
	terminal, ok := handle.(interface{ Done() <-chan struct{} })
	if !ok || terminal.Done() == nil {
		return
	}
	go func() {
		<-terminal.Done()
		s.observationMu.Lock()
		defer s.observationMu.Unlock()
		s.mu.Lock()
		current := s.mediaHandle == handle && s.mediaGeneration == generation && (s.mediaState == "active" || s.mediaState == "failed")
		s.mu.Unlock()
		if !current {
			return
		}
		mediaErr := s.stopMediaBounded(execution)
		st, _ := s.service.Status(s.scoped(context.Background()))
		result := s.publicSession(st, nil)
		result.Execution = execution
		result.Media = s.currentMediaState()
		if mediaErr != nil {
			s.record("session.media.exit_failed", result, nil)
			return
		}
		s.record("session.media.exit", result, nil)
	}()
}

func (s *sessionCoordinator) clearInputBinding() {
	s.mu.Lock()
	s.inputBinding = sessionInputBinding{}
	s.mu.Unlock()
}

func (s *sessionCoordinator) stopMedia(ctx context.Context, execution string) error {
	s.mu.Lock()
	handle := s.mediaHandle
	state := s.mediaState
	s.mu.Unlock()
	if handle == nil {
		return nil
	}
	parent := ctx
	if parent == nil || parent.Err() != nil {
		parent = context.Background()
	}
	cleanupCtx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	if err := handle.Stop(cleanupCtx); err != nil {
		s.mu.Lock()
		if state == "" {
			s.mediaState = "active"
		} else {
			s.mediaState = state
		}
		state = s.mediaState
		s.mu.Unlock()
		s.record("session.media.stop_failed", sessionResult{State: protocol.StateActive, Execution: execution, Media: state}, nil)
		return &protocol.APIError{Code: protocol.CodeInternal, Message: "media session could not be stopped"}
	}
	s.mu.Lock()
	s.mediaHandle = nil
	s.mediaState = "stopped"
	s.mu.Unlock()
	s.record("session.media.stop", sessionResult{State: protocol.StateIdle, Execution: execution, Media: "stopped"}, nil)
	return nil
}

func (s *sessionCoordinator) startMedia(ctx context.Context, id, execution string) error {
	if s.media == nil {
		return nil
	}
	handle, err := s.media.Start(ctx, id)
	if err != nil {
		media := "failed"
		s.mu.Lock()
		var generation uint64
		if handle != nil {
			s.mediaHandle = handle
			s.mediaGeneration++
			generation = s.mediaGeneration
		}
		s.mediaState = "failed"
		s.mu.Unlock()
		if handle != nil {
			s.watchMedia(handle, generation, execution)
		}
		s.record("session.media.failed", sessionResult{State: protocol.StateIdle, Execution: execution, Media: media}, nil)
		return err
	}
	s.mu.Lock()
	s.execution = execution
	s.mediaHandle = handle
	s.mediaState = mediaState(handle)
	s.mediaGeneration++
	generation := s.mediaGeneration
	s.mu.Unlock()
	s.watchMedia(handle, generation, execution)
	s.record("session.media.start", sessionResult{State: protocol.StateActive, Execution: execution, Media: mediaState(handle)}, nil)
	return nil
}

func (s *sessionCoordinator) stopMediaBounded(execution string) error {
	var first error
	for attempt := 0; attempt < 2; attempt++ {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := s.stopMedia(cleanupCtx, execution)
		cancel()
		if err == nil {
			return first
		}
		if first == nil {
			first = err
		}
	}
	return first
}

func (s *sessionCoordinator) inputStatus() (host.RemoteInputStatus, bool) {
	if s.remoteInput == nil {
		return host.RemoteInputStatus{}, false
	}
	return s.remoteInput.Status(), true
}

func (s *sessionCoordinator) sendCoreKey(ctx context.Context, event remoteinput.Event) error {
	// A host without remote input can mutate nothing; say so before any
	// lease or connection check can report a different reason.
	if s.remoteInput == nil {
		return errRemoteInputDisabled
	}
	if s.playHIDForeign() {
		return playHIDLeaseError()
	}
	status := s.remoteInput.Status()
	if status.State != host.RemoteInputAttached && status.State != host.RemoteInputReconnecting {
		return host.ErrRemoteInputNoStream
	}
	sender, ok := s.remoteInput.(interface {
		SendEventConfirmed(context.Context, remoteinput.Event, time.Time) error
	})
	if !ok {
		return host.ErrRemoteInputNoStream
	}
	return sender.SendEventConfirmed(ctx, event, time.Now())
}

func (s *sessionCoordinator) tapCoreKey(ctx context.Context, sessionID string, event remoteinput.Event, modifiers uint8) (time.Duration, error) {
	if s.remoteInput == nil {
		return 0, errRemoteInputDisabled
	}
	if s.tapHIDForeign() {
		return 0, playHIDLeaseError()
	}
	// Only a negotiated HID keyboard accepts physical usages unchanged. The
	// target silently drops unsupported usages on native and matrix cores.
	status, err := s.service.Status(s.scoped(ctx))
	if err != nil {
		return 0, err
	}
	if !inputEligibleStatus(status) || !protocol.KeyboardHIDCapable(status.CorePackage) {
		return 0, host.ErrRemoteInputNoStream
	}
	want := inputBindingForStatus(status)
	s.mu.Lock()
	bound := s.inputBinding == want
	s.mu.Unlock()
	if !bound {
		return 0, host.ErrRemoteInputNoStream
	}
	tapper, ok := s.remoteInput.(interface {
		TapKey(context.Context, string, uint64, remoteinput.Event, func() error) (time.Duration, error)
	})
	if !ok {
		return 0, host.ErrRemoteInputNoStream
	}
	checkOwnership := func() error {
		if s.tapHIDForeign() {
			return playHIDLeaseError()
		}
		// The input lock is held here. Binding changes happen without holding
		// s.mu while waiting for that lock, so this recheck cannot invert the
		// lock order and rejects an attachment replaced while tap was queued.
		s.mu.Lock()
		current := s.inputBinding
		s.mu.Unlock()
		if current != want || !current.keyboardHID {
			return host.ErrRemoteInputNoStream
		}
		return nil
	}
	var duration time.Duration
	if modifiers == 0 {
		duration, err = tapper.TapKey(ctx, sessionID, want.generation, event, checkOwnership)
	} else {
		chord, ok := s.remoteInput.(interface {
			TapKeyChord(context.Context, string, uint64, remoteinput.Event, uint8, func() error) (time.Duration, error)
		})
		if !ok {
			return 0, host.ErrRemoteInputNoStream
		}
		duration, err = chord.TapKeyChord(ctx, sessionID, want.generation, event, modifiers, checkOwnership)
	}
	if err != nil {
		return duration, err
	}
	if s.tapHIDForeign() {
		return duration, playHIDLeaseError()
	}
	return duration, nil
}

// Tap ownership must use the target selected by this coordinator, not the
// global foreground kit. An unknown scoped connection fails closed.
func (s *sessionCoordinator) tapHIDForeign() bool {
	var c fogcast.TargetConnection
	if s.target != "" {
		provider, ok := s.service.(interface {
			TargetConnectionForName(string) fogcast.TargetConnection
		})
		if !ok {
			return true
		}
		c = provider.TargetConnectionForName(s.target)
	} else {
		provider, ok := s.service.(interface {
			TargetConnection() fogcast.TargetConnection
		})
		if !ok {
			return true
		}
		c = provider.TargetConnection()
	}
	// Discovery reports a usable target as ready or active. Missing,
	// incompatible, or unknown observations do not establish ownership.
	return (c.State != "ready" && c.State != "active") ||
		kitlease.ForeignHID(kitlease.Status{State: c.State, Owner: c.Owner})
}

func (s *sessionCoordinator) playHIDForeign() bool {
	provider, ok := s.service.(interface {
		TargetConnection() fogcast.TargetConnection
	})
	if !ok {
		return false
	}
	c := provider.TargetConnection()
	return kitlease.ForeignHID(kitlease.Status{State: c.State, Owner: c.Owner})
}

func playHIDLeaseError() error {
	return &protocol.APIError{Code: protocol.CodeKitLeaseDenied, Message: "kit lease is foreign; HID is fail-closed"}
}

func (s *sessionCoordinator) attachInput(ctx context.Context) (sessionResult, error) {
	ctx = s.scoped(ctx)
	if s.remoteInput == nil {
		return sessionResult{}, errRemoteInputDisabled
	}
	if !s.begin() {
		return sessionResult{}, busyError()
	}
	defer s.end()

	st, err := s.service.Status(ctx)
	if err != nil {
		return sessionResult{}, err
	}
	if !inputEligibleStatus(st) {
		return sessionResult{}, remoteInputError()
	}
	core := sessionCore(st)
	if core == "" {
		return sessionResult{}, remoteInputError()
	}
	// Launch and status reconciliation can already own this exact stream.
	// Repeated explicit attachment must neither rotate it nor accept a stream
	// belonging to another package generation or capability binding.
	input := s.remoteInput.Status()
	if input.State == host.RemoteInputAttached {
		s.mu.Lock()
		binding := s.inputBinding
		s.mu.Unlock()
		if !input.Ready || binding != inputBindingForStatus(st) {
			return sessionResult{}, remoteInputError()
		}
		result := s.publicCurrentSession(st)
		s.record("session.input.attach", result, nil)
		return result, nil
	}
	if err := s.attachInputForStatus(ctx, st); err != nil {
		return sessionResult{}, remoteInputError()
	}
	result := s.publicCurrentSession(st)
	s.record("session.input.attach", result, nil)
	return result, nil
}

func (s *sessionCoordinator) detachInput(ctx context.Context) (sessionResult, error) {
	ctx = s.scoped(ctx)
	if s.remoteInput == nil {
		return sessionResult{}, errRemoteInputDisabled
	}
	if !s.begin() {
		return sessionResult{}, busyError()
	}
	defer s.end()

	if err := s.detachInputNow(ctx, "operator_detach"); err != nil {
		return sessionResult{}, remoteInputError()
	}
	st, err := s.service.Status(ctx)
	if err != nil {
		return sessionResult{}, err
	}
	result := s.publicCurrentSession(st)
	s.record("session.input.detach", result, nil)
	return result, nil
}

// Preserve an ownership refusal during read-side input reconciliation. Other
// bridge failures retain their existing sanitized unavailable response.
func remoteInputReconcileError(err error) error {
	var apiErr *protocol.APIError
	if errors.As(err, &apiErr) && (apiErr.Code == protocol.CodeKitLeaseDenied || apiErr.Code == protocol.CodeKitLeaseBusy) {
		return err
	}
	return remoteInputError()
}

// errRemoteInputDisabled reports a host whose configuration has no
// [remote_input] section enabled, so this session has no input bridge. It is
// a host configuration state, not a target failure, and must not read as
// MISTER_UNAVAILABLE or as a dropped stream.
var errRemoteInputDisabled = errors.New("remote input is not enabled in this host's configuration")

// writeRemoteInputDisabled reports whether err was errRemoteInputDisabled and,
// if so, writes its 503 INPUT_UNAVAILABLE response.
func writeRemoteInputDisabled(w http.ResponseWriter, err error) bool {
	if !errors.Is(err, errRemoteInputDisabled) {
		return false
	}
	writeError(w, http.StatusServiceUnavailable, "INPUT_UNAVAILABLE", errRemoteInputDisabled.Error())
	return true
}

func remoteInputError() error {
	return &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "remote input bridge is unavailable"}
}

func corePackageInputStatus(status protocol.Status) bool {
	return status.State == protocol.StateActive && status.Development && status.CorePackage != nil &&
		status.CorePackage.PackageID != "" && status.CorePackage.Generation != 0 && status.LastError == nil
}

func inputEligibleStatus(status protocol.Status) bool {
	if status.State != protocol.StateActive || sessionCore(status) == "" {
		return false
	}
	if !status.Development {
		return true
	}
	return (corePackageInputStatus(status) || resumedCoreDataStatus(status)) &&
		(status.CorePackage.Gamepad || corePackageHasKeyboard(status) || protocol.KeyboardHIDCapable(status.CorePackage) || protocol.MouseRelativeCapable(status.CorePackage))
}

func corePackageHasKeyboard(status protocol.Status) bool {
	if status.CorePackage == nil {
		return false
	}
	for _, contract := range status.CorePackage.ActiveInterfaces {
		if contract.ID == "fes.keyboard" && contract.Major == 1 && contract.Minor == 0 {
			return true
		}
	}
	return false
}

func inputBindingForStatus(status protocol.Status) sessionInputBinding {
	ports, keypad := protocol.ControllerPorts(status.CorePackage)
	binding := sessionInputBinding{
		core: sessionCore(status), keyboard: corePackageHasKeyboard(status),
		keyboardHID: protocol.KeyboardHIDCapable(status.CorePackage),
		gamepad:     ports, keypad: keypad, mouse: protocol.MouseRelativeCapable(status.CorePackage),
	}
	if status.CorePackage != nil {
		binding.packageID = status.CorePackage.PackageID
		binding.generation = status.CorePackage.Generation
	}
	return binding
}

func (s *sessionCoordinator) attachInputForStatus(ctx context.Context, status protocol.Status) error {
	core := sessionCore(status)
	ports, keypad := protocol.ControllerPorts(status.CorePackage)
	var err error
	if attacher, ok := s.remoteInput.(interface {
		AttachWithMouse(context.Context, string, bool, bool, bool, bool) error
	}); ok {
		err = attacher.AttachWithMouse(ctx, core, corePackageHasKeyboard(status), ports, keypad, protocol.MouseRelativeCapable(status.CorePackage))
	} else if attacher, ok := s.remoteInput.(interface {
		AttachWithControllerPorts(context.Context, string, bool, bool, bool) error
	}); ok {
		err = attacher.AttachWithControllerPorts(ctx, core, corePackageHasKeyboard(status), ports, keypad)
	} else if attacher, ok := s.remoteInput.(remoteInputCapabilityAttacher); ok {
		err = attacher.AttachWithCapabilities(ctx, core, corePackageHasKeyboard(status))
	} else {
		err = s.remoteInput.Attach(ctx, core)
	}
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.inputBinding = inputBindingForStatus(status)
	s.mu.Unlock()
	return nil
}

func (s *sessionCoordinator) detachInputNow(ctx context.Context, reason string) error {
	if err := s.remoteInput.Detach(ctx, reason); err != nil {
		return err
	}
	s.mu.Lock()
	s.inputBinding = sessionInputBinding{}
	s.mu.Unlock()
	return nil
}

func (s *sessionCoordinator) currentMediaState() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mediaState
}

func mediaState(handle MediaHandle) string {
	if handle == nil {
		return "inactive"
	}
	return "active"
}
