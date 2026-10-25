package fogcast

import (
	"bytes"
	"context"

	"github.com/DeanoC/FogCast/atarist"
	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

// BlankAtariStRequest asks the host library for one formatted drive-A image.
// Bind selects it on an existing title. Zero geometry is the 720 KiB disk.
type BlankAtariStRequest struct {
	Name              string
	Tracks            int
	Heads             int
	Sectors           int
	GameID            string
	ExpectedPackageID string
	ExpectedMediaID   string
	Bind              bool
}

// BlankAtariStDisk is the imported image. Entry is set when it was bound.
type BlankAtariStDisk struct {
	MediaID string
	Size    int64
	Name    string
	Tracks  int
	Heads   int
	Sectors int
	Created bool
	Entry   *catalog.CoreEntry
}

// CreateBlankAtariStDisk formats a disk, stores it in the host library, and
// optionally binds it as the title's next drive A. The saved bytes stay on
// the existing writable-ST path.
func (s *Service) CreateBlankAtariStDisk(parent context.Context, req BlankAtariStRequest) (BlankAtariStDisk, error) {
	disk, err := atarist.Format(req.Name, req.Tracks, req.Heads, req.Sectors)
	if err != nil {
		return BlankAtariStDisk{}, &protocol.APIError{Code: protocol.CodeBadRequest, Message: "blank ST disk name or geometry is invalid", Phase: "request"}
	}
	if req.Bind {
		if err := s.admitBlankAtariStDisk(parent, req, int64(len(disk.Bytes))); err != nil {
			return BlankAtariStDisk{}, err
		}
	}
	media, created, err := s.ImportCoreMedia(parent, int64(len(disk.Bytes)), bytes.NewReader(disk.Bytes))
	if err != nil {
		return BlankAtariStDisk{}, err
	}
	result := BlankAtariStDisk{
		MediaID: media.MediaID, Size: media.Size, Name: disk.Name,
		Tracks: disk.Tracks, Heads: disk.Heads, Sectors: disk.Sectors, Created: created,
	}
	if !req.Bind {
		return result, nil
	}
	entry, err := s.SelectCoreEntryMedia(parent, req.GameID, req.ExpectedPackageID, req.ExpectedMediaID, protocol.DiskRole, media.MediaID)
	if err != nil {
		return BlankAtariStDisk{}, err
	}
	result.Entry = &entry
	return result, nil
}

func (s *Service) admitBlankAtariStDisk(ctx context.Context, req BlankAtariStRequest, size int64) error {
	if protocol.ValidateGameID(req.GameID) != nil || protocol.ValidateDigest(req.ExpectedPackageID) != nil ||
		(req.ExpectedMediaID != "" && protocol.ValidateDigest(req.ExpectedMediaID) != nil) {
		return &protocol.APIError{Code: protocol.CodeBadRequest, Message: "blank ST disk binding is invalid", Phase: "request"}
	}
	entry, err := s.CoreEntry(ctx, req.GameID)
	if err != nil {
		return err
	}
	if entry.PackageID != req.ExpectedPackageID || entry.MediaID != req.ExpectedMediaID {
		return mapCoreEntryError(catalog.ErrCoreEntryConflict)
	}
	inspection, err := s.installedCoreInspection(ctx, entry.PackageID)
	if err != nil {
		return err
	}
	if inspection.Descriptor.Core.ID != entry.CoreID {
		return canonicalError(protocol.CodeInvalidArchive, nil)
	}
	if err := corepackage.ValidateWritableAtariStLibraryDiskPackage(inspection.Descriptor); err != nil {
		return canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	var capability *protocol.CoreMediaCapability
	for _, declared := range protocol.DeclaredCoreMediaCapabilities(inspection.Descriptor) {
		if declared.Role == protocol.DiskRole && declared.Format == "atari-st-floppy" {
			match := declared
			capability = &match
			break
		}
	}
	if capability == nil {
		return canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	if size < capability.MinBytes || size > capability.MaxBytes || !protocol.AdmitAtariStFloppySize(size) {
		return &protocol.APIError{Code: protocol.CodeBadRequest, Message: "blank ST disk geometry is outside this package", Phase: "request"}
	}
	return nil
}
