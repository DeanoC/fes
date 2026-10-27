package tenfoot

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/storage"
	"github.com/DeanoC/FogCast/ui/shared"
)

type driveSettingsState struct {
	Open, Busy, Host      bool
	Gen, Index            int
	Drives                []storage.Drive
	Draft                 *storage.Drive
	Editing               bool
	Path, Purpose, Status string
	Files                 []FirmwarePickerRow
}

func (a *App) driveSettingsRowsLocked() []FirmwarePickerRow {
	s := &a.driveSettings
	rows := []FirmwarePickerRow{}
	add := func(name, kind string) { rows = append(rows, FirmwarePickerRow{Name: name, Kind: kind}) }
	if s.Path != "" {
		add("Use this folder for "+strings.ToUpper(s.Purpose), "use-folder")
		for _, r := range s.Files {
			if r.Kind != firmwarePickerKindFile {
				rows = append(rows, r)
			}
		}
		return rows
	}
	if s.Draft != nil {
		d := s.Draft
		add("Name · "+d.Label, "label")
		if s.Editing {
			add("SMB share · "+d.Source, "source")
			add("Username · "+d.Username+" (empty for guest)", "username")
			add("Domain · "+d.Domain, "domain")
			password := "not set"
			if d.Password != "" || d.PasswordSet {
				password = "stored"
			}
			add("Password · "+password, "password")
			add("Save connection", "save")
		} else {
			add("Browse BIOS folder · "+driveFolderLabel(d.BIOSFolder), "bios")
			add("Browse media folder · "+driveFolderLabel(d.MediaFolder), "media")
			if d.Kind == "smb" {
				add("Edit SMB connection", "edit")
			}
			add("Forget this drive", "forget")
		}
		add("Back to drives", "back")
		return rows
	}
	owner := "This device"
	if s.Host {
		owner = "FogCast host"
	}
	add("Drives on · "+owner+" · select to switch", "owner")
	add("Refresh USB drives and saved shares", "refresh")
	add("Add SMB share", "add-smb")
	for _, d := range s.Drives {
		state := "saved"
		if !d.Saved {
			state = "select to remember"
		}
		add(d.Label+" · "+strings.ToUpper(d.Kind)+" · "+state, "drive:"+d.ID)
	}
	add("Back to settings", "close")
	return rows
}
func driveFolderLabel(path string) string {
	if path == "" || path == "." {
		return "/"
	}
	return path
}
func (a *App) driveSettingsSnapshotLocked() FirmwarePickerSnapshot {
	s := &a.driveSettings
	if !s.Open {
		return FirmwarePickerSnapshot{}
	}
	title := "BIOS & media drives · this device"
	if s.Host {
		title = "BIOS & media drives · FogCast host"
	}
	return FirmwarePickerSnapshot{Open: true, Title: title, Path: a.pickerDisplayPathLocked(s.Path), Rows: a.driveSettingsRowsLocked(), Index: s.Index, Status: s.Status, Busy: s.Busy, Hint: selectWord(a.affinity.current.Kind) + " select  " + backWord(a.affinity.current.Kind) + " back"}
}
func (a *App) openDriveSettingsLocked() {
	gen := a.driveSettings.Gen + 1
	a.driveSettings = driveSettingsState{Open: true, Gen: gen}
	a.refreshDriveSettingsLocked()
}
func (a *App) closeDriveSettingsLocked() {
	a.driveSettings.Open = false
	a.driveSettings.Gen++
	a.driveSettings.Busy = false
}
func (a *App) driveSettingsAsyncLocked(work func(context.Context) error, done func()) {
	s := &a.driveSettings
	if s.Busy {
		return
	}
	s.Busy = true
	s.Gen++
	gen := s.Gen
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := work(ctx)
		a.mu.Lock()
		defer a.mu.Unlock()
		if !a.driveSettings.Open || a.driveSettings.Gen != gen {
			return
		}
		a.driveSettings.Busy = false
		if err != nil {
			a.driveSettings.Status = err.Error()
			return
		}
		a.driveSettings.Status = ""
		done()
	}()
}
func (a *App) refreshDriveSettingsLocked() {
	s := &a.driveSettings
	host := s.Host
	manager, client := a.driveManagerLocked(), a.client
	s.Status = "Finding drives…"
	var drives []storage.Drive
	var notice string
	a.driveSettingsAsyncLocked(func(ctx context.Context) error {
		if host {
			if client == nil {
				return fmt.Errorf("FogCast host is unavailable")
			}
			inventory, err := client.StorageDrives(ctx, true)
			drives, notice = inventory.Drives, inventory.Notice
			return err
		}
		var err error
		drives, err = manager.List(ctx, true)
		if drives != nil && err != nil {
			notice = err.Error()
			return nil
		}
		return err
	}, func() { s.Drives = drives; s.Index = 0; s.Status = notice; a.refreshStorageRootsLocked() })
}
func (a *App) saveDriveSettingsLocked(d storage.Drive, done func(storage.Drive)) {
	s := &a.driveSettings
	host := s.Host
	manager, client := a.driveManagerLocked(), a.client
	var saved storage.Drive
	a.driveSettingsAsyncLocked(func(ctx context.Context) error {
		var err error
		if host {
			if client == nil {
				return fmt.Errorf("FogCast host is unavailable")
			}
			saved, err = client.SaveStorageDrive(ctx, d)
		} else {
			saved, err = manager.Save(d)
		}
		return err
	}, func() { a.refreshStorageRootsLocked(); done(saved) })
}
func (a *App) driveSettingsBackLocked() {
	s := &a.driveSettings
	if s.Busy {
		return
	}
	if s.Path != "" {
		host, id, rel, _ := splitDrivePath(s.Path)
		if rel == "" || rel == "." {
			s.Path = ""
			s.Index = 0
			s.Files = nil
			return
		}
		a.loadDrivePickerDirLocked(drivePath(host, id, filepath.Dir(rel)), "storage")
		return
	}
	if s.Draft != nil {
		s.Draft = nil
		s.Editing = false
		s.Index = 0
		return
	}
	a.closeDriveSettingsLocked()
}
func (a *App) handleDriveSettingsLocked(cmd Command) {
	s := &a.driveSettings
	if s.Busy {
		return
	}
	rows := a.driveSettingsRowsLocked()
	n := len(rows)
	switch cmd {
	case CmdBack:
		a.driveSettingsBackLocked()
		return
	case CmdUp, CmdLeft, CmdTabPrev:
		s.Index = (s.Index - 1 + n) % n
		return
	case CmdDown, CmdRight, CmdTab:
		s.Index = (s.Index + 1) % n
		return
	case CmdSelect:
	default:
		return
	}
	if s.Index < 0 || s.Index >= n {
		return
	}
	row := rows[s.Index]
	switch row.Kind {
	case "close":
		a.closeDriveSettingsLocked()
	case "back":
		s.Draft = nil
		s.Editing = false
		s.Index = 0
		a.refreshDriveSettingsLocked()
	case "owner":
		s.Host = !s.Host
		s.Drives = nil
		s.Index = 0
		a.refreshDriveSettingsLocked()
	case "refresh":
		a.refreshDriveSettingsLocked()
	case "add-smb":
		s.Draft = &storage.Drive{Kind: "smb"}
		s.Editing = true
		s.Index = 1
	case "edit":
		s.Editing = true
		s.Index = 1
	case "label", "source", "username", "domain", "password":
		d := s.Draft
		if d == nil {
			return
		}
		value := ""
		switch row.Kind {
		case "label":
			value = d.Label
		case "source":
			value = d.Source
		case "username":
			value = d.Username
		case "domain":
			value = d.Domain
		}
		a.settingsOSKKind = settingsOSKDriveField
		a.settingsOSKField = shared.TextField{Buffer: value}
		a.settingsOSKField.OSK.Reset()
		s.Status = row.Kind
	case "save":
		if s.Draft == nil {
			return
		}
		d := *s.Draft
		a.saveDriveSettingsLocked(d, func(saved storage.Drive) {
			s.Draft = &saved
			s.Editing = false
			s.Index = 0
			s.Status = "Connection saved. Choose folders to browse."
		})
	case "bios", "media":
		if s.Draft == nil {
			return
		}
		s.Purpose = row.Kind
		rel := s.Draft.MediaFolder
		if row.Kind == "bios" {
			rel = s.Draft.BIOSFolder
		}
		a.loadDrivePickerDirLocked(drivePath(s.Host, s.Draft.ID, rel), "storage")
	case "use-folder":
		if s.Draft == nil {
			return
		}
		_, _, rel, ok := splitDrivePath(s.Path)
		if !ok {
			return
		}
		d := *s.Draft
		if s.Purpose == "bios" {
			d.BIOSFolder = rel
		} else {
			d.MediaFolder = rel
		}
		a.saveDriveSettingsLocked(d, func(saved storage.Drive) {
			s.Draft = &saved
			s.Path = ""
			s.Files = nil
			s.Index = 0
			s.Status = "Folder remembered for future BIOS and media selection."
		})
	case firmwarePickerKindRoot, firmwarePickerKindDir, firmwarePickerKindParent:
		if row.Path == "" {
			s.Path = ""
			s.Index = 0
			s.Files = nil
			return
		}
		a.loadDrivePickerDirLocked(row.Path, "storage")
	case "forget":
		if s.Draft == nil {
			return
		}
		id, host := s.Draft.ID, s.Host
		manager, client := a.driveManagerLocked(), a.client
		a.driveSettingsAsyncLocked(func(ctx context.Context) error {
			if host {
				return client.ForgetStorageDrive(ctx, id)
			}
			return manager.Forget(id)
		}, func() { s.Draft = nil; s.Index = 0; s.Status = "Drive forgotten."; a.refreshDriveSettingsLocked() })
	default:
		if strings.HasPrefix(row.Kind, "drive:") {
			id := strings.TrimPrefix(row.Kind, "drive:")
			for _, d := range s.Drives {
				if d.ID == id {
					if d.Saved {
						copy := d
						s.Draft = &copy
						s.Index = 0
						s.Status = ""
					} else {
						a.saveDriveSettingsLocked(d, func(saved storage.Drive) { s.Draft = &saved; s.Index = 0 })
					}
					return
				}
			}
		}
	}
}
func (a *App) submitDriveFieldLocked() {
	s := &a.driveSettings
	if s.Draft == nil {
		a.closeSettingsOSKLocked()
		return
	}
	value := a.settingsOSKField.Buffer
	switch s.Status {
	case "label":
		s.Draft.Label = value
	case "source":
		s.Draft.Source = value
	case "username":
		s.Draft.Username = value
	case "domain":
		s.Draft.Domain = value
	case "password":
		s.Draft.Password = value
		s.Draft.PasswordSet = value != ""
	}
	a.closeSettingsOSKLocked()
	s.Status = ""
	if !s.Editing {
		d := *s.Draft
		a.saveDriveSettingsLocked(d, func(saved storage.Drive) { s.Draft = &saved })
	}
}
