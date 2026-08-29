//go:build linux && fpgadev

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/internal/fpgadev"
)

func TestCLITaggedFaultCommandsUseExactPositiveFraming(t *testing.T) {
	runID := strings.Repeat("a", 32)
	runner := &fakeFaultCommandRunner{}
	commands := []struct {
		name string
		out  string
	}{
		{name: "fault-arm"},
		{name: "inspect", out: "FOGCAST_FPGA_DEV_INSPECT run_id=" + runID + " session=" + strings.Repeat("b", 32) + " generation=7 phase=load_attempted pid=123 start_time=456 executable_sha256=" + strings.Repeat("c", 64) + "\n"},
		{name: "fault-kill", out: "FOGCAST_FPGA_DEV_FAULT_KILLED run_id=" + runID + "\n"},
		{name: "recovery-reboot"},
	}
	for _, command := range commands {
		t.Run(command.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			got := runFaultCommandWithPrivilege([]string{command.name, "--run-id", runID}, &stdout, &stderr, runner, func() bool { return true })
			if got != exitOK || stdout.String() != command.out || stderr.Len() != 0 {
				t.Fatalf("run() = exit:%d stdout:%q stderr:%q, want exit 0 stdout:%q", got, stdout.String(), stderr.String(), command.out)
			}
		})
	}
	if runner.armCalls != 1 || runner.inspectCalls != 1 || runner.killCalls != 1 || runner.rebootCalls != 1 {
		t.Fatalf("fault calls = arm:%d inspect:%d kill:%d reboot:%d, want one each", runner.armCalls, runner.inspectCalls, runner.killCalls, runner.rebootCalls)
	}
}

func TestCLITaggedRejectsLegacyFaultInspectAlias(t *testing.T) {
	if faultCommandName("fault-inspect") {
		t.Fatal("legacy fault-inspect alias must not be part of the command grammar")
	}
}

type fakeFaultCommandRunner struct {
	fakeCommandRunner
	armCalls     int
	inspectCalls int
	killCalls    int
	rebootCalls  int
}

func (r *fakeFaultCommandRunner) FaultArm(context.Context, string) error {
	r.armCalls++
	return nil
}

func (r *fakeFaultCommandRunner) FaultInspect(context.Context, string) (fpgadev.Inspection, error) {
	r.inspectCalls++
	return fpgadev.Inspection{
		Session: strings.Repeat("b", 32), Generation: 7, Phase: "load_attempted",
		PID: 123, StartTime: 456, ExecutableSHA256: strings.Repeat("c", 64),
	}, nil
}

func (r *fakeFaultCommandRunner) FaultKill(context.Context, string) error {
	r.killCalls++
	return nil
}

func (r *fakeFaultCommandRunner) RecoveryReboot(context.Context, string) error {
	r.rebootCalls++
	return nil
}
