//go:build linux

package rebootguard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func de25WatchdogFixture(t *testing.T) (string, uint64) {
	t.Helper()
	root := t.TempDir()
	var st unix.Stat_t
	if err := unix.Lstat("/dev/null", &st); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"firmware/devicetree/base/model":                     "SoCFPGA Agilex5 Terasic DE25-Nano\x00",
		"firmware/devicetree/base/compatible":                "intel,socfpga-agilex5-socdk\x00intel,socfpga-agilex5\x00",
		"class/watchdog/watchdog0/device/of_node/compatible": "snps,dw-wdt\x00",
		"class/watchdog/watchdog0/device/of_node/reg":        "\x10\xd0\x02\x00\x00\x00\x01\x00",
		"class/watchdog/watchdog0/dev":                       fmt.Sprintf("%d:%d\n", unix.Major(uint64(st.Rdev)), unix.Minor(uint64(st.Rdev))),
	} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0644); err != nil {
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
	return root, uint64(st.Rdev)
}

// Only /dev/null metadata is read. No fixture opens any character device.
func TestDe25WatchdogPreparation(t *testing.T) {
	legacy := func() error { t.Fatal("DE25 entered Cyclone V reset preparation"); return nil }
	root, expected := de25WatchdogFixture(t)
	actual, err := prepareWatchdogReset("arm64", root, "/dev/null", legacy)
	if err != nil || actual != expected {
		t.Fatalf("DE25 preparation: %d %v", actual, err)
	}
	for _, tc := range []struct{ name, path, value string }{
		{"wrong board", "firmware/devicetree/base/model", "Terasic DE10-nano\x00"},
		{"wrong SoC", "firmware/devicetree/base/compatible", "altr,socfpga-cyclone5\x00"},
		{"wrong watchdog", "class/watchdog/watchdog0/device/of_node/compatible", "other\x00"},
		{"wrong resource", "class/watchdog/watchdog0/device/of_node/reg", "\x10\xd0\x03\x00\x00\x00\x01\x00"},
		{"wrong device", "class/watchdog/watchdog0/dev", "1:5\n"},
		{"malformed device", "class/watchdog/watchdog0/dev", "1:3\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := de25WatchdogFixture(t)
			if err := os.WriteFile(filepath.Join(root, tc.path), []byte(tc.value), 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := prepareWatchdogReset("arm64", root, "/dev/null", legacy); err == nil {
				t.Fatal("accepted differing identity")
			}
		})
	}
	for _, arch := range []string{"arm", "amd64", ""} {
		if _, err := prepareWatchdogReset(arch, root, "/dev/null", legacy); err == nil {
			t.Fatalf("accepted architecture %q", arch)
		}
	}
	for _, device := range []string{filepath.Join(root, "missing"), filepath.Join(root, "class/watchdog/watchdog0/dev")} {
		if _, err := prepareWatchdogReset("arm64", root, device, legacy); err == nil {
			t.Fatalf("accepted non-device %s", device)
		}
	}
	link := filepath.Join(root, "watchdog-link")
	if err := os.Symlink("/dev/null", link); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareWatchdogReset("arm64", root, link, legacy); err == nil {
		t.Fatal("accepted linked watchdog")
	}
	t.Run("missing sysfs", func(t *testing.T) {
		root, _ := de25WatchdogFixture(t)
		if err := os.Remove(filepath.Join(root, "class/watchdog/watchdog0/dev")); err != nil {
			t.Fatal(err)
		}
		if _, err := prepareWatchdogReset("arm64", root, "/dev/null", legacy); err == nil {
			t.Fatal("accepted absent sysfs identity")
		}
	})
	t.Run("different driver", func(t *testing.T) {
		root, _ := de25WatchdogFixture(t)
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
		if _, err := prepareWatchdogReset("arm64", root, "/dev/null", legacy); err == nil {
			t.Fatal("accepted differently located driver")
		}
	})
}

func TestRebootPreparationPreservesDe10Path(t *testing.T) {
	root, _ := de25WatchdogFixture(t)
	for name, raw := range map[string]string{"model": "Terasic DE10-nano\x00", "compatible": "altr,socfpga-cyclone5\x00altr,socfpga\x00"} {
		if err := os.WriteFile(filepath.Join(root, "firmware/devicetree/base", name), []byte(raw), 0644); err != nil {
			t.Fatal(err)
		}
	}
	want := errors.New("legacy reset readback failed")
	called := 0
	got, err := prepareWatchdogReset("arm", root, "/missing-watchdog", func() error { called++; return want })
	if got != 0 || !errors.Is(err, want) || called != 1 {
		t.Fatalf("changed DE10 preparation result: %d %v calls%d", got, err, called)
	}
	_, err = prepareWatchdogReset("arm64", root, "/missing-watchdog", func() error { called++; return nil })
	if err == nil || called != 1 {
		t.Fatal("crossed board invoked legacy reset preparation")
	}
}

