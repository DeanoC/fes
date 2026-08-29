//go:build linux && fpgadev

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/DeanoC/FogCast-POC/internal/fpgadev"
)

type installCommandRunner interface {
	Install(context.Context, string) error
	Recover(context.Context) error
	Uninstall(context.Context) error
}

type recordedAgentObserver interface {
	SnapshotContext(context.Context) ([]fpgadev.ProcessIdentity, error)
}

// stopAgentOnlyWhenAbsent is the production stop policy.  The install
// transition is allowed to continue only after one complete, successful
// observer scan proves that no recorded agent process remains.  A scan error
// or any non-empty result is fail-closed; reboot is the caller's recovery
// path, not an excuse to proceed with an ambiguous process population.
func stopAgentOnlyWhenAbsent(observer recordedAgentObserver) func(context.Context) error {
	return func(ctx context.Context) error {
		if ctx == nil {
			ctx = context.Background()
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if observer == nil {
			return errors.New("agent observer is unavailable")
		}
		identities, err := observer.SnapshotContext(ctx)
		if err != nil {
			return err
		}
		if len(identities) != 0 {
			return errors.New("agent process remains present")
		}
		return ctx.Err()
	}
}

func installCommandName(name string) bool {
	switch name {
	case "install-profile", "recover-install", "uninstall-profile":
		return true
	default:
		return false
	}
}

func runInstallCommand(args []string, stdout, stderr io.Writer, runner commandRunner) int {
	return runInstallCommandWithPrivilege(args, stdout, stderr, runner, func() bool { return os.Geteuid() == 0 })
}

func runInstallCommandWithPrivilege(args []string, stdout, stderr io.Writer, runner commandRunner, privileged func() bool) int {
	packageRoot := ""
	valid := len(args) == 1 && installCommandName(args[0])
	if len(args) == 3 && args[0] == "install-profile" && args[1] == "--package-root" && validPackageRootArg(args[2]) {
		packageRoot, valid = args[2], true
	}
	if privileged == nil || !privileged() || !valid {
		_, _ = io.WriteString(stderr, "usage: mister-fpga-dev install-profile [--package-root PATH]|recover-install|uninstall-profile\n")
		return exitUsage
	}
	manager, ok := runner.(installCommandRunner)
	if !ok || manager == nil {
		var candidate any = productionInstallManager()
		manager, ok = candidate.(installCommandRunner)
	}
	if manager == nil {
		if productionInstallPrerequisiteError != nil {
			_, _ = fmt.Fprintf(stderr, "FOGCAST_FPGA_DEV_INSTALL code=unavailable detail=%s\n", productionInstallPrerequisiteError)
		} else {
			_, _ = io.WriteString(stderr, "FOGCAST_FPGA_DEV_INSTALL code=unavailable\n")
		}
		return exitFailure
	}
	var err error
	switch args[0] {
	case "install-profile":
		err = manager.Install(context.Background(), packageRoot)
	case "recover-install":
		err = manager.Recover(context.Background())
	case "uninstall-profile":
		err = manager.Uninstall(context.Background())
	}
	if err != nil && !errors.Is(err, fpgadev.ErrRebootRequested) {
		_, _ = fmt.Fprintln(stderr, "FOGCAST_FPGA_DEV_INSTALL code=unavailable")
		return exitFailure
	}
	_, _ = fmt.Fprintf(stdout, "FOGCAST_FPGA_DEV_INSTALL command=%s code=ok\n", args[0])
	return exitOK
}

func validPackageRootArg(path string) bool {
	return path != "" && filepath.IsAbs(path) && filepath.Clean(path) == path && path != string(filepath.Separator)
}
