package fogcast

import (
	"context"
	"net/url"
	"strings"

	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/internal/meshplace"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

// MeshPlacement is the Execute node and its local DisplaySink and
// InputSource recorded on the in-memory session. DisplaySink and
// InputSource are empty when that node does not advertise them. They
// are never a different node. The zero value means no decision is
// recorded.
type MeshPlacement struct {
	Execute     string
	DisplaySink string
	InputSource string
}

// MeshPlacementAsk asks Launch to run Place for one title. Nil means
// the launch is not asking. Candidates are nodes the caller already
// has. OverrideNodeID is the optional node id; empty keeps automatic
// placement. MissingRequiredSlot is a required composition slot with
// no source. The call does not dial a kit and does not read a config
// file.
type MeshPlacementAsk struct {
	Candidates          []meshplace.Candidate
	OverrideNodeID      string
	MissingRequiredSlot bool
}

// SetMeshPlacementAsk installs the placement request Launch reads.
// Nil clears it. A cleared request keeps today's bind.
func (s *Service) SetMeshPlacementAsk(ask *MeshPlacementAsk) {
	if s == nil {
		return
	}
	s.meshMu.Lock()
	defer s.meshMu.Unlock()
	if ask == nil {
		s.meshPlacementAsk = nil
		return
	}
	copied := *ask
	copied.Candidates = append([]meshplace.Candidate(nil), ask.Candidates...)
	copied.OverrideNodeID = strings.TrimSpace(ask.OverrideNodeID)
	s.meshPlacementAsk = &copied
}

// MeshSessionPlacement returns the decision recorded on the session.
func (s *Service) MeshSessionPlacement() MeshPlacement {
	if s == nil {
		return MeshPlacement{}
	}
	s.meshMu.Lock()
	defer s.meshMu.Unlock()
	return s.meshExecute.Placement
}

// applyMatchingPlacement runs Place when this launch asked. A selection
// of the executor the session is already bound to is recorded on the
// session. Launch and Ensure keep that bind, and this path does not
// claim again. A selection of another FPGA kit claims that kit with
// the existing kit lease and rebinds the session onto it. Conflict
// rejects and does not steal. Generation takeover is not this path.
// Ensure then runs on that executor only when mesh ensure is already
// on; otherwise Ensure stays off and the snapshot still pins the
// claimed kit through bind. Picture and pad stay on that kit. A native_emu
// selection of any other node returns ErrUnboundNode and does not
// change the bind. Unresolved and fail closed do not name an Execute
// node, so they are not recorded and are not that refusal. No request
// returns the snapshot unchanged.
func (s *Service) applyMatchingPlacement(ctx context.Context, gameID string, snap launchSnapshot) (launchSnapshot, error) {
	if s == nil {
		return snap, nil
	}
	s.meshMu.Lock()
	ask := s.meshPlacementAsk
	s.meshMu.Unlock()
	if ask == nil {
		return snap, nil
	}
	entry, ok := s.placementEntry(gameID, snap)
	if !ok {
		return snap, nil
	}
	opts := s.PlaceOptions()
	opts.OverrideNodeID = ask.OverrideNodeID
	opts.MissingRequiredSlot = ask.MissingRequiredSlot
	result := meshplace.Place(entry, ask.Candidates, opts)
	if result.Outcome != meshplace.OutcomeSelected {
		return snap, nil
	}
	if !placementRolesLocal(result.Choice) {
		return snap, meshcontent.ErrUnboundNode
	}
	bound := s.placementBoundID(snap)
	if result.Choice.Execute == bound {
		if !placementOnBoundExecutor(result.Choice, bound) {
			return snap, meshcontent.ErrUnboundNode
		}
		if err := s.recordBoundPlacement(snap, bound, result.Choice); err != nil {
			return snap, err
		}
		return snap, nil
	}
	if !placementFPGANode(ask.Candidates, result.Choice.Execute) {
		return snap, meshcontent.ErrUnboundNode
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return snap, err
	}
	return s.rebindPlacementKit(ctx, snap, result.Choice)
}

// recordBoundPlacement stores a selection of the executor the session
// is already bound to. It does not claim a lease and does not change
// the bind.
func (s *Service) recordBoundPlacement(snap launchSnapshot, bound string, choice meshplace.Choice) error {
	s.meshMu.Lock()
	defer s.meshMu.Unlock()
	if !s.placementStillBoundLocked(snap, bound) {
		return meshcontent.ErrUnboundNode
	}
	s.meshExecute.Placement = placementFromChoice(choice)
	return nil
}

// rebindPlacementKit claims the selected FPGA kit and points this
// launch at it. A claim conflict leaves the session where it was.
// Ensure is used only when the operator already turned it on.
func (s *Service) rebindPlacementKit(ctx context.Context, snap launchSnapshot, choice meshplace.Choice) (launchSnapshot, error) {
	cfg, ok := s.targetForPlacementNode(choice.Execute)
	if !ok {
		return snap, meshcontent.ErrUnboundNode
	}
	client, err := s.placementClient(cfg)
	if err != nil {
		return snap, err
	}
	claimed, generation, err := s.claimPlacementKit(ctx, client, cfg, choice.Execute)
	if err != nil {
		return snap, err
	}
	s.meshMu.Lock()
	ensure := s.meshEnsure
	s.meshMu.Unlock()
	var exec meshcontent.Executor
	if ensure {
		exec = s.executorForPlacementRebind(ctx, cfg)
		if exec == nil || exec.NodeID() != choice.Execute {
			if claimed {
				snap.client = client
				snap.placementClaimed = true
				snap.placementGeneration = generation
			}
			return snap, meshcontent.ErrUnboundNode
		}
	}
	s.installPlacementRebind(cfg, choice, exec, ensure)
	snap = retargetPlacementSnapshot(snap, cfg, choice, client, exec, ensure)
	snap.placementClaimed = claimed
	snap.placementGeneration = generation
	return snap, nil
}

func (s *Service) installPlacementRebind(cfg TargetConfig, choice meshplace.Choice, exec meshcontent.Executor, ensure bool) {
	nodeID := choice.Execute
	s.targetMu.Lock()
	previous := s.selectedTarget
	s.selectedTarget = cfg.Name
	origin := s.targetOrigin
	lease := kitLeaseOf(s.targetClients[cfg.Name])
	s.targetMu.Unlock()
	s.meshMu.Lock()
	s.meshExecute.BoundNode = nodeID
	s.meshExecute.Placement = placementFromChoice(choice)
	if ensure && exec != nil {
		s.meshExecute.Executor = exec
		s.meshInstalled = meshTargetIdentityOf(cfg.Name, cfg)
	} else if s.meshExecute.Executor != nil && s.meshExecute.Executor.NodeID() != nodeID {
		s.meshExecute.Executor = nil
		s.meshInstalled = meshTargetIdentity{}
	}
	s.meshMu.Unlock()
	// The origin hook is the production path that points CastStart at
	// the selected kit. The lease is the grant just claimed, so media
	// keeps the kit-lease header. Call the hook only after both locks
	// are released, and only when the selected name changed. The hook
	// must not call back into Service.
	if origin != nil && previous != cfg.Name {
		origin(cfg, lease)
	}
	if ensure && exec != nil {
		s.attachMeshAuthorizer(MeshExecuteSession{BoundNode: nodeID, Executor: exec})
	}
}

func retargetPlacementSnapshot(snap launchSnapshot, cfg TargetConfig, choice meshplace.Choice, client serviceClient, exec meshcontent.Executor, ensure bool) launchSnapshot {
	snap.client = client
	snap.boundNode = choice.Execute
	snap.requested = cfg.Name
	snap.name = cfg.Name
	snap.nodeID = choice.Execute
	snap.selectedName = cfg.Name
	snap.explicit = false
	snap.pinned = true
	snap.siblingExecutor = false
	snap.address = strings.TrimSpace(cfg.Address)
	snap.enabled = cfg.Enabled
	snap.known = true
	snap.targetID = cfg.NodeID()
	snap.hadNodeID = strings.TrimSpace(cfg.NodeID()) != ""
	// Keep the claimed kit pinned when ensure is off. frozen makes
	// revalidate check this identity instead of the live selected
	// target. executor stays nil, so Ensure remains a no-op.
	snap.frozen = true
	snap.executor = nil
	if ensure && exec != nil {
		snap.executor = exec
	}
	return snap
}

func placementFromChoice(choice meshplace.Choice) MeshPlacement {
	return MeshPlacement{
		Execute:     choice.Execute,
		DisplaySink: choice.DisplaySink,
		InputSource: choice.InputSource,
	}
}

// targetForPlacementNode is the enabled configured kit whose node id
// is the selected Execute node. A missing or disabled kit is not claimed.
func (s *Service) targetForPlacementNode(nodeID string) (TargetConfig, bool) {
	if s == nil {
		return TargetConfig{}, false
	}
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return TargetConfig{}, false
	}
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	for _, cfg := range s.targets {
		if !cfg.Enabled || strings.TrimSpace(cfg.Name) == "" {
			continue
		}
		if cfg.NodeID() == nodeID || cfg.TargetID == nodeID || cfg.Name == nodeID {
			return cfg, true
		}
	}
	return TargetConfig{}, false
}

