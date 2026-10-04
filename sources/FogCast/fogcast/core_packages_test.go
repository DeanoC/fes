package fogcast

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

type packageLibraryClient struct {
	*fakeServiceClient
	inspection  protocol.CoreInspection
	inspections int
}

func (c *packageLibraryClient) InspectCore(ctx context.Context, n int64, r io.Reader) (protocol.CoreInspection, error) {
	c.inspections++
	staged, err := corepackage.Stage(ctx, os.TempDir(), n, r)
	if err != nil {
		return protocol.CoreInspection{}, err
	}
	defer staged.Cleanup()
	if staged.PackageID != c.inspection.PackageID {
		return protocol.CoreInspection{PackageID: staged.PackageID, Descriptor: staged.Descriptor, Compatible: true}, nil
	}
	return c.inspection, nil
}

func libraryPackageFixture(t *testing.T, version string, extras ...string) []byte {
	t.Helper()
	base := "../corepackage/testdata/core-bundle-v2/"
	manifest, err := os.ReadFile(base + "manifests/valid-basic.toml")
	if err != nil {
		t.Fatal(err)
	}
	manifest = bytes.Replace(manifest, []byte(`version = "0.1.0"`), []byte(`version = "`+version+`"`), 1)
	for _, extra := range extras {
		manifest = append(manifest, []byte(extra)...)
	}
	payload, err := os.ReadFile(base + "payloads/fes-fixture.rbf")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	for _, e := range []struct {
		name string
		data []byte
	}{{"manifest.toml", manifest}, {"core.rbf", payload}} {
		h := make([]byte, 512)
		copy(h, e.name)
		copy(h[100:], "0000644\x00")
		copy(h[108:], "0000000\x00")
		copy(h[116:], "0000000\x00")
		copy(h[124:], fmt.Sprintf("%011o\x00", len(e.data)))
		copy(h[136:], "00000000000\x00")
		copy(h[148:], "        ")
		h[156] = '0'
		copy(h[257:], "ustar\x00")
		copy(h[263:], "00")
		sum := 0
		for _, v := range h {
			sum += int(v)
		}
		copy(h[148:], fmt.Sprintf("%06o\x00 ", sum))
		out.Write(h)
		out.Write(e.data)
		out.Write(make([]byte, (512-len(e.data)%512)%512))
	}
	out.Write(make([]byte, 1024))
	return out.Bytes()
}

func TestCoreEntryConflictMapsToStaleRevision(t *testing.T) {
	err := mapCoreEntryError(catalog.ErrCoreEntryConflict)
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %T %v, want API error", err, err)
	}
	if apiErr.Code != protocol.CodeStaleRevision {
		t.Fatalf("code = %q, want %q", apiErr.Code, protocol.CodeStaleRevision)
	}
	if apiErr.Message != "core entry selection changed; refresh and retry" {
		t.Fatalf("message = %q", apiErr.Message)
	}
}

type failedLaunchLeaseStub struct {
	held     bool
	releases int
}

func (l *failedLaunchLeaseStub) Held() bool { return l.held }
func (l *failedLaunchLeaseStub) Release(context.Context) error {
	l.releases++
	l.held = false
	return nil
}

func TestUpdateConflictReleasesOnlyNewLaunchLease(t *testing.T) {
	conflict := &protocol.APIError{Code: "UPDATE_CONFLICT", Message: "revision changed"}
	t.Run("newly acquired", func(t *testing.T) {
		lease := &failedLaunchLeaseStub{held: true}
		got := releaseFailedLaunchLease(conflict, false, lease, false)
		if !errors.Is(got, conflict) || lease.releases != 1 || lease.held {
			t.Fatalf("error=%v lease=%+v", got, lease)
		}
	})
	t.Run("pre-existing live play", func(t *testing.T) {
		lease := &failedLaunchLeaseStub{held: true}
		got := releaseFailedLaunchLease(conflict, true, lease, true)
		if !errors.Is(got, conflict) || lease.releases != 0 || !lease.held {
			t.Fatalf("error=%v lease=%+v", got, lease)
		}
	})
	t.Run("new grant adopted by live play", func(t *testing.T) {
		lease := &failedLaunchLeaseStub{held: true}
		got := releaseFailedLaunchLease(conflict, false, lease, true)
		if !errors.Is(got, conflict) || lease.releases != 0 || !lease.held {
			t.Fatalf("error=%v lease=%+v", got, lease)
		}
	})
}

