package btagent

import (
	"strings"
	"sync"
	"time"
)

const (
	hidUUID = "00001124-0000-1000-8000-00805f9b34fb"
	hogUUID = "00001812-0000-1000-8000-00805f9b34fb"

	majorComputer   = 1
	majorPhone      = 2
	majorAudio      = 4
	majorPeripheral = 5
	minorJoystick   = 0x01
	minorGamepad    = 0x02

	appearanceKeyboard = 0x03C1
	appearanceMouse    = 0x03C2
	appearanceJoystick = 0x03C3
	// classKeyPointing is the keyboard and pointing bits of a peripheral minor class.
	classKeyPointing  = 0xC0
	appearanceGamepad = 0x03C4

	attemptGap = 30 * time.Second
	attemptMax = 3
	// attemptCooldown expires the pairing lockout after repeated failures.
	attemptCooldown = 5 * time.Minute
)

type action int

const (
	actionIgnore action = iota
	actionPair
	actionConnect
)

// isGamepad reports whether a remote device is a gamepad or joystick. Only
// such devices are paired, trusted and connected; keyboards, mice, phones,
// computers, audio and anything else nearby are ignored. A device qualifies
// by its Class of Device (peripheral, joystick or gamepad minor), its LE
// appearance (joystick 0x03C3 or gamepad 0x03C4), or an exact known-controller
// vendor/product pair in its modalias. Vendor alone is not enough: controller
// makers also sell keyboards and other HID devices.
func isGamepad(dev devInfo) bool {
	major := (dev.Class >> 8) & 0x1f
	// Keyboard and pointing classifications take precedence over every
	// positive controller hint. Some controllers and keyboards share vendors,
	// and malformed devices can advertise contradictory CoD/appearance data.
	if major == majorPeripheral && dev.Class&classKeyPointing != 0 {
		return false
	}
	if dev.Appearance == appearanceKeyboard || dev.Appearance == appearanceMouse {
		return false
	}
	switch major {
	case majorComputer, majorPhone, majorAudio:
		return false
	}
	if major == majorPeripheral {
		kind := (dev.Class >> 2) & 0x0f
		if kind == minorJoystick || kind == minorGamepad {
			return true
		}
	}
	if dev.Appearance == appearanceJoystick || dev.Appearance == appearanceGamepad {
		return true
	}
	return knownController(dev.Modalias)
}

// controllerProducts contains exact USB/Bluetooth controller identities.
// These known models remain identifiable when they omit CoD and appearance.
var controllerProducts = map[string]map[string]struct{}{
	"054c": {"0268": {}, "05c4": {}, "09cc": {}, "0ce6": {}},                                                 // Sony: DualShock 3/4, DualSense
	"045e": {"02d1": {}, "02d2": {}, "02dd": {}, "02e3": {}, "02ea": {}, "02fd": {}, "0b00": {}, "0b12": {}}, // Microsoft: Xbox controllers
	"057e": {"2009": {}},                                                                                     // Nintendo Switch Pro Controller
	"2dc8": {"2000": {}, "200f": {}, "201e": {}, "3106": {}, "3109": {}, "310a": {}, "310b": {}, "6001": {}}, // 8BitDo gamepads
}

// knownController parses a BlueZ modalias such as "usb:v054Cp0CE6d0100" or
// "bluetooth:v057Ep2009d0001" and accepts only an exact controller VID:PID.
func knownController(modalias string) bool {
	_, rest, ok := strings.Cut(strings.ToLower(modalias), ":v")
	if !ok || len(rest) < 9 || rest[4] != 'p' {
		return false
	}
	products, ok := controllerProducts[rest[:4]]
	if !ok {
		return false
	}
	_, ok = products[rest[5:9]]
	return ok
}

func oneControllerUUID(id string) bool {
	return strings.EqualFold(id, hidUUID) || strings.EqualFold(id, hogUUID)
}

func decide(dev devInfo) action {
	if !isGamepad(dev) || dev.Connected {
		return actionIgnore
	}
	if dev.Paired {
		return actionConnect
	}
	return actionPair
}

// attempts serializes work per device and rate-limits each attempt. Initial
// pairing stops after attemptMax failures; reconnect attempts remain available.
// Has reports whether a gamepad pairing call is in flight so the agent can
// gate pairing callbacks.
type attempts struct {
	mu          sync.Mutex
	inflight    map[string]action
	fails       map[string]int
	lastPair    map[string]time.Time
	lastConnect map[string]time.Time
	lockedUntil map[string]time.Time
}

func newAttempts() *attempts {
	return &attempts{
		inflight:    map[string]action{},
		fails:       map[string]int{},
		lastPair:    map[string]time.Time{},
		lastConnect: map[string]time.Time{},
		lockedUntil: map[string]time.Time{},
	}
}

func (a *attempts) Start(dev devInfo, now time.Time, what action) bool {
	if dev.Path == "" || (what != actionPair && what != actionConnect) || !isGamepad(dev) {
		return false
	}
	path := dev.Path
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.inflight[path]; ok {
		return false
	}
	if what == actionPair {
		if until, ok := a.lockedUntil[path]; ok {
			if now.Before(until) {
				return false
			}
			delete(a.lockedUntil, path)
			a.fails[path] = 0
		}
	}
	if what == actionPair {
		if t, ok := a.lastPair[path]; ok && now.Sub(t) < attemptGap {
			return false
		}
		a.lastPair[path] = now
	} else {
		if t, ok := a.lastConnect[path]; ok && now.Sub(t) < attemptGap {
			return false
		}
		a.lastConnect[path] = now
	}
	a.inflight[path] = what
	return true
}

// AllowPairAfterBondRemoval lets discovery immediately restart pairing after a
// stale bond was removed, instead of applying the just-failed Connect delay.
func (a *attempts) AllowPairAfterBondRemoval(path string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.lastPair, path)
	delete(a.lastConnect, path)
}

func (a *attempts) Has(path string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.inflight[path] == actionPair
}

func (a *attempts) HasServiceAuthorization(path string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	what, ok := a.inflight[path]
	return ok && (what == actionPair || what == actionConnect)
}

func (a *attempts) Finish(path string, pairSucceeded bool, now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	what, exists := a.inflight[path]
	delete(a.inflight, path)
	if !exists || what != actionPair {
		return
	}
	if pairSucceeded {
		a.fails[path] = 0
		delete(a.lockedUntil, path)
		return
	}
	a.fails[path]++
	if a.fails[path] >= attemptMax {
		a.lockedUntil[path] = now.Add(attemptCooldown)
	}
}