// placementClient is the kit client the lease claim uses. Open creates
// a client only for the selected target, so a sibling kit is opened
// with the target client factory. A client that cannot be opened is
// not claimed, and the session stays where it was.
func (s *Service) placementClient(cfg TargetConfig) (serviceClient, error) {
	s.targetMu.Lock()
	defer s.targetMu.Unlock()
	if client := s.targetClients[cfg.Name]; client != nil {
		return client, nil
	}
	if s.targetClientFactory == nil {
		return nil, meshcontent.ErrLeaseNotFree
	}
	client, err := s.targetClientFactory(cfg)
	if err != nil || client == nil {
		return nil, canonicalError(protocol.CodeBadRequest, nil)
	}
	if s.targetClients == nil {
		s.targetClients = map[string]serviceClient{}
	}
	s.targetClients[cfg.Name] = client
	return client, nil
}

// claimPlacementKit claims the selected kit with the existing kit lease.
// A kit this session already holds is not claimed again. A foreign
// holder, or a claim the kit rejects, returns before the session moves.
// This path does not take over a generation. The kit's identity is
// verified before the claim, so a stale address is not claimed and a
// discovered endpoint is adopted first.
func (s *Service) claimPlacementKit(ctx context.Context, client serviceClient, cfg TargetConfig, nodeID string) (bool, string, error) {
	if s.placementKitInUse(nodeID) {
		return false, "", canonicalError(protocol.CodeKitLeaseDenied, nil)
	}
	owned, generation := false, ""
	if lease, ok := client.(meshKitLease); ok && lease != nil {
		owned, generation = lease.MeshKitLease()
	}
	if owned && generation != "" {
		if s.connectionDescribesNode(nodeID) {
			conn := s.TargetConnection()
			if conn.leaseSeen && (!conn.leaseOwned || conn.leaseGeneration != generation) {
				return false, "", meshcontent.ErrLeaseNotFree
			}
		}
		// Another in-flight launch already claimed this grant. Count
		// this launch too, so the first failure cannot drop it. A grant
		// with no in-flight holder belongs to the session and stays held.
		claimed, err := s.adoptPlacementClaim(client, generation)
		return claimed, generation, err
	}
	acquirer, ok := client.(meshPullAcquirer)
	if !ok || acquirer == nil {
		return false, "", meshcontent.ErrLeaseNotFree
	}
	if err := s.verifyPlacementKit(ctx, client, cfg, nodeID); err != nil {
		return false, "", err
	}
	if err := acquirer.AcquireContentPullLease(ctx); err != nil {
		if meshClaimDenied(err) {
			return false, "", canonicalError(protocol.CodeKitLeaseDenied, nil)
		}
		return false, "", err
	}
	owned, generation = false, ""
	if lease, ok := client.(meshKitLease); ok && lease != nil {
		owned, generation = lease.MeshKitLease()
	}
	if !owned || generation == "" {
		return false, "", meshcontent.ErrLeaseNotFree
	}
	s.registerPlacementClaim(client, generation)
	return true, generation, nil
}

