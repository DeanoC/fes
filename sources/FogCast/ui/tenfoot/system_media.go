package tenfoot

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/storage"
	"github.com/DeanoC/FogCast/ui/rooms"
)

func mediaCapabilityLabel(cap protocol.CoreMediaCapability) string {
	label := cap.Role
	if len(cap.Extensions) > 0 {
		label += " · " + strings.Join(cap.Extensions, " / ")
	}
	return label
}

func admitSetupMedia(cap protocol.CoreMediaCapability, name string, size int64) bool {
	if size < cap.MinBytes || size > cap.MaxBytes {
		return false
	}
	if len(cap.Extensions) == 0 {
		return true
	}
	ext := strings.ToLower(filepath.Ext(name))
	for _, allowed := range cap.Extensions {
		if ext == allowed {
			return true
		}
	}
	return false
}

func (a *App) openSystemMediaLocked(action rooms.Action) {
	if a.client == nil || a.session.State == "active" || hostclient.SessionKitLocal(a.session.State) || a.localLaunchBlockedLocked() || a.localCoreBusyLocked() || a.stopPhase == "stopping" || a.retryStopLock || a.launch.Phase == "launching" {
		a.status = "Stop the machine before choosing media for its next start."
		return
	}
	if protocol.ValidateGameID(action.GameID) != nil || protocol.ValidateDigest(action.PackageID) != nil {
		a.status = "Refresh the machine setup."
		return
	}
	a.resetCoreLibraryLocked()
	s := &a.coreLibrary
	s.HardwareMode = "system-media"
	s.HardwareGameID, s.HardwarePackageID, s.ExpectedMediaID = action.GameID, action.PackageID, action.ExpectedMediaID
	s.Status = "Checking this machine's media…"
	var setup hostclient.CoreSetup
	a.coreLibraryAsyncLocked(func(ctx context.Context) error {
		entry, err := a.client.CoreEntry(ctx, action.GameID)
		if err != nil {
			return err
		}
		if entry.PackageID != action.PackageID || entry.CoreID != action.CoreID || entry.MediaID != action.ExpectedMediaID {
			return fmt.Errorf("Machine setup changed. Close the picker and refresh the room.")
		}
		setup, err = a.client.InstalledCoreSetup(ctx, action.PackageID)
		return err
	}, func() {
		s.Setup = &setup
		s.Online = true
		s.Status = "Choose media for the next start. The running machine is unchanged."
	})
}

func (a *App) systemMediaRowsLocked() []FirmwarePickerRow {
	s := &a.coreLibrary
	if s.Setup == nil {
		return nil
	}
	rows := []FirmwarePickerRow{{Name: "No removable media", Kind: "clear-media", Selectable: true}}
	for _, cap := range s.Setup.Capabilities.Media {
		rows = append(rows, FirmwarePickerRow{Name: "Import " + mediaCapabilityLabel(cap) + "…", Kind: "media:" + cap.Role, Selectable: true})
	}
	return rows
}

func (a *App) handleSystemMediaLocked(row FirmwarePickerRow) {
	if row.Kind == "clear-media" {
		a.saveSystemMediaLocked("", "")
		return
	}
	if !strings.HasPrefix(row.Kind, "media:") {
		return
	}
	s := &a.coreLibrary
	s.PickID = row.Kind
	s.Path = ""
	s.Index = 0
	s.Files = a.storagePickerRootsLocked("media")
	a.refreshStorageRootsLocked()
	s.Status = "Choose a file from this launcher or a saved media drive."
}

func (a *App) saveSystemMediaLocked(role, id string) {
	s := &a.coreLibrary
	game, pkg, expected := s.HardwareGameID, s.HardwarePackageID, s.ExpectedMediaID
	var entry hostclient.CoreEntry
	a.coreLibraryAsyncLocked(func(ctx context.Context) error {
		var err error
		entry, err = a.client.SelectCoreEntryMedia(ctx, game, pkg, expected, role, id)
		return err
	}, func() { a.systemMediaSavedLocked(entry) })
}

func (a *App) systemMediaSavedLocked(entry hostclient.CoreEntry) {
	s := &a.coreLibrary
	s.ExpectedMediaID = entry.MediaID
	s.PickID, s.Path = "", ""
	s.Files = nil
	s.Index = 0
	s.Status = "Media saved for the next start. Back returns to the room."
	if a.room != nil {
		a.room.Resume()
	}
	a.reloadLocked()
}

func (a *App) importSystemMediaLocked(path string) {
	s := &a.coreLibrary
	if s.Setup == nil || !s.Online {
		return
	}
	role := strings.TrimPrefix(s.PickID, "media:")
	var capability *protocol.CoreMediaCapability
	for _, cap := range s.Setup.Capabilities.Media {
		if cap.Role == role {
			copy := cap
			capability = &copy
			break
		}
	}
	if capability == nil {
		return
	}
	game, pkg, expected := s.HardwareGameID, s.HardwarePackageID, s.ExpectedMediaID
	manager := a.driveManagerLocked()
	var entry hostclient.CoreEntry
	a.coreLibraryAsyncLocked(func(ctx context.Context) error {
		f, size, name, err := openPickerFile(ctx, manager, a.client, path, storage.MaxFileBytes)
		if err != nil {
			return err
		}
		defer f.Close()
		if !admitSetupMedia(*capability, name, size) {
			return fmt.Errorf("Choose %s of %d–%d bytes", mediaCapabilityLabel(*capability), capability.MinBytes, capability.MaxBytes)
		}
		media, err := a.client.ImportCoreMedia(ctx, size, f)
		if err != nil {
			return err
		}
		entry, err = a.client.SelectCoreEntryMedia(ctx, game, pkg, expected, role, media.MediaID)
		if err != nil {
			return err
		}
		if capability.Format == "zx81-p" || capability.Format == "zx80-o" {
			// The filename is display text. Older hosts may not expose the
			// optional route, so keep the successful media selection either way.
			_, _ = a.client.SetCoreEntryMediaName(ctx, game, pkg, media.MediaID, name)
		}
		return nil
	}, func() { a.systemMediaSavedLocked(entry) })
}
