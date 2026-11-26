package fogcast

import (
	"context"
	"io"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

// floppySource is a pc386 boot floppy staged on the kit by digest.
type floppySource struct {
	floppy protocol.StagedFloppy
	reader io.ReadCloser
}

func (f *floppySource) Close() error {
	if f == nil || f.reader == nil {
		return nil
	}
	err := f.reader.Close()
	f.reader = nil
	if err != nil {
		return canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return nil
}

func validateLaunchFloppySize(size int64) error {
	drive, ok := protocol.PcFloppyDrive(size)
	if !ok {
		return protocol.PcFloppySizeError(protocol.PcFloppyUnitA, size)
	}
	if drive == protocol.PcFloppyUnitB {
		return &protocol.APIError{Code: protocol.CodeBadRequest, Phase: "admission",
			Message: "pc386 boots from A: or C:. Choose a 720K, 1.44M or 1.68M boot floppy, or insert this image in B: during play."}
	}
	return nil
}

// readLaunchFloppy opens a floppy entry's boot disk for drive A:.
func (s *Service) readLaunchFloppy(ctx context.Context, entry catalog.CoreEntry) (*floppySource, error) {
	if entry.MediaRole != protocol.FloppyRole || protocol.ValidateDigest(entry.MediaID) != nil {
		return nil, canonicalError(protocol.CodeBadRequest, nil)
	}
	store, ok := s.catalog.(coreMediaCatalog)
	if !ok {
		return nil, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	media, reader, err := store.OpenCoreMedia(ctx, entry.MediaID)
	if err != nil {
		return nil, mapCoreMediaError(err)
	}
	if media.MediaID != entry.MediaID {
		return nil, joinClose(canonicalError(protocol.CodeInternal, nil), reader)
	}
	if err := validateLaunchFloppySize(media.Size); err != nil {
		return nil, joinClose(err, reader)
	}
	return &floppySource{floppy: protocol.StagedFloppy{Unit: protocol.PcFloppyUnitA, SHA256: media.MediaID, Size: media.Size}, reader: reader}, nil
}

func joinClose(err error, c io.Closer) error {
	if closeErr := c.Close(); closeErr != nil {
		return canonicalError(protocol.CodeInternal, safeContextError(closeErr))
	}
	return err
}

// stageLaunchFloppy uploads the disk only when the kit does not hold it.
func stageLaunchFloppy(ctx context.Context, client machineCoreClient, source *floppySource) error {
	return stageDiskImage(ctx, client, source.floppy.SHA256, source.floppy.Size, source.reader)
}

// launchFloppiesInserted checks the kit put each launch floppy in its drive:
// an older kit agent drops the floppy header and boots with the drive empty.
func launchFloppiesInserted(status protocol.Status, machine protocol.LibraryMachineBinding) bool {
	for _, f := range machine.Floppies {
		unit, ok := protocol.MediaUnit(status.CorePackage, f.Unit)
		if !ok || unit.Interface != protocol.PcFloppyInterface() || unit.State != protocol.MediaUnitReady ||
			(f.WriteProtected != (unit.Persistence == nil)) ||
			(unit.Persistence != nil && (unit.Persistence.BaseMediaID != f.SHA256 || unit.Persistence.GameID != machine.GameID)) {
			return false
		}
	}
	return true
}

func launchFloppyMissingError() *protocol.APIError {
	return &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Phase: "recovery",
		Message: "the kit started the machine without its launch floppy; update the kit software"}
}

// EjectPcFloppy empties pc386's A: or B:. The runtime saves a writable disk
// first; a failed save leaves it in the drive.
func (s *Service) EjectPcFloppy(parent context.Context, b protocol.MediaUnitBinding) (protocol.Status, error) {
	if !b.Valid() || b.Target == "" || !protocol.PcFloppyUnit(b.Unit) {
		return protocol.Status{}, mediaUnchanged(protocol.MediaUnitRequestError())
	}
	ctx, cancel := serviceTimeout(parent, 150*time.Second)
	defer cancel()
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return protocol.Status{}, mediaUnchanged(err)
	}
	defer release()
	ctx = WithSessionTarget(ctx, b.Target)
	if err := s.prepareMediaUnitClient(ctx, b); err != nil {
		return protocol.Status{}, mediaUnchanged(err)
	}
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	client, units, err := s.mediaUnitTargetLocked(b)
	if err != nil {
		return protocol.Status{}, mediaUnchanged(err)
	}
	prior, err := client.Status(ctx)
	if err != nil {
		return protocol.Status{}, mediaUnchanged(canonicalRemoteError(err, protocol.CodeMiSTerUnavailable))
	}
	if observed, ok := protocol.MediaUnit(prior.CorePackage, b.Unit); !ok || !b.Matches(prior) || observed.Interface != protocol.PcFloppyInterface() {
		return prior, mediaUnchanged(protocol.MediaUnitIdentityError())
	}
	status, err := units.EjectMedia(ctx, b)
	if err != nil {
		return status, preserveCorePackageError(err)
	}
	if state, ok := protocol.MediaUnit(status.CorePackage, b.Unit); !b.Matches(status) || !ok || state.State != protocol.MediaUnitEmpty {
		return protocol.Status{}, canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	return s.retainMediaUnitSessionIdentity(status, b), nil
}
