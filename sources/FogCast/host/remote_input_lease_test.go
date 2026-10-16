package host

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

type refusedLeaseBridge struct {
	code  protocol.ErrorCode
	dials int
}

func (b *refusedLeaseBridge) Ready() <-chan struct{}     { return nil }
func (b *refusedLeaseBridge) Endpoint() string           { return "unused" }
func (b *refusedLeaseBridge) Stop(context.Context) error { return nil }
func (b *refusedLeaseBridge) Dial(context.Context) (net.Conn, error) {
	b.dials++
	return nil, &protocol.APIError{Code: b.code, Message: "ownership refused"}
}

func TestInputReconnectStopsOnLeaseRefusal(t *testing.T) {
	for _, code := range []protocol.ErrorCode{protocol.CodeKitLeaseDenied, protocol.CodeKitLeaseBusy} {
		t.Run(string(code), func(t *testing.T) {
			bridge := &refusedLeaseBridge{code: code}
			input := &RemoteInput{bridge: bridge, now: time.Now, grace: time.Hour}
			// A refused lease must return before this context bounds the otherwise
			// long retry window. Count attempts rather than asserting wall timing.
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			err := input.reconnectLocked(ctx)
			var api *protocol.APIError
			if !errors.As(err, &api) || api.Code != code || bridge.dials != 1 {
				t.Fatalf("error=%v dials=%d", err, bridge.dials)
			}
		})
	}
}
