package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
)

func TestKitLeaseCleanupFreesWhenRuntimeSocketStaysClosed(t *testing.T) {
	err := kitLeaseCleanup(context.Background(), func(context.Context) bool { return false }, time.Millisecond,
		func(context.Context) error { return nil },
		func(context.Context) (protocol.Status, *protocol.APIError) {
			t.Fatal("stop called while runtime socket was closed")
			return protocol.Status{}, nil
		})
	if !errors.Is(err, kitlease.ErrRuntimeUnreachable) {
		t.Fatalf("cleanup = %v", err)
	}
}

func TestKitLeaseCleanupKeepsPeripheralFailureWhenSocketIsClosed(t *testing.T) {
	err := kitLeaseCleanup(context.Background(), func(context.Context) bool { return false }, time.Millisecond,
		func(context.Context) error { return errors.New("input still held") },
		func(context.Context) (protocol.Status, *protocol.APIError) {
			t.Fatal("stop called while runtime socket was closed")
			return protocol.Status{}, nil
		})
	if err == nil || errors.Is(err, kitlease.ErrRuntimeUnreachable) {
		t.Fatalf("cleanup = %v", err)
	}
}

func TestKitLeaseCleanupStopsAfterSocketOpens(t *testing.T) {
	stops := 0
	probes := 0
	err := kitLeaseCleanup(context.Background(), func(context.Context) bool {
		probes++
		return probes >= 2
	}, 200*time.Millisecond,
		func(context.Context) error { return nil },
		func(context.Context) (protocol.Status, *protocol.APIError) {
			stops++
			return protocol.Status{State: protocol.StateIdle}, nil
		})
	if err != nil || stops != 1 || probes < 2 {
		t.Fatalf("cleanup=%v stops=%d probes=%d", err, stops, probes)
	}
}

func TestKitLeaseCleanupBlocksWhenStopDoesNotReachIdle(t *testing.T) {
	err := kitLeaseCleanup(context.Background(), func(context.Context) bool { return true }, time.Millisecond,
		func(context.Context) error { return nil },
		func(context.Context) (protocol.Status, *protocol.APIError) {
			return protocol.Status{State: protocol.StateFailed, Recovery: protocol.RecoveryRebootRequired}, nil
		})
	if err == nil || errors.Is(err, kitlease.ErrRuntimeUnreachable) {
		t.Fatalf("cleanup = %v", err)
	}
}

type scriptedControl struct {
	mu         sync.Mutex
	loads      int
	stops      int
	stopErr    error
	statusErr  error
	loadErr    error
	freedEarly bool
	lease      *kitlease.Manager
	op         context.Context
}

func (c *scriptedControl) Protocol2Status(context.Context) (misterruntime.Protocol2Response, error) {
	c.mu.Lock()
	err := c.statusErr
	c.mu.Unlock()
	if err != nil {
		return misterruntime.Protocol2Response{}, err
	}
	return cleanIdleResponse(), nil
}

func (c *scriptedControl) Protocol2Stop(context.Context) (misterruntime.Protocol2Response, error) {
	if c.lease != nil && c.lease.Status().State == "free" {
		c.mu.Lock()
		c.freedEarly = true
		c.mu.Unlock()
	}
	c.mu.Lock()
	c.stops++
	err := c.stopErr
	c.mu.Unlock()
	if err != nil {
		return misterruntime.Protocol2Response{}, err
	}
	return cleanIdleResponse(), nil
}

func (c *scriptedControl) Protocol2LoadDevelopmentRBF(context.Context, string) (misterruntime.Protocol2Response, error) {
	return misterruntime.Protocol2Response{}, errors.New("unused")
}

func (c *scriptedControl) LoadCore(ctx context.Context, path, packageID string) (misterruntime.Protocol2Response, error) {
	c.mu.Lock()
	c.loads++
	c.op = ctx
	err := c.loadErr
	c.mu.Unlock()
	if err != nil {
		return misterruntime.Protocol2Response{}, err
	}
	if ctx.Err() != nil {
		return misterruntime.Protocol2Response{}, ctx.Err()
	}
	return installedLoadResponse(path, packageID)
}

