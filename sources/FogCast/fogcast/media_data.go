package fogcast

import (
	"context"
	"errors"
	"github.com/DeanoC/FogCast/protocol"
	"io"
	"time"
)

// mediaUnchanged marks a library-insert failure that returned before the
// target was asked to take the disk. The host keeps the confirmed filename.
func mediaUnchanged(err error) error {
	var api *protocol.APIError
	if err == nil || !errors.As(err, &api) || api == nil || api.NoChange {
		return err
	}
	copy := *api
	copy.NoChange = true
	return &copy
}

type mediaDataClient interface {
	InsertLibraryMedia(context.Context, int64, io.Reader, protocol.LibraryMediaBinding) (protocol.Status, error)
	SaveMedia(context.Context, protocol.MediaUnitBinding) (protocol.Status, error)
}

// InsertLibraryDisk restores the runtime's durable disk for this explicit
// library identity; it never changes the catalog's imported base blob.
func (s *Service) InsertLibraryDisk(parent context.Context, b protocol.LibraryMediaBinding) (protocol.Status, error) {
	if !b.Valid() || b.Target == "" {
		return protocol.Status{}, mediaUnchanged(protocol.MediaUnitRequestError())
	}
	ctx, cancel := serviceTimeout(parent, max(s.uploadTimeout, 450*time.Second))
	defer cancel()
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return protocol.Status{}, mediaUnchanged(err)
	}
	defer release()
	entry, err := s.CoreEntry(ctx, b.GameID)
	if err != nil {
		return protocol.Status{}, mediaUnchanged(err)
	}
	if entry.PackageID != b.PackageID || entry.MediaID != b.BaseMediaID || entry.MediaRole != "disk" {
		return protocol.Status{}, mediaUnchanged(protocol.MediaUnitIdentityError())
	}
	store, ok := s.catalog.(coreMediaCatalog)
	if !ok {
		return protocol.Status{}, mediaUnchanged(canonicalError(protocol.CodeUnsupportedOperation, nil))
	}
	info, reader, err := store.OpenCoreMedia(ctx, b.BaseMediaID)
	if err != nil {
		return protocol.Status{}, mediaUnchanged(mapCoreMediaError(err))
	}
	defer reader.Close()
	if !protocol.AdmitAtariStFloppySize(info.Size) {
		return protocol.Status{}, mediaUnchanged(protocol.DiskMediaRequestError())
	}
	status, err := s.insertBoundMediaUnitLocked(ctx, info.Size, reader, b.MediaUnitBinding, &b)
	if err != nil {
		return status, err
	}
	return s.retainMediaUnitSessionIdentity(status, b.MediaUnitBinding), nil
}
func (s *Service) SaveMedia(parent context.Context, b protocol.MediaUnitBinding) (protocol.Status, error) {
	if !b.Valid() || b.Target == "" {
		return protocol.Status{}, protocol.MediaUnitRequestError()
	}
	ctx, cancel := serviceTimeout(parent, 150*time.Second)
	defer cancel()
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return protocol.Status{}, err
	}
	defer release()
	if err = s.prepareMediaUnitClient(ctx, b); err != nil {
		return protocol.Status{}, err
	}
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	client, _, err := s.mediaUnitTargetLocked(b)
	if err != nil {
		return protocol.Status{}, err
	}
	prior, err := client.Status(ctx)
	if err != nil {
		return prior, preserveCorePackageError(err)
	}
	if !b.MatchesForSave(prior) {
		return prior, protocol.MediaUnitIdentityError()
	}
	unit, ok := protocol.MediaUnit(prior.CorePackage, b.Unit)
	if !ok || unit.Persistence == nil {
		return prior, protocol.MediaUnitRequestError()
	}
	saver, ok := client.(mediaDataClient)
	if !ok {
		return prior, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	status, err := saver.SaveMedia(ctx, b)
	if err != nil {
		return status, preserveCorePackageError(err)
	}
	if !b.MatchesSaveResult(prior, status) {
		return status, protocol.MediaUnitIdentityError()
	}
	return s.retainMediaUnitSessionIdentity(status, b), nil
}
