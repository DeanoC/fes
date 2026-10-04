package hostclient

import (
	"net/http"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

// This is a transport deadline hint, never session or mutation authorization.
// Request ordering prevents a late earlier poll from replacing a Stop reply.
type stopSessionObservation struct {
	mu             sync.Mutex
	next, accepted uint64
	lastMutation   uint64
	bound          bool
}

func (c *Client) beginSessionObservation() uint64 {
	if c.stopSession == nil {
		return 0
	}
	c.stopSession.mu.Lock()
	defer c.stopSession.mu.Unlock()
	c.stopSession.next++
	return c.stopSession.next
}
func (c *Client) recordSessionObservation(order uint64, session SessionResult) {
	c.recordSessionResponse(order, session, false)
}
func (c *Client) recordSessionMutation(order uint64, session SessionResult) {
	c.recordSessionResponse(order, session, true)
}
func (c *Client) recordSessionResponse(order uint64, session SessionResult, mutation bool) {
	if c.stopSession == nil || session.HTTPStatus != http.StatusOK || session.State == "" {
		return
	}
	c.stopSession.mu.Lock()
	defer c.stopSession.mu.Unlock()
	if mutation {
		if order < c.stopSession.lastMutation {
			return
		}
		c.stopSession.lastMutation = order
		// Fence every poll already started, including polls during this mutation.
		c.stopSession.accepted = c.stopSession.next
	} else {
		if order <= c.stopSession.accepted {
			return
		}
		c.stopSession.accepted = order
	}
	c.stopSession.bound = protocol.MediaDataBound(diskPackage(session.CorePackage))
}
func (c *Client) knownStopHTTP() *http.Client {
	if c.stopSession == nil {
		return c.stopHTTP
	}
	c.stopSession.mu.Lock()
	bound := c.stopSession.bound
	c.stopSession.mu.Unlock()
	return c.stopHTTPForBound(bound)
}
func (c *Client) stopHTTPForSession(session SessionResult) *http.Client {
	return c.stopHTTPForBound(protocol.MediaDataBound(diskPackage(session.CorePackage)))
}
func (c *Client) stopHTTPForBound(bound bool) *http.Client {
	if !bound {
		return c.stopHTTP
	}
	client := *c.stopHTTP
	if client.Timeout == 0 || client.Timeout < 150*time.Second {
		client.Timeout = 150 * time.Second
	}
	return &client
}
