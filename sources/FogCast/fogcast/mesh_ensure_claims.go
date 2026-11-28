package fogcast

import (
	"context"
	"errors"
	"time"

	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/protocol"
)

// meshKitLease is the session-owned kit grant on one target client.
// generation is empty when this session does not hold a current grant.
type meshKitLease interface {
	MeshKitLease() (owned bool, generation string)
}

// meshKitLeaseAbandoned is a grant this session held and then lost,
// closed, or let expire. A client that never claimed is not abandoned.
type meshKitLeaseAbandoned interface {
	MeshKitLeaseAbandoned() bool
}

// meshPullAcquirer claims the session grant the way content pull does.
// A client that cannot claim leaves the launch fail-closed.
type meshPullAcquirer interface {
	AcquireContentPullLease(context.Context) error
}

// meshPullReleaser releases a grant this call claimed. A grant the
// session already held is not released here.
type meshPullReleaser interface {
	ReleaseContentPullLease(context.Context) error
}

// claimContentPullLease acquires the session grant before Ensure when
// this launch does not already hold one. A held grant that failed the
// generation check is not claimed again. After a successful claim,
// LeaseFree is that new grant; the selected connection's previous free
// observation is not a mismatch against a grant that did not exist yet.
func (s *Service) claimContentPullLease(ctx context.Context, snap launchSnapshot) error {
	owned, generation := false, ""
	if lease, ok := snap.client.(meshKitLease); ok && lease != nil {
		owned, generation = lease.MeshKitLease()
	}
	if owned && generation != "" {
		return meshcontent.ErrLeaseNotFree
	}
	acquirer, ok := snap.client.(meshPullAcquirer)
	if !ok || acquirer == nil {
		return meshcontent.ErrLeaseNotFree
	}
	if err := acquirer.AcquireContentPullLease(ctx); err != nil {
		if meshClaimDenied(err) {
			return canonicalError(protocol.CodeKitLeaseDenied, nil)
		}
		return err
	}
	owned, generation = false, ""
	if lease, ok := snap.client.(meshKitLease); ok && lease != nil {
		owned, generation = lease.MeshKitLease()
	}
	if !owned || generation == "" {
		return meshcontent.ErrLeaseNotFree
	}
	return nil
}

// releaseLaunchClaim drops a grant this launch claimed when Ensure
// does not start execution. claimed is the grant Ensure itself
// acquired. placementClaimed is the grant a rebind acquired. A grant
// the session already held is neither, and it stays held. The
// placement grant is settled only when the release succeeds, so a
// failed release stays available for LaunchOn to retry.
func (s *Service) releaseLaunchClaim(snap launchSnapshot, claimed bool) {
	if !claimed && !snap.placementClaimed {
		return
	}
	if !s.releaseClaimedContentLease(snap) {
		return
	}
	if snap.placementClaimSettled != nil {
		*snap.placementClaimSettled = true
	}
}

// releaseClaimedContentLease drops a grant this launch claimed after
// Ensure did not start execution. A placement claim is released only
// when no other in-flight launch still holds it and the session did
// not already own it. The call uses its own deadline so a canceled
// launch still attempts the release. A failed release leaves the grant
// in place. The bool is false when the grant was not dropped and this
// launch should retry. It is true when the grant was dropped or when
// another holder keeps it.
func (s *Service) releaseClaimedContentLease(snap launchSnapshot) bool {
	if snap.placementClaimed {
		return s.releasePlacementHold(snap)
	}
	return releaseContentPullGrant(snap)
}

// placementClaimRecord is one target client's in-flight placement holds.
// sessionHeld means a launch started execution, or the session already
// owned the grant before this placement. releasing is set while the last
// holder is asking the kit to drop the grant.
type placementClaimRecord struct {
	generation  string
	inflight    int
	sessionHeld bool
	releasing   bool
}

func (s *Service) placementHoldLocked(client serviceClient) *placementClaimRecord {
	if s.placementHolds == nil {
		s.placementHolds = map[serviceClient]*placementClaimRecord{}
	}
	rec := s.placementHolds[client]
	if rec == nil {
		rec = &placementClaimRecord{}
		s.placementHolds[client] = rec
	}
	return rec
}

// registerPlacementClaim counts a grant this launch just acquired.
func (s *Service) registerPlacementClaim(client serviceClient, generation string) {
	if s == nil || client == nil || generation == "" {
		return
	}
	s.placementHoldMu.Lock()
	defer s.placementHoldMu.Unlock()
	rec := s.placementHoldLocked(client)
	if rec.releasing || rec.generation != generation {
		rec.inflight = 0
		rec.sessionHeld = false
		rec.releasing = false
	}
	rec.generation = generation
	rec.inflight++
}

// adoptPlacementClaim counts a launch that found the grant already
// owned. An in-flight placement claim of the same generation is adopted.
// A grant with no in-flight holder is the session's and is not adopted.
// A grant that is being released is not joined.
func (s *Service) adoptPlacementClaim(client serviceClient, generation string) (bool, error) {
	if s == nil || client == nil || generation == "" {
		return false, meshcontent.ErrLeaseNotFree
	}
	s.placementHoldMu.Lock()
	defer s.placementHoldMu.Unlock()
	rec := s.placementHoldLocked(client)
	if rec.releasing {
		return false, meshcontent.ErrLeaseNotFree
	}
	if rec.inflight > 0 && rec.generation == generation {
		rec.inflight++
		return true, nil
	}
	if rec.inflight > 0 && rec.generation != generation {
		return false, meshcontent.ErrLeaseNotFree
	}
	rec.sessionHeld = true
	if rec.generation == "" {
		rec.generation = generation
	}
	return false, nil
}

