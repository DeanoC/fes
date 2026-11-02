package tenfoot

import (
	"errors"
	"io/fs"
	"net"
	"os"
	"strings"
)

// SetKitDisplay marks the kit's own menu display, whose Settings shows the
// kit's wired link and its target ID.
func (a *App) SetKitDisplay(targetID string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.kitDisplay = true
	a.kitID = strings.TrimSpace(targetID)
}

// kitWiredStatus reads the kit's wired port. The image brings up eth0 with
// DHCP and has no Wi-Fi.
var kitWiredStatus = func() string {
	state, err := os.ReadFile("/sys/class/net/eth0/operstate")
	if err != nil {
		return "No wired port"
	}
	if strings.TrimSpace(string(state)) != "up" {
		return "Not connected"
	}
	iface, err := net.InterfaceByName("eth0")
	if err != nil {
		return "Connected"
	}
	addrs, _ := iface.Addrs()
	for _, addr := range addrs {
		if ip, ok := addr.(*net.IPNet); ok && ip.IP.To4() != nil {
			return "Connected  ·  " + ip.IP.String()
		}
	}
	return "Connected  ·  waiting for an address"
}

func (a *App) refreshSettingsWiredLocked() {
	if a.kitDisplay {
		a.settingsWired = kitWiredStatus()
	}
}

// factoryResetLocked erases tenfoot.json and puts the menu settings back to
// their defaults, then starts setup again. Images, software, cores, the
// library and the user's files are untouched. A pad keeps its remap until it
// is plugged in again, when tenfoot reads its (now absent) saved profile.
func (a *App) factoryResetLocked() {
	a.settingsConfirm = ""
	path := a.prefsPath
	if path == "" {
		path = defaultPrefsPath()
	}
	if path != "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			a.settingsStatus = "Could not erase settings: " + err.Error()
			a.status = a.settingsStatus
			return
		}
	}
	a.setSafeAreaPctLocked(DefaultSafeAreaPct, false)
	a.setLayoutLocked(LayoutGrid, false)
	a.setReducedMotionLocked(false, false)
	a.setSurroundLocked(true, false)
	a.setAllCoresLocked(false, false)
	if !a.attractForcedOff {
		a.setAttractPrefLocked(true, false)
	}
	a.pinnedRooms = nil
	a.firmwareCardSeen = map[string]bool{}
	a.setup.done = false
	a.setup.skipped = nil
	a.setup.loaded = true
	a.closeSettingsLocked()
	a.status = "Settings erased"
	if a.setup.firstRun && a.openSetupStepLocked(0) {
		return
	}
	a.closeAllRoomsLocked()
	a.goHomeNowLocked()
}
