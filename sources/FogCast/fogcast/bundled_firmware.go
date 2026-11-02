package fogcast

import (
	"bytes"
	"context"
	"log"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/bundledfirmware"
	"github.com/DeanoC/FogCast/protocol"
)

var bundledFirmwareLogf = log.Printf

// LoadBundledFirmware imports each valid object in dir into the core-media
// store and keeps it as the default for its core's named ROM. An object that
// fails validation or import is logged and skipped; it never stops the host.
// Import is idempotent: the media ID is the image's SHA-256.
func (s *Service) LoadBundledFirmware(ctx context.Context, dir string) {
	objects, problems := bundledfirmware.Load(dir)
	for _, problem := range problems {
		bundledFirmwareLogf("fogcast: skip %v", problem)
	}
	store, ok := s.catalog.(coreMediaCatalog)
	if !ok {
		return
	}
	var loaded []protocol.BundledFirmware
	for _, fw := range objects {
		media, _, err := store.ImportCoreMediaStream(ctx, fw.Size, bytes.NewReader(fw.Image))
		if err != nil || media.MediaID != fw.SHA256 || media.Size != fw.Size {
			bundledFirmwareLogf("fogcast: skip bundled firmware %s: import failed: %v", fw.ID, err)
			continue
		}
		item := protocol.BundledFirmware{
			ID: fw.ID, Name: fw.Name, Version: fw.Version, CoreID: fw.CoreID, ROMID: fw.ROMID,
			Size: fw.Size, MediaID: media.MediaID, Licence: fw.Licence, LicenceName: fw.LicenceName,
			LicenceText: fw.LicenceText, Notice: fw.Notice, OriginURL: fw.Origin.URL,
		}
		if fw.Source != nil {
			item.SourceURL, item.SourceSHA256 = fw.Source.URL, fw.Source.SHA256
		}
		loaded = append(loaded, item)
	}
	s.bundledFirmwareMu.Lock()
	s.bundledFirmware = loaded
	s.bundledFirmwareMu.Unlock()
}

// BundledFirmware lists the firmware this host selects by default.
func (s *Service) BundledFirmware(context.Context) (protocol.BundledFirmwareList, error) {
	s.bundledFirmwareMu.Lock()
	defer s.bundledFirmwareMu.Unlock()
	return protocol.BundledFirmwareList{Firmware: append([]protocol.BundledFirmware{}, s.bundledFirmware...)}, nil
}

func (s *Service) bundledFirmwareFor(coreID string, rom *corepackage.ROM) (protocol.BundledFirmware, bool) {
	if rom == nil || rom.Role != protocol.FirmwareRole {
		return protocol.BundledFirmware{}, false
	}
	s.bundledFirmwareMu.Lock()
	defer s.bundledFirmwareMu.Unlock()
	for _, fw := range s.bundledFirmware {
		if fw.CoreID == coreID && fw.ROMID == rom.ID && fw.Size == rom.SourceSize {
			return fw, true
		}
	}
	return protocol.BundledFirmware{}, false
}

// defaultBundledFirmware returns the current built-in object only while the
// title has no saved selection. Defaults are derived for reads and launches;
// saving one would make a later bundle update look like a user's choice.
func (s *Service) defaultBundledFirmware(ctx context.Context, entry catalog.CoreEntry, requirement *corepackage.ROM) (protocol.BundledFirmware, bool) {
	if requirement == nil || requirement.Role != protocol.FirmwareRole {
		return protocol.BundledFirmware{}, false
	}
	roms, ok := s.catalog.(coreROMCatalog)
	if !ok {
		return protocol.BundledFirmware{}, false
	}
	selected, err := roms.CoreEntryROM(ctx, entry.GameID)
	if err != nil || selected.MediaID != "" {
		return protocol.BundledFirmware{}, false
	}
	return s.bundledFirmwareFor(entry.CoreID, requirement)
}

func bundledFirmwareSelection(entry catalog.CoreEntry, requirement *corepackage.ROM, fw protocol.BundledFirmware) catalog.CoreEntryROM {
	return catalog.CoreEntryROM{
		GameID: entry.GameID, PackageID: entry.PackageID, ROMID: requirement.ID,
		MediaID: fw.MediaID, SourceSize: requirement.SourceSize,
	}
}
