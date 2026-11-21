package tenfoot

import (
	"context"
	"fmt"
	"sort"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/storage"
	"github.com/DeanoC/FogCast/ui/rooms"
)

// changeSessionDiskLocked puts one library disk of the running game's set in
// its drive: the core screen's Next disk. It uses the same live media change
// as the in-play picker, by media ID and the file name the disk was imported
// under, and leaves the room where it is.
func (a *App) changeSessionDiskLocked(mediaID, name string) {
	if !a.sessionLiveMediaOfferedLocked() {
		a.roomSessionNotice = "This running machine can't change disks from here."
		a.status = a.roomSessionNotice
		return
	}
	if a.tapePickerBusy || a.diskChangeBusy || a.client == nil {
		return
	}
	a.diskChangeBusy = true
	prior := a.session
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	a.status = "Changing the disk…"
	go func() {
		result, err := a.client.ReplaceRemovableMediaForSession(ctx, prior, mediaID, name)
		a.mu.Lock()
		defer a.mu.Unlock()
		a.diskChangeBusy = false
		if !samePlayHIDSession(a.session, prior) {
			return
		}
		if err != nil {
			if !protocol.LiveMediaNoChange(err) {
				a.session.MediaName = ""
				a.session.MediaEjected = false
			}
			a.roomSessionNotice = liveMediaStatusMessage(err)
			a.status = a.roomSessionNotice
			return
		}
		a.applySessionLocked(result)
		a.status = "Disk changed."
		if a.room != nil {
			a.room.Resume()
		}
	}()
}

// maxSetDisks matches the host's limit on one title's set, disk 1 included.
const maxSetDisks = 16

// diskSetMembers is the picked file's disk set from the folder it was picked
// in: every file whose name carries the same "(Disk N of M)" tag around the
// same name, ordered by disk number. It is nil when the name has no tag, the
// folder holds no other disk of it, two files claim one number, or the set
// is too large.
func diskSetMembers(rows []FirmwarePickerRow, path string) []FirmwarePickerRow {
	var key string
	found := false
	for _, row := range rows {
		if row.Kind == firmwarePickerKindFile && row.Path == path {
			var ok bool
			key, _, _, ok = protocol.DiskSetPosition(row.Name)
			found = ok
			break
		}
	}
	if !found {
		return nil
	}
	byIndex := map[int]FirmwarePickerRow{}
	for _, row := range rows {
		if row.Kind != firmwarePickerKindFile {
			continue
		}
		k, i, _, ok := protocol.DiskSetPosition(row.Name)
		if !ok || k != key {
			continue
		}
		if _, dup := byIndex[i]; dup {
			return nil
		}
		byIndex[i] = row
	}
	if len(byIndex) < 2 || len(byIndex) > maxSetDisks {
		return nil
	}
	indexes := make([]int, 0, len(byIndex))
	for i := range byIndex {
		indexes = append(indexes, i)
	}
	sort.Ints(indexes)
	out := make([]FirmwarePickerRow, 0, len(indexes))
	for _, i := range indexes {
		out = append(out, byIndex[i])
	}
	return out
}

// importedDisk is one disk of a set, imported into the library.
type importedDisk struct {
	MediaID, Name string
}

// importDiskFiles imports each file of a set in order. admit checks each
// file's name and size against the drive.
func (a *App) importDiskFiles(ctx context.Context, manager *storage.Manager, role string, members []FirmwarePickerRow, admit func(name string, size int64) error) ([]importedDisk, error) {
	out := make([]importedDisk, 0, len(members))
	for _, m := range members {
		f, size, name, err := a.openSetupMediaFile(ctx, manager, m.Path, role)
		if err != nil {
			return nil, err
		}
		if err := admit(name, size); err != nil {
			f.Close()
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		media, err := a.importSetupMedia(ctx, role, size, f)
		f.Close()
		if err != nil {
			return nil, err
		}
		out = append(out, importedDisk{MediaID: media.MediaID, Name: name})
	}
	return out, nil
}

// keepDiskSet records disk 1's file name and disks 2 to N on a title whose
// disk 1 is already selected.
func (a *App) keepDiskSet(ctx context.Context, entry hostclient.CoreEntry, disks []importedDisk) (hostclient.CoreEntry, error) {
	if len(disks) == 0 || entry.MediaID != disks[0].MediaID {
		return entry, fmt.Errorf("the game's first disk changed")
	}
	named, err := a.client.SetCoreEntryMediaName(ctx, entry.GameID, entry.PackageID, entry.MediaID, disks[0].Name)
	if err != nil {
		return entry, err
	}
	more := make([]hostclient.SetMedia, 0, len(disks)-1)
	for _, d := range disks[1:] {
		more = append(more, hostclient.SetMedia{MediaID: d.MediaID, MediaName: d.Name})
	}
	return a.client.SetCoreEntryMediaSet(ctx, named.GameID, named.PackageID, named.MediaID, setIDs(named.MoreMedia), more)
}

func setIDs(more []hostclient.SetMedia) []string {
	ids := make([]string, 0, len(more))
	for _, m := range more {
		ids = append(ids, m.MediaID)
	}
	return ids
}

// openAddDiskLocked opens the media picker to add one disk after the game's
// last. The game must be stopped and already have a disk or a tape.
func (a *App) openAddDiskLocked(action rooms.Action) {
	a.openSystemMediaModeLocked(action, true)
}

// addDiskFileLocked imports one file and adds it after the game's last disk.
func (a *App) addDiskFileLocked(path, role string, capability protocol.CoreMediaCapability) {
	s := &a.coreLibrary
	game, pkg, expected := s.HardwareGameID, s.HardwarePackageID, s.ExpectedMediaID
	manager := a.driveManagerLocked()
	var entry hostclient.CoreEntry
	a.coreLibraryAsyncLocked(func(ctx context.Context) error {
		disks, err := a.importDiskFiles(ctx, manager, role, []FirmwarePickerRow{{Path: path}}, func(name string, size int64) error {
			if !admitSetupMedia(capability, name, size) {
				return fmt.Errorf("choose %s of %d–%d bytes", mediaCapabilityLabel(capability), capability.MinBytes, capability.MaxBytes)
			}
			if !protocol.AdmitLiveMediaName(name) {
				return fmt.Errorf("choose a file named as a disk or tape")
			}
			return nil
		})
		if err != nil {
			return err
		}
		current, err := a.client.CoreEntry(ctx, game)
		if err != nil {
			return err
		}
		if current.PackageID != pkg || current.MediaID != expected || current.MediaRole != role {
			return fmt.Errorf("Machine setup changed. Close the picker and refresh the room.")
		}
		added := disks[0]
		if added.MediaID == current.MediaID {
			return fmt.Errorf("That disk is already this game's disk 1.")
		}
		more := append([]hostclient.SetMedia(nil), current.MoreMedia...)
		for _, m := range more {
			if m.MediaID == added.MediaID {
				return fmt.Errorf("That disk is already in this game's set.")
			}
		}
		more = append(more, hostclient.SetMedia{MediaID: added.MediaID, MediaName: added.Name})
		entry, err = a.client.SetCoreEntryMediaSet(ctx, game, pkg, current.MediaID, setIDs(current.MoreMedia), more)
		return err
	}, func() {
		a.systemMediaSavedLocked(entry)
		s.Status = fmt.Sprintf("Added as disk %d of %d. Back returns to the room.", len(entry.MoreMedia)+1, len(entry.MoreMedia)+1)
	})
}
