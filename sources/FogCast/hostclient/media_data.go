package hostclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/DeanoC/FogCast/protocol"
	"net/http"
	"time"
)

func DiskBinding(prior SessionResult) (protocol.MediaUnitBinding, error) {
	if prior.ID == "" || prior.Target == "" || prior.State != "active" || prior.CorePackage == nil || prior.CorePackage.ABI != (SessionCoreABI{ID: "fes.computer", Major: 1}) {
		return protocol.MediaUnitBinding{}, protocol.MediaUnitIdentityError()
	}
	b := protocol.MediaUnitBinding{PackageID: prior.CorePackage.PackageID, Generation: prior.CorePackage.Generation, Unit: protocol.AtariStFloppyUnit, Target: prior.Target, TargetID: prior.TargetID}
	u, known := protocol.MediaUnit(diskPackage(prior.CorePackage), b.Unit)
	known = known && u.Interface == protocol.AtariStFloppyInterface()
	if !b.Valid() || !known {
		return b, protocol.MediaUnitIdentityError()
	}
	return b, nil
}
func (c *Client) InsertLibraryDiskForSession(ctx context.Context, prior SessionResult, gameID, baseID string) (SessionResult, error) {
	b, err := DiskBinding(prior)
	if err != nil {
		return SessionResult{}, err
	}
	library := protocol.LibraryMediaBinding{MediaUnitBinding: b, GameID: gameID, BaseMediaID: baseID}
	if !library.Valid() {
		return SessionResult{}, protocol.MediaUnitRequestError()
	}
	payload, _ := json.Marshal(map[string]string{"game_id": gameID, "base_media_id": baseID})
	return c.postDisk(ctx, "/api/v1/session/disk/insert", prior, b, payload)
}
func diskPackage(p *SessionCorePackage) *protocol.CorePackageStatus {
	if p == nil {
		return nil
	}
	out := &protocol.CorePackageStatus{PackageID: p.PackageID, Generation: p.Generation, ABI: protocol.RuntimeContract{ID: p.ABI.ID, Major: p.ABI.Major, Minor: p.ABI.Minor}, MediaUnits: p.MediaUnits, PersistenceMode: p.PersistenceMode}
	for _, i := range p.ActiveInterfaces {
		out.ActiveInterfaces = append(out.ActiveInterfaces, protocol.RuntimeInterface{ID: i.ID, Major: i.Major, Minor: i.Minor})
	}
	return out
}
func (c *Client) SaveDiskForSession(ctx context.Context, prior SessionResult) (SessionResult, error) {
	b, err := DiskBinding(prior)
	if err != nil {
		return SessionResult{}, err
	}
	bound := false
	for _, u := range prior.CorePackage.MediaUnits {
		if u.Unit == b.Unit && u.Persistence != nil && u.Valid() {
			bound = true
		}
	}
	if !bound || !protocol.MediaDataBound(diskPackage(prior.CorePackage)) || prior.CorePackage.PersistenceMode != "persistent" {
		return SessionResult{}, protocol.MediaUnitRequestError()
	}
	after, err := c.postDisk(ctx, "/api/v1/session/disk/save", prior, b, nil)
	if err != nil {
		return after, err
	}
	retained := false
	for _, u := range after.CorePackage.MediaUnits {
		if u.Unit == b.Unit && u.Persistence != nil && u.Valid() {
			for _, old := range prior.CorePackage.MediaUnits {
				if old.Unit == u.Unit && old.Persistence != nil && old.Persistence.GameID == u.Persistence.GameID && old.Persistence.BaseMediaID == u.Persistence.BaseMediaID {
					retained = true
				}
			}
		}
	}
	if !retained || !protocol.MediaDataBound(diskPackage(after.CorePackage)) || after.CorePackage.PersistenceMode != "persistent" {
		return after, errors.New("disk checkpoint response lost its durable binding")
	}
	return after, nil
}
func (c *Client) postDisk(ctx context.Context, path string, prior SessionResult, b protocol.MediaUnitBinding, payload []byte) (SessionResult, error) {
	req, err := c.NewRequest(ctx, http.MethodPost, path, bytes.NewReader(payload))
	if err != nil {
		return SessionResult{}, err
	}
	b.SetHeaders(req.Header)
	req.Header.Set(protocol.HostSessionIDHeader, prior.ID)
	req.ContentLength = int64(len(payload))
	if len(payload) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	client := *c.HTTPClient()
	budget := 150 * time.Second
	if len(payload) > 0 {
		budget = 450 * time.Second
	}
	if client.Timeout == 0 || client.Timeout < budget {
		client.Timeout = budget
	}
	order := c.beginSessionObservation()
	response, err := client.Do(req)
	if err != nil {
		return SessionResult{}, err
	}
	defer response.Body.Close()
	raw, err := ReadResponseBody(response, maxResponseBytes)
	if err != nil {
		return SessionResult{}, err
	}
	result, err := DecodeSession(response.StatusCode, raw)
	if err == nil && response.StatusCode != http.StatusOK {
		return result, liveMediaAPIError(result)
	}
	if err != nil {
		return result, err
	}
	if result.ID != prior.ID || result.Target != prior.Target || result.TargetID != prior.TargetID || result.CorePackage == nil || result.CorePackage.PackageID != b.PackageID || result.CorePackage.Generation != b.Generation {
		return SessionResult{}, errors.New("disk response changed session identity")
	}
	c.recordSessionMutation(order, result)
	return result, nil
}
