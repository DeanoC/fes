package fogcast

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/protocol"
)

const (
	// defaultMeshCheckingTimeout bounds a Checking slot when the
	// service has not been given another positive duration.
	defaultMeshCheckingTimeout = 30 * time.Second

	launchSnapshotTargetDisabled     = "target disabled"
	launchSnapshotTargetRemoved      = "target removed"
	launchSnapshotAddressChanged     = "address changed"
	launchSnapshotTargetIDChanged    = "target id changed"
	launchSnapshotSelectionChanged   = "selection changed"
	launchSnapshotCompositionChanged = "composition changed"
	launchSnapshotBoundNodeChanged   = "bound node changed"
	launchSnapshotClientChanged      = "client changed"
)

// ErrLaunchSnapshot is the failure class when live state no longer
// matches the snapshot taken at launch start. Launch does not execute
// and does not bind a different client.
var ErrLaunchSnapshot = errors.New("launch snapshot no longer matches")

// LaunchSnapshotError is ErrLaunchSnapshot with the field that drifted.
type LaunchSnapshotError struct {
	Reason string
}

func (e *LaunchSnapshotError) Error() string {
	if e == nil || e.Reason == "" {
		return ErrLaunchSnapshot.Error()
	}
	return ErrLaunchSnapshot.Error() + ": " + e.Reason
}

func (e *LaunchSnapshotError) Unwrap() error { return ErrLaunchSnapshot }

func snapshotMismatch(reason string) error {
	return &LaunchSnapshotError{Reason: reason}
}

// MeshExecuteSession is the optional seam Launch calls before execute.
// A nil Executor leaves Phase 0 and Phase 1 launch unchanged. Rooms and
// GET /api/v1/games do not read it. The executor is one node. A
// launchable FPGA entry is ensured on the target this call will
// execute on. Launch does not pull onto a different node and does not
// release a lease.
type MeshExecuteSession struct {
	BoundNode string
	Executor  meshcontent.Executor
	// Entry returns the projected catalog row for gameID. ok false
	// means this launch is not asking for the mesh content contract.
	Entry func(gameID string) (meshcontent.Entry, bool)
}

// SetMeshExecuteSession installs the ensure seam. Tests pass a fake
// executor. A kit store is a meshcontent.Executor the target agent
// serves; the host installs that executor here. A nil executor leaves
// Phase 0 and Phase 1 launch unchanged.
func (s *Service) SetMeshExecuteSession(session MeshExecuteSession) {
	if s == nil {
		return
	}
	s.meshMu.Lock()
	s.meshExecute = session
	s.meshMu.Unlock()
}

// SetMeshCheckingTimeout sets how long Launch will wait on a Checking
// slot. Non-positive restores the default. Values above
// meshcontent.MaxCheckingTimeout are clamped.
func (s *Service) SetMeshCheckingTimeout(timeout time.Duration) {
	if s == nil {
		return
	}
	if timeout <= 0 {
		timeout = defaultMeshCheckingTimeout
	}
	if timeout > meshcontent.MaxCheckingTimeout {
		timeout = meshcontent.MaxCheckingTimeout
	}
	s.meshCheckingTimeout = timeout
}

func (s *Service) meshCheckingWait() time.Duration {
	if s == nil || s.meshCheckingTimeout <= 0 {
		return defaultMeshCheckingTimeout
	}
	if s.meshCheckingTimeout > meshcontent.MaxCheckingTimeout {
		return meshcontent.MaxCheckingTimeout
	}
	return s.meshCheckingTimeout
}

// meshEnsureFinishedHook runs after a successful Ensure and before
// launch reads the catalog again. Tests change the composition or the
// named target here. It is nil outside tests and must not call back
// into Service.
var meshEnsureFinishedHook func()

// launchSnapshot is the one capture taken at launch start when a mesh
// session is installed. Ensure uses it. revalidateLaunchSnapshot
// compares it with live state under lifecycle admission, immediately
// before execute. frozen is false when the seam is off: bind then
// resolves the selected target at bind time.
type launchSnapshot struct {
	frozen    bool
	requested string
	gameID    string

	name         string
	targetID     string
	address      string
	enabled      bool
	known        bool
	client       serviceClient
	nodeID       string
	boundNode    string
	hadNodeID    bool
	explicit     bool
	selectedName string

	executor meshcontent.Executor
	entry    meshcontent.Entry
	entryOK  bool
}

