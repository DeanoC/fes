package applianceupdate_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	"github.com/DeanoC/FogCast/internal/atomicwrite"
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

func writeTrialTicket(t *testing.T, root, bootID, image string, trial bool) {
	t.Helper()
	body := fmt.Sprintf("{\"boot_id\":%q,\"image_sha256\":%q,\"trial\":%t}\n", bootID, image, trial)
	if err := os.WriteFile(filepath.Join(root, "boot.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func releaseRoot(t *testing.T, s *appliance.Store, hash string) string {
	t.Helper()
	path, err := s.ImagePath(hash)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(filepath.Dir(path))
}

func TestConfirmClearsBootTicketTrial(t *testing.T) {
	s, m, n, c := fixture(t)
	if e := s.Activate(n.ImageSHA256); e != nil {
		t.Fatal(e)
	}
	if _, e := s.BeginBoot("boot-a"); e != nil {
		t.Fatal(e)
	}
	root := releaseRoot(t, s, n.ImageSHA256)
	idPath := filepath.Join(root, "boot_id")
	if e := os.WriteFile(idPath, []byte("boot-a\n"), 0o600); e != nil {
		t.Fatal(e)
	}
	writeTrialTicket(t, root, "boot-a", n.ImageSHA256, true)
	svc := applianceupdate.New(s, c, applianceupdate.BootIdentity{BootID: "boot-a", ImageSHA256: n.ImageSHA256, Trial: true}, nil, nil)
	if e := svc.Confirm(context.Background(), "boot-a", m.ImageSHA256); e == nil {
		t.Fatal("wrong image confirmed")
	}
	got, e := applianceupdate.ReadBootIdentity(filepath.Join(root, "boot.json"), idPath)
	if e != nil || !got.Trial || got.BootID != "boot-a" || got.ImageSHA256 != n.ImageSHA256 {
		t.Fatalf("rejected confirm rewrote ticket: %+v %v", got, e)
	}
	if e = svc.Confirm(context.Background(), "boot-a", n.ImageSHA256); e != nil {
		t.Fatal(e)
	}
	got, e = applianceupdate.ReadBootIdentity(filepath.Join(root, "boot.json"), idPath)
	if e != nil || got.Trial || got.BootID != "boot-a" || got.ImageSHA256 != n.ImageSHA256 {
		t.Fatalf("confirmed ticket = %+v %v", got, e)
	}
	status, e := svc.Status(context.Background())
	if e != nil || status.Trial || status.Good != n.ImageSHA256 {
		t.Fatalf("status after confirm: %+v %v", status, e)
	}
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
	root := releaseRoot(t, s, n.ImageSHA256)
	idPath := filepath.Join(root, "boot_id")
	if e := os.WriteFile(idPath, []byte("boot-a\n"), 0o600); e != nil {
		t.Fatal(e)
	}
	// An older confirm left the durable release confirmed while boot.json
	// still says trial. Reconciliation must not clear that ticket before raw idle.
	writeTrialTicket(t, root, "boot-a", n.ImageSHA256, true)
	control := &saveFailureControl{}
	control.failed.Store(true)
	c := agent.New(misterruntime.NewRuntime(control, "", time.Millisecond, time.Second), time.Second, time.Second)
	svc := applianceupdate.New(s, c, applianceupdate.BootIdentity{BootID: "boot-a", ImageSHA256: n.ImageSHA256, Trial: true}, nil, nil)
	status, e := svc.Status(context.Background())
	if e != nil || !status.Trial || status.RawIdleReady || c.Health("").Ready {
		t.Fatalf("restored confirmation skipped raw idle: %+v %v", status, e)
	}
	if got, e := applianceupdate.ReadBootIdentity(filepath.Join(root, "boot.json"), idPath); e != nil || !got.Trial {
		t.Fatalf("ticket cleared before raw idle: %+v %v", got, e)
	}
	control.failed.Store(false)
	status, e = svc.Status(context.Background())
	if e != nil || status.Trial || !status.RawIdleReady || !c.Health("").Ready {
		t.Fatalf("idle recovery did not reconcile confirmation: %+v %v", status, e)
	}
	if got, e := applianceupdate.ReadBootIdentity(filepath.Join(root, "boot.json"), idPath); e != nil || got.Trial || got.ImageSHA256 != n.ImageSHA256 || got.BootID != "boot-a" {
		t.Fatalf("reconciled ticket = %+v %v", got, e)
	}
}

func captureTicketLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

func ticketRewriteLogs(buf *bytes.Buffer) int {
	return strings.Count(buf.String(), `msg="appliance boot ticket rewrite failed after durable confirmation"`)
}

func blockBootJSON(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, "boot.json")
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
}

func requireAdmissionOpen(t *testing.T, svc *applianceupdate.Service, c *agent.Coordinator) {
	t.Helper()
	if !c.Health("").Ready {
		t.Fatal("admission stayed blocked")
	}
	ctx, done, err := svc.BeginOperation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil {
		done()
		t.Fatal(ctx.Err())
	}
	done()
}

func TestBootTicketRewriteFailureOpensAdmissionAndRetriesOnce(t *testing.T) {
	s, _, n, c := fixture(t)
	if err := s.Activate(n.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginBoot("boot-a"); err != nil {
		t.Fatal(err)
	}
	root := releaseRoot(t, s, n.ImageSHA256)
	idPath := filepath.Join(root, "boot_id")
	if err := os.WriteFile(idPath, []byte("boot-a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := applianceupdate.New(s, c, applianceupdate.BootIdentity{BootID: "boot-a", ImageSHA256: n.ImageSHA256, Trial: true}, nil, nil)
	if c.Health("").Ready {
		t.Fatal("unconfirmed trial admitted launch")
	}
	blockBootJSON(t, root)
	logs := captureTicketLogs(t)
	if err := svc.Confirm(context.Background(), "boot-a", n.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	if ticketRewriteLogs(logs) != 1 {
		t.Fatalf("confirm log count = %d\n%s", ticketRewriteLogs(logs), logs.String())
	}
	requireAdmissionOpen(t, svc, c)
	status, err := svc.Status(context.Background())
	if err != nil || status.Trial || !status.RawIdleReady || status.Good != n.ImageSHA256 {
		t.Fatalf("status after failed ticket rewrite: %+v %v", status, err)
	}
	if ticketRewriteLogs(logs) != 2 {
		t.Fatalf("retry log count = %d\n%s", ticketRewriteLogs(logs), logs.String())
	}
	requireAdmissionOpen(t, svc, c)
	if _, err = svc.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ticketRewriteLogs(logs) != 2 {
		t.Fatalf("status repeated the failing rewrite: %d\n%s", ticketRewriteLogs(logs), logs.String())
	}
	// The one retry is spent. A ticket that becomes writable stays until an
	// explicit confirm, which still repairs it.
	if err = os.RemoveAll(filepath.Join(root, "boot.json")); err != nil {
		t.Fatal(err)
	}
	writeTrialTicket(t, root, "boot-a", n.ImageSHA256, true)
	if _, err = svc.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := applianceupdate.ReadBootIdentity(filepath.Join(root, "boot.json"), idPath)
	if err != nil || !got.Trial || got.BootID != "boot-a" || got.ImageSHA256 != n.ImageSHA256 {
		t.Fatalf("reconciliation rewrote the ticket after its one retry: %+v %v", got, err)
	}
	if err = svc.Confirm(context.Background(), "boot-a", n.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	got, err = applianceupdate.ReadBootIdentity(filepath.Join(root, "boot.json"), idPath)
	if err != nil || got.Trial || got.BootID != "boot-a" || got.ImageSHA256 != n.ImageSHA256 {
		t.Fatalf("explicit confirm left the ticket: %+v %v", got, err)
	}
	requireAdmissionOpen(t, svc, c)
}

func TestBootTicketRewriteRetriesWhenTheTicketBecomesWritable(t *testing.T) {
	s, _, n, c := fixture(t)
	if err := s.Activate(n.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginBoot("boot-a"); err != nil {
		t.Fatal(err)
	}
	root := releaseRoot(t, s, n.ImageSHA256)
	idPath := filepath.Join(root, "boot_id")
	if err := os.WriteFile(idPath, []byte("boot-a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := applianceupdate.New(s, c, applianceupdate.BootIdentity{BootID: "boot-a", ImageSHA256: n.ImageSHA256, Trial: true}, nil, nil)
	blockBootJSON(t, root)
	logs := captureTicketLogs(t)
	if err := svc.Confirm(context.Background(), "boot-a", n.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	requireAdmissionOpen(t, svc, c)
	if err := os.RemoveAll(filepath.Join(root, "boot.json")); err != nil {
		t.Fatal(err)
	}
	writeTrialTicket(t, root, "boot-a", n.ImageSHA256, true)
	status, err := svc.Status(context.Background())
	if err != nil || status.Trial || !c.Health("").Ready {
		t.Fatalf("retry status: %+v %v ready=%v", status, err, c.Health("").Ready)
	}
	got, err := applianceupdate.ReadBootIdentity(filepath.Join(root, "boot.json"), idPath)
	if err != nil || got.Trial || got.BootID != "boot-a" || got.ImageSHA256 != n.ImageSHA256 {
		t.Fatalf("retried ticket = %+v %v", got, err)
	}
	if ticketRewriteLogs(logs) != 1 {
		t.Fatalf("successful retry logged a failure: %d\n%s", ticketRewriteLogs(logs), logs.String())
	}
	if _, err = svc.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err = applianceupdate.ReadBootIdentity(filepath.Join(root, "boot.json"), idPath)
	if err != nil || got.Trial {
		t.Fatalf("later status restored trial: %+v %v", got, err)
	}
}

func TestBootTicketRepairSurvivesStatusTimeout(t *testing.T) {
	s, _, n, c := fixture(t)
	if err := s.Activate(n.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginBoot("boot-a"); err != nil {
		t.Fatal(err)
	}
	root := releaseRoot(t, s, n.ImageSHA256)
	idPath := filepath.Join(root, "boot_id")
	if err := os.WriteFile(idPath, []byte("boot-a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := applianceupdate.New(s, c, applianceupdate.BootIdentity{BootID: "boot-a", ImageSHA256: n.ImageSHA256, Trial: true}, nil, nil)
	blockBootJSON(t, root)
	logs := captureTicketLogs(t)
	if err := svc.Confirm(context.Background(), "boot-a", n.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	if ticketRewriteLogs(logs) != 1 {
		t.Fatalf("confirm log count = %d\n%s", ticketRewriteLogs(logs), logs.String())
	}
	requireAdmissionOpen(t, svc, c)
	// The failed rewrite scheduled one repair. Make the ticket writable and
	// hold the store lock so status times out before the rewrite.
	if err := os.RemoveAll(filepath.Join(root, "boot.json")); err != nil {
		t.Fatal(err)
	}
	writeTrialTicket(t, root, "boot-a", n.ImageSHA256, true)
	lock, err := os.OpenFile(filepath.Join(root, ".lock"), os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err = svc.Status(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timed-out status = %v", err)
	}
	if ticketRewriteLogs(logs) != 1 {
		t.Fatalf("timeout consumed the repair: %d\n%s", ticketRewriteLogs(logs), logs.String())
	}
	got, err := applianceupdate.ReadBootIdentity(filepath.Join(root, "boot.json"), idPath)
	if err != nil || !got.Trial || got.BootID != "boot-a" || got.ImageSHA256 != n.ImageSHA256 {
		t.Fatalf("timeout rewrote ticket: %+v %v", got, err)
	}
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	requireAdmissionOpen(t, svc, c)
	status, err := svc.Status(context.Background())
	if err != nil || status.Trial || !status.RawIdleReady || !c.Health("").Ready {
		t.Fatalf("status after timeout: %+v %v ready=%v", status, err, c.Health("").Ready)
	}
	got, err = applianceupdate.ReadBootIdentity(filepath.Join(root, "boot.json"), idPath)
	if err != nil || got.Trial || got.BootID != "boot-a" || got.ImageSHA256 != n.ImageSHA256 {
		t.Fatalf("repair after timeout = %+v %v", got, err)
	}
	if ticketRewriteLogs(logs) != 1 {
		t.Fatalf("successful repair logged a failure: %d\n%s", ticketRewriteLogs(logs), logs.String())
	}
	// The rewrite was attempted, so the one retry is spent.
	writeTrialTicket(t, root, "boot-a", n.ImageSHA256, true)
	if _, err = svc.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err = applianceupdate.ReadBootIdentity(filepath.Join(root, "boot.json"), idPath)
	if err != nil || !got.Trial || got.BootID != "boot-a" || got.ImageSHA256 != n.ImageSHA256 {
		t.Fatalf("later status retried again: %+v %v", got, err)
	}
}

func TestStartupReconcileOpensAdmissionWhenBootTicketRewriteFails(t *testing.T) {
	s, _, n, c := fixture(t)
	if err := s.Activate(n.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginBoot("boot-a"); err != nil {
		t.Fatal(err)
	}
	root := releaseRoot(t, s, n.ImageSHA256)
	blockBootJSON(t, root)
	if err := s.Confirm("boot-a", n.ImageSHA256); !errors.Is(err, appliance.ErrBootTicketRewrite) {
		t.Fatalf("store confirm = %v", err)
	}
	ok, err := s.ConfirmedContext(context.Background(), "boot-a", n.ImageSHA256)
	if !ok || err != nil {
		t.Fatalf("durable confirm = %v %v", ok, err)
	}
	logs := captureTicketLogs(t)
	svc := applianceupdate.New(s, c, applianceupdate.BootIdentity{BootID: "boot-a", ImageSHA256: n.ImageSHA256, Trial: true}, nil, nil)
	if ticketRewriteLogs(logs) != 1 {
		t.Fatalf("startup log count = %d\n%s", ticketRewriteLogs(logs), logs.String())
	}
	requireAdmissionOpen(t, svc, c)
	status, err := svc.Status(context.Background())
	if err != nil || status.Trial {
		t.Fatalf("startup status: %+v %v", status, err)
	}
	if ticketRewriteLogs(logs) != 2 {
		t.Fatalf("startup retry log count = %d\n%s", ticketRewriteLogs(logs), logs.String())
	}
	if _, err = svc.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ticketRewriteLogs(logs) != 2 {
		t.Fatalf("startup reconciliation looped: %d\n%s", ticketRewriteLogs(logs), logs.String())
	}
	requireAdmissionOpen(t, svc, c)
}

func TestConfirmFailureBeforeDurableRecordKeepsAdmissionBlocked(t *testing.T) {
	s, _, n, c := fixture(t)
	if err := s.Activate(n.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginBoot("boot-a"); err != nil {
		t.Fatal(err)
	}
	root := releaseRoot(t, s, n.ImageSHA256)
	svc := applianceupdate.New(s, c, applianceupdate.BootIdentity{BootID: "boot-a", ImageSHA256: n.ImageSHA256, Trial: true}, nil, nil)
	image, err := s.ImagePath(n.ImageSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(image); err != nil {
		t.Fatal(err)
	}
	logs := captureTicketLogs(t)
	if err = svc.Confirm(context.Background(), "boot-a", n.ImageSHA256); err == nil || errors.Is(err, appliance.ErrBootTicketRewrite) {
		t.Fatalf("missing image confirm = %v", err)
	}
	if c.Health("").Ready {
		t.Fatal("admission opened without a durable confirm")
	}
	if _, _, err = svc.BeginOperation(context.Background()); !errors.Is(err, applianceupdate.ErrBlocked) {
		t.Fatalf("begin = %v", err)
	}
	if ticketRewriteLogs(logs) != 0 {
		t.Fatalf("pre-durable failure logged a ticket rewrite: %s", logs.String())
	}
	if _, err = os.Stat(filepath.Join(root, "boot.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pre-durable confirm created a ticket: %v", err)
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
	root := releaseRoot(t, s, factory.ImageSHA256)
	idPath := filepath.Join(root, "boot_id")
	sumNext := writeImageSplash(t, idle, record, []byte("splash-of-candidate-image"))
	if err := s.Activate(next.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginBoot("boot-next"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(idPath, []byte("boot-next\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeTrialTicket(t, root, "boot-next", next.ImageSHA256, true)
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
	publishChecked := false
	splash.SetPublishWriteForTest(func(path string, data []byte, perm os.FileMode) error {
		state, statusErr := s.Status()
		if statusErr != nil {
			return statusErr
		}
		if state.Good != next.ImageSHA256 || state.TrialImage != "" || state.ConfirmedImage != "" {
			return fmt.Errorf("FAT publish started before durable known-good record: %+v", state)
		}
		publishChecked = true
		return atomicwrite.WriteFile(path, data, perm)
	})
	svc := applianceupdate.NewWithSplash(s, c, applianceupdate.BootIdentity{BootID: "boot-next", ImageSHA256: next.ImageSHA256, Trial: true}, nil, nil, splash)
	if err := svc.Confirm(context.Background(), "boot-next", next.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	if !publishChecked {
		t.Fatal("FAT publish did not observe the durable known-good record")
	}
	confirmedTicket, err := applianceupdate.ReadBootIdentity(filepath.Join(root, "boot.json"), idPath)
	if err != nil || confirmedTicket.Trial || confirmedTicket.BootID != "boot-next" || confirmedTicket.ImageSHA256 != next.ImageSHA256 {
		t.Fatalf("confirmed splash left trial ticket: %+v %v", confirmedTicket, err)
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
	if err := os.WriteFile(idPath, []byte("boot-back\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeTrialTicket(t, root, "boot-back", factory.ImageSHA256, true)
	if got := shaOf(t, fat); got != sumNext {
		t.Fatalf("rollback activation changed FAT before confirm: %s", got)
	}
	sumFactory := writeImageSplash(t, idle, record, []byte("splash-of-factory-image"))
	splash.SetPublishWriteForTest(func(path string, data []byte, perm os.FileMode) error {
		state, statusErr := s.Status()
		if statusErr != nil {
			return statusErr
		}
		if state.Good != factory.ImageSHA256 || state.TrialImage != "" || state.ConfirmedImage != "" {
			return fmt.Errorf("rollback FAT publish started before durable known-good record: %+v", state)
		}
		return atomicwrite.WriteFile(path, data, perm)
	})
	restored := applianceupdate.NewWithSplash(s, c, applianceupdate.BootIdentity{BootID: "boot-back", ImageSHA256: factory.ImageSHA256, Trial: true}, nil, nil, splash)
	if err := restored.Confirm(context.Background(), "boot-back", factory.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	confirmedTicket, err = applianceupdate.ReadBootIdentity(filepath.Join(root, "boot.json"), idPath)
	if err != nil || confirmedTicket.Trial || confirmedTicket.BootID != "boot-back" || confirmedTicket.ImageSHA256 != factory.ImageSHA256 {
		t.Fatalf("confirmed rollback splash left trial ticket: %+v %v", confirmedTicket, err)
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
	ok, err := s.ConfirmedContext(context.Background(), "boot-next", next.ImageSHA256)
	if ok || err != nil {
		t.Fatalf("watchdog disarmed before splash publication: %v %v", ok, err)
	}
	st, err := s.Status()
	if err != nil || st.Good != next.ImageSHA256 || st.ConfirmedImage != "" || st.TrialImage != "" {
		t.Fatalf("failed publish did not leave an accepted trial: %+v %v", st, err)
	}
	status, err := svc.Status(context.Background())
	if err != nil || !status.Trial || status.SplashWarning != "" || c.Health("").Ready {
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
	ok, err = s.ConfirmedContext(context.Background(), "boot-next", next.ImageSHA256)
	if !ok || err != nil {
		t.Fatalf("watchdog still armed after splash publication: %v %v", ok, err)
	}
}

func TestRestartPublishesSplashBeforeWatchdogConfirmation(t *testing.T) {
	s, _, next, c := fixture(t)
	dir := t.TempDir()
	idle := filepath.Join(dir, "idle.rbf")
	record := filepath.Join(dir, "build-inputs")
	fat := filepath.Join(dir, "idle.fat")
	if err := os.WriteFile(fat, []byte("previous-splash"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := releaseRoot(t, s, next.ImageSHA256)
	splash := appliancesplash.New(appliancesplash.Paths{Idle: idle, Record: record, FAT: fat, ReleaseRoot: root})
	sum := writeImageSplash(t, idle, record, []byte("splash-after-process-death"))
	if err := s.Activate(next.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginBoot("boot-next"); err != nil {
		t.Fatal(err)
	}
	if err := s.AcceptTrialContext(context.Background(), "boot-next", next.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	ok, err := s.ConfirmedContext(context.Background(), "boot-next", next.ImageSHA256)
	if ok || err != nil {
		t.Fatalf("watchdog confirmed before the agent restarted: %v %v", ok, err)
	}
	svc := applianceupdate.NewWithSplash(s, c, applianceupdate.BootIdentity{BootID: "boot-next", ImageSHA256: next.ImageSHA256, Trial: true}, nil, nil, splash)
	if got := shaOf(t, fat); got != sum {
		t.Fatalf("restart left FAT at %s", got)
	}
	ok, err = s.ConfirmedContext(context.Background(), "boot-next", next.ImageSHA256)
	if !ok || err != nil {
		t.Fatalf("restart did not record splash publication: %v %v", ok, err)
	}
	status, err := svc.Status(context.Background())
	if err != nil || status.Trial || !c.Health("").Ready || status.FatIdleSHA256 != sum {
		t.Fatalf("restart gate: %+v %v", status, err)
	}
}

func TestNonTrialBootFinishesInterruptedSplash(t *testing.T) {
	s, _, next, c := fixture(t)
	dir := t.TempDir()
	idle := filepath.Join(dir, "idle.rbf")
	record := filepath.Join(dir, "build-inputs")
	fat := filepath.Join(dir, "idle.fat")
	if err := os.WriteFile(fat, []byte("previous-splash"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := releaseRoot(t, s, next.ImageSHA256)
	splash := appliancesplash.New(appliancesplash.Paths{Idle: idle, Record: record, FAT: fat, ReleaseRoot: root})
	sum := writeImageSplash(t, idle, record, []byte("splash-of-accepted-image"))
	if err := s.Activate(next.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginBoot("boot-next"); err != nil {
		t.Fatal(err)
	}
	if err := s.AcceptTrialContext(context.Background(), "boot-next", next.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	rbf, digest, err := appliance.SplashPaths(root, next.ImageSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(rbf); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err = os.Remove(digest); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if _, err = s.BeginBoot("boot-stable"); err != nil {
		t.Fatal(err)
	}
	stable := applianceupdate.NewWithSplash(s, c, applianceupdate.BootIdentity{BootID: "boot-stable", ImageSHA256: next.ImageSHA256}, nil, nil, splash)
	if got := shaOf(t, fat); got != sum {
		t.Fatalf("non-trial boot left FAT at %s", got)
	}
	if got := shaOf(t, rbf); got != sum {
		t.Fatalf("running image did not restage the splash: %s", got)
	}
	status, err := stable.Status(context.Background())
	if err != nil || status.Trial || status.FatIdleSHA256 != sum || status.IdleSHA256 != sum {
		t.Fatalf("non-trial status: %+v %v", status, err)
	}
}

func TestNonTrialSplashRepairFailureStaysOnStatus(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	previous := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(previous) })

	s, factory, _, c := fixture(t)
	dir := t.TempDir()
	idle := filepath.Join(dir, "idle.rbf")
	record := filepath.Join(dir, "build-inputs")
	fat := filepath.Join(dir, "idle.fat")
	if err := os.WriteFile(fat, []byte("stale-readable-splash"), 0o644); err != nil {
		t.Fatal(err)
	}
	sumIdle := writeImageSplash(t, idle, record, []byte("splash-of-running-image"))
	sumFAT := shaOf(t, fat)
	if sumFAT == sumIdle {
		t.Fatal("fixture digests match")
	}
	sel, err := s.BeginBoot("boot-stable")
	if err != nil || sel.Trial || sel.Manifest.ImageSHA256 != factory.ImageSHA256 {
		t.Fatalf("factory boot: %+v %v", sel, err)
	}
	splash := appliancesplash.New(appliancesplash.Paths{
		Idle: idle, Record: record, FAT: fat, ReleaseRoot: releaseRoot(t, s, factory.ImageSHA256),
	})
	// A mode 0555 directory does not stop root creating Publish's temporary.
	// The writer fails closed for every uid. Each blocked attempt names a
	// different temporary, so the warning stays comparable only after that
	// name is dropped.
	var blockPublish atomic.Bool
	var publishAttempt atomic.Uint64
	blockPublish.Store(true)
	splash.SetPublishWriteForTest(func(path string, data []byte, perm os.FileMode) error {
		if blockPublish.Load() {
			name := fmt.Sprintf(".%s.%d.tmp", filepath.Base(path), publishAttempt.Add(1))
			return &os.PathError{Op: "open", Path: filepath.Join(filepath.Dir(path), name), Err: os.ErrPermission}
		}
		return atomicwrite.WriteFile(path, data, perm)
	})
	svc := applianceupdate.NewWithSplash(s, c, applianceupdate.BootIdentity{BootID: "boot-stable", ImageSHA256: factory.ImageSHA256}, nil, nil, splash)
	assertOpen := func() applianceupdate.Status {
		t.Helper()
		status, statusErr := svc.Status(context.Background())
		if statusErr != nil {
			t.Fatalf("status hid the repair failure: %v", statusErr)
		}
		if status.Trial || status.Good != factory.ImageSHA256 || status.ImageSHA256 != factory.ImageSHA256 || status.BootID != "boot-stable" {
			t.Fatalf("boot state: %+v", status)
		}
		if status.FatIdleSHA256 != sumFAT || status.IdleSHA256 != sumIdle || status.SplashWarning == "" {
			t.Fatalf("digests or warning: %+v", status)
		}
		if !strings.Contains(status.SplashWarning, "permission denied") {
			t.Fatalf("warning %q", status.SplashWarning)
		}
		body, marshalErr := json.Marshal(status)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		for _, needle := range []string{
			`"fat_idle_sha256":"` + sumFAT + `"`,
			`"idle_sha256":"` + sumIdle + `"`,
			`"splash_warning":`,
			`"trial":false`,
			`"good":"` + factory.ImageSHA256 + `"`,
		} {
			if !strings.Contains(string(body), needle) {
				t.Fatalf("status JSON missing %s: %s", needle, body)
			}
		}
		if got := shaOf(t, fat); got != sumFAT {
			t.Fatalf("failed repair changed FAT: %s", got)
		}
		return status
	}
	first := assertOpen()
	second := assertOpen()
	if first.SplashWarning != second.SplashWarning {
		t.Fatalf("warning changed across polls: %q %q", first.SplashWarning, second.SplashWarning)
	}
	if got := strings.Count(logs.String(), "splash repair failed"); got != 1 {
		t.Fatalf("repair failure logged %d times:\n%s", got, logs.String())
	}
	if !strings.Contains(logs.String(), factory.ImageSHA256) {
		t.Fatalf("log missing image: %s", logs.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelled, err := svc.Status(ctx)
	if !errors.Is(err, context.Canceled) || cancelled.SplashWarning != first.SplashWarning {
		t.Fatalf("cancelled status: %+v %v", cancelled, err)
	}
	blockPublish.Store(false)
	status, err := svc.Status(context.Background())
	if err != nil || status.SplashWarning != "" || status.Trial || status.FatIdleSHA256 != sumIdle || status.IdleSHA256 != sumIdle || status.Good != factory.ImageSHA256 {
		t.Fatalf("repair did not recover: %+v %v", status, err)
	}
	if got := shaOf(t, fat); got != sumIdle {
		t.Fatalf("recovered FAT %s", got)
	}
	if got := strings.Count(logs.String(), "splash repair succeeded"); got != 1 {
		t.Fatalf("recovery logged %d times:\n%s", got, logs.String())
	}
	if _, err = svc.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(logs.String(), "splash repair succeeded"); got != 1 {
		t.Fatalf("recovery logged again:\n%s", logs.String())
	}
	if got := strings.Count(logs.String(), "splash repair failed"); got != 1 {
		t.Fatalf("failure logged again:\n%s", logs.String())
	}
}

func TestFactoryFallbackRepublishesSplashFromTheRunningImage(t *testing.T) {
	s, factory, next, c := fixture(t)
	dir := t.TempDir()
	idle := filepath.Join(dir, "idle.rbf")
	record := filepath.Join(dir, "build-inputs")
	fat := filepath.Join(dir, "idle.fat")
	if err := os.WriteFile(fat, []byte("splash-before-trial"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := releaseRoot(t, s, factory.ImageSHA256)
	splash := appliancesplash.New(appliancesplash.Paths{Idle: idle, Record: record, FAT: fat, ReleaseRoot: root})
	sumNext := writeImageSplash(t, idle, record, []byte("splash-of-failed-image"))
	if err := s.Activate(next.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginBoot("boot-next"); err != nil {
		t.Fatal(err)
	}
	svc := applianceupdate.NewWithSplash(s, c, applianceupdate.BootIdentity{BootID: "boot-next", ImageSHA256: next.ImageSHA256, Trial: true}, nil, nil, splash)
	if err := svc.Confirm(context.Background(), "boot-next", next.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	if got := shaOf(t, fat); got != sumNext {
		t.Fatalf("confirmed splash %s", got)
	}
	path, err := s.ImagePath(next.ImageSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	sel, err := s.BeginBoot("boot-factory")
	if err != nil || sel.Trial || sel.Manifest.ImageSHA256 != factory.ImageSHA256 {
		t.Fatalf("factory not selected: %+v %v", sel, err)
	}
	if err = s.RecordFallbackContext(context.Background(), "boot-factory", factory.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	for _, hash := range []string{factory.ImageSHA256, next.ImageSHA256} {
		rbf, digest, pathErr := appliance.SplashPaths(root, hash)
		if pathErr != nil {
			t.Fatal(pathErr)
		}
		if err = os.Remove(rbf); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if err = os.Remove(digest); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	if got := shaOf(t, fat); got != sumNext {
		t.Fatal("recording factory fallback changed FAT")
	}
	sumFactory := writeImageSplash(t, idle, record, []byte("splash-of-factory-image"))
	restored := applianceupdate.NewWithSplash(s, c, applianceupdate.BootIdentity{BootID: "boot-factory", ImageSHA256: factory.ImageSHA256}, nil, nil, splash)
	if got := shaOf(t, fat); got != sumFactory {
		t.Fatalf("factory boot FAT %s want %s", got, sumFactory)
	}
	staged, _, err := appliance.SplashPaths(root, factory.ImageSHA256)
	if err != nil {
		t.Fatal(err)
	}
	if got := shaOf(t, staged); got != sumFactory {
		t.Fatalf("factory splash was not restaged from the running image: %s", got)
	}
	status, err := restored.Status(context.Background())
	if err != nil || status.Trial || status.FatIdleSHA256 != sumFactory || status.Good != factory.ImageSHA256 {
		t.Fatalf("factory fallback status: %+v %v", status, err)
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

func shaOf(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