func TestArmRequiresPreparedWatchdogIdentity(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprint(changed), func(t *testing.T) {
			d := &fakeWatchdog{char: true, actual: 180 * time.Second, identity: unix.Mkdev(246, 0)}
			c, _, _ := testConfig(t, d, &fakeHelper{})
			c.prepare = func() (uint64, error) { return unix.Mkdev(246, 0), nil }
			if changed {
				d.identity = unix.Mkdev(246, 1)
			}
			result, err := Arm(t.Context(), c)
			if changed {
				if err == nil || result.Watchdog || !d.closed || string(d.writes) != "V" || d.timeout != 0 {
					t.Fatalf("changed descriptor armed: %+v %+v %v", d, result, err)
				}
			} else if err != nil || !result.Watchdog || d.closed {
				t.Fatalf("matching descriptor refused: %+v %v", result, err)
			}
			if err := result.Armed.Cancel(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestStaleDisarmRequiresPreparation(t *testing.T) {
	d := &fakeWatchdog{char: true}
	c, opened, _ := testConfig(t, d, &fakeHelper{})
	c.prepare = func() (uint64, error) { return 0, errors.New("wrong board") }
	DisarmStale(c, false)
	if *opened != 0 {
		t.Fatal("opened watchdog after rejected preparation")
	}
}

func TestStaleDisarmUsesIdentityPreparationWithoutResetWrites(t *testing.T) {
	d := &fakeWatchdog{char: true, identity: unix.Mkdev(246, 0)}
	c, opened, _ := testConfig(t, d, &fakeHelper{})
	resetCalls, identityCalls := 0, 0
	c.prepare = func() (uint64, error) {
		resetCalls++
		return 0, errors.New("reset-register preparation must not run")
	}
	c.prepareDisarm = func() (uint64, error) {
		identityCalls++
		return d.identity, nil
	}
	DisarmStale(c, false)
	if resetCalls != 0 || identityCalls != 1 || *opened != 1 || !d.closed || string(d.writes) != "V" {
		t.Fatalf("stale disarm changed reset policy: reset=%d identity=%d opened=%d device=%+v", resetCalls, identityCalls, *opened, d)
	}
}

// Opt in on a claimed native DE25 only. Reads metadata; never opens the timer.
func TestNativeDe25RebootWatchdogPreflight(t *testing.T) {
	if os.Getenv("FES_DE25_REBOOT_WATCHDOG_PREFLIGHT") != "1" {
		t.Skip("requires claimed native DE25")
	}
	if runtime.GOARCH != "arm64" || os.Geteuid() != 0 {
		t.Fatal("requires native AArch64 root")
	}
	c := (Config{}).defaults()
	if c.Device != "/dev/watchdog0" {
		t.Fatal("DE25 must select watchdog0")
	}
	id, err := c.prepare()
	if err != nil {
		t.Fatal(err)
	}
	staleID, err := c.prepareDisarm()
	if err != nil || staleID != id {
		t.Fatalf("stale-disarm identity differs: %d %v", staleID, err)
	}
	t.Logf("qualified stock DW watchdog0 %d:%d, not opened", unix.Major(id), unix.Minor(id))
}

// The external claimed-kit harness quiesces services and makes storage read-only.
// Opt-in requires the exact current boot, rather than a reusable boolean flag.
func TestNativeDe25RebootWatchdogArmCancel(t *testing.T) {
	expected := os.Getenv("FES_DE25_REBOOT_WATCHDOG_ARM_CANCEL_BOOT")
	if expected == "" {
		t.Skip("requires claimed native DE25 read-only trial")
	}
	if runtime.GOARCH != "arm64" || os.Geteuid() != 0 {
		t.Fatal("requires native AArch64 root")
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil || strings.TrimSpace(string(boot)) != expected {
		t.Fatal("boot differs from trial")
	}
	mounts, err := os.ReadFile("/proc/mounts")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, line := range strings.Split(string(mounts), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		if !strings.HasPrefix(fields[0], "/dev/mmcblk0p") && fields[0] != "/dev/root" && fields[0] != "/dev/loop0" {
			continue
		}
		opts := "," + fields[3] + ","
		if !strings.Contains(opts, ",ro,") || strings.Contains(opts, ",rw,") {
			t.Fatal("SD-backed mount remains writable")
		}
		count++
	}
	if count != 9 {
		t.Fatal("expected all nine SD-backed mounts")
	}
	c := (Config{}).defaults()
	result, err := Arm(t.Context(), c)
	if result.Armed != nil {
		t.Cleanup(func() {
			if err := result.Armed.Cancel(); err != nil {
				t.Error(err)
			}
		})
	}
	if err != nil || !result.Watchdog || !result.Fallback || result.Armed == nil {
		t.Fatalf("real arm: %+v %v", result, err)
	}
	if err := result.Armed.Cancel(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(c.Marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("marker not retired: %v", err)
	}
	t.Logf("native watchdog armed timeout=%s and magic-closed; fallback killed/reaped; marker removed; boot=%s", result.ActualTimeout, expected)
}
