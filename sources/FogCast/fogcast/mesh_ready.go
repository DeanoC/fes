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
	out := make(map[string]GameMeshReady, len(ids))
	foreign := neighborExecuteAd(nodes, session.BoundNode)
	for _, id := range ids {
		if _, seen := out[id]; seen {
			continue
		}
		entry, ok := session.Entry(id)
		if !ok {
			continue
		}
		bound := s.meshReadyBound(entry, session)
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
// installed binding, not another node's advertisement. Slot reads do
// not pull. A missing id that some source advertises is distant-only.
func (s *Service) meshReadyBound(entry meshcontent.Entry, session MeshExecuteSession) meshcontent.Bound {
	exec := session.Executor
	local := meshcontent.NewCache()
	distant := meshcontent.NewCache()
	var checking []meshcontent.ContentID
	for _, id := range entry.ContentIDs() {
		switch exec.Slot(id) {
		case meshcontent.StatePresent:
			_ = local.Hold(id)
		case meshcontent.StateChecking:
			checking = append(checking, id)
		default:
			if exec.SourceAdvertises(id) {
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
		MeshMajorOK: meshMajorOK(s.meshNodesCopy(), session.BoundNode),
		Local:       local,
		Distant:     distant,
		Checking:    checking,
		Packages:    packages,
		ABIs:        exec.EligibleABIs(),
	}
}

func (s *Service) meshNodesCopy() []MeshNode {
	if s == nil {
		return nil
	}
	s.meshMu.Lock()
	defer s.meshMu.Unlock()
	return append([]MeshNode(nil), s.meshNodes...)
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
// current grant and generation, or the kit is unleased and claimable.
// A foreign holder is not free. A held grant whose observed generation
// does not match is not free and is not claimed from this path.
func (s *Service) readyLeaseFree(entry meshcontent.Entry, boundNode string) bool {
	if meshLaunchExecution(entry) == ExecutionHostOnly {
		return true
	}
	if s.boundKitForeign(boundNode) {
		return false
	}
	owned, generation := false, ""
	if lease, ok := s.meshReadyClient(boundNode).(meshKitLease); ok && lease != nil {
		owned, generation = lease.MeshKitLease()
	}
	if !owned || generation == "" {
		return true
	}
	conn := s.TargetConnection()
	if conn.leaseSeen && (!conn.leaseOwned || conn.leaseGeneration != generation) {
		return false
	}
	return true
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
		if client := s.targetClients[cfg.Name]; client != nil {
			return client
		}
	}
	if client := s.targetClients[strings.TrimSpace(s.selectedTarget)]; client != nil {
		return client
	}
	return nil
}
