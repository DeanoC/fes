package targetclient

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/DeanoC/FogCast/protocol"
)

func (c *Client) ReplaceLiveMedia(ctx context.Context, size int64, body io.Reader, b protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	if !b.Valid() || b.Stream || b.Role == protocol.FirmwareRole {
		return protocol.Status{}, protocol.LiveMediaRequestError()
	}
	data, apiErr := protocol.ReadDevelopmentMedia(size, body)
	if apiErr != nil {
		return protocol.Status{}, protocol.LiveMediaRequestError()
	}
	if c.kitLease == nil {
		return protocol.Status{}, ErrKitLeaseLost
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("/v1/development/live-media", nil).String(), readOnlyReader{bytes.NewReader(data)})
	if err != nil {
		return protocol.Status{}, err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.ContentLength = int64(len(data))
	request.Header.Set("Content-Type", "application/octet-stream")
	b.SetHeaders(request.Header)
	if err = c.kitLease.Authorize(request, false); err != nil {
		return protocol.Status{}, err
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		return protocol.Status{}, newTransferError(err)
	}
	defer response.Body.Close()
	var status protocol.Status
	if err = decodeResponse(response, &status); err != nil {
		return protocol.Status{}, err
	}
	if !validCorePackageStatus(status) || !b.MatchesLive(status) {
		return protocol.Status{}, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "live media response does not match the active package generation", Phase: "recovery"}
	}
	return status, nil
}

func (c *Client) ClearLiveMedia(ctx context.Context, b protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	if !b.Valid() || b.Stream || b.Role == protocol.FirmwareRole {
		return protocol.Status{}, protocol.LiveMediaRequestError()
	}
	if c.kitLease == nil {
		return protocol.Status{}, ErrKitLeaseLost
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("/v1/development/clear-media", nil).String(), http.NoBody)
	if err != nil {
		return protocol.Status{}, err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	b.SetHeaders(request.Header)
	if err = c.kitLease.Authorize(request, false); err != nil {
		return protocol.Status{}, err
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		return protocol.Status{}, newTransferError(err)
	}
	defer response.Body.Close()
	var status protocol.Status
	if err = decodeResponse(response, &status); err != nil {
		return protocol.Status{}, err
	}
	if !validCorePackageStatus(status) || !b.MatchesLive(status) {
		return protocol.Status{}, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "live media clear response does not match the active package generation", Phase: "recovery"}
	}
	return status, nil
}
