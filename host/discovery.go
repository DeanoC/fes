package host

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"
)

// KitOwnership contains only the public lease metadata returned by the agent.
type KitOwnership struct {
	State      string `json:"state"`
	Generation string `json:"generation"`
	Owner      string `json:"owner,omitempty"`
	Purpose    string `json:"purpose,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Owned      bool   `json:"-"`
}

func (c *Client) setEndpoint(base *url.URL) {
	c.endpointMu.Lock()
	copy := *base
	c.baseURL = &copy
	c.endpointMu.Unlock()
}

func (l *KitLease) Endpoint() *url.URL {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.client.endpoint("", nil)
}

// AdoptEndpoint reconciles ownership using only GET. A reboot discards stale
// credentials without Stop, release, renewal, or any other cleanup request.
// Callers serialize this with application lifecycle operations after validating
// authenticated health at the new endpoint.
func (c *Client) AdoptEndpoint(ctx context.Context, base *url.URL, reboot bool) (KitOwnership, error) {
	if reboot {
		c.InvalidateKitSession()
	}
	probe := NewClient(base, c.token, c.httpClient)
	var ownership KitOwnership
	if err := probe.doJSON(ctx, http.MethodGet, "/v1/kit/lease", nil, &ownership); err != nil {
		return ownership, err
	}
	if ownership.State != "free" && ownership.State != "held" && ownership.State != "blocked" && ownership.State != "revoking" {
		return ownership, errors.New("invalid ownership state")
	}
	if ownership.State == "held" && ownership.Generation == "" {
		return ownership, errors.New("missing ownership generation")
	}
	if c.kitLease != nil {
		l := c.kitLease
		l.mu.Lock()
		owned := !reboot && !l.lost && !l.closed && l.grant.Token != "" && time.Now().Before(l.localExpiry) && ownership.State == "held" && ownership.Generation == l.grant.Status.Generation
		if !owned {
			l.invalidateLocked()
			l.requestID = ""
			// A reconciled endpoint permits a new explicit claim, never automatic takeover.
			l.lost = false
		}
		ownership.Owned = owned
		l.client.setEndpoint(base)
		l.mu.Unlock()
	}
	c.setEndpoint(base)
	return ownership, nil
}

func (c *Client) HasKitGrant() bool { return c.kitLease.currentToken() != "" }

// Peer uses the configured transport and bearer for read-only validation of a
// DNS-SD candidate, without sharing mutation authority.
func (c *Client) Peer(base *url.URL) *Client { return NewClient(base, c.token, c.httpClient) }
func (c *Client) EndpointURL() *url.URL      { return c.endpoint("", nil) }

// InvalidateKitSession forgets authority as soon as a different authenticated
// boot is observed, even if subsequent ownership/status reads fail.
func (c *Client) InvalidateKitSession() {
	if c.kitLease == nil {
		return
	}
	l := c.kitLease
	l.mu.Lock()
	defer l.mu.Unlock()
	l.invalidateLocked()
	l.requestID = ""
}
