//go:build !linux

package localcores

import (
	"context"
	"net"
	"net/http"
)

func (*Client) Identity(context.Context) (AgentIdentity, error) {
	return AgentIdentity{}, ErrUnavailable
}

func identityConnContext(ctx context.Context, _ net.Conn) context.Context { return ctx }
func identityRequestAllowed(*http.Request) bool                           { return false }
