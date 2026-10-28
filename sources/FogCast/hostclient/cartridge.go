package hostclient

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/DeanoC/FogCast/protocol"
)

// CartridgeSwapCapable is a running fes.application machine with a linked
// cartridge and the kit session plane: its media selector swaps the
// cartridge by relaunching the title.
func CartridgeSwapCapable(p *SessionCorePackage) bool {
	return SessionDisplayCapable(p) && p.ABI == (SessionCoreABI{ID: "fes.application", Major: 1}) &&
		linkedCartridgeSize(p) > 0
}

// linkedCartridgeSize is the running cartridge's source size: the single
// linked ROM, or the cartridge source after a linked BIOS.
func linkedCartridgeSize(p *SessionCorePackage) int64 {
	switch {
	case p.ROMLinks != nil:
		sources := p.ROMLinks.Sources
		if p.ROMLink != nil || len(sources) != 2 || sources[0].Role != protocol.FirmwareRole ||
			sources[0].SourceSize <= 0 || sources[1].Role != "cartridge" {
			return 0
		}
		return sources[1].SourceSize
	case p.ROMLink != nil:
		if p.ROMLink.Role == "cartridge" {
			return p.ROMLink.SourceSize
		}
	}
	return 0
}

// AdmitCartridge accepts a dump the running cartridge's exact size.
func AdmitCartridge(p *SessionCorePackage, name string, size int64) bool {
	return CartridgeSwapCapable(p) && protocol.AdmitCartridgeName(name) && size == linkedCartridgeSize(p)
}

// ReplaceCartridgeForSession relaunches the captured session's title with
// mediaID as its cartridge on the same kit. The result is the new load: the
// same title and target with a new package generation.
func (c *Client) ReplaceCartridgeForSession(ctx context.Context, prior SessionResult, mediaID, name string) (SessionResult, error) {
	b := protocol.DevelopmentMediaBinding{Target: prior.Target, TargetID: prior.TargetID}
	if prior.CorePackage != nil {
		b.PackageID, b.Generation = prior.CorePackage.PackageID, prior.CorePackage.Generation
	}
	if prior.State != "active" || strings.TrimSpace(prior.ID) == "" || b.Target == "" || !b.Valid() || !CartridgeSwapCapable(prior.CorePackage) {
		return SessionResult{}, protocol.CartridgeIdentityError()
	}
	if protocol.ValidateDigest(mediaID) != nil || !protocol.AdmitCartridgeName(name) {
		return SessionResult{}, protocol.CartridgeRequestError()
	}
	payload, err := json.Marshal(protocol.CartridgeRequest{MediaID: mediaID, Name: name})
	if err != nil {
		return SessionResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, liveMediaTimeout)
	defer cancel()
	transport := *c.HTTPClient()
	transport.Timeout = 0
	client := *c
	client.httpClient = &transport
	result, err := client.postLiveMedia(ctx, "/api/v1/session/cartridge", bytes.NewReader(payload), int64(len(payload)), prior.ID, b)
	if err == nil && (result.ID != prior.ID || result.State != "active" || result.Target != prior.Target || result.TargetID != prior.TargetID || result.GameID != prior.GameID ||
		result.CorePackage == nil || result.CorePackage.PackageID != b.PackageID || result.CorePackage.Generation == b.Generation) {
		return result, protocol.CartridgeIdentityError()
	}
	return result, err
}
