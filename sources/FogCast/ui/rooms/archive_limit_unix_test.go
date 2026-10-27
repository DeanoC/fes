//go:build unix

package rooms

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

const (
	archiveFDChildEnv  = "FOGCAST_ROOM_EXPORT_FD_CHILD"
	archiveFDSourceEnv = "FOGCAST_ROOM_EXPORT_FD_SOURCE"
	archiveFDOutputEnv = "FOGCAST_ROOM_EXPORT_FD_OUTPUT"
)

func TestExportWithManyAssetsUnderLowDescriptorLimit(t *testing.T) {
	if os.Getenv(archiveFDChildEnv) == "1" {
		var limit syscall.Rlimit
		if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &limit); err != nil {
			t.Fatal(err)
		}
		if limit.Max < 64 {
			t.Fatalf("file descriptor hard limit %d is below 64", limit.Max)
		}
		limit.Cur = 64
		if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &limit); err != nil {
			t.Fatal(err)
		}
		if err := Export(os.Getenv(archiveFDSourceEnv), os.Getenv(archiveFDOutputEnv)); err != nil {
			t.Fatal(err)
		}
		return
	}
	var limit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &limit); err != nil {
		t.Fatal(err)
	}
	if limit.Max < 64 {
		t.Skipf("file descriptor hard limit %d is below 64", limit.Max)
	}

	source := installSource(t, "Many assets", "function draw() end", "")
	for n := 0; n < 96; n++ {
		name := filepath.Join(source, fmt.Sprintf("asset-%03d.txt", n))
		if err := os.WriteFile(name, []byte("asset"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	output := filepath.Join(t.TempDir(), "many-assets.room.zip")
	command := exec.Command(os.Args[0], "-test.run=^TestExportWithManyAssetsUnderLowDescriptorLimit$")
	command.Env = append(os.Environ(),
		archiveFDChildEnv+"=1",
		archiveFDSourceEnv+"="+source,
		archiveFDOutputEnv+"="+output,
	)
	if result, err := command.CombinedOutput(); err != nil {
		t.Fatalf("low-descriptor export failed: %v\n%s", err, result)
	}
	if info, err := os.Stat(output); err != nil || info.Size() == 0 {
		t.Fatalf("export was not published: info=%v err=%v", info, err)
	}
}
