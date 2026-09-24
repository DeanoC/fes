package fogcast

import (
	"context"
	"strings"

	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/DeanoC/FogCast/internal/meshcontent"
)

// GameMeshReady is ReadyHere for one title when a mesh execute session
// is installed. NextAction is empty when Ready is true.
type GameMeshReady struct {
	Ready      bool
	Block      meshcontent.Block
	NextAction string
}

// GamesMeshReady evaluates ReadyHere for ids. The boolean is false when
// the ensure seam is off: no executor is installed, or the session has
// no catalog projection. Callers then keep Phase 0 and Phase 1
// composition Ready and omit the ready fields. This does not call
// Ensure and does not pull.
//
// LeaseFree for this view is the session's current grant and generation,
// or an unleased kit this session can claim. A foreign holder is not
// free. Ensure still treats an unleased kit as not owned until pull
// claims the grant.
func (s *Service) GamesMeshReady(ctx context.Context, ids []string) (map[string]GameMeshReady, bool) {
	if s == nil || ctx.Err() != nil {
		return nil, false
	}
	s.meshMu.Lock()
	session := s.meshExecute
	nodes := append([]MeshNode(nil), s.meshNodes...)
	s.meshMu.Unlock()
	if session.Executor == nil || session.Entry == nil {
		return nil, false
	}
	entries := make(map[string]meshcontent.Entry, len(ids))
	ordered := make([]meshcontent.Entry, 0, len(ids))
	for _, id := range ids {
		if _, seen := entries[id]; seen {
			continue
		}
		entry, ok := session.Entry(id)
		if !ok {
			continue
		}
		entries[id] = entry
		ordered = append(ordered, entry)
	}
	views, err := contentViews(ctx, session.Executor, ordered)
	if err != nil {
		if ctx.Err() != nil {
			return nil, false
		}
		out := make(map[string]GameMeshReady, len(entries))
		for id := range entries {
			out[id] = GameMeshReady{
				Ready:      false,
				Block:      meshcontent.BlockInvalid,
				NextAction: meshcontent.NextAction(meshcontent.BlockInvalid),
			}
		}
		return out, true
	}
	out := make(map[string]GameMeshReady, len(entries))
	foreign := neighborExecuteAd(nodes, session.BoundNode)
	majorOK := meshMajorOK(nodes, session.BoundNode)
	for id, entry := range entries {
		if ctx.Err() != nil {
			return nil, false
		}
		bound := s.meshReadyBound(entry, session, majorOK, views)
		ready, block := discovery.ReadyForBoundExecutor(true, foreign, &discovery.ReadyHereInput{
			Entry: entry,
			Bound: bound,
		})
		decision := GameMeshReady{Ready: ready, Block: block}
		if !ready {
			decision.NextAction = meshcontent.NextAction(block)
		}
		out[id] = decision
	}
	return out, true
}

func neighborExecuteAd(nodes []MeshNode, boundNode string) discovery.Advertisement {
	for _, node := range nodes {
		if node.NodeID == boundNode || node.TargetID == boundNode {
			continue
		}
		if len(node.Capabilities.Execute) == 0 {
			continue
		}
		return discovery.Advertisement{
			NodeID:       node.NodeID,
			TargetID:     node.TargetID,
			Capabilities: node.Capabilities,
		}
	}
	return discovery.Advertisement{}
}

// meshReadyBound is the session fact ReadyHere reads. Execute is the
// installed binding, not another node's advertisement. views come from
// one snapshot for the request. Slot reads do not pull. A missing id
// that some source advertises is distant-only.
func (s *Service) meshReadyBound(entry meshcontent.Entry, session MeshExecuteSession, majorOK bool, views map[string]meshcontent.SlotFact) meshcontent.Bound {
	exec := session.Executor
	local := meshcontent.NewCache()
	distant := meshcontent.NewCache()
	var checking []meshcontent.ContentID
	for _, id := range entry.ContentIDs() {
		fact := views[id.String()]
		switch fact.State {
		case meshcontent.StatePresent:
			_ = local.Hold(id)
		case meshcontent.StateChecking:
			checking = append(checking, id)
		default:
			if fact.Advertises {
				_ = distant.Hold(id)
			}
		}
	}
	var packages []string
	if holder, ok := exec.(meshcontent.PackageHolder); ok && holder != nil {
		packages = holder.Packages()
	}
	return meshcontent.Bound{
		Execute:     session.BoundNode != "" && exec.NodeID() == session.BoundNode,
		LeaseFree:   s.readyLeaseFree(entry, session.BoundNode),
		MeshMajorOK: majorOK,
		Local:       local,
		Distant:     distant,
		Checking:    checking,
		Packages:    packages,
		ABIs:        exec.EligibleABIs(),
	}
}

