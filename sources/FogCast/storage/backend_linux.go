//go:build linux

package storage

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/DeanoC/FogCast/appliance/blockprobe"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

const nonblock = syscall.O_NONBLOCK

type systemBackend struct{}

var mountMu sync.Mutex

type blockDevice struct {
	Path       string        `json:"path"`
	Transport  string        `json:"tran"`
	UUID       string        `json:"uuid"`
	Label      string        `json:"label"`
	FSType     string        `json:"fstype"`
	Mountpoint string        `json:"mountpoint"`
	Children   []blockDevice `json:"children"`
}

func usbDevices(ctx context.Context) ([]blockDevice, error) {
	names, err := blockprobe.Usable("/sys/class/block")
	if err != nil {
		return nil, err
	}
	args := []string{"--json", "--paths", "--nodeps", "--output", "PATH,UUID,LABEL,FSTYPE,MOUNTPOINT"}
	for _, name := range names {
		path, err := filepath.EvalSymlinks(filepath.Join("/sys/class/block", name))
		if err == nil && strings.Contains(path, "/usb") {
			args = append(args, filepath.Join("/dev", name))
		}
	}
	if len(args) == 5 {
		return []blockDevice{}, nil
	}
	body, err := exec.CommandContext(ctx, "lsblk", args...).Output()
	if err != nil {
		return nil, errors.New("USB discovery needs lsblk and permission to read block devices")
	}
	var all struct {
		Devices []blockDevice `json:"blockdevices"`
	}
	if err = json.Unmarshal(body, &all); err != nil {
		return nil, err
	}
	return collectUSB(all.Devices, true), nil
}

func collectUSB(devices []blockDevice, usb bool) []blockDevice {
	var out []blockDevice
	for _, d := range devices {
		external := usb || d.Transport == "usb"
		if external && d.UUID != "" && d.FSType != "" && d.FSType != "swap" && d.FSType != "crypto_LUKS" {
			out = append(out, d)
		}
		out = append(out, collectUSB(d.Children, external)...)
	}
	return out
}

func (systemBackend) Discover(ctx context.Context) ([]Drive, error) {
	devices, err := usbDevices(ctx)
	if err != nil {
		return nil, err
	}
	drives := []Drive{}
	for _, d := range devices {
		label := d.Label
		if label == "" {
			label = filepath.Base(d.Path) + " · " + d.FSType
		}
		drives = append(drives, Drive{Kind: "usb", Source: d.UUID, Label: label})
	}
	return drives, nil
}

func mountSource(target string) (string, error) {
	data, err := os.ReadFile("/proc/self/mounts")
	if err != nil {
		return "", err
	}
	unescape := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 2 && unescape.Replace(fields[1]) == target {
			return unescape.Replace(fields[0]), nil
		}
	}
	return "", nil
}

func (systemBackend) Mount(ctx context.Context, d Drive, target string) (string, error) {
	mountMu.Lock()
	defer mountMu.Unlock()
	if d.Kind != "usb" {
		return "", errors.New("only USB volumes use operating system mounts")
	}
	source := d.Source
	filesystem := ""
	{
		devices, err := usbDevices(ctx)
		if err != nil {
			return "", err
		}
		matches := []blockDevice{}
		for _, dev := range devices {
			if dev.UUID == d.Source {
				matches = append(matches, dev)
			}
		}
		if len(matches) != 1 {
			return "", errors.New("USB drive is disconnected or its UUID is ambiguous")
		}
		source = matches[0].Path
		filesystem = matches[0].FSType
		if matches[0].Mountpoint != "" {
			return matches[0].Mountpoint, nil
		}
	}
	mounted, err := mountSource(target)
	if err != nil {
		return "", err
	}
	if mounted == source {
		return target, nil
	}
	if mounted != "" {
		return "", errors.New("drive mount point is in use by another volume")
	}
	if os.Geteuid() != 0 {
		err = exec.CommandContext(ctx, "udisksctl", "mount", "--no-user-interaction", "--block-device", source, "--options", "ro,nosuid,nodev,noexec").Run()
		if err != nil {
			return "", errors.New("USB mounting needs desktop device permission; mount the drive on the host and refresh")
		}
		devices, err := usbDevices(ctx)
		if err != nil {
			return "", err
		}
		for _, device := range devices {
			if device.UUID == d.Source && device.Mountpoint != "" {
				return device.Mountpoint, nil
			}
		}
		return "", errors.New("USB drive has no mount point")
	}
	if err = os.MkdirAll(target, 0700); err != nil {
		return "", errors.New("cannot create USB mount point; use an already mounted volume or run the kit service as root")
	}
	command := "mount"
	args := []string{"-t", filesystem, "-o", "ro,nosuid,nodev,noexec", source, target}
	if filesystem == "ntfs" {
		command = "ntfs-3g"
		args = []string{"-o", "ro,nosuid,nodev,noexec", source, target}
	}
	// The ext4 driver also reads ext2/ext3; those separate drivers are not built into the kit kernel.
	if filesystem == "ext2" || filesystem == "ext3" {
		args[1] = "ext4"
	}
	err = exec.CommandContext(ctx, command, args...).Run()
	if err != nil {
		return "", errors.New("could not mount USB drive; check filesystem support and mount permissions")
	}
	mounted, err = mountSource(target)
	if err != nil {
		return "", err
	}
	if mounted != source {
		return "", errors.New("mounted volume does not match selected drive")
	}
	return target, nil
}
