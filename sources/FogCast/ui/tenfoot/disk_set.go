package tenfoot

import (
	"context"
	"fmt"
	"sort"
	"time"

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
		a.roomSessionNotice = "This running machine can't change its media from here."
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
	a.status = "Putting in " + name + "…"
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
		a.status = name + " is in."
		if a.room != nil {
			a.room.Resume()
		}
	}()
}

// setWord names one medium of a set in its drive's terms: "disk", or "tape"
// for a cassette deck.
func setWord(role string) string {
	if role == protocol.CassetteRole {
		return "tape"
	}
	return "disk"
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
		return entry, fmt.Errorf("the game's first %s changed", setWord(entry.MediaRole))
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
		added, word := disks[0], setWord(role)
		if added.MediaID == current.MediaID {
			return fmt.Errorf("That %s is already this game's %s 1.", word, word)
		}
		more := append([]hostclient.SetMedia(nil), current.MoreMedia...)
		for _, m := range more {
			if m.MediaID == added.MediaID {
				return fmt.Errorf("That %s is already in this game's set.", word)
			}
		}
		more = append(more, hostclient.SetMedia{MediaID: added.MediaID, MediaName: added.Name})
		entry, err = a.client.SetCoreEntryMediaSet(ctx, game, pkg, current.MediaID, setIDs(current.MoreMedia), more)
		return err
	}, func() {
		a.systemMediaSavedLocked(entry)
		s.Status = fmt.Sprintf("Added as %s %d of %d. Back returns to the room.", setWord(role), len(entry.MoreMedia)+1, len(entry.MoreMedia)+1)
	})
}

// setDiskRows lists a game's disk set for the running machine's media
// picker: "Disk 2 of 3 · NAME", the one in the drive marked. next is the
// index of the disk after the one in the drive. A disk with no file name
// cannot be put in by name and is left out.
func setDiskRows(entry hostclient.CoreEntry, inDrive string) (rows []TapePickerRow, next int) {
	if len(entry.MoreMedia) == 0 || (entry.MediaRole != protocol.DiskRole && entry.MediaRole != protocol.CassetteRole) {
		return nil, -1
	}
	word := "Disk"
	if entry.MediaRole == protocol.CassetteRole {
		word = "Tape"
	}
	disks := append([]hostclient.SetMedia{{MediaID: entry.MediaID, MediaName: entry.MediaName}}, entry.MoreMedia...)
	current := 0
	if inDrive != "" {
		current = -1
		for i, d := range disks {
			if d.MediaName == inDrive {
				current = i
			}
		}
	}
	next = -1
	for i, d := range disks {
		if d.MediaName == "" || !protocol.AdmitLiveMediaName(d.MediaName) {
			continue
		}
		label := fmt.Sprintf("%s %d of %d · %s", word, i+1, len(disks), d.MediaName)
		if i == current {
			label += " · In the drive"
		}
		if current >= 0 && i == (current+1)%len(disks) {
			next = len(rows)
		}
		rows = append(rows, TapePickerRow{Name: label, Kind: tapePickerKindSetDisk, Selectable: true, SetMediaID: d.MediaID, SetMediaName: d.MediaName})
	}
	return rows, next
}

// fetchSetDisks reads the running game's set and offers its disks at the top
// of the media picker, with focus on the next one while the person has not
// moved yet.
func (a *App) fetchSetDisks(parent context.Context, gen int, session hostclient.SessionResult) {
	if session.GameID == "" || a.client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	entry, err := a.client.CoreEntry(ctx, session.GameID)
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil || !a.tapePickerOpen || a.tapePickerGen != gen {
		return
	}
	rows, next := setDiskRows(entry, a.session.MediaName)
	if len(rows) == 0 {
		return
	}
	a.tapePickerSet = rows
	if !a.tapePickerAtRoots {
		return
	}
	at := 0
	if len(a.tapePickerRows) > 0 && a.tapePickerRows[0].Kind == tapePickerKindEject {
		at = 1
	}
	moved := a.tapePickerIndex != 0
	a.tapePickerRows = append(a.tapePickerRows[:at], append(append([]TapePickerRow(nil), rows...), a.tapePickerRows[at:]...)...)
	if moved {
		a.tapePickerIndex += len(rows)
	} else if next >= 0 {
		a.tapePickerIndex = at + next
	}
}

// startSetDiskArmLocked puts one disk of the running game's set in its drive
// from the media picker, by media ID and file name.
func (a *App) startSetDiskArmLocked(row TapePickerRow) {
	if a.tapePickerBusy || a.client == nil || !a.sessionMediaOfferedLocked() {
		return
	}
	a.tapePickerBusy = true
	a.tapePickerGen++
	gen, prior := a.tapePickerGen, a.tapePickerSession
	a.tapePickerStatus = "Putting in " + row.SetMediaName + "…"
	a.status = a.tapePickerStatus
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		result, err := a.client.ReplaceRemovableMediaForSession(ctx, prior, row.SetMediaID, row.SetMediaName)
		a.finishTapeArm(gen, prior, result, true, err)
	}()
}
