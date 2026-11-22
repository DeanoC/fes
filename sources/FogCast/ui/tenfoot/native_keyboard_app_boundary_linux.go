//go:build linux

package tenfoot

import (
	"errors"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/internal/playhid"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/localfeed"
)

func nativeGuestHIDUsage(code uint16) uint8 {
	if code == nativeKeyF12 {
		return 0
	}
	return nativeHIDUsage(code)
}

// Guest authority is useful only while the core owns keyboard focus. UI and
// Stop stay local; losing focus retires the whole acknowledged source.
func (a *App) nativeKeyboardGuestFocusLocked() bool {
	return !a.hostAwaitStatus && !a.kitHostPreparing && !a.searchOpen && !a.nameEntryOpenLocked() && !a.settingsOSKOpenLocked() && a.forwardsPlayHIDLocked() && !a.playHIDInputBlockedLocked()
}

func (a *App) nativeKeyboardGuestShortcutLocked(code uint16) bool {
	if code == nativeKeyF12 {
		return true
	}
	if code == 1 && a.consumePlayEscapeLocked("escape") {
		return true
	}
	if code != 102 || a.session.State != "active" || a.sessionDisplayBusy || a.stopPhase == "stopping" || a.retryStopLock || a.tapePickerOpen || a.firmwarePickerOpen || a.roomDuringPlay {
		return false
	}
	return a.playingHardwareRoomAvailableLocked() || a.colecoSessionLocked() || a.nativeUIRaw.ctrlHeld()
}

// nativeKeyboardBindingLocked observes the current App projection, never a
// remembered source binding relabelled with a newer runtime or lease.
func (a *App) nativeKeyboardBindingLocked(publisher [16]byte, epoch uint64) (nativeGuestBinding, error) {
	c := a.health.Connection
	now := time.Now()
	fresh := func(observed time.Time) bool {
		return !observed.IsZero() && !now.Before(observed) && now.Sub(observed) <= 5*time.Second
	}
	if !a.kitHost || a.client == nil || a.client.paired || !a.delegatedV2Active || a.nativeKeyboardClosing || a.nativeKeyboardGenerationExhausted || a.nativeKeyboardGeneration == 0 || publisher == ([16]byte{}) || epoch == 0 ||
		!a.nativeKeyboardGuestFocusLocked() ||
		!a.healthHave || !fresh(a.healthObservedAt) || !fresh(a.sessionObservedAt) || a.hostUnreachable || !a.health.Ready || !a.health.TargetReachable ||
		!a.kitLeaseHave || a.kitLease.Unavailable || !localhid.ValidRecoveryIdentity(c.TargetID) || !localhid.ValidRecoveryIdentity(c.BootID) ||
		!fullLocalIdentity(c.LeaseGeneration) || c.LeaseGeneration != strings.ToLower(c.LeaseGeneration) || c.LeaseGeneration == strings.Repeat("0", 64) || a.kitLease.Generation != c.LeaseGeneration ||
		a.session.State != "active" || (!a.session.CoreKeyboard && !a.session.CoreKeyboardHID) || a.session.TargetID != c.TargetID || a.session.RuntimeInput == nil || a.session.RuntimeInput.Validate() != nil || !protocol.ValidInputAgentInstance(a.session.InputAgentInstance) {
		return nativeGuestBinding{}, errNativeGuestState
	}
	identity := a.session.RuntimeInput
	if (identity.Kind == protocol.RuntimeInputDescribed && (a.session.CorePackage == nil || a.session.CorePackage.PackageID != identity.PackageID || a.session.CorePackage.Generation != identity.Generation)) ||
		(identity.Kind == protocol.RuntimeInputDevelopment && (!a.session.Development || a.session.CorePackage != nil)) {
		return nativeGuestBinding{}, errNativeGuestState
	}
	return nativeGuestBinding{Runtime: *a.session.RuntimeInput, TargetID: c.TargetID, BootID: c.BootID, InputAgentInstance: a.session.InputAgentInstance, LeaseGeneration: c.LeaseGeneration, Publisher: publisher, QueueEpoch: epoch, AuthorityGeneration: a.nativeKeyboardGeneration}, nil
}

