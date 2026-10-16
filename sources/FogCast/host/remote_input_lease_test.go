package host

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
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
			if !errors.As(err, &api) || api.Code != code || api.Message != "session does not own the kit lease" || bridge.dials != 1 {
				t.Fatalf("error=%v dials=%d", err, bridge.dials)
			}
		})
	}
}

func TestSendEventPreservesLeaseRefusal(t *testing.T) {
	for _, code := range []protocol.ErrorCode{protocol.CodeKitLeaseDenied, protocol.CodeKitLeaseBusy} {
		t.Run(string(code), func(t *testing.T) {
			bridge := &refusedLeaseBridge{code: code}
			client, peer := net.Pipe()
			peer.Close()
			defer client.Close()
			input := &RemoteInput{
				bridge:  bridge,
				conn:    client,
				state:   RemoteInputAttached,
				ready:   true,
				session: 1,
				core:    "Pong",
				now:     time.Now,
				grace:   time.Hour,
				dial:    time.Second,
			}
			event, err := remoteinput.NormalizeGamepad("a", true)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			err = input.sendEventLocked(ctx, event, time.Now())
			var api *protocol.APIError
			status := input.statusLocked()
			if !errors.As(err, &api) || api.Code != code || api.Message != "session does not own the kit lease" || errors.Is(err, ErrRemoteInputNoStream) || bridge.dials != 1 {
				t.Fatalf("error=%v dials=%d", err, bridge.dials)
			}
			if status.State != RemoteInputFailed || status.Metrics.ShutdownReason != "lease_lost" {
				t.Fatalf("status=%#v", status)
			}
		})
	}
}

func TestEnsureLiveStreamPreservesLeaseRefusal(t *testing.T) {
	bridge := &refusedLeaseBridge{code: protocol.CodeKitLeaseDenied}
	input := &RemoteInput{
		bridge: bridge,
		state:  RemoteInputAttached,
		now:    time.Now,
		grace:  time.Hour,
		dial:   time.Second,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := input.ensureLiveStreamLocked(ctx)
	var api *protocol.APIError
	if !errors.As(err, &api) || api.Code != protocol.CodeKitLeaseDenied || api.Message != "session does not own the kit lease" || errors.Is(err, ErrRemoteInputNoStream) || bridge.dials != 1 {
		t.Fatalf("error=%v dials=%d", err, bridge.dials)
	}
	status := input.statusLocked()
	if status.State != RemoteInputFailed || status.Metrics.ShutdownReason != "lease_lost" {
		t.Fatalf("status=%#v", status)
	}
}
