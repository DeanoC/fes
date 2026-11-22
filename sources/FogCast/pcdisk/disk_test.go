package pcdisk

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func TestBlankIsAnEmpty360KDOSDisk(t *testing.T) {
	disk, err := Blank("Work disk 2", [4]byte{1, 2, 3, 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(disk) != Size || Size != 368640 || !dosDisk(disk) {
		t.Fatalf("not a 360 KiB DOS disk: %d bytes", len(disk))
	}
	if binary.LittleEndian.Uint16(disk[24:]) != 9 || binary.LittleEndian.Uint16(disk[26:]) != 2 {
		t.Fatal("geometry is not nine sectors on two sides")
	}
	if got := string(disk[0x2B:0x36]); got != "WORK DISK 2" || !bytes.Equal(disk[0x27:0x2B], []byte{1, 2, 3, 4}) {
		t.Fatalf("boot record label %q serial % x", got, disk[0x27:0x2B])
	}
	if got := string(disk[rootOffset : rootOffset+11]); got != "WORK DISK 2" || disk[rootOffset+11] != labelAttribute {
		t.Fatalf("root label %q attribute %#x", got, disk[rootOffset+11])
	}
	for fat := 0; fat < fatCount; fat++ {
		at := (reservedSectors + fat*sectorsPerFAT) * bytesPerSector
		if !bytes.Equal(disk[at:at+3], []byte{0xFD, 0xFF, 0xFF}) || bytes.ContainsFunc(disk[at+3:at+sectorsPerFAT*bytesPerSector], func(r rune) bool { return r != 0 }) {
			t.Fatalf("FAT %d is not empty", fat)
		}
	}
	// Nothing else in the root or the data area.
	if bytes.ContainsFunc(disk[rootOffset+32:], func(r rune) bool { return r != 0 }) {
		t.Fatal("blank disk has files")
	}
	// The boot code prints why the disk cannot start, then retries the boot.
	if !bytes.Contains(disk[:510], []byte("This disk does not start the computer.")) || !bytes.Contains(disk[0x3E:0x62], []byte{0xCD, 0x19}) {
		t.Fatal("boot sector does not explain itself and retry")
	}
}

func TestNamedChangesOnlyTheLabelAndSerial(t *testing.T) {
	source, err := Blank("Source", [4]byte{9, 9, 9, 9})
	if err != nil {
		t.Fatal(err)
	}
	// A file on the disk stays where it was.
	copy(source[rootOffset+32:], "KERNEL  SYS\x20")
	copy(source[rootOffset+(14*bytesPerSector):], "kernel bytes")
	named, err := Named(source, "freedos boot disk 2", [4]byte{5, 6, 7, 8})
	if err != nil {
		t.Fatal(err)
	}
	if string(named[0x2B:0x36]) != "FREEDOS BOO" || string(named[rootOffset:rootOffset+11]) != "FREEDOS BOO" || !bytes.Equal(named[0x27:0x2B], []byte{5, 6, 7, 8}) {
		t.Fatalf("label %q / %q serial % x", named[0x2B:0x36], named[rootOffset:rootOffset+11], named[0x27:0x2B])
	}
	for i := range source {
		inLabel := i >= 0x27 && i < 0x36 || i >= rootOffset && i < rootOffset+11
		if source[i] != named[i] && !inLabel {
			t.Fatalf("byte %#x changed", i)
		}
	}
	if string(source[0x2B:0x36]) != "SOURCE     " {
		t.Fatal("Named changed its input")
	}
}

func TestNamedAddsALabelEntryWhenTheRootHasNone(t *testing.T) {
	source, _ := Blank("Source", [4]byte{})
	copy(source[rootOffset:], "KERNEL  SYS\x20")
	named, err := Named(source, "Boot", [4]byte{1})
	if err != nil {
		t.Fatal(err)
	}
	if string(named[rootOffset:rootOffset+11]) != "KERNEL  SYS" || string(named[rootOffset+32:rootOffset+43]) != "BOOT       " || named[rootOffset+43] != labelAttribute {
		t.Fatalf("root % q", named[rootOffset:rootOffset+64])
	}
}

func TestNamedRefusesWhatIsNotA360KDOSDisk(t *testing.T) {
	good, _ := Blank("Good", [4]byte{})
	for name, image := range map[string][]byte{
		"short":      good[:Size-512],
		"no 55AA":    append(append([]byte(nil), good[:510]...), append([]byte{0, 0}, good[512:]...)...),
		"no ext sig": func() []byte { b := append([]byte(nil), good...); b[0x26] = 0; return b }(),
		"720K layout": func() []byte {
			b := append([]byte(nil), good...)
			binary.LittleEndian.PutUint16(b[19:], 1440)
			return b
		}(),
	} {
		if _, err := Named(image, "X", [4]byte{}); !errors.Is(err, ErrImage) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestLabelKeepsWhatDOSStores(t *testing.T) {
	for in, want := range map[string]string{
		"Work disk 2":         "WORK DISK 2",
		"  saves  ":           "SAVES",
		"FreeDOS boot disk 2": "FREEDOS BOO",
		"Élan-1":              "LAN-1",
	} {
		if got, err := Label(in); err != nil || got != want {
			t.Errorf("Label(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "   ", "***"} {
		if _, err := Label(in); !errors.Is(err, ErrName) {
			t.Errorf("Label(%q) err = %v", in, err)
		}
	}
}
