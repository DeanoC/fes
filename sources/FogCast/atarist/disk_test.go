package atarist

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func TestBlankDiskMountsEmpty(t *testing.T) {
	for _, geometry := range Layouts() {
		geometry := geometry
		t.Run(geometryName(geometry.Tracks, geometry.Heads, geometry.Sectors), func(t *testing.T) {
			disk, err := Format("Work", geometry.Tracks, geometry.Heads, geometry.Sectors)
			if err != nil {
				t.Fatal(err)
			}
			if disk.Name != "WORK" || len(disk.Bytes) != geometry.Tracks*geometry.Heads*geometry.Sectors*512 {
				t.Fatalf("disk %+v size %d", disk.Name, len(disk.Bytes))
			}
			if wordSum(disk.Bytes[:512]) == 0x1234 {
				t.Fatal("boot sector is executable")
			}
			checkEmptyFAT12(t, disk.Bytes, geometry.Tracks, geometry.Heads, geometry.Sectors, "WORK")
		})
	}
}

func TestBlankDiskAcceptsAnIndependentWrite(t *testing.T) {
	for _, spec := range []struct {
		tracks, heads, sectors int
	}{{80, 2, 9}, {82, 1, 10}} {
		disk, err := Format("notes", spec.tracks, spec.heads, spec.sectors)
		if err != nil {
			t.Fatal(err)
		}
		payload := []byte("saved-work\n")
		writeTestFAT12File(t, disk.Bytes, "NOTE    TXT", payload)
		got := readTestFAT12File(t, disk.Bytes, "NOTE    TXT")
		if !bytes.Equal(got, payload) {
			t.Fatalf("read %q, want %q", got, payload)
		}
	}
}

func TestBlankDiskRejectsBadNamesAndShapes(t *testing.T) {
	if _, err := Format("", 80, 2, 9); err != ErrName {
		t.Fatalf("empty name: %v", err)
	}
	if _, err := Format("too-long-name", 80, 2, 9); err != ErrName {
		t.Fatalf("long name: %v", err)
	}
	if _, err := Format("bad/name", 80, 2, 9); err != ErrName {
		t.Fatalf("slash: %v", err)
	}
	if _, err := Format("ok", 40, 2, 9); err != ErrGeometry {
		t.Fatalf("40-track: %v", err)
	}
	if _, err := Format("ok", 80, 2, 0); err != ErrGeometry {
		t.Fatalf("partial: %v", err)
	}
	disk, err := Format("  desk ", 0, 0, 0)
	if err != nil || disk.Tracks != 80 || disk.Heads != 2 || disk.Sectors != 9 || disk.Name != "DESK" || !bytes.Contains(disk.Bytes, []byte("DESK")) {
		t.Fatalf("default disk: %+v %v", disk, err)
	}
}

type testFAT12Layout struct {
	bytesPerSector       int
	sectorsPerCluster    int
	reservedSectors      int
	fatCount             int
	rootEntries          int
	sectorsPerFAT        int
	fatStart, fatSize    int
	rootStart, rootSize  int
	dataStart, clusterSz int
}

func testFAT12LayoutFor(t *testing.T, image []byte) testFAT12Layout {
	t.Helper()
	if len(image) < 512 {
		t.Fatal("short boot sector")
	}
	boot := image[:512]
	l := testFAT12Layout{
		bytesPerSector:    int(binary.LittleEndian.Uint16(boot[11:13])),
		sectorsPerCluster: int(boot[13]),
		reservedSectors:   int(binary.LittleEndian.Uint16(boot[14:16])),
		fatCount:          int(boot[16]),
		rootEntries:       int(binary.LittleEndian.Uint16(boot[17:19])),
		sectorsPerFAT:     int(binary.LittleEndian.Uint16(boot[22:24])),
	}
	if l.bytesPerSector != 512 || l.sectorsPerCluster == 0 || l.reservedSectors == 0 || l.fatCount == 0 || l.rootEntries == 0 || l.sectorsPerFAT == 0 {
		t.Fatalf("invalid BPB: %+v", l)
	}
	l.fatStart = l.reservedSectors * l.bytesPerSector
	l.fatSize = l.sectorsPerFAT * l.bytesPerSector
	l.rootStart = l.fatStart + l.fatCount*l.fatSize
	l.rootSize = ((l.rootEntries*32 + l.bytesPerSector - 1) / l.bytesPerSector) * l.bytesPerSector
	l.dataStart = l.rootStart + l.rootSize
	l.clusterSz = l.sectorsPerCluster * l.bytesPerSector
	if l.dataStart > len(image) || l.dataStart%l.bytesPerSector != 0 || len(image)%l.bytesPerSector != 0 {
		t.Fatalf("BPB regions exceed image: %+v image size %d", l, len(image))
	}
	return l
}

