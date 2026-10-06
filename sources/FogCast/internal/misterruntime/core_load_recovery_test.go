package misterruntime_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

type initialRecoveryControl struct {
	*initialTargetControl
	programmedPath string
	wrongROM       bool
	failStop       bool
	stopCalls      int
}

func (c *initialRecoveryControl) retainedReply(r misterruntime.Protocol2Response, err error, path string) (misterruntime.Protocol2Response, error) {
	if err != nil {
		return r, err
	}
	c.programmedPath = path
	r.OK = false
	r.State = "reboot_required"
	r.Error = &misterruntime.Protocol2Error{Code: "idle_failed", Phase: "recovery", Message: "initial Start failed and disk capture failed"}
	if c.wrongROM {
		link := *r.ActivePackage.ROMLink
		link.SourceSHA256 = strings.Repeat("d", 64)
		r.ActivePackage.ROMLink = &link
	}
	c.status2 = &r
	return r, nil
}
func (c *initialRecoveryControl) LoadROMLinkedCoreWithInitialMedia(ctx context.Context, path, id, root, expansionPath, payload string, composition *expansion.Composition, programmed string, link corepackage.ROMLinkIdentity, initial *misterruntime.InitialMediaRequest) (misterruntime.Protocol2Response, error) {
	r, err := c.initialTargetControl.LoadROMLinkedCoreWithInitialMedia(ctx, path, id, root, expansionPath, payload, composition, programmed, link, initial)
	return c.retainedReply(r, err, programmed)
}
func (c *initialRecoveryControl) LoadROMPartsComposedCoreWithInitialMedia(ctx context.Context, path, id string, paths []misterruntime.PartPath, payload string, composition expansion.PartsComposition, programmed string, link corepackage.ROMLinkIdentity, initial *misterruntime.InitialMediaRequest) (misterruntime.Protocol2Response, error) {
	r, err := c.initialTargetControl.LoadROMPartsComposedCoreWithInitialMedia(ctx, path, id, paths, payload, composition, programmed, link, initial)
	return c.retainedReply(r, err, programmed)
}
func (c *initialRecoveryControl) Protocol2Stop(ctx context.Context) (misterruntime.Protocol2Response, error) {
	c.stopCalls++
	if c.failStop {
		return *c.status2, nil
	}
	r, err := c.packageControl.Protocol2Stop(ctx)
	if err == nil {
		c.status2 = &r
	}
	return r, err
}
func TestExplicitInitialDiskRecoveryRetainsSourcesUntilConfirmedStop(t *testing.T) {
	for _, tc := range []struct {
		name, kind string
		wrongROM   bool
	}{
		{"plain-matching-owner", "plain", false}, {"parts-matching-owner", "video", false}, {"different-ROM-owner-is-ambiguous", "plain", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := initialTargetInput(t, tc.kind)
			transport, err := corepackage.PrepareROMInput(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			inspection, err := corepackage.Stage(context.Background(), t.TempDir(), int64(len(in.Package)), bytes.NewReader(in.Package))
			if err != nil {
				t.Fatal(err)
			}
			defer inspection.Cleanup()
			c := &initialRecoveryControl{initialTargetControl: &initialTargetControl{stVideoTargetControl: &stVideoTargetControl{romTargetControl: newROMTargetControl(t, 1)}, kind: tc.kind}, wrongROM: tc.wrongROM, failStop: true}
			root := t.TempDir()
			r := misterruntime.NewRuntime(c, "", time.Millisecond, 20*time.Millisecond, misterruntime.WithCorePackageRoot(root))
			t.Cleanup(func() { c.failStop = false; _, _ = r.Stop(context.Background()) })
			var activation misterruntime.CoreActivation
			var attempted bool
			var failure *protocol.APIError
			if tc.kind == "plain" {
				activation, attempted, failure = r.LoadLibraryCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(transport.Data)), bytes.NewReader(transport.Data), inspection.PackageID)
			} else {
				activation, attempted, failure = r.LoadComposedCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(transport.Data)), bytes.NewReader(transport.Data), inspection.PackageID)
			}
			if !attempted || failure == nil || failure.Phase != "recovery" || activation.PackageID != "" || c.initialCalls != 1 || c.loadCalls != 1 {
				t.Fatal("retained failed load succeeded or replayed", attempted, failure, activation, c.initialCalls, c.loadCalls)
			}
			assertSources := func() {
				t.Helper()
				for _, path := range []string{c.activePath, c.programmedPath, c.initial.Path} {
					if _, err := os.Stat(path); err != nil {
						t.Fatal("explicit recovery removed retained source", path, err)
					}
				}
			}
			assertSources()
			adopted, err := corepackage.Adopt(root)
			if err != nil || len(adopted) != 1 || adopted[0].InitialMedia == nil || adopted[0].ROMLink == nil {
				t.Fatal("retained recovery sources cannot be revalidated", err)
			}
			restarted := misterruntime.NewRuntime(c, "", time.Millisecond, 20*time.Millisecond, misterruntime.WithCorePackageRoot(root))
			status := restarted.Reconcile(context.Background())
			if tc.wrongROM {
				if status.CorePackage != nil || status.State != protocol.StateFailed {
					t.Fatal("mismatched ROM adopted as retained owner", status)
				}
			} else {
				if status.State != protocol.StateFailed || status.Recovery != protocol.RecoveryRebootRequired || status.CorePackage == nil || status.CorePackage.PackageID != inspection.PackageID || status.CorePackage.ROMLink == nil || status.CorePackage.MediaUnits[0].Persistence.GameID != in.InitialMedia.GameID {
					t.Fatal("restart lost exact retained recovery owner", status)
				}
				_, _, _ = restarted.StopOwnedWithRecovery(context.Background(), context.Background())
				assertSources()
			}
			_, _, _ = r.StopOwnedWithRecovery(context.Background(), context.Background())
			assertSources()
			if c.initialCalls != 1 || c.loadCalls != 1 {
				t.Fatal("recovery or Stop replayed load")
			}
			c.failStop = false
			if tc.wrongROM {
				_, failure = r.Stop(context.Background())
			} else {
				_, failure = restarted.Stop(context.Background())
			}
			if failure != nil {
				t.Fatal("confirmed idle Stop failed", failure)
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 {
				t.Fatal("confirmed Stop retained staged sources", entries, err)
			}
		})
	}
}
