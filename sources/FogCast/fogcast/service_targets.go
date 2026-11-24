package fogcast

import (
	"context"
	"encoding/hex"
	"errors"
	"sort"
	"strings"

	"github.com/DeanoC/FogCast/internal/meshplace"
	"github.com/DeanoC/FogCast/kitlease"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

func (s *Service) selectedClientSnapshot() (serviceClient, bool) {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	return s.selectedClientLocked()
}

func (s *Service) pairedTargetClient(targetID string) (serviceClient, bool) {
	s.pairedTargetMu.Lock()
	defer s.pairedTargetMu.Unlock()
	for _, cfg := range s.pairedTargetConfigs {
		if !cfg.Enabled || cfg.TargetID != targetID {
			continue
		}
		client := s.pairedTargetClients[cfg.Name]
		if client == nil && s.targetClientFactory != nil {
			var err error
			client, err = s.targetClientFactory(cfg)
			if err != nil || client == nil {
				return nil, false
			}
			s.pairedTargetClients[cfg.Name] = client
		}
		return client, client != nil
	}
	return nil, false
}

func (s *Service) selectedClientLocked() (serviceClient, bool) {
	client := s.targetClients[s.sessionTargetNameLocked()]
	return client, client != nil
}

// targetClientLocked resolves one configured target and opens its client on
// first use. Caller holds targetMu.
func (s *Service) targetClientLocked(name string) (serviceClient, bool) {
	if strings.TrimSpace(name) == "" {
		return s.selectedClientLocked()
	}
	cfg := targetByName(s.targets, name)
	if cfg.Name == "" || !cfg.Enabled {
		return nil, false
	}
	if client := s.targetClients[name]; client != nil {
		return client, true
	}
	if s.targetClientFactory == nil {
		return nil, false
	}
	client, err := s.targetClientFactory(cfg)
	if err != nil || client == nil {
		return nil, false
	}
	if s.targetClients == nil {
		s.targetClients = make(map[string]serviceClient)
	}
	s.targetClients[name] = client
	return client, true
}

func (s *Service) clientForSessionContextLocked(ctx context.Context) (serviceClient, bool) {
	name := SessionTargetFromContext(ctx)
	if name == "" {
		return s.selectedClientLocked()
	}
	client := s.targetClients[name]
	return client, client != nil
}

// SessionTargetName reports the target used by an unscoped host session
// operation. A live foreground target wins; otherwise the selected target.
func (s *Service) SessionTargetName() string {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	return s.sessionTargetNameLocked()
}

// ForegroundSessionTargetName reports only a target already bound to the
// foreground FPGA session.
func (s *Service) ForegroundSessionTargetName() string {
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	return s.activeTarget
}

// Caller holds targetMu. Admission and transport must resolve the same binding.
// A launch already bound to a configured target uses that name; otherwise
// the load falls through to the selected kit.
func (s *Service) sessionTargetNameLocked() string {
	name := s.selectedTarget
	s.executionMu.Lock()
	active := s.activeTarget
	bound := s.boundKitTarget
	s.executionMu.Unlock()
	if s.configuredTargetLocked(bound) {
		return bound
	}
	if active == "" {
		return name
	}
	if s.configuredTargetLocked(active) {
		return active
	}
	return name
}

// Caller holds targetMu.
func (s *Service) configuredTargetLocked(name string) bool {
	_, ok := s.targetClients[name]
	return ok
}

// Caller holds targetMu. Preserve a configured kit bind.
func (s *Service) kitBindToKeepLocked() string {
	s.executionMu.Lock()
	active := s.activeTarget
	bound := s.boundKitTarget
	s.executionMu.Unlock()
	if s.configuredTargetLocked(bound) {
		return bound
	}
	if s.configuredTargetLocked(active) {
		return active
	}
	return ""
}

type targetPlay struct {
	execution         string
	gameID            string
	system            protocol.System
	coreID            string
	packageID         string
	packageGeneration uint64
	packageRejection  *protocol.APIError
}

type PlaySession struct {
	Target    string
	TargetID  string
	Execution string
	GameID    string
	System    protocol.System
}

// selectedKitPlayLocked returns the authoritative lifecycle record for the
// kit bound to the current operation.
func (s *Service) selectedKitPlayLocked() (string, targetPlay) {
	name := s.selectedTarget
	if s.boundKitTarget != "" {
		name = s.boundKitTarget
	} else if s.activeTarget != "" {
		name = s.activeTarget
	}
	return name, s.plays[name]
}

func (s *Service) clearForegroundLocked() {
	s.activeExecution, s.activeGameID, s.activeSystem = "", "", ""
}

// bindPlayTargetLocked saves the current kit play and restores the requested
// kit's record. Caller holds executionMu and targetMu.
func (s *Service) bindPlayTargetLocked(name string) {
	s.boundKitTarget = name
	if s.activeTarget == name {
		return
	}
	s.activeTarget = name
}

// notePlayDisplaySink records the last play DisplaySink once a library
// FPGA launch has finished, including its firmware and media. It takes
// targetMu and then executionMu. A launch that did not leave FPGA play
// active does not record.
func (s *Service) notePlayDisplaySink() {
	if s == nil {
		return
	}
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	_, play := s.selectedKitPlayLocked()
	if play.execution != ExecutionFPGANative {
		return
	}
	s.notePlayDisplaySinkLocked()
}

// notePlayDisplaySinkLocked records the bound target's node id as the
// last play DisplaySink. Caller holds executionMu and targetMu.
// An empty node id is unset and does not replace a remembered sink.
// Development loads, status reads and stops do not
// call this.
func (s *Service) notePlayDisplaySinkLocked() {
	if s.displayMemory == nil {
		return
	}
	name := s.boundKitTarget
	if name == "" {
		name = s.activeTarget
	}
	if name == "" {
		name = s.selectedTarget
	}
	s.displayMemory.NotePlayStarted(targetByName(s.targets, name).NodeID())
}

// SetDisplayPreference stores the household display preference node id.
// Empty clears it. The value stays in host memory.
func (s *Service) SetDisplayPreference(nodeID string) {
	if s == nil || s.displayMemory == nil {
		return
	}
	s.displayMemory.SetDisplayPreference(nodeID)
}

// PlaceOptions copies the household display preference and last play
// DisplaySink into meshplace.Options. Empty stays empty. Override and
// a missing composition slot stay unset.
func (s *Service) PlaceOptions() meshplace.Options {
	if s == nil || s.displayMemory == nil {
		return meshplace.Options{}
	}
	return s.displayMemory.PlaceOptions()
}

func (s *Service) PlaySessions() []PlaySession {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	out := make([]PlaySession, 0, len(s.plays))
	for name, play := range s.plays {
		if play.execution == "" || play.packageRejection != nil {
			continue
		}
		out = append(out, PlaySession{
			Target:    name,
			TargetID:  targetByName(s.targets, name).TargetID,
			Execution: play.execution,
			GameID:    play.gameID,
			System:    play.system,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Target < out[j].Target })
	return out
}

func (s *Service) clearUnstartedSessionTarget() {
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	if _, ok := s.plays[s.activeTarget]; !ok {
		s.activeTarget = ""
	}
	if _, ok := s.plays[s.boundKitTarget]; !ok {
		s.boundKitTarget = ""
	}
}

// launchPinnedTargetBoundHook observes the name bind stored. Tests move
// selectedTarget after Ensure and read this name. It is nil outside tests
// and must not call back into Service.
var launchPinnedTargetBoundHook func(name string)

// bindLiveLaunchTarget resolves the selected target under targetMu at
// bind time. LaunchOn uses it when no mesh session is installed, so a
// settings change that selects another target or replaces its client
// is the endpoint this launch binds.
func (s *Service) bindLiveLaunchTarget(target string) error {
	s.targetMu.Lock()
	defer s.targetMu.Unlock()
	explicit := strings.TrimSpace(target) != ""
	name := strings.TrimSpace(target)
	if name == "" {
		name = s.selectedTarget
	}
	cfg := targetByName(s.targets, name)
	if explicit && (strings.TrimSpace(cfg.Name) == "" || !cfg.Enabled) {
		return canonicalError(protocol.CodeBadRequest, nil)
	}
	if _, ok := s.targetClients[name]; !ok {
		if !explicit {
			s.executionMu.Lock()
			s.bindPlayTargetLocked(name)
			s.executionMu.Unlock()
			if launchPinnedTargetBoundHook != nil {
				launchPinnedTargetBoundHook(name)
			}
			return nil
		}
		if s.targetClientFactory == nil {
			return canonicalError(protocol.CodeInternal, nil)
		}
		client, err := s.targetClientFactory(cfg)
		if err != nil || client == nil {
			return canonicalError(protocol.CodeBadRequest, nil)
		}
		s.targetClients[name] = client
	}
	s.executionMu.Lock()
	s.bindPlayTargetLocked(name)
	s.executionMu.Unlock()
	if s.targetOrigin != nil && explicit && name != s.selectedTarget {
		hook, lease := s.targetOrigin, kitLeaseOf(s.targetClients[name])
		s.targetMu.Unlock()
		hook(cfg, lease)
		s.targetMu.Lock()
	}
	if launchPinnedTargetBoundHook != nil {
		launchPinnedTargetBoundHook(name)
	}
	return nil
}

func (s *Service) targetNameForID(targetID string) string {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	for _, target := range s.targets {
		if target.Enabled && target.TargetID == targetID {
			return target.Name
		}
	}
	return ""
}

// TargetNameForID resolves a configured paired target identity.
func (s *Service) TargetNameForID(targetID string) string { return s.targetNameForID(targetID) }

func (s *Service) TargetIDForName(name string) string {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	return targetByName(s.targets, name).TargetID
}

// SetTargetOrigin registers a hook that follows selected-target identity changes.
// The lease argument is the kit grant for that target and may be nil. The
// callback must not call back into Service or send network cleanup requests.
func (s *Service) SetTargetOrigin(hook func(TargetConfig, *targetclient.KitLease)) {
	s.targetMu.Lock()
	s.targetOrigin = hook
	s.targetMu.Unlock()
}

// kitLeaseOf is the kit grant attached to client. A client without one
// returns nil. The caller supplies a client it already holds.
func kitLeaseOf(client serviceClient) *targetclient.KitLease {
	leased, ok := client.(interface{ KitLease() *targetclient.KitLease })
	if !ok || leased == nil {
		return nil
	}
	return leased.KitLease()
}

// KitLocalForeignLease reads the current local agent lease without acquiring it.
// It is only an observation: the target still authorizes each local input edge.
func (s *Service) KitLocalForeignLease(ctx context.Context, name string) (bool, error) {
	if s == nil || !s.kitHost || name != KitHostTargetName {
		return false, nil
	}
	s.targetMu.RLock()
	target := targetByName(s.targets, name)
	client, ok := s.targetClients[name].(*targetclient.Client)
	s.targetMu.RUnlock()
	if !ok || client == nil || !target.Enabled || target.TargetID == "" {
		return false, errors.New("kit lease observation unavailable")
	}
	status, err := client.KitLeaseStatus(ctx)
	if err != nil {
		return false, err
	}
	s.targetMu.RLock()
	current := targetByName(s.targets, name)
	same := s.targetClients[name] == client && current.Enabled && current.TargetID == target.TargetID && current.Address == target.Address && current.Agent == target.Agent
	s.targetMu.RUnlock()
	if !same {
		return false, errors.New("kit target changed during lease observation")
	}
	if status.State != "held" {
		return false, nil
	}
	if len(status.Generation) != 64 || status.Owner == "" {
		return false, errors.New("kit lease observation is incomplete")
	}
	if _, err := hex.DecodeString(status.Generation); err != nil {
		return false, errors.New("kit lease generation is invalid")
	}
	owned, generation := client.MeshKitLease()
	return !owned || generation != status.Generation, nil
}

// KitHostMode reports whether this service controls its own kit agent.
func (s *Service) KitHostMode() bool { return s != nil && s.kitHost }

// SessionTarget is the FPGA target bound to the host session: the active
// execution target, or the selected configured target when idle.
func (s *Service) SessionTarget() (name, targetID string) {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	s.executionMu.Lock()
	name = s.activeTarget
	s.executionMu.Unlock()
	if name == "" {
		name = s.selectedTarget
	}
	return name, targetByName(s.targets, name).TargetID
}

// SelectedTargetConfig returns the configured selected target.
func (s *Service) SelectedTargetConfig() TargetConfig {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	return targetByName(s.targets, s.selectedTarget)
}

// TargetConfigForName returns an immutable copy of one configured target.
func (s *Service) TargetConfigForName(name string) TargetConfig {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	return targetByName(s.targets, name)
}

// PairedTargetKitLeaseStatus reads only the lease for an enabled target ID.
// Callers on the paired listener must supply the ID authenticated by pairing.
func (s *Service) PairedTargetKitLeaseStatus(ctx context.Context, targetID string) (kitlease.Status, error) {
	status, _, _, err := s.PairedTargetStatus(ctx, targetID)
	return status, err
}

// PairedTargetStatus reads lease and health for one enabled target ID.
func (s *Service) PairedTargetStatus(ctx context.Context, targetID string) (kitlease.Status, bool, bool, error) {
	client, ok := s.pairedTargetClient(targetID)
	if !ok {
		return kitlease.Status{}, false, false, errors.New("paired target unavailable")
	}
	health, healthErr := client.Health(ctx)
	provider, ok := client.(interface {
		KitLeaseStatus(context.Context) (kitlease.Status, error)
	})
	if !ok {
		return kitlease.Status{}, false, false, errors.New("paired target lease status unavailable")
	}
	status, err := provider.KitLeaseStatus(ctx)
	if err != nil {
		return kitlease.Status{}, false, false, err
	}
	if healthErr != nil {
		return status, false, false, nil
	}
	return status, true, health.Ready, nil
}

// KitLease returns the application-owned lease for the selected session target.
func (s *Service) KitLease() *targetclient.KitLease {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	client, ok := s.selectedClientLocked()
	if !ok {
		return nil
	}
	if leased, ok := client.(interface{ KitLease() *targetclient.KitLease }); ok {
		return leased.KitLease()
	}
	return nil
}

// KitLeaseForTarget returns the application-owned lease for a named target.
func (s *Service) KitLeaseForTarget(name string) *targetclient.KitLease {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	client := s.targetClients[name]
	if leased, ok := client.(interface{ KitLease() *targetclient.KitLease }); ok {
		return leased.KitLease()
	}
	return nil
}

// ShutdownCleanupRequired reports whether this process still has local
// ownership that permits shutdown cleanup. It never contacts or mutates a
// target: execution needs a currently held grant on any target with a retained play.
func (s *Service) ShutdownCleanupRequired() bool {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	s.executionMu.Lock()
	for name := range s.plays {
		client := s.targetClients[name]
		owner, ok := client.(interface{ HasKitGrant() bool })
		if ok && owner.HasKitGrant() {
			s.executionMu.Unlock()
			return true
		}
	}
	s.executionMu.Unlock()
	client := s.targetClients[s.sessionTargetNameLocked()]
	if client == nil {
		return false
	}
	owner, ok := client.(interface{ HasKitGrant() bool })
	return ok && owner.HasKitGrant()
}

// ReleaseKitLease is for an explicit user Stop after input/media cleanup.
// It releases every grant captured by Stop since the previous release,
// including a kit dropped from targetClients by an idle selected-target
// change. A grant that still backs a remaining PlaySession is left held:
// Soft-stop keeps the lease in stoppedKitLeases, and relaunch does not
// drop that entry, so stopping a different foreground target must not
// revoke the live session. A failed explicit Stop still calls this when
// the coordinator was already idle, so idle grants from earlier Soft-stops
// are released while the surviving play's grant stays held. Sofa Soft-stop
// (session stop with retain_lease) does not call it. A failed release stays
// in stoppedKitLeases.
// KitLease.Release keeps that grant renewing so a later Stop can retry it;
// only a successful release drops the entry. Replacement Stop retains
// ownership so the next launch uses the same grant.
func (s *Service) ReleaseKitLease(ctx context.Context) error {
	return s.releaseKitLease(ctx, "")
}

// ReleaseKitLeaseTarget releases retained grants belonging to one target.
func (s *Service) ReleaseKitLeaseTarget(ctx context.Context, target string) error {
	return s.releaseKitLease(ctx, target)
}

func (s *Service) releaseKitLease(ctx context.Context, target string) error {
	s.executionMu.Lock()
	pending := append([]*targetclient.KitLease(nil), s.stoppedKitLeases...)
	targets := make(map[*targetclient.KitLease]string, len(s.stoppedKitLeaseTargets))
	for lease, name := range s.stoppedKitLeaseTargets {
		targets[lease] = name
	}
	s.executionMu.Unlock()
	var first error
	for _, lease := range pending {
		if target != "" && targets[lease] != target {
			continue
		}
		if s.kitLeaseBacksPlay(lease) {
			continue
		}
		if err := lease.Release(ctx); err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		s.executionMu.Lock()
		s.stoppedKitLeases = dropStoppedKitLease(s.stoppedKitLeases, lease)
		delete(s.stoppedKitLeaseTargets, lease)
		s.executionMu.Unlock()
	}
	return first
}

// kitLeaseBacksPlay reports whether lease is the grant of a target that
// still has a play. Caller must not hold targetMu or executionMu.
func (s *Service) kitLeaseBacksPlay(lease *targetclient.KitLease) bool {
	if lease == nil {
		return false
	}
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	return s.kitLeaseBacksPlayLocked(lease)
}

// kitLeaseBacksPlayLocked uses the target binding already held by the caller.
// Caller holds targetMu and must not hold executionMu.
func (s *Service) kitLeaseBacksPlayLocked(lease *targetclient.KitLease) bool {
	if lease == nil {
		return false
	}
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	for name := range s.plays {
		leased, ok := s.targetClients[name].(interface{ KitLease() *targetclient.KitLease })
		if ok && leased.KitLease() == lease {
			return true
		}
	}
	return false
}

func retainStoppedKitLease(leases []*targetclient.KitLease, lease *targetclient.KitLease) []*targetclient.KitLease {
	if lease == nil {
		return leases
	}
	for _, existing := range leases {
		if existing == lease {
			return leases
		}
	}
	return append(leases, lease)
}

func dropStoppedKitLease(leases []*targetclient.KitLease, lease *targetclient.KitLease) []*targetclient.KitLease {
	if lease == nil || len(leases) == 0 {
		return leases
	}
	kept := make([]*targetclient.KitLease, 0, len(leases))
	for _, existing := range leases {
		if existing != lease {
			kept = append(kept, existing)
		}
	}
	if len(kept) == len(leases) {
		return leases
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}
