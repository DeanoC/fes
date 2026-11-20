package tenfoot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/rooms"
	"github.com/DeanoC/FogCast/ui/shared"
)

// stShelfState is the Atari ST disk shelf inside the library picker. The
// picker shows one level at a time: the shelf, one disk's actions, a
// confirmation, or the new-disk form.
type stShelfState struct {
	PackageID string
	Loaded    bool
	CanMake   bool
	Sizes     []stShelfSize
	Shelf     hostclient.AtariStShelf
	Disk      *hostclient.AtariStShelfDisk
	Confirm   string // "revert" or "delete"
	New       bool
	NewName   string
	NewSize   int
	NameFor   string // what the name being typed is for: "new", "copy", "rename", "copy-revert"
}

type stShelfSize struct {
	Label                  string
	Tracks, Heads, Sectors int
}

// The sizes a person would ask for by name. The package says which it takes.
var stShelfSizes = []stShelfSize{
	{"720K", 80, 2, 9}, {"360K", 80, 1, 9}, {"800K", 80, 2, 10}, {"400K", 80, 1, 10},
}

func (s stShelfSize) bytes() int64 {
	return int64(s.Tracks) * int64(s.Heads) * int64(s.Sectors) * 512
}

func stShelfSizeLabel(size int64) string {
	for _, s := range stShelfSizes {
		if s.bytes() == size {
			return s.Label
		}
	}
	return fmt.Sprintf("%dK", size/1024)
}

// stShelfError turns a host refusal into what to do about it. The host
// tells shelf refusals apart by code, not by message.
// stShelfSlow follows the working line of a change that adds a library
// entry: a new disk, copy or rename took 25 to 35 s on Kit B.
const stShelfSlow = " This can take half a minute."

func stShelfError(err error) error {
	var status *hostclient.HTTPStatusError
	if !errors.As(err, &status) {
		return errors.New("Couldn't reach the library. Please retry.")
	}
	text := status.Error()
	switch {
	case status.StatusCode == http.StatusConflict && strings.Contains(text, "STALE_REVISION"):
		return errors.New("That disk has changed since it was listed. Look again and retry.")
	case status.StatusCode == http.StatusConflict:
		return errors.New("Stop the computer first, then retry.")
	case status.StatusCode == http.StatusServiceUnavailable:
		return errors.New("Can't check the kit right now. Please retry.")
	case status.StatusCode == http.StatusNotFound && strings.Contains(text, "ROM_NOT_FOUND"):
		return errors.New("That disk is no longer in the library.")
	case status.StatusCode == http.StatusNotFound:
		return errors.New("This library doesn't have the disk shelf yet. Update FogCast on the host.")
	case strings.Contains(text, "UNSUPPORTED_OPERATION"):
		return errors.New("That can't be done with this disk.")
	case status.StatusCode == http.StatusBadRequest:
		return errors.New("That name is taken or can't be used. Choose another.")
	}
	return errors.New("Couldn't change the disk. Please retry.")
}

func (a *App) openStShelfLocked(action rooms.Action) {
	if a.client == nil || protocol.ValidateDigest(action.PackageID) != nil || action.CoreID != "fes.atari-st" {
		a.status = "Refresh the computer's screen."
		return
	}
	a.resetCoreLibraryLocked()
	s := &a.coreLibrary
	s.HardwareMode = "st-shelf"
	s.HardwareCoreID, s.HardwarePackageID = action.CoreID, action.PackageID
	s.Shelf = stShelfState{PackageID: action.PackageID}
	a.loadStShelfLocked("Looking at your disks…", "")
}

