package host

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

// RemoteInputSource binds one input producer to one bridge attachment. Its
// generation prevents a delayed close or event affecting a replacement source.
type RemoteInputEventSource interface {
	Check() error
	SendEvent(context.Context, remoteinput.Event, time.Time) error
	Close() error
}

type RemoteInputSource struct {
	owner      *RemoteInput
	session    uint64
	generation uint64
}

func (r *RemoteInput) sessionIDLocked() string {
	if r.session == 0 || (r.state != RemoteInputAttached && r.state != RemoteInputReconnecting) {
		return ""
	}
	return strconv.FormatUint(r.session, 16)
}

func (r *RemoteInput) ClaimSource(sessionID string) (RemoteInputEventSource, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, ErrRemoteInputClosed
	}
	if err := r.ensureLiveStreamLocked(context.Background()); err != nil {
		return nil, err
	}
	if sessionID == "" || sessionID != r.sessionIDLocked() {
		return nil, ErrRemoteInputInvalid
	}
	if r.source != "" {
		return nil, ErrRemoteInputBusy
	}
	r.sourceGeneration++
	r.source = "launcher"
	return &RemoteInputSource{owner: r, session: r.session, generation: r.sourceGeneration}, nil
}

// ensureLiveStreamLocked delivers only while the kit input stream is connected.
// A missing listener is an error. The event is not applied until this returns.
func (r *RemoteInput) ensureLiveStreamLocked(ctx context.Context) error {
	if r.connectionLiveLocked() {
		return nil
	}
	if r.bridge == nil || (r.state != RemoteInputAttached && r.state != RemoteInputReconnecting) {
		return ErrRemoteInputNoStream
	}
	r.closeConnLocked()
	if ctx == nil {
		ctx = context.Background()
	}
	dialCtx, cancel := context.WithTimeout(ctx, r.grace)
	err := r.ensureConnectionLocked(dialCtx)
	cancel()
	if err != nil {
		if refusal, lost := r.ownershipReconnectFailureLocked(err); lost {
			return refusal
		}
		r.state = RemoteInputReconnecting
		r.ready = false
		return streamUnavailable(err)
	}
	return nil
}

// connectionLiveLocked reports whether the kit stream is still connected.
// An idle socket stays live. EOF or reset does not.
func (r *RemoteInput) connectionLiveLocked() bool {
	if r.conn == nil {
		return false
	}
	if r.reader != nil && r.reader.Buffered() > 0 {
		return true
	}
	return peerStreamLive(r.conn)
}

func ownershipRefusal(err error) bool {
	var api *protocol.APIError
	return errors.As(err, &api) && (api.Code == protocol.CodeKitLeaseDenied || api.Code == protocol.CodeKitLeaseBusy)
}

// ownershipReconnectFailureLocked records lease loss and returns the sanitized
// refusal. Transient reconnect errors stay with the caller.
func (r *RemoteInput) ownershipReconnectFailureLocked(err error) (error, bool) {
	if !ownershipRefusal(err) {
		return err, false
	}
	r.failLocked("lease_lost")
	return streamUnavailable(err), true
}

func streamDialError(err error) error {
	if ownershipRefusal(err) {
		return remoteInputStartError(err)
	}
	if err == nil || errors.Is(err, ErrRemoteInputNoStream) || errors.Is(err, context.Canceled) {
		return err
	}
	return ErrRemoteInputNoStream
}

func streamUnavailable(err error) error {
	if ownershipRefusal(err) {
		return remoteInputStartError(err)
	}
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, ErrRemoteInputNoStream) {
		return err
	}
	return ErrRemoteInputNoStream
}

func (s *RemoteInputSource) validLocked() bool {
	r := s.owner
	return !r.closed && r.session == s.session && r.sourceGeneration == s.generation && r.source == "launcher" && (r.state == RemoteInputAttached || r.state == RemoteInputReconnecting)
}

func (s *RemoteInputSource) Check() error {
	s.owner.mu.Lock()
	defer s.owner.mu.Unlock()
	if !s.validLocked() {
		return ErrRemoteInputInvalid
	}
	return nil
}

func (s *RemoteInputSource) SendEvent(ctx context.Context, event remoteinput.Event, capturedAt time.Time) error {
	if ctx == nil {
		ctx = context.Background()
	}
	r := s.owner
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !s.validLocked() {
		return ErrRemoteInputInvalid
	}
	return r.confirmEventLocked(ctx, r.sendEventModeLocked(ctx, event, capturedAt, false))
}

// Close drops the authenticated bridge transport: the bridge releases every
// held control on disconnect. Clear the local snapshot before a later producer
// can reconnect, so no held state from this source is replayed.
func (s *RemoteInputSource) Close() error {
	r := s.owner
	r.mu.Lock()
	defer r.mu.Unlock()
	if !s.validLocked() {
		return nil
	}
	r.closeConnLocked()
	r.inputState.ReleaseAll()
	r.colecoOutput = nil
	r.source = ""
	r.sourceGeneration++
	r.state = RemoteInputReconnecting
	r.ready = false
	r.metrics.releases++
	return nil
}
