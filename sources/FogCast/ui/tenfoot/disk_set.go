package tenfoot

import (
	"context"

	"github.com/DeanoC/FogCast/protocol"
)

// changeSessionDiskLocked puts one library disk of the running game's set in
// its drive: the core screen's Next disk. It uses the same live media change
// as the in-play picker, by media ID and the file name the disk was imported
// under, and leaves the room where it is.
func (a *App) changeSessionDiskLocked(mediaID, name string) {
	if !a.sessionLiveMediaOfferedLocked() {
		a.roomSessionNotice = "This running machine can't change disks from here."
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
	a.status = "Changing the disk…"
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
		a.status = "Disk changed."
		if a.room != nil {
			a.room.Resume()
		}
	}()
}
