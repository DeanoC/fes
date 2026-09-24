package fogcast

import (
	"errors"
	"reflect"
	"strings"

	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/protocol"
)

// errMeshCompositionChanged means the catalog row Ensure checked is no
// longer the row launch would program. Launch must not continue.
var errMeshCompositionChanged = errors.New("ensured catalog composition changed before launch")

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

// meshEnsureFinishedHook runs after a successful Ensure and before
// launch reads the catalog again. Tests change the composition or the
// named target here. It is nil outside tests and must not call back
// into Service.
var meshEnsureFinishedHook func()

// meshEnsureBeforeExecute runs only when a session executor is installed.
// pinned is the target LaunchOn already captured for this call. FPGA
// ensure uses that identity and does not read selectedTarget again.
// Host-only execution stays on the installed session node, because that
// launch does not bind the named kit. A foreign-kit denial returns
// before Ensure, so a rejected FPGA launch does not pull. A node
// mismatch returns ErrUnboundNode before Pull or Link.
// The returned entry is the composition Ensure checked. Launch compares
// it again under lifecycle admission before programming.
// ErrContentMissingNoSource fails closed. A slot that is still
// Checking, or an executor that cannot run the package ABI, returns
// ErrExecuteBlocked and Launch must not continue into execute.
func (s *Service) meshEnsureBeforeExecute(gameID string, pinned pinnedLaunchTarget) (meshcontent.Entry, bool, error) {
	if s == nil {
		return meshcontent.Entry{}, false, nil
	}
	s.meshMu.Lock()
	session := s.meshExecute
	s.meshMu.Unlock()
	if session.Executor == nil {
		return meshcontent.Entry{}, false, nil
	}
	if session.Entry == nil {
		return meshcontent.Entry{}, false, meshcontent.ErrUnboundNode
	}
	entry, ok := session.Entry(gameID)
	if !ok {
		return meshcontent.Entry{}, false, nil
	}
	node := session.BoundNode
	if entry.Launchable && meshLaunchExecution(entry) != ExecutionHostOnly {
		if s.launchUsesForeignKit(pinned, ExecutionFPGANative) {
			return meshcontent.Entry{}, false, canonicalError(protocol.CodeKitLeaseDenied, nil)
		}
		node = pinned.nodeID
	}
	if strings.TrimSpace(node) == "" || session.Executor.NodeID() != node {
		return meshcontent.Entry{}, false, meshcontent.ErrUnboundNode
	}
	result, err := meshcontent.Ensure(entry, node, session.Executor)
	if err != nil {
		return meshcontent.Entry{}, false, err
	}
	if err := result.Blocked(); err != nil {
		return meshcontent.Entry{}, false, err
	}
	if meshEnsureFinishedHook != nil {
		meshEnsureFinishedHook()
	}
	return entry, true, nil
}

// meshLaunchCompositionChanged reports that the catalog row now selected
// is not the row Ensure already checked. Caller holds lifecycle admission,
// so a selector cannot change the row again before programming.
func (s *Service) meshLaunchCompositionChanged(gameID string, ensured meshcontent.Entry, ensuredOK bool) error {
	if s == nil || !ensuredOK {
		return nil
	}
	s.meshMu.Lock()
	session := s.meshExecute
	s.meshMu.Unlock()
	if session.Executor == nil || session.Entry == nil {
		return nil
	}
	current, ok := session.Entry(gameID)
	if !ok || !reflect.DeepEqual(ensured, current) {
		return errMeshCompositionChanged
	}
	return nil
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

// pinnedLaunchTarget is the kit LaunchOn will bind. It is captured once
// and then shared by Ensure and bind. name does not follow a later
// selectedTarget change. nodeID, address, and client are the endpoint
// Ensure validated. Bind rejects a renamed address or TargetID, and it
// keeps this client when the endpoint is unchanged.
type pinnedLaunchTarget struct {
	requested    string
	name         string
	selectedName string
	nodeID       string
	address      string
	client       serviceClient
	explicit     bool
	hadNodeID    bool
}

func (p pinnedLaunchTarget) endpointMatches(cfg TargetConfig) bool {
	if strings.TrimSpace(cfg.Address) != p.address {
		return false
	}
	if p.hadNodeID && strings.TrimSpace(cfg.NodeID()) != p.nodeID {
		return false
	}
	return true
}

// pinLaunchTarget resolves an implicit launch to the selected target
// that is current at this instant. name is what bind stores. nodeID is
// what FPGA Ensure compares to the executor. Neither is read again.
func (s *Service) pinLaunchTarget(target string) pinnedLaunchTarget {
	if s == nil {
		return pinnedLaunchTarget{}
	}
	s.meshMu.Lock()
	session := s.meshExecute
	s.meshMu.Unlock()
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	requested := strings.TrimSpace(target)
	selected := strings.TrimSpace(s.selectedTarget)
	name := requested
	if name == "" {
		name = selected
	}
	pinned := pinnedLaunchTarget{
		requested:    requested,
		name:         name,
		selectedName: selected,
		explicit:     requested != "",
	}
	if name == "" {
		pinned.nodeID = session.BoundNode
		return pinned
	}
	cfg := targetByName(s.targets, name)
	pinned.address = strings.TrimSpace(cfg.Address)
	pinned.client = s.targetClients[name]
	if id := strings.TrimSpace(cfg.NodeID()); id != "" {
		pinned.nodeID = id
		pinned.hadNodeID = true
		return pinned
	}
	if pinned.explicit {
		if strings.TrimSpace(cfg.Name) != "" {
			pinned.nodeID = cfg.Name
		} else {
			pinned.nodeID = requested
		}
		return pinned
	}
	if session.BoundNode != "" && session.BoundNode != name {
		for _, candidate := range s.targets {
			if candidate.Name == "" || candidate.Name == name {
				continue
			}
			if candidate.NodeID() == session.BoundNode || candidate.Name == session.BoundNode {
				pinned.nodeID = name
				return pinned
			}
		}
	}
	pinned.nodeID = session.BoundNode
	return pinned
}