// loadStShelfLocked reads the shelf and returns to its top level. after is
// the line shown once it has loaded; empty picks one from the shelf.
func (a *App) loadStShelfLocked(during, after string) {
	s := &a.coreLibrary
	pkg := s.Shelf.PackageID
	s.Shelf = stShelfState{PackageID: pkg}
	s.Index = 0
	s.Status = during
	var shelf hostclient.AtariStShelf
	var caps protocol.CoreMediaCapabilities
	a.coreLibraryAsyncLocked(func(ctx context.Context) error {
		var err error
		if shelf, err = a.client.AtariStShelf(ctx); err != nil {
			return stShelfError(err)
		}
		if caps, err = a.client.PackageMediaCapabilities(ctx, pkg); err != nil {
			return stShelfError(err)
		}
		return nil
	}, func() {
		s.Online = true
		s.Shelf.Shelf, s.Shelf.Loaded = shelf, true
		for _, m := range caps.Media {
			if m.Role != protocol.DiskRole || m.Format != "atari-st-floppy" {
				continue
			}
			for _, size := range stShelfSizes {
				if size.bytes() >= m.MinBytes && size.bytes() <= m.MaxBytes {
					s.Shelf.Sizes = append(s.Shelf.Sizes, size)
				}
			}
			s.Shelf.CanMake = len(s.Shelf.Sizes) > 0
			break
		}
		switch {
		case after != "":
			s.Status = after
		case !shelf.KeptKnown:
			s.Status = "Can't check for saved changes right now, so only disks made here are listed."
		case len(shelf.Disks) == 0:
			s.Status = "A new disk is a blank disk to save your work on. The files in your games folder are never changed."
		default:
			s.Status = "Disks you made, and game disks that have changes saved on them."
		}
	})
}

func stShelfDiskLine(d hostclient.AtariStShelfDisk) string {
	parts := []string{d.Title, stShelfSizeLabel(d.Size)}
	if d.InDrive {
		parts = append(parts, "In drive A")
	}
	switch d.Kept {
	case hostclient.ShelfKeptChanges:
		if !d.Made {
			parts = append(parts, "Game disk with saved changes")
		}
	case hostclient.ShelfKeptDamaged:
		parts = append(parts, "Saved changes are damaged")
	}
	return strings.Join(parts, " · ")
}

func (a *App) stShelfRowsLocked() []FirmwarePickerRow {
	st := &a.coreLibrary.Shelf
	row := func(name, kind string) FirmwarePickerRow {
		return FirmwarePickerRow{Name: name, Kind: kind, Selectable: true}
	}
	if !st.Loaded {
		return nil
	}
	switch {
	case st.New:
		size := "720K"
		if st.NewSize < len(st.Sizes) {
			size = st.Sizes[st.NewSize].Label
		}
		name := st.NewName
		if name == "" {
			name = "choose a name"
		}
		return []FirmwarePickerRow{row("Name · "+name, "shelf-name"), row("Size · "+size, "shelf-size"), row("Make the disk", "shelf-make")}
	case st.Disk != nil && st.Confirm == "revert":
		rows := []FirmwarePickerRow{row("Keep the changes", "shelf-cancel")}
		if st.Disk.Kept == hostclient.ShelfKeptChanges {
			rows = append(rows, row("Copy to a new disk first…", "shelf-copy-revert"))
		}
		return append(rows, row("Go back to the original", "shelf-confirm"))
	case st.Disk != nil && st.Confirm == "delete":
		return []FirmwarePickerRow{row("Keep this disk", "shelf-cancel"), row("Delete this disk", "shelf-confirm")}
	case st.Disk != nil:
		d := st.Disk
		var rows []FirmwarePickerRow
		if !d.InDrive {
			rows = append(rows, row("Start the computer with this disk", "shelf-start"))
		}
		if d.Kept != hostclient.ShelfKeptDamaged && d.Kept != hostclient.ShelfKeptUnknown {
			rows = append(rows, row("Copy to a new disk…", "shelf-copy"))
		}
		if d.Made && d.Kept != hostclient.ShelfKeptUnknown && d.Kept != hostclient.ShelfKeptDamaged {
			rows = append(rows, row("Rename…", "shelf-rename"))
		}
		if d.Kept == hostclient.ShelfKeptChanges || d.Kept == hostclient.ShelfKeptDamaged {
			rows = append(rows, row("Go back to the original…", "shelf-revert"))
		}
		if d.Made && d.Kept != hostclient.ShelfKeptUnknown {
			rows = append(rows, row("Delete…", "shelf-delete"))
		}
		return rows
	}
	var rows []FirmwarePickerRow
	if st.CanMake {
		rows = append(rows, row("New disk…", "shelf-new"))
	}
	for _, d := range st.Shelf.Disks {
		rows = append(rows, row(stShelfDiskLine(d), "shelf-disk:"+d.GameID))
	}
	return rows
}

