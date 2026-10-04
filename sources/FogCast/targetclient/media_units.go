package targetclient

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

// InsertMedia streams one exact image to the target's fes.computer media
// unit route under the existing kit lease. The target stages the bytes and
// makes one runtime insert_media call; a lost reply is not replayed.
func (c *Client) InsertMedia(ctx context.Context, size int64, body io.Reader, b protocol.MediaUnitBinding) (protocol.Status, error) {
	return c.insertMedia(ctx, size, body, b, 0)
}

// InsertMediaWithSave encloses outgoing capture, insertion and bounded recovery.
func (c *Client) InsertMediaWithSave(ctx context.Context, size int64, body io.Reader, b protocol.MediaUnitBinding) (protocol.Status, error) {
	return c.insertMedia(ctx, size, body, b, 450*time.Second)
}
func (c *Client) insertMedia(ctx context.Context, size int64, body io.Reader, b protocol.MediaUnitBinding, budget time.Duration) (protocol.Status, error) {
	if !b.Valid() || size < 1 || size > protocol.MaxComputerMediaBytes || body == nil {
		return protocol.Status{}, protocol.MediaUnitRequestError()
	}
	status, err := c.mediaUnitRequestBudget(ctx, "/v1/development/insert-media", size, readOnlyReader{body}, b, nil, budget)
	if err != nil {
		return protocol.Status{}, err
	}
	if unit, ok := protocol.MediaUnit(status.CorePackage, b.Unit); !ok || unit.State != protocol.MediaUnitReady {
		return protocol.Status{}, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "media unit response does not report the inserted image ready", Phase: "recovery"}
	}
	return status, nil
}

// EjectMedia empties one unit of the active fes.computer generation.
func (c *Client) EjectMedia(ctx context.Context, b protocol.MediaUnitBinding) (protocol.Status, error) {
	return c.ejectMedia(ctx, b, 0)
}
func (c *Client) EjectMediaWithSave(ctx context.Context, b protocol.MediaUnitBinding) (protocol.Status, error) {
	return c.ejectMedia(ctx, b, 150*time.Second)
}
func (c *Client) ejectMedia(ctx context.Context, b protocol.MediaUnitBinding, budget time.Duration) (protocol.Status, error) {
	if !b.Valid() {
		return protocol.Status{}, protocol.MediaUnitRequestError()
	}
	status, err := c.mediaUnitRequestBudget(ctx, "/v1/development/eject-media", 0, http.NoBody, b, nil, budget)
	if err != nil {
		return protocol.Status{}, err
	}
	if unit, ok := protocol.MediaUnit(status.CorePackage, b.Unit); !ok || unit.State != protocol.MediaUnitEmpty {
		return protocol.Status{}, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "media unit response does not report the unit empty", Phase: "recovery"}
	}
	return status, nil
}

func (c *Client) mediaUnitRequest(ctx context.Context, path string, size int64, body io.Reader, b protocol.MediaUnitBinding) (protocol.Status, error) {
	return c.mediaUnitBoundRequest(ctx, path, size, body, b, nil)
}
func (c *Client) mediaUnitBoundRequest(ctx context.Context, path string, size int64, body io.Reader, b protocol.MediaUnitBinding, library *protocol.LibraryMediaBinding) (protocol.Status, error) {
	return c.mediaUnitRequestBudget(ctx, path, size, body, b, library, 0)
}
func (c *Client) mediaUnitRequestBudget(ctx context.Context, path string, size int64, body io.Reader, b protocol.MediaUnitBinding, library *protocol.LibraryMediaBinding, budget time.Duration) (protocol.Status, error) {
	if c.kitLease == nil {
		return protocol.Status{}, ErrKitLeaseLost
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(path, nil).String(), body)
	if err != nil {
		return protocol.Status{}, err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	if size > 0 {
		request.ContentLength = size
		request.Header.Set("Content-Type", "application/octet-stream")
	}
	b.SetHeaders(request.Header)
	if library != nil {
		library.SetHeaders(request.Header)
	}
	if err = c.kitLease.Authorize(request, false); err != nil {
		return protocol.Status{}, err
	}
	client := *c.httpClient
	if budget > 0 && (client.Timeout == 0 || client.Timeout < budget) {
		client.Timeout = budget
	}
	response, err := client.Do(request)
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
		return protocol.Status{}, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "media unit response does not match the active package generation", Phase: "recovery"}
	}
	return status, nil
}