// verifyPlacementKit checks the selected kit before its lease is claimed.
// A real client is probed the way admission probes it: a stale address
// is replaced by the one discovered endpoint for that TargetID, and the
// claim then uses that endpoint. A reported identity that is not this
// node is not claimed. A client that does not advertise an identity
// keeps the existing claim path.
func (s *Service) verifyPlacementKit(ctx context.Context, client serviceClient, cfg TargetConfig, nodeID string) error {
	if concrete, ok := client.(*targetclient.Client); ok && concrete != nil {
		health, address, err := s.probeTarget(ctx, concrete, cfg)
		if err != nil {
			return err
		}
		if !placementHealthMatches(health.TargetID, nodeID, cfg) {
			return meshcontent.ErrUnboundNode
		}
		current := ""
		if endpoint := concrete.EndpointURL(); endpoint != nil {
			current = endpoint.String()
		}
		if address == "" || address == current {
			return nil
		}
		base, err := url.Parse(address)
		if err != nil || base.Scheme == "" || base.Host == "" {
			return meshcontent.ErrUnboundNode
		}
		if _, err := concrete.AdoptEndpoint(ctx, base, false); err != nil {
			return err
		}
		return nil
	}
	if client == nil {
		return meshcontent.ErrUnboundNode
	}
	health, err := client.Health(ctx)
	if err != nil {
		return err
	}
	if !placementHealthMatches(health.TargetID, nodeID, cfg) {
		return meshcontent.ErrUnboundNode
	}
	return nil
}