// captureLaunchSnapshot reads the target, the bound node, and the
// catalog row once. A disabled known target returns before Ensure.
// With the seam off, only the requested name is kept.
func (s *Service) captureLaunchSnapshot(gameID, target string) (launchSnapshot, error) {
	if s == nil {
		return launchSnapshot{}, nil
	}
	s.meshMu.Lock()
	session := s.meshExecute
	s.meshMu.Unlock()
	requested := strings.TrimSpace(target)
	if session.Executor == nil {
		return launchSnapshot{requested: requested, gameID: gameID}, nil
	}
	snap := s.captureTargetLocked(session, requested)
	snap.frozen = true
	snap.gameID = gameID
	snap.boundNode = session.BoundNode
	snap.executor = session.Executor
	if session.Entry != nil {
		snap.entry, snap.entryOK = session.Entry(gameID)
	}
	if snap.known && !snap.enabled {
		return snap, snapshotMismatch(launchSnapshotTargetDisabled)
	}
	return snap, nil
}

func (s *Service) captureTargetLocked(session MeshExecuteSession, requested string) launchSnapshot {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	selected := strings.TrimSpace(s.selectedTarget)
	name := requested
	if name == "" {
		name = selected
	}
	snap := launchSnapshot{
		requested:    requested,
		name:         name,
		selectedName: selected,
		explicit:     requested != "",
	}
	if name == "" {
		snap.nodeID = session.BoundNode
		return snap
	}
	cfg := targetByName(s.targets, name)
	snap.address = strings.TrimSpace(cfg.Address)
	snap.enabled = cfg.Enabled
	snap.known = strings.TrimSpace(cfg.Name) != ""
	snap.client = s.targetClients[name]
	if id := strings.TrimSpace(cfg.NodeID()); id != "" {
		snap.targetID = id
		snap.nodeID = id
		snap.hadNodeID = true
		return snap
	}
	if snap.explicit {
		if strings.TrimSpace(cfg.Name) != "" {
			snap.nodeID = cfg.Name
		} else {
			snap.nodeID = requested
		}
		return snap
	}
	if session.BoundNode != "" && session.BoundNode != name {
		for _, candidate := range s.targets {
			if candidate.Name == "" || candidate.Name == name {
				continue
			}
			if candidate.NodeID() == session.BoundNode || candidate.Name == session.BoundNode {
				snap.nodeID = name
				return snap
			}
		}
	}
	// Empty TargetID does not inherit the session node. Selection can
	// name target B while the session is still bound to node A, and
	// targets with no ids give the loop above nothing to recognize.
	// Ensure would then run on A and bind would still use B's client.
	if name == session.BoundNode {
		snap.nodeID = name
	}
	return snap
}

// meshEnsureBeforeExecute runs only when a session executor is installed.
// The snapshot is the target and catalog row LaunchOn already captured.
// FPGA ensure uses that identity and does not read selectedTarget again.
// Host-only execution stays on the installed session node, because that
// launch does not bind the named kit. A foreign-kit denial returns
// before Ensure, so a rejected FPGA launch does not pull. A node
// mismatch returns ErrUnboundNode before Pull or Link. LeaseFree is
// this session's grant and generation, not the absence of a foreign
// holder. InUse is the Phase 1 busy connection. A fresh FPGA session
// on a free kit claims through the content-pull mutation before
// Ensure, so that first launch holds the grant and can pull. A held
// grant whose observed generation differs, and a kit another session
// holds, both return before Pull.
// ErrContentMissingNoSource fails closed. A slot that stays Checking
// until the host timeout returns ErrCheckingTimeout. Launch must not
// continue into execute.
func (s *Service) meshEnsureBeforeExecute(ctx context.Context, snap launchSnapshot) error {
	if s == nil || !snap.frozen || snap.executor == nil || !snap.entryOK {
		return nil
	}
	node := snap.boundNode
	if snap.entry.Launchable && meshLaunchExecution(snap.entry) != ExecutionHostOnly {
		if s.launchUsesForeignKit(snap, ExecutionFPGANative) {
			return canonicalError(protocol.CodeKitLeaseDenied, nil)
		}
		node = snap.nodeID
	}
	if strings.TrimSpace(node) == "" || snap.executor.NodeID() != node {
		return meshcontent.ErrUnboundNode
	}
	// LeaseFree is this session's grant on the kit this launch executes
	// on, including that grant's generation. InUse is the Phase 1 busy
	// connection: another session holds the kit. Host-only play does
	// not take that lease. A foreign holder still returns the lease
	// denial before Ensure. A launchable FPGA session that does not
	// yet hold a grant claims it here, using the same acquiring
	// authorization as content pull, and Ensure then sees that grant.
	// A grant that is already held and fails the generation check is
	// left unchanged.
	fpga := meshLaunchExecution(snap.entry) != ExecutionHostOnly
	leaseFree, inUse := s.meshLeaseFacts(snap)
	if fpga && inUse {
		return canonicalError(protocol.CodeKitLeaseDenied, nil)
	}
	if fpga && snap.entry.Launchable && !leaseFree {
		if err := s.claimContentPullLease(ctx, snap); err != nil {
			return err
		}
		leaseFree = true
	}
	result, err := meshcontent.Ensure(ctx, snap.entry, node, snap.executor, meshcontent.EnsureOption{
		LeaseFree:       leaseFree,
		InUse:           inUse,
		CheckingTimeout: s.meshCheckingWait(),
	})
	if err != nil {
		return err
	}
	if err := result.Blocked(); err != nil {
		return err
	}
	if meshEnsureFinishedHook != nil {
		meshEnsureFinishedHook()
	}
	return nil
}

