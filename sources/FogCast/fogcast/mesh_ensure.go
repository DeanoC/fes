package fogcast

import (
	"strings"

	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/protocol"
)

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
// A launchable FPGA entry is compared to the node LaunchOn will execute
// on. Host-only execution stays on the installed session node, because
// that launch does not bind the named kit. A foreign-kit denial returns
// before Ensure, so a rejected FPGA launch does not pull. A node
// mismatch returns ErrUnboundNode before Pull or Link.
// ErrContentMissingNoSource fails closed. A slot that is still
// Checking, or an executor that cannot run the package ABI, returns
// ErrExecuteBlocked and Launch must not continue into execute.
func (s *Service) meshEnsureBeforeExecute(gameID, target string) error {
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
	node := session.BoundNode
	if entry.Launchable && meshLaunchExecution(entry) != ExecutionHostOnly {
		if s.launchUsesForeignKit(target, ExecutionFPGANative) {
			return canonicalError(protocol.CodeKitLeaseDenied, nil)
		}
		node = s.launchExecuteNode(target, session)
	}
	if strings.TrimSpace(node) == "" || session.Executor.NodeID() != node {
		return meshcontent.ErrUnboundNode
	}
	result, err := meshcontent.Ensure(entry, node, session.Executor)
	if err != nil {
		return err
	}
	return result.Blocked()
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

// launchExecuteNode is the mesh node this FPGA launch will execute on.
// A configured target's node id is its TargetID. An explicit target
// with no id uses its name, so Ensure cannot fall through to some
// other session node. With no target identity, the installed session
// node remains the executor.
func (s *Service) launchExecuteNode(target string, session MeshExecuteSession) string {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	requested := strings.TrimSpace(target)
	name := requested
	if name == "" {
		name = strings.TrimSpace(s.selectedTarget)
	}
	if name == "" {
		return session.BoundNode
	}
	cfg := targetByName(s.targets, name)
	if id := strings.TrimSpace(cfg.NodeID()); id != "" {
		return id
	}
	if requested != "" {
		if strings.TrimSpace(cfg.Name) != "" {
			return cfg.Name
		}
		return requested
	}
	if session.BoundNode != "" && session.BoundNode != name {
		for _, candidate := range s.targets {
			if candidate.Name == "" || candidate.Name == name {
				continue
			}
			if candidate.NodeID() == session.BoundNode || candidate.Name == session.BoundNode {
				return name
			}
		}
	}
	return session.BoundNode
}
