package hostclient

import (
	"context"
	"net/http"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

func (c *Client) stopHTTPForSession(session SessionResult) *http.Client {
	if !protocol.MediaDataBound(diskPackage(session.CorePackage)) {
		return c.stopHTTP
	}
	client := *c.stopHTTP
	if client.Timeout == 0 || client.Timeout < 150*time.Second {
		client.Timeout = 150 * time.Second
	}
	return &client
}

func (c *Client) observedStopHTTP(ctx context.Context) *http.Client {
	// Explicit Stop has no captured play. A read selects its HTTP budget without
	// changing the existing single Stop mutation or imposing a new identity.
	session, err := c.Session(ctx)
	if err != nil {
		return c.stopHTTP
	}
	return c.stopHTTPForSession(session)
}
