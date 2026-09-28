package fogcast

import (
	"context"
	"log"
	"strings"
	"time"
)

// placementLeaseLogf records placement-rebind lease decisions.
// Lines name the target and the node id only, never a lease token.
// Tests replace it to capture the text.
var placementLeaseLogf = log.Printf

// meshRebindReleaser releases the kit grant a session holds on a client
// it is leaving. notHeld is true when the kit reports that grant is
// already gone.
type meshRebindReleaser interface {
	ReleaseKitGrant(context.Context) (notHeld bool, err error)
}

// releaseLeftPlacementLease releases, best effort, the kit lease the
// session still holds on the kit a successful placement rebind left
// (#281). docs/mesh-vnext.md Q5: release through the kit lease API
// (POST /v1/kit/release), never an idle Stop. An idle Stop reprograms
// a menu kit and blips HDMI. A failed release is logged and does not
// change the launch result; the grant stays reachable for a later
// explicit Stop. No lease token is logged.
func (s *Service) releaseLeftPlacementLease(undo placementSessionUndo) {
	if s == nil {
		return
	}
	if undo.installedName == "" || undo.selectedName == "" || undo.selectedName == undo.installedName {
		placementRebindLog("skip left-kit release: same target")
		return
	}
	s.targetMu.RLock()
	old := s.targetClients[undo.selectedName]
	installed := s.targetClients[undo.installedName]
	current := s.selectedTarget
	oldCfg := targetByName(s.targets, undo.selectedName)
	newCfg := targetByName(s.targets, undo.installedName)
	s.targetMu.RUnlock()

	node := placementLeftNode(oldCfg, undo)
	if current == undo.selectedName {
		placementRebindLog("skip left-kit release on %q (node %q): session is back on the left kit", undo.selectedName, node)
		return
	}
	if old == nil {
		placementRebindLog("skip left-kit release on %q (node %q): left client is missing", undo.selectedName, node)
		return
	}
	if old == installed {
		placementRebindLog("skip left-kit release on %q (node %q): same client", undo.selectedName, node)
		return
	}
	oldLease := kitLeaseOf(old)
	if oldLease != nil && oldLease == kitLeaseOf(installed) {
		placementRebindLog("skip left-kit release on %q (node %q): same grant", undo.selectedName, node)
		return
	}
	if placementRebindSameKit(oldCfg, newCfg, undo) {
		placementRebindLog("skip left-kit release on %q (node %q): same kit", undo.selectedName, node)
		return
	}
	releaser, ok := old.(meshRebindReleaser)
	if !ok || releaser == nil {
		placementRebindLog("skip left-kit release on %q (node %q): client cannot release a kit grant", undo.selectedName, node)
		return
	}
	if lease, ok := old.(meshKitLease); ok && lease != nil {
		if owned, _ := lease.MeshKitLease(); !owned {
			placementRebindLog("skip left-kit release on %q (node %q): grant is not held", undo.selectedName, node)
			return
		}
	}
	s.executionMu.Lock()
	_, playing := s.plays[undo.selectedName]
	s.executionMu.Unlock()
	if playing {
		placementRebindLog("skip left-kit release on %q (node %q): left kit has a play", undo.selectedName, node)
		return
	}
	if !s.beginLeftKitRelease(old) {
		placementRebindLog("skip left-kit release on %q (node %q): placement hold in flight", undo.selectedName, node)
		return
	}
	released := false
	defer func() { s.finishLeftKitRelease(old, released) }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	notHeld, err := releaser.ReleaseKitGrant(ctx)
	if err != nil {
		placementRebindLog("release of kit lease on %q (node %q) failed: %v", undo.selectedName, node, err)
		s.executionMu.Lock()
		s.stoppedKitLeases = retainStoppedKitLease(s.stoppedKitLeases, kitLeaseOf(old))
		s.executionMu.Unlock()
		return
	}
	s.executionMu.Lock()
	s.stoppedKitLeases = dropStoppedKitLease(s.stoppedKitLeases, kitLeaseOf(old))
	s.executionMu.Unlock()
	released = true
	if notHeld {
		placementRebindLog("release of kit lease on %q (node %q) already released", undo.selectedName, node)
		return
	}
	placementRebindLog("released kit lease on %q (node %q)", undo.selectedName, node)
}

func placementRebindLog(format string, args ...any) {
	placementLeaseLogf("fogcast: placement rebind: "+format, args...)
}

func placementLeftNode(cfg TargetConfig, undo placementSessionUndo) string {
	if node := strings.TrimSpace(cfg.NodeID()); node != "" {
		return node
	}
	return strings.TrimSpace(undo.boundNode)
}

// placementRebindSameKit reports that the kit being left is the kit just
// bound, either by node id or by a shared non-empty target id.
func placementRebindSameKit(oldCfg, newCfg TargetConfig, undo placementSessionUndo) bool {
	oldNode := strings.TrimSpace(oldCfg.NodeID())
	if oldNode != "" && (oldNode == strings.TrimSpace(undo.installedNode) || oldNode == strings.TrimSpace(newCfg.NodeID())) {
		return true
	}
	oldID := strings.TrimSpace(oldCfg.TargetID)
	return oldID != "" && oldID == strings.TrimSpace(newCfg.TargetID)
}

// beginLeftKitRelease reserves the left kit's placement record so
// adoptPlacementClaim fails closed instead of joining a grant that is
// being released. It returns false when a launch still holds the grant
// or a release is already in progress.
func (s *Service) beginLeftKitRelease(client serviceClient) bool {
	if s == nil || client == nil {
		return false
	}
	s.placementHoldMu.Lock()
	defer s.placementHoldMu.Unlock()
	rec := s.placementHoldLocked(client)
	if rec.inflight > 0 || rec.releasing {
		return false
	}
	rec.releasing = true
	return true
}

// finishLeftKitRelease clears the reservation. released drops the
// recorded generation so the session no longer treats the grant as held.
func (s *Service) finishLeftKitRelease(client serviceClient, released bool) {
	if s == nil || client == nil {
		return
	}
	s.placementHoldMu.Lock()
	defer s.placementHoldMu.Unlock()
	rec := s.placementHolds[client]
	if rec == nil {
		return
	}
	rec.releasing = false
	if !released {
		return
	}
	rec.generation = ""
	rec.sessionHeld = false
}
