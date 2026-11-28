package fogcast

import (
	"context"
	"errors"
	"reflect"
	"strings"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/protocol"
)

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
	// siblingExecutor is set when this snapshot dialed an explicit
	// FPGA kit other than the installed selected-target session.
	// Revalidation still checks that kit. It does not require the
	// selected session to stay on this executor.
	siblingExecutor bool
	// placementClaimed is set when this launch claimed the kit lease
	// while rebinding, or adopted a claim another in-flight launch
	// already holds. That grant is released when this launch does not
	// start execution and no other launch still holds it. A grant the
	// session already held stays held.
	placementClaimed bool
	// placementGeneration is the grant this launch claimed or adopted.
	// A later release ignores a different generation.
	placementGeneration string
	// placementClaimSettled is shared with LaunchOn. It is set when
	// execution starts, or when an earlier failure already released
	// the placement grant, or when another launch still holds it. Nil
	// when this launch did not claim one.
	placementClaimSettled *bool
	// placementKept is set when execution starts. LaunchOn restores
	// the previous session when it stays false and leaves that kit's
	// lease untouched. When it is set, LaunchOn releases the lease the
	// session held on the kit the rebind left (#281). Nil when this
	// launch did not rebind the session.
	placementKept *bool
	// placementUndo is the session state from before this rebind.
	// LaunchOn applies it when execution does not start.
	placementUndo placementSessionUndo
	// pinned is set when placement claimed a kit. Bind keeps that
	// kit's target and client even if selectedTarget moves, including
	// when no mesh executor is installed. Address, TargetID, enabled
	// state, and the captured client are still validated.
	pinned bool
	// remoteTargetFrozen preserves the original local target across a
	// remote-source download even when mesh Ensure is disabled.
	remoteTargetFrozen bool
	remoteSelection    *remoteSelectionSnapshot
	// romSources is the Coleco format-4 BIOS and cartridge pair
	// captured at launch start. Nil when this launch is not that load.
	romSources *twoROMSourceSnapshot
}

type twoROMSourceSnapshot struct {
	packageID string
	biosID    string
	cartID    string
}

func (s *Service) captureTwoROMSources(gameID string) (*twoROMSourceSnapshot, error) {
	entries, entryOK := s.catalog.(coreEntryCatalog)
	roms, romOK := s.catalog.(coreROMCatalog)
	if !entryOK || !romOK {
		return nil, nil
	}
	ctx := context.Background()
	entry, err := entries.CoreEntry(ctx, gameID)
	if errors.Is(err, catalog.ErrCoreEntryNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, mapCoreEntryError(err)
	}
	if entry.CoreID != "fes.coleco" {
		return nil, nil
	}
	inspection, err := s.installedCoreInspection(ctx, entry.PackageID)
	if err != nil {
		return nil, err
	}
	if inspection.Descriptor.Format != 4 || len(inspection.Descriptor.ROMs) != 2 {
		return nil, nil
	}
	bios, err := roms.CoreEntryROM(ctx, gameID, inspection.Descriptor.ROMs[0].ID)
	if err != nil {
		return nil, romSelectionError(err)
	}
	cart, err := roms.CoreEntryROM(ctx, gameID, inspection.Descriptor.ROMs[1].ID)
	if err != nil {
		return nil, romSelectionError(err)
	}
	return &twoROMSourceSnapshot{packageID: entry.PackageID, biosID: bios.MediaID, cartID: cart.MediaID}, nil
}

// captureLaunchSnapshot reads the target, the bound node, and the
// catalog row once. A disabled known target returns before Ensure.
// With the seam off, only the requested name is kept. An explicit
// FPGA launch whose kit is a different node dials that kit for this
// snapshot and leaves the selected-target session installed. That
// dial captures the named kit's client before the snapshot returns.
func (s *Service) captureLaunchSnapshot(gameID, target string) (launchSnapshot, error) {
	if s == nil {
		return launchSnapshot{}, nil
	}
	s.activateMeshExecutor(context.Background())
	s.meshMu.Lock()
	session := s.meshExecute
	s.meshMu.Unlock()
	requested := strings.TrimSpace(target)
	romSources, err := s.captureTwoROMSources(gameID)
	if err != nil {
		return launchSnapshot{}, err
	}
	if session.Executor == nil {
		// Keep the original local target available for remote preparation.
		// Ordinary launches still bind the live target when the seam is off.
		snap := s.captureTargetLocked(session, requested)
		snap.gameID, snap.romSources = gameID, romSources
		snap.boundNode, snap.executor = session.BoundNode, session.Executor
		return snap, nil
	}
	snap := s.captureTargetLocked(session, requested)
	snap.romSources = romSources
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
	if err := s.useRequestedMeshExecutor(&snap); err != nil {
		return snap, err
	}
	return snap, nil
}

