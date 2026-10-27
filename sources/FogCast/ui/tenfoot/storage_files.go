package tenfoot

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/storage"
)

func drivePath(host bool, id, path string) string {
	prefix := "local-drive:"
	if host {
		prefix = "host-drive:"
	}
	return prefix + id + "/" + strings.TrimPrefix(filepath.ToSlash(filepath.Clean(path)), "./")
}
func splitDrivePath(path string) (host bool, id, rel string, ok bool) {
	var rest string
	if strings.HasPrefix(path, "host-drive:") {
		host = true
		rest = strings.TrimPrefix(path, "host-drive:")
	} else if strings.HasPrefix(path, "local-drive:") {
		rest = strings.TrimPrefix(path, "local-drive:")
	} else {
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	id = parts[0]
	rel = "."
	if len(parts) == 2 {
		rel = parts[1]
	}
	ok = len(id) == 24 && strings.Trim(id, "0123456789abcdef") == ""
	return
}
func (a *App) driveManagerLocked() *storage.Manager {
	if a.driveManager == nil {
		path := a.prefsPath
		if path == "" {
			path = defaultPrefsPath()
		}
		config := ""
		if path != "" {
			config = filepath.Join(filepath.Dir(path), "drives.json")
		}
		a.driveManager = storage.NewMemorySMBCredentials(config)
	}
	return a.driveManager
}
func (a *App) refreshStorageRootsLocked() {
	manager, client := a.driveManagerLocked(), a.client
	a.driveRootsGen++
	gen := a.driveRootsGen
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		local, _ := manager.List(ctx, false)
		var remote []storage.Drive
		if client != nil {
			inventory, err := client.StorageDrives(ctx, false)
			if err == nil {
				remote = inventory.Drives
			}
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		if gen != a.driveRootsGen {
			return
		}
		a.localDrives, a.hostDrives = local, remote
		if a.firmwarePickerOpen && a.firmwarePickerAtRoots && !a.firmwarePickerBusy {
			a.refreshFirmwarePickerRootsLocked(a.firmwarePickerStatus)
		}
		if a.tapePickerOpen && a.tapePickerAtRoots && !a.tapePickerBusy {
			a.refreshTapePickerRootsLocked(a.tapePickerStatus)
		}
		if a.coreLibrary.Open && a.coreLibrary.PickID != "" && a.coreLibrary.Path == "" && !a.coreLibrary.Busy {
			a.coreLibrary.Files = a.storagePickerRootsLocked(a.corePickerPurposeLocked())
		}
	}()
}

func retainPickerSelection[T any](previous, refreshed []T, index int, same func(T, T) bool) ([]T, int) {
	if index < 0 || index >= len(previous) {
		return refreshed, 0
	}
	selected := previous[index]
	for i, row := range refreshed {
		if same(selected, row) {
			return refreshed, i
		}
	}
	// Keep the previous view if its highlighted row disappeared during refresh.
	// A pending Select must never be redirected to a different action.
	return previous, index
}

func (a *App) refreshFirmwarePickerRootsLocked(status string) {
	previous, index := a.firmwarePickerRows, a.firmwarePickerIndex
	a.showFirmwarePickerRootsLocked(status)
	a.firmwarePickerRows, a.firmwarePickerIndex = retainPickerSelection(
		previous, a.firmwarePickerRows, index,
		func(a, b FirmwarePickerRow) bool {
			return a.Kind == b.Kind && a.Path == b.Path && a.Name == b.Name
		},
	)
}

func (a *App) refreshTapePickerRootsLocked(status string) {
	previous, index := a.tapePickerRows, a.tapePickerIndex
	a.showTapePickerRootsLocked(status)
	a.tapePickerRows, a.tapePickerIndex = retainPickerSelection(
		previous, a.tapePickerRows, index,
		func(a, b TapePickerRow) bool {
			if a.Kind != b.Kind || a.Path != b.Path || a.Name != b.Name {
				return false
			}
			if a.Kind == tapePickerKindStarter {
				return a.Starter != nil && b.Starter != nil && a.Starter.ID == b.Starter.ID
			}
			return true
		},
	)
}

func (a *App) corePickerPurposeLocked() string {
	if strings.HasPrefix(a.coreLibrary.PickID, "rom:") {
		return "bios"
	}
	return "media"
}
func (a *App) storagePickerRootsLocked(purpose string) []FirmwarePickerRow {
	home, _ := os.UserHomeDir()
	rows := firmwarePickerRoots(home, a.hostSettings.Libraries)
	for _, source := range []struct {
		host   bool
		drives []storage.Drive
		label  string
	}{{false, a.localDrives, "This device"}, {true, a.hostDrives, "FogCast host"}} {
		for _, drive := range source.drives {
			rel := drive.MediaFolder
			if purpose == "bios" {
				rel = drive.BIOSFolder
			}
			rows = append(rows, FirmwarePickerRow{Name: source.label + " · " + drive.Label, Path: drivePath(source.host, drive.ID, rel), Kind: firmwarePickerKindRoot})
		}
	}
	return rows
}

// listDriveDir runs off the app lock; SMB connection and enumeration can wait.
func listDriveDir(ctx context.Context, manager *storage.Manager, client *Client, path string) ([]FirmwarePickerRow, error) {
	host, id, rel, ok := splitDrivePath(path)
	if !ok {
		return nil, fmt.Errorf("invalid drive path")
	}
	var entries []storage.Entry
	var err error
	if host {
		if client == nil {
			return nil, fmt.Errorf("FogCast host is unavailable")
		}
		entries, err = client.StorageEntries(ctx, id, rel)
	} else {
		entries, err = manager.Entries(ctx, id, rel)
	}
	if err != nil {
		return nil, err
	}
	parent := ""
	if rel != "" && rel != "." {
		parent = drivePath(host, id, filepath.Dir(rel))
	}
	rows := []FirmwarePickerRow{{Name: "..", Path: parent, Kind: firmwarePickerKindParent}}
	for _, e := range entries {
		kind := firmwarePickerKindFile
		name := e.Name
		if e.Directory {
			kind = firmwarePickerKindDir
			name += "/"
		}
		rows = append(rows, FirmwarePickerRow{Name: name, Path: drivePath(host, id, filepath.Join(rel, e.Name)), Kind: kind, Size: e.Size, Selectable: !e.Directory})
	}
	return rows, nil
}

func tapePickerDriveRows(p *hostclient.SessionCorePackage, rows []FirmwarePickerRow) []TapePickerRow {
	tapes := make([]TapePickerRow, 0, len(rows))
	for _, row := range rows {
		if row.Kind == tapePickerKindFile && !hostclient.AdmitRemovableMedia(p, row.Name, row.Size) {
			continue
		}
		tapes = append(tapes, TapePickerRow{Name: row.Name, Path: row.Path, Kind: row.Kind, Size: row.Size, Selectable: row.Kind == tapePickerKindFile})
	}
	return tapePickerFolderActions(tapes)
}

func (a *App) loadDrivePickerDirLocked(path, destination string) {
	manager, client := a.driveManagerLocked(), a.client
	gen := 0
	switch destination {
	case "firmware":
		a.firmwarePickerBusy = true
		a.firmwarePickerStatus = "Connecting to drive…"
		a.firmwarePickerGen++
		gen = a.firmwarePickerGen
	case "tape":
		a.tapePickerBusy = true
		a.tapePickerStatus = "Connecting to drive…"
		a.tapePickerGen++
		gen = a.tapePickerGen
	case "core":
		a.coreLibrary.Busy = true
		a.coreLibrary.Status = "Connecting to drive…"
		a.coreLibrary.Gen++
		gen = a.coreLibrary.Gen
	case "storage":
		a.driveSettings.Busy = true
		a.driveSettings.Status = "Connecting to drive…"
		a.driveSettings.Gen++
		gen = a.driveSettings.Gen
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		rows, err := listDriveDir(ctx, manager, client, path)
		a.mu.Lock()
		defer a.mu.Unlock()
		switch destination {
		case "firmware":
			if !a.firmwarePickerOpen || a.firmwarePickerGen != gen {
				return
			}
			a.firmwarePickerBusy = false
			if err != nil {
				a.firmwarePickerStatus = err.Error()
				return
			}
			a.markFirmwarePickerRowsLocked(rows)
			a.firmwarePickerRows = rows
			a.firmwarePickerPath = path
			a.firmwarePickerAtRoots = false
			a.firmwarePickerIndex = 0
			a.firmwarePickerStatus = ""
		case "tape":
			if !a.tapePickerOpen || a.tapePickerGen != gen {
				return
			}
			a.tapePickerBusy = false
			if err != nil {
				a.tapePickerStatus = err.Error()
				return
			}
			a.tapePickerRows = tapePickerDriveRows(a.tapePickerSession.CorePackage, rows)
			a.tapePickerPath = path
			a.tapePickerAtRoots = false
			a.tapePickerIndex = 0
			a.tapePickerStatus = ""
		case "core":
			if !a.coreLibrary.Open || a.coreLibrary.Gen != gen {
				return
			}
			a.coreLibrary.Busy = false
			if err != nil {
				a.coreLibrary.Status = err.Error()
				return
			}
			a.coreLibrary.Files = rows
			a.coreLibrary.Path = path
			a.coreLibrary.Index = 0
			a.coreLibrary.Status = ""
		case "storage":
			s := &a.driveSettings
			if !s.Open || s.Gen != gen {
				return
			}
			s.Busy = false
			if err != nil {
				s.Status = err.Error()
				return
			}
			s.Path = path
			s.Files = rows
			s.Index = 0
			s.Status = "Choose a BIOS or media folder."
		}
	}()
}

func openPickerFile(ctx context.Context, manager *storage.Manager, client *Client, path string, maxBytes int64) (io.ReadCloser, int64, string, error) {
	if host, id, rel, ok := splitDrivePath(path); ok {
		var file io.ReadCloser
		var size int64
		var err error
		if host {
			if client == nil {
				return nil, 0, "", fmt.Errorf("FogCast host is unavailable")
			}
			file, size, err = client.OpenStorageFileWithLimit(ctx, id, rel, maxBytes)
		} else {
			file, size, err = manager.OpenWithLimit(ctx, id, rel, maxBytes)
		}
		return file, size, filepath.Base(rel), err
	}
	file, err := os.OpenFile(strings.TrimSpace(path), os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, 0, "", err
	}
	info, err := file.Stat()
	if err == nil && (!info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxBytes) {
		err = fmt.Errorf("choose a regular file of 1–%d bytes", maxBytes)
	}
	if err != nil {
		file.Close()
		return nil, 0, "", err
	}
	return file, info.Size(), filepath.Base(strings.TrimSpace(path)), nil
}

func (a *App) pickerDisplayPathLocked(path string) string {
	host, id, rel, ok := splitDrivePath(path)
	if !ok {
		return path
	}
	owner := "This device"
	drives := a.localDrives
	if host {
		owner = "FogCast host"
		drives = a.hostDrives
	}
	label := "Drive"
	for _, d := range drives {
		if d.ID == id {
			label = d.Label
			break
		}
	}
	if rel == "." {
		rel = ""
	}
	return owner + " · " + label + " /" + rel
}
