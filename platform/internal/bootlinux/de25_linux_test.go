//go:build linux

package bootlinux

import (
	"strings"
	"testing"
)

func TestDe25PartitionBootstrapAdmission(t *testing.T) {
	model := "SoCFPGA Agilex5 Terasic DE25-Nano\x00"
	compatible := "intel,socfpga-agilex5-socdk\x00intel,socfpga-agilex5\x00"
	root := "22 1 179:2 / / rw,relatime shared:1 - ext4 /dev/root rw\n"
	proc := "23 22 0:1 / /proc rw - proc proc rw\n"
	for _, readOnly := range []bool{false, true} {
		mounts := root + proc
		if readOnly {
			mounts = strings.Replace(mounts, "rw,relatime", "ro,relatime", 1)
		}
		got, err := validateDe25BootVolume("arm64", []byte(model), []byte(compatible), []byte(mounts), []byte("179:2\n"))
		if err != nil || got != readOnly {
			t.Fatalf("read-only %v: %v %v", readOnly, got, err)
		}
	}
	for name, change := range map[string]func() (string, string, string, string, string){
		"wrong CPU": func() (string, string, string, string, string) { return "arm", model, compatible, root, "179:2\n" },
		"wrong board": func() (string, string, string, string, string) {
			return "arm64", "other\x00", compatible, root, "179:2\n"
		},
		"wrong SoC": func() (string, string, string, string, string) {
			return "arm64", model, "altr,socfpga\x00", root, "179:2\n"
		},
		"other root": func() (string, string, string, string, string) { return "arm64", model, compatible, root, "179:3\n" },
		"bind root": func() (string, string, string, string, string) {
			return "arm64", model, compatible, strings.Replace(root, " / / ", " /subdir / ", 1), "179:2\n"
		},
		"loop root": func() (string, string, string, string, string) {
			return "arm64", model, compatible, strings.Replace(root, "/dev/root", "/dev/loop0", 1), "179:2\n"
		},
		"wrong filesystem": func() (string, string, string, string, string) {
			return "arm64", model, compatible, strings.Replace(root, "ext4", "tmpfs", 1), "179:2\n"
		},
		"stacked root": func() (string, string, string, string, string) {
			return "arm64", model, compatible, root + root, "179:2\n"
		},
		"missing root": func() (string, string, string, string, string) { return "arm64", model, compatible, proc, "179:2\n" },
		"ambiguous flags": func() (string, string, string, string, string) {
			return "arm64", model, compatible, strings.Replace(root, "rw,relatime", "rw,ro", 1), "179:2\n"
		},
		"already mounted FAT": func() (string, string, string, string, string) {
			return "arm64", model, compatible, root + "24 22 179:1 / /media/fat rw - vfat /dev/mmcblk0p1 rw\n", "179:2\n"
		},
		"broken mountinfo": func() (string, string, string, string, string) {
			return "arm64", model, compatible, "22 1 179:2\n", "179:2\n"
		},
		"broken sysfs": func() (string, string, string, string, string) { return "arm64", model, compatible, root, "179:2\n\n" },
	} {
		t.Run(name, func(t *testing.T) {
			a, m, c, mounts, dev := change()
			if _, err := validateDe25BootVolume(a, []byte(m), []byte(c), []byte(mounts), []byte(dev)); err == nil {
				t.Fatal("accepted invalid boot volume")
			}
		})
	}
}
