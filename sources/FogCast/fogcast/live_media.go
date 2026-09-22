package fogcast

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

type liveMediaClient interface {
	ReplaceLiveMedia(context.Context, int64, io.Reader, protocol.DevelopmentMediaBinding) (protocol.Status, error)
	ClearLiveMedia(context.Context, protocol.DevelopmentMediaBinding) (protocol.Status, error)
}

// ReplaceLiveMedia arms a household core-media id into the active generation
// via the mid-session live path. It does not inject LOAD "" keys.
func (s *Service) ReplaceLiveMedia(parent context.Context, mediaID, name string, b protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	if !b.Valid() || b.Target == "" || protocol.ValidateDigest(mediaID) != nil || !protocol.AdmitTapeMediaName(name) {
		return protocol.Status{}, protocol.LiveMediaRequestError()
	}
	ctx, cancel := serviceTimeout(parent, s.uploadTimeout)
	defer cancel()
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return protocol.Status{}, err
	}
	defer release()
	media, err := s.readLiveTapeMedia(ctx, mediaID)
	if err != nil {
		return protocol.Status{}, err
	}
	defer media.Close()
	return s.replaceLiveMediaLocked(ctx, media.size, media.ReadCloser, b)
}

func (s *Service) ClearLiveMedia(parent context.Context, b protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	if !b.Valid() || b.Target == "" {
		return protocol.Status{}, protocol.LiveMediaRequestError()
	}
	ctx, cancel := serviceTimeout(parent, s.uploadTimeout)
	defer cancel()
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return protocol.Status{}, err
	}
	defer release()
	return s.clearLiveMediaLocked(ctx, b)
}

func (s *Service) readLiveTapeMedia(ctx context.Context, mediaID string) (*coreEntryMedia, error) {
	store, ok := s.catalog.(coreMediaCatalog)
	if !ok {
		return nil, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	info, err := store.CoreMediaInfo(ctx, mediaID)
	if err != nil {
		return nil, mapCoreMediaError(err)
	}
	if !protocol.AdmitTapeMediaSize(info.Size) {
		return nil, protocol.LiveMediaRequestError()
	}
	opened, reader, err := store.OpenCoreMedia(ctx, mediaID)
	if err != nil {
		return nil, mapCoreMediaError(err)
	}
	if opened != info {
		_ = reader.Close()
		return nil, canonicalError(protocol.CodeInternal, nil)
	}
	return &coreEntryMedia{ReadCloser: reader, size: info.Size}, nil
}

func (s *Service) replaceLiveMediaLocked(ctx context.Context, size int64, body io.Reader, b protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	if err := s.prepareLiveMediaClient(ctx, b); err != nil {
		return protocol.Status{}, err
	}
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	client, ok := s.selectedClientLocked()
	if !ok {
		return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	loader, ok := client.(liveMediaClient)
	if !ok {
		return protocol.Status{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	prior, err := client.Status(ctx)
	if err != nil {
		return protocol.Status{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
	}
	if !b.MatchesLive(prior) {
		return prior, protocol.LiveMediaIdentityError()
	}
	if !protocol.AdmitTapeMediaSize(size) {
		return prior, protocol.LiveMediaRequestError()
	}
	status, err := loader.ReplaceLiveMedia(ctx, size, body, b)
	if err != nil {
		return status, preserveCorePackageError(err)
	}
	if !b.MatchesLive(status) {
		return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	return s.retainLiveSessionIdentity(status, b), nil
}

func (s *Service) clearLiveMediaLocked(ctx context.Context, b protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	if err := s.prepareLiveMediaClient(ctx, b); err != nil {
		return protocol.Status{}, err
	}
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	client, ok := s.selectedClientLocked()
	if !ok {
		return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	loader, ok := client.(liveMediaClient)
	if !ok {
		return protocol.Status{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	prior, err := client.Status(ctx)
	if err != nil {
		return protocol.Status{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
	}
	if !b.MatchesLive(prior) {
		return prior, protocol.LiveMediaIdentityError()
	}
	status, err := clearLiveMediaRetry(ctx, loader, b)
	if err != nil {
		return status, preserveCorePackageError(err)
	}
	if !b.MatchesLive(status) {
		return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	return s.retainLiveSessionIdentity(status, b), nil
}

func clearLiveMediaRetry(ctx context.Context, loader liveMediaClient, b protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	var status protocol.Status
	var err error
	waits := []time.Duration{0, 20 * time.Millisecond, 40 * time.Millisecond, 80 * time.Millisecond}
	for attempt, wait := range waits {
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				if err != nil {
					return status, err
				}
				return status, ctx.Err()
			case <-timer.C:
			}
		}
		status, err = loader.ClearLiveMedia(ctx, b)
		err = normalizeEjectError(err)
		if err == nil || !ejectRetryable(err) || attempt == len(waits)-1 {
			return status, err
		}
	}
	return status, err
}

func normalizeEjectError(err error) error {
	var apiErr *protocol.APIError
	if errors.As(err, &apiErr) && apiErr.Code == protocol.CodeMiSTerUnavailable && apiErr.Phase == "input" {
		return protocol.LiveMediaBusyError()
	}
	return err
}

func ejectRetryable(err error) bool {
	var apiErr *protocol.APIError
	return errors.As(err, &apiErr) && apiErr.Code == protocol.CodeBusy && apiErr.Phase == "input"
}

func (s *Service) prepareLiveMediaClient(ctx context.Context, b protocol.DevelopmentMediaBinding) error {
	s.executionMu.Lock()
	blocked := s.activeExecution == ExecutionHostOnly || s.packageRejection != nil
	s.executionMu.Unlock()
	if blocked {
		return protocol.LiveMediaIdentityError()
	}
	if s.protocolAdmissionEnabled() {
		if _, err := s.refreshTargetAdmission(ctx); err != nil {
			return canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
		}
	}
	if err := s.incompatibleTargetError(); err != nil {
		return err
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
		return protocol.LiveMediaIdentityError()
	}
	return nil
}

func (s *Service) retainLiveSessionIdentity(status protocol.Status, b protocol.DevelopmentMediaBinding) protocol.Status {
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	if s.activePackageID == b.PackageID && s.activePackageGeneration == b.Generation && s.activeGameID != "" {
		status.GameID = stringPtr(s.activeGameID)
		status.System = systemPtr(s.activeSystem)
	}
	return status
}
