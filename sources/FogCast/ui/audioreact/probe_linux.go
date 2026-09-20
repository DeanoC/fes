//go:build linux

package audioreact

import (
	"os"
	"path/filepath"
	"strings"
)

// Probe inspects ALSA sysfs/proc and Pulse sockets. It never treats Dummy
// playback/capture or a Pulse socket as a measured game-audio peak.
func Probe() Report {
	r := Report{}
	if data, err := os.ReadFile("/proc/asound/cards"); err == nil {
		r.ALSACards = parseALSACards(string(data))
	}
	if data, err := os.ReadFile("/proc/asound/pcm"); err == nil {
		r.ALSAPCM = strings.TrimSpace(string(data))
	}
	r.Pulse = pulsePresent()
	r.Notes = notesFor(r.ALSACards, r.ALSAPCM, r.Pulse)
	r.Measured = false
	return r
}

func pulsePresent() bool {
	paths := []string{
		"/run/pulse/native",
		"/var/run/pulse/native",
	}
	if matches, err := filepath.Glob("/run/user/*/pulse/native"); err == nil {
		paths = append(paths, matches...)
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}
