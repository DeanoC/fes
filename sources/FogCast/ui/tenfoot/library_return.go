package tenfoot

import "github.com/DeanoC/FogCast/ui/rooms"

// libraryOrigin is the room, and its nested parents, that the Library was
// opened from. It is suspended rather than closed, so Back from the Library
// returns to the same room instance with its selection, as Back from a
// nested room already does.
type libraryOrigin struct {
	room  *rooms.Instance
	stack []*rooms.Instance
}

// suspendRoomsForLibraryLocked leaves the open room for the Library. A room
// that has failed is closed instead: there is nothing to return to.
func (a *App) suspendRoomsForLibraryLocked() {
	if a.room == nil && a.libraryReturn != nil {
		// Already in the Library: keep where it was opened from.
		return
	}
	a.closeFirmwareCardLocked()
	a.discardLibraryOriginLocked()
	if a.room == nil || a.room.Err() != nil {
		a.closeAllRoomsLocked()
		return
	}
	a.closeRoomOverlaysLocked()
	a.libraryReturn = &libraryOrigin{room: a.room, stack: a.roomStack}
	id := a.room.ID()
	a.room = nil
	a.roomStack = nil
	a.roomErr = ""
	a.roomFrame = rooms.Frame{}
	a.roomWaitAck = false
	a.roomCoreDetail = ""
	a.postUIEventLocked("ui.nav", map[string]string{"reason": "room-suspend", "view": "room:" + id})
}

// restoreLibraryOriginLocked puts the suspended rooms back, closing Library
// overlays, so Back or Home can resume or reclaim them. The caller resumes.
func (a *App) restoreLibraryOriginLocked() bool {
	o := a.libraryReturn
	if o == nil || a.room != nil {
		return false
	}
	a.libraryReturn = nil
	// Library overlays belong to the Library, not to the room coming back.
	a.closeDetailLocked()
	a.closeFiltersLocked()
	a.closeCollectionOverlaysLocked()
	a.searchOpen = false
	a.libraryCoreIDs = nil
	// A Library refusal never started this room's launch. Leaving the error
	// set replays it as "Launch failed" over the resumed room, and offers
	// retry for a Confirm that already refused.
	if a.launch.Phase == "error" || a.launch.Phase == "host" {
		if a.status == a.launch.Message {
			a.status = ""
		}
		a.dismissLaunchOverlayLocked()
	}
	a.room = o.room
	a.roomStack = o.stack
	a.roomWasParked = a.gpuParked
	a.resizeRoomsLocked()
	return true
}

// returnFromLibraryLocked is Back from the Library: resume the room it was
// opened from. It reports false when the Library has no origin.
func (a *App) returnFromLibraryLocked() bool {
	if a.sessionStopOfferedLocked() || !a.restoreLibraryOriginLocked() {
		return false
	}
	a.postUIEventLocked("ui.nav", map[string]string{"reason": "room-resume", "view": "room:" + a.room.ID()})
	a.room.Resume()
	a.applyRoomActionsLocked()
	return true
}

// discardLibraryOriginLocked closes a suspended origin that can no longer be
// returned to, because another room or Home took its place.
func (a *App) discardLibraryOriginLocked() {
	o := a.libraryReturn
	if o == nil {
		return
	}
	a.libraryReturn = nil
	o.room.Close()
	for _, parent := range o.stack {
		if parent != nil {
			parent.Close()
		}
	}
}

// libraryBackToLocked names the room Back returns to, or "".
func (a *App) libraryBackToLocked() string {
	if a.libraryReturn == nil || a.room != nil {
		return ""
	}
	return a.libraryReturn.room.Title()
}

// setSuspendedReducedMotionLocked keeps the suspended origin in step with a
// Settings change made in the Library.
func (a *App) setSuspendedReducedMotionLocked(on bool) {
	if a.libraryReturn == nil {
		return
	}
	a.libraryReturn.room.SetReducedMotion(on)
	for _, parent := range a.libraryReturn.stack {
		if parent != nil {
			parent.SetReducedMotion(on)
		}
	}
}