func checkEmptyFAT12(t *testing.T, image []byte, tracks, heads, sectors int, label string) {
	t.Helper()
	l := testFAT12LayoutFor(t, image)
	boot := image[:l.bytesPerSector]
	if l.sectorsPerCluster != 2 || l.reservedSectors != 1 || l.fatCount != 2 || l.rootEntries != 112 || l.sectorsPerFAT != 5 || boot[21] != mediaDescriptor ||
		int(binary.LittleEndian.Uint16(boot[19:21])) != len(image)/l.bytesPerSector ||
		int(binary.LittleEndian.Uint16(boot[24:26])) != sectors ||
		int(binary.LittleEndian.Uint16(boot[26:28])) != heads ||
		len(image) != tracks*heads*sectors*l.bytesPerSector {
		t.Fatalf("BPB geometry does not match image: tracks=%d heads=%d sectors=%d size=%d", tracks, heads, sectors, len(image))
	}
	fat := image[l.fatStart : l.fatStart+l.fatSize]
	if !bytes.Equal(fat, image[l.fatStart+l.fatSize:l.fatStart+2*l.fatSize]) {
		t.Fatal("FAT copies differ")
	}
	if !bytes.Equal(fat[:3], []byte{mediaDescriptor, 0xff, 0xff}) || !allZero(fat[3:]) {
		t.Fatal("FAT does not contain only its reserved entries")
	}
	root := image[l.rootStart : l.rootStart+l.rootEntries*32]
	wantLabel := []byte(label + strings.Repeat(" ", 11-len(label)))
	if !bytes.Equal(root[:11], wantLabel) || root[11] != 0x08 {
		t.Fatalf("volume label entry = %q attr %#x", root[:11], root[11])
	}
	if !allZero(root[32:]) {
		t.Fatal("root directory is not empty after its volume label")
	}
	if !allZero(image[l.dataStart:]) {
		t.Fatal("blank disk data area is not empty")
	}
}

func writeTestFAT12File(t *testing.T, image []byte, name string, payload []byte) {
	t.Helper()
	l := testFAT12LayoutFor(t, image)
	if len(name) != 11 {
		t.Fatal("test filename must use an 11-byte FAT 8.3 name")
	}
	if len(payload) > l.clusterSz {
		t.Fatal("test payload must fit in one cluster")
	}
	root := image[l.rootStart : l.rootStart+l.rootEntries*32]
	entryOffset := -1
	for i := 0; i < len(root); i += 32 {
		if root[i] == 0 {
			entryOffset = i
			break
		}
	}
	if entryOffset < 0 {
		t.Fatal("no free root directory entry")
	}
	const cluster = 2
	for copyIndex := 0; copyIndex < l.fatCount; copyIndex++ {
		fat := image[l.fatStart+copyIndex*l.fatSize : l.fatStart+(copyIndex+1)*l.fatSize]
		if getTestFAT12Entry(fat, cluster) != 0 {
			t.Fatal("test cluster is already allocated")
		}
		setTestFAT12Entry(t, fat, cluster, 0x0fff)
	}
	entry := root[entryOffset : entryOffset+32]
	copy(entry[:11], name)
	entry[11] = 0x20
	binary.LittleEndian.PutUint16(entry[26:28], cluster)
	binary.LittleEndian.PutUint32(entry[28:32], uint32(len(payload)))
	dataStart := l.dataStart + (cluster-2)*l.clusterSz
	copy(image[dataStart:dataStart+l.clusterSz], payload)
}

func readTestFAT12File(t *testing.T, image []byte, name string) []byte {
	t.Helper()
	l := testFAT12LayoutFor(t, image)
	root := image[l.rootStart : l.rootStart+l.rootEntries*32]
	for i := 0; i < len(root); i += 32 {
		entry := root[i : i+32]
		if entry[0] == 0 {
			break
		}
		if entry[0] == 0xe5 || entry[11]&0x08 != 0 || string(entry[:11]) != name {
			continue
		}
		cluster := int(binary.LittleEndian.Uint16(entry[26:28]))
		size := int(binary.LittleEndian.Uint32(entry[28:32]))
		if cluster < 2 || size > l.clusterSz {
			t.Fatalf("invalid test file cluster=%d size=%d", cluster, size)
		}
		fat := image[l.fatStart : l.fatStart+l.fatSize]
		if getTestFAT12Entry(fat, cluster) != 0x0fff {
			t.Fatal("test file cluster is not marked end-of-chain")
		}
		start := l.dataStart + (cluster-2)*l.clusterSz
		return bytes.Clone(image[start : start+size])
	}
	t.Fatal("test file not found in root directory")
	return nil
}

func getTestFAT12Entry(fat []byte, cluster int) uint16 {
	offset := cluster + cluster/2
	value := uint16(fat[offset]) | uint16(fat[offset+1])<<8
	if cluster&1 == 0 {
		return value & 0x0fff
	}
	return value >> 4
}

func setTestFAT12Entry(t *testing.T, fat []byte, cluster int, value uint16) {
	t.Helper()
	offset := cluster + cluster/2
	if offset+1 >= len(fat) {
		t.Fatal("FAT entry outside table")
	}
	if cluster&1 == 0 {
		fat[offset] = byte(value)
		fat[offset+1] = fat[offset+1]&0xf0 | byte(value>>8)&0x0f
		return
	}
	fat[offset] = fat[offset]&0x0f | byte(value<<4)
	fat[offset+1] = byte(value >> 4)
}

func allZero(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return false
		}
	}
	return true
}

func geometryName(tracks, heads, sectors int) string {
	return strings.Join([]string{
		itoa(tracks), itoa(heads), itoa(sectors),
	}, "-")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [4]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