func (c *scriptedControl) counts() (loads, stops int, freedEarly bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.loads, c.stops, c.freedEarly
}

func (c *scriptedControl) operation() context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.op
}

func cleanIdleResponse() misterruntime.Protocol2Response {
	return misterruntime.Protocol2Response{Protocol: 2, OK: true, State: "idle", Execution: "none", Version: "test"}
}

type kitFixture struct {
	control     *scriptedControl
	runtime     *misterruntime.Runtime
	program     *kitLocalProgram
	native      nativeLocalRuntime
	manager     *kitlease.Manager
	coordinator *agent.Coordinator
}

func newKitFixture(t *testing.T, ttl time.Duration) *kitFixture {
	t.Helper()
	control := &scriptedControl{}
	runtime := misterruntime.NewRuntime(control, filepath.Join(t.TempDir(), "boot-id"), time.Millisecond, time.Second, misterruntime.WithCorePackageRoot(leasePackageStagingRoot(t)))
	coordinator := agent.New(runtime, time.Second, time.Second)
	program := &kitLocalProgram{}
	var manager *kitlease.Manager
	manager = kitlease.New(ttl, func(ctx context.Context) error {
		return kitLeaseCleanup(ctx, nil, time.Millisecond, nil, func(ctx context.Context) (protocol.Status, *protocol.APIError) {
			return stopLeasedRuntime(ctx, coordinator.Stop, runtime, program)
		})
	})
	control.lease = manager
	t.Cleanup(manager.Close)
	waitLease(t, manager, "free")
	return &kitFixture{
		control: control, runtime: runtime, program: program,
		native:      nativeLocalRuntime{runtime: runtime, program: program},
		manager:     manager,
		coordinator: coordinator,
	}
}

