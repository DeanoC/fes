// Package spectrumdisks reads ZX Spectrum disk images and writes the raw
// track image that fes.media.spectrum-disk 1.0 cores read.
//
// A raw track image holds what a floppy controller sees under the head: the
// bytes of every MFM track in rotation order, from the index hole, with the
// bytes written with a missing clock (the A1 sync of every MFM address mark,
// or an FM track's clocked mark bytes) flagged. The core's controller decodes gaps, address marks, IDs, data and
// CRCs itself, so Read Address, Read Track and odd layouts behave as they do
// on a real drive. Tracks are built the way Fuse's disk.c builds them.
//
// The image is a sequence of slots of 8 KiB, or 16 KiB when a track is longer
// than an 8 KiB slot holds. Slot 0 is the header. Cylinder c, side s is slot
// 1+2c+s; an image may end after its last formatted slot, and a slot past the
// end, or one whose length is zero, is an unformatted track.
//
//	header slot: "ZXDK", version 1, cylinders, sides, flags (bit 0 write
//	             protect, bit 1 boot), the slot size as a power of two (13
//	             or 14), zero to the end of the slot
//
// The host sets boot on the disk a launch inserts: a core that finds it
// when the disk becomes ready resets into its disk system and boots the
// disk. A disk changed while the machine runs leaves it clear, so the
// running software sees only a new medium.
//
//	track slot:  bytes 0-1 track length in bytes (little endian, 0 when
//	             unformatted), byte 2 flags (bit 0 an FM track), 3-15 zero,
//	             then from byte 16 the missing-clock bitmap (bit i&7 of byte
//	             16+i/8 flags track byte i); the track bytes start at D, an
//	             eighth of the slot (1024 or 2048), and D-144 holds up to
//	             eight weak ranges (start and length, little-endian 16-bit;
//	             a zero length ends the list). The rest is zero.
//
// A weak range's bytes read differently each time, as the sectors of copy
// protections do; a .dsk image gives them as several stored copies of a
// sector, as Fuse reads them.
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
	// SlotBytes is the size of the header and of every track slot of an
	// image whose tracks fit it; LargeSlotBytes holds longer tracks.
	SlotBytes      = 8 << 10
	LargeSlotBytes = 16 << 10
	// MaxTrackBytes is the longest track a slot's bitmap covers.
	MaxTrackBytes = (LargeSlotBytes/8 - trackClocks - 144) * 8
	// MaxWeakRanges is how many weak ranges a track slot holds.
	MaxWeakRanges = 8
	// MaxCylinders bounds the cylinders an image describes.
	MaxCylinders = 86
	// MaxImageBytes is the largest image: the header and two sides of
	// MaxCylinders cylinders.
	MaxImageBytes = (1 + 2*MaxCylinders) * LargeSlotBytes
	// MaxInputBytes bounds a disk image before it is read.
	MaxInputBytes = 1 << 20

	trackClocks = 16
	// ddBytes is a double-density track at 250 kbit/s and 300 rpm.
	ddBytes = 6250
)

// Magic starts every raw track image.
var Magic = [4]byte{'Z', 'X', 'D', 'K'}

// Extensions are the disk image names Convert reads, for every system.
var Extensions = []string{".trd", ".scl", ".dsk"}

// Admit reports whether name has a disk image extension.
func Admit(name string) bool {
	return admit(Extensions, name)
}

