package applianceupdate_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	release "github.com/DeanoC/FogCast/appliance"
	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/appliance"
	"github.com/DeanoC/FogCast/internal/applianceupdate"
	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"golang.org/x/sys/unix"
)

type idleControl struct{}

func (idleControl) Status(context.Context) (misterruntime.Response, error) {
	return misterruntime.Response{Protocol: 1, OK: true, State: "idle", Execution: "none"}, nil
}
func (idleControl) Stop(context.Context) (misterruntime.Response, error) {
	return misterruntime.Response{Protocol: 1, OK: true, State: "idle", Execution: "none"}, nil
}
func (idleControl) Launch(context.Context, misterruntime.LaunchRequest) (misterruntime.Response, error) {
	panic("unexpected launch")
}
func (idleControl) LoadDevelopmentRBF(context.Context, string) (misterruntime.Response, error) {
	panic("unexpected development")
}

func fixture(t *testing.T) (*appliance.Store, release.Manifest, release.Manifest, *agent.Coordinator) {
	t.Helper()
	b := make([]byte, 4096)
	b[1080] = 0x53
	b[1081] = 0xef
	b[1120] = 0x40
	m := release.Manifest{Format: 1, Board: release.Board, BootABI: release.BootABI, Version: "test", KernelSHA256: strings.Repeat("a", 64), ImageSHA256: fmt.Sprintf("%x", sha256.Sum256(b)), ImageSize: int64(len(b)), FESRevision: strings.Repeat("b", 40), FogCastRevision: strings.Repeat("c", 40), RuntimeRevision: strings.Repeat("d", 40)}
	s, e := appliance.New(t.TempDir(), m)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Stage(context.Background(), m, m.ImageSize, bytes.NewReader(b)); e != nil {
		t.Fatal(e)
	}
	b[0] = 1
	n := m
	n.ImageSHA256 = fmt.Sprintf("%x", sha256.Sum256(b))
	if e = s.Stage(context.Background(), n, n.ImageSize, bytes.NewReader(b)); e != nil {
		t.Fatal(e)
	}
	c := agent.New(misterruntime.NewRuntime(idleControl{}, "", time.Millisecond, time.Second), core.DefaultRegistry(), time.Second, time.Second)
	return s, m, n, c
}

func TestTrialRequiresExactBootAndRawIdleConfirmation(t *testing.T) {
	s, m, n, c := fixture(t)
	if e := s.Activate(n.ImageSHA256); e != nil {
		t.Fatal(e)
	}
	if _, e := s.BeginBoot("boot-a"); e != nil {
		t.Fatal(e)
	}
	svc := applianceupdate.New(s, c, applianceupdate.BootIdentity{BootID: "boot-a", ImageSHA256: n.ImageSHA256, Trial: true}, nil, nil)
	if c.Health("").Ready {
		t.Fatal("trial admitted launch")
	}
	if e := svc.Confirm(context.Background(), "boot-a", m.ImageSHA256); e == nil {
		t.Fatal("wrong image confirmed")
	}
	if e := svc.Confirm(context.Background(), "old", n.ImageSHA256); e == nil {
		t.Fatal("old boot confirmed")
	}
	if e := svc.Confirm(context.Background(), "boot-a", n.ImageSHA256); e != nil {
		t.Fatal(e)
	}
	if !c.Health("").Ready {
		t.Fatal("confirmation did not lift gate")
	}
}

