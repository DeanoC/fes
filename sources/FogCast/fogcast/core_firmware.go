package fogcast

import (
	"context"
	"errors"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

// CoreCompositions reports title-level firmware readiness for catalog rows.
func (s *Service) CoreCompositions(ctx context.Context, ids []string) (map[string]protocol.CoreComposition, error) {
	out := make(map[string]protocol.CoreComposition, len(ids))
	entries, ok := s.catalog.(coreEntryCatalog)
	if !ok {
		return out, nil
	}
	for _, id := range ids {
		entry, err := entries.CoreEntry(ctx, id)
		if errors.Is(err, catalog.ErrCoreEntryNotFound) {
			continue
		}
		if err != nil {
			return nil, mapCoreEntryError(err)
		}
		comp := protocol.CoreComposition{FirmwareRequired: entry.FirmwareRequired}
		inspection, inspectErr := s.installedCoreInspection(ctx, entry.PackageID)
		if inspectErr == nil && inspection.Descriptor.Format == 5 && entry.FirmwareRequired {
			// A household firmware object cannot fill a sealed static base.
			out[id] = comp
			continue
		}
		// Only expansion and slot-card checks need the archive bytes.
		var base []byte
		var baseErr error
		loadBase := func() ([]byte, error) {
			if base == nil && baseErr == nil {
				_, base, baseErr = s.readInstalledCore(ctx, entry.PackageID)
			}
			return base, baseErr
		}
		if inspectErr == nil && inspection.Descriptor.Format == 4 {
			comp.FirmwareRequired = true
			comp.ROMRequired = true
			comp.ROMID = inspection.Descriptor.ROMs[1].ID
			comp.ROMRole = inspection.Descriptor.ROMs[1].Role
			comp.ROMSourceSize = inspection.Descriptor.ROMs[1].SourceSize
			selected, _, romErr := s.readCoreEntryROM(ctx, entry, inspection.Descriptor)
			comp.ROMMediaID = selected.MediaID
			comp.ROMReady = romErr == nil
			_, _, biosErr := s.readTwoROMFirmware(ctx, entry, inspection.Descriptor)
			comp.FirmwareReady = biosErr == nil
			if biosErr != nil {
				bios := inspection.Descriptor.ROMs[0]
				comp.ROMID, comp.ROMRole, comp.ROMSourceSize = bios.ID, bios.Role, bios.SourceSize
				if roms, ok := s.catalog.(coreROMCatalog); ok {
					binding, err := roms.CoreEntryROM(ctx, entry.GameID, bios.ID)
					if err != nil {
						return nil, romSelectionError(err)
					}
					comp.ROMMediaID = binding.MediaID
				}
			}
		} else if inspectErr == nil && inspection.Descriptor.ROM != nil {
			comp.ROMID = inspection.Descriptor.ROM.ID
			comp.ROMRole = inspection.Descriptor.ROM.Role
			comp.ROMSourceSize = inspection.Descriptor.ROM.SourceSize
			if inspection.Descriptor.ROM.Role == protocol.FirmwareRole {
				comp.FirmwareRequired = true
				if inspection.Descriptor.Format == 3 {
					if fw, ok := s.defaultBundledFirmware(ctx, entry, inspection.Descriptor.ROM); ok {
						// LoadBundledFirmware imports and verifies each immutable object
						// once at host startup. A library listing can use that result
						// without hashing the same image once per title on every page.
						comp.ROMMediaID = fw.MediaID
						comp.FirmwareReady = true
					} else {
						selected, _, romErr := s.readCoreEntryROM(ctx, entry, inspection.Descriptor)
						comp.ROMMediaID = selected.MediaID
						comp.FirmwareReady = romErr == nil
					}
				} else {
					selected, _, romErr := s.readCoreEntryROM(ctx, entry, inspection.Descriptor)
					comp.ROMMediaID = selected.MediaID
					comp.FirmwareReady = romErr == nil
				}
			} else {
				selected, _, romErr := s.readCoreEntryROM(ctx, entry, inspection.Descriptor)
				comp.ROMMediaID = selected.MediaID
				comp.ROMRequired = true
				comp.ROMReady = romErr == nil
			}
		}
		if expansions, ok := s.catalog.(coreExpansionCatalog); ok {
			selected, err := expansions.CoreEntryExpansion(ctx, id)
			if err != nil {
				return nil, expansionError(err)
			}
			comp.ExpansionID = selected.ExpansionID
			if selected.ExpansionID != "" {
				asset, assetErr := expansions.ReadCoreExpansion(ctx, selected.ExpansionID)
				comp.ExpansionReady = assetErr == nil && inspectErr == nil && asset.Manifest.ShellPackageID == entry.PackageID && asset.Manifest.ShellBuildID == inspection.Descriptor.Build.ID && asset.Manifest.ShellSHA256 == inspection.Descriptor.Payload.SHA256
				if comp.ExpansionReady {
					data, err := loadBase()
					if err == nil && inspection.Descriptor.Format == 5 {
						err = validateExpansionSelection(inspection, data, asset)
					} else if err == nil {
						err = corepackage.ValidateExpansionArchive(data, asset)
					}
					comp.ExpansionReady = err == nil
				}
			}
		}
		if inspectErr == nil {
			slots, err := s.slotCompositionFor(ctx, entry, inspection, loadBase)
			if err != nil {
				return nil, err
			}
			comp.SlotExpansions = slots
		}
		if inspection.Descriptor.Format == 4 && inspectErr == nil {
			out[id] = comp
			continue
		}
		if inspectErr == nil && inspection.Descriptor.ROM != nil && inspection.Descriptor.ROM.Role == protocol.FirmwareRole {
			out[id] = comp
			continue
		}
		if !comp.FirmwareRequired {
			comp.FirmwareReady = true
			out[id] = comp
			continue
		}
		// A legacy package requiring the retired household slot cannot launch.
		comp.FirmwareReady = false
		out[id] = comp
	}
	return out, nil
}
