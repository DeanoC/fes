package targetclient

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"

	"github.com/DeanoC/FogCast/protocol"
)

func (c *Client) SetSessionDisplay(ctx context.Context, visible bool, b protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	if !b.Valid() || b.Stream || b.Role == protocol.FirmwareRole {
		return protocol.Status{}, protocol.SessionDisplayRequestError()
	}
	if c.kitLease == nil {
		return protocol.Status{}, ErrKitLeaseLost
	}
	body, _ := json.Marshal(protocol.SessionDisplayRequest{Visible: &visible})
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("/v1/session/display", nil).String(), bytes.NewReader(body))
	if err != nil {
		return protocol.Status{}, err
	}
	r.Header.Set("Authorization", "Bearer "+c.token)
	r.Header.Set("Content-Type", "application/json")
	b.SetHeaders(r.Header)
	if err = c.kitLease.Authorize(r, false); err != nil {
		return protocol.Status{}, err
	}
	response, err := c.httpClient.Do(r)
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
	if !validCorePackageStatus(status) || !b.MatchesSessionDisplay(status) {
		return protocol.Status{}, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "launcher display response does not match the active package generation", Phase: "display"}
	}
	return status, nil
}
