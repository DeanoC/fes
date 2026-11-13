package fat

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
)

func command(t *testing.T, args ...string) {
	t.Helper()
	if output, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, output)
	}
}
func fixture(t *testing.T) string {
	t.Helper()
	for _, tool := range []string{"mformat", "mcopy", "mmd"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip("mtools not installed")
		}
	}
	dir := t.TempDir()
	image := filepath.Join(dir, "disk.img")
	f, err := os.Create(image)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(64 << 20); err != nil {
		t.Fatal(err)
	}
	f.Close()
	command(t, "mformat", "-i", image, "-F", "::")
	command(t, "mmd", "-i", image, "::FES")
	for name, data := range map[string][]byte{"PREFS.JSON": bytes.Repeat([]byte(" "), 4096), "CATALOG.JSON": []byte(`[{"title":"Pong"}]`), "Long preference name.json": []byte("long-name")} {
		path := filepath.Join(dir, name)
		if err = os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		command(t, "mcopy", "-i", image, path, "::FES/"+name)
	}
	return image
}

func openVolume(t *testing.T, path string) (*block.File, *Volume) {
	t.Helper()
	d, err := block.OpenFile(path, true)
	if err != nil {
		t.Fatal(err)
	}
	v, err := Mount(d)
	if err != nil {
		d.Close()
		t.Fatal(err)
	}
	return d, v
}

func TestRealFATReadWriteAndRemount(t *testing.T) {
	path := fixture(t)
	d, v := openVolume(t, path)
	got, err := v.ReadFile("fes/catalog.json")
	if err != nil || string(got) != `[{"title":"Pong"}]` {
		t.Fatalf("catalog %q: %v", got, err)
	}
	got, err = v.ReadFile("FES/Long preference name.json")
	if err != nil || string(got) != "long-name" {
		t.Fatalf("LFN %q: %v", got, err)
	}
	entries, err := v.ReadDir("FES")
	if err != nil || len(entries) != 3 {
		t.Fatalf("directory %v: %v", entries, err)
	}
	directory, err := v.Open("FES")
	if err != nil {
		t.Fatal(err)
	}
	reader := directory.(fs.ReadDirFile)
	if entries, err = reader.ReadDir(1); err != nil || len(entries) != 1 {
		t.Fatalf("first directory entry %v: %v", entries, err)
	}
	if entries, err = reader.ReadDir(int(^uint(0) >> 1)); err != nil || len(entries) != 2 {
		t.Fatalf("large directory count %v: %v", entries, err)
	}
	directory.Close()
	if _, err = v.Open("../FES"); !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("traversal: %v", err)
	}
	payload := []byte(`{"layout":"list","safe_area_pct":0.025}`)
	if err = v.WriteFile("FES/PREFS.JSON", payload); err != nil {
		t.Fatal(err)
	}
	d.Close()
	d, v = openVolume(t, path)
	defer d.Close()
	got, err = v.ReadFile("FES/PREFS.JSON")
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("remount %q: %v", got, err)
	}
	larger := bytes.Repeat([]byte("x"), 3000)
	if err = v.WriteFile("FES/PREFS.JSON", larger); err != nil {
		t.Fatalf("reserved capacity lost after shrink: %v", err)
	}
	copyPath := filepath.Join(t.TempDir(), "copied.json")
	command(t, "mcopy", "-i", path, "::FES/PREFS.JSON", copyPath)
	got, err = os.ReadFile(copyPath)
	if err != nil || !bytes.Equal(got, larger) {
		t.Fatalf("mtools readback %d: %v", len(got), err)
	}
}

type trackedDevice struct {
	block.Device
	writes, flushes int
	failFlush       bool
}

func (d *trackedDevice) WriteSectors(lba uint64, b []byte) error {
	d.writes++
	return d.Device.WriteSectors(lba, b)
}
func (d *trackedDevice) Flush() error {
	d.flushes++
	if d.failFlush {
		return errors.New("injected flush failure")
	}
	return d.Device.Flush()
}

func TestWriteAdmissionAndFlushFailure(t *testing.T) {
	path := fixture(t)
	d, v := openVolume(t, path)
	defer d.Close()
	spy := &trackedDevice{Device: d}
	v.device = spy
	if err := v.WriteFile("FES/PREFS.JSON", make([]byte, 4097)); !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity: %v", err)
	}
	if spy.writes != 0 || spy.flushes != 0 {
		t.Fatal("oversized write mutated storage")
	}
	if err := v.WriteFile("FES/new.json", []byte("x")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created a file: %v", err)
	}
	spy.failFlush = true
	if err := v.WriteFile("FES/PREFS.JSON", []byte("new")); err == nil {
		t.Fatal("flush failure reported success")
	}
	f, err := v.Open("FES/PREFS.JSON")
	if err != nil {
		t.Fatal(err)
	}
	info, err := f.Stat()
	f.Close()
	if err != nil || info.Size() != 4096 {
		t.Fatalf("metadata changed before data flush: %v size=%d", err, info.Size())
	}
}

func TestRejectGeometryAndCycle(t *testing.T) {
	path := fixture(t)
	d, v := openVolume(t, path)
	defer d.Close()
	e, err := v.lookup("FES/PREFS.JSON")
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 512)
	offset := uint64(e.cluster) * 4
	sector := v.fatStart + offset/512
	if err = d.ReadSectors(sector, b); err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint32(b[offset%512:], e.cluster)
	if err = d.WriteSectors(sector, b); err != nil {
		t.Fatal(err)
	}
	if _, err = v.Open("FES/PREFS.JSON"); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("cycle accepted: %v", err)
	}
	if err = d.ReadSectors(0, b); err != nil {
		t.Fatal(err)
	}
	b[13] = 3
	if err = d.WriteSectors(0, b); err != nil {
		t.Fatal(err)
	}
	if _, err = Mount(d); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("invalid geometry accepted: %v", err)
	}
}

func TestMBRPartition(t *testing.T) {
	path := fixture(t)
	partitioned := filepath.Join(t.TempDir(), "partitioned.img")
	in, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.Create(partitioned)
	if err != nil {
		t.Fatal(err)
	}
	const start = 2048
	mbr := make([]byte, 512)
	mbr[510] = 0x55
	mbr[511] = 0xaa
	mbr[450] = 0x0c
	binary.LittleEndian.PutUint32(mbr[454:], start)
	binary.LittleEndian.PutUint32(mbr[458:], 64<<20/512)
	if _, err = out.Write(mbr); err != nil {
		t.Fatal(err)
	}
	if _, err = out.Seek(start*512, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if _, err = io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	out.Close()
	d, v := openVolume(t, partitioned)
	defer d.Close()
	got, err := v.ReadFile("FES/CATALOG.JSON")
	if err != nil || len(got) == 0 {
		t.Fatalf("partition read %q: %v", got, err)
	}
	if err = v.Mkdir(context.Background(), "FES/Partition directory"); err != nil {
		t.Fatal(err)
	}
	name := "FES/Partition directory/New package.fcp"
	want := bytes.Repeat([]byte("partition data"), 100)
	if err = v.CreateFile(context.Background(), name, want); err != nil {
		t.Fatal(err)
	}
	got, err = exec.Command("mtype", "-i", partitioned+"@@1048576", "::"+name).CombinedOutput()
	if err != nil || !bytes.Equal(got, want) {
		t.Fatal("partition creation readback", string(got), err)
	}
}