func waitLease(t *testing.T, manager *kitlease.Manager, state string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if manager.Status().State == state {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("lease %s, want %s", manager.Status().State, state)
}

func (f *kitFixture) load(t *testing.T) {
	t.Helper()
	if f.coordinator.Status().State != protocol.StateIdle {
		t.Fatalf("coordinator %s", f.coordinator.Status().State)
	}
	root := t.TempDir()
	packages, selections := filepath.Join(root, "pkgs"), filepath.Join(root, "sel")
	if err := os.MkdirAll(packages, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(selections, 0755); err != nil {
		t.Fatal(err)
	}
	id := installPong(t, packages, selections)
	if err := f.native.LoadCore(context.Background(), context.Background(), filepath.Join(packages, id), id); err != nil {
		t.Fatal(err)
	}
	loads, stops, early := f.control.counts()
	if loads != 1 || stops != 0 || early {
		t.Fatalf("after load loads=%d stops=%d early=%v", loads, stops, early)
	}
}

func (f *kitFixture) claim(t *testing.T) {
	t.Helper()
	if _, err := f.manager.Claim(kitlease.ClaimRequest{
		RequestID: strings.Repeat("ab", 32),
		Owner:     "kit-hostless",
		Purpose:   "kit-local-core",
	}); err != nil {
		t.Fatal(err)
	}
}

func (f *kitFixture) takeover(t *testing.T) {
	t.Helper()
	status := f.manager.Status()
	_, err := f.manager.Takeover(kitlease.TakeoverRequest{
		ClaimRequest: kitlease.ClaimRequest{
			RequestID: strings.Repeat("cd", 32),
			Owner:     "fogcast@host",
			Purpose:   "play",
		},
		ExpectedGeneration: status.Generation,
		Reason:             "operator takeover",
	})
	if !errors.Is(err, kitlease.ErrBusy) {
		t.Fatalf("takeover %v", err)
	}
}

func TestKitLocalRevokeStopsRuntimeBeforeLeaseIsFree(t *testing.T) {
	t.Run("takeover", func(t *testing.T) {
		fixture := newKitFixture(t, time.Minute)
		fixture.load(t)
		fixture.claim(t)
		fixture.takeover(t)
		waitLease(t, fixture.manager, "free")
		_, stops, early := fixture.control.counts()
		if stops != 1 || early || fixture.manager.Status().State != "free" {
			t.Fatalf("stops=%d early=%v state=%s", stops, early, fixture.manager.Status().State)
		}
	})
	t.Run("expiry", func(t *testing.T) {
		fixture := newKitFixture(t, 150*time.Millisecond)
		fixture.load(t)
		fixture.claim(t)
		waitLease(t, fixture.manager, "free")
		_, stops, early := fixture.control.counts()
		if stops != 1 || early || fixture.manager.Status().State != "free" {
			t.Fatalf("stops=%d early=%v state=%s", stops, early, fixture.manager.Status().State)
		}
	})
	t.Run("close", func(t *testing.T) {
		fixture := newKitFixture(t, time.Minute)
		fixture.load(t)
		fixture.claim(t)
		fixture.manager.Close()
		_, stops, early := fixture.control.counts()
		// Shutdown does not publish free. Close still has to stop the runtime
		// before it returns, and the lease stays blocked rather than claimable.
		state := fixture.manager.Status().State
		if stops != 1 || early || state == "free" || state == "held" {
			t.Fatalf("stops=%d early=%v state=%s", stops, early, state)
		}
	})
	t.Run("stop failure stays blocked", func(t *testing.T) {
		fixture := newKitFixture(t, time.Minute)
		fixture.load(t)
		fixture.claim(t)
		fixture.control.mu.Lock()
		fixture.control.stopErr = errors.New("stop failed")
		fixture.control.statusErr = errors.New("status failed")
		fixture.control.mu.Unlock()
		fixture.takeover(t)
		waitLease(t, fixture.manager, "blocked")
		_, stops, early := fixture.control.counts()
		if stops != 1 || early || fixture.manager.Status().State == "free" {
			t.Fatalf("stops=%d early=%v state=%s", stops, early, fixture.manager.Status().State)
		}
		fixture.control.mu.Lock()
		fixture.control.stopErr = nil
		fixture.control.statusErr = nil
		fixture.control.mu.Unlock()
		fixture.takeover(t)
		waitLease(t, fixture.manager, "free")
		_, stops, early = fixture.control.counts()
		if stops != 2 || early || fixture.manager.Status().State != "free" {
			t.Fatalf("retry stops=%d early=%v state=%s", stops, early, fixture.manager.Status().State)
		}
	})
	t.Run("no kit-local load does not stop", func(t *testing.T) {
		fixture := newKitFixture(t, time.Minute)
		fixture.claim(t)
		fixture.takeover(t)
		waitLease(t, fixture.manager, "free")
		_, stops, _ := fixture.control.counts()
		if stops != 0 || fixture.manager.Status().State != "free" {
			t.Fatalf("stops=%d state=%s", stops, fixture.manager.Status().State)
		}
	})
}

func TestFailedLoadReleaseStopsRuntimeBeforeLeaseIsFree(t *testing.T) {
	fixture := newKitFixture(t, time.Minute)
	fixture.control.mu.Lock()
	fixture.control.loadErr = errors.New("load failed")
	fixture.control.mu.Unlock()
	root := t.TempDir()
	packages := filepath.Join(root, "pkgs")
	selections := filepath.Join(root, "sel")
	if err := os.Mkdir(packages, 0o755); err != nil || os.Mkdir(selections, 0o755) != nil {
		t.Fatal(err)
	}
	id := installPong(t, packages, selections)
	service := localcores.New(fixture.manager, fixture.native, localcores.Roots{Selections: selections, Packages: packages})
	t.Cleanup(service.Close)
	if _, err := service.Launch(context.Background(), id); err == nil {
		t.Fatal("launch succeeded")
	}
	waitLease(t, fixture.manager, "free")
	loads, stops, early := fixture.control.counts()
	if loads != 1 || stops != 1 || early || fixture.manager.Status().State != "free" {
		t.Fatalf("loads=%d stops=%d early=%v state=%s", loads, stops, early, fixture.manager.Status().State)
	}
}

func TestExplicitStopReleasesWithoutASecondRuntimeStop(t *testing.T) {
	fixture := newKitFixture(t, time.Minute)
	root := t.TempDir()
	packages := filepath.Join(root, "pkgs")
	selections := filepath.Join(root, "sel")
	if err := os.Mkdir(packages, 0o755); err != nil || os.Mkdir(selections, 0o755) != nil {
		t.Fatal(err)
	}
	id := installPong(t, packages, selections)
	service := localcores.New(fixture.manager, fixture.native, localcores.Roots{Selections: selections, Packages: packages})
	t.Cleanup(service.Close)
	if _, err := service.Launch(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if err := service.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitLease(t, fixture.manager, "free")
	loads, stops, early := fixture.control.counts()
	if loads != 1 || stops != 1 || early || fixture.manager.Status().State != "free" {
		t.Fatalf("loads=%d stops=%d early=%v state=%s", loads, stops, early, fixture.manager.Status().State)
	}
}

func TestOwnedLoadKeepsOperationOnItsOwnContext(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	control := &blockingControl{started: started, release: release, once: &once}
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	runtime := misterruntime.NewRuntime(control, filepath.Join(t.TempDir(), "boot-id"), time.Millisecond, time.Second, misterruntime.WithCorePackageRoot(leasePackageStagingRoot(t)))
	native := nativeLocalRuntime{runtime: runtime, program: &kitLocalProgram{}}
	admission, cancel := context.WithCancel(context.Background())
	defer cancel()
	root := t.TempDir()
	packages, selections := filepath.Join(root, "pkgs"), filepath.Join(root, "sel")
	if err := os.MkdirAll(packages, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(selections, 0755); err != nil {
		t.Fatal(err)
	}
	id := installPong(t, packages, selections)
	done := make(chan error, 1)
	go func() {
		done <- native.LoadCore(admission, context.Background(), filepath.Join(packages, id), id)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("load did not start")
	}
	cancel()
	time.Sleep(30 * time.Millisecond)
	if control.operation().Err() != nil {
		t.Fatalf("client cancel aborted load: %v", control.operation().Err())
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !native.program.active() {
		t.Fatal("successful load did not record a kit-local program")
	}
}

type blockingControl struct {
	started chan struct{}
	release chan struct{}
	once    *sync.Once
	mu      sync.Mutex
	op      context.Context
}

func (c *blockingControl) Protocol2Status(context.Context) (misterruntime.Protocol2Response, error) {
	return cleanIdleResponse(), nil
}

func (c *blockingControl) Protocol2Stop(context.Context) (misterruntime.Protocol2Response, error) {
	return misterruntime.Protocol2Response{}, errors.New("unused")
}

func (c *blockingControl) Protocol2LoadDevelopmentRBF(context.Context, string) (misterruntime.Protocol2Response, error) {
	return misterruntime.Protocol2Response{}, errors.New("unused")
}

func (c *blockingControl) LoadCore(ctx context.Context, path, packageID string) (misterruntime.Protocol2Response, error) {
	c.mu.Lock()
	c.op = ctx
	c.mu.Unlock()
	c.once.Do(func() { close(c.started) })
	<-c.release
	if ctx.Err() != nil {
		return misterruntime.Protocol2Response{}, ctx.Err()
	}
	return installedLoadResponse(path, packageID)
}

func (c *blockingControl) operation() context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.op
}

func installPong(t *testing.T, packages, selections string) string {
	t.Helper()
	dir := filepath.Join(packages, "incoming")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	fixtureRoot := filepath.Join("..", "..", "corepackage", "testdata", "core-bundle-v2")
	for _, item := range []struct{ source, target string }{{"manifests/valid-basic.toml", "manifest.toml"}, {"payloads/fes-fixture.rbf", "core.rbf"}} {
		data, err := os.ReadFile(filepath.Join(fixtureRoot, item.source))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, item.target), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	inspection, err := corepackage.InspectPackage(dir)
	if err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(packages, inspection.PackageID)
	if err := os.Rename(dir, installed); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("core_id = %q\npackage_id = %q\npayload_sha256 = %q\ninstall_path = %q\n", inspection.Descriptor.Core.ID, inspection.PackageID, inspection.Descriptor.Payload.SHA256, installed)
	if err := os.WriteFile(filepath.Join(selections, "fes-pong.package.toml"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return inspection.PackageID
}

func (c *scriptedControl) InspectCore(_ context.Context, path, packageID string) (misterruntime.Protocol2Response, error) {
	return installedInspectionResponse(path, packageID)
}

func (c *blockingControl) InspectCore(_ context.Context, path, packageID string) (misterruntime.Protocol2Response, error) {
	return installedInspectionResponse(path, packageID)
}

func installedInspectionResponse(path, packageID string) (misterruntime.Protocol2Response, error) {
	inspection, err := corepackage.InspectPackage(path)
	if err != nil {
		return misterruntime.Protocol2Response{}, err
	}
	if inspection.PackageID != packageID {
		return misterruntime.Protocol2Response{}, errors.New("staged package identity mismatch")
	}
	return misterruntime.Protocol2Response{Protocol: 2, OK: true, State: "idle", Execution: "none", Version: "test",
		Capabilities:     installedControlCapabilities(inspection.Descriptor, false),
		InspectedPackage: &misterruntime.Protocol2Inspection{PackageID: packageID, Descriptor: inspection.Descriptor, Compatible: true}}, nil
}

func installedLoadResponse(path, packageID string) (misterruntime.Protocol2Response, error) {
	inspection, err := corepackage.InspectPackage(path)
	if err != nil {
		return misterruntime.Protocol2Response{}, err
	}
	if inspection.PackageID != packageID {
		return misterruntime.Protocol2Response{}, errors.New("staged load identity mismatch")
	}
	generation := uint64(1)
	buildID := inspection.Descriptor.Build.ID
	observedABI := misterruntime.Protocol2Contract{ID: inspection.Descriptor.ABI.ID, Major: uint16(inspection.Descriptor.ABI.Major), Minor: uint16(inspection.Descriptor.ABI.Minor)}
	return misterruntime.Protocol2Response{Protocol: 2, OK: true, State: "running_development", Execution: "development", Version: "test", Generation: &generation,
		Capabilities:  installedControlCapabilities(inspection.Descriptor, true),
		ActivePackage: &misterruntime.Protocol2ActivePackage{PackageID: packageID, Descriptor: inspection.Descriptor, PersistenceMode: "volatile", Observed: misterruntime.Protocol2Observed{ABI: &observedABI, BuildID: &buildID}}}, nil
}

func leasePackageStagingRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Cleanup(func() {
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err == nil && entry.IsDir() {
				return os.Chmod(path, 0700)
			}
			return nil
		})
	})
	return root
}

func installedControlCapabilities(descriptor corepackage.Descriptor, active bool) misterruntime.Protocol2Capabilities {
	interfaces := make([]misterruntime.Protocol2Interface, 0, len(descriptor.Interfaces))
	for _, iface := range descriptor.Interfaces {
		interfaces = append(interfaces, misterruntime.Protocol2Interface{ID: iface.ID, Major: uint16(iface.Major), Minor: uint16(iface.Minor)})
	}
	capabilities := misterruntime.Protocol2Capabilities{ProgrammingProfiles: []string{descriptor.Target.ProgrammingProfile}, ABIs: []misterruntime.Protocol2ABI{{ID: descriptor.ABI.ID, Major: uint16(descriptor.ABI.Major), Minor: uint16(descriptor.ABI.Minor), Interfaces: interfaces}}, ActiveInterfaces: []misterruntime.Protocol2Interface{}}
	if active {
		capabilities.ActiveInterfaces = interfaces
	}
	return capabilities
}
