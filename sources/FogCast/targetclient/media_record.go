package targetclient

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"

	"github.com/DeanoC/FogCast/protocol"
)

var errMediaRecordRequest = &protocol.APIError{Code: protocol.CodeBadRequest, Message: "saved media request is invalid", Phase: "request"}

// InspectMediaRecord reports what the kit has saved for one library disk.
// It changes nothing, so it needs no kit lease.
func (c *Client) InspectMediaRecord(ctx context.Context, id protocol.MediaRecordIdentity) (protocol.MediaRecord, error) {
	if !id.Valid() {
		return protocol.MediaRecord{}, errMediaRecordRequest
	}
	return c.mediaRecord(ctx, "/v1/library/media/record", id)
}

// DiscardMediaRecord removes one library disk's saved record. The record
// must still have d.ExpectedRevision. Like a settings change it takes the
// kit lease for the request when this client does not already hold it, and
// gives it back once the kit has answered.
func (c *Client) DiscardMediaRecord(ctx context.Context, d protocol.MediaRecordDiscard) (protocol.MediaRecord, error) {
	if !d.Valid() {
		return protocol.MediaRecord{}, errMediaRecordRequest
	}
	hadLease := c.kitLease.CurrentToken() != ""
	record, err := c.mediaRecord(ctx, "/v1/library/media/record/discard", d)
	if err == nil && record.State != protocol.MediaRecordAbsent {
		err = &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "saved media was not discarded", Phase: "recovery"}
	}
	if err = c.finishSettingsLease(ctx, !hadLease, err); err != nil {
		return protocol.MediaRecord{}, err
	}
	return record, nil
}

func (c *Client) mediaRecord(ctx context.Context, path string, payload any) (protocol.MediaRecord, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return protocol.MediaRecord{}, err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(path, nil).String(), bytes.NewReader(body))
	if err != nil {
		return protocol.MediaRecord{}, err
	}
	r.Header.Set("Authorization", "Bearer "+c.token)
	r.Header.Set("Content-Type", "application/json")
	if err = c.authorizeMutation(r); err != nil {
		return protocol.MediaRecord{}, err
	}
	response, err := c.httpClient.Do(r)
	if err != nil {
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		return protocol.MediaRecord{}, newTransferError(err)
	}
	defer response.Body.Close()
	var record protocol.MediaRecord
	if err = decodeResponse(response, &record); err != nil {
		return protocol.MediaRecord{}, err
	}
	if !record.Valid() {
		return protocol.MediaRecord{}, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "saved media response is invalid", Phase: "recovery"}
	}
	return record, nil
}