func TestActivationCancelsNormalAdmissionAndRetainsTransition(t *testing.T) {
	s, m, n, c := fixture(t)
	rebooted := false
	svc := applianceupdate.New(s, c, applianceupdate.BootIdentity{BootID: "good", ImageSHA256: m.ImageSHA256}, func(context.Context) error { rebooted = true; return nil }, nil)
	op, done, e := svc.BeginOperation(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	go func() { <-op.Done(); done() }()
	finish, e := svc.Activate(context.Background(), n.ImageSHA256)
	if e != nil {
		t.Fatal(e)
	}
	if rebooted {
		t.Fatal("reboot occurred before response")
	}
	if _, _, e := svc.BeginOperation(context.Background()); e == nil {
		t.Fatal("normal operation admitted after activation")
	}
	if e = finish(context.Background()); e != nil {
		t.Fatal(e)
	}
	if !rebooted {
		t.Fatal("reboot not requested")
	}
	if release, e := c.BeginMaintenance(context.Background()); e == nil {
		release()
		t.Fatal("transition released before reboot")
	}
}

func TestCancelledActivationNeverReboots(t *testing.T) {
	s, m, n, c := fixture(t)
	rebooted := false
	svc := applianceupdate.New(s, c, applianceupdate.BootIdentity{BootID: "good", ImageSHA256: m.ImageSHA256}, func(context.Context) error { rebooted = true; return nil }, nil)
	finish, e := svc.Activate(context.Background(), n.ImageSHA256)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e = finish(ctx); e == nil {
		t.Fatal("cancel accepted")
	}
	if rebooted {
		t.Fatal("cancelled request rebooted")
	}
}

func TestActivationCancellationInterruptsStoreLockWait(t *testing.T) {
	s, m, n, c := fixture(t)
	svc := applianceupdate.New(s, c, applianceupdate.BootIdentity{BootID: "good", ImageSHA256: m.ImageSHA256}, func(context.Context) error { t.Error("canceled operation rebooted"); return nil }, nil)
	imagePath, e := s.ImagePath(m.ImageSHA256)
	if e != nil {
		t.Fatal(e)
	}
	lock, e := os.OpenFile(filepath.Join(filepath.Dir(filepath.Dir(imagePath)), ".lock"), os.O_RDWR, 0600)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Close()
	if e = unix.Flock(int(lock.Fd()), unix.LOCK_EX); e != nil {
		t.Fatal(e)
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		finish, e := svc.Activate(ctx, n.ImageSHA256)
		if e == nil {
			e = finish(ctx)
		}
		result <- e
	}()
	select {
	case e := <-result:
		if e == nil {
			t.Fatal("canceled activation accepted")
		}
	case <-time.After(time.Second):
		unix.Flock(int(lock.Fd()), unix.LOCK_UN)
		<-result
		t.Fatal("activation ignored cancellation while waiting for store")
	}
	if release, e := c.BeginMaintenance(context.Background()); e != nil {
		t.Fatalf("cancellation retained transition: %v", e)
	} else {
		release()
	}
}

func TestStatusReconcilesDurableConfirmationFromIndependentGuard(t *testing.T) {
	s, _, n, c := fixture(t)
	if e := s.Activate(n.ImageSHA256); e != nil {
		t.Fatal(e)
	}
	if _, e := s.BeginBoot("boot-a"); e != nil {
		t.Fatal(e)
	}
	svc := applianceupdate.New(s, c, applianceupdate.BootIdentity{BootID: "boot-a", ImageSHA256: n.ImageSHA256, Trial: true}, nil, nil)
	// Represents a confirmation whose durable outcome is recovered outside the
	// service after its initial write returned an indeterminate sync error.
	if e := s.Confirm("boot-a", n.ImageSHA256); e != nil {
		t.Fatal(e)
	}
	if c.Health("").Ready {
		t.Fatal("confirmation became ready without reconciliation")
	}
	status, e := svc.Status(context.Background())
	if e != nil || status.Trial || !status.RawIdleReady || !c.Health("").Ready {
		t.Fatalf("durable confirmation not reconciled: %+v %v", status, e)
	}
	ctx, done, e := svc.BeginOperation(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer done()
	if ctx.Err() != nil {
		t.Fatal(ctx.Err())
	}
}

type saveFailureControl struct {
	idleControl
	failed atomic.Bool
}

func (c *saveFailureControl) Status(ctx context.Context) (misterruntime.Response, error) {
	response, e := c.idleControl.Status(ctx)
	if c.failed.Load() {
		response.Error = &misterruntime.RemoteError{Code: "save_failed", Message: "save failed"}
	}
	return response, e
}

func TestRestoredConfirmationWaitsForRawNativeIdle(t *testing.T) {
	s, _, n, _ := fixture(t)
	if e := s.Activate(n.ImageSHA256); e != nil {
		t.Fatal(e)
	}
	if _, e := s.BeginBoot("boot-a"); e != nil {
		t.Fatal(e)
	}
	if e := s.Confirm("boot-a", n.ImageSHA256); e != nil {
		t.Fatal(e)
	}
	control := &saveFailureControl{}
	control.failed.Store(true)
	c := agent.New(misterruntime.NewRuntime(control, "", time.Millisecond, time.Second), core.DefaultRegistry(), time.Second, time.Second)
	svc := applianceupdate.New(s, c, applianceupdate.BootIdentity{BootID: "boot-a", ImageSHA256: n.ImageSHA256, Trial: true}, nil, nil)
	status, e := svc.Status(context.Background())
	if e != nil || !status.Trial || status.RawIdleReady || c.Health("").Ready {
		t.Fatalf("restored confirmation skipped raw idle: %+v %v", status, e)
	}
	control.failed.Store(false)
	status, e = svc.Status(context.Background())
	if e != nil || status.Trial || !status.RawIdleReady || !c.Health("").Ready {
		t.Fatalf("idle recovery did not reconcile confirmation: %+v %v", status, e)
	}
}
