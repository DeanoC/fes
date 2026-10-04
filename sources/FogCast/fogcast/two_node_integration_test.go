package fogcast

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/hostexec"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/romsource"
)

const twoNodeSoftwareGameID = "two-node-software"

type twoNodeHostExecutor struct {
	mu          sync.Mutex
	launchCalls int
	stopCalls   int
	active      bool
	launchErr   error
	launchGate  <-chan struct{}
	started     chan struct{}
	startOnce   sync.Once
}

func (*twoNodeHostExecutor) ID() string { return "two-node-host" }

func (*twoNodeHostExecutor) Capabilities() []hostexec.Capability {
	return []hostexec.Capability{hostexec.HostOnly}
}

func (e *twoNodeHostExecutor) Launch(_ context.Context, content io.Reader, identity protocol.ContentIdentity) (hostexec.Status, error) {
	if content == nil || identity.Size == 0 {
		return hostexec.Status{}, errors.New("missing host content")
	}
	if _, err := io.Copy(io.Discard, content); err != nil {
		return hostexec.Status{}, err
	}
	e.mu.Lock()
	e.launchCalls++
	if e.active {
		e.mu.Unlock()
		return hostexec.Status{}, hostexec.ErrBusy
	}
	if e.launchErr != nil {
		err := e.launchErr
		e.mu.Unlock()
		return hostexec.Status{}, err
	}
	e.active = true
	gate := e.launchGate
	started := e.started
	e.mu.Unlock()
	if started != nil {
		e.startOnce.Do(func() { close(started) })
	}
	if gate != nil {
		<-gate
	}
	return hostexec.Status{State: hostexec.Active}, nil
}

func (e *twoNodeHostExecutor) Stop(context.Context) error {
	e.mu.Lock()
	e.stopCalls++
	e.active = false
	e.mu.Unlock()
	return nil
}

func (e *twoNodeHostExecutor) Status(context.Context) (hostexec.Status, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.active {
		return hostexec.Status{State: hostexec.Active}, nil
	}
	return hostexec.Status{State: hostexec.Idle}, nil
}

func (e *twoNodeHostExecutor) counts() (launches, stops int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.launchCalls, e.stopCalls
}

func launchTwoNodeKit(t *testing.T, f *twoNodeFixture) {
	t.Helper()
	if _, err := f.service.LaunchOn(context.Background(), f.kitGame.GameID, "kit", nil); err != nil {
		t.Fatalf("LaunchOn kit: %v", err)
	}
}

type twoNodeFixture struct {
	service  *Service
	kit      *packageLibraryClient
	host     *twoNodeHostExecutor
	kitGame  catalog.CoreEntry
	software catalog.Game
}

