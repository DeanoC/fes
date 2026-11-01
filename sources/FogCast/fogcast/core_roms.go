package fogcast

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"reflect"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/zx81machine"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

type coreROMCatalog interface {
	CoreEntryROM(context.Context, string) (catalog.CoreEntryROM, error)
	SelectCoreEntryROM(context.Context, string, string, string, int64, string, string) (catalog.CoreEntryROM, error)
}

type cartridgeOverrideKey struct{}

type cartridgeOverride struct{ gameID, mediaID string }

// WithCartridge runs one launch of gameID with mediaID as its cartridge, in
// place of the title's binding. The library binding is never changed: the
// next ordinary launch of the title uses its own cartridge again.
func WithCartridge(ctx context.Context, gameID, mediaID string) context.Context {
	return context.WithValue(ctx, cartridgeOverrideKey{}, cartridgeOverride{gameID, mediaID})
}

// CartridgeFromContext is the swap cartridge for gameID, if any.
func CartridgeFromContext(ctx context.Context, gameID string) (string, bool) {
	o, ok := ctx.Value(cartridgeOverrideKey{}).(cartridgeOverride)
	return o.mediaID, ok && o.gameID == gameID
}

// linkedCartridge reports whether the ROM an entry selects is a cartridge:
// the single format-3 ROM, or the second format-4 source after the BIOS.
func linkedCartridge(d corepackage.Descriptor) bool {
	if d.Format == 4 {
		return len(d.ROMs) == 2 && d.ROMs[1].Role == "cartridge"
	}
	return d.Format == 3 && d.ROM != nil && d.ROM.Role == "cartridge"
}

