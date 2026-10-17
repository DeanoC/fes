package blockprobe

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestFakeSysfsSkipsOpticalAndZeroKeepsUSBAndSD(t *testing.T) {
	root := t.TempDir()
	block := filepath.Join(root, "class", "block")
	// Dock DVD behind a USB bridge: sr-style SCSI type, removable, 0 bytes.
	writeDev(t, block, "sda", "0", "1", "5")
	// Native optical node, including one that reports a disc size.
	writeDev(t, block, "sr0", "0", "1", "5")
	writeDev(t, block, "sr1", "2048", "1", "5")
	// Removable disk with no media, reported as SCSI type 0 (the JMicron case).
	writeDev(t, block, "sdc", "0", "1", "0")
	// Non-removable zero-capacity disk.
	writeDev(t, block, "sdd", "0", "0", "0")
	// Ordinary USB stick.
	writeDev(t, block, "sdb", "15633408", "1", "0")
	writeDev(t, block, "sdb1", "15633376", "1", "0")
	// SD card and the FESDATA3 partition. SD is often marked removable.
	writeDev(t, block, "mmcblk0", "62333952", "1", "")
	writeDev(t, block, "mmcblk0p1", "2097152", "1", "")
	writeDev(t, block, "mmcblk0p3", "41943040", "1", "")
	// A sysfs attribute that blocks if opened. Classification must not touch it.
	inquiry := filepath.Join(block, "sda", "device", "inquiry")
	if err := syscall.Mkfifo(inquiry, 0o600); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		usable, err := Usable(block)
		if err != nil {
			t.Errorf("usable: %v", err)
			return
		}
		want := []string{"mmcblk0", "mmcblk0p1", "mmcblk0p3", "sdb", "sdb1"}
		if !reflect.DeepEqual(usable, want) {
			t.Errorf("usable = %v, want %v", usable, want)
		}
		for _, name := range []string{"sda", "sr0", "sr1", "sdc", "sdd"} {
			if err := RejectsName(root, name); !errors.Is(err, ErrSkip) {
				t.Errorf("%s: %v, want ErrSkip", name, err)
			}
		}
		for _, name := range want {
			if err := RejectsName(root, name); err != nil {
				t.Errorf("%s rejected: %v", name, err)
			}
		}
		if decision, _ := Decide(block, "sda"); decision.Reason != ReasonOptical {
			t.Errorf("sda reason = %s", decision.Reason)
		}
		if decision, _ := Decide(block, "sdc"); decision.Reason != ReasonNoMedia {
			t.Errorf("sdc reason = %s", decision.Reason)
		}
		if decision, _ := Decide(block, "sdd"); decision.Reason != ReasonZero {
			t.Errorf("sdd reason = %s", decision.Reason)
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("classification blocked while reading sysfs")
	}
}

