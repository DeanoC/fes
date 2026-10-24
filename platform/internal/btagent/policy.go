package btagent

import (
	"strings"
	"sync"
	"time"
)

const (
	hidUUID = "00001124-0000-1000-8000-00805f9b34fb"
	hogUUID = "00001812-0000-1000-8000-00805f9b34fb"

	majorComputer    = 1
	majorPhone       = 2
	majorAudio       = 4
	majorPeripheral  = 5
	minorJoystick    = 0x01
	minorGamepad     = 0x02
	minorKeyPointing = 0x30

	attemptGap = 30 * time.Second
	attemptMax = 3
)

type action int

const (
	actionIgnore action = iota
	actionPair
	actionConnect
)

// isController reports whether a remote device is a gamepad, joystick,
// keyboard or pointing device. Computers, phones and audio devices are
// rejected even when they advertise HID. A class of zero is a controller
// only when it carries the HID or HoG service UUID.
func isController(uuids []string, class uint32) bool {
	switch (class >> 8) & 0x1f {
	case majorComputer, majorPhone, majorAudio:
		return false
	default:
		if controllerUUID(uuids) {
			return true
		}
		if (class>>8)&0x1f != majorPeripheral {
			return false
		}
		minor := (class >> 2) & 0x3f
		kind := minor & 0x0f
		return kind == minorJoystick || kind == minorGamepad || minor&minorKeyPointing != 0
	}
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

func decide(paired, connected bool, uuids []string, class uint32) action {
	if !isController(uuids, class) || connected {
		return actionIgnore
	}
	if paired {
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