func (a *App) stShelfTitleLocked() string {
	st := &a.coreLibrary.Shelf
	switch {
	case st.New:
		return "New disk"
	case st.Disk != nil && st.Confirm == "revert":
		return "Go back to the original?"
	case st.Disk != nil && st.Confirm == "delete":
		return "Delete this disk?"
	case st.Disk != nil:
		return st.Disk.Title
	}
	return "Your disks"
}

// stShelfBackLocked steps out one level. It reports false at the top
// level, where Back closes the shelf.
func (a *App) stShelfBackLocked() bool {
	s := &a.coreLibrary
	st := &s.Shelf
	switch {
	case st.Confirm != "":
		st.Confirm = ""
		s.Status = ""
	case st.Disk != nil:
		st.Disk = nil
		s.Status = "Disks you made, and game disks that have changes saved on them."
	case st.New:
		st.New = false
		s.Status = ""
	default:
		return false
	}
	s.Index = 0
	return true
}

func (a *App) askStShelfNameLocked(purpose, start string) {
	a.coreLibrary.Shelf.NameFor = purpose
	a.settingsOSKKind = settingsOSKShelfName
	a.settingsOSKField = shared.TextField{Buffer: start}
	a.settingsOSKField.OSK.Reset()
}

func (a *App) handleStShelfLocked(row FirmwarePickerRow) {
	s := &a.coreLibrary
	st := &s.Shelf
	if id, ok := strings.CutPrefix(row.Kind, "shelf-disk:"); ok {
		for i := range st.Shelf.Disks {
			if st.Shelf.Disks[i].GameID == id {
				disk := st.Shelf.Disks[i]
				st.Disk = &disk
				s.Index = 0
				s.Status = stShelfDiskStatus(disk)
			}
		}
		return
	}
	switch row.Kind {
	case "shelf-new":
		st.New, st.NewName, st.NewSize = true, "", 0
		s.Index = 0
		s.Status = "Name the disk and choose its size."
	case "shelf-name":
		a.askStShelfNameLocked("new", st.NewName)
	case "shelf-size":
		if len(st.Sizes) > 0 {
			st.NewSize = (st.NewSize + 1) % len(st.Sizes)
		}
	case "shelf-make":
		a.makeStShelfDiskLocked()
	case "shelf-cancel":
		a.stShelfBackLocked()
	case "shelf-start":
		if st.Disk == nil {
			return
		}
		if a.room == nil {
			return
		}
		game := st.Disk.GameID
		s.Open = false
		s.Gen++
		a.room.Resume()
		// As the core screen starts a game: the entry's own package, unchanged.
		a.launchFromRoomWithPackageLocked(game, "", "")
	case "shelf-copy":
		if st.Disk != nil {
			a.askStShelfNameLocked("copy", st.Disk.Title+" copy")
		}
	case "shelf-copy-revert":
		if st.Disk != nil {
			a.askStShelfNameLocked("copy-revert", st.Disk.Title+" copy")
		}
	case "shelf-rename":
		if st.Disk != nil {
			a.askStShelfNameLocked("rename", st.Disk.Title)
		}
	case "shelf-revert":
		st.Confirm = "revert"
		s.Index = 0
		s.Status = "The saved changes on this disk will be lost. The original is kept."
		if st.Disk != nil && st.Disk.Kept == hostclient.ShelfKeptDamaged {
			s.Status = "The saved changes are damaged and can't be read. Going back removes them."
		}
	case "shelf-delete":
		st.Confirm = "delete"
		s.Index = 0
		s.Status = "The disk and everything saved on it will be removed."
	case "shelf-confirm":
		a.confirmStShelfLocked()
	}
}

func stShelfDiskStatus(d hostclient.AtariStShelfDisk) string {
	switch {
	case d.InDrive:
		return "This disk is in drive A. Stop the computer to change it."
	case d.Kept == hostclient.ShelfKeptUnknown:
		return "Can't check this disk's saved changes right now."
	case d.Made:
		return "A disk you made."
	}
	return "A game disk with saved changes. The original is kept beside them."
}

