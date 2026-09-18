package fogcast

import (
	"bytes"
	"context"
	"io"

	"github.com/DeanoC/FogCast/protocol"
)

type developmentMediaClient interface {
	LoadDevelopmentMedia(context.Context, int64, io.Reader, protocol.DevelopmentMediaBinding) (protocol.Status, error)
}

func (s *Service) LoadDevelopmentMedia(parent context.Context, size int64, body io.Reader, b protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	if !b.Valid() || b.Target == "" {
		return protocol.Status{}, protocol.DevelopmentMediaRequestError()
	}
	data, apiErr := protocol.ReadDevelopmentMedia(size, body)
	if apiErr != nil {
		return protocol.Status{}, apiErr
	}
	ctx, cancel := serviceTimeout(parent, s.uploadTimeout)
	defer cancel()
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return protocol.Status{}, err
	}
	defer release()
	return s.loadDevelopmentMediaLocked(ctx, data, b)
}

// Caller holds lifecycle admission and supplies an already bounded snapshot.
func (s *Service) loadDevelopmentMediaLocked(ctx context.Context, data []byte, b protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	return s.loadDevelopmentMediaReaderLocked(ctx, int64(len(data)), bytes.NewReader(data), b)
}

func (s *Service) loadDevelopmentMediaReaderLocked(ctx context.Context, size int64, body io.Reader, b protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	s.executionMu.Lock()
	blocked := s.activeExecution == ExecutionHostOnly || s.packageRejection != nil
	s.executionMu.Unlock()
	if blocked {
		return protocol.Status{}, protocol.DevelopmentMediaIdentityError()
	}
	if s.protocolAdmissionEnabled() {
		if _, err := s.refreshTargetAdmission(ctx); err != nil {
			return protocol.Status{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
		}
	}
	if err := s.incompatibleTargetError(); err != nil {
		return protocol.Status{}, err
	}
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	s.executionMu.Lock()
	target := s.activeTarget
	s.executionMu.Unlock()
	if target == "" {
		target = s.selectedTarget
	}
	if target != b.Target || targetByName(s.targets, target).TargetID != b.TargetID {
		return protocol.Status{}, protocol.DevelopmentMediaIdentityError()
	}
	client, ok := s.selectedClientLocked()
	if !ok {
		return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	loader, ok := client.(developmentMediaClient)
	if !ok {
		return protocol.Status{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	prior, err := client.Status(ctx)
	if err != nil {
		return protocol.Status{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
	}
	if !b.Matches(prior) {
		return prior, protocol.DevelopmentMediaIdentityError()
	}
	if !b.AcceptsSize(prior, size) {
		return prior, protocol.DevelopmentMediaRequestError()
	}
	status, err := loader.LoadDevelopmentMedia(ctx, size, body, b)
	if err != nil {
		// A status read cannot prove media delivery; never replay.
		return status, preserveCorePackageError(err)
	}
	if !b.Matches(status) {
		return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	s.executionMu.Lock()
	if s.activePackageID == b.PackageID && s.activePackageGeneration == b.Generation && s.activeGameID != "" {
		status.GameID = stringPtr(s.activeGameID)
		status.System = systemPtr(s.activeSystem)
	}
	s.executionMu.Unlock()
	return status, nil
}
