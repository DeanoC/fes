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
	BackendFPGA     = "fpga"
	BackendFPGAStub = "fpga-stub"
	BackendLinuxFB  = "linuxfb"
)

// ParseBackend maps a name or TENFOOT_GFX value to a canonical backend.
// Empty, "sdl", and "sdl3" select the production SDL3 path. Unknown names
// return an error so a typo cannot silently change the sofa renderer.
// "fpga" is the FC2D command-stream Device; "fpga-stub" remains the thin
// Software wrapper without a stream.
func ParseBackend(name string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "sdl", "sdl3":
		return BackendSDL, nil
	case "software", "sw":
		return BackendSoftware, nil
	case "fpga", "fpga-recorder":
		return BackendFPGA, nil
	case "fpga-stub", "stub":
		return BackendFPGAStub, nil
	case "linuxfb", "fb", "fb0":
		return BackendLinuxFB, nil
	default:
		return "", fmt.Errorf("unknown gfx backend %q (want sdl, software, fpga, fpga-stub, or linuxfb)", name)
	}
}

// OpenCPU constructs a CGO-free Device: software, fpga, or fpga-stub.
// SDL and linuxfb need their own constructors.
func OpenCPU(name string, logicalW, logicalH int) (Device, error) {
	backend, err := ParseBackend(name)
	if err != nil {
		return nil, err
	}
	switch backend {
	case BackendSoftware:
		return NewSoftware(logicalW, logicalH)
	case BackendFPGA:
		return NewFPGA(logicalW, logicalH)
	case BackendFPGAStub:
		return NewFPGAStub(logicalW, logicalH)
	default:
		return nil, fmt.Errorf("gfx backend %q is not a CPU device", backend)
	}
}
