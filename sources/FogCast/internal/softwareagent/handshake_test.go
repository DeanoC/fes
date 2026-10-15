package softwareagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/kitlease"
	contract "github.com/DeanoC/FogCast/kitlease"
)

func manager(t *testing.T, root string, cleanup func(context.Context) error) *kitlease.Manager {
	t.Helper()
	m := kitlease.New(time.Minute, cleanup, kitlease.WithMaintenanceFile(filepath.Join(root, "maintenance.json"), filepath.Join(root, "maintenance.lock"), "12345678-1234-1234-1234-123456789abc"))
	t.Cleanup(m.Close)
	return m
}
func await(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not reached")
}
func request() Request {
	return Request{ClaimRequest: contract.ClaimRequest{RequestID: strings.Repeat("a", 64), Owner: "host-one", Purpose: "software-update"}, Credential: strings.Repeat("b", 64)}
}
func newAdmission(m *kitlease.Manager, blocked bool) *Admission {
	return &Admission{Leases: m, Fence: NewFence(blocked), Ready: func(context.Context) bool { return true }, UpdateBlocked: func(context.Context) bool { return false }, BundleID: strings.Repeat("c", 64)}
}
func post(h http.Handler, path string, value any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(value)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", path, bytes.NewReader(b)))
	return w
}
func assertBlocked(t *testing.T, f *Fence) {
	t.Helper()
	_, done, e := f.Begin(context.Background())
	if done != nil {
		done()
	}
	if !errors.Is(e, ErrBlocked) {
		t.Fatalf("mutation admitted: %v", e)
	}
}

func TestRealLeaseHandshakePreservesOwnerThenDrains(t *testing.T) {
	var cleanups atomic.Int32
	m := manager(t, t.TempDir(), func(context.Context) error { cleanups.Add(1); return nil })
	await(t, func() bool { return m.Status().State == "free" })
	owner, e := m.Claim(contract.ClaimRequest{RequestID: strings.Repeat("d", 64), Owner: "game-host", Purpose: "playing"})
	if e != nil {
		t.Fatal(e)
	}
	a := newAdmission(m, false)
	h := a.Handler()
	req := request()
	if w := post(h, "/queue", req); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := post(h, "/acquire", req); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, e = m.Renew(owner.Token); e != nil {
		t.Fatal("queue disturbed live owner", e)
	}
	if cleanups.Load() != 1 {
		t.Fatal("queue stopped game")
	}
	if _, e = m.Release(owner.Token); e != nil {
		t.Fatal(e)
	}
	await(t, func() bool { return m.Status().State == "free" })
	if w := post(h, "/acquire", req); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	await(t, func() bool { s, e := m.MaintenanceStatusChecked(); return e == nil && s.State == "ready" })
	ctx, done, e := a.Fence.Begin(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	response := make(chan *httptest.ResponseRecorder, 1)
	go func() { response <- post(h, "/acquire", req) }()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("mutation was not cancelled")
	}
	assertBlocked(t, a.Fence)
	select {
	case <-response:
		t.Fatal("acquire returned before mutation drained")
	default:
	}
	done()
	w := <-response
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var grant contract.MaintenanceGrant
	if e = json.Unmarshal(w.Body.Bytes(), &grant); e != nil {
		t.Fatal(e)
	}
	if grant.Status.State != "ready" {
		t.Fatal(grant)
	}
	if w = post(h, "/cancel", req); w.Code != 409 {
		t.Fatal("cancel reopened active maintenance", w.Code)
	}
	assertBlocked(t, a.Fence)
}

