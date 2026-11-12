//go:build linux

package rebootguard

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// The DE25 checks mirror platform/internal/bootlinux/de25_watchdog_linux.go.
// Stock DW watchdog expiry reloads the qualified Terasic firmware without reset
// register writes. DE10 continues through its existing Cyclone V preparation.
func prepareWatchdogReset(arch, sysRoot, device string, de10 func() error) (uint64, error) {
	read := func(path string) ([]byte, error) {
		raw, err := os.ReadFile(filepath.Join(sysRoot, path))
		if err != nil {
			return nil, fmt.Errorf("read reboot watchdog identity %s: %w", path, err)
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
	if arch == "arm" && validateSoCFPGABoard(arch, model, compatible) == nil {
		return 0, de10()
	}
	if arch != "arm64" || !bytes.Equal(model, []byte("SoCFPGA Agilex5 Terasic DE25-Nano\x00")) ||
		!bytes.Equal(compatible, []byte("intel,socfpga-agilex5-socdk\x00intel,socfpga-agilex5\x00")) {
		return 0, errors.New("reboot watchdog requires exact DE10 ARM or DE25 AArch64 board identity")
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
			return 0, errors.New("DE25 reboot watchdog must be stock DW watchdog0 at 0x10d00200")
		}
	}
	driver, err := filepath.EvalSymlinks(filepath.Join(sysRoot, watchdog, "device/driver"))
	if err != nil {
		return 0, fmt.Errorf("resolve DE25 reboot watchdog driver: %w", err)
	}
	if driver != filepath.Join(sysRoot, "bus/platform/drivers/dw_wdt") {
		return 0, errors.New("DE25 reboot watchdog must use stock dw_wdt driver")
	}
	var st unix.Stat_t
	if err := unix.Lstat(device, &st); err != nil {
		return 0, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFCHR {
		return 0, errors.New("DE25 reboot watchdog must be a real character device")
	}
	identity, err := read(watchdog + "dev")
	if err != nil {
		return 0, err
	}
	if string(identity) != fmt.Sprintf("%d:%d\n", unix.Major(uint64(st.Rdev)), unix.Minor(uint64(st.Rdev))) {
		return 0, errors.New("DE25 reboot watchdog device differs from watchdog0 sysfs")
	}
	return uint64(st.Rdev), nil
}
