package tenfoot

import (
	"errors"
	"io/fs"
	"slices"

	"github.com/DeanoC/FogCast/ui/rooms"
)

// setupStep is one first-run setup screen (#1345). The controllers (#980)
// and games folders (#1346) steps go between Welcome and Ready.
type setupStep struct {
	id, room string
}

var setupSteps = []setupStep{
	{id: "welcome", room: "example.welcome"},
	{id: "ready", room: "example.setup-ready"},
}

const setupBlockedStatus = "Stop the running machine to continue setup."

// setupState is first-run setup. done is the first-run marker; skipped
// lists the steps Skip setup left, which Home's banner offers to finish.
// step is the open setup room's index, or -1.
type setupState struct {
	firstRun, loaded, done bool
	skipped                []string
	step                   int
}

// SetFirstRun turns the first-run gate on: with no marker in tenfoot.json,
// starting Home opens setup instead. The kit enables it for a rooms Home;
// other launchers and tests start Home directly.
func (a *App) SetFirstRun(on bool) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.setup.firstRun = on
}

func (a *App) loadSetupLocked() {
	if a.setup.loaded {
		return
	}
	a.setup.loaded = true
	path := a.prefsPath
	if path == "" {
		path = defaultPrefsPath()
	}
	if path == "" {
		return
	}
	prefs, err := loadTenfootPrefs(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		warnPrefsWrite(path, err)
	}
	a.setup.done = prefs.SetupDone
	a.setup.skipped = append([]string(nil), prefs.SetupSkipped...)
}

// setupPendingLocked reports that starting Home must open setup.
func (a *App) setupPendingLocked() bool {
	if !a.setup.firstRun {
		return false
	}
	a.loadSetupLocked()
	return !a.setup.done
}

// roomSetupLocked is what a room opened as pack sees in room.setup.
func (a *App) roomSetupLocked(packID string) rooms.Setup {
	a.loadSetupLocked()
	s := rooms.Setup{Unfinished: a.setup.firstRun && a.setup.done && len(a.setup.skipped) > 0, Skipped: append([]string(nil), a.setup.skipped...)}
	if i := a.setup.step; i >= 0 && i < len(setupSteps) && setupSteps[i].room == packID {
		s.Step, s.Steps = i+1, len(setupSteps)
	}
	return s
}

// setupRoomOpenLocked reports that the current room is the open setup step.
func (a *App) setupRoomOpenLocked() bool {
	i := a.setup.step
	return a.room != nil && i >= 0 && i < len(setupSteps) && a.room.ID() == setupSteps[i].room
}

// openSetupStepLocked opens step i as the root room. Setup replaces Home
// rather than nesting under it, so Home opens fresh, with the new setup
// state, when setup ends.
func (a *App) openSetupStepLocked(i int) bool {
	if i < 0 || i >= len(setupSteps) || a.setupBlockedLocked() {
		return false
	}
	if _, ok := a.roomsIndex.Find(setupSteps[i].room); !ok {
		return false
	}
	a.closeSettingsLocked()
	a.roomPickerOpen = false
	a.closeAllRoomsLocked()
	a.setup.step = i
	a.openRoomLocked(setupSteps[i].room)
	return a.room != nil
}

func (a *App) persistSetupLocked() {
	a.persistPrefsLocked("setup")
}

// setupBlockedLocked is a running or starting session: setup's rooms
// replace Home, so they wait for it to stop.
func (a *App) setupBlockedLocked() bool {
	if a.sessionStopOfferedLocked() || a.launch.Phase == "launching" {
		a.status = setupBlockedStatus
		return true
	}
	return false
}

// endSetupLocked writes the marker, keeps skipped for Home's banner, and
// opens Home.
func (a *App) endSetupLocked(skipped []string) {
	if a.setupBlockedLocked() {
		return
	}
	a.loadSetupLocked()
	a.setup.done = true
	a.setup.skipped = skipped
	a.setup.step = -1
	a.persistSetupLocked()
	a.closeAllRoomsLocked()
	a.goHomeNowLocked()
}

// runSetupActionLocked handles the setup_* launcher actions.
func (a *App) runSetupActionLocked(id string) {
	a.loadSetupLocked()
	i := a.setup.step
	switch id {
	case "setup_continue":
		if i < 0 || i >= len(setupSteps) || a.setupBlockedLocked() {
			return
		}
		a.setup.skipped = slices.DeleteFunc(a.setup.skipped, func(s string) bool { return s == setupSteps[i].id })
		if i+1 < len(setupSteps) {
			a.openSetupStepLocked(i + 1)
			return
		}
		a.endSetupLocked(nil)
	case "setup_back":
		if i > 0 {
			a.openSetupStepLocked(i - 1)
		}
	case "setup_skip":
		if i < 0 || i >= len(setupSteps) {
			return
		}
		// Skipping Run setup again keeps what the finished setup left.
		if a.setup.done {
			a.endSetupLocked(a.setup.skipped)
			return
		}
		var left []string
		for _, s := range setupSteps[max(i, 1):] {
			left = append(left, s.id)
		}
		a.endSetupLocked(left)
	case "setup_finish":
		a.endSetupLocked(nil)
	case "setup_resume":
		a.openSetupStepLocked(a.firstUnfinishedStepLocked())
	}
}

// firstUnfinishedStepLocked is where Finish setup resumes: the first
// skipped step, else the start.
func (a *App) firstUnfinishedStepLocked() int {
	for i, s := range setupSteps {
		if slices.Contains(a.setup.skipped, s.id) {
			return i
		}
	}
	return 0
}

// runSetupAgainLocked is Settings' Run setup again: step 1, clearing
// nothing.
func (a *App) runSetupAgainLocked() {
	if a.setupBlockedLocked() {
		a.settingsStatus = a.status
		return
	}
	if !a.openSetupStepLocked(0) {
		a.settingsStatus = "setup is not installed"
		a.status = a.settingsStatus
	}
}
