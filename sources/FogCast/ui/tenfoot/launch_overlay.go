package tenfoot

import (
	"strings"

	"github.com/DeanoC/FogCast/ui/rooms"
)

// LaunchOverlayCopy is the FES-owned launch chrome shown over a room.
// Progress is a real phase string (launch message or session stage); it
// never invents a percentage.
type LaunchOverlayCopy struct {
	Visible bool
	Failed  bool
	Heading string
	Title   string
	Phase   string
	Reason  string
	Hint    string
}

func launchOverlayVisible(snap Snapshot) bool {
	if !snap.Room.Open || snap.GPUParked {
		return false
	}
	switch snap.Launch.Phase {
	case "launching", "error", "host":
		return true
	default:
		return false
	}
}

func launchOverlayCopy(snap Snapshot) LaunchOverlayCopy {
	if !launchOverlayVisible(snap) {
		return LaunchOverlayCopy{}
	}
	failed := snap.Launch.Phase == "error" || snap.Launch.Phase == "host"
	copy := LaunchOverlayCopy{
		Visible: true,
		Failed:  failed,
		Heading: "Launching",
		Title:   launchOverlayTitle(snap),
		Phase:   honestLaunchPhase(snap),
	}
	if failed {
		copy.Heading = "Launch failed"
		copy.Reason = launchOverlayReason(snap)
		copy.Hint = launchOverlayFailHint(snap.Affinity)
	} else {
		copy.Hint = launchOverlayBusyHint(snap.Affinity)
	}
	return copy
}

func launchOverlayTitle(snap Snapshot) string {
	if title := strings.TrimSpace(snap.Session.Title); title != "" {
		return title
	}
	if label := strings.TrimSpace(snap.Room.Destination.Label); label != "" {
		return label
	}
	if id := strings.TrimSpace(snap.Launch.GameID); id != "" {
		return id
	}
	return "this title"
}

// honestLaunchPhase reports a real host/session phase. It does not
// synthesize completion percentages.
func honestLaunchPhase(snap Snapshot) string {
	if progress := strings.TrimSpace(snap.Session.Progress); progress != "" {
		return progress
	}
	if msg := strings.TrimSpace(snap.Launch.Message); msg != "" {
		return msg
	}
	switch snap.Launch.Phase {
	case "launching":
		return "launching"
	case "error", "host":
		return "launch failed"
	default:
		return strings.TrimSpace(snap.Launch.Phase)
	}
}

func launchOverlayReason(snap Snapshot) string {
	code := strings.TrimSpace(snap.Launch.ErrorCode)
	msg := strings.TrimSpace(snap.Launch.ErrorMessage)
	if msg == "" {
		msg = strings.TrimSpace(snap.Launch.Message)
	}
	switch {
	case code != "" && msg != "" && !strings.Contains(msg, code):
		return code + "  ·  " + msg
	case code != "":
		return code
	default:
		return msg
	}
}

func launchOverlayBusyHint(kind InputKind) string {
	return "launch in progress  " + backWord(kind) + " stays here  " + settingsWord(kind) + " settings"
}

func launchOverlayFailHint(kind InputKind) string {
	return selectWord(kind) + " retry  " + backWord(kind) + " back to room  " + settingsWord(kind) + " settings"
}

func (a *App) launchOverlayActiveLocked() bool {
	if a == nil || a.room == nil {
		return false
	}
	switch a.launch.Phase {
	case "launching", "error", "host":
		return true
	default:
		return false
	}
}

func (a *App) handleLaunchOverlayLocked(cmd Command) {
	switch a.launch.Phase {
	case "launching":
		if cmd == CmdBack {
			a.status = "launch in progress"
		}
		return
	case "error", "host":
		switch cmd {
		case CmdSelect:
			a.retryLaunchOverlayLocked()
		case CmdBack:
			a.dismissLaunchOverlayLocked()
		}
	}
}

func (a *App) retryLaunchOverlayLocked() {
	if a.launch.Phase == "launching" {
		return
	}
	dest := rooms.Destination{}
	if a.room != nil {
		dest = a.roomDestinationLocked()
	}
	if game, ok := dest.Game(); ok {
		a.startLaunchGameLocked(game)
		return
	}
	if id := strings.TrimSpace(a.launch.GameID); id != "" {
		a.launchFromRoomLocked(id)
		return
	}
	if dest.GameID != "" {
		a.launchFromRoomLocked(dest.GameID)
	}
}

func (a *App) dismissLaunchOverlayLocked() {
	if a.launch.Phase == "launching" {
		a.status = "launch in progress"
		return
	}
	a.launch.Phase = "idle"
	a.launch.Message = ""
	a.launch.ErrorCode = ""
	a.launch.ErrorMessage = ""
	a.launch.HTTPStatus = 0
	a.launch.State = ""
}
