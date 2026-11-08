package kitsoftware

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestServiceRestartLedgerReconcilesAndRetainsByRequestID(t *testing.T) {
	root := privateRestartDir(t)
	now := time.Date(2026, 11, 8, 17, 0, 0, 0, time.UTC)
	ledger, err := OpenServiceRestartLedger(root)
	if err != nil {
		t.Fatal(err)
	}
	ledger.Now = func() time.Time { return now }
	request := ServiceRestartRequest{RequestID: strings.Repeat("a", 64), Service: serviceFogCastKit}
	record := ServiceRestartRecord{Request: request, Phase: "readiness", CreatedAt: now, UpdatedAt: now}
	if err := ledger.Save(record); err != nil {
		t.Fatal(err)
	}
	conflictingRequest := request
	conflictingRequest.Service = serviceTenfoot
	if err := ledger.Save(ServiceRestartRecord{Request: conflictingRequest, Phase: "complete", Outcome: "succeeded", CreatedAt: now, UpdatedAt: now}); err == nil {
		t.Fatal("ledger accepted a different request result under the same ID")
	}

	reopened, err := OpenServiceRestartLedger(root)
	if err != nil {
		t.Fatal(err)
	}
	interrupted, found, err := reopened.Lookup(request.RequestID)
	if err != nil || !found || interrupted.Phase != "interrupted" || interrupted.Outcome != "failed" {
		t.Fatalf("reconciled record = %+v, found=%v, err=%v", interrupted, found, err)
	}
	if err := reopened.Save(ServiceRestartRecord{Request: request, Phase: "complete", Outcome: "succeeded", CreatedAt: now, UpdatedAt: now}); err == nil {
		t.Fatal("interrupted request was replayable")
	}

	for index := 0; index < ServiceRestartMaxResults+1; index++ {
		id := fmt.Sprintf("%064x", index+1)
		at := now.Add(time.Duration(index) * time.Second)
		entry := ServiceRestartRecord{Request: ServiceRestartRequest{RequestID: id, Service: "other"}, Phase: "refused", Outcome: "refused", CreatedAt: at, UpdatedAt: at}
		if err := reopened.Save(entry); err != nil {
			t.Fatal(err)
		}
	}
	file, err := reopened.read()
	if err != nil || len(file.Records) != ServiceRestartMaxResults {
		t.Fatalf("retained records = %d, err=%v", len(file.Records), err)
	}
	if _, found, err := reopened.Lookup(fmt.Sprintf("%064x", 1)); err != nil || found {
		t.Fatalf("oldest result retained after capacity limit: found=%v err=%v", found, err)
	}
}

func TestServiceRestartLedgerExpiresResults(t *testing.T) {
	root := privateRestartDir(t)
	ledger, err := OpenServiceRestartLedger(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-ServiceRestartRetention - time.Second)
	ledger.Now = func() time.Time { return now }
	request := ServiceRestartRequest{RequestID: strings.Repeat("f", 64), Service: serviceFogCastKit}
	if err := ledger.Save(ServiceRestartRecord{
		Request: request, Phase: "refused", Outcome: "refused", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenServiceRestartLedger(root)
	if err != nil {
		t.Fatal(err)
	}
	file, err := reopened.read()
	if err != nil || len(file.Records) != 0 {
		t.Fatalf("expired result survived supervisor restart: records=%d err=%v", len(file.Records), err)
	}
}

func TestHostHealthReadinessRequiresExpectedReadyTarget(t *testing.T) {
	valid := fmt.Sprintf(`{"ready":true,"target":{"reachable":true,"ready":true,"connection":{"target_id":"%s"}}}`, DiagnosticPongTargetID)
	cases := map[string]string{
		"host-not-ready":     strings.Replace(valid, `"ready":true`, `"ready":false`, 1),
		"target-unreachable": strings.Replace(valid, `"reachable":true`, `"reachable":false`, 1),
		"target-not-ready":   strings.Replace(valid, `"target":{"reachable":true,"ready":true`, `"target":{"reachable":true,"ready":false`, 1),
		"missing-connection": `{"ready":true,"target":{"reachable":true,"ready":true}}`,
		"wrong-target":       strings.Replace(valid, DiagnosticPongTargetID, "another-target", 1),
		"trailing-json":      valid + `{}`,
	}
	if err := validateFogCastKitHealth(strings.NewReader(valid), DiagnosticPongTargetID); err != nil {
		t.Fatalf("positive host/target readiness rejected: %v", err)
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if err := validateFogCastKitHealth(strings.NewReader(body), DiagnosticPongTargetID); err == nil {
				t.Fatal("incomplete or mismatched host readiness was accepted")
			}
		})
	}
}

