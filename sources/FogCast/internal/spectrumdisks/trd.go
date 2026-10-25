package spectrumdisks

import (
	"bytes"
	"fmt"
)

// TR-DOS disks: 16 sectors of 256 bytes a track. Track 0 sector 9 holds the
// disk descriptor: byte 227 the disk type (0x16 80 cylinders double sided,
// 0x17 40 double, 0x18 80 single, 0x19 40 single) and byte 231 the TR-DOS
// ID 0x10. A .trd image is the logical tracks in order (cylinder by
// cylinder, side 0 then side 1 on a double-sided disk). A .scl image is
// "SINCLAIR", a file count, 14-byte directory entries and the files'
// sectors, which a disk lays out from track 1 sector 1.

const (
	trdosSectors      = 16
	trdosSectorBytes  = 256
	trdosTrackBytes   = trdosSectors * trdosSectorBytes
	trdosInfo         = 8 * trdosSectorBytes // sector 9
	trdosID           = 0x10
	trdosMaxCylinders = 83
)

var sclMagic = []byte("SINCLAIR")

func isTRD(data []byte) bool {
	if len(data) < trdosInfo+trdosSectorBytes {
		return false
	}
	info := data[trdosInfo:]
	return info[231] == trdosID && info[227] >= 0x16 && info[227] <= 0x19
}

func isSCL(data []byte) bool {
	return len(data) > len(sclMagic) && bytes.HasPrefix(data, sclMagic)
}

// sectorsFrom serves 256-byte sectors from data in order, zero-filled past
// its end, as Fuse's trackgen autofills a short image.
type sectorsFrom struct{ data []byte }

func (r *sectorsFrom) next() []byte {
	sector := make([]byte, trdosSectorBytes)
	n := copy(sector, r.data)
	r.data = r.data[n:]
	return sector
}

func (r *sectorsFrom) track(d *Disk, cylinder, side int) {
	sectors := make([][]byte, trdosSectors)
	for s := range sectors {
		sectors[s] = r.next()
	}
	d.Tracks[cylinder][side] = formatTrack(cylinder, side, trdosSectors, trdosSectorBytes, trdosGaps,
		func(s int) []byte { return sectors[s] })
}

// readTRD lays a .trd out on the geometry its descriptor names, extended
// to the cylinders a longer image needs (up to 83), as Fuse's open_trd does.
func readTRD(data []byte) (*Disk, error) {
	kind := data[trdosInfo+227]
	sides := 2
	if kind&0x08 != 0 {
		sides = 1
	}
	cylinders := 80
	if kind&0x01 != 0 {
		cylinders = 40
	}
	if len(data) > sides*cylinders*trdosTrackBytes {
		for cylinders++; cylinders < trdosMaxCylinders; cylinders++ {
			if sides*cylinders*trdosTrackBytes >= len(data) {
				break
			}
		}
	}
	d := newDisk(cylinders, sides)
	r := &sectorsFrom{data: data}
	for c := 0; c < cylinders; c++ {
		for s := 0; s < sides; s++ {
			r.track(d, c, s)
		}
	}
	return d, nil
}

// readSCL builds the TR-DOS directory and descriptor for the image's files
// on an 80-cylinder double-sided disk, as Fuse's open_scl does.
func readSCL(data []byte) (*Disk, error) {
	files := int(data[len(sclMagic)])
	entries := data[len(sclMagic)+1:]
	if files < 1 || files > 128 || len(entries) < 14*files {
		return nil, fmt.Errorf("spectrum disk: .scl directory of %d files is invalid", files)
	}
	directory := make([]byte, 8*trdosSectorBytes)
	used, deleted := 0, 0
	for i := 0; i < files; i++ {
		entry := directory[16*i:]
		copy(entry, entries[14*i:14*i+14])
		entry[14] = byte(used % trdosSectors)
		entry[15] = byte(used/trdosSectors + 1)
		used += int(entry[13])
		if entry[0] == 0x01 {
			deleted++
		}
		if used > trdosSectors*159 {
			return nil, fmt.Errorf("spectrum disk: .scl files need %d sectors, more than a disk holds", used)
		}
	}
	info := make([]byte, trdosSectorBytes)
	info[225] = byte(used % trdosSectors)
	info[226] = byte(used/trdosSectors + 1)
	info[227] = 0x16
	info[228] = byte(files)
	free := 2544 - used
	info[229], info[230] = byte(free), byte(free>>8)
	info[231] = trdosID
	copy(info[234:243], "         ")
	info[244] = byte(deleted)
	copy(info[245:253], "FUSE-SCL")

	d := newDisk(80, 2)
	d.Tracks[0][0] = formatTrack(0, 0, trdosSectors, trdosSectorBytes, trdosGaps, func(s int) []byte {
		switch {
		case s < 8:
			return directory[s*trdosSectorBytes:][:trdosSectorBytes]
		case s == 8:
			return info
		default:
			return make([]byte, trdosSectorBytes)
		}
	})
	r := &sectorsFrom{data: entries[14*files:]}
	for t := 1; t < 160; t++ {
		r.track(d, t/2, t%2)
	}
	return d, nil
}