// settlePlacementClaim keeps the grant and the rebound session once
// execution has started. LaunchOn's failure defer then leaves both in
// place.
func (s *Service) settlePlacementClaim(snap launchSnapshot) {
	if snap.placementKept != nil {
		*snap.placementKept = true
	}
	if snap.placementClaimSettled == nil {
		return
	}
	if snap.placementClaimed {
		s.retainPlacementClaim(snap.client, snap.placementGeneration)
	}
	*snap.placementClaimSettled = true
}

func (s *Service) retainPlacementClaim(client serviceClient, generation string) {
	if s == nil || client == nil {
		return
	}
	s.placementHoldMu.Lock()
	defer s.placementHoldMu.Unlock()
	rec := s.placementHolds[client]
	if rec == nil {
		return
	}
	if generation != "" && rec.generation != generation {
		return
	}
	if rec.inflight > 0 {
		rec.inflight--
	}
	rec.sessionHeld = true
	rec.releasing = false
}

// releasePlacementHold drops this launch's hold. The network release
// runs only for the last in-flight holder of a placement-created grant.
func (s *Service) releasePlacementHold(snap launchSnapshot) bool {
	if !s.beginPlacementRelease(snap.client, snap.placementGeneration) {
		return true
	}
	if !releaseContentPullGrant(snap) {
		s.abortPlacementRelease(snap.client, snap.placementGeneration)
		return false
	}
	s.finishPlacementRelease(snap.client, snap.placementGeneration)
	return true
}

func (s *Service) beginPlacementRelease(client serviceClient, generation string) bool {
	if s == nil || client == nil {
		return true
	}
	s.placementHoldMu.Lock()
	defer s.placementHoldMu.Unlock()
	rec := s.placementHolds[client]
	if rec == nil {
		return true
	}
	if generation != "" && rec.generation != generation {
		return false
	}
	if rec.releasing {
		return false
	}
	if rec.inflight > 1 {
		rec.inflight--
		return false
	}
	if rec.inflight == 1 && rec.sessionHeld {
		rec.inflight--
		return false
	}
	if rec.inflight <= 0 {
		return !rec.sessionHeld
	}
	rec.releasing = true
	return true
}

func (s *Service) abortPlacementRelease(client serviceClient, generation string) {
	if s == nil || client == nil {
		return
	}
	s.placementHoldMu.Lock()
	defer s.placementHoldMu.Unlock()
	rec := s.placementHolds[client]
	if rec == nil {
		return
	}
	if generation != "" && rec.generation != generation {
		return
	}
	rec.releasing = false
}

func (s *Service) finishPlacementRelease(client serviceClient, generation string) {
	if s == nil || client == nil {
		return
	}
	s.placementHoldMu.Lock()
	defer s.placementHoldMu.Unlock()
	rec := s.placementHolds[client]
	if rec == nil {
		return
	}
	if generation != "" && rec.generation != generation {
		return
	}
	if rec.inflight > 0 {
		rec.inflight--
	}
	rec.releasing = false
	if rec.inflight == 0 {
		rec.generation = ""
		rec.sessionHeld = false
	}
}

func releaseContentPullGrant(snap launchSnapshot) bool {
	releaser, ok := snap.client.(meshPullReleaser)
	if !ok || releaser == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return releaser.ReleaseContentPullLease(ctx) == nil
}

func meshClaimDenied(err error) bool {
	var api *protocol.APIError
	if errors.As(err, &api) {
		switch string(api.Code) {
		case string(protocol.CodeKitLeaseDenied), "KIT_LEASE_BUSY", "KIT_LEASE_REQUIRED", "KIT_LEASE_BLOCKED":
			return true
		}
	}
	var lease *meshcontent.LeaseDeniedError
	return errors.As(err, &lease)
}

// meshLeaseFacts is what Ensure's LeaseFree and InUse options are.
// FPGA play is owned only when this session holds the kit grant and,
// once ownership has been observed, that observation is the same
// generation. A free or unleased kit is not owned. InUse is the Phase 1
// busy connection for the kit this launch would execute on.
func (s *Service) meshLeaseFacts(snap launchSnapshot) (leaseFree, inUse bool) {
	inUse = s.launchUsesForeignKit(snap)
	owned, generation := false, ""
	if lease, ok := snap.client.(meshKitLease); ok {
		owned, generation = lease.MeshKitLease()
	}
	if !owned || generation == "" {
		return false, inUse
	}
	// The selected connection's generation is this launch's grant only
	// when the launch executes on that kit. An explicit launch to
	// another target uses that target's client grant alone.
	if s.launchObservesSelectedConnection(snap) {
		conn := s.TargetConnection()
		if conn.leaseSeen && (!conn.leaseOwned || conn.leaseGeneration != generation) {
			return false, inUse
		}
	}
	return true, inUse
}

// Native software executors are unsupported; do not pull or claim a kit for them.
func meshEntryHasNativeEmulator(entry meshcontent.Entry) bool {
	for _, execute := range entry.Execute {
		if execute.Kind == meshcontent.ExecuteNativeEmu {
			return true
		}
	}
	return false
}
