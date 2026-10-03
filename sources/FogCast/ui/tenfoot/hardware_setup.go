package tenfoot

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/rooms"
)

type hardwareExpansionFamily struct {
	label, description string
	inProgress         bool
}

var hardwareExpansionFamilies = []hardwareExpansionFamily{
	{"16K RAM", "Memory expansion for the Sinclair ZX81 rear connector. Select a compatible RAM archive.", false},
	{"Zon X", "Zon X sound expansion. Implementation is in progress.", true},
	{"QS Character Board", "Quicksilva programmable character expansion. Implementation is in progress.", true},
}

func (a *App) openHardwarePickerLocked(action rooms.Action) {
	if a.localLaunchBlockedLocked() {
		a.status = localLaunchCheckingCopy
		return
	}
	if a.client == nil || a.session.State == "active" || a.stopPhase == "stopping" || a.retryStopLock || a.launch.Phase == "launching" || a.localCoreBusyLocked() {
		a.status = "Stop the machine before choosing its next setup."
		return
	}
	if protocol.ValidateGameID(action.GameID) != nil || protocol.ValidateDigest(action.PackageID) != nil {
		a.status = "Refresh the machine setup."
		return
	}
	a.resetCoreLibraryLocked()
	s := &a.coreLibrary
	s.HardwareGameID = action.GameID
	s.HardwarePackageID = action.PackageID
	s.ExpectedMediaID = action.ExpectedMediaID
	s.Ref = nil
	s.Setup = nil
	s.PickID = ""
	s.Tapes = nil
	s.Index = 0
	s.Online = true
	if action.Kind == rooms.ActionHardwareImportExpansion {
		s.HardwareMode = "expansions"
		s.Status = "Choose an expansion type, then import its archive. Fitting is a separate action."
		return
	}
	s.HardwareMode = "tapes"
	s.Status = "Loading starter cassettes…"
	var tapes []hostclient.HardwareTape
	a.coreLibraryAsyncLocked(func(ctx context.Context) error { var err error; tapes, err = a.client.ZX81Tapes(ctx); return err }, func() {
		s.Tapes = tapes
		s.Status = "Choose a cassette for the next start. After starting, type LOAD \"\", then RUN. Each starter tape shows its recommended RAM."
	})
}

func (a *App) hardwarePickerRowsLocked() []FirmwarePickerRow {
	s := &a.coreLibrary
	rows := []FirmwarePickerRow{}
	if s.HardwareMode == "expansions" {
		for i, f := range hardwareExpansionFamilies {
			label := f.label
			if f.inProgress {
				label += " · In progress"
			}
			rows = append(rows, FirmwarePickerRow{Name: label, Kind: "family:" + strconv.Itoa(i), Selectable: true})
		}
		return rows
	}
	rows = append(rows, FirmwarePickerRow{Name: "No cassette · start BASIC", Kind: "empty-tape", Selectable: true}, FirmwarePickerRow{Name: "Import a .p cassette…", Kind: "import-tape", Selectable: true})
	for _, t := range s.Tapes {
		rows = append(rows, FirmwarePickerRow{Name: fmt.Sprintf("%s · %dK RAM · %s", t.Name, t.RAMKB, t.License), Kind: "tape:" + t.ID, Selectable: true})
	}
	return rows
}

func (a *App) handleHardwarePickerLocked(row FirmwarePickerRow) {
	s := &a.coreLibrary
	if strings.HasPrefix(row.Kind, "family:") {
		index, err := strconv.Atoi(strings.TrimPrefix(row.Kind, "family:"))
		if err != nil || index < 0 || index >= len(hardwareExpansionFamilies) {
			return
		}
		s.ExpansionFamily = index
		s.PickID = "expansion"
		s.Status = hardwareExpansionFamilies[index].description + " Archive must match this exact installed core."
	} else if row.Kind == "import-tape" {
		s.PickID = "tape"
		s.Status = "Choose a .p / .P file of 1–16384 bytes on this launcher."
	} else if row.Kind == "empty-tape" {
		a.selectHardwareTapeLocked(nil)
		return
	} else if strings.HasPrefix(row.Kind, "tape:") {
		for _, t := range s.Tapes {
			if row.Kind == "tape:"+t.ID {
				a.selectHardwareTapeLocked(&t)
				return
			}
		}
		return
	} else {
		return
	}
	home, _ := os.UserHomeDir()
	s.Files = firmwarePickerRoots(home, a.hostSettings.Libraries)
	s.Path = ""
	s.Index = 0
}