func TestFailedCoreLaunchCleanupDoesNotReenterTargetMu(t *testing.T) {
	statusEntered := make(chan struct{})
	finishStatus := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/health":
			_ = json.NewEncoder(w).Encode(protocol.Health{APIVersion: "v1", Ready: true})
		case "/v1/status":
			close(statusEntered)
			<-finishStatus
			http.Error(w, "status unavailable", http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	base, _ := url.Parse(srv.URL)
	client := targetclient.NewClient(base, "secret", srv.Client()).WithKitLease(targetclient.NewKitLease(base, "secret", srv.Client(), "host", "test"))
	s := newService(Config{Targets: []TargetConfig{{Name: "kit", Enabled: true, Address: srv.URL, Agent: "secret"}}, SelectedTarget: "kit", RequestTimeout: time.Second}, Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, client)
	done := make(chan error, 1)
	go func() {
		_, err := s.loadCoreLocked(context.Background(), context.Background(), func(context.Context) (coreLoadSource, error) {
			return coreLoadSource{size: 1, body: bytes.NewReader([]byte{1})}, nil
		})
		done <- err
	}()
	select {
	case <-statusEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("launch did not reach status")
	}
	writerDone := make(chan struct{})
	go func() { s.targetMu.Lock(); s.targetMu.Unlock(); close(writerDone) }()
	deadline := time.Now().Add(time.Second)
	for s.targetMu.TryRLock() {
		s.targetMu.RUnlock()
		if time.Now().After(deadline) {
			t.Fatal("target writer did not queue")
		}
		runtime.Gosched()
	}
	close(finishStatus)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("launch unexpectedly succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("failed launch cleanup blocked behind queued target writer")
	}
	select {
	case <-writerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("target writer did not complete")
	}
}

func TestInstalledPackageSelectionAndLibraryLaunch(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := catalog.OpenContext(ctx, filepath.Join(root, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	packages, err := corepackage.NewStore(filepath.Join(root, "packages"))
	if err != nil {
		t.Fatal(err)
	}
	client := &packageLibraryClient{fakeServiceClient: &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateIdle}}}
	s := newService(Config{RequestTimeout: time.Second, UploadTimeout: time.Second}, Paths{}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, client)
	s.corePackages = packages
	raw := libraryPackageFixture(t, "0.1.0")
	first, created, err := s.ImportCorePackage(ctx, int64(len(raw)), bytes.NewReader(raw))
	if err != nil || !created {
		t.Fatalf("import: %v new=%v", err, created)
	}
	if client.inspections != 0 || client.coreCalls != 0 {
		t.Fatal("import contacted target")
	}
	client.inspection = protocol.CoreInspection{PackageID: first.PackageID, Descriptor: first.Descriptor, Compatible: true}
	entry, err := s.CreateCoreEntry(ctx, "Standalone Pong", first.PackageID)
	if err != nil {
		t.Fatal(err)
	}
	client.coreLoad = func(_ context.Context, n int64, r io.Reader) (protocol.Status, error) {
		b, _ := io.ReadAll(r)
		if !bytes.Equal(raw, b) {
			t.Fatal("wrong selected archive")
		}
		core := "fes.pong"
		return protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core, CorePackage: &protocol.CorePackageStatus{PackageID: first.PackageID, Generation: 4, ABI: protocol.RuntimeContract{ID: first.Descriptor.ABI.ID, Major: 1}, BuildID: first.Descriptor.Build.ID, Gamepad: true}}, nil
	}
	response, err := s.Launch(ctx, entry.GameID, nil)
	if err != nil || response.Status.GameID == nil || *response.Status.GameID != entry.GameID {
		t.Fatalf("launch: %+v %v", response, err)
	}
	client.statusResult = response.Status
	client.statusResult.GameID = nil
	client.statusResult.System = nil
	got, err := s.Status(ctx)
	if err != nil || got.GameID == nil || *got.GameID != entry.GameID {
		t.Fatalf("status %+v %v", got, err)
	}
	secondRaw := libraryPackageFixture(t, "0.2.0")
	second, _, err := s.ImportCorePackage(ctx, int64(len(secondRaw)), bytes.NewReader(secondRaw))
	if err != nil {
		t.Fatal(err)
	}
	client.inspection = protocol.CoreInspection{PackageID: second.PackageID, Descriptor: second.Descriptor, Compatible: false, CompatibilityError: &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "unsupported", Phase: "compatibility"}}
	if _, err = s.SelectCoreEntry(ctx, entry.GameID, first.PackageID, second.PackageID); err == nil {
		t.Fatal("incompatible selected")
	}
	selected, err := s.CoreEntry(ctx, entry.GameID)
	if err != nil || selected.PackageID != first.PackageID {
		t.Fatalf("changed selection: %+v %v", selected, err)
	}
	client.inspection.Compatible = true
	client.inspection.CompatibilityError = nil
	if _, err = s.SelectCoreEntry(ctx, entry.GameID, first.PackageID, second.PackageID); err != nil {
		t.Fatal(err)
	}
	got, err = s.Status(ctx)
	if err != nil || got.CorePackage.PackageID != first.PackageID || got.GameID == nil {
		t.Fatalf("selection relabeled active: %+v %v", got, err)
	}

	if _, err = s.SelectCoreEntry(ctx, entry.GameID, first.PackageID, second.PackageID); err == nil {
		t.Fatal("stale selection accepted")
	}
	client.inspection = protocol.CoreInspection{PackageID: first.PackageID, Descriptor: first.Descriptor, Compatible: true}
	if _, err = s.SelectCoreEntry(ctx, entry.GameID, second.PackageID, first.PackageID); err != nil {
		t.Fatal("rollback:", err)
	}
	client.statusResult.CorePackage.Generation++
	got, err = s.Status(ctx)
	if err != nil || got.GameID != nil {
		t.Fatalf("new external generation inherited game: %+v %v", got, err)
	}
	if client.coreCalls != 1 {
		t.Fatal("selection activated package")
	}
	// A contradictory active identity is not allowed to acquire controls through a later status poll.
	unexpected := client.statusResult
	copied := *unexpected.CorePackage
	copied.PackageID = second.PackageID
	copied.Generation = 99
	unexpected.CorePackage = &copied
	unexpected.GameID = nil
	unexpected.System = nil
	client.coreLoad = func(context.Context, int64, io.Reader) (protocol.Status, error) { return unexpected, nil }
	client.stopErr = errors.New("lost stop")
	client.statusResult = unexpected
	response, err = s.Launch(ctx, entry.GameID, nil)
	if err == nil || response.Status.LastError == nil {
		t.Fatalf("unexpected identity accepted: %+v %v", response, err)
	}
	got, err = s.Status(ctx)
	if err != nil || got.LastError == nil || got.GameID != nil {
		t.Fatalf("rejected identity regained controls: %+v %v", got, err)
	}
	client.stopErr = nil
	client.stopResult = protocol.Status{State: protocol.StateIdle}
	if _, err = s.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	client.statusResult = protocol.Status{State: protocol.StateIdle}
	got, err = s.Status(ctx)
	if err != nil || got.LastError != nil {
		t.Fatalf("stop did not clear rejection: %+v %v", got, err)
	}
	// Retain a host executor whose cleanup fails while the unexpected FPGA owner also needs recovery.
	executor := &fakeHostExecutor{stopErr: errors.New("host busy")}
	s.hostExecutor = executor
	s.hostCastClaimsKitDisplay = true // This case exercises cast-mode dual-owner recovery.
	s.activeExecution = ExecutionHostOnly
	s.activeGameID = "host-game"
	client.stopErr = errors.New("target stop lost")
	client.statusResult = unexpected
	response, err = s.Launch(ctx, entry.GameID, nil)
	if err == nil || executor.stopCalls != 1 || s.activeExecution != ExecutionHostOnly {
		t.Fatalf("host owner lost: %+v %v stops=%d execution=%s", response, err, executor.stopCalls, s.activeExecution)
	}
	s.targets = []TargetConfig{{Name: s.selectedTarget, Enabled: true}}
	got, err = s.StatusTarget(ctx, s.selectedTarget)
	if err != nil || got.LastError == nil || got.CorePackage == nil || got.GameID != nil {
		t.Fatalf("dual recovery hidden: %+v %v", got, err)
	}
	client.stopErr = nil
	if _, err = s.Stop(ctx); err == nil {
		t.Fatal("declared idle while host cleanup failed")
	}
	client.statusResult = protocol.Status{State: protocol.StateIdle}
	stops := client.stopCalls
	executor.stopErr = nil
	if _, err = s.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if client.stopCalls != stops || s.activeExecution != "" || executor.stopCalls != 3 || s.plays[s.selectedTarget].packageRejection == nil {
		t.Fatalf("host cleanup retry: target stops=%d/%d host=%d execution=%s kit=%+v", client.stopCalls, stops, executor.stopCalls, s.activeExecution, s.plays[s.selectedTarget])
	}
	if _, err = s.Stop(ctx); err != nil || s.plays[s.selectedTarget].packageRejection != nil {
		t.Fatalf("kit recovery: %v play=%+v", err, s.plays[s.selectedTarget])
	}

}