func romAdmissionError() error {
	return &protocol.APIError{Code: protocol.CodeBadRequest, Phase: "admission", Message: "required ROM must select an exact-size binary for this package and named ROM requirement"}
}
func romSelectionError(err error) error {
	if errors.Is(err, catalog.ErrInvalidCoreROM) || errors.Is(err, catalog.ErrInvalidCoreMedia) || errors.Is(err, catalog.ErrCoreMediaNotFound) {
		return romAdmissionError()
	}
	return mapCoreEntryError(err)
}
func (s *Service) CoreEntryROM(ctx context.Context, gameID string) (catalog.CoreEntryROM, error) {
	store, ok := s.catalog.(coreROMCatalog)
	if !ok {
		return catalog.CoreEntryROM{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	entry, err := s.CoreEntry(ctx, gameID)
	if err != nil {
		return catalog.CoreEntryROM{}, err
	}
	result, err := store.CoreEntryROM(ctx, gameID)
	if err != nil {
		return result, romSelectionError(err)
	}
	if result.MediaID != "" {
		return result, nil
	}
	inspection, err := s.installedCoreInspection(ctx, entry.PackageID)
	if err != nil || inspection.Descriptor.Format != 3 || inspection.Descriptor.Core.ID != entry.CoreID ||
		inspection.Descriptor.ROM == nil || inspection.Descriptor.ROM.Role != protocol.FirmwareRole {
		return result, nil
	}
	if fw, ok := s.defaultBundledFirmware(ctx, entry, inspection.Descriptor.ROM); ok {
		return bundledFirmwareSelection(entry, inspection.Descriptor.ROM, fw), nil
	}
	return result, nil
}
func (s *Service) SelectCoreEntryROM(parent context.Context, gameID, packageID, romID, expected, mediaID string) (catalog.CoreEntryROM, error) {
	ctx, cancel := serviceTimeout(parent, s.uploadTimeout)
	defer cancel()
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return catalog.CoreEntryROM{}, err
	}
	defer release()
	store, ok := s.catalog.(coreROMCatalog)
	if !ok {
		return catalog.CoreEntryROM{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	entry, err := s.CoreEntry(ctx, gameID)
	if err != nil {
		return catalog.CoreEntryROM{}, err
	}
	if entry.PackageID != packageID {
		return catalog.CoreEntryROM{}, mapCoreEntryError(catalog.ErrCoreEntryConflict)
	}
	inspected, err := s.installedCoreInspection(ctx, packageID)
	if err != nil {
		return catalog.CoreEntryROM{}, err
	}
	req := inspected.Descriptor.ROM
	var sourceSize int64
	if req != nil {
		sourceSize = req.SourceSize
	}
	if inspected.Descriptor.Format == 4 && len(inspected.Descriptor.ROMs) == 2 {
		romIDExpected := inspected.Descriptor.ROMs[1].ID
		if romID != romIDExpected {
			return catalog.CoreEntryROM{}, romAdmissionError()
		}
		sourceSize = inspected.Descriptor.ROMs[1].SourceSize
	} else if req == nil || req.ID != romID {
		return catalog.CoreEntryROM{}, romAdmissionError()
	}
	if expected != "" {
		current, currentErr := store.CoreEntryROM(ctx, gameID)
		if currentErr != nil {
			return catalog.CoreEntryROM{}, romSelectionError(currentErr)
		}
		// GET reports the effective bundled default even though that default
		// is not saved. Translate its digest back to the empty stored value for
		// the compare-and-swap when a user chooses or clears a ROM.
		if current.MediaID == "" && inspected.Descriptor.Format == 3 && req != nil && req.Role == protocol.FirmwareRole {
			if fw, ok := s.bundledFirmwareFor(entry.CoreID, req); ok && expected == fw.MediaID {
				expected = ""
			}
		}
	}
	value, err := store.SelectCoreEntryROM(ctx, gameID, packageID, romID, sourceSize, expected, mediaID)
	return value, romSelectionError(err)
}
func (s *Service) readCoreEntryROM(ctx context.Context, entry catalog.CoreEntry, descriptor corepackage.Descriptor) (catalog.CoreEntryROM, []byte, error) {
	if err := validateROMMediaContract(descriptor); err != nil {
		return catalog.CoreEntryROM{}, nil, err
	}
	store, ok := s.catalog.(coreROMCatalog)
	media, mediaOK := s.catalog.(coreMediaCatalog)
	if !ok || !mediaOK || (descriptor.ROM == nil && descriptor.Format != 4) {
		return catalog.CoreEntryROM{}, nil, romAdmissionError()
	}
	selected, err := store.CoreEntryROM(ctx, entry.GameID)
	if err != nil {
		return selected, nil, romSelectionError(err)
	}
	reqID, reqSize := "", int64(0)
	if descriptor.ROM != nil {
		reqID, reqSize = descriptor.ROM.ID, descriptor.ROM.SourceSize
	}
	if descriptor.Format == 4 && len(descriptor.ROMs) == 2 {
		reqID, reqSize = descriptor.ROMs[1].ID, descriptor.ROMs[1].SourceSize
	}
	if descriptor.Core.ID == entry.CoreID && descriptor.Format == 3 && descriptor.ROM != nil && descriptor.ROM.Role == protocol.FirmwareRole {
		bundled, hasBundled := s.bundledFirmwareFor(entry.CoreID, descriptor.ROM)
		stale := selected.MediaID != "" && selected.PackageID != entry.PackageID && selected.ROMID == reqID && selected.SourceSize == reqSize
		zx81 := entry.CoreID == "fes.zx81" && reqID == "machine-rom" && reqSize == 8192
		switch {
		case selected.MediaID == "" && hasBundled:
			selected = bundledFirmwareSelection(entry, descriptor.ROM, bundled)
		case selected.MediaID == "" && zx81:
			selected, err = s.provisionZX81MachineROM(ctx, entry, descriptor.ROM)
		case stale && (zx81 || hasBundled && selected.MediaID == bundled.MediaID):
			selected, err = s.rebindCoreFirmware(ctx, entry, selected, descriptor.ROM)
		}
		if err != nil {
			return selected, nil, err
		}
	}
	if selected.MediaID == "" || selected.PackageID != entry.PackageID || selected.ROMID != reqID || selected.SourceSize != reqSize {
		return selected, nil, romAdmissionError()
	}
	// A cartridge swap over play replaces only the linked cartridge, never
	// firmware; a format-4 package keeps its BIOS source.
	if mediaID, ok := CartridgeFromContext(ctx, entry.GameID); ok {
		if !linkedCartridge(descriptor) {
			return selected, nil, romAdmissionError()
		}
		selected.MediaID = mediaID
	}
	object, reader, err := media.OpenCoreMedia(ctx, selected.MediaID)
	if err != nil {
		return selected, nil, romSelectionError(err)
	}
	defer reader.Close()
	if object.Size != reqSize || object.MediaID != selected.MediaID {
		return selected, nil, romAdmissionError()
	}
	data, err := io.ReadAll(io.LimitReader(reader, reqSize+1))
	if err != nil || int64(len(data)) != reqSize {
		return selected, nil, romAdmissionError()
	}
	return selected, data, nil
}

// provisionZX81MachineROM imports the built-in ZX81 machine image only when a
// title has no firmware selection. A saved user choice always takes priority.
func (s *Service) provisionZX81MachineROM(ctx context.Context, entry catalog.CoreEntry, requirement *corepackage.ROM) (catalog.CoreEntryROM, error) {
	if requirement == nil || requirement.ID != "machine-rom" || requirement.Role != protocol.FirmwareRole || requirement.SourceSize != zx81machine.MachineROMBytes {
		return catalog.CoreEntryROM{}, romAdmissionError()
	}
	roms, ok := s.catalog.(coreROMCatalog)
	if !ok {
		return catalog.CoreEntryROM{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	mediaStore, ok := s.catalog.(coreMediaCatalog)
	if !ok {
		return catalog.CoreEntryROM{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	image, err := zx81machine.MachineROM()
	if err != nil || int64(len(image)) != requirement.SourceSize {
		return catalog.CoreEntryROM{}, romAdmissionError()
	}
	media, _, err := mediaStore.ImportCoreMediaStream(ctx, int64(len(image)), bytes.NewReader(image))
	if err != nil {
		return catalog.CoreEntryROM{}, mapCoreMediaError(err)
	}
	selected, err := roms.SelectCoreEntryROM(ctx, entry.GameID, entry.PackageID, requirement.ID, requirement.SourceSize, "", media.MediaID)
	if err == nil {
		return selected, nil
	}
	// A concurrent request may have filled the empty selection after our read.
	// Accept it only when it now matches this package's exact firmware contract.
	current, readErr := roms.CoreEntryROM(ctx, entry.GameID)
	if readErr == nil && current.MediaID != "" && current.PackageID == entry.PackageID && current.ROMID == requirement.ID && current.SourceSize == requirement.SourceSize {
		return current, nil
	}
	return catalog.CoreEntryROM{}, romSelectionError(err)
}

// rebindCoreFirmware carries a saved firmware binary across package changes
// only when the active package declares the same named, exact-size contract.
// It applies to every ZX81 selection and to bundled firmware on any core.
// The media digest is the compare-and-swap value, so concurrent user choices
// are not overwritten.
func (s *Service) rebindCoreFirmware(ctx context.Context, entry catalog.CoreEntry, selected catalog.CoreEntryROM, requirement *corepackage.ROM) (catalog.CoreEntryROM, error) {
	roms, ok := s.catalog.(coreROMCatalog)
	if !ok {
		return catalog.CoreEntryROM{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	rebound, err := roms.SelectCoreEntryROM(ctx, entry.GameID, entry.PackageID, requirement.ID, requirement.SourceSize, selected.MediaID, selected.MediaID)
	if err == nil {
		return rebound, nil
	}
	// A concurrent request may already have migrated or replaced the selection.
	// Keep its winner only if it now matches the active package's exact contract.
	current, readErr := roms.CoreEntryROM(ctx, entry.GameID)
	if readErr == nil && current.MediaID != "" && current.PackageID == entry.PackageID && current.ROMID == requirement.ID && current.SourceSize == requirement.SourceSize {
		return current, nil
	}
	return selected, romSelectionError(err)
}

func (s *Service) romLaunchSource(ctx context.Context, entry catalog.CoreEntry, descriptor corepackage.Descriptor, base []byte, initial *corepackage.InitialMedia) (coreLoadSource, error) {
	// Format 4 is Coleco only: the 8 KiB BIOS with the 16 KiB cartridge
	// shell or the 128 KiB MegaCart lane.
	if descriptor.Format == 4 && (descriptor.Core.ID != "fes.coleco" || len(descriptor.ROMs) != 2 || descriptor.ROMs[0].SourceSize != 8192 || (descriptor.ROMs[1].SourceSize != 16384 && descriptor.ROMs[1].SourceSize != 131072)) {
		return coreLoadSource{}, romAdmissionError()
	}
	selected, rom, err := s.readCoreEntryROM(ctx, entry, descriptor)
	if err != nil {
		return coreLoadSource{}, err
	}
	var videoParts []expansion.Asset
	shell := corepackage.Inspection{PackageID: entry.PackageID, Descriptor: descriptor}
	if hasVideoSocket(shell) {
		videoParts, err = s.videoEntryAssets(ctx, entry, shell, base)
		if err != nil {
			return coreLoadSource{}, err
		}
	}
	var asset *expansion.Asset
	if store, ok := s.catalog.(coreExpansionCatalog); ok && len(videoParts) == 0 {
		selection, err := store.CoreEntryExpansion(ctx, entry.GameID)
		if err != nil {
			return coreLoadSource{}, expansionError(err)
		}
		if selection.ExpansionID != "" {
			value, err := store.ReadCoreExpansion(ctx, selection.ExpansionID)
			if err != nil {
				return coreLoadSource{}, expansionError(err)
			}
			if corepackage.ValidateExpansionArchive(base, value) != nil {
				return coreLoadSource{}, expansionError(catalog.ErrInvalidCoreExpansion)
			}
			asset = &value
		}
	}
	var slotCards []expansion.Asset
	if store, ok := s.catalog.(coreSlotExpansionCatalog); ok && corepackage.SlotSockets(descriptor) != nil && len(videoParts) == 0 {
		cards, _, err := s.readSlotCards(ctx, store, entry, corepackage.Inspection{PackageID: entry.PackageID, Descriptor: descriptor}, base)
		if err != nil {
			return coreLoadSource{}, err
		}
		slotCards = cards
	}
	var data []byte
	var biosMediaID string
	var slotComposition *expansion.SlotComposition
	var partsComposition *expansion.PartsComposition
	if descriptor.Format == 4 {
		var bios []byte
		var biosErr error
		biosMediaID, bios, biosErr = s.readTwoROMFirmware(ctx, descriptor)
		if biosErr != nil {
			return coreLoadSource{}, biosErr
		}
		var transport corepackage.ROMTransport
		transport, err = corepackage.PrepareROMInputV2(ctx, corepackage.ROMInputV2{Package: base, BIOS: bios, Cartridge: rom, Expansion: asset, Parts: videoParts})
		data, partsComposition = transport.Data, transport.PartsComposition
	} else {
		var transport corepackage.ROMTransport
		input := corepackage.ROMInput{Package: base, ROM: rom, Expansion: asset, SlotExpansions: slotCards}
		input.InitialMedia = initial
		input.Parts = videoParts
		transport, err = corepackage.PrepareROMInput(ctx, input)
		data, slotComposition, partsComposition = transport.Data, transport.SlotComposition, transport.PartsComposition
		if err != nil && len(slotCards) != 0 {
			return coreLoadSource{}, slotExpansionUnavailable()
		}
	}
	// A part that cannot link with the shell is a video part fault, not a ROM one.
	if err != nil && len(videoParts) != 0 {
		if _, composeErr := corepackage.ComposePartsArchive(ctx, base, videoParts); composeErr != nil {
			return coreLoadSource{}, videoPartUnavailable()
		}
	}
	if err != nil {
		return coreLoadSource{}, romAdmissionError()
	}
	source := coreLoadSource{size: int64(len(data)), body: bytes.NewReader(data), entry: &entry, romID: selected.ROMID, romMediaID: selected.MediaID, slotComposition: slotComposition, partsComposition: partsComposition}
	source.initialMedia = initial
	if descriptor.Format == 3 && descriptor.Core.ID == "fes.atari-st" {
		source.activationBudget = 300 * time.Second
	}
	if descriptor.Format == 4 {
		source.biosID = descriptor.ROMs[0].ID
		source.biosMediaID = biosMediaID
		source.biosSourceSize = descriptor.ROMs[0].SourceSize
		source.romMapSHA256 = descriptor.ROMMap.SHA256
		source.romSourceSize = descriptor.ROMs[1].SourceSize
	} else {
		source.romMapSHA256 = descriptor.ROM.SHA256
		source.romSourceSize = descriptor.ROM.SourceSize
	}
	if asset != nil {
		source.expansionID = asset.ID
	}
	return source, nil
}

func (s *Service) readTwoROMFirmware(ctx context.Context, descriptor corepackage.Descriptor) (string, []byte, error) {
	store, ok := s.catalog.(coreFirmwareCatalog)
	if !ok || descriptor.Format != 4 || len(descriptor.ROMs) != 2 || descriptor.ROMs[0].Role != "firmware" {
		return "", nil, romAdmissionError()
	}
	slot, err := store.CoreFirmware(ctx, protocol.FirmwareRole)
	if err != nil {
		return "", nil, mapCoreFirmwareError(err)
	}
	if slot.MediaID == "" || slot.Size != descriptor.ROMs[0].SourceSize || protocol.ValidateDigest(slot.MediaID) != nil {
		return "", nil, romAdmissionError()
	}
	object, reader, err := store.OpenCoreMedia(ctx, slot.MediaID)
	if err != nil {
		return "", nil, mapCoreFirmwareError(err)
	}
	defer reader.Close()
	if object.MediaID != slot.MediaID || object.Size != slot.Size {
		return "", nil, romAdmissionError()
	}
	data, err := io.ReadAll(io.LimitReader(reader, slot.Size+1))
	if err != nil || int64(len(data)) != slot.Size || fmt.Sprintf("%x", sha256.Sum256(data)) != slot.MediaID {
		return "", nil, romAdmissionError()
	}
	return slot.MediaID, data, nil
}

// The target computes linked and composed identities. The host binds the
// returned receipt to its selected source identities, not a host-built image.
func (s coreLoadSource) matchesLoadedIdentity(status *protocol.CorePackageStatus) bool {
	if s.initialMedia != nil {
		u, ok := protocol.MediaUnit(status, s.initialMedia.Unit)
		if !ok || u.State != protocol.MediaUnitReady || status.PersistenceMode != "persistent" || u.Persistence == nil || u.Persistence.Mode != "persistent" || u.Persistence.GameID != s.initialMedia.GameID || u.Persistence.BaseMediaID != s.initialMedia.BaseMediaID {
			return false
		}
	}
	if s.machine != nil {
		// The kit bound the disk the host sent to the entry's game, or left
		// the drive empty when it sent none.
		u, ok := protocol.MediaUnit(status, protocol.AtaDiskUnit)
		if disk := s.machine.Disk; disk != nil {
			if !ok || u.Interface != protocol.AtaDiskInterface() || u.State != protocol.MediaUnitReady || status.PersistenceMode != "persistent" ||
				u.Persistence == nil || u.Persistence.Mode != "persistent" || u.Persistence.GameID != s.machine.GameID || u.Persistence.BaseMediaID != disk.SHA256 {
				return false
			}
		} else if ok && (u.Interface != protocol.AtaDiskInterface() || u.State != protocol.MediaUnitEmpty || u.Persistence != nil) {
			return false
		}
	}
	if !reflect.DeepEqual(status.PartsComposition, s.partsComposition) {
		return false
	}
	if s.partsComposition != nil && s.romID == "" {
		return status.Composition == nil && status.SlotComposition == nil && status.ROMLink == nil && status.ROMLinks == nil
	}
	if s.biosID != "" {
		links := status.ROMLinks
		if links == nil || len(links.Sources) != 2 || links.Sources[0].ID != s.biosID || links.Sources[0].Role != "firmware" || links.Sources[0].SourceSHA256 != s.biosMediaID || links.Sources[0].SourceSize != s.biosSourceSize || links.Sources[1].ID != s.romID || links.Sources[1].Role != "cartridge" || links.Sources[1].SourceSHA256 != s.romMediaID || links.Sources[1].SourceSize != s.romSourceSize || links.MapSHA256 != s.romMapSHA256 || status.PackageID != s.entry.PackageID {
			return false
		}
		if s.expansionID == "" {
			return status.Composition == nil
		}
		return status.Composition != nil && status.Composition.ExpansionID == s.expansionID
	}
	if !reflect.DeepEqual(status.SlotComposition, s.slotComposition) {
		return false
	}
	if s.romID == "" {
		return reflect.DeepEqual(status.Composition, s.composition)
	}
	link := status.ROMLink
	if link == nil || link.ROMID != s.romID || link.SourceSHA256 != s.romMediaID || link.MapSHA256 != s.romMapSHA256 || link.SourceSize != s.romSourceSize {
		return false
	}
	if s.expansionID == "" {
		return status.Composition == nil
	}
	return status.Composition != nil && status.Composition.ExpansionID == s.expansionID
}

// Cartridge linking supplies startup content before activation. A mailbox that
// holds the application in reset for a later media upload cannot also own it.
func validateROMMediaContract(descriptor corepackage.Descriptor) error {
	if descriptor.Format != 4 && (descriptor.ROM == nil || descriptor.ROM.Role != "cartridge") {
		return nil
	}
	application := descriptor.ABI.ID == "fes.application" && descriptor.ABI.Major == 1
	for _, endpoint := range descriptor.Interfaces {
		if endpoint.Major == 1 && endpoint.Minor == 0 && (endpoint.ID == "fes.media.blob-stream" || endpoint.ID == "fes.firmware.blob" || application && endpoint.ID == "fes.media.blob") {
			return &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Phase: "compatibility", Message: "linked cartridge packages must omit media endpoints that wait for a separate startup upload"}
		}
	}
	return nil
}
