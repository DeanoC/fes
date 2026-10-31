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

// hardDiskImportRate is the slowest link a hard-disk import is given time
// for, about 1 Mbit/s: a 128 MiB image gets about 18 minutes.
const hardDiskImportRate = 128 << 10

// hardDiskImportBudget is the time an import of size bytes may take.
func hardDiskImportBudget(size int64) time.Duration {
	return time.Minute + time.Duration(size/hardDiskImportRate)*time.Second
}

// HardDiskMedia is an imported raw image and the geometry its master boot
// record implies, or why it implies none. Such an image is still stored; its
// entry needs a geometry override before it can launch.
type HardDiskMedia struct {
	catalog.CoreMedia
	Geometry      *protocol.DiskGeometry `json:"geometry,omitempty"`
	GeometryError string                 `json:"geometry_error,omitempty"`
}

// ImportHardDiskMedia stores a raw hard-disk image of 1..128 MiB. It never
// selects the image for an entry.
func (s *Service) ImportHardDiskMedia(parent context.Context, size int64, body io.Reader) (HardDiskMedia, bool, error) {
	store, ok := s.catalog.(hardDiskCatalog)
	if !ok {
		return HardDiskMedia{}, false, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	if !protocol.AdmitAtaDiskSize(size) {
		return HardDiskMedia{}, false, &protocol.APIError{Code: protocol.CodeBadRequest, Phase: "request",
			Message: "hard-disk image must be 1 to 128 MiB in whole 512-byte sectors"}
	}
	ctx, cancel := serviceTimeout(parent, max(s.uploadTimeout, hardDiskImportBudget(size)))
	defer cancel()
	// The catalog hashes every byte it stores, so the master boot record is
	// kept as it streams past rather than read back.
	var first sectorCapture
	value, created, err := store.ImportHardDiskMediaStream(ctx, size, io.TeeReader(body, &first))
	if err != nil {
		return HardDiskMedia{}, false, mapCoreMediaError(err)
	}
	media := HardDiskMedia{CoreMedia: value}
	if geometry, err := protocol.InferDiskGeometry(first.sector, value.Size); err != nil {
		media.GeometryError = err.Error()
	} else {
		media.Geometry = &geometry
	}
	return media, created, nil
}

// sectorCapture keeps the first sector written to it.
type sectorCapture struct{ sector []byte }

func (c *sectorCapture) Write(p []byte) (int, error) {
	if room := int(protocol.AtaDiskSectorBytes) - len(c.sector); room > 0 {
		c.sector = append(c.sector, p[:min(room, len(p))]...)
	}
	return len(p), nil
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
// the entry's override or the one its master boot record implies. A table
// that implies none refuses the launch with the reason. No selection
// launches with the drive empty.
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
	if err != nil {
		return nil, errors.Join(&protocol.APIError{Code: protocol.CodeBadRequest, Phase: "admission", Message: err.Error()}, reader.Close())
	}
	disk := protocol.StagedDisk{SHA256: media.MediaID, Size: media.Size, Geometry: geometry}
	if !disk.Valid() {
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
