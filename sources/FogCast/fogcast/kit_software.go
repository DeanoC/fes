package fogcast

import (
	"context"

	"github.com/DeanoC/FogCast/appliance/software"
	"github.com/DeanoC/FogCast/protocol"
)

type kitSoftwareInventoryClient interface {
	KitSoftwareReleases(context.Context) ([]software.Release, error)
}

// KitSoftwareReleases reads immutable release contracts from the same target
// as health. It neither claims a lease nor changes the software selection.
func (s *Service) KitSoftwareReleases(parent context.Context) ([]software.Release, error) {
	ctx, cancel := serviceTimeout(parent, s.requestTimeout)
	defer cancel()
	var client serviceClient
	var ok bool
	if targetID := PairedTargetFromContext(ctx); targetID != "" {
		// Paired reads must not wait behind another target's launch lock.
		client, ok = s.pairedTargetClient(targetID)
	} else {
		release, err := s.acquireLifecycle(ctx)
		if err != nil {
			return nil, err
		}
		if _, err := s.refreshTargetConnection(ctx); err != nil {
			release()
			return nil, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
		}
		client, ok = s.selectedClientSnapshot()
		// Inventory verifies release bytes on the kit and can be slow. Keep
		// the captured client, but never hold foreground admission while waiting.
		release()
	}
	if !ok {
		return nil, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	inventory, ok := client.(kitSoftwareInventoryClient)
	if !ok {
		return nil, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	releases, err := inventory.KitSoftwareReleases(ctx)
	if err != nil {
		return nil, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
	}
	return releases, nil
}
