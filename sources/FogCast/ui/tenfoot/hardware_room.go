package tenfoot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/rooms"
)

const hardwareRoomID = "example.hardware"
const hardwareRoomFooterHeight = 52
const maxPendingPlayHID = 128

func (s roomServices) Hardware(ctx context.Context) (hostclient.HardwareSnapshot, error) {
	if s.client == nil {
		return hostclient.HardwareSnapshot{}, fmt.Errorf("host API is unavailable")
	}
	return s.client.Hardware(ctx)
}

func (s roomServices) SelectCoreEntryExpansion(ctx context.Context, gameID, packageID, expectedID, expansionID string) (hostclient.CoreEntryExpansion, error) {
	if s.client == nil {
		return hostclient.CoreEntryExpansion{}, fmt.Errorf("host API is unavailable")
	}
	return s.client.SelectCoreEntryExpansion(ctx, gameID, packageID, expectedID, expansionID)
}

func (a *App) playingHardwareRoomAvailableLocked() bool {
	if a.session.State != "active" || a.stopPhase == "stopping" || a.retryStopLock || a.roomsIndex == nil {
		return false
	}
	pack, ok := a.roomsIndex.Find(hardwareRoomID)
	return ok && pack.Valid()
}

// openPlayingHardwareRoomLocked changes only launcher presentation. The live
// room instance, session and kit lease survive both visits and tape changes.
func (a *App) openPlayingHardwareRoomLocked() {
	if !a.playingHardwareRoomAvailableLocked() {
		return
	}
	a.releasePlayHIDLocked()
	a.repeat.Clear()
	a.hold.Clear()
	if a.room == nil {
		a.openRoomLocked(hardwareRoomID)
	} else if a.room.ID() != hardwareRoomID {
		a.openNestedRoomLocked(hardwareRoomID)
	} else {
		// Reused rooms retain focus but must refresh their pre-launch snapshot.
		a.room.Resume()
		a.dropRoomNavActionsLocked()
	}
	if a.room == nil || a.room.ID() != hardwareRoomID {
		return
	}
	a.roomPickerOpen = false
	a.roomDuringPlay = true
	a.roomWasParked = false // This visit already loaded or resumed the room.
	a.syncGPUParkLocked()
	a.status = "Hardware room · the machine is still running"
}

func (a *App) resumeRoomSessionLocked() {
	if !a.roomDuringPlay || a.tapePickerBusy {
		return
	}
	a.closeTapePickerLocked()
	a.closeRoomOverlaysLocked()
	a.roomPickerOpen = false
	a.roomDuringPlay = false
	a.repeat.Clear()
	a.hold.Clear()
	a.syncGPUParkLocked()
}

func (a *App) roomSessionMatchesLocked(action rooms.Action) bool {
	s := a.session
	b := action.MediaBinding
	if s.State == "active" && s.ID == action.SessionID && s.GameID == action.GameID && s.FlightID == action.FlightID &&
		s.Target == b.Target && s.TargetID == b.TargetID && s.CorePackage != nil &&
		s.CorePackage.PackageID == b.PackageID && s.CorePackage.Generation == b.Generation {
		a.roomSessionNotice = ""
		return true
	}
	a.roomSessionNotice = "The running machine changed. Review the refreshed setup."
	a.status = a.roomSessionNotice
	if a.room != nil {
		a.room.Resume()
		a.dropRoomNavActionsLocked()
	}
	return false
}

