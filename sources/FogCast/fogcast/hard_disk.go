package fogcast

import (
	"bytes"
	"context"
	"errors"
	"io"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

// hardDiskActivationBudget covers staging up to 128 MiB on the kit, the
// runtime's window writes and the core's CRC pass before execution starts.
const hardDiskActivationBudget = 300 * time.Second

type hardDiskCatalog interface {
	ImportHardDiskMediaStream(context.Context, int64, io.Reader) (catalog.CoreMedia, bool, error)
	SetCoreEntryDiskGeometry(context.Context, string, string, string, *protocol.DiskGeometry) (catalog.CoreEntry, error)
}

// HardDiskMedia is an imported raw image and the geometry inferred from its
// master boot record.
type HardDiskMedia struct {
	catalog.CoreMedia
	Geometry protocol.DiskGeometry `json:"geometry"`
}

// ImportHardDiskMedia stores a raw hard-disk image of 1..128 MiB. It never
// selects the image for an entry.
func (s *Service) ImportHardDiskMedia(parent context.Context, size int64, body io.Reader) (HardDiskMedia, bool, error) {
	store, ok := s.catalog.(hardDiskCatalog)
	media, readable := s.catalog.(coreMediaCatalog)
	if !ok || !readable {
		return HardDiskMedia{}, false, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	if !protocol.AdmitAtaDiskSize(size) {
		return HardDiskMedia{}, false, &protocol.APIError{Code: protocol.CodeBadRequest, Phase: "request",
			Message: "hard-disk image must be 1 to 128 MiB in whole 512-byte sectors"}
	}
	ctx, cancel := serviceTimeout(parent, max(s.uploadTimeout, hardDiskActivationBudget))
	defer cancel()
	value, created, err := store.ImportHardDiskMediaStream(ctx, size, body)
	if err != nil {
		return HardDiskMedia{}, false, mapCoreMediaError(err)
	}
	opened, reader, err := media.OpenCoreMedia(ctx, value.MediaID)
	if err != nil {
		return HardDiskMedia{}, false, mapCoreMediaError(err)
	}
	geometry, err := readDiskGeometry(reader, opened.Size)
	if closeErr := reader.Close(); err == nil && closeErr != nil {
		err = canonicalError(protocol.CodeInternal, safeContextError(closeErr))
	}
	if err != nil {
		return HardDiskMedia{}, false, err
	}
	return HardDiskMedia{CoreMedia: value, Geometry: geometry}, created, nil
}

func readDiskGeometry(reader io.Reader, size int64) (protocol.DiskGeometry, error) {
	sector := make([]byte, protocol.AtaDiskSectorBytes)
	if _, err := io.ReadFull(reader, sector); err != nil {
		return protocol.DiskGeometry{}, canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	geometry, err := protocol.InferDiskGeometry(sector, size)
	if err != nil {
		return protocol.DiskGeometry{}, &protocol.APIError{Code: protocol.CodeBadRequest, Phase: "request", Message: err.Error()}
	}
	return geometry, nil
}

// SetCoreEntryDiskGeometry overrides the inferred geometry of an entry's
// selected hard disk for the next launch, or with nil restores inference.
func (s *Service) SetCoreEntryDiskGeometry(parent context.Context, gameID, expectedPackage, expectedMedia string, geometry *protocol.DiskGeometry) (catalog.CoreEntry, error) {
	if protocol.ValidateGameID(gameID) != nil || protocol.ValidateDigest(expectedPackage) != nil || protocol.ValidateDigest(expectedMedia) != nil {
		return catalog.CoreEntry{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	ctx, cancel := serviceTimeout(parent, s.uploadTimeout)
	defer cancel()
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return catalog.CoreEntry{}, err
	}
	defer release()
	store, ok := s.catalog.(hardDiskCatalog)
	if !ok {
		return catalog.CoreEntry{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	entry, err := store.SetCoreEntryDiskGeometry(ctx, gameID, expectedPackage, expectedMedia, geometry)
	if err != nil {
		return catalog.CoreEntry{}, mapCoreMediaError(err)
	}
	return entry, nil
}

// hardDiskSource is a pc386 launch's disk: the staged identity and the
// verified catalog snapshot to upload when the kit does not hold it yet.
type hardDiskSource struct {
	disk   protocol.StagedDisk
	reader io.ReadCloser
}

func (d *hardDiskSource) Close() error {
	if d == nil || d.reader == nil {
		return nil
	}
	err := d.reader.Close()
	d.reader = nil
	if err != nil {
		return canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return nil
}

// readHardDisk opens an entry's selected hard disk with its launch geometry:
// the entry's override or the one its master boot record implies. No
// selection launches with the drive empty.
func (s *Service) readHardDisk(ctx context.Context, entry catalog.CoreEntry) (*hardDiskSource, error) {
	if entry.MediaRole == "" && entry.MediaID == "" {
		return nil, nil
	}
	if entry.MediaRole != protocol.HardDiskRole || protocol.ValidateDigest(entry.MediaID) != nil {
		return nil, &protocol.APIError{Code: protocol.CodeBadRequest, Phase: "admission",
			Message: "a pc386 entry launches only a selected hard-disk image"}
	}
	store, ok := s.catalog.(coreMediaCatalog)
	if !ok {
		return nil, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	media, reader, err := store.OpenCoreMedia(ctx, entry.MediaID)
	if err != nil {
		return nil, mapCoreMediaError(err)
	}
	if media.MediaID != entry.MediaID || !protocol.AdmitAtaDiskSize(media.Size) {
		return nil, errors.Join(protocol.HardDiskRequestError(), reader.Close())
	}
	sector := make([]byte, protocol.AtaDiskSectorBytes)
	if _, err := io.ReadFull(reader, sector); err != nil {
		return nil, errors.Join(canonicalError(protocol.CodeInternal, safeContextError(err)), reader.Close())
	}
	geometry, err := protocol.InferDiskGeometry(sector, media.Size)
	if entry.DiskGeometry != nil {
		geometry, err = *entry.DiskGeometry, nil
	}
	disk := protocol.StagedDisk{SHA256: media.MediaID, Size: media.Size, Geometry: geometry}
	if err != nil || !disk.Valid() {
		return nil, errors.Join(protocol.HardDiskRequestError(), reader.Close())
	}
	return &hardDiskSource{disk: disk, reader: readCloser{Reader: io.MultiReader(bytes.NewReader(sector), reader), Closer: reader}}, nil
}

// machineCoreClient is the kit surface of pc386 library launches.
type machineCoreClient interface {
	DiskImage(context.Context, string) (protocol.DiskImageStatus, error)
	StageDiskImage(context.Context, string, int64, io.Reader) (protocol.DiskImageStatus, error)
	LoadLibraryMachineCore(context.Context, int64, io.Reader, string, protocol.LibraryMachineBinding) (protocol.Status, error)
}

// stageHardDisk uploads the disk only when the kit does not already hold a
// copy with its digest, so relaunching does not move up to 128 MiB. A kit
// without the staging route predates hard-disk launches.
func stageHardDisk(ctx context.Context, client machineCoreClient, source *hardDiskSource) error {
	disk := source.disk
	held, err := client.DiskImage(ctx, disk.SHA256)
	if err != nil {
		return kitDiskStagingError(err)
	}
	if held.Staged && held.SHA256 == disk.SHA256 && held.Size == disk.Size {
		return nil
	}
	staged, err := client.StageDiskImage(ctx, disk.SHA256, disk.Size, source.reader)
	if err != nil {
		return kitDiskStagingError(err)
	}
	if !staged.Staged || staged.SHA256 != disk.SHA256 || staged.Size != disk.Size {
		return canonicalError(protocol.CodeMiSTerUnavailable, nil)
	}
	return nil
}

func kitDiskStagingError(err error) error {
	var apiErr *protocol.APIError
	if errors.As(err, &apiErr) && apiErr.Code == protocol.CodeUnsupportedOperation {
		return apiErr
	}
	return canonicalRemoteError(err, protocol.CodeMiSTerUnavailable)
}

type readCloser struct {
	io.Reader
	io.Closer
}