// contentViews reads every required content-id once for this request.
// A ContentSnapshot is one batch. Otherwise each id is read once and
// reused across titles. A transport error is returned and is not Missing.
func contentViews(ctx context.Context, exec meshcontent.Executor, entries []meshcontent.Entry) (map[string]meshcontent.SlotFact, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var ids []meshcontent.ContentID
	seen := map[string]struct{}{}
	for _, entry := range entries {
		for _, id := range entry.ContentIDs() {
			if _, ok := seen[id.String()]; ok {
				continue
			}
			seen[id.String()] = struct{}{}
			ids = append(ids, id)
		}
	}
	if snap, ok := exec.(meshcontent.ContentSnapshot); ok && snap != nil {
		return snap.Snapshot(ctx, ids)
	}
	out := make(map[string]meshcontent.SlotFact, len(ids))
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		state, err := readReadySlot(ctx, exec, id)
		if err != nil {
			return nil, err
		}
		fact := meshcontent.SlotFact{State: state}
		if state == meshcontent.StateMissing {
			advertises, err := readReadySource(ctx, exec, id)
			if err != nil {
				return nil, err
			}
			fact.Advertises = advertises
		}
		out[id.String()] = fact
	}
	return out, nil
}

func readReadySlot(ctx context.Context, exec meshcontent.Executor, id meshcontent.ContentID) (meshcontent.SlotState, error) {
	if reader, ok := exec.(meshcontent.SlotReader); ok && reader != nil {
		return reader.ReadSlot(ctx, id)
	}
	return exec.Slot(id), nil
}

func readReadySource(ctx context.Context, exec meshcontent.Executor, id meshcontent.ContentID) (bool, error) {
	if reader, ok := exec.(meshcontent.SourceReader); ok && reader != nil {
		return reader.ReadSource(ctx, id)
	}
	return exec.SourceAdvertises(id), nil
}

func meshMajorOK(nodes []MeshNode, boundNode string) bool {
	for _, node := range nodes {
		if node.NodeID != boundNode && node.TargetID != boundNode {
			continue
		}
		return discovery.MeshMajorCompatible(node.Mesh)
	}
	return false
}

// readyLeaseFree is LeaseFree for ReadyHere. A host-only title does not
// take the kit lease. An FPGA title is free when this session holds the
// current grant and generation on the bound node, or that node's client
// can claim an unleased kit. A foreign holder is not free. A lost or
// closed grant is not free. A client for a different kit is not used.
// A held grant whose observed generation does not match is not free
// and is not claimed from this path. The generation observation is the
// selected connection only when that connection is this bound node.
func (s *Service) readyLeaseFree(entry meshcontent.Entry, boundNode string) bool {
	if meshLaunchExecution(entry) == ExecutionHostOnly {
		return true
	}
	if s.boundKitForeign(boundNode) {
		return false
	}
	client := s.meshReadyClient(boundNode)
	if abandoned, ok := client.(meshKitLeaseAbandoned); ok && abandoned != nil && abandoned.MeshKitLeaseAbandoned() {
		return false
	}
	owned, generation := false, ""
	if lease, ok := client.(meshKitLease); ok && lease != nil {
		owned, generation = lease.MeshKitLease()
	}
	if owned && generation != "" {
		if s.connectionDescribesNode(boundNode) {
			conn := s.TargetConnection()
			if conn.leaseSeen && (!conn.leaseOwned || conn.leaseGeneration != generation) {
				return false
			}
		}
		return true
	}
	acquirer, ok := client.(meshPullAcquirer)
	return ok && acquirer != nil
}

func (s *Service) boundKitForeign(boundNode string) bool {
	if s == nil || !s.kitLeaseForeign() {
		return false
	}
	conn := s.TargetConnection()
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	for _, cfg := range s.targets {
		if cfg.NodeID() != boundNode && cfg.TargetID != boundNode && cfg.Name != boundNode {
			continue
		}
		if conn.TargetID != "" && cfg.TargetID != "" {
			return conn.TargetID == cfg.TargetID
		}
		if conn.Address != "" && cfg.Address != "" {
			return conn.Address == cfg.Address
		}
		return cfg.Name == strings.TrimSpace(s.selectedTarget)
	}
	return conn.TargetID != "" && conn.TargetID == boundNode
}

func (s *Service) meshReadyClient(boundNode string) serviceClient {
	if s == nil {
		return nil
	}
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	for _, cfg := range s.targets {
		if cfg.NodeID() != boundNode && cfg.TargetID != boundNode && cfg.Name != boundNode {
			continue
		}
		return s.targetClients[cfg.Name]
	}
	return nil
}

// connectionDescribesNode reports whether the cached target connection
// is the bound node. A sibling kit's grant is not compared with it.
func (s *Service) connectionDescribesNode(boundNode string) bool {
	if s == nil || strings.TrimSpace(boundNode) == "" {
		return false
	}
	conn := s.TargetConnection()
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	node := strings.TrimSpace(boundNode)
	cfg := targetByName(s.targets, strings.TrimSpace(s.selectedTarget))
	describes := cfg.NodeID() == node || cfg.TargetID == node || cfg.Name == node
	if !describes {
		return conn.TargetID != "" && conn.TargetID == node
	}
	if conn.TargetID != "" && cfg.TargetID != "" {
		return conn.TargetID == cfg.TargetID
	}
	if conn.Address != "" && cfg.Address != "" {
		return conn.Address == cfg.Address
	}
	return true
}
