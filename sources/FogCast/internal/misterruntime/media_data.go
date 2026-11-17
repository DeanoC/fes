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
	if !validRuntimePath(path) || !validRuntimePath(root) || !b.Valid() || !protocol.AdmitLibraryFloppySize(int64(size)) {
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
	if !b.Valid() || !protocol.AdmitLibraryFloppySize(size) {
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

type protocol2MediaRecordControl interface {
	InspectMediaData(context.Context, string, protocol.MediaRecordIdentity) (Protocol2Response, error)
	DiscardMediaData(context.Context, string, protocol.MediaRecordDiscard) (Protocol2Response, error)
}

type mediaRecordRequest struct {
	Protocol  int    `json:"protocol"`
	Operation string `json:"operation"`
	Root      string `json:"data_root"`
	CoreID    string `json:"core_id"`
	GameID    string `json:"game_id"`
	BaseID    string `json:"base_media_id"`
	Unit      uint8  `json:"unit"`
	Revision  string `json:"expected_revision,omitempty"`
}

func (c *Client) InspectMediaData(ctx context.Context, root string, id protocol.MediaRecordIdentity) (Protocol2Response, error) {
	if !validRuntimePath(root) || !id.Valid() {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	return c.mediaRecord(ctx, mediaRecordRequest{2, "inspect_media_data", root, id.CoreID, id.GameID, id.BaseMediaID, id.Unit, ""})
}

func (c *Client) DiscardMediaData(ctx context.Context, root string, d protocol.MediaRecordDiscard) (Protocol2Response, error) {
	if !validRuntimePath(root) || !d.Valid() {
		return Protocol2Response{}, errInvalidRuntimeRequest
	}
	return c.mediaRecord(ctx, mediaRecordRequest{2, "discard_media_data", root, d.CoreID, d.GameID, d.BaseMediaID, d.Unit, d.ExpectedRevision})
}

func (c *Client) mediaRecord(ctx context.Context, request mediaRecordRequest) (Protocol2Response, error) {
	line, _, err := c.callRawTracked(ctx, request) // Never replay a possibly applied discard.
	if err != nil {
		return Protocol2Response{}, err
	}
	response, err := decodeProtocol2Response(line)
	if err != nil {
		return Protocol2Response{}, err
	}
	if response.OK && (response.MediaData == nil || !response.MediaData.Valid()) {
		return Protocol2Response{}, errInvalidRuntimeResponse
	}
	return response, nil
}

// InspectMediaRecord reports what the kit has saved for one library disk.
func (r *Runtime) InspectMediaRecord(ctx context.Context, id protocol.MediaRecordIdentity) (protocol.MediaRecord, *protocol.APIError) {
	if !id.Valid() {
		return protocol.MediaRecord{}, mediaRecordRequestError()
	}
	control, ok := r.control.(protocol2MediaRecordControl)
	if !ok {
		return protocol.MediaRecord{}, unsupportedOperationError()
	}
	response, err := control.InspectMediaData(ctx, MediaDataRoot, id)
	r.noteDispatch("inspect_media_data", err == nil)
	return mediaRecordResult(response, err)
}

// DiscardMediaRecord removes one library disk's saved record, so its next
// start reads the immutable source again. The runtime refuses while a core
// is loaded.
func (r *Runtime) DiscardMediaRecord(ctx context.Context, d protocol.MediaRecordDiscard) (protocol.MediaRecord, *protocol.APIError) {
	if !d.Valid() {
		return protocol.MediaRecord{}, mediaRecordRequestError()
	}
	control, ok := r.control.(protocol2MediaRecordControl)
	if !ok {
		return protocol.MediaRecord{}, unsupportedOperationError()
	}
	response, err := control.DiscardMediaData(ctx, MediaDataRoot, d)
	r.noteDispatch("discard_media_data", err == nil)
	record, apiErr := mediaRecordResult(response, err)
	if apiErr == nil && record.State != protocol.MediaRecordAbsent {
		return protocol.MediaRecord{}, unavailableError()
	}
	return record, apiErr
}

func mediaRecordRequestError() *protocol.APIError {
	return &protocol.APIError{Code: protocol.CodeBadRequest, Message: "saved media request is invalid", Phase: "request"}
}

func mediaRecordResult(response Protocol2Response, err error) (protocol.MediaRecord, *protocol.APIError) {
	if err != nil {
		return protocol.MediaRecord{}, unavailableError()
	}
	if !response.OK {
		result := mapProtocol2Error(response.Error)
		result.Expected, result.Observed = "", ""
		return protocol.MediaRecord{}, result
	}
	if response.MediaData == nil {
		return protocol.MediaRecord{}, unavailableError()
	}
	return *response.MediaData, nil
}
