package tenfoot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/zx81family"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/storage"
	"github.com/DeanoC/FogCast/ui/shared"
)

type coreLibraryState struct {
	Open, Busy, Online                                     bool
	Cancel                                                 context.CancelFunc
	Gen, Index                                             int
	Cores                                                  []hostclient.AvailableCore
	Ref                                                    *hostclient.CoreReference
	Setup                                                  *hostclient.CoreSetup
	ROMs                                                   map[string]string
	Label, Title, Status, Path, PickID, MediaID, MediaRole string
	Files                                                  []FirmwarePickerRow
	FilterCoreID, HardwareMode                             string
	HardwareGameID, HardwarePackageID                      string
	ExpectedMediaID                                        string
	InstalledSetup                                         bool
	SetupFamilyPicker                                      bool
	SystemExpansions                                       systemExpansionState
	ExpansionFamily                                        int
	Tapes                                                  []hostclient.HardwareTape
}

func (a *App) coreLibraryRowsLocked() []FirmwarePickerRow {
	s := &a.coreLibrary
	if s.PickID != "" {
		return s.Files
	}
	if s.HardwareMode == "system-media" {
		return a.systemMediaRowsLocked()
	}
	if s.HardwareMode == "system-expansions" {
		return a.systemExpansionRowsLocked()
	}
	if s.HardwareMode != "" {
		return a.hardwarePickerRowsLocked()
	}
	rows := []FirmwarePickerRow{}
	add := func(name, kind string) {
		rows = append(rows, FirmwarePickerRow{Name: name, Kind: kind, Selectable: true})
	}
	if s.Ref == nil {
		if s.InstalledSetup && s.SetupFamilyPicker && s.FilterCoreID == "" {
			for _, variant := range zx81family.Variants() {
				add(variant.Label+" · "+variant.Detail, "variant:"+variant.CoreID)
			}
			return rows
		}
		add("Refresh systems", "refresh")
		if s.InstalledSetup {
			add("Browse published systems", "published")
		}
		for _, r := range s.Cores {
			if s.FilterCoreID != "" && r.CoreID != s.FilterCoreID {
				continue
			}
			label := r.Label + " · " + r.Standing + " · " + r.ArtifactState
			if strings.TrimSpace(r.VideoInventoryError) != "" {
				label += " · " + r.VideoInventoryError
			}
			add(label, "system")
			rows[len(rows)-1].Core = &r
		}
		return rows
	}
	if s.Setup == nil {
		add("Install core", "install")
		return rows
	}
	for _, r := range s.Setup.ROMs {
		status := "choose file"
		if s.ROMs[r.ID] != "" {
			status = "selected"
		}
		add(fmt.Sprintf("%s · %s · %d bytes · %s", r.ID, r.Role, r.SourceSize, status), "rom:"+r.ID)
		if r.Binding == "household-firmware" && s.ROMs[r.ID] != "" && s.ROMs[r.ID] != s.Setup.FirmwareMediaID {
			add("Use selected BIOS for household", "firmware:"+r.ID)
		}
	}
	blankDisk := false
	for _, m := range s.Setup.Capabilities.Media {
		if m.Role == protocol.DiskRole && m.Format == "atari-st-floppy" && !blankDisk {
			blankDisk = true
			status := "720 KiB"
			if s.MediaRole == protocol.DiskRole && s.MediaID != "" {
				status = "selected"
			}
			add("Blank disk · "+status, "blank-disk")
		}
		if m.Role == "blob" || m.Role == "disk" || m.Role == protocol.CassetteRole {
			status := "choose file"
			if s.MediaRole == m.Role && s.MediaID != "" {
				status = "selected"
			}
			if m.Role == "blob" && protocol.RequiresCoreMedia(s.Setup.Descriptor) {
				status = "required · " + status
			}
			add(mediaCapabilityLabel(m)+" · "+status, "media:"+m.Role)
		}
	}
	add("Title · "+s.Title, "title")
	add("Add game to library", "create")
	return rows
}
func (a *App) coreLibrarySnapshotLocked() FirmwarePickerSnapshot {
	s := &a.coreLibrary
	if !s.Open {
		return FirmwarePickerSnapshot{}
	}
	title := "Systems"
	if s.HardwareMode == "system-media" {
		title = "Media for next start"
	}
	if s.HardwareMode == "system-expansions" {
		title = "Expansion for next start"
	}
	if s.HardwareMode == "tapes" {
		title = "Cassette for next start"
	}
	if s.HardwareMode == "expansions" {
		title = "Import expansion"
	}
	if s.InstalledSetup && s.Ref == nil {
		if s.FilterCoreID == "" {
			title = "ZX81 family"
		} else if variant, ok := zx81family.ByID(s.FilterCoreID); ok {
			title = variant.Label
		} else {
			title = s.FilterCoreID
		}
	}
	if s.Ref != nil {
		title = "Set up " + s.Label
	}
	if s.PickID != "" {
		title = "Choose " + strings.TrimPrefix(strings.TrimPrefix(s.PickID, "rom:"), "media:")
	}
	return FirmwarePickerSnapshot{Open: true, Title: title, Path: a.pickerDisplayPathLocked(s.Path), Rows: a.coreLibraryRowsLocked(), Index: s.Index, Busy: s.Busy, Status: s.Status, Hint: selectWord(a.affinity.current.Kind) + " choose  " + backWord(a.affinity.current.Kind) + " back"}
}
func (a *App) openCoreLibraryLocked() {
	a.resetCoreLibraryLocked()
	a.refreshCoreLibraryLocked()
}