// revalidateLaunchSnapshot is the single check immediately before
// execute. The caller holds lifecycle admission. With the seam off it
// binds the live selected target. With the seam on, any difference
// from the launch snapshot returns ErrLaunchSnapshot and does not
// install a client. A match binds the captured client and no other.
func (s *Service) revalidateLaunchSnapshot(snap launchSnapshot) error {
	if s == nil {
		return nil
	}
	if !snap.frozen {
		return s.bindLiveLaunchTarget(snap.requested)
	}
	if err := s.snapshotSessionDrift(snap); err != nil {
		return err
	}
	s.targetMu.Lock()
	defer s.targetMu.Unlock()
	if err := s.snapshotTargetDriftLocked(snap); err != nil {
		return err
	}
	return s.bindCapturedLocked(snap)
}

func (s *Service) snapshotSessionDrift(snap launchSnapshot) error {
	s.meshMu.Lock()
	session := s.meshExecute
	s.meshMu.Unlock()
	if session.BoundNode != snap.boundNode || session.Executor != snap.executor {
		return snapshotMismatch(launchSnapshotBoundNodeChanged)
	}
	if !snap.entryOK {
		return nil
	}
	if session.Entry == nil {
		return snapshotMismatch(launchSnapshotCompositionChanged)
	}
	current, ok := session.Entry(snap.gameID)
	if !ok || !reflect.DeepEqual(snap.entry, current) {
		return snapshotMismatch(launchSnapshotCompositionChanged)
	}
	return nil
}

// snapshotTargetDriftLocked compares the captured target with the
// configs currently stored. Caller holds targetMu.
func (s *Service) snapshotTargetDriftLocked(snap launchSnapshot) error {
	if !snap.explicit && strings.TrimSpace(s.selectedTarget) != snap.selectedName {
		return snapshotMismatch(launchSnapshotSelectionChanged)
	}
	if snap.name == "" {
		return nil
	}
	cfg := targetByName(s.targets, snap.name)
	if strings.TrimSpace(cfg.Name) == "" {
		return snapshotMismatch(launchSnapshotTargetRemoved)
	}
	if !cfg.Enabled {
		return snapshotMismatch(launchSnapshotTargetDisabled)
	}
	if strings.TrimSpace(cfg.Address) != snap.address {
		return snapshotMismatch(launchSnapshotAddressChanged)
	}
	liveID := strings.TrimSpace(cfg.NodeID())
	if snap.hadNodeID && liveID != snap.targetID {
		return snapshotMismatch(launchSnapshotTargetIDChanged)
	}
	if !snap.hadNodeID && liveID != "" {
		return snapshotMismatch(launchSnapshotTargetIDChanged)
	}
	return nil
}