// BeginShutdown fences new recovery, snapshots and guest edges before input
// retirement; a live worker can still neutralize its exact old token.
func (a *App) nativeKeyboardBeginShutdown() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.nativeKeyboardClosing = true
	a.nativePadRevokeConnectionLocked()
	a.requestNativeKeyboardQuarantineLocked(nativeFaultAuthority)
}

// Prepare allocates a never-reused queue epoch; no source is granted here.
func (a *App) nativeKeyboardPrepareBinding(publisher [16]byte) (nativeGuestBinding, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if (a.nativeGuest.phase != nativeGuestIdle && a.nativeGuest.phase != nativeGuestUncertain) || a.nativeGuestBeginRequest != nil || a.nativeGuestSnapshotAllowed || a.nativeGuestQueueEpoch >= ^uint64(0)-1 {
		return nativeGuestBinding{}, errNativeGuestState
	}
	current, err := a.nativeKeyboardBindingLocked(publisher, a.nativeGuestQueueEpoch+1)
	if err != nil || current.AuthorityGeneration <= a.nativeGuest.lastGeneration {
		return nativeGuestBinding{}, errNativeGuestState
	}
	if a.nativeGuest.phase == nativeGuestUncertain && (a.nativeGuest.binding.TargetID != current.TargetID || a.nativeGuest.binding.BootID != current.BootID || !protocol.ValidInputAgentInstance(a.nativeGuest.binding.InputAgentInstance)) {
		return nativeGuestBinding{}, errNativeGuestState
	}
	a.nativeGuestQueueEpoch = current.QueueEpoch
	a.nativeGuestBinding = current
	return current, nil
}

func (a *App) nativeKeyboardObserveBinding() (nativeGuestBinding, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.nativeKeyboardBindingLocked(a.nativeGuestBinding.Publisher, a.nativeGuestBinding.QueueEpoch)
}

// PermitAfterRecovery consumes no network data itself. The caller must pass
// only the exact correlated reply obtained from its still-owned recovery
// transport. It never clears guest quarantine or marks the keyboard ready.
func (a *App) nativeKeyboardPermitAfterRecovery(expected nativeGuestBinding, previousAgent string, reply localhid.Reply) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.nativeKeyboardPermitAfterRecoveryLocked(expected, previousAgent, reply)
}

func (a *App) nativeKeyboardPermitAfterRecoveryLocked(expected nativeGuestBinding, previousAgent string, reply localhid.Reply) error {
	current, err := a.nativeKeyboardBindingLocked(expected.Publisher, expected.QueueEpoch)
	request := localhid.Request{Version: 2, Op: "recover", Request: reply.Request, AgentInstance: previousAgent, TargetID: expected.TargetID, BootID: expected.BootID}
	if err != nil || current != expected || a.nativeGuestBinding != expected || (a.nativeGuest.phase != nativeGuestIdle && a.nativeGuest.phase != nativeGuestUncertain) || a.nativeGuestBeginRequest != nil || a.nativeGuestSnapshotAllowed ||
		(reply.Status != "reader-drained" && reply.Status != "startup-retired") || (reply.Status == "startup-retired" && !a.managedKitSoftware) || reply.AgentInstance != current.InputAgentInstance || localhid.CorrelateRecoveryReply(reply, request) != nil {
		return errNativeGuestState
	}
	if a.nativeGuest.phase == nativeGuestUncertain {
		if a.nativeGuest.binding.InputAgentInstance != previousAgent || a.nativeGuest.binding.TargetID != expected.TargetID || a.nativeGuest.binding.BootID != expected.BootID {
			return errNativeGuestState
		}
		generation, serial := a.nativeGuest.lastGeneration, a.nativeGuest.serial
		a.nativeGuest = nativeGuestLifecycle{lastGeneration: generation, serial: serial}
	}
	a.nativeGuestSnapshotAllowed = true
	return nil
}

