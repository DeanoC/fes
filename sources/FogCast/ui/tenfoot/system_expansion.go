package tenfoot

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/rooms"
	"github.com/DeanoC/misteross/expansion"
)

type systemExpansionState struct {
	Sockets    []int
	Selected   map[int]string
	Assets     []hostclient.CoreExpansion
	ImportSlot int
}

func (a *App) openSystemExpansionsLocked(action rooms.Action) {
	if a.client == nil || a.session.State == "active" || hostclient.SessionKitLocal(a.session.State) || a.stopPhase == "stopping" || a.retryStopLock || a.launch.Phase == "launching" || a.localCoreBusyLocked() || a.localLaunchBlockedLocked() {
		a.status = "Stop the machine before choosing its next setup."
		return
	}
	if protocol.ValidateGameID(action.GameID) != nil || protocol.ValidateDigest(action.PackageID) != nil {
		a.status = "Refresh the machine setup."
		return
	}
	a.resetCoreLibraryLocked()
	s := &a.coreLibrary
	s.HardwareMode = "system-expansions"
	s.HardwareGameID, s.HardwarePackageID = action.GameID, action.PackageID
	s.SystemExpansions = systemExpansionState{}
	s.Status = "Reading expansion sockets and saved cards…"
	var view systemExpansionState
	a.coreLibraryAsyncLocked(func(ctx context.Context) error {
		entry, err := a.client.CoreEntry(ctx, action.GameID)
		if err != nil {
			return err
		}
		if entry.PackageID != action.PackageID || action.CoreID != "" && entry.CoreID != action.CoreID {
			return fmt.Errorf("machine setup changed; refresh the room")
		}
		setup, err := a.client.InstalledCoreSetup(ctx, action.PackageID)
		if err != nil {
			return err
		}
		view.Selected = map[int]string{}
		view.Sockets = corepackage.SlotSockets(setup.Descriptor)
		if len(view.Sockets) != 0 {
			selected, err := a.client.CoreEntrySlotExpansions(ctx, action.GameID)
			if err != nil {
				return err
			}
			if selected.PackageID != action.PackageID || !slices.Equal(selected.Sockets, view.Sockets) {
				return fmt.Errorf("machine sockets changed; refresh the room")
			}
			for _, card := range selected.Expansions {
				view.Selected[card.Slot] = card.ExpansionID
			}
		} else {
			for _, socket := range setup.Descriptor.Interfaces {
				if !socket.Required && socket.Minor == 0 && (socket.ID == expansion.ColecoSlot || socket.ID == expansion.Slot) && (socket.Major == 1 || socket.Major == 2) {
					view.Sockets = []int{0}
				}
			}
			if len(view.Sockets) == 0 {
				return nil
			}
			selected, err := a.client.CoreEntryExpansion(ctx, action.GameID)
			if err != nil {
				return err
			}
			view.Selected[0] = selected.ExpansionID
		}
		view.Assets, err = a.client.CoreExpansions(ctx)
		return err
	}, func() {
		s.SystemExpansions = view
		s.Status = "Choose a card for its physical socket. Changes apply to the next start."
		if len(view.Sockets) == 0 {
			s.Status = "This package has no supported expansion socket."
		}
	})
}

func systemSocketLabel(slot int) string {
	if slot == 0 {
		return "Expansion socket"
	}
	return "Socket " + strconv.Itoa(slot)
}

