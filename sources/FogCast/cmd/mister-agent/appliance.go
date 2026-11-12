package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"

	release "github.com/DeanoC/FogCast/appliance"
	"github.com/DeanoC/FogCast/appliance/store"
	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/appliancesplash"
	"github.com/DeanoC/FogCast/internal/applianceupdate"
	"github.com/DeanoC/FogCast/internal/rebootguard"
)

const applianceFactory = "/.fes-bootstrap/etc/fes/factory.json"

var armReboot = rebootguard.Arm
var disarmStaleReboot = rebootguard.DisarmStale
var cancelReboot = func(handle *rebootguard.Armed) error { return handle.Cancel() }
var runReboot = func(ctx context.Context) error { return exec.CommandContext(ctx, rebootCommand).Run() }

func guardedReboot(parent context.Context) error {
	// finish has already committed to rebooting after flushing the response. A
	// client disconnect during the bounded watchdog handover must not drop the
	// reboot request (which would abort the activation), so detach cancellation.
	ctx := context.WithoutCancel(parent)
	cancel := armRebootBackstop(ctx, "appliance")
	err := runReboot(ctx)
	if err != nil {
		cancel()
	}
	return err
}

// startRecoveryReboot keeps the existing asynchronous recovery command, with
// the same backstop as an appliance update. Invalid/canceled requests never arm
// it. Once armed, command lifetime is independent of the HTTP caller.
func startRecoveryReboot(parent context.Context, path string) error {
	if err := parent.Err(); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return errors.New("recovery reboot command is unavailable")
	}
	if err := parent.Err(); err != nil {
		return err
	}
	cancel := armRebootBackstop(context.WithoutCancel(parent), "development")
	command := exec.Command(path)
	if err := command.Start(); err != nil {
		cancel()
		return err
	}
	go func() {
		if err := command.Wait(); err != nil {
			// Init may signal this child during an already-committed shutdown.
			// Only a normal failed exit proves that the request failed; an
			// interrupted command must retain the shutdown backstop.
			if command.ProcessState != nil && command.ProcessState.Exited() {
				cancel()
			}
			slog.Warn("development reboot command failed", "error", err)
		}
	}()
	return nil
}

func armRebootBackstop(ctx context.Context, purpose string) func() {
	result, armErr := armReboot(ctx, rebootguard.Config{})
	slog.Info(purpose+" reboot backstop", "reason", result.Reason, "fallback", result.Fallback, "watchdog", result.Watchdog, "timeout", result.ActualTimeout)
	if armErr != nil {
		slog.Warn(purpose+" reboot backstop incomplete", "error", armErr)
	}
	// Capture the owner of this handle before asynchronous command retirement.
	cancel := cancelReboot
	return func() {
		if result.Armed != nil {
			if err := cancel(result.Armed); err != nil {
				slog.Warn(purpose+" reboot backstop cancel failed", "error", err)
			}
		}
	}
}

func loadApplianceUpdate(coordinator *agent.Coordinator, cleanup func(context.Context) error) (*applianceupdate.Service, error) {
	board, err := applianceupdate.ImageBoard(applianceFactory, "/etc/fes-board", runtime.GOARCH)
	if err != nil {
		return nil, err
	}
	return openApplianceUpdateForBoard(applianceFactory, "/proc/self/mountinfo", appliance.DefaultRoot, bootIDFile, coordinator, cleanup,
		guardedReboot, board)
}

func openApplianceUpdateForBoard(factoryPath, mountInfoPath, root, bootPath string, coordinator *agent.Coordinator, cleanup, reboot func(context.Context) error, board string) (*applianceupdate.Service, error) {
	info, err := os.Lstat(factoryPath)
	if errors.Is(err, os.ErrNotExist) {
		// A direct-root development image has no bootstrap. A mounted bootstrap
		// with a missing factory record is damaged and must not silently disable
		// trial admission or pretend to be that older direct-root layout.
		mounts, readErr := os.ReadFile(mountInfoPath)
		if readErr != nil {
			return nil, readErr
		}
		for _, line := range strings.Split(string(mounts), "\n") {
			fields := strings.Fields(line)
			if len(fields) > 4 && fields[4] == "/.fes-bootstrap" {
				return nil, errors.New("mounted bootstrap has no factory manifest")
			}
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("factory manifest is not a regular file")
	}
	f, err := os.Open(factoryPath)
	if err != nil {
		return nil, err
	}
	factory, _, err := release.DecodeManifestForBoard(f, board)
	f.Close()
	if err != nil {
		return nil, err
	}
	boot, err := applianceupdate.ReadBootIdentity(root+"/boot.json", bootPath)
	if err != nil {
		return nil, err
	}
	store, err := appliance.NewForBoard(root, factory, board)
	if err != nil {
		return nil, err
	}
	if _, err = store.Verify(boot.ImageSHA256); err != nil {
		return nil, err
	}
	service := applianceupdate.NewWithSplash(store, coordinator, boot, reboot, cleanup, appliancesplash.New(appliancesplash.Paths{
		ReleaseRoot: root,
	}))
	disarmStaleReboot(rebootguard.Config{}, boot.Trial)
	return service, nil
}