// TransportLost fences only the exact source that used that connection. EOF
// is never a neutral acknowledgment; same-socket RECOVER must retire it.
func (a *App) nativeKeyboardTransportLost(expected nativeGuestBinding) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.nativeGuestBinding != expected {
		return false
	}
	if a.nativeGuest.phase == nativeGuestIdle {
		// Recovery was acknowledged on this connection but no source BEGIN
		// was sent. A successor connection still needs its own RECOVER.
		a.nativeGuestSnapshotAllowed = false
		a.requestNativeKeyboardQuarantineLocked(nativeFaultAuthority)
		return true
	}
	if a.nativeGuest.phase == nativeGuestUncertain && a.nativeGuest.binding == expected {
		a.requestNativeKeyboardQuarantineLocked(nativeFaultAuthority)
		return true
	}
	if a.nativeGuest.binding != expected || (a.nativeGuest.phase != nativeGuestBeginning && a.nativeGuest.phase != nativeGuestReady && a.nativeGuest.phase != nativeGuestNeedsReset && a.nativeGuest.phase != nativeGuestResetting) {
		return false
	}
	a.nativeGuest.uncertain()
	a.nativeGuestBeginRequest = nil
	a.requestNativeKeyboardQuarantineLocked(nativeFaultAuthority)
	return true
}

// AuthorityLost fences only the exact prepared source. A completed pending
// edge can still transition to same-token RESET; this method never claims
// neutral and never posts input while holding App.mu.
func (a *App) nativeKeyboardAuthorityLost(expected nativeGuestBinding) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if expected.Publisher == ([16]byte{}) || expected.QueueEpoch == 0 || expected.AuthorityGeneration == 0 || a.nativeGuestBinding != expected || a.nativeKeyboardGeneration != expected.AuthorityGeneration {
		return false
	}
	a.requestNativeKeyboardQuarantineLocked(nativeFaultAuthority)
	return true
}

// nativeKeyboardRetireForPadRecoveryLocked is called under App.mu by the
// same-connection recovery transaction. It never permits a keyboard snapshot.
// A live source must first complete its same-token RESET; an uncertain source
// requires recovery proof for its retained old target, boot and agent.
func (a *App) nativeKeyboardRetireForPadRecoveryLocked(expected padOnlyAuthority, previousAgent string, reply localhid.Reply) error {
	current, err := a.padOnlyAuthorityLocked(time.Now(), true)
	request := localhid.Request{Version: 2, Op: "recover", Request: reply.Request, AgentInstance: previousAgent, TargetID: expected.TargetID, BootID: expected.BootID}
	if err != nil || current != expected || reply.AgentInstance != expected.AgentInstance || (reply.Status != "reader-drained" && reply.Status != "startup-retired") || (reply.Status == "startup-retired" && !a.managedKitSoftware) || localhid.CorrelateRecoveryReply(reply, request) != nil || a.nativeGuestBeginRequest != nil || a.nativeGuestSnapshotAllowed {
		return errNativeGuestState
	}
	s := &a.nativeGuest
	if s.phase == nativeGuestUncertain {
		if s.binding.TargetID != expected.TargetID || s.binding.BootID != expected.BootID || s.binding.InputAgentInstance != previousAgent {
			return errNativeGuestState
		}
	} else if s.phase != nativeGuestIdle {
		return errNativeGuestState
	}
	generation, serial := s.lastGeneration, s.serial
	*s = nativeGuestLifecycle{lastGeneration: generation, serial: serial}
	a.nativeGuestBinding = nativeGuestBinding{}
	a.nativeGuestSnapshotAllowed = false
	a.requestNativeKeyboardQuarantineLocked(nativeFaultAuthority)
	return nil
}