func TestPartitionInheritsOpticalParent(t *testing.T) {
	block := t.TempDir()
	writeDev(t, block, "sda", "0", "1", "5")
	part := filepath.Join(block, "sda1")
	if err := os.MkdirAll(part, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(part, "size"), []byte("0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	decision, ok := Decide(block, "sda1")
	if !ok || decision.Allow || decision.Reason != ReasonOptical {
		t.Fatalf("sda1 = %+v ok=%v", decision, ok)
	}
	// A second trailing digit must still resolve to sda, and a non-zero size
	// does not make a type-5 partition usable.
	wide := filepath.Join(block, "sda", "sda10")
	if err := os.MkdirAll(wide, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wide, "size"), []byte("2048\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	decision, ok = Decide(block, "sda10")
	if !ok || decision.Allow || decision.Reason != ReasonOptical {
		t.Fatalf("sda10 = %+v ok=%v", decision, ok)
	}
}

func TestNestedPartitionAndMountListing(t *testing.T) {
	root := t.TempDir()
	block := filepath.Join(root, "block")
	writeDev(t, block, "sda", "0", "1", "0")
	writeDev(t, block, "sdb", "15633408", "1", "0")
	writeDev(t, block, "mmcblk0", "62333952", "0", "")
	nested := filepath.Join(block, "mmcblk0", "mmcblk0p3")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "size"), []byte("41943040\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	usable, err := Usable(block)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"mmcblk0", "mmcblk0p3", "sdb"}
	if !reflect.DeepEqual(usable, want) {
		t.Fatalf("usable = %v, want %v", usable, want)
	}

	mounts := filepath.Join(root, "mountinfo")
	body := "" +
		"1 0 8:0 / /media/optical rw - udf /dev/sda rw\n" +
		"2 0 8:16 / /media/stick rw - vfat /dev/sdb rw\n" +
		"3 0 179:1 / /media/fat rw - exfat /dev/mmcblk0p1 rw\n" +
		"4 0 179:3 / /run/fogcast/fesdata3 rw - ext4 /dev/mmcblk0p3 rw\n" +
		"5 0 179:0 / / rw - ext4 /dev/mmcblk0 rw\n"
	if err := os.WriteFile(mounts, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	// p1 is not in this nested tree; give it a sibling entry so FAT is allowed.
	writeDev(t, block, "mmcblk0p1", "2097152", "0", "")

	media := filepath.Join(root, "media")
	for _, name := range []string{"optical", "stick", "fat"} {
		if err := os.MkdirAll(filepath.Join(media, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	stated := map[string]bool{}
	entries, err := os.ReadDir(media)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		full := filepath.Join("/media", entry.Name())
		if SkipsRoot(root, mounts, full) {
			continue
		}
		stated[entry.Name()] = true
		if _, err := os.Stat(filepath.Join(media, entry.Name())); err != nil {
			t.Fatal(err)
		}
	}
	if stated["optical"] {
		t.Fatal("stated the empty optical mount")
	}
	if !stated["stick"] || !stated["fat"] {
		t.Fatalf("stated = %v", stated)
	}
	if SkipsRoot(root, mounts, "/run/fogcast/fesdata3") {
		t.Fatal("FESDATA3 mount was skipped")
	}
	if SkipsRoot(root, mounts, "/media/stick/games") {
		t.Fatal("USB stick contents were skipped")
	}
	// "/" is the SD card. A child with no more specific mount stays on that device.
	if SkipsRoot(root, mounts, "/var/log") {
		t.Fatal("root filesystem child was skipped")
	}
}

func TestRootMountCoversChildrenAndLosesToLongerMount(t *testing.T) {
	root := t.TempDir()
	block := filepath.Join(root, "block")
	writeDev(t, block, "sda", "0", "1", "5")
	writeDev(t, block, "sdb", "15633408", "1", "0")
	writeDev(t, block, "mmcblk0", "62333952", "0", "")
	mounts := filepath.Join(root, "mountinfo")
	body := "" +
		"1 0 8:0 / / rw - ext4 /dev/sda rw\n" +
		"2 0 8:16 / /media/stick rw - vfat /dev/sdb rw\n" +
		"3 0 179:0 / /run/fogcast/fesdata3 rw - ext4 /dev/mmcblk0 rw\n"
	if err := os.WriteFile(mounts, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if !SkipsRoot(root, mounts, "/") {
		t.Fatal("optical root mount was admitted")
	}
	if !SkipsRoot(root, mounts, "/var/log") {
		t.Fatal("child of the optical root mount was admitted")
	}
	if SkipsRoot(root, mounts, "/media/stick") || SkipsRoot(root, mounts, "/media/stick/games") {
		t.Fatal("USB stick lost to the shorter root mount")
	}
	if SkipsRoot(root, mounts, "/run/fogcast/fesdata3") {
		t.Fatal("FESDATA mount lost to the shorter root mount")
	}
}

func TestRejectsNameSkipsBeforeOpen(t *testing.T) {
	root := t.TempDir()
	sysfs := filepath.Join(root, "sys")
	writeDev(t, filepath.Join(sysfs, "class", "block"), "sda", "0", "1", "5")
	if err := RejectsName(sysfs, "sda"); !errors.Is(err, ErrSkip) {
		t.Fatal(err)
	}
	if err := RejectsDevice("/sys", filepath.Join(t.TempDir(), "sda")); err != nil {
		t.Fatal(err)
	}
}

func TestReadFullAtReadsAllowedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stick")
	payload := []byte("usb-stick-bytes")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(payload))
	if err := ReadFullAt(path, buf, 0, time.Second); err != nil {
		t.Fatal(err)
	}
	if string(buf) != string(payload) {
		t.Fatalf("read %q", buf)
	}
}

func TestSymlinkedSysfsClassifiesOpticalAndUSB(t *testing.T) {
	root := t.TempDir()
	devices := filepath.Join(root, "devices")
	writeDev(t, devices, "sda", "0", "1", "5")
	writeDev(t, devices, "sdb", "15633408", "1", "0")
	for _, base := range []string{
		filepath.Join(root, "class", "block"),
		filepath.Join(root, "block"),
	} {
		if err := os.MkdirAll(base, 0o700); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"sda", "sdb"} {
			if err := os.Symlink(filepath.Join(devices, name), filepath.Join(base, name)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := RejectsName(root, "sda"); !errors.Is(err, ErrSkip) {
		t.Fatalf("symlink optical: %v", err)
	}
	if err := RejectsName(root, "sdb"); err != nil {
		t.Fatalf("symlink usb: %v", err)
	}
	usable, err := Usable(filepath.Join(root, "class", "block"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(usable, []string{"sdb"}) {
		t.Fatalf("usable = %v", usable)
	}
}

func TestReadFullAtClosesFileAfterTimeout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stick")
	payload := []byte("abcd")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	original := readDeviceAt
	readDeviceAt = func(file *os.File, buf []byte, off int64) (int, error) {
		close(started)
		<-release
		return file.ReadAt(buf, off)
	}
	t.Cleanup(func() { readDeviceAt = original })

	before := openFDCount(t)
	finished := make(chan error, 1)
	go func() {
		finished <- ReadFullAt(path, make([]byte, len(payload)), 0, 50*time.Millisecond)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("read did not start")
	}
	select {
	case err := <-finished:
		if err == nil || !strings.Contains(err.Error(), "timed out") {
			t.Fatalf("expected a timeout, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("caller did not time out")
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for openFDCount(t) > before {
		if time.Now().After(deadline) {
			t.Fatalf("open fds %d, before %d", openFDCount(t), before)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func openFDCount(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

func writeDev(t *testing.T, block, name, size, removable, scsiType string) {
	t.Helper()
	dir := filepath.Join(block, name, "device")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(block, name, "size"), []byte(size+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(block, name, "removable"), []byte(removable+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if scsiType != "" {
		if err := os.WriteFile(filepath.Join(dir, "type"), []byte(scsiType+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
