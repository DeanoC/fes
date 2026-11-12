package tenfoot

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/ui/rooms"
)

// A root Home room retired for missing its time budget (rooms.ErrBudget) is
// reopened as a fresh instance after a backoff. On the kit this happens when
// the Lua thread waits for a CPU during the boot burst; the checks themselves
// are unchanged, the launcher only gives Home another go once the burst has
// passed. Script errors are never retried.
//
// The cap counts automatic reopens per launcher process. The launcher process
// lives for one kit boot unless the supervisor restarts it, and a restart is
// itself a fresh boot-like burst, so a per-process cap is the per-boot cap
// without persisting anything. The count resets only after a Home instance
// has run without failing for homeHealthyReset, so a Home that keeps failing
// soon after each reopen stops at the cap instead of looping.
const (
	homeAutoReopenMax = 3
	homeHealthyReset  = 10 * time.Minute
)

// homeReopenBackoff is the wait before automatic reopen n (0-based).
var homeReopenBackoff = [homeAutoReopenMax]time.Duration{2 * time.Second, 10 * time.Second, 30 * time.Second}

type homeRecovery struct {
	// reopens is the automatic reopens used since the last healthy reset.
	reopens int
	// failed is the last failed root Home instance this policy has handled.
	failed *rooms.Instance
	// due is when failed is reopened; zero when no reopen is waiting.
	due time.Time
	// healthy is the root Home instance last seen stepping without a
	// failure, and healthySince when it was first seen.
	healthy      *rooms.Instance
	healthySince time.Time
}

// rootHomeLocked reports that the open room is the configured Home room at
// the root of the room stack.
func (a *App) rootHomeLocked() bool {
	return a.room != nil && a.homeRooms && !a.roomDuringPlay && len(a.roomStack) == 0 &&
		a.room.ID() == strings.TrimSpace(a.homeRoom)
}

// failedRootHomeLocked reports that the root Home room has failed.
func (a *App) failedRootHomeLocked() bool {
	return a.rootHomeLocked() && a.room.Err() != nil
}

// homeRetryPendingLocked is the 1-based automatic reopen waiting for the open
// failed root Home, or 0.
func (a *App) homeRetryPendingLocked() int {
	r := &a.homeRecovery
	if r.due.IsZero() || r.failed == nil || r.failed != a.room {
		return 0
	}
	return r.reopens + 1
}

// noteHomeHealthyLocked runs after a successful step. A root Home instance
// that has stayed up for homeHealthyReset returns the automatic reopens.
func (a *App) noteHomeHealthyLocked(now time.Time) {
	if !a.rootHomeLocked() {
		return
	}
	r := &a.homeRecovery
	if r.healthy != a.room {
		r.healthy, r.healthySince = a.room, now
		return
	}
	if r.reopens > 0 && now.Sub(r.healthySince) >= homeHealthyReset {
		fmt.Fprintf(os.Stderr, "tenfoot: home room %s healthy for %s; automatic reopens reset\n", a.room.ID(), homeHealthyReset)
		r.reopens = 0
	}
}

// recoverHomeLocked runs on each tick while the open room has failed. It
// schedules and performs the automatic reopen of a root Home that failed on
// its time budget.
func (a *App) recoverHomeLocked(now time.Time) {
	r := &a.homeRecovery
	if !a.failedRootHomeLocked() {
		r.due = time.Time{}
		return
	}
	inst := a.room
	if r.failed != inst {
		r.failed, r.due = inst, time.Time{}
		if r.healthy == inst {
			r.healthy = nil
		}
		err := inst.Err()
		switch {
		case !errors.Is(err, rooms.ErrBudget):
			// A script error fails the same way every time.
			return
		case r.reopens >= homeAutoReopenMax:
			fmt.Fprintf(os.Stderr, "tenfoot: home room %s failed again; %d automatic reopens used, staying failed: %v\n", inst.ID(), homeAutoReopenMax, err)
			return
		}
		r.due = now.Add(homeReopenBackoff[r.reopens])
		return
	}
	if r.due.IsZero() || now.Before(r.due) {
		return
	}
	// The picker and Settings sit over the failed room; reopening would
	// close the picker under the user, so wait until they are gone.
	if a.roomPickerOpen || a.settingsOpen {
		return
	}
	r.due = time.Time{}
	r.reopens++
	err := inst.Err()
	fmt.Fprintf(os.Stderr, "tenfoot: reopening home room %s (automatic reopen %d of %d) after budget failure: %v\n", inst.ID(), r.reopens, homeAutoReopenMax, err)
	a.postUIEventLocked("ui.home_room_reopen", map[string]string{
		"room":    inst.ID(),
		"attempt": strconv.Itoa(r.reopens),
		"error":   err.Error(),
	})
	a.reopenHomeLocked()
}

// reopenHomeLocked replaces a failed root Home with a fresh instance of the
// selected Home pack, or opens the picker when Home cannot be opened.
//
// This is not an update, so it skips the trial and rollback that
// openInstalledRoomLocked applies to an installed pack: a fresh instance that
// misses its deadline in the same CPU burst must not roll back or delete the
// installed Home selection. A fresh instance that fails stays up with its
// error panel, and recovery handles it like any other failure.
func (a *App) reopenHomeLocked() {
	a.closeRoomOverlaysLocked()
	id := strings.TrimSpace(a.homeRoom)
	if !a.sessionStopOfferedLocked() && a.refreshInstalledRoomsLocked() {
		pack, reason, ok := a.resolveHomeRoomLocked(id)
		if ok {
			a.closeAllRoomsLocked()
			a.roomPickerOpen = false
			if a.openValidRoomLocked(pack, "") {
				return
			}
			a.roomErr = ""
			reason = "open failed"
		}
		a.noteHomeRoomFallbackLocked(id, reason)
	}
	a.closeRoomLocked()
	a.openRoomPickerLocked()
}
