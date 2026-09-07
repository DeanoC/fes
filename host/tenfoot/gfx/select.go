package gfx

import (
	"fmt"
	"strings"
)

// Canonical backend names. Empty input to ParseBackend means SDL, the
// production default.
const (
	BackendSDL      = "sdl"
	BackendSoftware = "software"
	BackendFPGAStub = "fpga-stub"
	BackendLinuxFB  = "linuxfb"
)

// ParseBackend maps a name or TENFOOT_GFX value to a canonical backend.
// Empty, "sdl", and "sdl3" select the production SDL3 path. Unknown names
// return an error so a typo cannot silently change the sofa renderer.
func ParseBackend(name string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "sdl", "sdl3":
		return BackendSDL, nil
	case "software", "sw":
		return BackendSoftware, nil
	case "fpga-stub", "fpga", "stub":
		return BackendFPGAStub, nil
	case "linuxfb", "fb", "fb0":
		return BackendLinuxFB, nil
	default:
		return "", fmt.Errorf("unknown gfx backend %q (want sdl, software, fpga-stub, or linuxfb)", name)
	}
}