// Physical Home remains a single-key chrome route even when a described
// keyboard is attached. The letter h keeps its ordinary core meaning.
func (a *App) handlePlayingRoomKey(name string, down bool, now time.Time) bool {
	if a == nil || !strings.EqualFold(strings.TrimSpace(name), "home") {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.playingHardwareRoomAvailableLocked() || a.tapePickerOpen || (a.roomDuringPlay && down) {
		return false
	}
	if down {
		a.noteActivityLocked(now)
		a.openPlayingHardwareRoomLocked()
	}
	return true
}

type playHIDKey struct {
	player uint8
	device remoteinput.Device
	kind   remoteinput.Kind
	code   remoteinput.Code
}

func (a *App) rememberPlayHIDLocked(event remoteinput.Event) {
	key := playHIDKey{event.Player, event.Device, event.Kind, event.Code}
	if event.Action == remoteinput.ActionRelease || event.Kind == remoteinput.KindAxis && event.Value == 0 {
		delete(a.playHIDHeld, key)
		return
	}
	if a.playHIDHeld == nil {
		a.playHIDHeld = make(map[playHIDKey]remoteinput.Event)
	}
	a.playHIDHeld[key] = event
}

// queuePlayHIDLocked preserves press/release order, including releases queued
// when local input moves from the core to menu navigation.
func (a *App) queuePlayHIDLocked(events []remoteinput.Event) {
	if len(events) == 0 || a.client == nil {
		return
	}
	previous := a.playHIDTail
	if a.playHIDContext == nil {
		parent := a.ctx
		if parent == nil {
			parent = context.Background()
		}
		a.playHIDContext, a.playHIDCancel = context.WithCancel(parent)
	}
	parent, epoch := a.playHIDContext, a.playHIDEpoch
	done := make(chan struct{})
	a.playHIDTail = done
	a.playHIDPending++
	client := a.client
	go func() {
		defer close(done)
		defer func() {
			a.mu.Lock()
			if a.playHIDEpoch == epoch {
				a.playHIDPending--
			}
			a.mu.Unlock()
		}()
		if previous != nil {
			select {
			case <-parent.Done():
				return
			case <-previous:
			}
		}
		ctx, cancel := context.WithTimeout(parent, 5*time.Second)
		defer cancel()
		for _, event := range events {
			_ = client.SendCoreKey(ctx, event)
		}
	}()
}

func samePlayHIDSession(a, b hostclient.SessionResult) bool {
	if a.ID != b.ID || a.State != b.State || a.Target != b.Target || a.TargetID != b.TargetID || a.GameID != b.GameID || a.FlightID != b.FlightID {
		return false
	}
	if a.CorePackage == nil || b.CorePackage == nil {
		return a.CorePackage == b.CorePackage
	}
	return a.CorePackage.PackageID == b.CorePackage.PackageID && a.CorePackage.Generation == b.CorePackage.Generation
}

func (a *App) cancelPlayHIDLocked() {
	if a.playHIDCancel != nil {
		a.playHIDCancel()
	}
	a.playHIDContext, a.playHIDCancel = nil, nil
	a.playHIDTail, a.playHIDHeld = nil, nil
	a.playHIDPending = 0
	a.playHIDEpoch++
}

func (a *App) releasePlayHIDLocked() {
	if a.playHIDFailClosedLocked() {
		a.playHIDHeld = nil
		return
	}
	events := make([]remoteinput.Event, 0, len(a.playHIDHeld))
	for _, event := range a.playHIDHeld {
		event.Action = remoteinput.ActionRelease
		event.Value = 0
		if event.Kind == remoteinput.KindAxis {
			event.Action = remoteinput.ActionAbsolute
		}
		events = append(events, event)
	}
	a.playHIDHeld = nil
	a.queuePlayHIDLocked(events)
}

func hardwareRoomWord(kind InputKind) string {
	if kind == InputKeyboard || kind == InputMouse {
		return "Home"
	}
	return "SELECT"
}

func hardwareRoomButton(snap Snapshot) (rectI, bool) {
	if !snap.GPUParked || !snap.Session.HardwareRoom || snap.TapePicker.Open || snap.OSK.Open {
		return rectI{}, false
	}
	w := min(360, snap.Grid.contentWidth()-32)
	return rectI{X: snap.Grid.contentLeft() + 16, Y: snap.Grid.contentTop() + snap.Grid.contentHeight() - 62, W: w, H: 46}, w > 0
}

func drawHardwareRoomButton(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}) {
	button, ok := hardwareRoomButton(snap)
	if !ok {
		return
	}
	fillRect(dev, float32(button.X), float32(button.Y), float32(button.W), float32(button.H), 48, 84, 103, 255)
	drawLabel(dev, labels, used, "np-hardware", button.X+14, button.Y+11, button.W-28, 18, "Hardware room · "+hardwareRoomWord(snap.Affinity))
}
