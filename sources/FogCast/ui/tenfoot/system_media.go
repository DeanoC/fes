package tenfoot

import (
	"context"
	"fmt"
	"io"
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
	if cap.Role == protocol.HardDiskRole && !protocol.AdmitAtaDiskSize(size) {
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

func (a *App) openSetupMediaFile(ctx context.Context, manager *storage.Manager, path, role string) (io.ReadCloser, int64, string, error) {
	if role == protocol.HardDiskRole {
		return openHardDiskPickerFile(ctx, manager, a.client, path)
	}
	return openPickerFile(ctx, manager, a.client, path, storage.MaxFileBytes)
}

func (a *App) importSetupMedia(ctx context.Context, role string, size int64, content io.Reader) (hostclient.CoreMedia, error) {
	if role == protocol.HardDiskRole {
		return a.client.ImportHardDiskMedia(ctx, size, content)
	}
	return a.client.ImportCoreMedia(ctx, size, content)
}

func (a *App) openSystemMediaLocked(action rooms.Action) {
	a.openSystemMediaModeLocked(action, false)
}

// openSystemMediaModeLocked opens the next-start media picker, or with
// addDisk the picker that adds a disk after the game's last.
func (a *App) openSystemMediaModeLocked(action rooms.Action, addDisk bool) {
	if a.client == nil || a.session.State == "active" || a.stopPhase == "stopping" || a.retryStopLock || a.launch.Phase == "launching" {
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
	s.AddDisk = addDisk
	s.Status = "Checking this machine's media…"
	var setup hostclient.CoreSetup
	var role string
	a.coreLibraryAsyncLocked(func(ctx context.Context) error {
		entry, err := a.client.CoreEntry(ctx, action.GameID)
		if err != nil {
			return err
		}
		if entry.PackageID != action.PackageID || entry.CoreID != action.CoreID || entry.MediaID != action.ExpectedMediaID {
			return fmt.Errorf("Machine setup changed. Close the picker and refresh the room.")
		}
		if addDisk {
			if entry.MediaRole != protocol.DiskRole && entry.MediaRole != protocol.CassetteRole {
				return fmt.Errorf("Choose this game's first disk or tape before adding another.")
			}
			if len(entry.MoreMedia)+1 >= maxSetDisks {
				return fmt.Errorf("This game already has %d %ss.", maxSetDisks, setWord(entry.MediaRole))
			}
			role = entry.MediaRole
		}
		setup, err = a.client.InstalledCoreSetup(ctx, action.PackageID)
		return err
	}, func() {
		s.Setup = &setup
		s.Online = true
		s.AddDiskRole = role
		s.Status = "Choose media for the next start. The running machine is unchanged."
		if addDisk {
			s.Status = "Choose the " + setWord(role) + " to add after this game's last one."
		}
	})
}

func (a *App) systemMediaRowsLocked() []FirmwarePickerRow {
	s := &a.coreLibrary
	if s.Setup == nil {
		return nil
	}
	rows := []FirmwarePickerRow{{Name: "No removable media", Kind: "clear-media", Selectable: true}}
	if s.AddDisk {
		rows = rows[:0]
	}
	for _, cap := range s.Setup.Capabilities.Media {
		// A disk added to a set goes in the drive the game's first disk is in.
		if s.AddDisk && cap.Role != s.AddDiskRole {
			continue
		}
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
	if s.AddDisk {
		a.addDiskFileLocked(path, role, *capability)
		return
	}
	game, pkg, expected := s.HardwareGameID, s.HardwarePackageID, s.ExpectedMediaID
	manager := a.driveManagerLocked()
	var entry hostclient.CoreEntry
	// A file named as one disk of a set brings the rest of its folder's set.
	var members []FirmwarePickerRow
	if role == protocol.DiskRole || role == protocol.CassetteRole {
		members = diskSetMembers(s.Files, path)
	}
	if members != nil {
		a.coreLibraryAsyncDisksLocked(len(members), func(ctx context.Context) error {
			disks, err := a.importDiskFiles(ctx, manager, role, members, func(name string, size int64) error {
				if !admitSetupMedia(*capability, name, size) {
					return fmt.Errorf("choose %s of %d–%d bytes", mediaCapabilityLabel(*capability), capability.MinBytes, capability.MaxBytes)
				}
				return nil
			})
			if err != nil {
				return err
			}
			if entry, err = a.client.SelectCoreEntryMedia(ctx, game, pkg, expected, role, disks[0].MediaID); err != nil {
				return err
			}
			entry, err = a.keepDiskSet(ctx, entry, disks)
			return err
		}, func() {
			a.systemMediaSavedLocked(entry)
			w := setWord(role)
			s.Status = fmt.Sprintf("All %d %ss of the set saved; %s 1 goes in at the next start. Back returns to the room.", len(members), w, w)
		})
		return
	}
	a.coreLibraryAsyncLocked(func(ctx context.Context) error {
		f, size, name, err := a.openSetupMediaFile(ctx, manager, path, role)
		if err != nil {
			return err
		}
		defer f.Close()
		if !admitSetupMedia(*capability, name, size) {
			return fmt.Errorf("Choose %s of %d–%d bytes", mediaCapabilityLabel(*capability), capability.MinBytes, capability.MaxBytes)
		}
		media, err := a.importSetupMedia(ctx, role, size, f)
		if err != nil {
			return err
		}
		entry, err = a.client.SelectCoreEntryMedia(ctx, game, pkg, expected, role, media.MediaID)
		if err != nil {
			return err
		}
		if capability.Format == "zx81-p" || capability.Format == "zx80-o" ||
			((role == protocol.DiskRole || role == protocol.CassetteRole) && protocol.AdmitLiveMediaName(name)) {
			// The filename is display text, and a disk set's Next disk needs
			// it to put this disk back. Older hosts may not expose the route,
			// or take only cassette names, so keep the selection either way.
			_, _ = a.client.SetCoreEntryMediaName(ctx, game, pkg, media.MediaID, name)
		}
		return nil
	}, func() { a.systemMediaSavedLocked(entry) })
}
