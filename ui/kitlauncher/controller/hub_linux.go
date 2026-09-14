//go:build linux

package controller

import (
	"errors"
	"path/filepath"

	"github.com/DeanoC/FogCast/ui/tenfoot/inputmap"
)

// Open discovers every eligible USB gamepad and returns a multiplexing Hub
// with the identity profile. js* nodes and the FogCast virtual pad are skipped.
func Open() (*Hub, error) {
	return OpenWith(inputmap.IdentityRemapper())
}

// OpenWith is Open using an explicit remapper.
func OpenWith(remap *inputmap.Remapper) (*Hub, error) {
	if remap == nil {
		remap = inputmap.IdentityRemapper()
	}
	paths, err := filepath.Glob("/dev/input/event*")
	if err != nil {
		return nil, err
	}
	var pads []padSource
	for _, path := range paths {
		d, err := openDevice(path)
		if err != nil {
			continue
		}
		d.remap = remap
		pads = append(pads, d)
	}
	if len(pads) == 0 {
		return nil, errors.New("connect a USB gamepad")
	}
	h := NewHub(remap, pads)
	h.rescan = linuxRescan
	return h, nil
}

func linuxRescan(h *Hub) {
	paths, err := filepath.Glob("/dev/input/event*")
	if err != nil {
		return
	}
	for _, path := range paths {
		if h.hasID(path) {
			continue
		}
		d, err := openDevice(path)
		if err != nil {
			continue
		}
		h.add(d)
	}
}
