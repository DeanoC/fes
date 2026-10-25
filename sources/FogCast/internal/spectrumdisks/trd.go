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
	trdosMaxFiles     = 128
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

// trdos is a TR-DOS disk as its logical tracks: the bytes of a .trd image.
type trdos struct {
	data             []byte // cylinders*sides tracks
	cylinders, sides int
}

// trdosFromTRD takes a .trd's geometry from its descriptor, extended to the
// cylinders a longer image needs (up to 83), and zero-fills a short image,
// as Fuse's open_trd does.
func trdosFromTRD(data []byte) *trdos {
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
	logical := make([]byte, sides*cylinders*trdosTrackBytes)
	copy(logical, data)
	return &trdos{data: logical, cylinders: cylinders, sides: sides}
}

// trdosFromSCL builds the TR-DOS directory and descriptor for the image's
// files on an 80-cylinder double-sided disk, as Fuse's open_scl does.
func trdosFromSCL(data []byte) (*trdos, error) {
	files := int(data[len(sclMagic)])
	entries := data[len(sclMagic)+1:]
	if files < 1 || files > trdosMaxFiles || len(entries) < 14*files {
		return nil, fmt.Errorf("spectrum disk: .scl directory of %d files is invalid", files)
	}
	d := &trdos{data: make([]byte, 160*trdosTrackBytes), cylinders: 80, sides: 2}
	used, deleted := 0, 0
	for i := 0; i < files; i++ {
		entry := d.data[16*i:]
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
	info := d.data[trdosInfo:][:trdosSectorBytes]
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
	copy(d.data[trdosTrackBytes:], entries[14*files:])
	return d, nil
}

// addBootLoader gives a disk without a "boot" BASIC file one that runs its
// first BASIC program, as Fuse's disk autoload does: line 1, RANDOMIZE USR
// 15619: REM : RUN "name", which TR-DOS's command entry runs. It is stored
// as the next file, with the usual end marker and autostart line, when the
// directory and the disk have room.
func (d *trdos) addBootLoader() {
	info := d.data[trdosInfo:][:trdosSectorBytes]
	files := int(info[228])
	free := int(info[229]) | int(info[230])<<8
	var first []byte
	for i := 0; i < trdosMaxFiles; i++ {
		entry := d.data[16*i:][:16]
		if entry[0] <= 0x01 || entry[8] != 'B' {
			continue
		}
		if string(entry[:8]) == "boot    " {
			return
		}
		if first == nil {
			first = entry[:8]
		}
	}
	if first == nil || files >= trdosMaxFiles {
		return
	}
	program := []byte{0x00, 0x01, 0, 0, // line 1, length below
		0xf9, 0xc0, '1', '5', '6', '1', '9', 0x0e, 0x00, 0x00, 0x03, 0x3d, 0x00, // RANDOMIZE USR 15619
		':', 0xea, ':', 0xf7, '"'} // : REM : RUN "
	program = append(append(program, first...), '"', 0x0d)
	program[2] = byte(len(program) - 4)
	body := append(append([]byte(nil), program...), 0x80, 0xaa, 0x01, 0x00) // end, autostart line 1
	sectors := (len(body) + trdosSectorBytes - 1) / trdosSectorBytes
	start := int(info[226])*trdosSectors + int(info[225])
	if free < sectors || (start+sectors)*trdosSectorBytes > len(d.data) {
		return
	}
	file := d.data[start*trdosSectorBytes:][:sectors*trdosSectorBytes]
	clear(file)
	copy(file, body)
	entry := d.data[16*files:][:16]
	copy(entry, "boot    B")
	entry[9], entry[10] = byte(len(program)), byte(len(program)>>8)
	entry[11], entry[12] = byte(len(program)), byte(len(program)>>8)
	entry[13] = byte(sectors)
	entry[14], entry[15] = info[225], info[226]
	next := start + sectors
	info[225], info[226] = byte(next%trdosSectors), byte(next/trdosSectors)
	info[228] = byte(files + 1)
	free -= sectors
	info[229], info[230] = byte(free), byte(free>>8)
}

// disk lays the logical tracks out as MFM tracks.
func (d *trdos) disk() *Disk {
	disk := newDisk(d.cylinders, d.sides)
	for t := 0; t < d.cylinders*d.sides; t++ {
		track := d.data[t*trdosTrackBytes:][:trdosTrackBytes]
		disk.Tracks[t/d.sides][t%d.sides] = formatTrack(t/d.sides, t%d.sides, trdosSectors, trdosSectorBytes,
			trdosGaps, func(s int) []byte { return track[s*trdosSectorBytes:][:trdosSectorBytes] })
	}
	return disk
}
