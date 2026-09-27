package fogcast

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/DeanoC/FogCast/internal/kitcontent"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/internal/meshplace"
)

const (
	// placementNodeTTL is how long one node document read is reused
	// for fpga_native eligibility.
	placementNodeTTL = 10 * time.Second
	// placementNodeBackoff is how long a failed read is remembered, so
	// an unreachable kit does not stall every games read.
	placementNodeBackoff = 5 * time.Second
	// placementNodeTimeout bounds one node document read.
	placementNodeTimeout = 2 * time.Second
)

// placementNodeRead is one node document read for placement. identity
// is the configured kit the read used; a read for another identity is
// not reused. ok is false when the read failed or named another node.
type placementNodeRead struct {
	identity meshTargetIdentity
	abis     []meshcontent.EligibleABI
	ok       bool
	at       time.Time
}

// EnableMeshPlacement turns production placement on when configuration
// asked for it. Open records that choice and does not read a node.
// Production mains call this after Open. [mesh] placement defaults off,
// so an unset key keeps today's bind. This does not turn mesh ensure on.
func (s *Service) EnableMeshPlacement() {
	if s == nil {
		return
	}
	s.meshMu.Lock()
	defer s.meshMu.Unlock()
	if !s.meshPlacementConfig.Enabled {
		return
	}
	s.meshPlacement = true
	if s.meshHTTP == nil {
		s.meshHTTP = &http.Client{}
	}
}

// placementAsk is the placement request for one launch or one games
// read. An installed ask is used as it is. Otherwise production
// placement builds one from the node inventory and the configured
// override. Nil means the caller is not asking. An empty inventory is
// not a placement decision, so it returns nil and the bind stays.
func (s *Service) placementAsk(ctx context.Context) *MeshPlacementAsk {
	if s == nil {
		return nil
	}
	s.meshMu.Lock()
	ask := s.meshPlacementAsk
	wired := s.meshPlacement
	override := s.meshPlacementConfig.Override
	s.meshMu.Unlock()
	if ask != nil {
		return ask
	}
	if !wired {
		return nil
	}
	candidates := s.placementCandidates(ctx)
	if len(candidates) == 0 {
		return nil
	}
	return &MeshPlacementAsk{Candidates: candidates, OverrideNodeID: override}
}

// placementEntryFunc is the projection placement reads when the launch
// snapshot has none. The installed session's projection wins.
// Production placement projects the host library itself when no
// session is installed, so placement does not need the ensure seam.
func (s *Service) placementEntryFunc() func(string) (meshcontent.Entry, bool) {
	if s == nil {
		return nil
	}
	s.meshMu.Lock()
	entryFn := s.meshExecute.Entry
	wired := s.meshPlacement
	s.meshMu.Unlock()
	if entryFn == nil && wired {
		return s.meshCatalogEntry
	}
	return entryFn
}

// placementCandidates builds the Place input from the node inventory.
// Execute kinds, DisplaySink, InputSource, and the mesh major come from
// each advertisement. An fpga_native row carries the abis of that
// node's content document, read only when the node is an enabled
// configured kit. An advertisement alone is not eligibility. Rows keep
// inventory order (node id order), which is the order Place reads when
// several native_emu nodes can run a title.
func (s *Service) placementCandidates(ctx context.Context) []meshplace.Candidate {
	nodes := s.MeshNodes()
	out := make([]meshplace.Candidate, 0, len(nodes))
	for _, node := range nodes {
		candidate := meshplace.Candidate{
			NodeID:      node.NodeID,
			MeshMajorOK: discovery.MeshMajorCompatible(node.Mesh),
			DisplaySink: node.Capabilities.DisplaySink,
			InputSource: node.Capabilities.InputSource,
		}
		fpga := false
		for _, execute := range node.Capabilities.Execute {
			candidate.Execute = append(candidate.Execute, execute.Kind)
			fpga = fpga || execute.Kind == meshcontent.ExecuteFPGANative
		}
		if fpga {
			candidate.ABIs = s.placementNodeABIs(ctx, node.NodeID)
		}
		out = append(out, candidate)
	}
	return out
}

// placementNodeABIs is the abis placement passes for nodeID. The node
// must be an enabled configured kit with an address, an agent token,
// and that TargetID, and its content document must name nodeID. A
// failed read, another node id, and an unconfigured node return nil,
// which is not eligibility. A read is reused for placementNodeTTL and
// a failure for placementNodeBackoff. A read the caller canceled is
// not remembered. The read is not a kit-lease mutation.
func (s *Service) placementNodeABIs(ctx context.Context, nodeID string) []meshcontent.EligibleABI {
	cfg, ok := s.targetForPlacementNode(nodeID)
	if !ok {
		return nil
	}
	want := meshTargetIdentityOf(cfg.Name, cfg)
	if !want.dialable() || want.nodeID != nodeID {
		return nil
	}
	s.meshMu.Lock()
	cached, hit := s.placementNodes[nodeID]
	client := s.meshHTTP
	s.meshMu.Unlock()
	if hit && cached.identity == want {
		ttl := placementNodeTTL
		if !cached.ok {
			ttl = placementNodeBackoff
		}
		if time.Since(cached.at) < ttl {
			return append([]meshcontent.EligibleABI(nil), cached.abis...)
		}
	}
	abis, ok := readPlacementNode(ctx, want, client)
	if !ok && ctx != nil && ctx.Err() != nil {
		return nil
	}
	s.meshMu.Lock()
	if s.placementNodes == nil {
		s.placementNodes = map[string]placementNodeRead{}
	}
	s.placementNodes[nodeID] = placementNodeRead{identity: want, abis: abis, ok: ok, at: time.Now()}
	s.meshMu.Unlock()
	return append([]meshcontent.EligibleABI(nil), abis...)
}

// readPlacementNode reads one kit's content document with that kit's
// agent token. The document must name the configured node id.
func readPlacementNode(ctx context.Context, want meshTargetIdentity, client *http.Client) ([]meshcontent.EligibleABI, bool) {
	endpoint, err := url.Parse(want.address)
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" {
		return nil, false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		client = &http.Client{}
	}
	readCtx, cancel := context.WithTimeout(ctx, placementNodeTimeout)
	defer cancel()
	node, err := kitcontent.ReadNode(readCtx, endpoint, want.agent, client)
	if err != nil || node.NodeID != want.nodeID {
		return nil, false
	}
	return node.ABIs, true
}
