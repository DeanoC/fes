package fogcast

import (
	"context"
	"io"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

// mediaUnitClient carries fes.computer removable media to the bound target.
// The target stages the bytes and makes one runtime insert_media or
// eject_media call; the machine keeps running throughout.
type savedMediaUnitClient interface {
	InsertMediaWithSave(context.Context, int64, io.Reader, protocol.MediaUnitBinding) (protocol.Status, error)
	EjectMediaWithSave(context.Context, protocol.MediaUnitBinding) (protocol.Status, error)
}

type mediaUnitClient interface {
	InsertMedia(context.Context, int64, io.Reader, protocol.MediaUnitBinding) (protocol.Status, error)
	EjectMedia(context.Context, protocol.MediaUnitBinding) (protocol.Status, error)
}

// prepareMediaUnitClient repeats the development-media admission: host-only
// play and a pending rejection block delivery, and the binding must name the
// session's target. Caller holds lifecycle admission.
func (s *Service) prepareMediaUnitClient(ctx context.Context, b protocol.MediaUnitBinding) error {
	s.executionMu.Lock()
	blocked := s.activeExecution == ExecutionHostOnly || s.packageRejection != nil
	s.executionMu.Unlock()
	if blocked {
		return protocol.MediaUnitIdentityError()
	}
	ctx = WithSessionTarget(ctx, b.Target)
	if s.protocolAdmissionEnabled() {
		if _, err := s.refreshTargetAdmission(ctx); err != nil {
			return canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
		}
	}
	return s.incompatibleSessionTargetError(ctx)
}

func (s *Service) mediaUnitTargetLocked(b protocol.MediaUnitBinding) (serviceClient, mediaUnitClient, error) {
	target := b.Target
	if target != b.Target || targetByName(s.targets, target).TargetID != b.TargetID {
		return nil, nil, protocol.MediaUnitIdentityError()
	}
	client := s.targetClients[target]
	ok := client != nil
	if !ok {
		return nil, nil, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	loader, ok := client.(mediaUnitClient)
	if !ok {
		return nil, nil, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	return client, loader, nil
}

// insertMediaUnitLocked delivers one exact image into a declared unit of the
// active fes.computer generation. Size limits come from the unit the runtime
// reports. A lost reply is never replayed. Caller holds lifecycle admission.
func (s *Service) insertMediaUnitLocked(ctx context.Context, size int64, body io.Reader, b protocol.MediaUnitBinding) (protocol.Status, error) {
	return s.insertBoundMediaUnitLocked(ctx, size, body, b, nil)
}
func (s *Service) insertBoundMediaUnitLocked(ctx context.Context, size int64, body io.Reader, b protocol.MediaUnitBinding, library *protocol.LibraryMediaBinding, owners ...context.Context) (protocol.Status, error) {
	if !b.Valid() || b.Target == "" || body == nil {
		return protocol.Status{}, protocol.MediaUnitRequestError()
	}
	ctx = WithSessionTarget(ctx, b.Target)
	if err := s.prepareMediaUnitClient(ctx, b); err != nil {
		return protocol.Status{}, err
	}
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	client, loader, err := s.mediaUnitTargetLocked(b)
	if err != nil {
		return protocol.Status{}, err
	}
	prior, err := client.Status(ctx)
	if err != nil {
		return protocol.Status{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
	}
	if !b.Matches(prior) {
		return prior, protocol.MediaUnitIdentityError()
	}
	if !b.AcceptsSize(prior, size) {
		return prior, protocol.MediaUnitRequestError()
	}
	if protocol.MediaDataBound(prior.CorePackage) {
		owner := ctx
		if len(owners) > 0 {
			owner = owners[0]
		}
		operation, cancel := serviceTimeout(owner, 450*time.Second)
		defer cancel()
		ctx = operation
	}
	var status protocol.Status
	if library != nil {
		durable, ok := client.(mediaDataClient)
		if !ok {
			return prior, canonicalError(protocol.CodeUnsupportedOperation, nil)
		}
		status, err = durable.InsertLibraryMedia(ctx, size, body, *library)
	} else {
		if bound, ok := client.(savedMediaUnitClient); ok && protocol.MediaDataBound(prior.CorePackage) {
			status, err = bound.InsertMediaWithSave(ctx, size, body, b)
		} else {
			status, err = loader.InsertMedia(ctx, size, body, b)
		}
	}
	if err != nil {
		return status, preserveCorePackageError(err)
	}
	if unit, ok := protocol.MediaUnit(status.CorePackage, b.Unit); !b.Matches(status) || !ok || unit.State != protocol.MediaUnitReady {
		return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	return status, nil
}

// ejectMediaUnitLocked empties one unit of the active generation. Caller
// holds lifecycle admission.
func (s *Service) ejectMediaUnitLocked(ctx context.Context, b protocol.MediaUnitBinding) (protocol.Status, error) {
	if !b.Valid() || b.Target == "" {
		return protocol.Status{}, protocol.MediaUnitRequestError()
	}
	ctx = WithSessionTarget(ctx, b.Target)
	if err := s.prepareMediaUnitClient(ctx, b); err != nil {
		return protocol.Status{}, err
	}
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	client, loader, err := s.mediaUnitTargetLocked(b)
	if err != nil {
		return protocol.Status{}, err
	}
	prior, err := client.Status(ctx)
	if err != nil {
		return protocol.Status{}, canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
	}
	if !b.Matches(prior) {
		return prior, protocol.MediaUnitIdentityError()
	}
	var status protocol.Status
	if bound, ok := client.(savedMediaUnitClient); ok && protocol.MediaDataBound(prior.CorePackage) {
		status, err = bound.EjectMediaWithSave(ctx, b)
	} else {
		status, err = loader.EjectMedia(ctx, b)
	}
	if err != nil {
		return status, preserveCorePackageError(err)
	}
	if unit, ok := protocol.MediaUnit(status.CorePackage, b.Unit); !b.Matches(status) || !ok || unit.State != protocol.MediaUnitEmpty {
		return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	return status, nil
}

// retainMediaUnitSessionIdentity restores the host library association the
// target status cannot carry.
func (s *Service) retainMediaUnitSessionIdentity(status protocol.Status, b protocol.MediaUnitBinding) protocol.Status {
	s.executionMu.Lock()
	defer s.executionMu.Unlock()
	if s.activePackageID == b.PackageID && s.activePackageGeneration == b.Generation && s.activeGameID != "" {
		status.GameID = stringPtr(s.activeGameID)
		status.System = systemPtr(s.activeSystem)
	}
	return status
}

// diskUnitBinding binds a supported disk image to its declared media unit.
func diskUnitBinding(name string, b protocol.DevelopmentMediaBinding) protocol.MediaUnitBinding {
	unit := protocol.Apple2FloppyUnit
	if protocol.AdmitC64DiskName(name) {
		unit = protocol.C64DiskUnit
	} else if protocol.AdmitAtariStFloppyName(name) {
		unit = protocol.AtariStFloppyUnit
	}
	return protocol.MediaUnitBinding{PackageID: b.PackageID, Generation: b.Generation, Unit: unit, Target: b.Target, TargetID: b.TargetID}
}

func diskImageBytes(name string) (int64, bool) {
	switch {
	case protocol.AdmitDiskMediaName(name):
		return protocol.Apple2FloppyBytes, true
	case protocol.AdmitC64DiskName(name):
		return protocol.C64DiskBytes, true
	case protocol.AdmitAtariStFloppyName(name):
		return protocol.AtariStFloppyBytes, true
	default:
		return 0, false
	}
}

func cassetteUnitBinding(b protocol.DevelopmentMediaBinding) protocol.MediaUnitBinding {
	return protocol.MediaUnitBinding{PackageID: b.PackageID, Generation: b.Generation, Unit: protocol.SpectrumTapeUnit, Target: b.Target, TargetID: b.TargetID}
}

// replaceLiveDisk inserts a household disk image into the running machine.
func (s *Service) replaceLiveDisk(parent context.Context, mediaID, name string, b protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	size, named := diskImageBytes(name)
	if !b.Valid() || b.Target == "" || protocol.ValidateDigest(mediaID) != nil || !named {
		return protocol.Status{}, protocol.DiskMediaRequestError()
	}
	ctx, cancel := serviceTimeout(parent, max(s.uploadTimeout, 150*time.Second))
	defer cancel()
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return protocol.Status{}, err
	}
	defer release()
	store, ok := s.catalog.(coreMediaCatalog)
	if !ok {
		return protocol.Status{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	info, err := store.CoreMediaInfo(ctx, mediaID)
	if err != nil {
		return protocol.Status{}, mapCoreMediaError(err)
	}
	if info.Size != size {
		return protocol.Status{}, protocol.DiskMediaRequestError()
	}
	opened, reader, err := store.OpenCoreMedia(ctx, mediaID)
	if err != nil {
		return protocol.Status{}, mapCoreMediaError(err)
	}
	defer reader.Close()
	if opened != info {
		return protocol.Status{}, canonicalError(protocol.CodeInternal, nil)
	}
	unit := diskUnitBinding(name, b)
	status, err := s.insertBoundMediaUnitLocked(ctx, info.Size, reader, unit, nil, parent)
	if err != nil {
		return status, err
	}
	return s.retainMediaUnitSessionIdentity(status, unit), nil
}

// replaceLiveCassette inserts a .tap image into the running Spectrum.
func (s *Service) replaceLiveCassette(parent context.Context, mediaID, name string, b protocol.DevelopmentMediaBinding) (protocol.Status, error) {
	if !b.Valid() || b.Target == "" || protocol.ValidateDigest(mediaID) != nil || !protocol.AdmitSpectrumTapeName(name) {
		return protocol.Status{}, protocol.CassetteMediaRequestError()
	}
	ctx, cancel := serviceTimeout(parent, max(s.uploadTimeout, 150*time.Second))
	defer cancel()
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return protocol.Status{}, err
	}
	defer release()
	store, ok := s.catalog.(coreMediaCatalog)
	if !ok {
		return protocol.Status{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	info, err := store.CoreMediaInfo(ctx, mediaID)
	if err != nil {
		return protocol.Status{}, mapCoreMediaError(err)
	}
	if !protocol.AdmitSpectrumTapeSize(info.Size) {
		return protocol.Status{}, protocol.CassetteMediaRequestError()
	}
	opened, reader, err := store.OpenCoreMedia(ctx, mediaID)
	if err != nil {
		return protocol.Status{}, mapCoreMediaError(err)
	}
	defer reader.Close()
	if opened != info {
		return protocol.Status{}, canonicalError(protocol.CodeInternal, nil)
	}
	unit := cassetteUnitBinding(b)
	status, err := s.insertMediaUnitLocked(ctx, info.Size, reader, unit)
	if err != nil {
		return status, err
	}
	return s.retainMediaUnitSessionIdentity(status, unit), nil
}
