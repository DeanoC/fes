package tenfoot

import (
	"context"
	"fmt"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/shared"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
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
}

func (a *App) coreLibraryRowsLocked() []FirmwarePickerRow {
	s := &a.coreLibrary
	if s.PickID != "" {
		return s.Files
	}
	rows := []FirmwarePickerRow{}
	add := func(name, kind string) {
		rows = append(rows, FirmwarePickerRow{Name: name, Kind: kind, Selectable: true})
	}
	if s.Ref == nil {
		add("Refresh systems", "refresh")
		for _, r := range s.Cores {
			add(r.Label+" · "+r.Standing+" · "+r.ArtifactState, "system")
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
	for _, m := range s.Setup.Capabilities.Media {
		if m.Role == "blob" || m.Role == "disk" {
			status := "choose file"
			if s.MediaRole == m.Role && s.MediaID != "" {
				status = "selected"
			}
			if m.Role == "blob" && protocol.RequiresCoreMedia(s.Setup.Descriptor) {
				status = "required · " + status
			}
			add(m.Role+" · "+status, "media:"+m.Role)
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
	if s.Ref != nil {
		title = "Set up " + s.Label
	}
	if s.PickID != "" {
		title = "Choose " + strings.TrimPrefix(strings.TrimPrefix(s.PickID, "rom:"), "media:")
	}
	return FirmwarePickerSnapshot{Open: true, Title: title, Path: s.Path, Rows: a.coreLibraryRowsLocked(), Index: s.Index, Busy: s.Busy, Status: s.Status, Hint: selectWord(a.affinity.current.Kind) + " choose  " + backWord(a.affinity.current.Kind) + " back"}
}
func (a *App) openCoreLibraryLocked() {
	a.closeSettingsLocked()
	a.closeDetailLocked()
	a.closeFirmwarePickerLocked()
	a.closeTapePickerLocked()
	a.coreLibrary.Open = true
	a.coreLibrary.Index = 0
	a.coreLibrary.Ref = nil
	a.coreLibrary.Setup = nil
	a.coreLibrary.PickID = ""
	a.refreshCoreLibraryLocked()
}
func (a *App) coreLibraryAsyncLocked(work func(context.Context) error, done func()) {
	s := &a.coreLibrary
	if s.Busy {
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
	var setup hostclient.CoreSetup
	a.coreLibraryAsyncLocked(func(ctx context.Context) error {
		if install {
			if err := a.client.InstallAvailableCore(ctx, ref); err != nil {
				return err
			}
		}
		var err error
		setup, err = a.client.CoreSetup(ctx, ref.SourceID, ref.CoreID, ref.PackageID)
		if err == nil && (setup.LibrarySourceID != ref.LibrarySourceID || setup.PackageID != ref.PackageID || setup.SourceID != ref.SourceID || setup.CoreID != ref.CoreID) {
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
		s.Open = false
		s.Gen++
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
	if row.Kind == "refresh" {
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
		home, _ := os.UserHomeDir()
		s.Files = firmwarePickerRoots(home, a.hostSettings.Libraries)
		s.Index = 0
		return
	}
	if strings.HasPrefix(row.Kind, "firmware:") {
		id := strings.TrimPrefix(row.Kind, "firmware:")
		media := s.ROMs[id]
		a.coreLibraryAsyncLocked(func(ctx context.Context) error { _, err := a.client.SelectHouseholdFirmware(ctx, media); return err }, func() { s.Setup.FirmwareMediaID = media; s.Status = "Household BIOS selected explicitly." })
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
	if !s.Online || s.Setup == nil {
		s.Status = "Refresh and choose an installed system."
		return
	}
	pick := s.PickID
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
	var media hostclient.CoreMedia
	a.coreLibraryAsyncLocked(func(ctx context.Context) error {
		f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			return err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() < min || info.Size() > max {
			return fmt.Errorf("Choose a regular file of %d–%d bytes", min, max)
		}
		media, err = a.client.ImportCoreMedia(ctx, info.Size(), f)
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
func (a *App) createCoreLibraryGameLocked() {
	s := &a.coreLibrary
	if s.Ref == nil || s.Setup == nil || !s.Online {
		return
	}
	if strings.TrimSpace(s.Title) == "" {
		s.Status = "Enter a game title."
		return
	}
	if protocol.RequiresCoreMedia(s.Setup.Descriptor) && (s.MediaRole != "blob" || s.MediaID == "") {
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
	req := hostclient.CoreSetupRequest{CoreReference: *s.Ref, Title: s.Title, ROMs: roms, MediaRole: s.MediaRole, MediaID: s.MediaID}
	a.coreLibraryAsyncLocked(func(ctx context.Context) error { _, err := a.client.CreateCoreSetupEntry(ctx, req); return err }, func() {
		s.Status = "Game added to the library. Launch checks readiness and executor compatibility."
		a.reloadLocked()
	})
}