func (a *App) selectHardwareTapeLocked(tape *hostclient.HardwareTape) {
	s := &a.coreLibrary
	game, pkg, expected := s.HardwareGameID, s.HardwarePackageID, s.ExpectedMediaID
	var selected hostclient.CoreEntry
	name, controls := "No cassette", "Start opens BASIC."
	if tape != nil {
		name, controls = tape.Name, tape.Controls
	}
	a.coreLibraryAsyncLocked(func(ctx context.Context) error {
		role, id := "", ""
		if tape != nil {
			media, err := a.client.ImportZX81Tape(ctx, *tape)
			if err != nil {
				return err
			}
			role, id = "blob", media.MediaID
		}
		var err error
		selected, err = a.client.SelectCoreEntryMedia(ctx, game, pkg, expected, role, id)
		return err
	}, func() {
		s.ExpectedMediaID = selected.MediaID
		s.Status = name + " saved for next start. " + controls
		if a.room != nil {
			a.room.Resume()
		}
	})
}

func (a *App) importHardwareFileLocked(path string) {
	s := &a.coreLibrary
	mode, game, pkg, expected := s.HardwareMode, s.HardwareGameID, s.HardwarePackageID, s.ExpectedMediaID
	family := hardwareExpansionFamilies[s.ExpansionFamily]
	var selected hostclient.CoreEntry
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
		if !info.Mode().IsRegular() {
			return fmt.Errorf("choose a regular file")
		}
		if mode == "expansions" {
			asset, err := a.client.ImportCoreExpansion(ctx, info.Size(), f)
			if err != nil {
				return err
			}
			if asset.PackageID != pkg || asset.Slot != 0 {
				return fmt.Errorf("archive imported, but it does not fit this exact Sinclair ZX81 setup")
			}
			return a.client.DescribeCoreExpansion(ctx, asset.ExpansionID, family.label, family.description, family.inProgress)
		}
		if !protocol.AdmitTapeMediaName(info.Name()) || !protocol.AdmitTapeMediaSize(info.Size()) {
			return fmt.Errorf("choose a .p / .P cassette of 1–16384 bytes")
		}
		media, err := a.client.ImportCoreMedia(ctx, info.Size(), f)
		if err != nil {
			return err
		}
		selected, err = a.client.SelectCoreEntryMedia(ctx, game, pkg, expected, "blob", media.MediaID)
		return err
	}, func() {
		s.PickID = ""
		s.Files = nil
		s.Path = ""
		s.Index = 0
		if mode == "expansions" {
			s.Status = "Expansion imported. Back returns to the shelf; choose Fit & save setup to fit it."
		} else {
			s.ExpectedMediaID = selected.MediaID
			s.Status = "Cassette saved for next start. After starting, type LOAD \"\", then RUN."
		}
		if a.room != nil {
			a.room.Resume()
		}
	})
}

func (a *App) openZX81SetupLocked() {
	if a.client == nil {
		a.status = "Host API is unavailable."
		return
	}
	a.resetCoreLibraryLocked()
	a.coreLibrary.InstalledSetup = true
	a.coreLibrary.FilterCoreID = "fes.zx81"
	a.refreshInstalledZX81Locked()
}

func (a *App) refreshInstalledZX81Locked() {
	s := &a.coreLibrary
	s.Online = false
	s.Ref = nil
	s.Setup = nil
	s.Index = 0
	s.Status = "Finding installed Sinclair ZX81 packages…"
	var cores []hostclient.AvailableCore
	a.coreLibraryAsyncLocked(func(ctx context.Context) error {
		library, err := a.client.CoreLibrary(ctx)
		if err != nil {
			return err
		}
		for _, p := range library.Packages {
			if p.CoreID != "fes.zx81" {
				continue
			}
			setup, err := a.client.InstalledZX81Setup(ctx, p.PackageID)
			if err != nil {
				continue
			}
			cores = append(cores, hostclient.AvailableCore{CoreReference: setup.CoreReference, Label: "Sinclair ZX81 " + p.Version + " · " + p.PackageID[:8], Standing: "supported", ArtifactState: "installed"})
		}
		return nil
	}, func() {
		s.Cores = cores
		s.Online = true
		s.Status = "Choose an installed Sinclair ZX81 and its BASIC ROM. Adding a setup does not start it."
		if len(cores) == 0 {
			s.Status = "No Sinclair ZX81 with a sealed ROM requirement is installed. Browse published systems to install one."
		}
	})
}