// nativeKeyboardConfirmPadDrain consumes only a correlated RECOVER reply for
// the exact pad promotion epoch and prior agent/target/boot. The caller must
// have closed that handle and obtained the reply on its successor connection.
// Target non-HID drain ordering remains an activation prerequisite.
func (a *App) nativeKeyboardConfirmPadDrain(epoch uint64, previous padOnlyAuthority, reply localhid.Reply) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	req := localhid.Request{Version: 2, Op: "recover", Request: reply.Request, AgentInstance: previous.AgentInstance, TargetID: previous.TargetID, BootID: previous.BootID}
	// Changed-agent software retirement relies on the managed group drain and
	// runtime idle-program contract; unmanaged startup never assumes it.
	if !a.nativePadRetiring || epoch == 0 || epoch != a.nativePadRetirementEpoch || previous != a.nativePadRetirementBinding || (reply.Status != "reader-drained" && reply.Status != "startup-retired") || (reply.Status == "startup-retired" && !a.managedKitSoftware) || localhid.CorrelateRecoveryReply(reply, req) != nil {
		return errNativeGuestState
	}
	if a.nativeMappedPad.active {
		if err := a.nativeMappedPad.confirmedRetired(epoch, previous); err != nil {
			return err
		}
	}
	a.nativePadRetiring = false
	a.nativePadRetirementBinding = padOnlyAuthority{}
	select {
	case a.nativeKeyboardWake <- struct{}{}:
	default:
	}
	return nil
}

// nativeKeyboardConfirmReady is called only after BEGIN_ACK and all buffered
// edges have completed. It does not itself post input or acknowledge a sink.
func (a *App) nativeKeyboardConfirmReady(expected nativeGuestBinding) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.nativeGuestBinding != expected {
		return false
	}
	current, err := a.nativeKeyboardBindingLocked(expected.Publisher, expected.QueueEpoch)
	if err != nil || current != expected {
		a.requestNativeKeyboardQuarantineLocked(nativeFaultAuthority)
		return false
	}
	if a.nativeGuest.phase != nativeGuestReady || a.nativeGuest.binding != expected || a.nativeGuest.invalidated || a.nativeGuest.pending != nil || len(a.nativeGuest.queue) != 0 {
		return false
	}
	a.nativeKeyboardQuarantined = false
	return true
}

// The event loop never waits for target I/O. Snapshot permission is false by
// default; the recovery worker establishes receiver-confirmed retirement and
// fresh authority before granting it.
func (a *App) requestNativeKeyboardQuarantine(reason nativeKeyboardFault) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.nativeGuestSnapshotAllowed = false
	a.nativeGuest.invalidate()
	a.requestNativeKeyboardQuarantineLocked(reason)
}

func (a *App) nativeKeyboardSnapshotPermit() (uint64, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.nativeGuestSnapshotAllowed {
		current, err := a.nativeKeyboardBindingLocked(a.nativeGuestBinding.Publisher, a.nativeGuestBinding.QueueEpoch)
		if err != nil || current != a.nativeGuestBinding {
			a.requestNativeKeyboardQuarantineLocked(nativeFaultAuthority)
		}
	}
	return a.nativeKeyboardGeneration, a.delegatedV2Active && !a.nativeKeyboardGenerationExhausted && a.nativeGuestSnapshotAllowed && a.nativeGuest.phase == nativeGuestIdle
}

func (a *App) publishNativeKeyboardBoundary(gen uint64, b nativeRawBoundary) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.delegatedV2Active || a.nativeKeyboardGenerationExhausted || gen == 0 || gen != a.nativeKeyboardGeneration || !a.nativeGuestSnapshotAllowed || a.nativeGuestBinding.AuthorityGeneration != gen {
		return errNativeGuestState
	}
	current, err := a.nativeKeyboardBindingLocked(a.nativeGuestBinding.Publisher, a.nativeGuestBinding.QueueEpoch)
	if err != nil || current != a.nativeGuestBinding {
		a.requestNativeKeyboardQuarantineLocked(nativeFaultAuthority)
		return errNativeGuestState
	}
	ui := a.nativeUIRaw
	if err := ui.replace(b); err != nil {
		a.requestNativeKeyboardQuarantineLocked(nativeFaultDesync)
		return err
	}
	req, err := a.nativeGuest.begin(a.nativeGuestBinding, b, nativeGuestHIDUsage)
	if err != nil {
		a.requestNativeKeyboardQuarantineLocked(nativeFaultAuthority)
		return err
	}
	a.nativeUIRaw = ui
	a.repeat.ClearNative()
	a.nativeUIActions = nil
	a.nativeGuestSnapshotAllowed = false
	a.nativeGuestBeginRequest = &req
	select {
	case a.nativeKeyboardWake <- struct{}{}:
	default:
	}
	return nil
}

