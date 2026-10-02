package fogcast

import (
	"context"

	"github.com/DeanoC/FogCast/protocol"
)

type sessionDisplayClient interface {
	SetSessionDisplay(context.Context, bool, protocol.DevelopmentMediaBinding) (protocol.Status, error)
}

func (s *Service) SetSessionDisplay(parent context.Context, visible bool, b protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	if !b.Valid() || b.Target == "" {
		return protocol.Status{}, protocol.SessionDisplayRequestError()
	}
	ctx, cancel := serviceTimeout(parent, s.uploadTimeout)
	defer cancel()
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return protocol.Status{}, err
	}
	defer release()
	ctx = WithSessionTarget(ctx, b.Target)
	if err = s.prepareLiveMediaClient(ctx, b); err != nil {
		return protocol.Status{}, err
	}
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	client, ok := s.clientForSessionContextLocked(ctx)
	if !ok {
		return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	display, ok := client.(sessionDisplayClient)
	if !ok {
		return protocol.Status{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	prior, err := client.Status(ctx)
	if err != nil {
		return protocol.Status{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
	}
	if !b.MatchesSessionDisplay(prior) {
		return prior, protocol.SessionDisplayIdentityError()
	}
	status, err := display.SetSessionDisplay(ctx, visible, b)
	if err != nil {
		return status, preserveCorePackageError(err)
	}
	if !b.MatchesSessionDisplay(status) {
		return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	return s.retainLiveSessionIdentity(status, b), nil
}
