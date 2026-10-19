package applianceupdate_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	release "github.com/DeanoC/FogCast/appliance"
	"github.com/DeanoC/FogCast/appliance/store"
	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/appliancesplash"
	"github.com/DeanoC/FogCast/internal/applianceupdate"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"golang.org/x/sys/unix"
)

type idleControl struct{}

func (idleControl) Protocol2Status(context.Context) (misterruntime.Protocol2Response, error) {
	return misterruntime.Protocol2Response{Protocol: 2, OK: true, State: "idle", Execution: "none"}, nil
}
func (idleControl) Protocol2Stop(context.Context) (misterruntime.Protocol2Response, error) {
	return misterruntime.Protocol2Response{Protocol: 2, OK: true, State: "idle", Execution: "none"}, nil
}
func (idleControl) Protocol2LoadDevelopmentRBF(context.Context, string) (misterruntime.Protocol2Response, error) {
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
	c := agent.New(misterruntime.NewRuntime(idleControl{}, "", time.Millisecond, time.Second), time.Second, time.Second)
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

func (c *saveFailureControl) Protocol2Status(ctx context.Context) (misterruntime.Protocol2Response, error) {
	response, e := c.idleControl.Protocol2Status(ctx)
	if c.failed.Load() {
		response.Error = &misterruntime.Protocol2Error{Code: "save_failed", Message: "save failed"}
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
	c := agent.New(misterruntime.NewRuntime(control, "", time.Millisecond, time.Second), time.Second, time.Second)
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

func writeImageSplash(t *testing.T, idle, record string, payload []byte) string {
	t.Helper()
	sum := sha256.Sum256(payload)
	encoded := hex.EncodeToString(sum[:])
	if err := os.WriteFile(idle, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("format=1\nidle_sha256=%s\nidle_size=%d\n", encoded, len(payload))
	if err := os.WriteFile(record, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return encoded
}

func releaseRoot(t *testing.T, s *appliance.Store, hash string) string {
	t.Helper()
	return filepath.Dir(filepath.Dir(mustImage(t, s, hash)))
}

func TestUpdateConfirmAndRollbackRefreshFATSplash(t *testing.T) {
	s, factory, next, c := fixture(t)
	dir := t.TempDir()
	idle := filepath.Join(dir, "idle.rbf")
	record := filepath.Join(dir, "build-inputs")
	fat := filepath.Join(dir, "idle.fat")
	if err := os.WriteFile(fat, []byte("stale-factory-splash"), 0o644); err != nil {
		t.Fatal(err)
	}
	splash := appliancesplash.New(appliancesplash.Paths{
		Idle: idle, Record: record, FAT: fat, ReleaseRoot: releaseRoot(t, s, factory.ImageSHA256),
	})
	sumNext := writeImageSplash(t, idle, record, []byte("splash-of-candidate-image"))
	if err := s.Activate(next.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginBoot("boot-next"); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(fat); err != nil || string(got) != "stale-factory-splash" {
		t.Fatalf("activate changed FAT: %q %v", got, err)
	}
	rbf, _, err := appliance.SplashPaths(splash.ReleaseRoot, next.ImageSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Lstat(rbf); !os.IsNotExist(err) {
		t.Fatalf("splash staged before confirm: %v", err)
	}
	svc := applianceupdate.NewWithSplash(s, c, applianceupdate.BootIdentity{BootID: "boot-next", ImageSHA256: next.ImageSHA256, Trial: true}, nil, nil, splash)
	if err := svc.Confirm(context.Background(), "boot-next", next.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	if got := shaOf(t, fat); got != sumNext {
		t.Fatalf("confirmed FAT sha %s want %s", got, sumNext)
	}
	if got := shaOf(t, rbf); got != sumNext {
		t.Fatalf("staged splash sha %s want %s", got, sumNext)
	}
	status, err := svc.Status(context.Background())
	if err != nil || status.Trial || status.FatIdleSHA256 != sumNext || status.IdleSHA256 != sumNext || status.Good != next.ImageSHA256 {
		t.Fatalf("status after confirm: %+v %v", status, err)
	}
	if err := s.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginBoot("boot-back"); err != nil {
		t.Fatal(err)
	}
	if got := shaOf(t, fat); got != sumNext {
		t.Fatalf("rollback activation changed FAT before confirm: %s", got)
	}
	sumFactory := writeImageSplash(t, idle, record, []byte("splash-of-factory-image"))
	restored := applianceupdate.NewWithSplash(s, c, applianceupdate.BootIdentity{BootID: "boot-back", ImageSHA256: factory.ImageSHA256, Trial: true}, nil, nil, splash)
	if err := restored.Confirm(context.Background(), "boot-back", factory.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	if got := shaOf(t, fat); got != sumFactory {
		t.Fatalf("rollback confirm FAT sha %s want %s", got, sumFactory)
	}
	back, _, err := appliance.SplashPaths(splash.ReleaseRoot, factory.ImageSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if got := shaOf(t, back); got != sumFactory {
		t.Fatalf("factory splash not staged: %s", got)
	}
	status, err = restored.Status(context.Background())
	if err != nil || status.Trial || status.FatIdleSHA256 != sumFactory || status.Good != factory.ImageSHA256 || status.Previous != next.ImageSHA256 {
		t.Fatalf("status after rollback confirm: %+v %v", status, err)
	}
}

func TestConfirmKeepsTrialUntilSplashPublishes(t *testing.T) {
	s, _, next, c := fixture(t)
	dir := t.TempDir()
	idle := filepath.Join(dir, "idle.rbf")
	record := filepath.Join(dir, "build-inputs")
	fat := filepath.Join(dir, "idle.fat")
	if err := os.Mkdir(fat, 0o755); err != nil {
		t.Fatal(err)
	}
	writeImageSplash(t, idle, record, []byte("splash-that-cannot-publish"))
	splash := appliancesplash.New(appliancesplash.Paths{
		Idle: idle, Record: record, FAT: fat, ReleaseRoot: releaseRoot(t, s, next.ImageSHA256),
	})
	if err := s.Activate(next.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginBoot("boot-next"); err != nil {
		t.Fatal(err)
	}
	svc := applianceupdate.NewWithSplash(s, c, applianceupdate.BootIdentity{BootID: "boot-next", ImageSHA256: next.ImageSHA256, Trial: true}, nil, nil, splash)
	if err := svc.Confirm(context.Background(), "boot-next", next.ImageSHA256); err == nil {
		t.Fatal("confirm succeeded while FAT splash was a directory")
	}
	status, err := svc.Status(context.Background())
	if err != nil || !status.Trial || c.Health("").Ready {
		t.Fatalf("gate lifted without a splash: %+v %v", status, err)
	}
	if err := os.Remove(fat); err != nil {
		t.Fatal(err)
	}
	if err := svc.Confirm(context.Background(), "boot-next", next.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	status, err = svc.Status(context.Background())
	if err != nil || status.Trial || !c.Health("").Ready || status.FatIdleSHA256 == "" {
		t.Fatalf("retry did not publish splash: %+v %v", status, err)
	}
}

func TestConfirmRejectsSplashThatDiffersFromImageRecord(t *testing.T) {
	s, _, next, c := fixture(t)
	dir := t.TempDir()
	idle := filepath.Join(dir, "idle.rbf")
	record := filepath.Join(dir, "build-inputs")
	fat := filepath.Join(dir, "idle.fat")
	if err := os.WriteFile(fat, []byte("keep-me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(idle, []byte("not-the-record"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(record, []byte("format=1\nidle_sha256="+strings.Repeat("d", 64)+"\nidle_size=14\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	splash := appliancesplash.New(appliancesplash.Paths{
		Idle: idle, Record: record, FAT: fat, ReleaseRoot: releaseRoot(t, s, next.ImageSHA256),
	})
	if err := s.Activate(next.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginBoot("boot-next"); err != nil {
		t.Fatal(err)
	}
	svc := applianceupdate.NewWithSplash(s, c, applianceupdate.BootIdentity{BootID: "boot-next", ImageSHA256: next.ImageSHA256, Trial: true}, nil, nil, splash)
	if err := svc.Confirm(context.Background(), "boot-next", next.ImageSHA256); err == nil {
		t.Fatal("mismatched splash confirmed")
	}
	got, err := os.ReadFile(fat)
	if err != nil || string(got) != "keep-me" {
		t.Fatalf("FAT changed: %q %v", got, err)
	}
	st, err := s.Status()
	if err != nil || st.TrialImage != next.ImageSHA256 || st.Good == next.ImageSHA256 {
		t.Fatalf("selection changed: %+v %v", st, err)
	}
}

func mustImage(t *testing.T, s *appliance.Store, hash string) string {
	t.Helper()
	path, err := s.ImagePath(hash)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func shaOf(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
