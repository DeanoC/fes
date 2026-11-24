// SPDX-License-Identifier: GPL-3.0-or-later
package packagestore

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"testing/fstest"
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
		write: func(path string, data []byte) error {
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
