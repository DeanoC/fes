//go:build linux

package bootlinux

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// This opt-in test MUST run as PID 1 in a disposable privileged Docker
// container. It creates its own loop-backed images and never opens a hardware
// watchdog. Example: CGO_ENABLED=0 go test -c -o /tmp/bootlinux.test ./internal/bootlinux
//
//	docker run --rm --privileged --network none --user 0 \
//	  -e BOOTLINUX_INTEGRATION=prepare -v /tmp/bootlinux.test:/validation:ro \
//	  --entrypoint /validation EXISTING_MEDIA_BUILDER_IMAGE \
//	  -test.run=^TestLinuxRootSwitchIntegration$ -test.v
//
// No source tree or block device is mounted from the host. Allocated loop
// devices use AUTOCLEAR and only container-owned image files as backing.
func TestLinuxRootSwitchIntegration(t *testing.T) {
	stage := os.Getenv("BOOTLINUX_INTEGRATION")
	if stage == "" {
		t.Skip("requires disposable privileged container")
	}
	if stage == "guard" {
		integrationGuard(t)
		return
	}
	if os.Getpid() != 1 || os.Geteuid() != 0 {
		t.Fatal("integration requires root and PID 1; refusing host mutation")
	}
	switch stage {
	case "prepare":
		if _, err := os.Stat("/.dockerenv"); err != nil {
			t.Fatal("initial stage requires Docker container")
		}
		if err := unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
			t.Fatal(err)
		}
		integrationMount(t, "tmpfs", "/media/fat", "tmpfs")
		integrationMount(t, "tmpfs", "/run", "tmpfs")
		for _, name := range []string{"bootstrap", "candidate"} {
			integrationImage(t, name)
		}
		integrationSwitch(t, "bootstrap", "bootstrap")
	case "bootstrap":
		integrationExt4(t, "/")
		integrationMount(t, "tmpfs", "/run", "tmpfs")
		if err := os.Setenv("BOOTLINUX_INTEGRATION", "guard"); err != nil {
			t.Fatal(err)
		}
		_, err := StartGuard(context.Background(), "/sbin/init", []string{"-test.run=^TestLinuxRootSwitchIntegration$", "-test.v"})
		if err != nil {
			t.Fatal(err)
		}
		integrationSwitch(t, "candidate", "candidate")
	case "candidate":
		integrationExt4(t, "/")
		integrationExt4(t, BootstrapMount)
		if data, err := os.ReadFile(BootstrapMount + "/bootstrap-sentinel"); err != nil || string(data) != "stable" {
			t.Fatalf("bootstrap mount lost: %q %v", data, err)
		}
		if _, err := os.Stat("/bootstrap-sentinel"); !os.IsNotExist(err) {
			t.Fatalf("candidate did not become root: %v", err)
		}
		if err := os.WriteFile("/must-be-read-only", nil, 0600); !os.IsPermission(err) && err != unix.EROFS && !strings.Contains(fmt.Sprint(err), "read-only") {
			t.Fatalf("root is writable: %v", err)
		}
		for _, path := range []string{"/proc/self/mountinfo", "/sys/class", "/dev/null"} {
			if _, err := os.Stat(path); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile("/media/fat/candidate-ready", []byte("candidate"), 0600); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			data, err := os.ReadFile("/media/fat/guard-view")
			if err == nil && string(data) == "stable guard saw candidate" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("guard lost original namespace FAT view: %q %v", data, err)
			}
			time.Sleep(10 * time.Millisecond)
		}
		fmt.Println("BOOTLINUX_INTEGRATION_PASS: ext4 bootstrap -> ext4 candidate, PID 1, read-only root, retained bootstrap, writable FAT, private guard namespace")
	default:
		t.Fatalf("unknown integration stage %q", stage)
	}
}

func integrationMount(t *testing.T, source, path, kind string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mount(source, path, kind, 0, ""); err != nil {
		t.Fatal(err)
	}
}
func integrationImage(t *testing.T, name string) {
	t.Helper()
	tree := t.TempDir()
	for _, path := range []string{".fes-bootstrap", "media/fat", "dev", "proc", "sys", "run", "sbin", "tmp"} {
		if err := os.MkdirAll(filepath.Join(tree, path), 0755); err != nil {
			t.Fatal(err)
		}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	dst, err := os.OpenFile(filepath.Join(tree, "sbin/init"), os.O_CREATE|os.O_WRONLY, 0755)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(dst, src)
	_ = src.Close()
	_ = dst.Close()
	if err != nil {
		t.Fatal(err)
	}
	if name == "bootstrap" {
		if err := os.WriteFile(filepath.Join(tree, "bootstrap-sentinel"), []byte("stable"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	path := "/media/fat/" + name + ".img"
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(64 << 20); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	output, err := exec.Command("/usr/sbin/mkfs.ext4", "-q", "-F", "-d", tree, path).CombinedOutput()
	if err != nil {
		t.Fatalf("create integration image: %v %s", err, output)
	}
}
func integrationSwitch(t *testing.T, image, stage string) {
	t.Helper()
	if err := os.MkdirAll("/run/fes-root", 0755); err != nil {
		t.Fatal(err)
	}
	r, err := PrepareRoot("/media/fat/"+image+".img", "/run/fes-root")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("BOOTLINUX_INTEGRATION", stage); err != nil {
		t.Fatal(err)
	}
	err = r.PivotAndExec("/sbin/init", []string{"/sbin/init", "-test.run=^TestLinuxRootSwitchIntegration$", "-test.v"}, os.Environ())
	t.Fatalf("integration exec returned: %v", err)
}
func integrationExt4(t *testing.T, path string) {
	t.Helper()
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		t.Fatal(err)
	}
	if stat.Type != unix.EXT4_SUPER_MAGIC {
		t.Fatalf("%s is not ext4: %x", path, stat.Type)
	}
}
func integrationGuard(t *testing.T) {
	t.Helper()
	if err := privateGuardNamespace(); err != nil {
		t.Fatal(err)
	}
	ready := os.NewFile(3, "armed")
	defer ready.Close()
	cfg := guardFixture()
	cfg.TrialTimeout = 10 * time.Second
	err := runGuard(context.Background(), cfg, ready, func(ctx context.Context, boot, image string) (bool, error) {
		if _, err := os.Stat("/media/fat/candidate-ready"); os.IsNotExist(err) {
			return false, nil
		} else if err != nil {
			return false, err
		}
		if data, err := os.ReadFile("/bootstrap-sentinel"); err != nil || string(data) != "stable" {
			return false, fmt.Errorf("guard root changed: %q %v", data, err)
		}
		return true, os.WriteFile("/media/fat/guard-view", []byte("stable guard saw candidate"), 0600)
	}, &fakeWatchdog{})
	if err != nil {
		t.Fatal(err)
	}
}
