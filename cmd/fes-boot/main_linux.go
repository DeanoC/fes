//go:build linux

// fes-boot is the fixed appliance bootstrap PID 1 and its watchdog child.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	release "github.com/DeanoC/FogCast/appliance"
	store "github.com/DeanoC/FogCast/appliance/store"
	"github.com/DeanoC/FogCast/internal/applianceboot"
	"github.com/DeanoC/FogCast/internal/bootlinux"
	"golang.org/x/sys/unix"
)

const factoryFile = "/etc/fes/factory.json"
const bootIDFile = "/proc/sys/kernel/random/boot_id"
const mountpoint = "/run/fes-root"

func main() {
	guard := flag.Bool("guard", false, "run independent trial watchdog")
	bootID := flag.String("boot-id", "", "trial boot identity")
	imageSHA := flag.String("image-sha256", "", "trial image identity")
	flag.Parse()
	var err error
	if *guard {
		err = guardMain(*bootID, *imageSHA)
	} else if os.Getpid() != 1 {
		err = errors.New("bootstrap must run as PID 1")
	} else {
		err = bootMain()
	}
	if err == nil && *guard {
		return
	}
	fmt.Fprintln(os.Stderr, "fes-boot:", err)
	if os.Getpid() == 1 {
		unix.Sync()
		_ = unix.Reboot(unix.LINUX_REBOOT_CMD_RESTART)
		// PID 1 must not exit if the syscall is unavailable. An armed trial guard
		// still resets the board. Console evidence remains for media failure.
		for {
			time.Sleep(time.Second)
		}
	}
	os.Exit(1)
}

func openStore() (*store.Store, release.Manifest, error) {
	f, err := os.Open(factoryFile)
	if err != nil {
		return nil, release.Manifest{}, err
	}
	defer f.Close()
	factory, err := release.DecodeManifest(f)
	if err != nil {
		return nil, factory, err
	}
	s, err := store.New(store.DefaultRoot, factory)
	return s, factory, err
}

func bootMain() error {
	// Kernel has already loop-mounted this ext4 bootstrap and bind-mounted FAT.
	// These directories are baked into the stable image; only tmpfs is writable.
	for _, m := range []struct {
		source, target, kind string
		flags                uintptr
	}{
		{"proc", "/proc", "proc", unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC},
		{"sysfs", "/sys", "sysfs", unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC},
		{"devtmpfs", "/dev", "devtmpfs", unix.MS_NOSUID},
		{"tmpfs", "/run", "tmpfs", unix.MS_NOSUID | unix.MS_NODEV},
	} {
		if err := unix.Mount(m.source, m.target, m.kind, m.flags, ""); err != nil && !errors.Is(err, unix.EBUSY) {
			return fmt.Errorf("mount %s: %w", m.target, err)
		}
	}
	if err := os.MkdirAll(mountpoint, 0755); err != nil {
		return err
	}
	raw, err := os.ReadFile(bootIDFile)
	if err != nil {
		return err
	}
	s, factory, err := openStore()
	if err != nil {
		return err
	}
	return applianceboot.Run(context.Background(), s, factory.ImageSHA256, strings.TrimSpace(string(raw)), linuxPlatform{})
}

func guardMain(bootID, image string) error {
	ready := os.NewFile(3, "armed-ack")
	if ready == nil {
		return errors.New("missing guard acknowledgement pipe")
	}
	defer ready.Close()
	s, _, err := openStore()
	if err != nil {
		return err
	}
	if err := bootlinux.PrepareSoCFPGAWatchdogReset(); err != nil {
		return err
	}
	return bootlinux.RunGuard(context.Background(), bootlinux.GuardConfig{BootID: bootID, ImageSHA256: image}, ready,
		func(ctx context.Context, id, hash string) (bool, error) {
			return s.ConfirmedContext(ctx, id, hash)
		})
}

type linuxPlatform struct{}
type linuxRoot struct{ *bootlinux.Root }

func (linuxPlatform) Prepare(path string) (applianceboot.Root, error) {
	root, err := bootlinux.PrepareRoot(path, mountpoint)
	if err != nil {
		return nil, err
	}
	return linuxRoot{root}, nil
}
func (linuxRoot linuxRoot) Exec() error {
	return linuxRoot.PivotAndExec("/sbin/init", []string{"/sbin/init"}, []string{"PATH=/sbin:/bin:/usr/sbin:/usr/bin", "HOME=/", "TERM=linux"})
}
func (linuxPlatform) Arm(ctx context.Context, choice store.Selection) error {
	_, err := bootlinux.StartGuard(ctx, "/sbin/init", []string{"--guard", "--boot-id", choice.BootID, "--image-sha256", choice.Manifest.ImageSHA256})
	return err
}
func (linuxPlatform) Ticket(choice store.Selection) error {
	return writeTicket(filepath.Join(store.DefaultRoot, "boot.json"), choice)
}
func writeTicket(path string, choice store.Selection) error {
	ticket := struct {
		BootID      string `json:"boot_id"`
		ImageSHA256 string `json:"image_sha256"`
		Trial       bool   `json:"trial"`
	}{choice.BootID, choice.Manifest.ImageSHA256, choice.Trial}
	bytes, err := json.Marshal(ticket)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".boot-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err = f.Write(append(bytes, '\n')); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