func (a *App) recordNativeKeyboardEdge(gen uint64, edge nativeRawEdge) (nativeEdgeDecision, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.delegatedV2Active || a.nativeKeyboardGenerationExhausted || gen == 0 || gen != a.nativeKeyboardGeneration {
		return nativeEdgeDecision{}, errNativeGuestState
	}
	d := a.nativeUIRaw.devices[edge.Device]
	if d == nil || edge.Code == 0 || edge.Code >= 256 {
		a.requestNativeKeyboardQuarantineLocked(nativeFaultDesync)
		return nativeEdgeDecision{}, errNativeGuestState
	}
	if err := a.recordNativeUIRawLocked(edge.Device, edge.Code, edge.Down, true); err != nil {
		a.requestNativeKeyboardQuarantineLocked(nativeFaultDesync)
		return nativeEdgeDecision{}, err
	}
	if a.nativeMappedPad.active && !a.nativeMappedPad.invalid {
		l := &a.nativeMappedPad
		current, err := a.padOnlyAuthorityLocked(time.Now(), true)
		if err != nil || current != l.binding || !a.nativePadRetiring || a.nativePadRetirementEpoch != l.epoch || a.nativePadConnectionFence.Revoked() {
			a.nativePadRevokeConnectionLocked()
		} else {
			name := nativeKeyName(edge.Code)
			shortcut := a.nativeKeyboardGuestShortcutLocked(edge.Code) || playhid.ChromeStop(name) || loadTapeChromeKey(name)
			if !a.nativeKeyboardGuestFocusLocked() || (edge.Down && shortcut) {
				a.nativePadFenceMappingLocked()
			} else {
				event, mapped := playhid.Event(name, true, false)
				admit := edge.Down && mapped && !d.suppressed[edge.Code] && !a.nativePadMappingFenced && !a.nativePadDeviceBlocked[edge.Device] && !a.nativePadMappingFence.Revoked() && !a.nativePadDeviceFences[edge.Device].Revoked()
				n := len(l.queue)
				if err := l.edge(l.epoch, l.binding, edge.Device, edge.Code, edge.Down, admit, event); err != nil {
					a.nativePadRevokeConnectionLocked()
				} else if len(l.queue) > n {
					l.queue[len(l.queue)-1].mappingFence = a.nativePadMappingFence
					l.queue[len(l.queue)-1].deviceFence = a.nativePadDeviceFences[edge.Device]
					a.nativePadWakeLocked()
				}
			}
		}
	}
	guest := nativeGuestQuarantined
	if !a.nativeGuest.invalidated && (a.nativeGuest.phase == nativeGuestBeginning || a.nativeGuest.phase == nativeGuestReady) {
		if !a.nativeKeyboardGuestFocusLocked() || (edge.Down && a.nativeKeyboardGuestShortcutLocked(edge.Code)) {
			a.requestNativeKeyboardQuarantineLocked(nativeFaultAuthority)
		} else if a.nativeGuest.rawEdge(a.nativeGuestBinding, edge) == nil {
			guest = nativeGuestBuffered
			select {
			case a.nativeKeyboardWake <- struct{}{}:
			default:
			}
		} else {
			a.requestNativeKeyboardQuarantineLocked(nativeFaultAuthority)
		}
	}
	token := a.issueNativeUIActionLocked(a.nativeKeyboardGeneration, edge.Device, edge.Code, edge.Down, false)
	ui := nativeUISuppress
	if token != 0 {
		ui = nativeUIAllow
	}
	return nativeEdgeDecision{UI: ui, Guest: guest, UIAction: token}, nil
}

