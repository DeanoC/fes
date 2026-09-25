package fogcast

import (
	"strings"

	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/internal/meshplace"
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
// session. Launch and Ensure keep that bind. A selection that names
// any other node returns ErrUnboundNode and does not change the bind.
// Unresolved and fail closed do not name an Execute node, so they are
// not recorded and are not that refusal. No request returns nil.
func (s *Service) applyMatchingPlacement(gameID string, snap launchSnapshot) error {
	if s == nil {
		return nil
	}
	s.meshMu.Lock()
	ask := s.meshPlacementAsk
	s.meshMu.Unlock()
	if ask == nil {
		return nil
	}
	entry, ok := s.placementEntry(gameID, snap)
	if !ok {
		return nil
	}
	opts := s.PlaceOptions()
	opts.OverrideNodeID = ask.OverrideNodeID
	opts.MissingRequiredSlot = ask.MissingRequiredSlot
	result := meshplace.Place(entry, ask.Candidates, opts)
	if result.Outcome != meshplace.OutcomeSelected {
		return nil
	}
	bound := s.placementBoundID(snap)
	if !placementOnBoundExecutor(result.Choice, bound) {
		return meshcontent.ErrUnboundNode
	}
	s.meshMu.Lock()
	if !s.placementStillBoundLocked(snap, bound) {
		s.meshMu.Unlock()
		return meshcontent.ErrUnboundNode
	}
	s.meshExecute.Placement = MeshPlacement{
		Execute:     result.Choice.Execute,
		DisplaySink: result.Choice.DisplaySink,
		InputSource: result.Choice.InputSource,
	}
	s.meshMu.Unlock()
	return nil
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
	if choice.DisplaySink != "" && choice.DisplaySink != bound {
		return false
	}
	if choice.InputSource != "" && choice.InputSource != bound {
		return false
	}
	return true
}