func admit(extensions []string, name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	for _, e := range extensions {
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
	Weak   []bool // Weak[i]: Bytes[i] reads at random; nil for none
	FM     bool   // single density
}

// slotTrackBytes is the longest track a slot of the given size holds.
func slotTrackBytes(slot int) int { return (slot/8 - trackClocks - 144) * 8 }

// weakRanges returns the track's runs of weak bytes.
func (t *Track) weakRanges() [][2]int {
	var ranges [][2]int
	for i := 0; i < len(t.Weak); i++ {
		if !t.Weak[i] {
			continue
		}
		j := i
		for j < len(t.Weak) && t.Weak[j] {
			j++
		}
		ranges = append(ranges, [2]int{i, j - i})
		i = j
	}
	return ranges
}

// System is the disk system a disk image is written for.
type System int

const (
	// TRDOS is a Beta 128 TR-DOS disk (.trd, .scl).
	TRDOS System = iota
	// Plus3 is a +3 disk (.dsk).
	Plus3
)

// Extensions are the image names a disk of the system is read from.
func (s System) Extensions() []string {
	if s == Plus3 {
		return []string{".dsk"}
	}
	return []string{".trd", ".scl"}
}

// Admit reports whether name has one of the system's extensions.
func (s System) Admit(name string) bool {
	return admit(s.Extensions(), name)
}

// Disk is a raw track disk.
type Disk struct {
	Cylinders, Sides int
	WriteProtect     bool
	Boot             bool
	System           System
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
				if s >= d.Sides || len(t.Bytes) == 0 || len(t.Bytes) > MaxTrackBytes || len(t.Clocks) != len(t.Bytes) ||
					(t.Weak != nil && len(t.Weak) != len(t.Bytes)) {
					return nil, fmt.Errorf("spectrum disk: track %d side %d is outside the image", c, s)
				}
				if len(t.weakRanges()) > MaxWeakRanges {
					return nil, fmt.Errorf("spectrum disk: track %d side %d has more than %d weak ranges", c, s, MaxWeakRanges)
				}
				last = 1 + 2*c + s
			}
		}
	}
	slotBytes, shift := SlotBytes, byte(13)
	for c := range d.Tracks {
		for s := 0; s < 2; s++ {
			if t := d.Tracks[c][s]; t != nil && len(t.Bytes) > slotTrackBytes(SlotBytes) {
				slotBytes, shift = LargeSlotBytes, 14
			}
		}
	}
	image := make([]byte, (last+1)*slotBytes)
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
	image[8] = shift
	data := slotBytes / 8
	for c := range d.Tracks {
		for s := 0; s < 2; s++ {
			t := d.Tracks[c][s]
			if t == nil {
				continue
			}
			slot := image[(1+2*c+s)*slotBytes:][:slotBytes]
			binary.LittleEndian.PutUint16(slot, uint16(len(t.Bytes)))
			for i, clock := range t.Clocks {
				if clock {
					slot[trackClocks+i/8] |= 1 << (i % 8)
				}
			}
			if t.FM {
				slot[2] = 1
			}
			for k, r := range t.weakRanges() {
				binary.LittleEndian.PutUint16(slot[data-144+4*k:], uint16(r[0]))
				binary.LittleEndian.PutUint16(slot[data-144+4*k+2:], uint16(r[1]))
			}
			copy(slot[data:], t.Bytes)
		}
	}
	return image, nil
}

// ErrNotDisk reports data that is not a disk image this package reads.
var ErrNotDisk = errors.New("spectrum disk: not a TR-DOS .trd or .scl, or +3 .dsk, image")

// ErrWrongSystem reports a disk image written for another disk system.
var ErrWrongSystem = errors.New("spectrum disk: the image is for another disk system")

// Convert reads a disk image written for system and returns its raw track
// image. A launch image carries the boot flag, and a TR-DOS disk without a
// "boot" program gets one that runs its first BASIC program, as Fuse's disk
// autoload adds.
func Convert(data []byte, system System, boot bool) ([]byte, error) {
	if isDSK(data) {
		if system != Plus3 {
			return nil, ErrWrongSystem
		}
		d, err := readDSK(data)
		if err != nil {
			return nil, err
		}
		d.Boot = boot
		return d.Image()
	}
	t, err := readTRDOS(data)
	if err != nil {
		return nil, err
	}
	if system != TRDOS {
		return nil, ErrWrongSystem
	}
	if boot {
		t.addBootLoader()
	}
	d := t.disk()
	d.Boot = boot
	return d.Image()
}

// Read reads a .trd, .scl or .dsk image as it is.
func Read(data []byte) (*Disk, error) {
	if len(data) <= MaxInputBytes && isDSK(data) {
		d, err := readDSK(data)
		if d != nil {
			d.System = Plus3
		}
		return d, err
	}
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
