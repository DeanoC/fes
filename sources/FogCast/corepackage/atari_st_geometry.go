package corepackage

import "encoding/binary"

// Geometry is an opt-in extension of the raw ST image contract. Its physical
// shape is determined by length; compressed and flux images are not admitted.
const AtariStGeometryInterfaceID = "fes.media.atari-st-floppy-geometry"
const AtariStGeometryMinBytes = 80 * 1 * 9 * 512
const AtariStGeometryMaxBytes = 82 * 2 * 10 * 512

type AtariStGeometry struct{ Tracks, Heads, Sectors int }

func AtariStGeometryForSize(size int64) (AtariStGeometry, bool) {
	for tracks := 80; tracks <= 82; tracks++ {
		for heads := 1; heads <= 2; heads++ {
			for sectors := 9; sectors <= 10; sectors++ {
				if size == int64(tracks*heads*sectors*512) {
					return AtariStGeometry{tracks, heads, sectors}, true
				}
			}
		}
	}
	return AtariStGeometry{}, false
}

func DeclaresAtariStGeometry(d Descriptor) bool {
	if d.ABI != (Contract{ID: "fes.computer", Major: 1}) {
		return false
	}
	for _, i := range d.Interfaces {
		if i.ID == AtariStGeometryInterfaceID && i.Major == 1 && i.Minor == 0 && i.Required {
			return true
		}
	}
	return false
}

// ValidAtariStBase checks immutable source bytes. The original 720 KiB
// contract deliberately permits arbitrary boot sectors. Other layouts need
// a matching BPB; durable mutable snapshots retain their admitted geometry.
func ValidAtariStBase(data []byte, extended bool) bool {
	if len(data) == InitialMediaBytes {
		return true
	}
	if !extended {
		return false
	}
	g, ok := AtariStGeometryForSize(int64(len(data)))
	if !ok || len(data) < 28 {
		return false
	}
	le := binary.LittleEndian
	return le.Uint16(data[11:13]) == 512 && int(le.Uint16(data[19:21])) == len(data)/512 && int(le.Uint16(data[24:26])) == g.Sectors && int(le.Uint16(data[26:28])) == g.Heads
}