func TestPackageRejectionClearsOnNewRawOwnership(t *testing.T) {
	core := "dev.rbf"
	active := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core}
	client := &fakeServiceClient{developmentLoad: func(context.Context, int64, io.Reader) (protocol.Status, error) { return active, nil }, statusResult: active,
		stopResult: protocol.Status{State: protocol.StateIdle}}
	s := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
	s.plays[s.selectedTarget] = targetPlay{execution: ExecutionFPGADevelopment, packageRejection: &protocol.APIError{Code: protocol.CodeUnrecognizedCore, Phase: "identity"}}
	if _, err := s.LoadDevelopmentRBF(context.Background(), 3, bytes.NewReader([]byte("rbf"))); err != nil {
		t.Fatal(err)
	}
	if client.stopCalls != 1 {
		t.Fatalf("rejected package recovery stops=%d", client.stopCalls)
	}
	status, err := s.Status(context.Background())
	if err != nil || status.LastError != nil {
		t.Fatalf("new raw load inherited rejection: %+v %v", status, err)
	}
}

func TestRejectedLibraryKitRecoversBeforeReplacementWhileHostPlays(t *testing.T) {
	ctx := context.Background()
	raw := libraryPackageFixture(t, "0.1.0")
	s, client, entry, inspection := newCoreEntryLaunchFixture(t, raw, "Pong")
	host := &fakeHostExecutor{}
	s.hostExecutor = host
	s.activeExecution, s.activeTarget, s.activeGameID, s.activeSystem = ExecutionHostOnly, "host", "host-game", protocol.SystemSNES
	active := coreEntryActiveStatus(inspection, 3, false)
	client.statusResult = active
	client.stopErr = errors.New("kit recovery unavailable")
	client.coreLoad = func(context.Context, int64, io.Reader) (protocol.Status, error) {
		wrong := active
		wrong.CorePackage = &protocol.CorePackageStatus{PackageID: fmt.Sprintf("%064x", 1), Generation: 3,
			ABI: active.CorePackage.ABI, BuildID: active.CorePackage.BuildID}
		return wrong, nil
	}
	if _, err := s.Launch(ctx, entry.GameID, nil); err == nil {
		t.Fatal("mismatched package was accepted")
	}
	if s.plays[s.selectedTarget].packageRejection == nil || host.stopCalls != 0 || s.activeTarget != "host" || s.activeGameID != "host-game" || len(s.PlaySessions()) != 0 {
		t.Fatalf("rejection=%+v host stops=%d host game=%q plays=%+v", s.plays[s.selectedTarget], host.stopCalls, s.activeGameID, s.PlaySessions())
	}
	priorLoads := client.coreCalls
	client.stopErr = nil
	client.stopResult = protocol.Status{State: protocol.StateIdle}
	client.statusResult = protocol.Status{State: protocol.StateIdle}
	client.coreLoad = func(context.Context, int64, io.Reader) (protocol.Status, error) { return active, nil }
	if _, err := s.Launch(ctx, entry.GameID, nil); err != nil {
		t.Fatal(err)
	}
	if client.stopCalls == 0 || client.coreCalls != priorLoads+1 || s.plays[s.selectedTarget].packageRejection != nil || host.stopCalls != 0 || s.activeTarget != "host" || s.activeGameID != "host-game" {
		t.Fatalf("recovery stops=%d loads=%d/%d kit=%+v host stops=%d host game=%q", client.stopCalls, client.coreCalls, priorLoads, s.plays[s.selectedTarget], host.stopCalls, s.activeGameID)
	}
}