func TestTenfootReadinessMarkerRequiresCurrentIdentity(t *testing.T) {
	controller, _, _, bundle, _ := namedRestartFixture(t)
	process := targetProcess(controller, serviceTenfoot)
	marker := filepath.Join(controller.RunDir, "launcher-ready.json")
	write := func(bundleID string, pid int, suffix string) {
		t.Helper()
		data, err := json.Marshal(map[string]any{"bundle_id": bundleID, "pid": pid})
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, suffix...)
		if err := os.WriteFile(marker, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := probeTenfootReady(marker, process, bundle); err == nil {
		t.Fatal("missing readiness marker was accepted")
	}
	write(bundle, process.cmd.Process.Pid+1, "")
	if err := probeTenfootReady(marker, process, bundle); err == nil {
		t.Fatal("marker for a different process was accepted")
	}
	write(bundle+"x", process.cmd.Process.Pid, "")
	if err := probeTenfootReady(marker, process, bundle); err == nil {
		t.Fatal("marker for a different bundle was accepted")
	}
	write(bundle, process.cmd.Process.Pid, "{}")
	if err := probeTenfootReady(marker, process, bundle); err == nil {
		t.Fatal("marker with trailing JSON was accepted")
	}
	write(bundle, process.cmd.Process.Pid, "")
	if err := probeTenfootReady(marker, process, bundle); err != nil {
		t.Fatalf("current process frame acknowledgement rejected: %v", err)
	}
}

func TestAuthorizeNamedRestartSessionIsIdleOrExactPongOnly(t *testing.T) {
	idle := ServiceRestartSession{State: "idle", Execution: "none", LeaseState: "free", MaintenanceState: "none", RawIdleReady: true}
	if err := authorizeNamedRestartSession(ServiceRestartRequest{}, idle); err != nil {
		t.Fatalf("idle session rejected: %v", err)
	}

	active := ServiceRestartSession{
		TargetID: DiagnosticPongTargetID, State: "running_development", Execution: "development",
		Core: "fes.pong", PackageID: DiagnosticPongPackageID, RuntimeGeneration: 77,
		LeaseState: "held", LeaseGeneration: "lease-7", LeaseOwner: "hil-diagnostic",
		LeasePurpose: "pong-continuity", MaintenanceState: "none", SoftwareBundleID: strings.Repeat("a", 64),
	}
	request := ServiceRestartRequest{Session: &DiagnosticSessionExpectation{
		TargetID: DiagnosticPongTargetID, PackageID: DiagnosticPongPackageID, Generation: 77,
	}}
	if err := authorizeNamedRestartSession(request, active); err != nil {
		t.Fatalf("exact Pong diagnostic session rejected: %v", err)
	}
	for name, mutate := range map[string]func(*ServiceRestartRequest, *ServiceRestartSession){
		"target": func(r *ServiceRestartRequest, s *ServiceRestartSession) { r.Session.TargetID = "other" },
		"package": func(r *ServiceRestartRequest, s *ServiceRestartSession) {
			r.Session.PackageID = strings.Repeat("b", 64)
		},
		"generation":  func(r *ServiceRestartRequest, s *ServiceRestartSession) { r.Session.Generation++ },
		"lease":       func(r *ServiceRestartRequest, s *ServiceRestartSession) { s.LeaseState = "free" },
		"maintenance": func(r *ServiceRestartRequest, s *ServiceRestartSession) { s.MaintenanceState = "blocked" },
		"runtime":     func(r *ServiceRestartRequest, s *ServiceRestartSession) { s.State = "idle" },
	} {
		t.Run(name, func(t *testing.T) {
			gotRequest, gotSession := request, active
			gotRequest.Session = &DiagnosticSessionExpectation{TargetID: request.Session.TargetID, PackageID: request.Session.PackageID, Generation: request.Session.Generation}
			mutate(&gotRequest, &gotSession)
			if err := authorizeNamedRestartSession(gotRequest, gotSession); err == nil {
				t.Fatal("mismatched diagnostic session was accepted")
			}
		})
	}
	if err := authorizeNamedRestartSession(ServiceRestartRequest{}, active); err == nil {
		t.Fatal("live execution was accepted without an explicit diagnostic signature")
	}
}

func TestRestartHandlerRequiresRootAndIsIdempotent(t *testing.T) {
	ledger, err := OpenServiceRestartLedger(privateRestartDir(t))
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{RestartLedger: ledger}
	handler := server.RestartHandler()
	request := ServiceRestartRequest{RequestID: strings.Repeat("c", 64), Service: "unknown"}
	body, _ := json.Marshal(request)
	call := func(uid uint32, valid bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/v1/kit/software/restart-service", strings.NewReader(string(body)))
		if valid {
			r = r.WithContext(context.WithValue(r.Context(), localPeerCredentialsKey{}, localPeerCredentials{uid: uid, valid: true}))
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if response := call(1, true); response.Code != http.StatusForbidden {
		t.Fatalf("non-root peer status=%d body=%s", response.Code, response.Body.String())
	}
	initial := call(0, true)
	if initial.Code != http.StatusOK {
		t.Fatalf("root peer status=%d body=%s", initial.Code, initial.Body.String())
	}
	initialBody := initial.Body.String()
	duplicate := call(0, true)
	if duplicate.Code != http.StatusOK || duplicate.Body.String() != initialBody {
		t.Fatalf("duplicate request changed stored result: status=%d first=%s duplicate=%s", duplicate.Code, initialBody, duplicate.Body.String())
	}
	var first ServiceRestartRecord
	if err := json.Unmarshal(duplicate.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if first.Outcome != "refused" || first.Request != request {
		t.Fatalf("first result = %+v", first)
	}
	conflict := request
	conflict.Service = serviceFogCastKit
	conflictBytes, _ := json.Marshal(conflict)
	conflictRequest := httptest.NewRequest(http.MethodPost, "/v1/kit/software/restart-service", strings.NewReader(string(conflictBytes)))
	conflictRequest = conflictRequest.WithContext(context.WithValue(conflictRequest.Context(), localPeerCredentialsKey{}, localPeerCredentials{valid: true}))
	conflictResponse := httptest.NewRecorder()
	handler.ServeHTTP(conflictResponse, conflictRequest)
	if conflictResponse.Code != http.StatusConflict {
		t.Fatalf("request ID conflict status=%d body=%s", conflictResponse.Code, conflictResponse.Body.String())
	}
}

func TestRestartHandlerSerializesConcurrentDuplicateRequests(t *testing.T) {
	ledger, err := OpenServiceRestartLedger(privateRestartDir(t))
	if err != nil {
		t.Fatal(err)
	}
	restarter := &blockingNamedRestarter{started: make(chan struct{}), release: make(chan struct{})}
	state := State{Version: 1, Selected: strings.Repeat("a", 64), Receipts: map[string]Receipt{}}
	server := &Server{
		Repository: restartTestRepository{state: state}, Admission: &UnixAdmission{}, RestartLedger: ledger,
		Controller: restartTestController{restarter: restarter},
	}
	handler := server.RestartHandler()
	request := ServiceRestartRequest{RequestID: strings.Repeat("b", 64), Service: serviceFogCastKit}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	call := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/v1/kit/software/restart-service", strings.NewReader(string(body)))
		r = r.WithContext(context.WithValue(r.Context(), localPeerCredentialsKey{}, localPeerCredentials{valid: true}))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() { firstDone <- call() }()
	select {
	case <-restarter.started:
	case <-time.After(time.Second):
		close(restarter.release)
		t.Fatal("first request did not enter the serialized service operation")
	}
	secondStarted := make(chan struct{})
	secondDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		close(secondStarted)
		secondDone <- call()
	}()
	<-secondStarted
	select {
	case response := <-secondDone:
		close(restarter.release)
		t.Fatalf("duplicate request completed before the first operation: %s", response.Body.String())
	case <-time.After(20 * time.Millisecond):
	}
	close(restarter.release)
	var first, second *httptest.ResponseRecorder
	select {
	case first = <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("first operation did not finish after release")
	}
	select {
	case second = <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("serialized duplicate did not return its stored result")
	}
	if first.Code != http.StatusOK || second.Code != http.StatusOK || first.Body.String() != second.Body.String() {
		t.Fatalf("concurrent duplicate results differ: first=%d %s second=%d %s", first.Code, first.Body.String(), second.Code, second.Body.String())
	}
	restarter.mu.Lock()
	calls := restarter.calls
	restarter.mu.Unlock()
	if calls != 1 {
		t.Fatalf("duplicate request ran restart operation %d times", calls)
	}
}

type restartTestRepository struct{ state State }

func (r restartTestRepository) Read() (State, error) { return r.state, nil }
func (r restartTestRepository) Update(update func(*State) error) error {
	return update(&r.state)
}
func (restartTestRepository) WorkerLock() (func(), error) { return func() {}, nil }

type blockingNamedRestarter struct {
	mu      sync.Mutex
	calls   int
	started chan struct{}
	release chan struct{}
}

func (r *blockingNamedRestarter) RestartNamedService(_ context.Context, _ ServiceRestartRequest, _ State, _ *UnixAdmission, checkpoint func(ServiceRestartRecord) error, record ServiceRestartRecord) ServiceRestartRecord {
	r.mu.Lock()
	r.calls++
	if r.calls == 1 {
		close(r.started)
	}
	r.mu.Unlock()
	<-r.release
	now := time.Now().UTC()
	record.Phase, record.Outcome = "complete", "succeeded"
	record.UpdatedAt, record.FinishedAt = now, &now
	if err := checkpoint(record); err != nil {
		record.Phase, record.Outcome, record.Reason = "failed", "failed", err.Error()
	}
	return record
}

type restartTestController struct{ restarter namedServiceRestarter }

func (c restartTestController) Verify(context.Context, string) error         { return nil }
func (c restartTestController) Stop(context.Context) error                   { return nil }
func (c restartTestController) Start(context.Context, string, Request) error { return nil }
func (c restartTestController) Healthy(context.Context, string) error        { return nil }
func (c restartTestController) Active() string                               { return "" }
func (c restartTestController) ServiceFault() string                         { return "" }
func (c restartTestController) HostFault() string                            { return "" }
func (c restartTestController) RestartNamedService(ctx context.Context, request ServiceRestartRequest, state State, admission *UnixAdmission, checkpoint func(ServiceRestartRecord) error, record ServiceRestartRecord) ServiceRestartRecord {
	return c.restarter.RestartNamedService(ctx, request, state, admission, checkpoint, record)
}

func TestUnixPeerContextUsesKernelCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peer.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	serverConnection, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer serverConnection.Close()
	uid, valid := unixPeerUID(UnixPeerContext(context.Background(), serverConnection))
	if !valid || int(uid) != os.Getuid() {
		t.Fatalf("peer uid=%d valid=%v, want uid %d", uid, valid, os.Getuid())
	}
}

func TestNamedRestartAllowlistPreservesPong(t *testing.T) {
	for _, service := range []string{serviceFogCastKit, serviceTenfoot} {
		t.Run(service, func(t *testing.T) {
			controller, state, admission, bundle, original := namedRestartFixture(t)
			request := ServiceRestartRequest{
				RequestID: strings.Repeat("d", 64), Service: service,
				Session: &DiagnosticSessionExpectation{TargetID: DiagnosticPongTargetID, PackageID: DiagnosticPongPackageID, Generation: 77},
			}
			oldTarget := targetProcess(controller, service)
			ledger, err := OpenServiceRestartLedger(privateRestartDir(t))
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			record := ServiceRestartRecord{Request: request, Phase: "accepted", CreatedAt: now, UpdatedAt: now}
			if err := ledger.Save(record); err != nil {
				t.Fatal(err)
			}
			result := controller.RestartNamedService(context.Background(), request, state, admission, ledger.Save, record)
			if result.Outcome != "succeeded" || result.ReadinessResult != "positive-functional-readiness-stable" || !result.SiblingIdentitiesOK {
				t.Fatalf("restart result = %+v", result)
			}
			if result.Old == nil || result.Replacement == nil || result.Old.PID == result.Replacement.PID && result.Old.StartTime == result.Replacement.StartTime {
				t.Fatalf("old/new identities = old %+v new %+v", result.Old, result.Replacement)
			}
			if result.SessionBefore == nil || result.SessionAfter == nil || *result.SessionBefore != *result.SessionAfter {
				t.Fatalf("session changed across restart: before=%+v after=%+v", result.SessionBefore, result.SessionAfter)
			}
			for process, identity := range original {
				if process == oldTarget {
					continue
				}
				current, err := processIdentity(process)
				if err != nil || !sameProcessIdentity(identity, current) {
					t.Fatalf("sibling %s changed: identity=%+v current=%+v err=%v", identity.Role, identity, current, err)
				}
			}
			if controller.Active() != bundle || controller.pendingGroup != "" {
				t.Fatalf("active bundle or group recovery changed: active=%s pending=%s", controller.Active(), controller.pendingGroup)
			}
		})
	}
}

func TestNamedRestartSessionChangeSkipsReplacement(t *testing.T) {
	controller, state, admission, _, _ := namedRestartFixture(t)
	request := ServiceRestartRequest{RequestID: strings.Repeat("e", 64), Service: serviceFogCastKit,
		Session: &DiagnosticSessionExpectation{TargetID: DiagnosticPongTargetID, PackageID: DiagnosticPongPackageID, Generation: 77}}
	oldHost := controller.host
	previous := controller.serviceRestartRuntimeProbe
	var reads int
	controller.serviceRestartRuntimeProbe = func(ctx context.Context, process *child) (nativeStatus, error) {
		reads++
		status, err := previous(ctx, process)
		if reads >= 2 && err == nil {
			generation := uint64(78)
			status.Generation = &generation
		}
		return status, err
	}
	result := controller.RestartNamedService(context.Background(), request, state, admission, func(ServiceRestartRecord) error { return nil }, ServiceRestartRecord{
		Request: request, Phase: "accepted", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	if result.Outcome != "superseded" || result.Replacement != nil {
		t.Fatalf("changed session result = %+v", result)
	}
	if controller.host != oldHost || !processDone(oldHost) {
		t.Fatal("supervisor started a targeted host replacement after the session changed")
	}
}

func TestNamedRestartRefusesStaleExecutablePath(t *testing.T) {
	controller, state, admission, _, _ := namedRestartFixture(t)
	oldHost := controller.host
	controller.host.path += ".stale"
	request := ServiceRestartRequest{RequestID: strings.Repeat("8", 64), Service: serviceFogCastKit,
		Session: &DiagnosticSessionExpectation{TargetID: DiagnosticPongTargetID, PackageID: DiagnosticPongPackageID, Generation: 77}}
	result := controller.RestartNamedService(context.Background(), request, state, admission,
		func(ServiceRestartRecord) error { return nil }, ServiceRestartRecord{
			Request: request, Phase: "accepted", CreatedAt: time.Now(), UpdatedAt: time.Now(),
		})
	if result.Outcome != "refused" || processDone(oldHost) {
		t.Fatalf("stale executable path was not refused safely: result=%+v processDone=%v", result, processDone(oldHost))
	}
}

func TestRestartNeedsReadiness(t *testing.T) {
	for _, service := range []string{serviceFogCastKit, serviceTenfoot} {
		t.Run(service, func(t *testing.T) {
			controller, state, admission, _, _ := namedRestartFixture(t)
			controller.serviceRestartBudget = 20 * time.Millisecond
			controller.serviceRestartHostProbe = func(context.Context, string) error {
				return fmt.Errorf("host health is not ready")
			}
			controller.serviceRestartLauncherProbe = func(context.Context, *child, string) error {
				return fmt.Errorf("Tenfoot frame acknowledgement is absent")
			}
			request := ServiceRestartRequest{RequestID: strings.Repeat("9", 64), Service: service,
				Session: &DiagnosticSessionExpectation{TargetID: DiagnosticPongTargetID, PackageID: DiagnosticPongPackageID, Generation: 77}}
			record := ServiceRestartRecord{Request: request, Phase: "accepted", CreatedAt: time.Now(), UpdatedAt: time.Now()}
			result := controller.RestartNamedService(context.Background(), request, state, admission,
				func(ServiceRestartRecord) error { return nil }, record)
			if result.Outcome != "failed" || result.ReadinessResult != "failed" || result.Old == nil || result.Replacement == nil {
				t.Fatalf("process-only restart was accepted: %+v", result)
			}
			replacement := targetProcess(controller, service)
			if !replacement.failedReadiness || !processDone(replacement) {
				t.Fatalf("unready replacement was not stopped and marked for backoff: %+v", replacement)
			}
		})
	}
}

func processDone(process *child) bool {
	select {
	case <-process.done:
		return true
	default:
		return false
	}
}

func targetProcess(controller *ProcessController, service string) *child {
	if service == serviceFogCastKit {
		return controller.host
	}
	return controller.children[2]
}

func namedRestartFixture(t *testing.T) (*ProcessController, State, *UnixAdmission, string, map[*child]ServiceProcessIdentity) {
	t.Helper()
	bundle := strings.Repeat("a", 64)
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	sleep, err := os.ReadFile("/bin/sleep")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"mister-runtime", "mister-agent", "fogcast-tenfoot", hostExecutable} {
		path := filepath.Join(bin, name)
		if name == hostExecutable {
			path = filepath.Join(root, name)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, sleep, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	runDir := t.TempDir()
	if err := os.Symlink(root, filepath.Join(runDir, "current")); err != nil {
		t.Fatal(err)
	}
	entries, err := releaseIdentities(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	controller := &ProcessController{
		RunDir: runDir, active: bundle, prepared: &preparedRelease{id: bundle, root: root, entries: entries},
		serviceRestartStable: 5 * time.Millisecond, serviceRestartBudget: time.Second, serviceRestartPoll: time.Millisecond,
		serviceRestartTargetIDPath: filepath.Join(t.TempDir(), "target-id"),
	}
	if err := os.WriteFile(controller.serviceRestartTargetIDPath, []byte(DiagnosticPongTargetID+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, pair := range []struct{ role, path string }{
		{roleRuntime, filepath.Join(bin, "mister-runtime")},
		{roleAgent, filepath.Join(bin, "mister-agent")},
		{roleLauncher, filepath.Join(bin, "fogcast-tenfoot")},
	} {
		if err := controller.start(pair.path, []string{"60"}, nil, pair.role); err != nil {
			t.Fatal(err)
		}
	}
	controller.hostPlan = &hostPlan{path: filepath.Join(root, hostExecutable), args: []string{"60"}}
	controller.host, err = controller.spawn(controller.hostPlan.path, controller.hostPlan.args, nil, roleHost)
	if err != nil {
		t.Fatal(err)
	}
	controller.serviceRestartRuntimeProbe = func(context.Context, *child) (nativeStatus, error) {
		core := "fes.pong"
		generation := uint64(77)
		return nativeStatus{
			Protocol: 2, OK: true, State: "running_development", Execution: "development", Core: &core,
			Generation: &generation, ActivePackage: json.RawMessage(`{"package_id":"` + DiagnosticPongPackageID + `"}`), Version: "fixture",
		}, nil
	}
	controller.serviceRestartHostProbe = func(context.Context, string) error { return nil }
	controller.serviceRestartLauncherProbe = func(_ context.Context, process *child, bundleID string) error {
		data, _ := json.Marshal(map[string]any{"bundle_id": bundleID, "pid": process.cmd.Process.Pid})
		marker := filepath.Join(runDir, "launcher-ready.json")
		if err := os.WriteFile(marker, data, 0o600); err != nil {
			return err
		}
		return probeTenfootReady(marker, process, bundleID)
	}
	leaseStatus := []byte(fmt.Sprintf(`{"maintenance":{"state":"none"},"execution_lease":{"state":"held","generation":"lease-generation-77","owner":"hil-diagnostic","purpose":"pong-continuity"},"raw_idle_ready":false,"update_blocked":false,"bundle_id":"%s"}`, bundle))
	statusSocket := filepath.Join(t.TempDir(), "admission.sock")
	listener, err := net.Listen("unix", statusSocket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/status" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(leaseStatus)
			return
		}
		http.NotFound(w, r)
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	admission, err := NewUnixAdmission(statusSocket, filepath.Join(t.TempDir(), "admission.key"))
	if err != nil {
		t.Fatal(err)
	}
	original := make(map[*child]ServiceProcessIdentity)
	for _, process := range append(append([]*child(nil), controller.children...), controller.host) {
		identity, err := processIdentity(process)
		if err != nil {
			t.Fatal(err)
		}
		original[process] = identity
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), StopGroupTimeout)
		defer cancel()
		_ = controller.Stop(cleanupCtx)
	})
	return controller, State{Version: 1, Selected: bundle}, admission, bundle, original
}

func privateRestartDir(t *testing.T) string {
	t.Helper()
	path := t.TempDir()
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