func (a *App) systemExpansionRowsLocked() []FirmwarePickerRow {
	s := &a.coreLibrary
	rows := []FirmwarePickerRow{{Name: "Refresh saved setup", Kind: "system-refresh", Selectable: true}}
	for _, slot := range s.SystemExpansions.Sockets {
		prefix := systemSocketLabel(slot)
		selected := s.SystemExpansions.Selected[slot]
		name := prefix + " · empty"
		if selected != "" {
			name = prefix + " · remove saved card " + selected[:8]
		}
		rows = append(rows, FirmwarePickerRow{Name: name, Kind: "system-remove:" + strconv.Itoa(slot), Selectable: selected != ""})
		for _, card := range s.SystemExpansions.Assets {
			if card.PackageID != s.HardwarePackageID || card.Slot != slot {
				continue
			}
			label := card.Label
			if label == "" {
				label = card.ExpansionID[:8]
			}
			name := prefix + " · " + label
			if selected == card.ExpansionID {
				name += " · saved"
			}
			rows = append(rows, FirmwarePickerRow{Name: name, Kind: "system-card:" + strconv.Itoa(slot) + ":" + card.ExpansionID, Selectable: true})
		}
		rows = append(rows, FirmwarePickerRow{Name: prefix + " · import expansion archive…", Kind: "system-import:" + strconv.Itoa(slot), Selectable: true})
	}
	return rows
}

func (a *App) handleSystemExpansionLocked(row FirmwarePickerRow) {
	if !row.Selectable {
		return
	}
	s := &a.coreLibrary
	if row.Kind == "system-refresh" {
		a.openSystemExpansionsLocked(rooms.Action{GameID: s.HardwareGameID, PackageID: s.HardwarePackageID})
		return
	}
	parts := strings.Split(row.Kind, ":")
	if len(parts) < 2 {
		return
	}
	slot, err := strconv.Atoi(parts[1])
	if err != nil || !slices.Contains(s.SystemExpansions.Sockets, slot) {
		return
	}
	switch parts[0] {
	case "system-import":
		s.SystemExpansions.ImportSlot = slot
		s.PickID = "system-expansion"
		s.Path = ""
		s.Index = 0
		s.Files = a.storagePickerRootsLocked("media")
		a.refreshStorageRootsLocked()
		s.Status = "Choose a sealed expansion archive for this exact package and socket. Importing does not fit it."
	case "system-remove":
		a.selectSystemExpansionLocked(slot, "")
	case "system-card":
		if len(parts) != 3 {
			return
		}
		for _, card := range s.SystemExpansions.Assets {
			if card.PackageID == s.HardwarePackageID && card.Slot == slot && card.ExpansionID == parts[2] {
				a.selectSystemExpansionLocked(slot, card.ExpansionID)
				return
			}
		}
	}
}

func (a *App) selectSystemExpansionLocked(slot int, id string) {
	s := &a.coreLibrary
	game, pkg, expected := s.HardwareGameID, s.HardwarePackageID, s.SystemExpansions.Selected[slot]
	a.coreLibraryAsyncLocked(func(ctx context.Context) error {
		if slot == 0 {
			_, err := a.client.SelectCoreEntryExpansion(ctx, game, pkg, expected, id)
			return err
		}
		_, err := a.client.SelectCoreEntrySlotExpansion(ctx, game, pkg, slot, expected, id)
		return err
	}, func() {
		s.SystemExpansions.Selected[slot] = id
		s.Status = systemSocketLabel(slot) + " saved for next start. Stop clears the running machine's memory."
		if a.room != nil {
			a.room.Resume()
		}
	})
}

func (a *App) importSystemExpansionLocked(path string) {
	s := &a.coreLibrary
	pkg, slot := s.HardwarePackageID, s.SystemExpansions.ImportSlot
	manager := a.driveManagerLocked()
	var cards []hostclient.CoreExpansion
	a.coreLibraryAsyncLocked(func(ctx context.Context) error {
		f, size, _, err := openPickerFile(ctx, manager, a.client, path, int64(expansion.MaxArchiveBytes))
		if err != nil {
			return err
		}
		defer f.Close()
		asset, err := a.client.ImportCoreExpansion(ctx, size, f)
		if err != nil {
			return err
		}
		if asset.PackageID != pkg || asset.Slot != slot {
			return fmt.Errorf("archive imported, but it belongs to a different package or socket")
		}
		cards, err = a.client.CoreExpansions(ctx)
		return err
	}, func() {
		s.SystemExpansions.Assets = cards
		s.PickID, s.Path = "", ""
		s.Files = nil
		s.Index = 0
		s.Status = "Expansion imported. Choose its card to save it for next start."
	})
}
