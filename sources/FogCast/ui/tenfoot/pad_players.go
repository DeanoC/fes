package tenfoot

import (
	"time"

	"github.com/DeanoC/FogCast/remoteinput"
)

// localControllerPorts is how many pads a core can see: fes.gamepad.ports
// has two, and mister-agent claims one per kit-local player.
const localControllerPorts = 2

// noLocalPlayer marks a pad that found both ports taken. It never reaches a
// machine and drives the menu where a pad with a port would.
const noLocalPlayer int8 = -1

// yieldsPort reports whether a pad with this vendor and product gives its
// controller port to any other pad. 081f:e401 is the generic USB pad the
// kits keep plugged in as a fixture. A pad plugged in to play, such as a
// DualSense, takes port 1 from it even when the fixture opened first, and
// the fixture moves to the port that pad would have taken. With no other
// pad the fixture keeps port 1.
func yieldsPort(vendor, product uint16) bool {
	return vendor == 0x081f && product == 0xe401
}

// padKey is one pad's control. Held and sent state is per pad: a second
// pad pressing A must not release the first pad's A.
type padKey struct {
	player uint8
	code   remoteinput.Code
}

// chordPair is one pad's Select and Start. The stop chord needs both on the
// same pad; Select on one pad and Start on another is two players playing.
// armed is when press last saw this pad complete its pair.
type chordPair struct {
	selectDown, startDown bool
	armed                 time.Time
}

func (p chordPair) held() bool { return p.selectDown && p.startDown }

type chordPairs [localControllerPorts]chordPair

// set records one pair button and reports whether that pad now holds both.
// A code that is not Select or Start leaves the pair unchanged.
func (c *chordPairs) set(player uint8, code remoteinput.Code, down bool) bool {
	if int(player) >= len(c) {
		return false
	}
	switch code {
	case remoteinput.ButtonSelect:
		c[player].selectDown = down
	case remoteinput.ButtonStart:
		c[player].startDown = down
	}
	return c[player].held()
}

// press records one pair button, keeps the shared stop timer honest
// (retime), and reports whether this pad now holds both.
func (c *chordPairs) press(player uint8, code remoteinput.Code, down bool, since *time.Time, now time.Time) bool {
	was := c.at(player).held()
	held := c.set(player, code, down)
	if held && !was {
		c[player].armed = now
	}
	c.retime(was && !held, since, now)
	return held
}

// drop forgets an unplugged pad's pair under the same timer rule.
func (c *chordPairs) drop(player uint8, since *time.Time, now time.Time) {
	was := c.at(player).held()
	c.clear(player)
	c.retime(was, since, now)
}

// retime keeps the shared stop timer on a pad that still holds its pair. The
// timer starts when the first pad completes one. When a pad lets go, the
// timer moves to when a remaining pad completed its own pair, never earlier
// than it was, so the stop always waits for one pad's full second and a pad
// that did not start the timer cannot postpone it.
func (c *chordPairs) retime(broke bool, since *time.Time, now time.Time) {
	switch {
	case !c.anyHeld():
		*since = time.Time{}
	case since.IsZero():
		*since = now
	case broke:
		if armed := c.heldArmed(); armed.After(*since) {
			*since = armed
		}
	}
}

// heldArmed is the earliest completion among pads still holding their pair,
// or zero when press saw none of them complete it.
func (c *chordPairs) heldArmed() time.Time {
	var earliest time.Time
	for _, p := range c {
		if p.held() && !p.armed.IsZero() && (earliest.IsZero() || p.armed.Before(earliest)) {
			earliest = p.armed
		}
	}
	return earliest
}

func (c *chordPairs) at(player uint8) chordPair {
	if int(player) >= len(c) {
		return chordPair{}
	}
	return c[player]
}

func (c *chordPairs) clear(player uint8) {
	if int(player) < len(c) {
		c[player] = chordPair{}
	}
}

// anyHeld reports a complete chord on some pad.
func (c *chordPairs) anyHeld() bool {
	for _, p := range c {
		if p.held() {
			return true
		}
	}
	return false
}

// anyDown reports Select or Start still down on some pad. A fired chord
// rearms only once every pad has let go of both.
func (c *chordPairs) anyDown() bool {
	for _, p := range c {
		if p.selectDown || p.startDown {
			return true
		}
	}
	return false
}

// forgetPadLocked drops every record of one pad's buttons. The pad is gone,
// so nothing it held can be released by a later event from it.
func (a *App) forgetPadLocked(player uint8, now time.Time) {
	for key := range a.playPadHeld {
		if key.player == player {
			delete(a.playPadHeld, key)
		}
	}
	for _, sent := range []map[padKey]bool{a.playPadSuppressed, a.localSent} {
		for key := range sent {
			if key.player == player {
				delete(sent, key)
			}
		}
	}
	a.playPadPair.drop(player, &a.playPadChordSince, now)
	a.localPair.drop(player, &a.localChordSince, now)
}

// suppressPadLocked swallows the rest of one pad's held buttons once its
// Select+Start chord takes it. The other pad keeps playing.
func (a *App) suppressPadLocked(player uint8) {
	if a.playPadSuppressed == nil {
		a.playPadSuppressed = make(map[padKey]bool)
	}
	for key := range a.playPadHeld {
		if key.player == player {
			a.playPadSuppressed[key] = true
		}
	}
}

// portlessPadReachesMenu reports whether a pad without a controller port may
// press a menu control now. It may drive the room shown over a running session,
// except for Start and Select. Select also reaches focused launcher controls
// for keypad sessions, where it confirms the picker instead of reaching the
// machine. During an uncertain display transition, B uses the same guarded
// return as a ported pad without enabling ordinary menu commands.
func (a *App) portlessPadReachesMenu(code remoteinput.Code) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.localUnownedRun || a.localCoreBusyLocked() {
		return false
	}
	if !a.needsSessionDisplayLocked() || a.session.State != "active" {
		return true
	}
	if a.sessionDisplayUncertain {
		if code == remoteinput.ButtonB && !a.sessionDisplayBusy && a.stopPhase != "stopping" && !a.playHIDFailClosedLocked() {
			a.beginSessionDisplayLocked(a.sessionReturnDisplayLocked(), false)
		}
		return false
	}
	menuSelect := code == remoteinput.ButtonSelect && a.sessionKeypadLocked() && a.sessionMenuOwnsPadInputLocked()
	roomCommand := a.roomDuringPlay && code != remoteinput.ButtonStart && code != remoteinput.ButtonSelect
	return (roomCommand || menuSelect) &&
		!a.sessionDisplayBusy && a.stopPhase != "stopping" && !a.retryStopLock && !a.playHIDFailClosedLocked()
}