// useRequestedMeshExecutor dials the kit an explicit FPGA launch named
// when the installed executor is a different node. Readiness keeps the
// selected-target session. A failed dial leaves that executor in the
// snapshot, so Ensure returns ErrUnboundNode and does not pull on it.
// A successful
// dial captures the named kit's client before the snapshot is returned,
// so Ensure claims that kit's lease through a real client.
func (s *Service) useRequestedMeshExecutor(snap *launchSnapshot) error {
	if s == nil || snap == nil || !snap.explicit || !snap.entryOK || !snap.known || !snap.enabled {
		return nil
	}
	if snap.executor == nil || strings.TrimSpace(snap.nodeID) == "" || snap.executor.NodeID() == snap.nodeID {
		return nil
	}
	s.meshMu.Lock()
	ensure := s.meshEnsure
	s.meshMu.Unlock()
	if !ensure {
		return nil
	}
	remote := s.dialNamedMeshExecutor(context.Background(), snap.name)
	if remote == nil || remote.NodeID() != snap.nodeID {
		return nil
	}
	if err := s.captureRequestedClient(snap); err != nil {
		return err
	}
	snap.executor = remote
	snap.siblingExecutor = true
	s.attachMeshAuthorizer(MeshExecuteSession{BoundNode: snap.nodeID, Executor: remote})
	return nil
}

// captureRequestedClient stores the named kit's client on the snapshot
// before Ensure. Open creates a client only for the selected target, so
// a fresh explicit sibling is nil at capture. The factory opens that
// client here. Ensure then claims the lease through it. A client that
// cannot be opened returns before the sibling executor is installed.
func (s *Service) captureRequestedClient(snap *launchSnapshot) error {
	if snap == nil || snap.client != nil {
		return nil
	}
	s.targetMu.Lock()
	defer s.targetMu.Unlock()
	if client := s.targetClients[snap.name]; client != nil {
		snap.client = client
		return nil
	}
	if s.targetClientFactory == nil {
		return canonicalError(protocol.CodeInternal, nil)
	}
	cfg := targetByName(s.targets, snap.name)
	client, err := s.targetClientFactory(cfg)
	if err != nil || client == nil {
		return canonicalError(protocol.CodeBadRequest, nil)
	}
	if s.targetClients == nil {
		s.targetClients = map[string]serviceClient{}
	}
	s.targetClients[snap.name] = client
	snap.client = client
	return nil
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

// revalidateLaunchSnapshot is the single check immediately before
// execute. The caller holds lifecycle admission. With the seam off it
// binds the live selected target. With the seam on, any difference
// from the launch snapshot returns ErrLaunchSnapshot and does not
// install a client. A match binds the captured client and no other.
func (s *Service) revalidateLaunchSnapshot(snap launchSnapshot) error {
	if s == nil {
		return nil
	}
	if !snap.frozen && !snap.pinned && !snap.remoteTargetFrozen {
		if err := s.snapshotROMSourcesDrift(snap); err != nil {
			return err
		}
		return s.bindLiveLaunchTarget(snap.requested)
	}
	if err := s.validateLaunchSnapshot(snap); err != nil {
		return err
	}
	s.targetMu.Lock()
	defer s.targetMu.Unlock()
	// Recheck under the same lock used to bind the captured client.
	if err := s.snapshotTargetDriftLocked(snap); err != nil {
		return err
	}
	return s.bindCapturedLocked(snap)
}

func (s *Service) snapshotROMSourcesDrift(snap launchSnapshot) error {
	if snap.romSources == nil {
		return nil
	}
	current, err := s.captureTwoROMSources(snap.gameID)
	if err != nil {
		return err
	}
	if current == nil || *current != *snap.romSources {
		return snapshotMismatch(launchSnapshotROMSourcesChanged)
	}
	return nil
}

// validateLaunchSnapshot checks the original identities without binding or
// changing the foreground kit. Remote preparation uses it around network I/O.
func (s *Service) validateLaunchSnapshot(snap launchSnapshot) error {
	if err := s.snapshotROMSourcesDrift(snap); err != nil {
		return err
	}
	if err := s.snapshotRemoteSelectionDrift(snap); err != nil {
		return err
	}
	if snap.frozen || snap.pinned || snap.remoteTargetFrozen {
		if err := s.snapshotSessionDrift(snap); err != nil {
			return err
		}
	}
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	if err := s.snapshotTargetDriftLocked(snap); err != nil {
		return err
	}
	live, exists := s.targetClients[snap.name]
	if (snap.client != nil && exists && live != snap.client) || (snap.client == nil && exists) {
		return snapshotMismatch(launchSnapshotClientChanged)
	}
	return nil
}

func (s *Service) snapshotSessionDrift(snap launchSnapshot) error {
	s.meshMu.Lock()
	session := s.meshExecute
	s.meshMu.Unlock()
	if !snap.siblingExecutor && (session.BoundNode != snap.boundNode || session.Executor != snap.executor) {
		return snapshotMismatch(launchSnapshotBoundNodeChanged)
	}
	if !snap.entryOK {
		if snap.remoteTargetFrozen && session.Entry != nil {
			if _, ok := session.Entry(snap.gameID); ok {
				return snapshotMismatch(launchSnapshotCompositionChanged)
			}
		}
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
	if !snap.explicit && !snap.pinned && strings.TrimSpace(s.selectedTarget) != snap.selectedName {
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
