// SPDX-License-Identifier: GPL-3.0-or-later
package packagestore

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
)

func TestBTBondsAndFirmwareBoundedReadWriteAndLease(t *testing.T) {
	l := lease()
	wantBonds := `{"version":1,"bonds":[]}`
	fwData := []byte("Realtech\x01\x02\x03\x04\x51\x04\xfd\x77")
	fsys := fstest.MapFS{
		BTBondsPath:                        &fstest.MapFile{Data: []byte(wantBonds)},
		BTFirmwareDir + "/RTL8761BU_FW.BIN": &fstest.MapFile{Data: fwData},
	}
	var writtenPath string
	var writtenData []byte
	pf := &preferenceFiles{
		trackedFS: &trackedFS{base: fsys, lease: l},
		replace: func(path string, data []byte) error {
			writtenPath = path
			writtenData = append([]byte(nil), data...)
			return nil
		},
	}
	s, err := New(pf, l)
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.ReadBTBonds(context.Background())
	if err != nil || string(got) != wantBonds {
		t.Fatalf("ReadBTBonds: got=%q err=%v", got, err)
	}
	updated := `{"version":1,"bonds":[{"addr":"AA:BB:CC:DD:EE:FF","link_key":"00112233445566778899aabbccddeeff"}]}`
	if err := s.WriteBTBonds(context.Background(), []byte(updated)); err != nil {
		t.Fatalf("WriteBTBonds: %v", err)
	}
	if writtenPath != BTBondsPath || string(writtenData) != updated {
		t.Fatalf("WriteBTBonds mismatch path=%q data=%q", writtenPath, writtenData)
	}
	if err := s.WriteBTBonds(context.Background(), []byte("not-json")); err == nil {
		t.Fatal("invalid JSON accepted by WriteBTBonds")
	}

	// Firmware read resolves lowercase filename against uppercase FAT32 entry and rejects traversal.
	gotFW, err := s.ReadBTFirmware(context.Background(), "rtl8761bu_fw.bin")
	if err != nil || !bytes.Equal(gotFW, fwData) {
		t.Fatalf("ReadBTFirmware: got=%x err=%v", gotFW, err)
	}
	if _, err := s.ReadBTFirmware(context.Background(), "../PREFS.JSON"); err == nil {
		t.Fatal("path traversal admitted by ReadBTFirmware")
	}

	// Contained store rejects reads and writes without I/O.
	if err := s.Quiesce(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadBTBonds(context.Background()); !errors.Is(err, ErrState) {
		t.Fatalf("quiesced ReadBTBonds=%v", err)
	}
	if err := s.WriteBTBonds(context.Background(), []byte(updated)); !errors.Is(err, ErrState) {
		t.Fatalf("quiesced WriteBTBonds=%v", err)
	}
}

type creatableBondsFS struct {
	*trackedFS
	files fstest.MapFS
	dirs  map[string]bool
}

func (f *creatableBondsFS) Mkdir(_ context.Context, name string) error {
	if f.dirs[name] {
		return fs.ErrExist
	}
	f.dirs[name] = true
	return nil
}

func (f *creatableBondsFS) ReplaceFile(_ context.Context, path string, data []byte) error {
	if !f.dirs["FES"] {
		return fs.ErrNotExist
	}
	f.files[path] = &fstest.MapFile{Data: append([]byte(nil), data...)}
	return nil
}

func TestWriteBTBondsProvisionsMissingFileOnFreshCard(t *testing.T) {
	l := lease()
	mapFS := fstest.MapFS{}
	fsys := &creatableBondsFS{
		trackedFS: &trackedFS{base: mapFS, lease: l},
		files:     mapFS,
		dirs:      map[string]bool{},
	}
	s, err := New(fsys, l)
	if err != nil {
		t.Fatal(err)
	}

	first := `{"version":1,"bonds":[{"addr":"11:22:33:44:55:66","link_key":"00112233445566778899aabbccddeeff","slot":0}]}`
	if err := s.WriteBTBonds(context.Background(), []byte(first)); err != nil {
		t.Fatalf("first WriteBTBonds on fresh card failed: %v", err)
	}
	created := mapFS[BTBondsPath]
	if created == nil || string(created.Data) != first {
		t.Fatalf("expected %s to hold %q, got %+v", BTBondsPath, first, created)
	}
	got, err := s.ReadBTBonds(context.Background())
	if err != nil || string(got) != first {
		t.Fatalf("ReadBTBonds after provision: got=%q err=%v", got, err)
	}

	// Second larger write grows and replaces BTBondsPath cleanly via ReplaceFile.
	second := `{"version":1,"bonds":[{"addr":"11:22:33:44:55:66","link_key":"00112233445566778899aabbccddeeff","slot":0},{"addr":"AA:BB:CC:DD:EE:FF","link_key":"ffeeddccbbaa99887766554433221100","slot":1}]}`
	if err := s.WriteBTBonds(context.Background(), []byte(second)); err != nil {
		t.Fatalf("second WriteBTBonds failed: %v", err)
	}
	got2, err := s.ReadBTBonds(context.Background())
	if err != nil || string(got2) != second {
		t.Fatalf("ReadBTBonds after update: got=%q err=%v", got2, err)
	}
}

func TestWriteBTBondsMutationReadOnlyRetiresStore(t *testing.T) {
	l := lease()
	pf := &preferenceFiles{
		trackedFS: &trackedFS{base: fstest.MapFS{}, lease: l},
		replace: func(string, []byte) error {
			return errors.Join(fat.ErrMutation, block.ErrReadOnly)
		},
	}
	s, err := New(pf, l)
	if err != nil {
		t.Fatal(err)
	}
	err = s.WriteBTBonds(context.Background(), []byte(`{"version":1,"bonds":[]}`))
	if !errors.Is(err, fat.ErrMutation) || !errors.Is(err, ErrRetired) {
		t.Fatalf("expected ErrMutation+ErrRetired, got %v", err)
	}
}