func TestRestartResumeAndEndWaitForActualCleanup(t *testing.T) {
	root := t.TempDir()
	m := manager(t, root, func(context.Context) error { return nil })
	await(t, func() bool { return m.Status().State == "free" })
	req := request()
	if e := m.QueueIdleMaintenance(req.ClaimRequest, req.Credential); e != nil {
		t.Fatal(e)
	}
	grant, e := m.StartQueuedMaintenance(req.ClaimRequest, req.Credential)
	if e != nil {
		t.Fatal(e)
	}
	await(t, func() bool { return m.MaintenanceStatus().State == "ready" })
	m.Close()
	var finishing atomic.Bool
	finishStarted := make(chan struct{}, 1)
	finishAllowed := make(chan struct{})
	replacement := manager(t, root, func(ctx context.Context) error {
		if finishing.Load() {
			finishStarted <- struct{}{}
			select {
			case <-finishAllowed:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})
	await(t, func() bool { return replacement.MaintenanceStatus().State == "blocked" })
	a := newAdmission(replacement, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.QueueStartup(ctx, req)
	h := a.Handler()
	complete := Completion{MaintenanceEndRequest: contract.MaintenanceEndRequest{OperationID: grant.Status.OperationID, ExpectedGeneration: grant.Status.Generation}, Credential: grant.Token}
	if w := post(h, "/resume", complete); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	await(t, func() bool { return replacement.MaintenanceStatus().State == "ready" })
	assertBlocked(t, a.Fence)
	finishing.Store(true)
	w := post(h, "/end", complete)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	<-finishStarted
	assertBlocked(t, a.Fence)
	if w = post(h, "/cancel", req); w.Code != 409 {
		t.Fatal("cancel finishing", w.Code)
	}
	assertBlocked(t, a.Fence)
	close(finishAllowed)
	await(t, func() bool { return replacement.MaintenanceStatus().State == "none" })
	w = post(h, "/end", complete)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	_, done, e := a.Fence.Begin(context.Background())
	if e != nil {
		t.Fatal("completed operation kept fence closed", e)
	}
	done()
}

func TestStartupCancellationCannotRequeueAndOpensFence(t *testing.T) {
	m := manager(t, t.TempDir(), func(context.Context) error { return nil })
	await(t, func() bool { return m.Status().State == "free" })
	a := newAdmission(m, true)
	var updateBlocked atomic.Bool
	updateBlocked.Store(true)
	a.UpdateBlocked = func(context.Context) bool { return updateBlocked.Load() }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := request()
	a.QueueStartup(ctx, req)
	if w := post(a.Handler(), "/cancel", req); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	updateBlocked.Store(false)
	// The retry loop would resurrect the intent after its 100ms interval without
	// cancellation sharing the startup-registration lock.
	time.Sleep(150 * time.Millisecond)
	owner, e := m.Claim(contract.ClaimRequest{RequestID: strings.Repeat("d", 64), Owner: "next-host", Purpose: "playing"})
	if e != nil {
		t.Fatal("cancelled startup request requeued", e)
	}
	if owner.Token == "" {
		t.Fatal("missing lease")
	}
	_, done, e := a.Fence.Begin(context.Background())
	if e != nil {
		t.Fatal("cancelled startup left mutation fence closed", e)
	}
	done()
}

func TestTrialReconciliationClaimDuringHandoff(t *testing.T) {
	m := manager(t, t.TempDir(), func(context.Context) error { return nil })
	await(t, func() bool { return m.Status().State == "free" })
	a := newAdmission(m, false)
	h := a.Handler()
	boot := request()
	if w := post(h, "/queue", boot); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var grant contract.MaintenanceGrant
	deadline := time.Now().Add(3 * time.Second)
	for {
		w := post(h, "/acquire", boot)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &grant); err != nil {
			t.Fatal(err)
		}
		if grant.Status.State == "ready" && grant.Token != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(grant)
		}
		time.Sleep(10 * time.Millisecond)
	}
	foreign := request()
	foreign.RequestID = strings.Repeat("c", 64)
	foreign.Credential = strings.Repeat("d", 64)
	if w := post(h, "/queue", foreign); w.Code == 200 {
		t.Fatal("queue without the boot credential crossed maintenance")
	}
	foreign.HeldToken = grant.Token
	if w := post(h, "/queue", foreign); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var claimed atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	competitor := contract.ClaimRequest{RequestID: strings.Repeat("e", 64), Owner: "other-host", Purpose: "playing"}
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := m.Claim(competitor); err == nil {
				claimed.Add(1)
			}
		}()
	}
	close(start)
	end := Completion{MaintenanceEndRequest: contract.MaintenanceEndRequest{OperationID: grant.Status.OperationID, ExpectedGeneration: grant.Status.Generation}, Credential: grant.Token}
	if w := post(h, "/end", end); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	await(t, func() bool {
		status, err := m.MaintenanceStatusChecked()
		return err == nil && (status.State == "none" || status.State == "completed")
	})
	await(t, func() bool { return m.Status().State == "free" })
	wg.Wait()
	if claimed.Load() != 0 {
		t.Fatalf("claim won during trial reconciliation: %d", claimed.Load())
	}
	if _, err := m.Claim(competitor); err == nil {
		t.Fatal("claim won after the boot reservation ended")
	}
}
