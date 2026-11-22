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

// diskShelfState is one computer's disk shelf inside the library picker:
// the Atari ST's or the PC/XT's. The picker shows one level at a time: the
// shelf, one disk's actions, a confirmation, or the new-disk form.
type diskShelfState struct {
	CoreID    string
	PackageID string
	Loaded    bool
	CanMake   bool
	Sizes     []diskShelfSize
	// Contents are what a new disk can hold, the first the default: a
	// PC/XT disk can start FreeDOS when the host has it, or be blank.
	Contents    []diskShelfContents
	NewContents int
	Shelf       hostclient.DiskShelf
	Disk        *hostclient.ShelfDisk
	Confirm     string // "revert" or "delete"
	New         bool
	NewName     string
	NewSize     int
	NameFor     string // what the name being typed is for: "new", "copy", "rename", "copy-revert"
}

type diskShelfSize struct {
	Label                  string
	Tracks, Heads, Sectors int
}

type diskShelfContents struct {
	Label, Value string
}

// The ST sizes a person would ask for by name. The package says which it
// takes. A PC/XT disk is always 360K, shown so the limit is visible.
var diskShelfSizes = []diskShelfSize{
	{"720K", 80, 2, 9}, {"360K", 80, 1, 9}, {"800K", 80, 2, 10}, {"400K", 80, 1, 10},
}

var pcxtShelfSize = diskShelfSize{Label: "360K"}

// diskShelfFormat is the package media format each shelf's disks are.
var diskShelfFormat = map[string]string{
	hostclient.AtariStShelfCore: "atari-st-floppy",
	hostclient.PcxtShelfCore:    "pcxt-floppy",
}

func (s diskShelfSize) bytes() int64 {
	return int64(s.Tracks) * int64(s.Heads) * int64(s.Sectors) * 512
}

func diskShelfSizeLabel(size int64) string {
	if size == protocol.PcxtFloppyBytes {
		return pcxtShelfSize.Label
	}
	for _, s := range diskShelfSizes {
		if s.bytes() == size {
			return s.Label
		}
	}
	return fmt.Sprintf("%dK", size/1024)
}

// diskShelfError turns a host refusal into what to do about it. The host
// tells shelf refusals apart by code, not by message.
// diskShelfSlow follows the working line of a change that adds a library
// entry: a new disk, copy or rename took 25 to 35 s on Kit B.
const diskShelfSlow = " This can take half a minute."