func TestPendingPackageRejectionPreservesRecoveryFailure(t *testing.T) {
	rejection := &protocol.APIError{Code: protocol.CodeUnrecognizedCore, Phase: "identity", Expected: "selected", Observed: "unexpected"}
	unexpected := protocol.Status{
		State:       protocol.StateActive,
		Development: true,
		CorePackage: &protocol.CorePackageStatus{PackageID: "unexpected", Generation: 9},
	}
	client := &fakeServiceClient{
		statusResult: unexpected,
		stopErr:      errors.New("target stop lost"),
	}
	s := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
	s.plays[s.selectedTarget] = targetPlay{execution: ExecutionFPGADevelopment, packageRejection: rejection}

	status, err := s.LoadCore(context.Background(), 3, bytes.NewReader([]byte("new")))
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Phase != "recovery" || apiErr.Code != protocol.CodeMiSTerUnavailable {
		t.Fatalf("error = %#v, want MISTER_UNAVAILABLE recovery", err)
	}
	if status.CorePackage == nil || status.CorePackage.PackageID != "unexpected" || status.LastError != rejection {
		t.Fatalf("status lost recovery evidence: %+v", status)
	}
	if client.coreCalls != 0 {
		t.Fatalf("new package dispatched during pending recovery: %d", client.coreCalls)
	}
}

