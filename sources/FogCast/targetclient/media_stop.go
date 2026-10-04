package targetclient

import (
	"context"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

// StopWithMediaSave allows one bound disk capture before physical teardown.
// It preserves authentication, lease ownership and the caller's cancellation.
func (c *Client) StopWithMediaSave(ctx context.Context) (protocol.Status, error) {
	client := *c.httpClient
	if client.Timeout == 0 || client.Timeout < 150*time.Second {
		client.Timeout = 150 * time.Second
	}
	var status protocol.Status
	err := c.doJSONQueryWithClient(ctx, "POST", "/v1/stop", nil, nil, &status, &client)
	return status, err
}
