package targetclient

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/DeanoC/FogCast/protocol"
)

func (c *Client) LoadDevelopmentRBF(ctx context.Context, size int64, content io.Reader) (protocol.Status, error) {
	if size < 1 || size > protocol.MaxDevelopmentRBFBytes || content == nil {
		return protocol.Status{}, fmt.Errorf("development RBF input is invalid")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("/v1/development/rbf", nil).String(), readOnlyReader{Reader: content})
	if err != nil {
		return protocol.Status{}, fmt.Errorf("create request: %w", err)
	}
	request.ContentLength = size
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/octet-stream")

	if err := c.authorizeMutation(request); err != nil {
		return protocol.Status{}, err
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return protocol.Status{}, newTransferError(err)
	}
	defer response.Body.Close()

	var status protocol.Status
	if err := decodeResponse(response, &status); err != nil {
		return protocol.Status{}, err
	}
	if status.State != protocol.StateActive || !status.Development || status.GameID != nil ||
		status.System != nil || status.ExpectedCore != nil || status.LastError != nil || status.Recovery != "" {
		return protocol.Status{}, fmt.Errorf("development RBF response does not match requested load")
	}
	return status, nil
}

func (c *Client) RecoverIdle(ctx context.Context) (protocol.Status, error) {
	var status protocol.Status
	err := c.doJSON(ctx, http.MethodPost, "/v1/development/recover-idle", nil, &status)
	return status, err
}

func (c *Client) RebootDevelopment(ctx context.Context) (protocol.Status, error) {
	var status protocol.Status
	err := c.doJSON(ctx, http.MethodPost, "/v1/development/reboot", nil, &status)
	return status, err
}
