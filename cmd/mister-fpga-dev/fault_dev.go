//go:build linux && fpgadev

package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/DeanoC/FogCast-POC/internal/fpgadev"
)

type faultRunner interface {
	FaultArm(context.Context, string) error
	FaultInspect(context.Context, string) (fpgadev.Inspection, error)
	FaultKill(context.Context, string) error
	RecoveryReboot(context.Context, string) error
}

func faultCommandName(name string) bool {
	switch name {
	case "fault-arm", "inspect", "fault-kill", "recovery-reboot":
		return true
	default:
		return false
	}
}

func runFaultCommand(args []string, stdout, stderr io.Writer, runner commandRunner) int {
	return runFaultCommandWithPrivilege(args, stdout, stderr, runner, func() bool { return os.Geteuid() == 0 })
}

// runFaultCommandWithPrivilege keeps the command grammar testable without
// granting fixture tests a real root-only side effect. Production calls the
// wrapper above, which supplies the actual effective-UID check.
func runFaultCommandWithPrivilege(args []string, stdout, stderr io.Writer, runner commandRunner, privileged func() bool) int {
	if privileged == nil || !privileged() {
		return usage(stderr)
	}
	if len(args) != 3 || args[1] != "--run-id" || !validRunID(args[2]) {
		return usage(stderr)
	}
	faults, ok := runner.(faultRunner)
	if !ok {
		return usage(stderr)
	}
	ctx := context.Background()
	var err error
	switch args[0] {
	case "fault-arm":
		err = faults.FaultArm(ctx, args[2])
	case "inspect":
		var inspection fpgadev.Inspection
		inspection, err = faults.FaultInspect(ctx, args[2])
		if err == nil {
			_, _ = io.WriteString(stdout, inspectLine(args[2], inspection))
		}
	case "fault-kill":
		err = faults.FaultKill(ctx, args[2])
		if err == nil {
			_, _ = fmt.Fprintf(stdout, "FOGCAST_FPGA_DEV_FAULT_KILLED run_id=%s\n", args[2])
		}
	case "recovery-reboot":
		err = faults.RecoveryReboot(ctx, args[2])
	default:
		return usage(stderr)
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "FOGCAST_FPGA_DEV_FAULT code=unavailable\n")
		return exitFailure
	}
	return exitOK
}

func validRunID(runID string) bool {
	if len(runID) != 32 {
		return false
	}
	_, err := hex.DecodeString(runID)
	return err == nil && strings.ToLower(runID) == runID
}

func inspectLine(runID string, inspection fpgadev.Inspection) string {
	return fmt.Sprintf("FOGCAST_FPGA_DEV_INSPECT run_id=%s session=%s generation=%d phase=%s pid=%d start_time=%d executable_sha256=%s\n", runID, inspection.Session, inspection.Generation, inspection.Phase, inspection.PID, inspection.StartTime, inspection.ExecutableSHA256)
}