func placementHealthMatches(reported, nodeID string, cfg TargetConfig) bool {
	reported = strings.TrimSpace(reported)
	if reported == "" {
		return true
	}
	nodeID = strings.TrimSpace(nodeID)
	return reported == nodeID || reported == strings.TrimSpace(cfg.TargetID) || reported == strings.TrimSpace(cfg.NodeID())
}

// executorForPlacementRebind is the content executor for a kit placement
// is moving onto. An executor installed for that node is used as-is.
// Otherwise the host dials the configured kit. A miss does not invent
// an executor.
func (s *Service) executorForPlacementRebind(ctx context.Context, cfg TargetConfig) meshcontent.Executor {
	if s == nil {
		return nil
	}
	s.meshMu.Lock()
	executors := s.meshPlacementExecutors
	s.meshMu.Unlock()
	for _, key := range []string{cfg.NodeID(), cfg.TargetID, strings.TrimSpace(cfg.Name)} {
		if key == "" || executors == nil {
			continue
		}
		if exec := executors[key]; exec != nil {
			return exec
		}
	}
	if remote := s.dialNamedMeshExecutor(ctx, cfg.Name); remote != nil {
		return remote
	}
	return nil
}

// placementKitInUse reports that the host already sees nodeID held by
// another session. The observation is the cached target connection.
// This does not dial and does not claim.
func (s *Service) placementKitInUse(nodeID string) bool {
	return s.kitLeaseForeign() && s.connectionDescribesNode(nodeID)
}

func (s *Service) placementEntry(gameID string, snap launchSnapshot) (meshcontent.Entry, bool) {
	if snap.entryOK {
		return snap.entry, true
	}
	s.meshMu.Lock()
	entryFn := s.meshExecute.Entry
	s.meshMu.Unlock()
	if entryFn == nil {
		return meshcontent.Entry{}, false
	}
	return entryFn(gameID)
}

// placementBoundID is the executor this session is already bound to.
// A frozen snapshot uses the node Ensure will keep. Otherwise the
// bound node, the installed executor, or the selected target's node
// id, in that order.
func (s *Service) placementBoundID(snap launchSnapshot) string {
	if snap.frozen {
		if id := strings.TrimSpace(snap.boundNode); id != "" {
			return id
		}
		if snap.executor != nil {
			if id := strings.TrimSpace(snap.executor.NodeID()); id != "" {
				return id
			}
		}
	}
	return s.sessionBoundExecutorID()
}

func (s *Service) sessionBoundExecutorID() string {
	if s == nil {
		return ""
	}
	s.meshMu.Lock()
	session := s.meshExecute
	s.meshMu.Unlock()
	if id := strings.TrimSpace(session.BoundNode); id != "" {
		return id
	}
	if session.Executor != nil {
		if id := strings.TrimSpace(session.Executor.NodeID()); id != "" {
			return id
		}
	}
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	return strings.TrimSpace(targetByName(s.targets, s.selectedTarget).NodeID())
}

// placementStillBoundLocked reports that the session still names the
// executor Place matched. Caller holds meshMu.
func (s *Service) placementStillBoundLocked(snap launchSnapshot, bound string) bool {
	session := s.meshExecute
	if id := strings.TrimSpace(session.BoundNode); id != "" && id != bound {
		return false
	}
	if snap.frozen && snap.executor != nil && session.Executor != snap.executor {
		return false
	}
	return true
}

func placementOnBoundExecutor(choice meshplace.Choice, bound string) bool {
	if bound == "" || choice.Execute != bound {
		return false
	}
	return placementRolesLocal(choice)
}

// placementRolesLocal reports that display and input, when named, are
// the Execute node. A remote pad or a remote picture is not this path.
func placementRolesLocal(choice meshplace.Choice) bool {
	if choice.Execute == "" {
		return false
	}
	if choice.DisplaySink != "" && choice.DisplaySink != choice.Execute {
		return false
	}
	if choice.InputSource != "" && choice.InputSource != choice.Execute {
		return false
	}
	return true
}

// placementFPGANode reports that the selected candidate advertises
// Execute fpga_native. A native_emu selection is not rebound here.
func placementFPGANode(candidates []meshplace.Candidate, nodeID string) bool {
	for _, candidate := range candidates {
		if candidate.NodeID != nodeID {
			continue
		}
		for _, kind := range candidate.Execute {
			if kind == meshcontent.ExecuteFPGANative {
				return true
			}
		}
	}
	return false
}