func (a *App) makeStShelfDiskLocked() {
	s := &a.coreLibrary
	st := &s.Shelf
	name := strings.TrimSpace(st.NewName)
	if name == "" {
		s.Status = "Give the disk a name first."
		return
	}
	size := stShelfSizes[0]
	if st.NewSize < len(st.Sizes) {
		size = st.Sizes[st.NewSize]
	}
	pkg := st.PackageID
	s.Status = "Making " + name + "…" + stShelfSlow
	a.coreLibraryAsyncLocked(func(ctx context.Context) error {
		_, err := a.client.NewAtariStShelfDisk(ctx, name, pkg, size.Tracks, size.Heads, size.Sectors)
		if err != nil {
			return stShelfError(err)
		}
		return nil
	}, func() { a.stShelfChangedLocked("Made " + name + ".") })
}

// submitStShelfNameLocked takes the typed name for the pending action.
func (a *App) submitStShelfNameLocked() {
	name := strings.TrimSpace(a.settingsOSKField.Buffer)
	a.closeSettingsOSKLocked()
	s := &a.coreLibrary
	st := &s.Shelf
	purpose := st.NameFor
	st.NameFor = ""
	if !s.Open || s.HardwareMode != "st-shelf" {
		return
	}
	if purpose == "new" {
		st.NewName = name
		return
	}
	if st.Disk == nil {
		return
	}
	if name == "" {
		s.Status = "Give the disk a name first."
		return
	}
	disk := *st.Disk
	if purpose == "rename" && name == disk.Title {
		return
	}
	var failed error
	switch purpose {
	case "rename":
		s.Status = "Renaming to " + name + "…" + stShelfSlow
	case "copy-revert":
		s.Status = "Copying to " + name + ", then going back to the original…" + stShelfSlow
	default:
		s.Status = "Copying to " + name + "…" + stShelfSlow
	}
	a.coreLibraryAsyncLocked(func(ctx context.Context) error {
		var err error
		switch purpose {
		case "rename":
			_, err = a.client.RenameAtariStShelfDisk(ctx, disk.GameID, disk.KeptRevision, name)
		case "copy":
			_, err = a.client.CopyAtariStShelfDisk(ctx, disk.GameID, name)
		case "copy-revert":
			if _, err = a.client.CopyAtariStShelfDisk(ctx, disk.GameID, name); err == nil {
				if _, err = a.client.RevertAtariStShelfDisk(ctx, disk.GameID, disk.KeptRevision); err != nil {
					failed = errors.New("Copied to " + name + ", but " + disk.Title + " still has its changes. " + stShelfError(err).Error())
					return nil
				}
			}
		}
		if err != nil {
			failed = stShelfError(err)
		}
		return nil
	}, func() {
		if failed != nil {
			a.stShelfChangedLocked(failed.Error())
			return
		}
		switch purpose {
		case "rename":
			a.stShelfChangedLocked("Renamed to " + name + ".")
		case "copy-revert":
			a.stShelfChangedLocked("Copied to " + name + ". " + disk.Title + " is back to its original.")
		default:
			a.stShelfChangedLocked("Copied to " + name + ".")
		}
	})
}

func (a *App) confirmStShelfLocked() {
	st := &a.coreLibrary.Shelf
	if st.Disk == nil {
		return
	}
	disk, action := *st.Disk, st.Confirm
	var failed error
	if action == "delete" {
		a.coreLibrary.Status = "Deleting " + disk.Title + "…"
	} else {
		a.coreLibrary.Status = "Going back to the original…"
	}
	a.coreLibraryAsyncLocked(func(ctx context.Context) error {
		var err error
		if action == "delete" {
			err = a.client.DeleteAtariStShelfDisk(ctx, disk.GameID, disk.KeptRevision)
		} else {
			_, err = a.client.RevertAtariStShelfDisk(ctx, disk.GameID, disk.KeptRevision)
		}
		if err != nil {
			failed = stShelfError(err)
		}
		return nil
	}, func() {
		if failed != nil {
			a.stShelfChangedLocked(failed.Error())
			return
		}
		if action == "delete" {
			a.stShelfChangedLocked("Deleted " + disk.Title + ".")
		} else {
			a.stShelfChangedLocked(disk.Title + " is back to its original.")
		}
	})
}

// stShelfChangedLocked shows the shelf again after a change, and lets the
// core screen behind it read the library the change touched. A refused or
// half-done change comes here too, so the list is never older than the
// refusal that asked the person to look again.
func (a *App) stShelfChangedLocked(done string) {
	if a.room != nil {
		a.room.Resume()
	}
	a.reloadLocked()
	a.loadStShelfLocked(done, done)
}