func diskShelfError(err error) error {
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

func (a *App) openDiskShelfLocked(action rooms.Action) {
	if _, shelf := diskShelfFormat[action.CoreID]; a.client == nil || protocol.ValidateDigest(action.PackageID) != nil || !shelf {
		a.status = "Refresh the computer's screen."
		return
	}
	a.resetCoreLibraryLocked()
	s := &a.coreLibrary
	s.HardwareMode = "disk-shelf"
	s.HardwareCoreID, s.HardwarePackageID = action.CoreID, action.PackageID
	s.Shelf = diskShelfState{CoreID: action.CoreID, PackageID: action.PackageID}
	a.loadDiskShelfLocked("Looking at your disks…", "")
}

// loadDiskShelfLocked reads the shelf and returns to its top level. after is
// the line shown once it has loaded; empty picks one from the shelf.
func (a *App) loadDiskShelfLocked(during, after string) {
	s := &a.coreLibrary
	core, pkg := s.Shelf.CoreID, s.Shelf.PackageID
	s.Shelf = diskShelfState{CoreID: core, PackageID: pkg}
	s.Index = 0
	s.Status = during
	var shelf hostclient.DiskShelf
	var caps protocol.CoreMediaCapabilities
	freeDOS := false
	a.coreLibraryAsyncLocked(func(ctx context.Context) error {
		var err error
		if shelf, err = a.client.DiskShelf(ctx, core); err != nil {
			return diskShelfError(err)
		}
		if caps, err = a.client.PackageMediaCapabilities(ctx, pkg); err != nil {
			return diskShelfError(err)
		}
		if core == hostclient.PcxtShelfCore {
			// A host that can't say has no FreeDOS disk to copy.
			if disks, err := a.client.BundledDisks(ctx); err == nil {
				for _, d := range disks {
					freeDOS = freeDOS || d.CoreID == core
				}
			}
		}
		return nil
	}, func() {
		s.Online = true
		s.Shelf.Shelf, s.Shelf.Loaded = shelf, true
		for _, m := range caps.Media {
			if m.Role != protocol.DiskRole || m.Format != diskShelfFormat[core] {
				continue
			}
			if core == hostclient.PcxtShelfCore {
				s.Shelf.Sizes = []diskShelfSize{pcxtShelfSize}
				if freeDOS {
					s.Shelf.Contents = append(s.Shelf.Contents, diskShelfContents{"Starts FreeDOS", hostclient.ShelfContentsFreeDOS})
				}
				s.Shelf.Contents = append(s.Shelf.Contents, diskShelfContents{"Formatted and empty", hostclient.ShelfContentsBlank})
			} else {
				for _, size := range diskShelfSizes {
					if size.bytes() >= m.MinBytes && size.bytes() <= m.MaxBytes {
						s.Shelf.Sizes = append(s.Shelf.Sizes, size)
					}
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
		case len(shelf.Disks) == 0 && core == hostclient.PcxtShelfCore:
			s.Status = "A new disk can start FreeDOS, or be blank for your files. The files in your games folder are never changed."
		case len(shelf.Disks) == 0:
			s.Status = "A new disk is a blank disk to save your work on. The files in your games folder are never changed."
		default:
			s.Status = "Disks you made, and game disks that have changes saved on them."
		}
	})
}

func diskShelfDiskLine(d hostclient.ShelfDisk) string {
	parts := []string{d.Title, diskShelfSizeLabel(d.Size)}
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

func (a *App) diskShelfRowsLocked() []FirmwarePickerRow {
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
		rows := []FirmwarePickerRow{row("Name · "+name, "shelf-name"), row("Size · "+size, "shelf-size")}
		if len(st.Contents) > 0 {
			rows = append(rows, row("Contents · "+st.Contents[st.NewContents%len(st.Contents)].Label, "shelf-contents"))
		}
		return append(rows, row("Make the disk", "shelf-make"))
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
		rows = append(rows, row(diskShelfDiskLine(d), "shelf-disk:"+d.GameID))
	}
	return rows
}

func (a *App) diskShelfTitleLocked() string {
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

// diskShelfBackLocked steps out one level. It reports false at the top
// level, where Back closes the shelf.
func (a *App) diskShelfBackLocked() bool {
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

func (a *App) askDiskShelfNameLocked(purpose, start string) {
	a.coreLibrary.Shelf.NameFor = purpose
	a.settingsOSKKind = settingsOSKShelfName
	a.settingsOSKField = shared.TextField{Buffer: start}
	a.settingsOSKField.OSK.Reset()
}

func (a *App) handleDiskShelfLocked(row FirmwarePickerRow) {
	s := &a.coreLibrary
	st := &s.Shelf
	if id, ok := strings.CutPrefix(row.Kind, "shelf-disk:"); ok {
		for i := range st.Shelf.Disks {
			if st.Shelf.Disks[i].GameID == id {
				disk := st.Shelf.Disks[i]
				st.Disk = &disk
				s.Index = 0
				s.Status = diskShelfDiskStatus(disk)
			}
		}
		return
	}
	switch row.Kind {
	case "shelf-new":
		st.New, st.NewName, st.NewSize, st.NewContents = true, "", 0, 0
		s.Index = 0
		s.Status = "Name the disk and choose its size."
		if len(st.Contents) > 0 {
			s.Status = "Name the disk and choose what it holds. A PC/XT disk is 360K."
		}
	case "shelf-name":
		a.askDiskShelfNameLocked("new", st.NewName)
	case "shelf-size":
		if len(st.Sizes) > 0 {
			st.NewSize = (st.NewSize + 1) % len(st.Sizes)
		}
	case "shelf-contents":
		if len(st.Contents) > 0 {
			st.NewContents = (st.NewContents + 1) % len(st.Contents)
		}
	case "shelf-make":
		a.makeDiskShelfDiskLocked()
	case "shelf-cancel":
		a.diskShelfBackLocked()
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
			a.askDiskShelfNameLocked("copy", st.Disk.Title+" copy")
		}
	case "shelf-copy-revert":
		if st.Disk != nil {
			a.askDiskShelfNameLocked("copy-revert", st.Disk.Title+" copy")
		}
	case "shelf-rename":
		if st.Disk != nil {
			a.askDiskShelfNameLocked("rename", st.Disk.Title)
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
		a.confirmDiskShelfLocked()
	}
}

func diskShelfDiskStatus(d hostclient.ShelfDisk) string {
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

func (a *App) makeDiskShelfDiskLocked() {
	s := &a.coreLibrary
	st := &s.Shelf
	name := strings.TrimSpace(st.NewName)
	if name == "" {
		s.Status = "Give the disk a name first."
		return
	}
	size := diskShelfSizes[0]
	if st.NewSize < len(st.Sizes) {
		size = st.Sizes[st.NewSize]
	}
	core, pkg := st.CoreID, st.PackageID
	req := hostclient.NewShelfDiskRequest{Title: name, PackageID: pkg, Tracks: size.Tracks, Heads: size.Heads, Sectors: size.Sectors}
	if len(st.Contents) > 0 {
		req.Contents = st.Contents[st.NewContents%len(st.Contents)].Value
	}
	s.Status = "Making " + name + "…" + diskShelfSlow
	a.coreLibraryAsyncLocked(func(ctx context.Context) error {
		_, err := a.client.NewShelfDisk(ctx, core, req)
		if err != nil {
			return diskShelfError(err)
		}
		return nil
	}, func() { a.diskShelfChangedLocked("Made " + name + ".") })
}

// submitDiskShelfNameLocked takes the typed name for the pending action.
func (a *App) submitDiskShelfNameLocked() {
	name := strings.TrimSpace(a.settingsOSKField.Buffer)
	a.closeSettingsOSKLocked()
	s := &a.coreLibrary
	st := &s.Shelf
	purpose := st.NameFor
	st.NameFor = ""
	if !s.Open || s.HardwareMode != "disk-shelf" {
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
	disk, core := *st.Disk, st.CoreID
	if purpose == "rename" && name == disk.Title {
		return
	}
	var failed error
	switch purpose {
	case "rename":
		s.Status = "Renaming to " + name + "…" + diskShelfSlow
	case "copy-revert":
		s.Status = "Copying to " + name + ", then going back to the original…" + diskShelfSlow
	default:
		s.Status = "Copying to " + name + "…" + diskShelfSlow
	}
	a.coreLibraryAsyncLocked(func(ctx context.Context) error {
		var err error
		switch purpose {
		case "rename":
			_, err = a.client.RenameShelfDisk(ctx, core, disk.GameID, disk.KeptRevision, name)
		case "copy":
			_, err = a.client.CopyShelfDisk(ctx, core, disk.GameID, name)
		case "copy-revert":
			if _, err = a.client.CopyShelfDisk(ctx, core, disk.GameID, name); err == nil {
				if _, err = a.client.RevertShelfDisk(ctx, core, disk.GameID, disk.KeptRevision); err != nil {
					failed = errors.New("Copied to " + name + ", but " + disk.Title + " still has its changes. " + diskShelfError(err).Error())
					return nil
				}
			}
		}
		if err != nil {
			failed = diskShelfError(err)
		}
		return nil
	}, func() {
		if failed != nil {
			a.diskShelfChangedLocked(failed.Error())
			return
		}
		switch purpose {
		case "rename":
			a.diskShelfChangedLocked("Renamed to " + name + ".")
		case "copy-revert":
			a.diskShelfChangedLocked("Copied to " + name + ". " + disk.Title + " is back to its original.")
		default:
			a.diskShelfChangedLocked("Copied to " + name + ".")
		}
	})
}

func (a *App) confirmDiskShelfLocked() {
	st := &a.coreLibrary.Shelf
	if st.Disk == nil {
		return
	}
	disk, action, core := *st.Disk, st.Confirm, st.CoreID
	var failed error
	if action == "delete" {
		a.coreLibrary.Status = "Deleting " + disk.Title + "…"
	} else {
		a.coreLibrary.Status = "Going back to the original…"
	}
	a.coreLibraryAsyncLocked(func(ctx context.Context) error {
		var err error
		if action == "delete" {
			err = a.client.DeleteShelfDisk(ctx, core, disk.GameID, disk.KeptRevision)
		} else {
			_, err = a.client.RevertShelfDisk(ctx, core, disk.GameID, disk.KeptRevision)
		}
		if err != nil {
			failed = diskShelfError(err)
		}
		return nil
	}, func() {
		if failed != nil {
			a.diskShelfChangedLocked(failed.Error())
			return
		}
		if action == "delete" {
			a.diskShelfChangedLocked("Deleted " + disk.Title + ".")
		} else {
			a.diskShelfChangedLocked(disk.Title + " is back to its original.")
		}
	})
}

// diskShelfChangedLocked shows the shelf again after a change, and lets the
// core screen behind it read the library the change touched. A refused or
// half-done change comes here too, so the list is never older than the
// refusal that asked the person to look again.
func (a *App) diskShelfChangedLocked(done string) {
	if a.room != nil {
		a.room.Resume()
	}
	a.reloadLocked()
	a.loadDiskShelfLocked(done, done)
}
