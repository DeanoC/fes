package host

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/DeanoC/FogCast/protocol"
)

func (c *Client) LoadDevelopmentMedia(ctx context.Context, size int64, body io.Reader, b protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	if !b.Valid() {
		return protocol.Status{}, protocol.DevelopmentMediaRequestError()
	}
	data, apiErr := protocol.ReadDevelopmentMedia(size, body)
	if apiErr != nil {
		return protocol.Status{}, apiErr
	}
	if c.kitLease == nil {
		return protocol.Status{}, ErrKitLeaseLost
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("/v1/development/media", nil).String(), bytes.NewReader(data))
	if err != nil {
		return protocol.Status{}, err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
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
	if !validCorePackageStatus(status) || !b.Matches(status) {
		return protocol.Status{}, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "media response does not match the active package generation", Phase: "recovery"}
	}
	return status, nil
}
