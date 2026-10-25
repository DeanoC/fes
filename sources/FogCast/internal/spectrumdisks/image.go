// Package spectrumdisks reads ZX Spectrum disk images and writes the raw
// track image that fes.media.spectrum-disk 1.0 cores read.
//
// A raw track image holds what a floppy controller sees under the head: the
// bytes of every MFM track in rotation order, from the index hole, with the
// bytes written with a missing clock (the A1 sync of every address mark)
// flagged. The core's controller decodes gaps, address marks, IDs, data and
// CRCs itself, so Read Address, Read Track and odd layouts behave as they do
// on a real drive. Tracks are built the way Fuse's disk.c builds them.
//
// The image is a sequence of 8 KiB slots. Slot 0 is the header. Cylinder c,
// side s is slot 1+2c+s; an image may end after its last formatted slot, and
// a slot past the end, or one whose length is zero, is an unformatted track.
//
//	header slot: "ZXDK", version 1, cylinders, sides, flags (bit 0 write
//	             protect, bit 1 boot), zero to the end of the slot
//
// The host sets boot on the disk a launch inserts: a core that finds it
// when the disk becomes ready resets into its disk system and boots the
// disk. A disk changed while the machine runs leaves it clear, so the
// running software sees only a new medium.
//
//	track slot:  bytes 0-1 track length in bytes (little endian, 0 when
//	             unformatted), 2-15 zero, 16-879 the missing-clock bitmap
//	             (bit i&7 of byte 16+i/8 flags track byte i), 880-1023
//	             zero, 1024 on the track bytes, zero after them
package spectrumdisks

import (
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	// Version is the raw track image format this package writes.
	Version = 1
	// SlotBytes is the size of the header and of every track slot.
	SlotBytes = 8 << 10
	// MaxTrackBytes is the longest track a slot's bitmap covers.
	MaxTrackBytes = (trackData - trackClocks - 144) * 8
	// MaxCylinders bounds the cylinders an image describes.
	MaxCylinders = 86
	// MaxImageBytes is the largest image: the header and two sides of
	// MaxCylinders cylinders.
	MaxImageBytes = (1 + 2*MaxCylinders) * SlotBytes
	// MaxInputBytes bounds a disk image before it is read.
	MaxInputBytes = 1 << 20

	trackClocks = 16
	trackData   = 1024
	// ddBytes is a double-density track at 250 kbit/s and 300 rpm.
	ddBytes = 6250
)

// Magic starts every raw track image.
var Magic = [4]byte{'Z', 'X', 'D', 'K'}

// Extensions are the disk image names Convert reads.
var Extensions = []string{".trd", ".scl"}

// Admit reports whether name has a disk image extension.
func Admit(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	for _, e := range Extensions {
		if ext == e {
			return true
		}
	}
	return false
}

// Track is one side of one cylinder as the head reads it from the index.
type Track struct {
	Bytes  []byte
	Clocks []bool // Clocks[i]: Bytes[i] was written with a missing clock
}

// Disk is a raw track disk.
type Disk struct {
	Cylinders, Sides int
	WriteProtect     bool
	Boot             bool
	Tracks           [][2]*Track // [cylinder][side]; nil is unformatted
}

func newDisk(cylinders, sides int) *Disk {
	return &Disk{Cylinders: cylinders, Sides: sides, Tracks: make([][2]*Track, cylinders)}
}

// Image encodes the disk as a raw track image.
func (d *Disk) Image() ([]byte, error) {
	if d.Cylinders < 1 || d.Cylinders > MaxCylinders || d.Sides < 1 || d.Sides > 2 ||
		len(d.Tracks) != d.Cylinders {
		return nil, fmt.Errorf("spectrum disk: %d cylinders and %d sides are outside the image", d.Cylinders, d.Sides)
	}
	last := 0
	for c := range d.Tracks {
		for s := 0; s < 2; s++ {
			if t := d.Tracks[c][s]; t != nil {
				if s >= d.Sides || len(t.Bytes) == 0 || len(t.Bytes) > MaxTrackBytes || len(t.Clocks) != len(t.Bytes) {
					return nil, fmt.Errorf("spectrum disk: track %d side %d is outside the image", c, s)
				}
				last = 1 + 2*c + s
			}
		}
	}
	image := make([]byte, (last+1)*SlotBytes)
	copy(image, Magic[:])
	image[4] = Version
	image[5] = byte(d.Cylinders)
	image[6] = byte(d.Sides)
	if d.WriteProtect {
		image[7] |= 1
	}
	if d.Boot {
		image[7] |= 2
	}
	for c := range d.Tracks {
		for s := 0; s < 2; s++ {
			t := d.Tracks[c][s]
			if t == nil {
				continue
			}
			slot := image[(1+2*c+s)*SlotBytes:][:SlotBytes]
			binary.LittleEndian.PutUint16(slot, uint16(len(t.Bytes)))
			for i, clock := range t.Clocks {
				if clock {
					slot[trackClocks+i/8] |= 1 << (i % 8)
				}
			}
			copy(slot[trackData:], t.Bytes)
		}
	}
	return image, nil
}

// ErrNotDisk reports data that is not a disk image this package reads.
var ErrNotDisk = errors.New("spectrum disk: not a TR-DOS .trd or .scl image")

// Convert reads a .trd or .scl image and returns its raw track image. A
// launch image carries the boot flag, and a disk without a "boot" program
// gets one that runs its first BASIC program, as Fuse's disk autoload adds.
func Convert(data []byte, boot bool) ([]byte, error) {
	t, err := readTRDOS(data)
	if err != nil {
		return nil, err
	}
	if boot {
		t.addBootLoader()
	}
	d := t.disk()
	d.Boot = boot
	return d.Image()
}

// Read reads a .trd or .scl image as it is.
func Read(data []byte) (*Disk, error) {
	t, err := readTRDOS(data)
	if err != nil {
		return nil, err
	}
	return t.disk(), nil
}

func readTRDOS(data []byte) (*trdos, error) {
	if len(data) > MaxInputBytes {
		return nil, fmt.Errorf("spectrum disk: %d bytes is larger than %d", len(data), MaxInputBytes)
	}
	if isSCL(data) {
		return trdosFromSCL(data)
	}
	if isTRD(data) {
		return trdosFromTRD(data), nil
	}
	return nil, ErrNotDisk
}
