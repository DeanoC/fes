package tenfoot

import (
	"fmt"
	"time"

	"github.com/DeanoC/FogCast/ui/rooms"
)

// Refresh discovery at a navigation boundary, rather than polling while rendering.
// Rebuild from the current loose packs so rollback can reveal a shadowed pack.
func (a *App) refreshInstalledRoomsLocked() bool {
	if a.roomsDir == "" {
		return true
	}
	packs, err := rooms.LoadDir(a.roomsDir)
	if err != nil {
		a.status = "Could not refresh rooms: " + err.Error()
		return false
	}
	a.roomsIndex = rooms.NewIndex(rooms.Examples(), packs)
	return true
}

// Trial the selected pack with actual launcher services before retiring the
// working instance. No queued room/session actions are applied on failure.
// The return value reports trial success even if an action navigates elsewhere.
func (a *App) openInstalledRoomLocked(pack rooms.Pack, root bool, initialGameID string) bool {
	if a.sessionStopOfferedLocked() || a.launch.Phase == "launching" {
		a.status = "Stop the running machine before updating a room."
		return false
	}
	if a.room != nil {
		a.room.FlushState()
	}
	candidate, err := a.newRoomInstanceLocked(pack, initialGameID)
	if err == nil {
		candidate.StageState()
		candidate.SetSessionState(a.session.State)
		candidate.SetLiveControls(a.liveControlsOfferedLocked())
		err = candidate.Load()
		if err == nil {
			candidate.Step(time.Now())
			err = candidate.Err()
		}
	}
	if err != nil {
		if candidate != nil {
			candidate.Discard()
		}
		rollbackErr := rooms.Rollback(a.roomsDir, pack.ID, pack.Revision)
		a.refreshInstalledRoomsLocked()
		if a.room == nil && rollbackErr == nil {
			if fallback, ok := a.roomsIndex.Find(pack.ID); ok && fallback.Valid() {
				a.openRoomWithSelectionLocked(pack.ID, initialGameID)
			}
		}
		a.status = fmt.Sprintf("Room update failed: %v. Previous selection restored.", err)
		if rollbackErr != nil {
			a.status = fmt.Sprintf("Room update failed: %v. Rollback: %v", err, rollbackErr)
		}
		return false
	}
	a.discardLibraryOriginLocked()
	if root {
		a.closeAllRoomsLocked()
	} else {
		a.closeRoomLocked()
	}
	candidate.AdoptSavedState()
	a.closeRoomOverlaysLocked()
	a.closeFiltersLocked()
	a.closeCollectionOverlaysLocked()
	a.searchOpen = false
	a.room = candidate
	a.roomErr = ""
	a.roomWasParked = a.gpuParked
	a.roomFrame = candidate.Frame()
	a.postUIEventLocked("ui.nav", map[string]string{"reason": "room-update", "view": "room:" + pack.ID})
	a.applyRoomActionsLocked()
	a.status = pack.Title
	return true
}
