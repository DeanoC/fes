package fogcast

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/spectrumdisks"
	"github.com/DeanoC/FogCast/internal/spectrumsnapshots"
	"github.com/DeanoC/FogCast/internal/spectrumtapes"
	"github.com/DeanoC/FogCast/protocol"
)

type coreMediaCatalog interface {
	ImportCoreMediaStream(context.Context, int64, io.Reader) (catalog.CoreMedia, bool, error)
	CoreMediaInfo(context.Context, string) (catalog.CoreMedia, error)
	OpenCoreMedia(context.Context, string) (catalog.CoreMedia, io.ReadCloser, error)
	CreateCoreMediaEntry(context.Context, string, string, string, string, string) (catalog.CoreEntry, error)
	SelectCoreEntryMedia(context.Context, string, string, string, string, string) (catalog.CoreEntry, error)
}

// ImportCoreMedia snapshots a bounded asset into the existing catalog. Import
// never selects media for an entry or mutates a running session.
func (s *Service) ImportCoreMedia(parent context.Context, size int64, body io.Reader) (catalog.CoreMedia, bool, error) {
	store, ok := s.catalog.(coreMediaCatalog)
	if !ok {
		return catalog.CoreMedia{}, false, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	ctx, cancel := serviceTimeout(parent, s.uploadTimeout)
	defer cancel()
	value, created, storeErr := store.ImportCoreMediaStream(ctx, size, body)
	return value, created, mapCoreMediaError(storeErr)
}

func (s *Service) CoreMedia(ctx context.Context, id string) (catalog.CoreMedia, error) {
	store, ok := s.catalog.(coreMediaCatalog)
	if !ok {
		return catalog.CoreMedia{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	value, err := store.CoreMediaInfo(ctx, id)
	return value, mapCoreMediaError(err)
}

// CoreMediaCapabilities is an offline declaration projection. The running
// target remains the authority for actual active capability and delivery.
func (s *Service) CoreMediaCapabilities(ctx context.Context, id string) (protocol.CoreMediaCapabilities, error) {
	inspection, err := s.installedCoreInspection(ctx, id)
	if err != nil {
		return protocol.CoreMediaCapabilities{}, err
	}
	return protocol.CoreMediaCapabilities{
		PackageID: inspection.PackageID, Source: "declared-contract",
		Compatibility: "unknown", ImportMaxBytes: catalog.MaxCoreMediaBytes,
		Media:    protocol.DeclaredCoreMediaCapabilities(inspection.Descriptor),
		Firmware: protocol.DeclaredFirmwareCapabilities(inspection.Descriptor),
	}, nil
}

// SelectCoreEntryMedia changes only the next launch. The expected package and
// media identities prevent stale clients overwriting another selection.
func (s *Service) SelectCoreEntryMedia(parent context.Context, gameID, expectedPackage, expectedMedia, role, mediaID string) (catalog.CoreEntry, error) {
	if protocol.ValidateGameID(gameID) != nil || protocol.ValidateDigest(expectedPackage) != nil ||
		(expectedMedia != "" && protocol.ValidateDigest(expectedMedia) != nil) {
		return catalog.CoreEntry{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	ctx, cancel := serviceTimeout(parent, s.uploadTimeout)
	defer cancel()
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return catalog.CoreEntry{}, err
	}
	defer release()
	store, ok := s.catalog.(coreMediaCatalog)
	if !ok {
		return catalog.CoreEntry{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	entry, err := s.CoreEntry(ctx, gameID)
	if err != nil {
		return catalog.CoreEntry{}, err
	}
	if entry.PackageID != expectedPackage || entry.MediaID != expectedMedia {
		return catalog.CoreEntry{}, mapCoreEntryError(catalog.ErrCoreEntryConflict)
	}
	inspection, err := s.installedCoreInspection(ctx, entry.PackageID)
	if err != nil {
		return catalog.CoreEntry{}, err
	}
	if inspection.Descriptor.Core.ID != entry.CoreID {
		return catalog.CoreEntry{}, canonicalError(protocol.CodeInvalidArchive, nil)
	}
	if err := s.validateCoreEntryMedia(ctx, inspection.Descriptor, role, mediaID); err != nil {
		return catalog.CoreEntry{}, err
	}
	selected, err := store.SelectCoreEntryMedia(ctx, gameID, expectedPackage, expectedMedia, role, mediaID)
	return selected, mapCoreMediaError(err)
}

// readCoreEntryMedia validates and snapshots the selected bytes before package
// activation. This is capability-based: a core ID never chooses content.
type coreEntryMedia struct {
	io.ReadCloser
	size   int64
	stream bool
	// unit names the fes.computer media unit a removable disk is inserted
	// into; selected writable ST disks are included in ROM activation before
	// CPU release. Nil keeps the launch-time load_media delivery.
	unit *uint8
}

func (m *coreEntryMedia) Close() error {
	if err := m.ReadCloser.Close(); err != nil {
		return canonicalError(protocol.CodeInternal, safeContextError(err))
	}
	return nil
}

func (s *Service) validateCoreEntryMedia(ctx context.Context, descriptor corepackage.Descriptor, role, id string) error {
	media, err := s.readCoreEntryMedia(ctx, descriptor, role, id)
	if err != nil || media == nil {
		return err
	}
	return media.Close()
}

// savedZx81TapeRole keeps setups saved as blob launchable after the ZX81
// package moved to the cassette unit. A descriptor that still declares blob
// keeps that role.
func savedZx81TapeRole(descriptor corepackage.Descriptor, role string) string {
	if role != "blob" || !protocol.DeclaresZx81Tape(descriptor) {
		return role
	}
	for _, declared := range protocol.DeclaredCoreMediaCapabilities(descriptor) {
		if declared.Role == "blob" {
			return role
		}
	}
	return protocol.CassetteRole
}

func (s *Service) readCoreEntryMedia(ctx context.Context, descriptor corepackage.Descriptor, role, id string) (*coreEntryMedia, error) {
	if role == "" && id == "" {
		return nil, nil
	}
	role = savedZx81TapeRole(descriptor, role)
	if (role != "blob" && role != protocol.DiskRole && role != protocol.CassetteRole) || protocol.ValidateDigest(id) != nil {
		return nil, canonicalError(protocol.CodeBadRequest, nil)
	}
	var capability *protocol.CoreMediaCapability
	for _, declared := range protocol.DeclaredCoreMediaCapabilities(descriptor) {
		if declared.Role == role {
			capability = &declared
			break
		}
	}
	if capability == nil {
		return nil, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	store, ok := s.catalog.(coreMediaCatalog)
	if !ok {
		return nil, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	media, err := store.CoreMediaInfo(ctx, id)
	if err != nil {
		return nil, mapCoreMediaError(err)
	}
	if media.Size < capability.MinBytes || media.Size > capability.MaxBytes || (capability.Interface == protocol.AtariStFloppyInterface() && !protocol.AdmitAtariStFloppySize(media.Size)) {
		return nil, &protocol.APIError{Code: protocol.CodeBadRequest, Phase: "request",
			Message: fmt.Sprintf("selected %s media is %d bytes; package contract accepts %d..%d bytes", role, media.Size, capability.MinBytes, capability.MaxBytes)}
	}
	opened, reader, err := store.OpenCoreMedia(ctx, id)
	if err != nil {
		return nil, mapCoreMediaError(err)
	}
	if opened != media {
		closeErr := (&coreEntryMedia{ReadCloser: reader}).Close()
		return nil, errors.Join(canonicalError(protocol.CodeInternal, nil), closeErr)
	}
	if capability.Interface == protocol.AtariStFloppyInterface() && media.Size != protocol.AtariStFloppyBytes {
		data, readErr := io.ReadAll(io.LimitReader(reader, media.Size+1))
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil || int64(len(data)) != media.Size || !corepackage.ValidAtariStBase(data, corepackage.DeclaresAtariStGeometry(descriptor)) {
			return nil, protocol.DiskMediaRequestError()
		}
		reader = io.NopCloser(bytes.NewReader(data))
	}
	size := media.Size
	if capability.Interface == protocol.SpectrumTapeInterface() {
		stream, err := spectrumCassetteStream(descriptor, "", reader, media.Size)
		if err != nil {
			return nil, err
		}
		reader, size = io.NopCloser(bytes.NewReader(stream)), int64(len(stream))
	}
	if capability.Interface == protocol.SpectrumDiskInterface() {
		// A launch disk carries the boot flag: the core boots it once it is in.
		system, _ := protocol.SpectrumDiskSystem(descriptor)
		image, err := spectrumDiskImage(reader, media.Size, system, true)
		if err != nil {
			return nil, err
		}
		reader, size = io.NopCloser(bytes.NewReader(image)), int64(len(image))
	}
	// OpenCoreMedia returns a verified private snapshot, not a live SQL cursor.
	return &coreEntryMedia{ReadCloser: reader, size: size, stream: capability.Interface == protocol.MediaStreamInterface(), unit: capability.Unit}, nil
}

// spectrumCassetteStream reads a stored .tzx, .tap, .pzx or .csw image and
// converts it to the PZX stream fes.media.spectrum-tape 2.0 plays, or a .z80,
// .sna or .szx snapshot that descriptor runs to its restore stream. A tape or
// snapshot name decides which it is. Library media and inserts by digest have
// no name (""), and then the content decides: .tap has no signature, so a
// snapshot's bytes can tile as .tap blocks and a .tap can be the size of a
// snapshot. TZX, PZX and CSW signatures keep those images on the tape path
// before snapshot probing. A file with a ZXST signature is a snapshot, and
// another valid snapshot is taken as one unless every .tap block also passes
// the ROM loader's checksum. It closes reader.
func spectrumCassetteStream(descriptor corepackage.Descriptor, name string, reader io.ReadCloser, size int64) ([]byte, error) {
	data, readErr := io.ReadAll(io.LimitReader(reader, size+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || int64(len(data)) != size {
		return nil, errors.Join(canonicalError(protocol.CodeInternal, nil), readErr, closeErr)
	}
	snapshot := spectrumsnapshots.Admit(name)
	if name == "" {
		snapshot = bytes.HasPrefix(data, []byte("ZXST")) ||
			!spectrumtapes.HasSignature(data) && spectrumsnapshots.Recognize(data) && !spectrumtapes.CheckedTAP(data)
	}
	if snapshot {
		machine, stream, err := spectrumsnapshots.Convert(data)
		if err != nil {
			return nil, protocol.SnapshotConversionError(err)
		}
		if !protocol.SpectrumSnapshotRuns(descriptor, machine) {
			return nil, protocol.SnapshotConversionError(errors.New(snapshotMachineNames[machine] + " snapshots do not run on this package"))
		}
		return stream, nil
	}
	pzx, err := spectrumtapes.Convert(data)
	if err != nil {
		return nil, protocol.CassetteConversionError(err)
	}
	return pzx, nil
}

// spectrumDiskImage reads a stored disk image written for the package's disk
// system and converts it to the raw track image fes.media.spectrum-disk 1.0
// reads, flagged to boot when a launch inserts it. It closes reader.
func spectrumDiskImage(reader io.ReadCloser, size int64, system spectrumdisks.System, boot bool) ([]byte, error) {
	data, readErr := io.ReadAll(io.LimitReader(reader, size+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || int64(len(data)) != size {
		return nil, errors.Join(canonicalError(protocol.CodeInternal, nil), readErr, closeErr)
	}
	image, err := spectrumdisks.Convert(data, system, boot)
	if err != nil {
		return nil, protocol.SpectrumDiskConversionError(err)
	}
	return image, nil
}

var snapshotMachineNames = map[spectrumsnapshots.Machine]string{
	spectrumsnapshots.Machine48K: "48K", spectrumsnapshots.Machine128K: "128K", spectrumsnapshots.MachinePlus3: "+2A/+3",
}

func mapCoreMediaError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, catalog.ErrInvalidCoreMedia):
		return canonicalError(protocol.CodeBadRequest, nil)
	case errors.Is(err, catalog.ErrCoreMediaNotFound):
		return canonicalError(protocol.CodeROMNotFound, nil)
	default:
		return mapCoreEntryError(err)
	}
}
