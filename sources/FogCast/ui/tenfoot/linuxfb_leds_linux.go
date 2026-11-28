//go:build linux

package tenfoot

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const defaultSysClassInput = "/sys/class/input"

// dualSensePlayerLEDMasks mirrors platform/embedded/tamago/input/controllers/dualsense.go
// for 5-LED Sony player indicators (bits 0..4 map to :player-1..:player-5).
var dualSensePlayerLEDMasks = [4]uint8{
	0x04, // Player 1: center LED
	0x0a, // Player 2: inner pair
	0x15, // Player 3: center + outer pair
	0x1b, // Player 4: 4 LEDs
}

type padSysfsLEDs struct {
	player       [6]string // 1..5 indexed
	maxPlayer    int
	rgbIndicator string
	red          string
	green        string
	blue         string
	xpad         []string
}

// syncEvdevPlayerLEDs updates kernel LED class entries under sysfs to show the
// pad's assigned player port (1..4), or turns them off when player is 0.
func syncEvdevPlayerLEDs(sysClassInput, eventPath string, player int) {
	if eventPath == "" {
		return
	}
	base := filepath.Base(eventPath)
	if !strings.HasPrefix(base, "event") {
		return
	}
	if sysClassInput == "" {
		sysClassInput = defaultSysClassInput
	}
	if player < 1 || player > 4 {
		player = 0
	}

	leds := discoverPadSysfsLEDs(sysClassInput, base)
	if leds.maxPlayer > 0 {
		mask := uint8(0)
		if player > 0 {
			if leds.maxPlayer == 5 {
				mask = dualSensePlayerLEDMasks[player-1]
			} else {
				mask = uint8(1 << uint(player-1))
			}
		}
		for slot := 1; slot <= leds.maxPlayer && slot < len(leds.player); slot++ {
			dir := leds.player[slot]
			if dir == "" {
				continue
			}
			val := "0\n"
			if mask&(1<<uint(slot-1)) != 0 {
				val = "1\n"
			}
			writeSysfsValue(filepath.Join(dir, "brightness"), val)
		}
	}

	if leds.rgbIndicator != "" {
		if player > 0 {
			rgb := playerLEDColors[player-1]
			writeSysfsValue(filepath.Join(leds.rgbIndicator, "multi_intensity"), fmt.Sprintf("%d %d %d\n", rgb[0], rgb[1], rgb[2]))
			writeSysfsValue(filepath.Join(leds.rgbIndicator, "brightness"), readSysfsMaxBrightness(leds.rgbIndicator)+"\n")
		} else {
			writeSysfsValue(filepath.Join(leds.rgbIndicator, "brightness"), "0\n")
		}
	} else if leds.red != "" && leds.green != "" && leds.blue != "" {
		if player > 0 {
			rgb := playerLEDColors[player-1]
			writeSysfsValue(filepath.Join(leds.red, "brightness"), fmt.Sprintf("%d\n", rgb[0]))
			writeSysfsValue(filepath.Join(leds.green, "brightness"), fmt.Sprintf("%d\n", rgb[1]))
			writeSysfsValue(filepath.Join(leds.blue, "brightness"), fmt.Sprintf("%d\n", rgb[2]))
		} else {
			writeSysfsValue(filepath.Join(leds.red, "brightness"), "0\n")
			writeSysfsValue(filepath.Join(leds.green, "brightness"), "0\n")
			writeSysfsValue(filepath.Join(leds.blue, "brightness"), "0\n")
		}
	}

	if len(leds.xpad) > 0 {
		// Linux xpad expects 0 (off) or player+1 (2..5 => solid LED 1..4).
		val := "0\n"
		if player > 0 {
			val = fmt.Sprintf("%d\n", player+1)
		}
		for _, dir := range leds.xpad {
			writeSysfsValue(filepath.Join(dir, "brightness"), val)
		}
	}
}

func discoverPadSysfsLEDs(sysClassInput, eventBase string) padSysfsLEDs {
	var leds padSysfsLEDs
	for _, rel := range [][]string{{"device", "device", "leds"}, {"device", "leds"}} {
		candidate := filepath.Join(append([]string{sysClassInput, eventBase}, rel...)...)
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			continue
		}
		entries, err := os.ReadDir(resolved)
		if err != nil || len(entries) == 0 {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			full := filepath.Join(resolved, name)
			if slot, ok := parseSysfsPlayerSlot(name); ok && slot >= 1 && slot < len(leds.player) {
				leds.player[slot] = full
				if slot > leds.maxPlayer {
					leds.maxPlayer = slot
				}
				continue
			}
			switch {
			case strings.HasSuffix(name, ":rgb:indicator"):
				leds.rgbIndicator = full
			case strings.HasSuffix(name, ":red"):
				leds.red = full
			case strings.HasSuffix(name, ":green"):
				leds.green = full
			case strings.HasSuffix(name, ":blue"):
				leds.blue = full
			case isXpadLEDName(name):
				leds.xpad = append(leds.xpad, full)
			}
		}
		return leds
	}
	return leds
}

func parseSysfsPlayerSlot(name string) (int, bool) {
	const prefix = ":player-"
	idx := strings.LastIndex(name, prefix)
	if idx < 0 {
		return 0, false
	}
	slot, err := strconv.Atoi(name[idx+len(prefix):])
	if err != nil {
		return 0, false
	}
	return slot, true
}

func isXpadLEDName(name string) bool {
	if !strings.HasPrefix(name, "xpad") || len(name) == len("xpad") {
		return false
	}
	for _, r := range name[len("xpad"):] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func readSysfsMaxBrightness(dir string) string {
	raw, err := os.ReadFile(filepath.Join(dir, "max_brightness"))
	if err != nil {
		return "255"
	}
	val := strings.TrimSpace(string(raw))
	if val == "" {
		return "255"
	}
	return val
}

func writeSysfsValue(path, value string) {
	_ = os.WriteFile(path, []byte(value), 0)
}
