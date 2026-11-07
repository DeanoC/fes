//go:build linux

package bootlinux

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"golang.org/x/sys/unix"
)

// PrepareDe25WatchdogReset identifies the qualified stock DW watchdog without
// opening it. Native expiry recovered Linux both before programming and with
// MENU scanout active. That boot path requires no reset-register writes.
// The returned device identity must also match the descriptor opened by RunGuard.
func PrepareDe25WatchdogReset() (uint64, error) {
	return prepareDe25WatchdogReset(runtime.GOARCH, "/sys", "/dev/watchdog0")
}

func prepareDe25WatchdogReset(arch, sysRoot, device string) (uint64, error) {
	read := func(path string) ([]byte, error) {
		raw, err := os.ReadFile(filepath.Join(sysRoot, path))
		if err != nil {
			return nil, fmt.Errorf("read DE25 watchdog identity %s: %w", path, err)
		}
		return raw, nil
	}
	model, err := read("firmware/devicetree/base/model")
	if err != nil {
		return 0, err
	}
	compatible, err := read("firmware/devicetree/base/compatible")
	if err != nil {
		return 0, err
	}
	if err := validateDe25Board(arch, model, compatible); err != nil {
		return 0, err
	}
	const watchdog = "class/watchdog/watchdog0/"
	for _, expected := range []struct {
		path string
		raw  []byte
	}{
		{watchdog + "device/of_node/compatible", []byte("snps,dw-wdt\x00")},
		{watchdog + "device/of_node/reg", []byte{0x10, 0xd0, 0x02, 0, 0, 0, 1, 0}},
	} {
		raw, err := read(expected.path)
		if err != nil {
			return 0, err
		}
		if !bytes.Equal(raw, expected.raw) {
			return 0, errors.New("DE25 watchdog0 must be the stock DW watchdog at 0x10d00200")
		}
	}
	driver, err := filepath.EvalSymlinks(filepath.Join(sysRoot, watchdog, "device/driver"))
	if err != nil {
		return 0, fmt.Errorf("resolve DE25 watchdog driver: %w", err)
	}
	if driver != filepath.Join(sysRoot, "bus/platform/drivers/dw_wdt") {
		return 0, errors.New("DE25 watchdog0 must use the stock dw_wdt platform driver")
	}
	var st unix.Stat_t
	if err := unix.Lstat(device, &st); err != nil {
		return 0, fmt.Errorf("stat DE25 watchdog0: %w", err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFCHR {
		return 0, errors.New("DE25 watchdog0 must be a real character device")
	}
	identity, err := read(watchdog + "dev")
	if err != nil {
		return 0, err
	}
	if string(identity) != fmt.Sprintf("%d:%d\n", unix.Major(uint64(st.Rdev)), unix.Minor(uint64(st.Rdev))) {
		return 0, errors.New("DE25 watchdog0 character device differs from sysfs")
	}
	return uint64(st.Rdev), nil
}
