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
	result, armErr := armReboot(ctx, rebootguard.Config{})
	slog.Info("appliance reboot backstop", "reason", result.Reason, "fallback", result.Fallback, "watchdog", result.Watchdog, "timeout", result.ActualTimeout)
	if armErr != nil {
		slog.Warn("appliance reboot backstop incomplete", "error", armErr)
	}
	err := runReboot(ctx)
	if err != nil && result.Armed != nil {
		if cancelErr := cancelReboot(result.Armed); cancelErr != nil {
			slog.Warn("appliance reboot backstop cancel failed", "error", cancelErr)
		}
	}
	return err
}

func loadApplianceUpdate(coordinator *agent.Coordinator, cleanup func(context.Context) error) (*applianceupdate.Service, error) {
	board, err := applianceupdate.ImageBoard(applianceFactory, "/etc/fes-board", runtime.GOARCH)
	if err != nil {
		return nil, err
	}
	return openApplianceUpdateForBoard(applianceFactory, "/proc/self/mountinfo", appliance.DefaultRoot, bootIDFile, coordinator, cleanup,
		guardedReboot, board)
}

func openApplianceUpdate(factoryPath, mountInfoPath, root, bootPath string, coordinator *agent.Coordinator, cleanup, reboot func(context.Context) error) (*applianceupdate.Service, error) {
	return openApplianceUpdateForBoard(factoryPath, mountInfoPath, root, bootPath, coordinator, cleanup, reboot, release.Board)
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
