//go:build linux && arm && fpgadev

package main

import (
	"context"
	"os/exec"

	"github.com/DeanoC/FogCast-POC/internal/fpgadev"
)

func (supervisorResetAdapter) Reset(ctx context.Context) error {
	return fpgadev.ResetSupervisor(ctx)
}

func (supervisorRebootRequester) Request(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := exec.CommandContext(ctx, "/sbin/reboot").Run(); err != nil {
		return err
	}
	return ctx.Err()
}
