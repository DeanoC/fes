// SPDX-License-Identifier: GPL-3.0-or-later
package sdcontrol

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdhci"
)

func TestOwnedPIOWriteContainResume(t *testing.T) {
	firmware, c := newRPC(t, "")
	s, err := Open(firmware, c, 200000000, ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte{0x35, 0x9a, 0x7f, 0x01}, 256)
	if err := s.WriteSectors(3, data); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(data))
	if err := s.ReadSectors(3, got); err != nil || !bytes.Equal(got, data) {
		t.Fatal("readback", err)
	}
	if err := s.Quiesce(); err != nil {
		t.Fatal(err)
	}
	receipt, valid := s.LastReceipt()
	if !valid || receipt.Active || c.active {
		t.Fatal("firmware did not contain SD", receipt)
	}
	accesses, calls := c.accesses, len(firmware.requests)
	if err := s.WriteSectors(3, data); !errors.Is(err, sdhci.ErrQuiesced) || c.accesses != accesses || len(firmware.requests) != calls {
		t.Fatal("contained write touched hardware", err)
	}
	if err := s.Resume(); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteSectors(5, data[:512]); err != nil {
		t.Fatal(err)
	}
	if err := s.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := s.Quiesce(); err != nil {
		t.Fatal(err)
	}
}

func TestOwnedPIOWriteFailureRetainsLeaseWithoutStopOrReplay(t *testing.T) {
	firmware, c := newRPC(t, "")
	s, err := Open(firmware, c, 200000000, ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	c.failWrite = true
	if err := s.WriteSectors(3, bytes.Repeat([]byte{0xab}, 1024)); !errors.Is(err, ErrRetired) {
		t.Fatal(err)
	}
	if _, valid := s.LastReceipt(); valid || !c.active || firmware.stats[3] != 0 || len(c.written) != 1 {
		t.Fatal("uncertain write released lease or continued batch", firmware.stats)
	}
	accesses, calls := c.accesses, len(firmware.requests)
	for _, call := range []func() error{
		func() error { return s.WriteSectors(3, make([]byte, 512)) },
		func() error { return s.ReadSectors(3, make([]byte, 512)) }, s.Flush, s.Quiesce, s.Resume,
	} {
		if err := call(); !errors.Is(err, ErrRetired) || c.accesses != accesses || len(firmware.requests) != calls {
			t.Fatal("retired session reused", err)
		}
	}
}

func TestReadOnlySessionAndInvalidAccessCannotWrite(t *testing.T) {
	firmware, c := newRPC(t, "")
	s, err := Open(firmware, c, 200000000, ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	accesses, calls := c.accesses, len(firmware.requests)
	if err := s.WriteSectors(3, make([]byte, 512)); !errors.Is(err, block.ErrReadOnly) || c.accesses != accesses || len(firmware.requests) != calls {
		t.Fatal(err)
	}
	if _, valid := s.LastReceipt(); !valid {
		t.Fatal("read-only refusal retired a healthy lease")
	}
	cpu := callFunc(func([7]uint64) ([4]uint64, error) {
		t.Fatal("invalid access contacted firmware")
		return [4]uint64{}, nil
	})
	if s, err := Open(cpu, newCard(), 200000000, Access(2)); err == nil || s != nil {
		t.Fatal("invalid access accepted")
	}
	if err := s.Quiesce(); err != nil {
		t.Fatal(err)
	}
}

func TestOwnedFATPreferencesSurviveContainResumeAndIndependentReadback(t *testing.T) {
	for _, tool := range []string{"mformat", "mmd", "mcopy", "mtype"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatal("mtools required for FAT write integration:", err)
		}
	}
	dir := t.TempDir()
	image := filepath.Join(dir, "sd.img")
	pref := filepath.Join(dir, "PREFS.JSN")
	f, err := os.Create(image)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(64 << 20); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(pref, bytes.Repeat([]byte(" "), 4096), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"mformat", "-i", image, "-F", "::"}, {"mmd", "-i", image, "::FES"}, {"mcopy", "-i", image, pref, "::FES/PREFS.JSN"}} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v %s", args, err, out)
		}
	}
	firmware, c := newRPC(t, "")
	c.image, err = os.ReadFile(image)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(firmware, c, 200000000, ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	v, err := fat.Mount(s)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"safe_area":7,"layout":"grid"}`)
	if err = v.WriteFile("FES/PREFS.JSN", data); err != nil {
		t.Fatal(err)
	}
	if err = s.Quiesce(); err != nil {
		t.Fatal(err)
	}
	if err = v.WriteFile("FES/PREFS.JSN", data); !errors.Is(err, sdhci.ErrQuiesced) {
		t.Fatal("contained FAT write accepted", err)
	}
	if err = s.Resume(); err != nil {
		t.Fatal(err)
	}
	v, err = fat.Mount(s)
	if err != nil {
		t.Fatal(err)
	}
	got, err := v.ReadFile("FES/PREFS.JSN")
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("remounted preferences", err, string(got))
	}
	if err = s.Quiesce(); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(image, c.image, 0600); err != nil {
		t.Fatal(err)
	}
	got, err = exec.Command("mtype", "-i", image, "::FES/PREFS.JSN").Output()
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("independent FAT reader", err, string(got))
	}
}
