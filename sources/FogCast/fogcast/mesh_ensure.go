package fogcast

import (
	"github.com/DeanoC/FogCast/internal/meshcontent"
)

// MeshExecuteSession is the optional seam Launch calls before execute.
// A nil Executor leaves Phase 0 and Phase 1 launch unchanged. Rooms and
// GET /api/v1/games do not read it. The executor is the node the
// session is already bound to. Launch does not choose another node and
// does not release a lease.
type MeshExecuteSession struct {
	BoundNode string
	Executor  meshcontent.Executor
	// Entry returns the projected catalog row for gameID. ok false
	// means this launch is not asking for the mesh content contract.
	Entry func(gameID string) (meshcontent.Entry, bool)
}

// SetMeshExecuteSession installs the ensure seam. Tests pass a fake
// executor. A kit store that pulls bytes through the target agent is
// a later slice.
func (s *Service) SetMeshExecuteSession(session MeshExecuteSession) {
	if s == nil {
		return
	}
	s.meshMu.Lock()
	s.meshExecute = session
	s.meshMu.Unlock()
}

// meshEnsureBeforeExecute runs only when a session executor is installed.
// ErrContentMissingNoSource and ErrUnboundNode fail closed. A slot that
// is still Checking, or an executor that cannot run the package ABI,
// returns ErrExecuteBlocked and Launch must not continue into execute.
func (s *Service) meshEnsureBeforeExecute(gameID string) error {
	if s == nil {
		return nil
	}
	s.meshMu.Lock()
	session := s.meshExecute
	s.meshMu.Unlock()
	if session.Executor == nil {
		return nil
	}
	if session.Entry == nil {
		return meshcontent.ErrUnboundNode
	}
	entry, ok := session.Entry(gameID)
	if !ok {
		return nil
	}
	result, err := meshcontent.Ensure(entry, session.BoundNode, session.Executor)
	if err != nil {
		return err
	}
	return result.Blocked()
}
