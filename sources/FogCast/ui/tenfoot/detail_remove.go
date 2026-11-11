package tenfoot

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/DeanoC/FogCast/hostclient"
)

// detailRemoveRects places the action in the visible Details pane. In the
// confirmation state the same action becomes Remove, beside a Cancel button.
func detailRemoveRects(s Snapshot) (rectI, rectI, bool) {
	if !s.Detail.Open || !s.Detail.RemoveAvailable {
		return rectI{}, rectI{}, false
	}
	geom, ok := detailGeomOf(s)
	if !ok || geom.Pane.W < 360 || geom.Pane.H < 90 {
		return rectI{}, rectI{}, false
	}
	pane := geom.Pane
	action := rectI{X: pane.X + pane.W - 158, Y: pane.Y + pane.H - 42, W: 142, H: 30}
	cancel := rectI{}
	if s.Detail.RemoveConfirm {
		cancel = rectI{X: action.X - 150, Y: action.Y, W: 142, H: 30}
	}
	return action, cancel, true
}

func detailRemoveHit(s Snapshot, x, y int) (PointerHit, bool) {
	action, cancel, ok := detailRemoveRects(s)
	if !ok || s.Detail.RemoveBusy {
		return PointerHit{}, false
	}
	if action.contains(x, y) {
		return PointerHit{Kind: PointerDetailRemove}, true
	}
	if s.Detail.RemoveConfirm && cancel.contains(x, y) {
		return PointerHit{Kind: PointerDetailRemoveCancel}, true
	}
	return PointerHit{}, false
}

// detailRemoveTargetLocked identifies the exact library entry shown by Details.
// A core package ID distinguishes removable core entries from scanned games.
func (a *App) detailRemoveTargetLocked() string {
	if !a.detailOpen || a.client == nil || a.roomDuringPlay {
		return ""
	}
	var game hostclient.Game
	switch {
	case a.room != nil:
		game = a.roomDetail
	case strings.TrimSpace(a.libraryDetail.ID) != "":
		game = a.libraryDetail
	case a.grid.Focus >= 0 && a.grid.Focus < len(a.games):
		game = a.games[a.grid.Focus]
	}
	if game.ID == "" || game.PackageID == "" {
		return ""
	}
	return game.ID
}

func (a *App) startDetailRemovalLocked() bool {
	gameID := a.detailRemoveTargetLocked()
	if gameID == "" || a.detailRemoveBusy {
		return false
	}
	a.detailRemoveID = gameID
	a.detailRemoveError = ""
	return true
}

// handleDetailRemoveConfirmLocked consumes all confirmation input so that
// confirming removal cannot also launch a title or act on another selection.
func (a *App) handleDetailRemoveConfirmLocked(cmd Command) bool {
	if a.detailRemoveID == "" {
		return false
	}
	if cmd == CmdBack || cmd == CmdUp {
		a.detailRemoveID = ""
		a.detailRemoveError = ""
		a.detailRemoveGeneration++
		return true
	}
	if cmd != CmdSelect || a.detailRemoveBusy {
		return true
	}
	gameID := a.detailRemoveID
	if a.detailRemoveTargetLocked() != gameID {
		a.detailRemoveID = ""
		a.detailRemoveError = ""
		a.detailRemoveGeneration++
		return true
	}
	a.detailRemoveBusy = true
	a.detailRemoveError = ""
	generation := a.detailRemoveGeneration
	originRoom := a.room
	client := a.client
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		err := client.DeleteCoreEntry(ctx, gameID)
		a.mu.Lock()
		defer a.mu.Unlock()
		a.detailRemoveBusy = false
		sameDetail := a.detailOpen && a.detailRemoveGeneration == generation && a.detailRemoveID == gameID && a.detailSubjectIDLocked() == gameID
		if err != nil {
			if sameDetail {
				a.detailRemoveError = detailRemoveError(err)
				a.status = a.detailRemoveError
			}
			return
		}
		if sameDetail {
			a.closeDetailLocked()
			if a.room != nil && a.room == originRoom {
				a.roomDetail = hostclient.Game{}
				a.room.Resume()
				a.dropRoomNavActionsLocked()
			}
		}
		// The completed removal is real even if the user changed selection
		// while waiting. Refresh without closing an unrelated Details pane.
		a.ensureHomeRecentsLocked()
		if sameDetail {
			a.reloadCatalogWithNoticeLocked("Title removed. Saved data was kept.")
		} else {
			a.reloadCatalogLocked()
		}
	}()
	return true
}

func detailRemoveError(err error) string {
	var status *hostclient.HTTPStatusError
	if errors.As(err, &status) {
		switch status.StatusCode {
		case http.StatusConflict:
			return "Kit busy. Stop play or finish recovery, then retry."
		case http.StatusServiceUnavailable:
			return "Can't check kits. Reconnect them and retry."
		}
	}
	return "Couldn't remove title. Please retry."
}
