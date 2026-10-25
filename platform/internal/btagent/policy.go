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

	appearanceJoystick = 0x03C3
	appearanceGamepad  = 0x03C4

	attemptGap = 30 * time.Second
	attemptMax = 3
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
// by its Class of Device (peripheral, joystick or gamepad minor), by its LE
// appearance (joystick 0x03C3 or gamepad 0x03C4), or by a known pad vendor
// in its modalias together with the HID/HoG service or a peripheral class.
func isGamepad(dev devInfo) bool {
	major := (dev.Class >> 8) & 0x1f
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
	return padVendor(dev.Modalias) && (controllerUUID(dev.UUIDs) || major == majorPeripheral)
}

// padVendors are USB/Bluetooth vendor ids of game controller makers:
// Sony, Microsoft, Nintendo and 8BitDo.
var padVendors = map[string]bool{"054c": true, "045e": true, "057e": true, "2dc8": true}

// padVendor parses a BlueZ modalias such as "usb:v054CpCE6Bd0100" or
// "bluetooth:v057Ep2009d0001" and reports a known pad vendor.
func padVendor(modalias string) bool {
	_, rest, ok := strings.Cut(modalias, ":v")
	if !ok || len(rest) < 4 {
		return false
	}
	return padVendors[strings.ToLower(rest[:4])]
}

func controllerUUID(ids []string) bool {
	for _, id := range ids {
		if oneControllerUUID(id) {
			return true
		}
	}
	return false
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
// Has reports the device whose pairing call is in flight so an agent callback
// can accept its PIN.
type attempts struct {
	mu       sync.Mutex
	inflight map[string]action
	fails    map[string]int
	last     map[string]time.Time
}

func newAttempts() *attempts {
	return &attempts{
		inflight: map[string]action{},
		fails:    map[string]int{},
		last:     map[string]time.Time{},
	}
}

func (a *attempts) Start(path string, now time.Time, what action) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.inflight[path]; ok {
		return false
	}
	if what == actionPair && a.fails[path] >= attemptMax {
		return false
	}
	if t, ok := a.last[path]; ok && now.Sub(t) < attemptGap {
		return false
	}
	a.inflight[path] = what
	a.last[path] = now
	return true
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

func (a *attempts) Finish(path string, pairSucceeded bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	what, exists := a.inflight[path]
	delete(a.inflight, path)
	if !exists || what != actionPair {
		return
	}
	if pairSucceeded {
		a.fails[path] = 0
		return
	}
	a.fails[path]++
}
