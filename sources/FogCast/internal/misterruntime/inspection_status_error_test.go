package misterruntime_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
)

// The daemon retains the previous MENU failure in status after LoadIdle
// recovers it. Respond still includes that status error when inspection itself
// succeeds. The operation's OK and its inspected data remain authoritative.
type inspectionStatusControl struct {
	dataControl
	failInspection, failData, badIdentity bool
	retained                              *misterruntime.Protocol2Error
}

func (c *inspectionStatusControl) InspectCore(ctx context.Context, path, id string) (misterruntime.Protocol2Response, error) {
	r, err := c.packageControl.InspectCore(ctx, path, id)
	r.Error = c.retained
	if c.failInspection {
		r.OK, r.InspectedPackage = false, nil
	} else if c.badIdentity && r.InspectedPackage != nil {
		r.InspectedPackage.PackageID = strings.Repeat("d", 64)
	}
	return r, err
}

func (c *inspectionStatusControl) InspectCoreData(ctx context.Context, path, id, root string) (misterruntime.Protocol2Response, error) {
	r, err := c.dataControl.InspectCoreData(ctx, path, id, root)
	r.Error = c.retained
	if c.failData {
		r.OK, r.CoreData = false, nil
	}
	return r, err
}

func TestInspectionResultIsSeparateFromRetainedRuntimeError(t *testing.T) {
	for _, kind := range []string{"package", "data", "library launch", "failed package", "failed data", "wrong package"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			archive := canonicalCoreArchive(t)
			staged, err := corepackage.Stage(ctx, t.TempDir(), int64(len(archive)), bytes.NewReader(archive))
			if err != nil {
				t.Fatal(err)
			}
			defer staged.Cleanup()
			retained := &misterruntime.Protocol2Error{Code: "io_failed", Phase: "menu", Message: "previous MENU grant underflow"}
			control := &inspectionStatusControl{retained: retained, failInspection: kind == "failed package", failData: kind == "failed data", badIdentity: kind == "wrong package"}
			idle := misterruntime.Protocol2Response{Protocol: 2, OK: true, State: "idle", Execution: "none", Version: "test", Error: retained}
			control.status2 = &idle
			r := misterruntime.NewRuntime(control, "", 0, 0, misterruntime.WithCorePackageRoot(t.TempDir()))
			defer r.Stop(ctx)
			switch kind {
			case "package":
				inspection, failure := r.InspectCore(ctx, int64(len(archive)), bytes.NewReader(archive))
				if failure != nil || inspection.PackageID != staged.PackageID || !inspection.Compatible {
					t.Fatalf("inspection=%+v failure=%+v", inspection, failure)
				}
			case "data":
				data, failure := r.InspectCoreData(ctx, int64(len(archive)), bytes.NewReader(archive), staged.PackageID)
				if failure != nil || data.PackageID != staged.PackageID || data.Mode != "volatile" {
					t.Fatalf("data=%+v failure=%+v", data, failure)
				}
			default:
				activation, attempted, failure := r.LoadLibraryCoreOwned(ctx, ctx, ctx, int64(len(archive)), bytes.NewReader(archive), staged.PackageID)
				if kind == "library launch" {
					if failure != nil || !attempted || activation.PackageID != staged.PackageID || control.libraryCalls != 1 {
						t.Fatalf("activation=%+v attempted=%v failure=%+v loads=%d", activation, attempted, failure, control.libraryCalls)
					}
					return
				}
				if failure == nil || failure.Code != protocol.CodeMiSTerUnavailable || attempted || control.loadCalls != 0 {
					t.Fatalf("attempted=%v failure=%+v loads=%d", attempted, failure, control.loadCalls)
				}
			}
			status := r.Reconcile(ctx)
			if status.State != protocol.StateIdle || status.LastError == nil || control.loadCalls != 0 {
				t.Fatalf("inspection must preserve retained status: %+v loads=%d", status, control.loadCalls)
			}
		})
	}
}
