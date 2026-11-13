//go:build linux

package bootlinux

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"golang.org/x/sys/unix"
)

func de25WatchdogFixture(t *testing.T) (string, uint64) {
	t.Helper()
	root := t.TempDir()
	var device unix.Stat_t
	if err := unix.Lstat("/dev/null", &device); err != nil {
		t.Fatal(err)
	}
	for path, raw := range map[string]string{
		"firmware/devicetree/base/model":                     "SoCFPGA Agilex5 Terasic DE25-Nano\x00",
		"firmware/devicetree/base/compatible":                "intel,socfpga-agilex5-socdk\x00intel,socfpga-agilex5\x00",
		"class/watchdog/watchdog0/device/of_node/compatible": "snps,dw-wdt\x00",
		"class/watchdog/watchdog0/device/of_node/reg":        "\x10\xd0\x02\x00\x00\x00\x01\x00",
		"class/watchdog/watchdog0/dev":                       fmt.Sprintf("%d:%d\n", unix.Major(uint64(device.Rdev)), unix.Minor(uint64(device.Rdev))),
	} {
		path = filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
			t.Fatal(err)
		}
	}
	driver := filepath.Join(root, "bus/platform/drivers/dw_wdt")
	if err := os.MkdirAll(driver, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(driver, filepath.Join(root, "class/watchdog/watchdog0/device/driver")); err != nil {
		t.Fatal(err)
	}
	return root, uint64(device.Rdev)
}

// /dev/null supplies a harmless real character-device stat; this test never
// opens it or any watchdog. Only the synthetic sysfs identifies it as a fixture.
func TestDe25WatchdogPreflight(t *testing.T) {
	root, expected := de25WatchdogFixture(t)
	got, err := prepareDe25WatchdogReset("arm64", root, "/dev/null")
	if err != nil || got != expected {
		t.Fatalf("qualified device identity: %d %v", got, err)
	}
	for name, mutate := range map[string]func(string){
		"wrong board": func(root string) { watchdogWrite(t, root, "firmware/devicetree/base/model", "Terasic DE10-nano\x00") },
		"wrong SoC": func(root string) {
			watchdogWrite(t, root, "firmware/devicetree/base/compatible", "altr,socfpga-cyclone5\x00")
		},
		"wrong compatible": func(root string) {
			watchdogWrite(t, root, "class/watchdog/watchdog0/device/of_node/compatible", "other\x00")
		},
		"another watchdog": func(root string) {
			watchdogWrite(t, root, "class/watchdog/watchdog0/device/of_node/reg", "\x10\xd0\x03\x00\x00\x00\x01\x00")
		},
		"wrong device":    func(root string) { watchdogWrite(t, root, "class/watchdog/watchdog0/dev", "1:5\n") },
		"malformed sysfs": func(root string) { watchdogWrite(t, root, "class/watchdog/watchdog0/dev", "1:3\n\n") },
		"missing identity": func(root string) {
			if err := os.Remove(filepath.Join(root, "class/watchdog/watchdog0/dev")); err != nil {
				t.Fatal(err)
			}
		},
		"different driver": func(root string) {
			path := filepath.Join(root, "class/watchdog/watchdog0/device/driver")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			other := filepath.Join(root, "other/dw_wdt")
			if err := os.MkdirAll(other, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(other, path); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			root, _ := de25WatchdogFixture(t)
			mutate(root)
			if _, err := prepareDe25WatchdogReset("arm64", root, "/dev/null"); err == nil {
				t.Fatal("accepted unqualified watchdog identity")
			}
		})
	}
	for _, arch := range []string{"arm", "amd64", ""} {
		if _, err := prepareDe25WatchdogReset(arch, root, "/dev/null"); err == nil {
			t.Fatalf("accepted CPU %q", arch)
		}
	}
	for _, device := range []string{filepath.Join(root, "class/watchdog/watchdog0/dev"), filepath.Join(root, "missing")} {
		if _, err := prepareDe25WatchdogReset("arm64", root, device); err == nil {
			t.Fatalf("accepted non-device %s", device)
		}
	}
	link := filepath.Join(root, "watchdog-link")
	if err := os.Symlink("/dev/null", link); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareDe25WatchdogReset("arm64", root, link); err == nil {
		t.Fatal("accepted a symlink device")
	}
}

func watchdogWrite(t *testing.T, root, path, raw string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, path), []byte(raw), 0444); err != nil {
		t.Fatal(err)
	}
}

func TestOpenedWatchdogKeepsPreflightIdentity(t *testing.T) {
	expected := unix.Mkdev(246, 0)
	st := unix.Stat_t{Mode: unix.S_IFCHR, Rdev: expected}
	if err := validateWatchdogDevice(st, expected); err != nil {
		t.Fatal(err)
	}
	st.Rdev = unix.Mkdev(246, 1)
	if err := validateWatchdogDevice(st, expected); err == nil {
		t.Fatal("accepted a different opened watchdog after preflight")
	}
	if err := validateWatchdogDevice(st, 0); err != nil {
		t.Fatal(err)
	}
	st.Mode = unix.S_IFREG
	if err := validateWatchdogDevice(st, 0); err == nil {
		t.Fatal("accepted a regular file")
	}
}

// Opt in only on a claimed DE25. This checks real sysfs/device metadata without
// opening the watchdog, arming it, programming the FPGA or changing mounts.
func TestNativeDe25WatchdogPreflight(t *testing.T) {
	if os.Getenv("FES_DE25_WATCHDOG_PREFLIGHT") != "1" {
		t.Skip("requires explicit native DE25 preflight")
	}
	if runtime.GOARCH != "arm64" {
		t.Fatal("native preflight requires AArch64")
	}
	device, err := PrepareDe25WatchdogReset()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("qualified stock watchdog0 identity %d:%d; device not opened", unix.Major(device), unix.Minor(device))
}