func newTwoNodeFixture(t *testing.T) *twoNodeFixture {
	t.Helper()
	// These six cases leave host media disabled, so RetroArch uses the runner's
	// local display and input independently of the kit's FPGA play.
	ctx := context.Background()
	rootDir := t.TempDir()
	libraryDir := filepath.Join(rootDir, "library")
	if err := os.MkdirAll(libraryDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(libraryDir, "software.sfc"), []byte("rom"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := catalog.Root{ID: "two-node-library", System: protocol.SystemSNES, Path: libraryDir}
	store, err := catalog.OpenContext(ctx, filepath.Join(rootDir, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	scan, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scan.Observe(ctx, catalog.Candidate{
		ID: twoNodeSoftwareGameID, System: protocol.SystemSNES, RelativePath: "software.sfc", Title: "Software title",
		Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable,
		Fingerprint: catalog.Fingerprint{SourceSize: 3, ModifiedNS: 1},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := scan.Complete(ctx); err != nil {
		t.Fatal(err)
	}
	software, err := store.Game(ctx, twoNodeSoftwareGameID)
	if err != nil {
		t.Fatal(err)
	}

	baseClient := &packageLibraryClient{fakeServiceClient: &fakeServiceClient{
		statusResult: protocol.Status{State: protocol.StateIdle},
		stopResult:   protocol.Status{State: protocol.StateIdle},
	}}
	host := &twoNodeHostExecutor{}
	kitConfig := TargetConfig{Name: "kit", TargetID: "kit", Enabled: true}
	identity := protocol.ContentIdentity{SHA256: serviceDigest, Size: 3, Extension: "sfc"}
	preparer := &fakeServicePreparer{prepare: func(context.Context, catalog.Root, catalog.Game) (*romsource.Prepared, error) {
		return preparedServiceFixture(t, []byte("rom"), identity), nil
	}}
	service := newService(Config{
		Libraries: []catalog.Root{root}, Targets: []TargetConfig{kitConfig}, SelectedTarget: "kit",
		RequestTimeout: time.Second, UploadTimeout: 2 * time.Second,
	}, Paths{Staging: filepath.Join(rootDir, "staging")}, store, &fakeServiceScanner{}, preparer,
		baseClient, WithExecutionPolicy(ExecutionPolicy{
			Host: host,
			Resolver: ExecutionResolverFunc(func(_ context.Context, game catalog.Game) (string, error) {
				if game.ID == twoNodeSoftwareGameID {
					return ExecutionHostOnly, nil
				}
				return ExecutionFPGANative, nil
			}),
		}),
	)
	// The constructor normally dials configured clients; attach the fake kit
	// here because this test must exercise the service without a real target.
	service.targetClients["kit"] = baseClient
	service.pairedTargetClients["kit"] = baseClient
	service.corePackages, err = corepackage.NewStore(filepath.Join(rootDir, "packages"))
	if err != nil {
		t.Fatal(err)
	}
	raw := libraryPackageFixture(t, "0.1.0")
	inspection, created, err := service.ImportCorePackage(ctx, int64(len(raw)), bytes.NewReader(raw))
	if err != nil || !created {
		t.Fatalf("ImportCorePackage: created=%v err=%v", created, err)
	}
	baseClient.inspection = protocol.CoreInspection{PackageID: inspection.PackageID, Descriptor: inspection.Descriptor, Compatible: true}
	kitGame, err := service.CreateCoreEntry(ctx, "Kit Pong", inspection.PackageID)
	if err != nil {
		t.Fatal(err)
	}
	active := coreEntryActiveStatus(inspection, 9, false)
	baseClient.coreLoad = func(context.Context, int64, io.Reader) (protocol.Status, error) {
		baseClient.statusResult = active
		return active, nil
	}
	return &twoNodeFixture{service: service, kit: baseClient, host: host, kitGame: kitGame, software: software}
}

func TestTwoNodeSimultaneousPlay(t *testing.T) {
	f := newTwoNodeFixture(t)
	launchTwoNodeKit(t, f)
	if _, err := f.service.Launch(context.Background(), twoNodeSoftwareGameID, nil); err != nil {
		t.Fatalf("Launch host-only game: %v", err)
	}
	kitLaunches, kitStops := f.kit.coreCalls, f.kit.stopCalls
	hostLaunches, hostStops := f.host.counts()
	if kitLaunches != 1 || kitStops != 0 || hostLaunches != 1 || hostStops != 0 {
		t.Fatalf("kit launches/stops=%d/%d host launches/stops=%d/%d, want 1/0 and 1/0", kitLaunches, kitStops, hostLaunches, hostStops)
	}
	plays := f.service.PlaySessions()
	if len(plays) != 1 || plays[0].Target != "kit" || plays[0].GameID != f.kitGame.GameID || plays[0].Execution != ExecutionFPGANative {
		t.Fatalf("PlaySessions after host launch = %+v, want the kit play retained", plays)
	}
	kitStatus, err := f.service.StatusTarget(context.Background(), "kit")
	if err != nil || kitStatus.State != protocol.StateActive {
		t.Fatalf("kit StatusTarget = %+v, err=%v", kitStatus, err)
	}
	hostStatus, err := f.service.Status(context.Background())
	if err != nil || hostStatus.State != protocol.StateActive || hostStatus.GameID == nil || *hostStatus.GameID != twoNodeSoftwareGameID {
		t.Fatalf("root Status = %+v, err=%v", hostStatus, err)
	}
}

func TestTwoNodeHostFirstThenKitKeepsBoth(t *testing.T) {
	f := newTwoNodeFixture(t)
	if _, err := f.service.Launch(context.Background(), twoNodeSoftwareGameID, nil); err != nil {
		t.Fatalf("Launch host-only game: %v", err)
	}
	launchTwoNodeKit(t, f)
	_, hostStops := f.host.counts()
	if hostStops != 0 {
		t.Fatalf("host stop calls after kit launch=%d, want 0", hostStops)
	}
	if plays := f.service.PlaySessions(); len(plays) != 1 || plays[0].Target != "kit" || plays[0].GameID != f.kitGame.GameID {
		t.Fatalf("PlaySessions = %+v, want kit play retained", plays)
	}
	rootStatus, err := f.service.Status(context.Background())
	if err != nil || rootStatus.State != protocol.StateActive || rootStatus.GameID == nil || *rootStatus.GameID != twoNodeSoftwareGameID {
		t.Fatalf("root Status = %+v, err=%v", rootStatus, err)
	}
	kitStatus, err := f.service.StatusTarget(context.Background(), "kit")
	if err != nil || kitStatus.State != protocol.StateActive || kitStatus.GameID == nil || *kitStatus.GameID != f.kitGame.GameID {
		t.Fatalf("kit StatusTarget = %+v, err=%v", kitStatus, err)
	}
	if _, err := f.service.StopTarget(context.Background(), "kit"); err != nil {
		t.Fatalf("StopTarget kit: %v", err)
	}
	if _, err := f.service.Stop(context.Background()); err != nil {
		t.Fatalf("Stop host-only: %v", err)
	}
	_, hostStops = f.host.counts()
	if f.kit.stopCalls != 1 || hostStops != 1 {
		t.Fatalf("kit/host stop calls=%d/%d, want 1/1", f.kit.stopCalls, hostStops)
	}
}

func TestTwoNodeCastModeKitLaunchStopsHostOnly(t *testing.T) {
	f := newTwoNodeFixture(t)
	f.service.hostCastClaimsKitDisplay = hostCastClaimsKitDisplay(Config{Media: MediaConfig{Enabled: true, Decoder: "none"}})
	if _, err := f.service.Launch(context.Background(), twoNodeSoftwareGameID, nil); err != nil {
		t.Fatalf("Launch host-only game: %v", err)
	}
	launchTwoNodeKit(t, f)
	_, hostStops := f.host.counts()
	if hostStops != 1 {
		t.Fatalf("host stop calls after kit launch=%d, want 1", hostStops)
	}
	if plays := f.service.PlaySessions(); len(plays) != 1 || plays[0].Target != "kit" || plays[0].GameID != f.kitGame.GameID {
		t.Fatalf("PlaySessions = %+v, want active kit play", plays)
	}
}

func TestTwoNodeHostMediaOnSameKitReplacesKitPlay(t *testing.T) {
	f := newTwoNodeFixture(t)
	// Set the minimal service configuration state read by LaunchOn; constructing
	// a real capture sender would require media sockets unrelated to this path.
	f.service.hostCastClaimsKitDisplay = hostCastClaimsKitDisplay(Config{Media: MediaConfig{Enabled: true, Decoder: "none"}})
	launchTwoNodeKit(t, f)
	if _, err := f.service.Launch(context.Background(), twoNodeSoftwareGameID, nil); err != nil {
		t.Fatalf("Launch host-only game with host media: %v", err)
	}
	if f.kit.stopCalls != 1 {
		t.Fatalf("kit stop calls=%d, want package play replaced once", f.kit.stopCalls)
	}
	for _, play := range f.service.PlaySessions() {
		if play.Target == "kit" && play.GameID == f.kitGame.GameID {
			t.Fatalf("kit package play remained after host cast took its display: %+v", play)
		}
	}
	if status, err := f.service.Status(context.Background()); err != nil || status.State != protocol.StateActive || status.GameID == nil || *status.GameID != twoNodeSoftwareGameID {
		t.Fatalf("host-only session after kit replacement = %+v, err=%v", status, err)
	}
}

func TestHostCastClaimsKitDisplayFromConfig(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		want   bool
	}{
		{name: "media disabled", config: Config{Media: MediaConfig{Decoder: "none"}}, want: false},
		{name: "mjpeg preview", config: Config{Media: MediaConfig{Enabled: true, Decoder: "mjpeg"}}, want: false},
		{name: "ffplay local playback", config: Config{Media: MediaConfig{Enabled: true, Decoder: "ffplay"}}, want: false},
		{name: "managed sender", config: Config{Media: MediaConfig{Enabled: true, Decoder: "none"}}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hostCastClaimsKitDisplay(tt.config); got != tt.want {
				t.Fatalf("hostCastClaimsKitDisplay()=%v, want %v", got, tt.want)
			}
		})
	}
}

func TestTwoNodeScopedStop(t *testing.T) {
	f := newTwoNodeFixture(t)
	launchTwoNodeKit(t, f)
	if _, err := f.service.Launch(context.Background(), twoNodeSoftwareGameID, nil); err != nil {
		t.Fatalf("Launch host-only game: %v", err)
	}
	if _, err := f.service.StopTarget(context.Background(), "missing"); err == nil {
		t.Fatal("StopTarget of unknown target succeeded")
	}
	hostLaunches, hostStops := f.host.counts()
	if f.kit.stopCalls != 0 || hostLaunches != 1 || hostStops != 0 {
		t.Fatalf("unknown-target stop changed state: kit stops=%d host launches/stops=%d/%d", f.kit.stopCalls, hostLaunches, hostStops)
	}
	if _, err := f.service.StopTarget(context.Background(), "kit"); err != nil {
		t.Fatalf("StopTarget kit: %v", err)
	}
	if f.kit.stopCalls != 1 {
		t.Fatalf("kit stop calls=%d, want 1", f.kit.stopCalls)
	}
	if _, hostStops = f.host.counts(); hostStops != 0 {
		t.Fatalf("kit Stop stopped host executor %d times", hostStops)
	}
	hostStatus, err := f.service.Status(context.Background())
	if err != nil || hostStatus.State != protocol.StateActive || hostStatus.GameID == nil || *hostStatus.GameID != twoNodeSoftwareGameID {
		t.Fatalf("host status after kit Stop = %+v, err=%v", hostStatus, err)
	}
	if _, err := f.service.Stop(context.Background()); err != nil {
		t.Fatalf("unscoped Stop: %v", err)
	}
	if _, hostStops = f.host.counts(); hostStops != 1 || f.kit.stopCalls != 1 {
		t.Fatalf("final stop counts kit=%d host=%d, want 1/1", f.kit.stopCalls, hostStops)
	}
}

func TestTwoNodeHostOnlyBusyKeepsKit(t *testing.T) {
	f := newTwoNodeFixture(t)
	launchTwoNodeKit(t, f)
	if _, err := f.service.Launch(context.Background(), twoNodeSoftwareGameID, nil); err != nil {
		t.Fatalf("first host-only Launch: %v", err)
	}
	playsBefore := f.service.PlaySessions()
	_, err := f.service.Launch(context.Background(), twoNodeSoftwareGameID, nil)
	assertServiceErrorCode(t, err, protocol.CodeBusy)
	if f.kit.stopCalls != 0 || f.kit.coreCalls != 1 {
		t.Fatalf("second host launch changed kit: loads=%d stops=%d", f.kit.coreCalls, f.kit.stopCalls)
	}
	if got := f.service.PlaySessions(); len(got) != len(playsBefore) || got[0] != playsBefore[0] {
		t.Fatalf("host busy changed kit sessions: before=%+v after=%+v", playsBefore, got)
	}
	if status, err := f.service.Status(context.Background()); err != nil || status.State != protocol.StateActive || status.GameID == nil || *status.GameID != twoNodeSoftwareGameID {
		t.Fatalf("first host session after BUSY = %+v, err=%v", status, err)
	}

	// The host executor pauses the first process start so both public Launch
	// calls are outstanding together. Service lifecycle admission still
	// serializes the mutations; the later launch must see the active executor.
	f2 := newTwoNodeFixture(t)
	gate := make(chan struct{})
	f2.host.mu.Lock()
	f2.host.launchGate = gate
	f2.host.started = make(chan struct{})
	f2.host.mu.Unlock()
	type launchResult struct{ err error }
	results := make(chan launchResult, 2)
	go func() {
		_, err := f2.service.Launch(context.Background(), twoNodeSoftwareGameID, nil)
		results <- launchResult{err: err}
	}()
	select {
	case <-f2.host.started:
	case <-time.After(2 * time.Second):
		t.Fatal("first concurrent launch did not reach host executor")
	}
	secondStarted := make(chan struct{})
	go func() {
		close(secondStarted)
		_, err := f2.service.Launch(context.Background(), twoNodeSoftwareGameID, nil)
		results <- launchResult{err: err}
	}()
	<-secondStarted
	close(gate)
	first, second := <-results, <-results
	successes, busy := 0, 0
	for _, result := range []launchResult{first, second} {
		if result.err == nil {
			successes++
		} else {
			assertServiceErrorCode(t, result.err, protocol.CodeBusy)
			busy++
		}
	}
	if successes != 1 || busy != 1 {
		t.Fatalf("concurrent host launches: successes=%d BUSY=%d", successes, busy)
	}
	if launches, stops := f2.host.counts(); launches != 2 || stops != 0 {
		t.Fatalf("concurrent executor calls launches/stops=%d/%d, want 2/0", launches, stops)
	}
}

func TestTwoNodeKitUnavailableKeepsHostOnly(t *testing.T) {
	f := newTwoNodeFixture(t)
	launchTwoNodeKit(t, f)
	if _, err := f.service.Launch(context.Background(), twoNodeSoftwareGameID, nil); err != nil {
		t.Fatalf("Launch host-only game: %v", err)
	}
	f.kit.statusErr = errors.New("target transport unavailable")
	_, err := f.service.StatusTarget(context.Background(), "kit")
	assertServiceErrorCode(t, err, protocol.CodeMiSTerUnavailable)
	if status, err := f.service.Status(context.Background()); err != nil || status.State != protocol.StateActive || status.GameID == nil || *status.GameID != twoNodeSoftwareGameID {
		t.Fatalf("root status with unavailable kit = %+v, err=%v", status, err)
	}
	f.kit.statusErr = nil
	status, err := f.service.StatusTarget(context.Background(), "kit")
	if err != nil || status.State != protocol.StateActive {
		t.Fatalf("restored kit status = %+v, err=%v", status, err)
	}
	if f.kit.coreCalls != 1 || f.kit.stopCalls != 0 {
		t.Fatalf("kit loads/stops=%d/%d after status recovery, want 1/0", f.kit.coreCalls, f.kit.stopCalls)
	}
}

func TestTwoNodeKitBusyLeaseRefusesOnlyKit(t *testing.T) {
	f := newTwoNodeFixture(t)
	// The public lease observer is network backed; set its observed busy state
	// directly to model the target client's foreign-lease refusal.
	f.service.connection = TargetConnection{State: "busy", Owner: "foreign-shell"}
	_, err := f.service.LaunchOn(context.Background(), f.kitGame.GameID, "kit", nil)
	assertServiceErrorCode(t, err, protocol.CodeKitLeaseDenied)
	if f.kit.coreCalls != 0 || f.kit.stopCalls != 0 {
		t.Fatalf("foreign kit launch contacted target: core loads=%d stops=%d", f.kit.coreCalls, f.kit.stopCalls)
	}
	if _, err := f.service.Launch(context.Background(), twoNodeSoftwareGameID, nil); err != nil {
		t.Fatalf("host-only launch with foreign kit lease: %v", err)
	}
	if launches, stops := f.host.counts(); launches != 1 || stops != 0 || f.kit.stopCalls != 0 {
		t.Fatalf("foreign kit lease affected host/kit: host launches/stops=%d/%d kit stops=%d", launches, stops, f.kit.stopCalls)
	}
}

func TestTwoNodeHostExecutorUnavailableKeepsKit(t *testing.T) {
	f := newTwoNodeFixture(t)
	launchTwoNodeKit(t, f)
	playsBefore := f.service.PlaySessions()
	f.host.mu.Lock()
	f.host.launchErr = hostexec.ErrUnavailable
	f.host.mu.Unlock()
	_, err := f.service.Launch(context.Background(), twoNodeSoftwareGameID, nil)
	assertServiceErrorCode(t, err, protocol.CodeUnavailable)
	if f.kit.stopCalls != 0 || f.kit.coreCalls != 1 {
		t.Fatalf("unavailable host launch changed kit: loads=%d stops=%d", f.kit.coreCalls, f.kit.stopCalls)
	}
	if got := f.service.PlaySessions(); len(got) != len(playsBefore) || got[0] != playsBefore[0] {
		t.Fatalf("unavailable host launch changed kit sessions: before=%+v after=%+v", playsBefore, got)
	}
}