func (a *App) resetCoreLibraryLocked() {
	if a.coreLibrary.Cancel != nil {
		a.coreLibrary.Cancel()
		a.coreLibrary.Cancel = nil
	}
	a.coreLibrary.Gen++
	a.coreLibrary.Busy = false
	a.closeSettingsLocked()
	a.closeDetailLocked()
	a.closeFirmwarePickerLocked()
	a.closeTapePickerLocked()
	a.coreLibrary.InstalledSetup = false
	a.coreLibrary.SetupFamilyPicker = false
	a.coreLibrary.HardwareMode = ""
	a.coreLibrary.FilterCoreID = ""
	a.coreLibrary.Open = true
	a.coreLibrary.Index = 0
	a.coreLibrary.Ref = nil
	a.coreLibrary.Setup = nil
	a.coreLibrary.PickID = ""
}
func (a *App) coreLibraryAsyncLocked(work func(context.Context) error, done func()) {
	s := &a.coreLibrary
	if s.Busy {
		s.Status = "Please wait for the current library action to finish."
		return
	}
	s.Busy = true
	s.Gen++
	gen := s.Gen
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	s.Cancel = cancel
	go func() {
		defer cancel()
		err := work(ctx)
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.coreLibrary.Gen != gen || !a.coreLibrary.Open {
			return
		}
		a.coreLibrary.Busy = false
		a.coreLibrary.Cancel = nil
		if err != nil {
			a.coreLibrary.Status = err.Error()
			return
		}
		done()
	}()
}
func (a *App) refreshCoreLibraryLocked() {
	a.coreLibrary.Online = false
	a.coreLibrary.Setup = nil
	a.coreLibrary.Ref = nil
	a.coreLibrary.Index = 0
	a.coreLibrary.Status = "Loading systems…"
	var rows []hostclient.AvailableCore
	a.coreLibraryAsyncLocked(func(ctx context.Context) error { var err error; rows, err = a.client.AvailableCores(ctx); return err }, func() {
		a.coreLibrary.Cores = rows
		a.coreLibrary.Online = true
		a.coreLibrary.Status = "Choose a system. Adding a game does not launch it."
	})
}
func (a *App) loadCoreSetupLocked(install bool) {
	ref := *a.coreLibrary.Ref
	installedSetup := a.coreLibrary.InstalledSetup
	var setup hostclient.CoreSetup
	a.coreLibraryAsyncLocked(func(ctx context.Context) error {
		if install {
			if err := a.client.InstallAvailableCore(ctx, ref); err != nil {
				return err
			}
		}
		var err error
		if installedSetup {
			setup, err = a.client.InstalledCoreSetup(ctx, ref.PackageID)
		} else {
			setup, err = a.client.CoreSetup(ctx, ref.SourceID, ref.CoreID, ref.PackageID)
		}
		if err == nil && !installedSetup && (setup.LibrarySourceID != ref.LibrarySourceID || setup.PackageID != ref.PackageID || setup.SourceID != ref.SourceID || setup.CoreID != ref.CoreID) {
			return fmt.Errorf("System changed; refresh and choose again")
		}
		return err
	}, func() {
		if install {
			for i := range a.coreLibrary.Cores {
				row := &a.coreLibrary.Cores[i]
				if row.LibrarySourceID == ref.LibrarySourceID && row.SourceID == ref.SourceID && row.CoreID == ref.CoreID && row.PackageID == ref.PackageID {
					row.ArtifactState = "installed"
				}
			}
		}
		a.coreLibrary.Setup = &setup
		a.coreLibrary.ROMs = map[string]string{}
		for _, r := range setup.ROMs {
			if r.Binding == "household-firmware" && setup.FirmwareMediaID != "" {
				a.coreLibrary.ROMs[r.ID] = setup.FirmwareMediaID
			}
		}
		a.coreLibrary.Index = 0
		a.coreLibrary.Status = "Choose the required ROMs, then add the game."
	})
}
func (a *App) handleCoreLibraryLocked(cmd Command) {
	s := &a.coreLibrary
	if s.Busy {
		if cmd == CmdBack || cmd == CmdHome || cmd == CmdSettings {
			if s.Cancel != nil {
				s.Cancel()
				s.Cancel = nil
			}
			s.Gen++
			s.Open = false
			s.Busy = false
		}
		return
	}
	if cmd == CmdBack || cmd == CmdHome || cmd == CmdSettings {
		if s.PickID != "" {
			s.PickID = ""
			s.Files = nil
			s.Path = ""
			s.Index = 0
			return
		}
		if s.Ref != nil {
			s.Ref = nil
			s.Setup = nil
			s.Index = 0
			return
		}
		if s.InstalledSetup && s.SetupFamilyPicker && s.FilterCoreID != "" {
			s.FilterCoreID = ""
			s.Index = 0
			s.Online = true
			s.Status = "Choose a ZX81-family machine…"
			s.Cores = nil
			return
		}
		s.Open = false
		s.Gen++
		if a.room != nil {
			a.room.Resume()
		}
		return
	}
	rows := a.coreLibraryRowsLocked()
	n := len(rows)
	if n == 0 {
		return
	}
	if s.Index < 0 || s.Index >= n {
		s.Index = 0
	}
	switch cmd {
	case CmdUp, CmdLeft, CmdTabPrev:
		s.Index = (s.Index + n - 1) % n
		return
	case CmdDown, CmdRight, CmdTab:
		s.Index = (s.Index + 1) % n
		return
	case CmdSelect:
	default:
		return
	}
	row := rows[s.Index]
	if s.PickID != "" {
		a.coreLibraryFileLocked(row)
		return
	}
	if s.HardwareMode == "system-media" {
		a.handleSystemMediaLocked(row)
		return
	}
	if s.HardwareMode == "system-expansions" {
		a.handleSystemExpansionLocked(row)
		return
	}
	if s.HardwareMode != "" {
		a.handleHardwarePickerLocked(row)
		return
	}
	if row.Kind == "published" {
		variant := s.FilterCoreID
		a.openCoreLibraryLocked()
		if variant != "" {
			a.coreLibrary.FilterCoreID = variant
		}
		return
	}
	if strings.HasPrefix(row.Kind, "variant:") {
		id := strings.TrimPrefix(row.Kind, "variant:")
		if _, ok := zx81family.ByID(id); !ok {
			return
		}
		s.FilterCoreID = id
		s.Index = 0
		a.refreshInstalledCoreSetupLocked()
		return
	}
	if row.Kind == "refresh" {
		if s.InstalledSetup {
			a.refreshInstalledCoreSetupLocked()
			return
		}
		a.refreshCoreLibraryLocked()
		return
	}
	if !s.Online {
		s.Status = "Source unavailable. Refresh systems to enable setup."
		return
	}
	if row.Kind == "system" {
		if row.Core == nil {
			s.Status = "System changed; refresh and choose again"
			return
		}
		r := *row.Core
		if r.PackageID == "" || r.ArtifactState == "unavailable" || r.ArtifactState == "unproduced" {
			s.Status = "No installable package has been published for this system."
			return
		}
		if strings.TrimSpace(r.VideoInventoryError) != "" {
			s.Status = r.VideoInventoryError
			return
		}
		ref := r.CoreReference
		s.Ref = &ref
		s.Setup = nil
		s.Index = 0
		s.ROMs = map[string]string{}
		s.Title = r.Label
		s.Label = r.Label
		s.MediaID = ""
		s.MediaRole = ""
		if r.ArtifactState == "installed" {
			a.loadCoreSetupLocked(false)
		}
		return
	}
	if row.Kind == "install" {
		a.loadCoreSetupLocked(true)
		return
	}
	if strings.HasPrefix(row.Kind, "rom:") || strings.HasPrefix(row.Kind, "media:") {
		s.PickID = row.Kind
		s.Path = ""
		s.Files = a.storagePickerRootsLocked(a.corePickerPurposeLocked())
		a.refreshStorageRootsLocked()
		s.Index = 0
		return
	}
	if strings.HasPrefix(row.Kind, "firmware:") {
		id := strings.TrimPrefix(row.Kind, "firmware:")
		media := s.ROMs[id]
		a.coreLibraryAsyncLocked(func(ctx context.Context) error { _, err := a.client.SelectHouseholdFirmware(ctx, media); return err }, func() { s.Setup.FirmwareMediaID = media; s.Status = "Household BIOS selected explicitly." })
		return
	}
	if row.Kind == "blank-disk" {
		a.createBlankAtariStDiskLocked()
		return
	}
	if row.Kind == "title" {
		a.settingsOSKKind = settingsOSKCoreTitle
		a.settingsOSKField = shared.TextField{Buffer: s.Title}
		a.settingsOSKField.OSK.Reset()
		return
	}
	if row.Kind == "create" {
		a.createCoreLibraryGameLocked()
	}
}
func (a *App) coreLibraryFileLocked(row FirmwarePickerRow) {
	s := &a.coreLibrary
	if row.Kind == firmwarePickerKindOSK {
		a.settingsOSKKind = settingsOSKCorePath
		a.settingsOSKField = shared.TextField{Buffer: s.Path}
		a.settingsOSKField.OSK.Reset()
		a.settingsOSKField.OSK.CyclePage(1)
		return
	}
	if row.Kind == firmwarePickerKindDir || row.Kind == firmwarePickerKindRoot || row.Kind == firmwarePickerKindParent {
		a.coreLibraryDirLocked(row.Path)
		return
	}
	if row.Kind == firmwarePickerKindFile {
		a.importCoreLibraryFileLocked(row.Path)
	}
}
func (a *App) coreLibraryDirLocked(path string) {
	if path == "" {
		a.coreLibrary.Path = ""
		a.coreLibrary.Files = a.storagePickerRootsLocked(a.corePickerPurposeLocked())
		a.coreLibrary.Index = 0
		return
	}
	if _, _, _, ok := splitDrivePath(path); ok {
		a.loadDrivePickerDirLocked(path, "core")
		return
	}
	rows, err := listFirmwarePickerDir(path)
	if err != nil {
		a.coreLibrary.Status = err.Error()
		return
	}
	for i := range rows {
		rows[i].Selectable = true
	}
	a.coreLibrary.Files = rows
	a.coreLibrary.Path = filepath.Clean(path)
	a.coreLibrary.Index = 0
}
func (a *App) submitCoreLibraryOSKLocked() {
	kind := a.settingsOSKKind
	value := strings.TrimSpace(a.settingsOSKField.Buffer)
	a.closeSettingsOSKLocked()
	if kind == settingsOSKCoreTitle {
		a.coreLibrary.Title = value
		return
	}
	info, err := os.Stat(value)
	if err != nil {
		a.coreLibrary.Status = err.Error()
		return
	}
	if info.IsDir() {
		a.coreLibraryDirLocked(value)
		return
	}
	a.importCoreLibraryFileLocked(value)
}
func (a *App) importCoreLibraryFileLocked(path string) {
	s := &a.coreLibrary
	if s.HardwareMode == "system-media" {
		a.importSystemMediaLocked(path)
		return
	}
	if s.HardwareMode == "system-expansions" {
		a.importSystemExpansionLocked(path)
		return
	}
	if s.HardwareMode != "" {
		a.importHardwareFileLocked(path)
		return
	}
	if !s.Online || s.Setup == nil {
		s.Status = "Refresh and choose an installed system."
		return
	}
	pick := s.PickID
	capabilities := append([]protocol.CoreMediaCapability(nil), s.Setup.Capabilities.Media...)
	min, max := int64(0), int64(0)
	for _, r := range s.Setup.ROMs {
		if pick == "rom:"+r.ID {
			min = r.SourceSize
			max = r.SourceSize
		}
	}
	for _, m := range s.Setup.Capabilities.Media {
		if pick == "media:"+m.Role {
			min = m.MinBytes
			max = m.MaxBytes
		}
	}
	if max < 1 || max > 32<<20 {
		s.Status = "Unsupported media requirement"
		return
	}
	manager := a.driveManagerLocked()
	var media hostclient.CoreMedia
	a.coreLibraryAsyncLocked(func(ctx context.Context) error {
		f, size, name, err := openPickerFile(ctx, manager, a.client, path, storage.MaxFileBytes)
		if err != nil {
			return err
		}
		defer f.Close()
		if size < min || size > max {
			return fmt.Errorf("Choose a regular file of %d–%d bytes", min, max)
		}
		if strings.HasPrefix(pick, "media:") {
			for _, cap := range capabilities {
				if pick == "media:"+cap.Role && !admitSetupMedia(cap, name, size) {
					return fmt.Errorf("Choose %s of %d–%d bytes", mediaCapabilityLabel(cap), cap.MinBytes, cap.MaxBytes)
				}
			}
		}
		media, err = a.client.ImportCoreMedia(ctx, size, f)
		return err
	}, func() {
		if strings.HasPrefix(pick, "rom:") {
			s.ROMs[strings.TrimPrefix(pick, "rom:")] = media.MediaID
		} else {
			s.MediaRole = strings.TrimPrefix(pick, "media:")
			s.MediaID = media.MediaID
		}
		s.PickID = ""
		s.Path = ""
		s.Index = 0
		s.Files = nil
		s.Status = "File imported. Review selections before adding the game."
	})
}
func (a *App) createBlankAtariStDiskLocked() {
	s := &a.coreLibrary
	if !s.Online || s.Setup == nil {
		s.Status = "Refresh and choose an installed system."
		return
	}
	var media hostclient.BlankAtariStDisk
	a.coreLibraryAsyncLocked(func(ctx context.Context) error {
		var err error
		media, err = a.client.CreateBlankAtariStDisk(ctx, hostclient.BlankAtariStRequest{Name: "WORK"})
		return err
	}, func() {
		s.MediaRole = protocol.DiskRole
		s.MediaID = media.MediaID
		s.Status = "Blank 720 KiB disk stored. Review selections before adding the game."
	})
}
func (a *App) createCoreLibraryGameLocked() {
	s := &a.coreLibrary
	if s.Ref == nil || s.Setup == nil || !s.Online {
		return
	}
	if strings.TrimSpace(s.Title) == "" {
		s.Status = "Enter a game title."
		return
	}
	if protocol.RequiresCoreMedia(s.Setup.Descriptor) && !(s.Setup.Descriptor.ROM != nil && s.Setup.Descriptor.ROM.Role == "cartridge") && (s.MediaRole != "blob" || s.MediaID == "") {
		s.Status = "Choose required blob media."
		return
	}
	roms := map[string]string{}
	for _, r := range s.Setup.ROMs {
		id := s.ROMs[r.ID]
		if id == "" {
			s.Status = "Choose " + r.ID
			return
		}
		if r.Binding == "household-firmware" && id != s.Setup.FirmwareMediaID {
			s.Status = "Explicitly select this BIOS for the household first."
			return
		}
		roms[r.ID] = id
	}
	installedSetup, setup := s.InstalledSetup, *s.Setup
	req := hostclient.CoreSetupRequest{CoreReference: *s.Ref, Title: s.Title, ROMs: roms, MediaRole: s.MediaRole, MediaID: s.MediaID}
	a.coreLibraryAsyncLocked(func(ctx context.Context) error {
		if installedSetup {
			_, err := a.client.CreateInstalledCoreEntry(ctx, setup, req.Title, roms, req.MediaRole, req.MediaID)
			return err
		}
		_, err := a.client.CreateCoreSetupEntry(ctx, req)
		return err
	}, func() {
		s.Status = "Game added to the library. Launch checks readiness and executor compatibility."
		if installedSetup {
			s.Status = "Setup added to the library."
			if a.room != nil {
				a.room.Resume()
			}
		}
		a.reloadLocked()
	})
}
