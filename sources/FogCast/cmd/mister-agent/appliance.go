package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	release "github.com/DeanoC/FogCast/appliance"
	"github.com/DeanoC/FogCast/appliance/store"
	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/applianceupdate"
	"github.com/DeanoC/FogCast/internal/rebootguard"
)

const applianceFactory = "/.fes-bootstrap/etc/fes/factory.json"

var armReboot = rebootguard.Arm
var disarmStaleReboot = rebootguard.DisarmStale
var cancelReboot = func(handle *rebootguard.Armed) error { return handle.Cancel() }
var runReboot = func(ctx context.Context) error { return exec.CommandContext(ctx, rebootCommand).Run() }

func guardedReboot(ctx context.Context) error {
	result, armErr := armReboot(ctx, rebootguard.Config{})
	if armErr != nil {
		slog.Warn("appliance reboot backstop not armed", "reason", result.Reason, "timeout", result.ActualTimeout, "error", armErr)
	} else {
		slog.Info("appliance reboot backstop", "reason", result.Reason, "timeout", result.ActualTimeout)
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
	return openApplianceUpdate(applianceFactory, "/proc/self/mountinfo", appliance.DefaultRoot, bootIDFile, coordinator, cleanup,
		guardedReboot)
}

func openApplianceUpdate(factoryPath, mountInfoPath, root, bootPath string, coordinator *agent.Coordinator, cleanup, reboot func(context.Context) error) (*applianceupdate.Service, error) {
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
	factory, err := release.DecodeManifest(f)
	f.Close()
	if err != nil {
		return nil, err
	}
	boot, err := applianceupdate.ReadBootIdentity(root+"/boot.json", bootPath)
	if err != nil {
		return nil, err
	}
	store, err := appliance.New(root, factory)
	if err != nil {
		return nil, err
	}
	if _, err = store.Verify(boot.ImageSHA256); err != nil {
		return nil, err
	}
	service := applianceupdate.New(store, coordinator, boot, reboot, cleanup)
	disarmStaleReboot(rebootguard.Config{}, boot.Trial)
	return service, nil
}
