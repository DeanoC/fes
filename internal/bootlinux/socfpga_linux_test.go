//go:build linux

package bootlinux

import (
	"encoding/binary"
	"errors"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestSoCFPGARequiresExactBoard(t *testing.T) {
	compatible := []byte("altr,socfpga-cyclone5\x00altr,socfpga\x00")
	model := []byte("Terasic DE10-nano\x00")
	if err := validateSoCFPGABoard("arm", model, compatible); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		arch              string
		model, compatible []byte
	}{
		{"amd64", model, compatible},
		{"arm", []byte("other board\x00"), compatible},
		{"arm", model, []byte("altr,socfpga-arria10\x00altr,socfpga\x00")},
		{"arm", model, []byte("altr,socfpga-cyclone5\x00")},
		{"arm", model, []byte("altr,socfpga-cyclone5\x00altr,socfpga")},
	} {
		if err := validateSoCFPGABoard(tc.arch, tc.model, tc.compatible); err == nil {
			t.Fatalf("accepted unexpected board: %+v", tc)
		}
	}
}

func TestSoCFPGAResetPreparationChangesOnlyBootROMPolicy(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "registers")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	page := make([]byte, 4096)
	for i := range page {
		page[i] = 0x5a
	}
	binary.LittleEndian.PutUint32(page[0xe0:], 0xae9efebc)
	binary.LittleEndian.PutUint32(page[0xc8:], 0x100)
	if _, err = f.Write(page); err != nil {
		t.Fatal(err)
	}
	regs, err := mapSoCFPGARegisters(int(f.Fd()), 0)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = configureSoCFPGAReset(regs); err != nil {
			t.Fatal(err)
		}
	}
	if err = regs.Close(); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint32(page[0xe0:], 0)
	binary.LittleEndian.PutUint32(page[0xc8:], 0x49535756)
	if string(actual) != string(page) {
		t.Fatal("reset preparation changed unrelated registers or failed to persist required words")
	}
}

type faultyResetRegisters struct {
	words                                  map[int]uint32
	readErrorAt, writeErrorAt, lostWriteAt int
	writes                                 int
}

func (r *faultyResetRegisters) Read32(offset int) (uint32, error) {
	if offset == r.readErrorAt {
		return 0, unix.EIO
	}
	return r.words[offset], nil
}
func (r *faultyResetRegisters) Write32(offset int, value uint32) error {
	r.writes++
	if offset == r.writeErrorAt {
		return unix.EIO
	}
	if offset != r.lostWriteAt {
		r.words[offset] = value
	}
	return nil
}

func TestSoCFPGAResetPreparationFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name              string
		read, write, lost int
	}{
		{"read warm policy", 0xe0, -1, -1},
		{"read preloader", 0xc8, -1, -1},
		{"write preloader", -1, 0xc8, -1},
		{"write warm policy", -1, 0xe0, -1},
		{"preloader readback", -1, -1, 0xc8},
		{"warm policy readback", -1, -1, 0xe0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &faultyResetRegisters{map[int]uint32{0xe0: 0xae9efebc, 0xc8: 0x100}, tc.read, tc.write, tc.lost, 0}
			if err := configureSoCFPGAReset(r); err == nil {
				t.Fatal("allowed watchdog arming after failed register preparation")
			}
			if tc.read >= 0 && r.writes != 0 {
				t.Fatal("wrote registers before validating readable state")
			}
		})
	}
	r := &faultyResetRegisters{map[int]uint32{0xe0: 0x12345678}, -1, -1, -1, 0}
	if err := configureSoCFPGAReset(r); err == nil || !strings.Contains(err.Error(), "unexpected") {
		t.Fatalf("unexpected policy accepted: %v", err)
	}
	if r.writes != 0 {
		t.Fatal("changed unknown boot policy")
	}
}

func TestSoCFPGARegisterMappingRejectsClosedDescriptor(t *testing.T) {
	_, err := mapSoCFPGARegisters(-1, 0)
	if !errors.Is(err, unix.EBADF) {
		t.Fatalf("mapping failure not returned: %v", err)
	}
}
