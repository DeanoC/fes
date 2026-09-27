package fogcast

import (
	"context"
	"strings"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

// SetupROM is a manifest requirement, not a claim of executor compatibility.
type SetupROM struct {
	ID         string `json:"id"`
	Role       string `json:"role"`
	SourceSize int64  `json:"source_size"`
	Binding    string `json:"binding"`
}
type CoreSetup struct {
	LibrarySourceID string                         `json:"library_source_id"`
	SourceID        string                         `json:"source_id"`
	CoreID          string                         `json:"core_id"`
	PackageID       string                         `json:"package_id"`
	Descriptor      corepackage.Descriptor         `json:"descriptor"`
	ROMs            []SetupROM                     `json:"roms"`
	Entries         []catalog.CoreEntry            `json:"entries"`
	Capabilities    protocol.CoreMediaCapabilities `json:"capabilities"`
	FirmwareMediaID string                         `json:"firmware_media_id,omitempty"`
}
type CoreSetupRequest struct {
	LibrarySourceID  string            `json:"library_source_id"`
	SourceID         string            `json:"source_id"`
	CoreID           string            `json:"core_id"`
	PackageID        string            `json:"package_id"`
	Title            string            `json:"title"`
	ROMs             map[string]string `json:"roms,omitempty"`
	MediaRole        string            `json:"media_role,omitempty"`
	MediaID          string            `json:"media_id,omitempty"`
	FirmwareRequired bool              `json:"firmware_required,omitempty"`
}

func (s *Service) CoreSetup(ctx context.Context, sourceID, coreID, packageID string) (CoreSetup, error) {
	c, err := s.availableCatalog()
	if err != nil {
		return CoreSetup{}, err
	}
	if sourceID != c.SourceID {
		return CoreSetup{}, canonicalError(protocol.CodeStaleRevision, nil)
	}
	found := false
	for _, e := range c.Entries {
		if e.CoreID == coreID && e.PackageID == packageID && packageID != "" {
			found = true
		}
	}
	if !found {
		return CoreSetup{}, canonicalError(protocol.CodeStaleRevision, nil)
	}
	p, _, err := s.readInstalledCore(ctx, packageID)
	if err != nil {
		return CoreSetup{}, err
	}
	if p.Descriptor.Core.ID != coreID {
		return CoreSetup{}, canonicalError(protocol.CodeInvalidArchive, nil)
	}
	result := CoreSetup{LibrarySourceID: s.coreLibrarySourceID, SourceID: sourceID, CoreID: coreID, PackageID: packageID, Descriptor: p.Descriptor, ROMs: []SetupROM{}, Entries: []catalog.CoreEntry{}}
	if r := p.Descriptor.ROM; r != nil {
		result.ROMs = append(result.ROMs, SetupROM{r.ID, r.Role, r.SourceSize, "entry"})
	}
	for i, r := range p.Descriptor.ROMs {
		binding := "entry"
		if i == 0 && r.Role == "firmware" {
			binding = "household-firmware"
		}
		result.ROMs = append(result.ROMs, SetupROM{r.ID, r.Role, r.SourceSize, binding})
	}
	entries, err := s.CoreEntries(ctx)
	if err != nil {
		return CoreSetup{}, err
	}
	for _, e := range entries {
		if e.CoreID == coreID {
			result.Entries = append(result.Entries, e)
		}
	}
	result.Capabilities, err = s.CoreMediaCapabilities(ctx, packageID)
	if err != nil {
		return CoreSetup{}, err
	}
	if len(p.Descriptor.ROMs) > 0 {
		f, err := s.CoreFirmware(ctx, protocol.FirmwareRole)
		if err != nil {
			return CoreSetup{}, err
		}
		result.FirmwareMediaID = f.MediaID
	}
	return result, nil
}

// Creation and retries only change the local library. Launch owns compatibility
// checks. ROM saves are separate durable writes; retry can finish an empty slot,
// but cannot replace an existing selection.
func (s *Service) CreateCoreSetupEntry(parent context.Context, req CoreSetupRequest) (catalog.CoreEntry, error) {
	ctx, cancel := serviceTimeout(parent, s.uploadTimeout)
	defer cancel()
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		return catalog.CoreEntry{}, err
	}
	defer release()
	if req.LibrarySourceID != s.coreLibrarySourceID || !validLibrarySourceID(req.LibrarySourceID) {
		return catalog.CoreEntry{}, canonicalError(protocol.CodeStaleRevision, nil)
	}
	setup, err := s.CoreSetup(ctx, req.SourceID, req.CoreID, req.PackageID)
	if err != nil {
		return catalog.CoreEntry{}, err
	}
	store, ok := s.catalog.(coreEntryCatalog)
	if !ok {
		return catalog.CoreEntry{}, canonicalError(protocol.CodeUnsupportedOperation, nil)
	}
	if err = s.validateCoreEntryMedia(ctx, setup.Descriptor, req.MediaRole, req.MediaID); err != nil {
		return catalog.CoreEntry{}, err
	}
	known := map[string]SetupROM{}
	for _, r := range setup.ROMs {
		known[r.ID] = r
	}
	for id, mediaID := range req.ROMs {
		r, ok := known[id]
		if !ok || protocol.ValidateDigest(mediaID) != nil {
			return catalog.CoreEntry{}, romAdmissionError()
		}
		m, err := s.CoreMedia(ctx, mediaID)
		if err != nil {
			return catalog.CoreEntry{}, err
		}
		if m.Size != r.SourceSize {
			return catalog.CoreEntry{}, romAdmissionError()
		}
		if r.Binding == "household-firmware" && mediaID != setup.FirmwareMediaID {
			return catalog.CoreEntry{}, canonicalError(protocol.CodeStaleRevision, nil)
		}
	}
	firmwareRequired := req.FirmwareRequired || len(setup.Descriptor.ROMs) > 0
	if req.FirmwareRequired && len(setup.Capabilities.Firmware) == 0 {
		return catalog.CoreEntry{}, canonicalError(protocol.CodeBadRequest, nil)
	}
	var entry catalog.CoreEntry
	for _, existing := range setup.Entries {
		if existing.Title == strings.TrimSpace(req.Title) {
			if existing.PackageID != req.PackageID || existing.MediaID != req.MediaID || existing.MediaRole != req.MediaRole || existing.FirmwareRequired != firmwareRequired {
				return catalog.CoreEntry{}, mapCoreEntryError(catalog.ErrCoreEntryConflict)
			}
			entry = existing
			break
		}
	}
	if entry.GameID == "" {
		if firmwareRequired {
			f, ok := s.catalog.(coreFirmwareCatalog)
			if !ok {
				return entry, canonicalError(protocol.CodeUnsupportedOperation, nil)
			}
			entry, err = f.CreateFirmwareRequiredEntry(ctx, req.Title, req.CoreID, req.PackageID, req.MediaRole, req.MediaID)
		} else if req.MediaID != "" {
			m, ok := s.catalog.(coreMediaCatalog)
			if !ok {
				return entry, canonicalError(protocol.CodeUnsupportedOperation, nil)
			}
			entry, err = m.CreateCoreMediaEntry(ctx, req.Title, req.CoreID, req.PackageID, req.MediaRole, req.MediaID)
		} else {
			entry, err = store.CreateCoreEntry(ctx, req.Title, req.CoreID, req.PackageID)
		}
		if err != nil {
			return entry, mapCoreEntryError(err)
		}
	}
	for _, r := range setup.ROMs {
		id := req.ROMs[r.ID]
		if id == "" || r.Binding != "entry" {
			continue
		}
		romStore, ok := s.catalog.(coreROMCatalog)
		if !ok {
			return entry, canonicalError(protocol.CodeUnsupportedOperation, nil)
		}
		current, err := romStore.CoreEntryROM(ctx, entry.GameID)
		if err != nil {
			return entry, romSelectionError(err)
		}
		if current.MediaID == id && current.PackageID == entry.PackageID && current.ROMID == r.ID {
			continue
		}
		if current.MediaID != "" {
			return entry, mapCoreEntryError(catalog.ErrCoreEntryConflict)
		}
		_, err = romStore.SelectCoreEntryROM(ctx, entry.GameID, entry.PackageID, r.ID, r.SourceSize, "", id)
		if err != nil {
			return entry, romSelectionError(err)
		}
	}
	return entry, nil
}