func TestStopUsesUploadTimeoutForPendingPackageRejection(t *testing.T) {
	client := &fakeServiceClient{}
	client.statusFn = func(ctx context.Context) (protocol.Status, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) < 500*time.Millisecond {
			return protocol.Status{}, errors.New("recovery received request timeout")
		}
		return protocol.Status{State: protocol.StateIdle}, nil
	}
	s := newService(
		Config{RequestTimeout: 10 * time.Millisecond, UploadTimeout: time.Second},
		Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, client,
	)
	s.hostExecutor = &fakeHostExecutor{}
	s.activeExecution = ExecutionHostOnly
	s.plays[s.selectedTarget] = targetPlay{packageRejection: &protocol.APIError{Code: protocol.CodeUnrecognizedCore, Phase: "identity"}}

	status, err := s.Stop(context.Background())
	if err != nil || status.State != protocol.StateIdle {
		t.Fatalf("stop = %+v, %v", status, err)
	}
}

func TestActivatedLibraryPackageRetainsFailedHostCleanup(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	catalogStore, err := catalog.OpenContext(ctx, filepath.Join(root, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer catalogStore.Close()
	packages, err := corepackage.NewStore(filepath.Join(root, "packages"))
	if err != nil {
		t.Fatal(err)
	}
	client := &packageLibraryClient{fakeServiceClient: &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateIdle}}}
	s := newService(Config{RequestTimeout: time.Second, UploadTimeout: time.Second}, Paths{}, catalogStore, &fakeServiceScanner{}, &fakeServicePreparer{}, client)
	s.corePackages = packages
	raw := libraryPackageFixture(t, "0.1.0")
	installed, _, err := s.ImportCorePackage(ctx, int64(len(raw)), bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	client.inspection = protocol.CoreInspection{PackageID: installed.PackageID, Descriptor: installed.Descriptor, Compatible: true}
	entry, err := s.CreateCoreEntry(ctx, "Cleanup Pong", installed.PackageID)
	if err != nil {
		t.Fatal(err)
	}
	core := installed.Descriptor.Core.ID
	active := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core, CorePackage: &protocol.CorePackageStatus{PackageID: installed.PackageID, Generation: 7, ABI: protocol.RuntimeContract{ID: installed.Descriptor.ABI.ID, Major: 1}, BuildID: installed.Descriptor.Build.ID, Gamepad: true}}
	client.coreLoad = func(context.Context, int64, io.Reader) (protocol.Status, error) {
		client.statusResult = active
		return active, nil
	}
	executor := &fakeHostExecutor{stopErr: errors.New("host cleanup failed")}
	s.hostExecutor = executor
	s.hostCastClaimsKitDisplay = true // Failed cast cleanup must retain the host owner.
	s.activeExecution = ExecutionHostOnly
	s.activeGameID = "host-game"
	response, err := s.Launch(ctx, entry.GameID, nil)
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Phase != "recovery" {
		t.Fatalf("launch error: %v", err)
	}
	if s.activeExecution != ExecutionHostOnly || s.plays[s.selectedTarget].packageRejection == nil || response.Status.LastError == nil {
		t.Fatalf("lost host cleanup owner: execution=%s status=%+v", s.activeExecution, response.Status)
	}
	s.targets = []TargetConfig{{Name: s.selectedTarget, Enabled: true}}
	status, err := s.StatusTarget(ctx, s.selectedTarget)
	if err != nil || status.LastError == nil || status.CorePackage == nil || status.GameID != nil || s.activeGameID != "host-game" {
		t.Fatalf("pending cleanup hidden: %+v %v", status, err)
	}
	// A replacement must finish pending cleanup before dispatching another package.
	client.stopResult = protocol.Status{State: protocol.StateIdle}
	if _, err = s.Launch(ctx, entry.GameID, nil); err == nil || client.coreCalls != 1 {
		t.Fatalf("replacement skipped cleanup: calls=%d err=%v", client.coreCalls, err)
	}
	client.statusResult = protocol.Status{State: protocol.StateIdle}
	if _, err = s.Stop(ctx); err == nil {
		t.Fatal("idle reported while host cleanup still fails")
	}
	stops := client.stopCalls
	executor.stopErr = nil
	status, err = s.Stop(ctx)
	if err != nil || status.State != protocol.StateIdle || s.activeExecution != "" || s.plays[s.selectedTarget].packageRejection != nil || executor.stopCalls != 4 || client.stopCalls != stops {
		t.Fatalf("cleanup retry: %+v %v host=%d target=%d/%d execution=%s", status, err, executor.stopCalls, client.stopCalls, stops, s.activeExecution)
	}
}

func (c *packageLibraryClient) InspectCoreData(ctx context.Context, n int64, r io.Reader, id string) (protocol.CoreDataInspection, error) {
	staged, err := corepackage.Stage(ctx, os.TempDir(), n, r)
	if err != nil {
		return protocol.CoreDataInspection{}, err
	}
	defer staged.Cleanup()
	return protocol.CoreDataInspection{CoreData: protocol.CoreData{PackageID: staged.PackageID, CoreID: staged.Descriptor.Core.ID, Mode: "volatile", Revision: "absent", PaddleSpeed: 1}, Descriptor: staged.Descriptor}, nil
}
func (c *packageLibraryClient) UpdateCoreSettings(context.Context, int64, io.Reader, protocol.CoreSettingsUpdate) (protocol.CoreDataInspection, error) {
	return protocol.CoreDataInspection{}, errors.New("unexpected settings write")
}
func (c *packageLibraryClient) LoadLibraryCore(ctx context.Context, n int64, r io.Reader, id string) (protocol.Status, error) {
	return c.LoadCore(ctx, n, r)
}