func (a *App) quarantineNativeKeyboardUIOnly(id localhid.DeviceID) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.nativePadRetireDeviceLocked(id)
	if a.nativeUIRaw.devices[id] != nil {
		_ = a.quarantineNativeUIRawDeviceLocked(id)
	}
}

func (a *App) registerNativeKeyboardUIOnly(id localhid.DeviceID, fd int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.delegatedV2Active || fd < 0 {
		return errNativeGuestState
	}
	if err := a.nativeUIRaw.addUnknown(id); err != nil {
		return err
	}
	a.nativeUIRaw.devices[id].fd = fd
	if a.nativeMappedPad.active && !a.nativeMappedPad.invalid {
		if a.nativePadDeviceFences == nil {
			a.nativePadDeviceFences = make(map[localhid.DeviceID]*localfeed.PadWriteFence)
		}
		a.nativePadDeviceFences[id] = &localfeed.PadWriteFence{}
	}
	return nil
}

func (a *App) nativeKeyboardUIOnlySnapshotAllowedLocked() bool {
	return a.delegatedV2Active && !a.nativeKeyboardClosing && !a.nativeGuestSnapshotAllowed &&
		(a.nativeGuest.phase == nativeGuestIdle || a.nativeGuest.phase == nativeGuestUncertain || a.nativeGuest.phase == nativeGuestNeedsReset || a.nativeGuest.phase == nativeGuestResetting || a.nativeGuest.invalidated)
}

func (a *App) nativeKeyboardUIOnlySnapshotAllowed() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.nativeKeyboardUIOnlySnapshotAllowedLocked()
}

// This local-only per-device boundary arms proven-up UI keys and suppresses
// keys found held. It cannot authorize a guest source or affect other inputs.
func (a *App) publishNativeKeyboardUIOnly(id localhid.DeviceID, held []uint16) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	old := a.nativeUIRaw.devices[id]
	if !a.nativeKeyboardUIOnlySnapshotAllowedLocked() || old == nil || len(held) > 255 || a.nativeUIRaw.revision == ^uint64(0) {
		return errNativeGuestState
	}
	d := &nativeKeyboardUIRawDevice{fd: old.fd}
	for _, code := range held {
		if code == 0 || code >= 256 || d.down[code] {
			return errNativeGuestState
		}
		d.down[code], d.suppressed[code] = true, true
		d.revision[code] = a.nativeUIRaw.revision + 1
	}
	a.nativeUIRaw.revision++
	a.nativeUIRaw.devices[id] = d
	a.repeat.RetireNative([16]byte(id))
	for token, action := range a.nativeUIActions {
		if action.id == id {
			delete(a.nativeUIActions, token)
		}
	}
	return nil
}

func (a *App) recordNativeKeyboardRepeat(gen uint64, id localhid.DeviceID, code uint16) (nativeUIDisposition, uint64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.delegatedV2Active || a.nativeKeyboardGenerationExhausted || gen == 0 || gen != a.nativeKeyboardGeneration {
		return nativeUISuppress, 0, errNativeGuestState
	}
	token := a.issueNativeUIActionLocked(gen, id, code, true, true)
	if token == 0 {
		return nativeUISuppress, 0, nil
	}
	return nativeUIAllow, token, nil
}

func (a *App) retireNativeKeyboardDevice(gen uint64, id localhid.DeviceID) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if gen != a.nativeKeyboardGeneration || id == (localhid.DeviceID{}) {
		return errors.New("stale native retirement")
	}
	a.retireNativeUIRawLocked(id)
	a.nativeGuestSnapshotAllowed = false
	a.nativeGuest.invalidate()
	a.requestNativeKeyboardQuarantineLocked(nativeFaultHotplug)
	return nil
}
