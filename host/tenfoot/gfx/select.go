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
	default:
		return "", fmt.Errorf("unknown gfx backend %q (want sdl, software, or fpga-stub)", name)
	}
}
