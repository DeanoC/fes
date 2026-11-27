package hostclient

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/DeanoC/FogCast/protocol"
)

// PcFloppyBinding binds pc386's A: (unit 1) or B: (unit 2) of the session.
func PcFloppyBinding(prior SessionResult, unit uint8) (protocol.MediaUnitBinding, error) {
	if prior.ID == "" || prior.Target == "" || prior.State != "active" || prior.CorePackage == nil || !protocol.PcFloppyUnit(unit) {
		return protocol.MediaUnitBinding{}, protocol.MediaUnitIdentityError()
	}
	b := protocol.MediaUnitBinding{PackageID: prior.CorePackage.PackageID, Generation: prior.CorePackage.Generation, Unit: unit, Target: prior.Target, TargetID: prior.TargetID}
	if u, ok := protocol.MediaUnit(diskPackage(prior.CorePackage), unit); !ok || u.Interface != protocol.PcFloppyInterface() || !b.Valid() {
		return b, protocol.MediaUnitIdentityError()
	}
	return b, nil
}

// InsertPcFloppyForSession puts a library floppy in A: or B:. A writable disk
// comes back bound to its saved record; a write-protected one to none.
func (c *Client) InsertPcFloppyForSession(ctx context.Context, prior SessionResult, unit uint8, gameID, baseID string, writeProtected bool) (SessionResult, error) {
	b, err := PcFloppyBinding(prior, unit)
	if err != nil {
		return SessionResult{}, err
	}
	library := protocol.LibraryMediaBinding{MediaUnitBinding: b, GameID: gameID, BaseMediaID: baseID, WriteProtected: writeProtected}
	if !library.Valid() {
		return SessionResult{}, protocol.MediaUnitRequestError()
	}
	payload, _ := json.Marshal(struct {
		GameID         string `json:"game_id"`
		BaseID         string `json:"base_media_id"`
		WriteProtected bool   `json:"write_protected,omitempty"`
	}{gameID, baseID, writeProtected})
	after, err := c.postDisk(ctx, "/api/v1/session/disk/insert", prior, b, payload)
	if err != nil {
		return after, err
	}
	current, ok := protocol.MediaUnit(diskPackage(after.CorePackage), unit)
	if !ok || current.Interface != protocol.PcFloppyInterface() || current.State != protocol.MediaUnitReady ||
		writeProtected != (current.Persistence == nil) ||
		(current.Persistence != nil && (current.Persistence.GameID != gameID || current.Persistence.BaseMediaID != baseID)) {
		return after, errors.New("floppy insertion response lost its binding")
	}
	return after, nil
}

// EjectPcFloppyForSession empties A: or B:, saving a writable disk first.
func (c *Client) EjectPcFloppyForSession(ctx context.Context, prior SessionResult, unit uint8) (SessionResult, error) {
	b, err := PcFloppyBinding(prior, unit)
	if err != nil {
		return SessionResult{}, err
	}
	after, err := c.postDisk(ctx, "/api/v1/session/disk/eject", prior, b, nil)
	if err != nil {
		return after, err
	}
	if current, ok := protocol.MediaUnit(diskPackage(after.CorePackage), unit); !ok || current.State != protocol.MediaUnitEmpty {
		return after, errors.New("floppy eject response still shows a disk")
	}
	return after, nil
}
