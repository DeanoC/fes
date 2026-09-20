package fogcast

import (
	"context"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

type nativeTargetKey struct{ name, address, id string }
type nativeObservation struct {
	known bool
	cores *protocol.NativeCoreAvailability
}

func nativeKey(target TargetConfig) nativeTargetKey {
	return nativeTargetKey{target.Name, target.Address, target.TargetID}
}

func (s *Service) rememberNativeAvailability(target TargetConfig, health protocol.Health, err error) {
	observation := nativeObservation{known: err == nil}
	if err == nil && health.NativeCores != nil {
		value := *health.NativeCores
		if value.Systems != nil {
			value.Systems = append([]protocol.System{}, value.Systems...)
		}
		observation.cores = &value
	}
	s.nativeAvailabilityMu.Lock()
	defer s.nativeAvailabilityMu.Unlock()
	if s.nativeAvailability == nil {
		s.nativeAvailability = map[nativeTargetKey]nativeObservation{}
	}
	s.nativeAvailability[nativeKey(target)] = observation
}

// No network calls: browsing consumes a copied health observation. Admission
// refreshes health independently before resolving the session-bound target.
func (s *Service) nativeSystemAvailable(system protocol.System, bound bool) bool {
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	return s.nativeSystemAvailableLocked(system, bound)
}

// Caller holds targetMu. Launch admission must not recursively acquire RLock:
// a queued target writer prevents a second reader from entering.
func (s *Service) nativeSystemAvailableLocked(system protocol.System, bound bool) bool {
	name := s.selectedTarget
	if bound {
		name = s.sessionTargetNameLocked()
	}
	target := targetByName(s.targets, name)
	_, real := s.targetClients[name].(*targetclient.Client)
	s.nativeAvailabilityMu.RLock()
	defer s.nativeAvailabilityMu.RUnlock()
	observation, observed := s.nativeAvailability[nativeKey(target)]
	if !observed {
		return !real && catalog.Launchable(system)
	}
	if !observation.known {
		return false
	}
	// Omitted capability retains the existing Main/older-agent contract. It
	// is not a positive native file-presence observation.
	if observation.cores == nil {
		return catalog.Launchable(system)
	}
	if !observation.cores.Valid() {
		return false
	}
	for _, available := range observation.cores.Systems {
		if available == system {
			return true
		}
	}
	return false
}

// Caller holds targetMu, as launchGame does for the entire launch transaction.
func (s *Service) nativeCatalogAdmission(ctx context.Context, game catalog.Game) error {
	execution, err := s.resolveExecution(ctx, game)
	if err != nil {
		return err
	}
	if execution == ExecutionHostOnly || execution == ExecutionFPGADevelopment {
		return nil
	}
	// Protocol mapping precedes target file availability: an unmapped platform
	// is not a supported native system with a missing installed core.
	if !catalog.Launchable(game.System) {
		return canonicalError(protocol.CodeUnsupportedSystem, nil)
	}
	if !s.nativeSystemAvailableLocked(game.System, true) {
		return &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "legacy native core is unavailable on the selected target; choose an installed FPGA package", Phase: "admission"}
	}
	return nil
}
