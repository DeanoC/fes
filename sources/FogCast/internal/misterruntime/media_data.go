package misterruntime

import (
	"context"
	"github.com/DeanoC/FogCast/protocol"
	"io"
)

const MediaDataRoot = CoreDataRoot + "/media"

type protocol2LibraryMediaControl interface {
	InsertLibraryMedia(context.Context, string, string, protocol.LibraryMediaBinding, uint32) (Protocol2Response, error)
	SaveMedia(context.Context, protocol.MediaUnitBinding) (Protocol2Response, error)
}

func (c *Client) InsertLibraryMedia(ctx context.Context, path, root string, b protocol.LibraryMediaBinding, size uint32) (Protocol2Response, error) {
	if !validRuntimePath(path) || !validRuntimePath(root) || !b.Valid() || !protocol.AdmitAtariStFloppySize(int64(size)) {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	line, _, err := c.callRawTracked(ctx, struct {
		Protocol   int    `json:"protocol"`
		Operation  string `json:"operation"`
		Path       string `json:"path"`
		PackageID  string `json:"expected_package_id"`
		Generation uint64 `json:"expected_generation"`
		Unit       uint8  `json:"unit"`
		Size       uint32 `json:"size"`
		Root       string `json:"data_root"`
		GameID     string `json:"game_id"`
		BaseID     string `json:"base_media_id"`
	}{2, "insert_library_media", path, b.PackageID, b.Generation, b.Unit, size, root, b.GameID, b.BaseMediaID})
	if err != nil {
		return Protocol2Response{}, err
	}
	return decodeComputerMediaResponse(line, b.PackageID, b.Generation)
}
func (c *Client) SaveMedia(ctx context.Context, b protocol.MediaUnitBinding) (Protocol2Response, error) {
	if !b.Valid() {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	line, _, err := c.callRawTracked(ctx, struct {
		Protocol   int    `json:"protocol"`
		Operation  string `json:"operation"`
		PackageID  string `json:"expected_package_id"`
		Generation uint64 `json:"expected_generation"`
		Unit       uint8  `json:"unit"`
	}{2, "save_media", b.PackageID, b.Generation, b.Unit})
	if err != nil {
		return Protocol2Response{}, err
	}
	return decodeComputerMediaResponse(line, b.PackageID, b.Generation)
}
func (r *Runtime) InsertLibraryMedia(ctx, owner context.Context, size int64, body io.Reader, b protocol.LibraryMediaBinding) ([]protocol.MediaUnitStatus, *protocol.APIError) {
	if !b.Valid() || !protocol.AdmitAtariStFloppySize(size) {
		return nil, protocol.MediaUnitRequestError()
	}
	if _, ok := r.control.(protocol2LibraryMediaControl); !ok {
		return nil, unsupportedOperationError()
	}
	return r.insertMedia(ctx, owner, size, body, b.MediaUnitBinding, &b)
}
func (r *Runtime) SaveMedia(ctx context.Context, b protocol.MediaUnitBinding) ([]protocol.MediaUnitStatus, *protocol.APIError) {
	if !b.Valid() {
		return nil, protocol.MediaUnitRequestError()
	}
	control, ok := r.control.(protocol2LibraryMediaControl)
	if !ok {
		return nil, unsupportedOperationError()
	}
	r.computerMu.Lock()
	defer r.computerMu.Unlock()
	statusControl, ok := r.control.(protocol2StatusControl)
	if !ok {
		return nil, unsupportedOperationError()
	}
	before, err := statusControl.Protocol2Status(ctx)
	if err != nil {
		return nil, unavailableError()
	}
	prior, ok := computerMediaSaveStatus(before, b)
	if !ok {
		return nil, protocol.MediaUnitIdentityError()
	}
	response, err := control.SaveMedia(ctx, b)
	r.noteDispatch("save_media", err == nil)
	if err != nil {
		return nil, unavailableError()
	}
	if !response.OK {
		return nil, mapProtocol2Error(response.Error)
	}
	after, ok := computerMediaSaveStatus(response, b)
	if !ok || !b.MatchesSaveResult(prior, after) {
		return nil, unavailableError()
	}
	return cloneMediaUnits(response.Capabilities.MediaUnits), nil
}

// Only Save may inspect the active generation retained after save_failed/save.
// Neutralizing that error in a local copy leaves all generic response guards
// unchanged; a failed mutation response is never a successful checkpoint.
func computerMediaSaveStatus(response Protocol2Response, b protocol.MediaUnitBinding) (protocol.Status, bool) {
	clean := response
	if response.Error != nil {
		if response.Error.Code != "save_failed" || response.Error.Phase != "save" {
			return protocol.Status{}, false
		}
		clean.Error = nil
		clean.OK = true
	}
	if !computerResponseMatches(clean, b.PackageID, b.Generation) {
		return protocol.Status{}, false
	}
	status := computerMediaStatus(clean)
	if response.Error != nil {
		status.LastError = mapProtocol2Error(response.Error)
	}
	return status, b.MatchesForSave(status)
}