// bindCapturedLocked installs the client captured with the snapshot.
// Caller holds targetMu and has already rejected drift. A client that
// appeared after capture is not used. A live client already stored for
// this name is kept only when it is the captured client. A different
// client at the same address and TargetID is LAUNCH_CHANGED; the live
// client is not overwritten.
func (s *Service) bindCapturedLocked(snap launchSnapshot) error {
	name := snap.name
	live, liveOK := s.targetClients[name]
	if snap.client != nil {
		if liveOK && live != snap.client {
			return snapshotMismatch(launchSnapshotClientChanged)
		}
		if s.targetClients == nil {
			s.targetClients = map[string]serviceClient{}
		}
		s.targetClients[name] = snap.client
	} else if liveOK {
		return snapshotMismatch(launchSnapshotClientChanged)
	}
	if _, ok := s.targetClients[name]; !ok {
		if !snap.explicit {
			s.executionMu.Lock()
			s.activeTarget = name
			s.executionMu.Unlock()
			if launchPinnedTargetBoundHook != nil {
				launchPinnedTargetBoundHook(name)
			}
			return nil
		}
		if s.targetClientFactory == nil {
			return canonicalError(protocol.CodeInternal, nil)
		}
		cfg := targetByName(s.targets, name)
		client, err := s.targetClientFactory(cfg)
		if err != nil || client == nil {
			return canonicalError(protocol.CodeBadRequest, nil)
		}
		s.targetClients[name] = client
	}
	s.executionMu.Lock()
	s.activeTarget = name
	s.executionMu.Unlock()
	cfg := targetByName(s.targets, name)
	if s.targetOrigin != nil && snap.explicit && name != snap.selectedName {
		s.targetOrigin(cfg)
	}
	if launchPinnedTargetBoundHook != nil {
		launchPinnedTargetBoundHook(name)
	}
	return nil
}

// meshKitLease is the session-owned kit grant on one target client.
// generation is empty when this session does not hold a current grant.
type meshKitLease interface {
	MeshKitLease() (owned bool, generation string)
}

// meshPullAcquirer claims the session grant the way content pull does.
// A client that cannot claim leaves the launch fail-closed.
type meshPullAcquirer interface {
	AcquireContentPullLease(context.Context) error
}

// claimContentPullLease acquires the session grant before Ensure when
// this launch does not already hold one. A held grant that failed the
// generation check is not claimed again. After a successful claim,
// LeaseFree is that new grant; the selected connection's previous free
// observation is not a mismatch against a grant that did not exist yet.
func (s *Service) claimContentPullLease(ctx context.Context, snap launchSnapshot) error {
	owned, generation := false, ""
	if lease, ok := snap.client.(meshKitLease); ok && lease != nil {
		owned, generation = lease.MeshKitLease()
	}
	if owned && generation != "" {
		return meshcontent.ErrLeaseNotFree
	}
	acquirer, ok := snap.client.(meshPullAcquirer)
	if !ok || acquirer == nil {
		return meshcontent.ErrLeaseNotFree
	}
	if err := acquirer.AcquireContentPullLease(ctx); err != nil {
		return err
	}
	owned, generation = false, ""
	if lease, ok := snap.client.(meshKitLease); ok && lease != nil {
		owned, generation = lease.MeshKitLease()
	}
	if !owned || generation == "" {
		return meshcontent.ErrLeaseNotFree
	}
	return nil
}

// meshLeaseFacts is what Ensure's LeaseFree and InUse options are.
// FPGA play is owned only when this session holds the kit grant and,
// once ownership has been observed, that observation is the same
// generation. A free or unleased kit is not owned. InUse is the Phase 1
// busy connection for the kit this launch would execute on. Host-only
// play does not use that lease.
func (s *Service) meshLeaseFacts(snap launchSnapshot) (leaseFree, inUse bool) {
	if meshLaunchExecution(snap.entry) == ExecutionHostOnly {
		return true, false
	}
	inUse = s.launchUsesForeignKit(snap, ExecutionFPGANative)
	owned, generation := false, ""
	if lease, ok := snap.client.(meshKitLease); ok {
		owned, generation = lease.MeshKitLease()
	}
	if !owned || generation == "" {
		return false, inUse
	}
	conn := s.TargetConnection()
	if conn.leaseSeen && (!conn.leaseOwned || conn.leaseGeneration != generation) {
		return false, inUse
	}
	return true, inUse
}

// meshLaunchExecution maps a mesh entry onto the launch execution kind
// the foreign-kit check already uses. Host emulator play does not take
// the kit lease. Package-backed play does.
func meshLaunchExecution(entry meshcontent.Entry) string {
	if len(entry.Execute) > 0 && entry.Execute[0].Kind == meshcontent.ExecuteNativeEmu {
		return ExecutionHostOnly
	}
	return ExecutionFPGANative
}
