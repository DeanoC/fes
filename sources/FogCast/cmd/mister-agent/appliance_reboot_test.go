package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/rebootguard"
)

func TestGuardedReboot(t *testing.T) {
	oldArm, oldRun, oldCancel := armReboot, runReboot, cancelReboot
	t.Cleanup(func() { armReboot, runReboot, cancelReboot = oldArm, oldRun, oldCancel })
	for _, failed := range []bool{true, false} {
		t.Run(map[bool]string{true: "failure", false: "success"}[failed], func(t *testing.T) {
			handle := &rebootguard.Armed{}
			calls := 0
			armReboot = func(context.Context, rebootguard.Config) (rebootguard.Result, error) {
				return rebootguard.Result{Armed: handle, ActualTimeout: 180 * time.Second, Reason: "armed"}, nil
			}
			runReboot = func(context.Context) error {
				if failed {
					return errors.New("reboot failed")
				}
				return nil
			}
			cancelReboot = func(got *rebootguard.Armed) error {
				if got != handle {
					t.Fatal("wrong handle")
				}
				calls++
				return nil
			}
			err := guardedReboot(context.Background())
			if (err != nil) != failed || calls != map[bool]int{true: 1, false: 0}[failed] {
				t.Fatalf("err=%v cancels=%d", err, calls)
			}
		})
	}
}
