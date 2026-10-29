package block

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestBoundsAndFileDevice(t *testing.T) {
	for _, test := range []struct {
		lba  uint64
		n    int
		want error
	}{{0, 512, nil}, {2, 0, nil}, {2, 512, ErrBounds}, {^uint64(0), 512, ErrBounds}, {0, 513, ErrAlignment}, {0, -1, ErrAlignment}} {
		if got := CheckRange(2, test.lba, test.n); !errors.Is(got, test.want) {
			t.Fatalf("range %d/%d: %v", test.lba, test.n, got)
		}
	}
	path := filepath.Join(t.TempDir(), "disk.img")
	if err := os.WriteFile(path, make([]byte, 1024), 0600); err != nil {
		t.Fatal(err)
	}
	d, err := OpenFile(path, true)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 512)
	b[3] = 17
	if err = d.WriteSectors(1, b); err != nil {
		t.Fatal(err)
	}
	if err = d.Flush(); err != nil {
		t.Fatal(err)
	}
	if err = d.Close(); err != nil {
		t.Fatal(err)
	}
	d, err = OpenFile(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	clear(b)
	if err = d.ReadSectors(1, b); err != nil || b[3] != 17 {
		t.Fatalf("retained %v %d", err, b[3])
	}
	if err = d.WriteSectors(0, b); !errors.Is(err, ErrReadOnly) {
		t.Fatal(err)
	}
	if _, err = OpenFile(t.TempDir(), false); err == nil {
		t.Fatal("directory accepted")
	}
}
