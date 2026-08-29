//go:build linux && fpgadev

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/DeanoC/FogCast-POC/internal/fpgadev"
)

const (
	supervisorExitOK      = 0
	supervisorExitFailure = 1
	supervisorExitUsage   = 2
)

// The command grammar and fail-stop orchestration are platform-neutral within
// the tagged Linux build. Target-only reset, child-start, launch-source, and
// reboot adapters are composed exclusively by the ARM dependency file.
func main() { os.Exit(runSupervisor(os.Args[1:], os.Stdout, os.Stderr, productionSupervisor)) }

// runSupervisor keeps the target grammar small: only the recovery trampoline
// may invoke the supervisor and it must pass exactly one inherited lock fd.
func runSupervisor(args []string, stdout, stderr io.Writer, runner func(context.Context, fpgadev.Dependencies) error) int {
	if len(args) != 2 || args[0] != "--inherited-fd" {
		_, _ = io.WriteString(stderr, "usage: fogcast-dev-supervisor --inherited-fd FD\n")
		return supervisorExitUsage
	}
	fd, err := strconv.Atoi(args[1])
	if err != nil || fd < 0 || fd > 1<<20 {
		_, _ = io.WriteString(stderr, "usage: fogcast-dev-supervisor --inherited-fd FD\n")
		return supervisorExitUsage
	}
	if runner == nil {
		return supervisorExitFailure
	}
	deps := productionSupervisorDependencies(fd)
	if err := runner(context.Background(), deps); err != nil {
		_, _ = fmt.Fprintln(stderr, "FOGCAST_FPGA_DEV_SUPERVISOR code=unavailable")
		return supervisorExitFailure
	}
	_, _ = io.WriteString(stdout, "FOGCAST_FPGA_DEV_SUPERVISOR code=ok\n")
	return supervisorExitOK
}

func productionSupervisor(ctx context.Context, deps fpgadev.Dependencies) error {
	return fpgadev.NewSupervisor().Run(ctx, deps)
}

var _ = productionSupervisor
