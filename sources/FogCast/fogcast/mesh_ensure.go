package fogcast

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/internal/kitcontent"
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
	launchSnapshotROMSourcesChanged  = "ROM sources changed"
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
// A nil Executor leaves Phase 0 and Phase 1 launch unchanged. GET
// /api/v1/games calls ReadyHere through this session when an executor
// and a catalog projection are installed. Rooms follow that result.
// Neither path Ensures or pulls. The executor is one node. A
// launchable FPGA entry is ensured on the target this call will
// execute on. Launch does not pull onto a different node. A grant
// this call claims is released when Ensure does not start execution.
// A grant the session already held stays held. Placement is set when
// a launch asked Place and the selection was recorded, including after
// a rebind onto another FPGA kit.
type MeshExecuteSession struct {
	BoundNode string
	Executor  meshcontent.Executor
	// Entry returns the projected catalog row for gameID. ok false
	// means this launch is not asking for the mesh content contract.
	Entry func(gameID string) (meshcontent.Entry, bool)
	// Placement is the recorded Execute node and its local DisplaySink
	// and InputSource. The zero value means no decision is recorded.
	// Launch and Ensure keep using BoundNode and Executor.
	Placement MeshPlacement
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
	s.attachMeshAuthorizer(session)
}

// attachMeshAuthorizer gives a remote executor the client for the bound
// node. That client is the one claimContentPullLease uses. A selected
// sibling is not attached. Reads do not use the authorizer.
func (s *Service) attachMeshAuthorizer(session MeshExecuteSession) {
	setter, ok := session.Executor.(interface {
		SetMutationAuthorizer(kitcontent.MutationAuthorizer)
	})
	if !ok || setter == nil {
		return
	}
	client := s.meshReadyClient(session.BoundNode)
	auth, ok := client.(kitcontent.MutationAuthorizer)
	if !ok || auth == nil {
		setter.SetMutationAuthorizer(nil)
		return
	}
	setter.SetMutationAuthorizer(auth)
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

// meshEnsureBeforeExecute runs only when a session executor is installed.
// The snapshot is the target and catalog row LaunchOn already captured.
// FPGA ensure uses that identity and does not read selectedTarget again.
// A foreign-kit denial returns
// before Ensure, so a rejected FPGA launch does not pull. A node
// mismatch returns ErrUnboundNode before Pull or Link. LeaseFree is
// this session's grant and generation, not the absence of a foreign
// holder. InUse is the Phase 1 busy connection. A fresh FPGA session
// on a free kit claims through the content-pull mutation before
// Ensure, so that first launch holds the grant and can pull. A grant
// claimed in this call is released when Ensure does not start
// execution. A grant the session already held stays held. A held
// grant whose observed generation differs is compared with the
// selected connection only when this launch executes on that kit. A
// kit another session holds returns before Pull.
// ErrContentMissingNoSource fails closed. A slot that stays Checking
// until the host timeout returns ErrCheckingTimeout. Launch must not
// continue into execute.
func (s *Service) meshEnsureBeforeExecute(ctx context.Context, snap launchSnapshot) error {
	if s == nil || !snap.frozen || snap.executor == nil || !snap.entryOK {
		return nil
	}
	if meshEntryHasNativeEmulator(snap.entry) {
		return canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	node := snap.boundNode
	if snap.entry.Launchable {
		if s.launchUsesForeignKit(snap) {
			s.releaseLaunchClaim(snap, false)
			return canonicalError(protocol.CodeKitLeaseDenied, nil)
		}
		node = snap.nodeID
	}
	if strings.TrimSpace(node) == "" || snap.executor.NodeID() != node {
		s.releaseLaunchClaim(snap, false)
		return meshcontent.ErrUnboundNode
	}
	// LeaseFree is this session's grant on the kit this launch executes
	// on, including that grant's generation. InUse is the Phase 1 busy
	// connection: another session holds the kit. A foreign holder returns the lease
	// denial before Ensure. A launchable FPGA session that does not
	// yet hold a grant claims it here, using the same acquiring
	// authorization as content pull, and Ensure then sees that grant.
	// A grant that is already held and fails the generation check is
	// left unchanged.
	leaseFree, inUse := s.meshLeaseFacts(snap)
	if inUse {
		s.releaseLaunchClaim(snap, false)
		return canonicalError(protocol.CodeKitLeaseDenied, nil)
	}
	claimed := false
	if snap.entry.Launchable && !leaseFree {
		if err := s.claimContentPullLease(ctx, snap); err != nil {
			s.releaseLaunchClaim(snap, false)
			return err
		}
		claimed = true
		leaseFree = true
	}
	result, err := meshcontent.Ensure(ctx, snap.entry, node, snap.executor, meshcontent.EnsureOption{
		LeaseFree:       leaseFree,
		InUse:           inUse,
		CheckingTimeout: s.meshCheckingWait(),
	})
	if err != nil {
		s.releaseLaunchClaim(snap, claimed)
		return err
	}
	if err := result.Blocked(); err != nil {
		s.releaseLaunchClaim(snap, claimed)
		return err
	}
	if meshEnsureFinishedHook != nil {
		meshEnsureFinishedHook()
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
			if snap.remoteTargetFrozen && !snap.frozen && !snap.pinned {
				s.bindPlayTargetLocked(name)
			} else {
				s.activeTarget = name
			}
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
	if snap.remoteTargetFrozen && !snap.frozen && !snap.pinned {
		s.bindPlayTargetLocked(name)
	} else {
		s.activeTarget = name
	}
	s.executionMu.Unlock()
	cfg := targetByName(s.targets, name)
	if s.targetOrigin != nil && snap.explicit && name != snap.selectedName {
		hook, lease := s.targetOrigin, kitLeaseOf(s.targetClients[name])
		s.targetMu.Unlock()
		hook(cfg, lease)
		s.targetMu.Lock()
	}
	if launchPinnedTargetBoundHook != nil {
		launchPinnedTargetBoundHook(name)
	}
	return nil
}
