package misterruntime_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/corepackage"

	"github.com/DeanoC/FogCast/internal/misterruntime"
)

type packageControl struct {
	mu                    sync.Mutex
	generation            uint64
	loadCalls             int
	loadErr               error
	remoteErr             *misterruntime.Protocol2Error
	beforeReply           func(string)
	activePath            string
	activeID              string
	status2               *misterruntime.Protocol2Response
	status2Err            error
	status2ErrAfterLoad   error
	status2Calls          int
	blockFirstObservation bool
	lostReply             bool
	preserveOnLost        bool
}

func (*packageControl) Status(context.Context) (misterruntime.Response, error) {
	return misterruntime.Response{Protocol: 1, OK: true, State: "idle", Execution: "none", Version: "test"}, nil
}
func (*packageControl) Launch(context.Context, misterruntime.LaunchRequest) (misterruntime.Response, error) {
	return misterruntime.Response{}, errors.New("unexpected launch")
}
func (*packageControl) LoadDevelopmentRBF(context.Context, string) (misterruntime.Response, error) {
	return misterruntime.Response{}, errors.New("unexpected raw load")
}
func (*packageControl) Stop(context.Context) (misterruntime.Response, error) {
	return misterruntime.Response{Protocol: 1, OK: true, State: "idle", Execution: "none", Version: "test"}, nil
}
func (c *packageControl) Protocol2Status(ctx context.Context) (misterruntime.Protocol2Response, error) {
	return c.protocol2Status(ctx)
}

func (c *packageControl) protocol2Status(ctx context.Context) (misterruntime.Protocol2Response, error) {
	c.mu.Lock()
	c.status2Calls++
	call := c.status2Calls
	loadCalls := c.loadCalls
	block := c.blockFirstObservation && call == 2
	statusErr := c.status2Err
	if loadCalls > 0 && c.status2ErrAfterLoad != nil {
		statusErr = c.status2ErrAfterLoad
	}
	status := c.status2
	c.mu.Unlock()
	if block {
		<-ctx.Done()
		return misterruntime.Protocol2Response{}, ctx.Err()
	}
	if statusErr != nil {
		return misterruntime.Protocol2Response{}, statusErr
	}
	if status != nil {
		return *status, nil
	}
	return misterruntime.Protocol2Response{Protocol: 2, OK: true, State: "idle", Execution: "none", Version: "test"}, nil
}
func (c *packageControl) LoadCore(_ context.Context, path, packageID string) (misterruntime.Protocol2Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadCalls++
	if c.loadErr != nil {
		return misterruntime.Protocol2Response{}, c.loadErr
	}
	inspection, err := corepackageInspection(path)
	if err != nil {
		return misterruntime.Protocol2Response{}, err
	}
	if c.beforeReply != nil {
		c.beforeReply(path)
	}
	if c.lostReply && c.preserveOnLost {
		return misterruntime.Protocol2Response{}, io.EOF
	}
	if c.remoteErr != nil {
		return misterruntime.Protocol2Response{Protocol: 2, OK: false, State: "idle", Execution: "none", Version: "test", Error: c.remoteErr}, nil
	}
	c.generation++
	c.activePath, c.activeID = path, packageID
	generation := c.generation
	buildID := inspection.Descriptor.Build.ID
	response := misterruntime.Protocol2Response{
		Protocol: 2, OK: true, State: "running_development", Execution: "development", Version: "test",
		Core: stringPointer(inspection.Descriptor.Core.ID), Generation: &generation,
		Capabilities: misterruntime.Protocol2Capabilities{ActiveInterfaces: []misterruntime.Protocol2Interface{
			{ID: "fes.gamepad", Major: 1}, {ID: "fes.video.fixed-720p60", Major: 1},
		}},
		ActivePackage: &misterruntime.Protocol2ActivePackage{
			PackageID: packageID, Descriptor: inspection.Descriptor,
			Observed: misterruntime.Protocol2Observed{
				ABI: &misterruntime.Protocol2Contract{ID: "fes.simple-game", Major: 1}, BuildID: &buildID,
			},
		},
	}
	c.status2 = &response
	if c.lostReply {
		return misterruntime.Protocol2Response{}, io.EOF
	}
	return response, nil
}

func TestCorePackageDispatchAndExplicitPreflightClassifications(t *testing.T) {
	archive := canonicalCoreArchive(t)
	for _, test := range []struct {
		name               string
		loadErr            error
		statusErrAfterLoad error
		remoteErr          *misterruntime.Protocol2Error
		attempted          bool
	}{
		{name: "alternate transport is ambiguous", loadErr: io.EOF, statusErrAfterLoad: io.EOF, attempted: true},
		{name: "busy is preflight", remoteErr: &misterruntime.Protocol2Error{Code: "busy", Message: "busy", Phase: "lifecycle"}},
		{name: "save is preflight", remoteErr: &misterruntime.Protocol2Error{Code: "save_failed", Message: "save", Phase: "save"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			control := &packageControl{loadErr: test.loadErr, remoteErr: test.remoteErr, status2ErrAfterLoad: test.statusErrAfterLoad}
			runtime := misterruntime.NewRuntime(control, "", 0, 0, misterruntime.WithCorePackageRoot(t.TempDir()))
			t.Cleanup(func() {
				control.loadErr = nil
				control.remoteErr = nil
				control.status2ErrAfterLoad = nil
				_, _ = runtime.Stop(context.Background())
			})
			_, attempted, apiErr := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(archive)), bytes.NewReader(archive))
			if apiErr == nil || attempted != test.attempted {
				t.Fatalf("attempted=%t want=%t error=%#v", attempted, test.attempted, apiErr)
			}
		})
	}
}

func TestCorePackageFailedRejectedCleanupIsRetainedForStopRetry(t *testing.T) {
	root := t.TempDir()
	moved := root + "-moved"
	archive := canonicalCoreArchive(t)
	var publication string
	control := &packageControl{remoteErr: &misterruntime.Protocol2Error{
		Code: "unsupported_interface", Message: "missing input", Phase: "compatibility",
	}}
	control.beforeReply = func(path string) {
		publication = filepath.Base(path)
		if err := os.Rename(root, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	runtime := misterruntime.NewRuntime(control, "", 0, 0, misterruntime.WithCorePackageRoot(root))
	_, attempted, apiErr := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(archive)), bytes.NewReader(archive))
	if apiErr == nil || attempted {
		t.Fatalf("attempted=%t error=%#v", attempted, apiErr)
	}
	if _, err := os.Stat(filepath.Join(moved, publication)); err != nil {
		t.Fatalf("rejected publication was not retained: %v", err)
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(moved, root); err != nil {
		t.Fatal(err)
	}
	control.beforeReply = nil
	control.remoteErr = nil
	if _, stopErr := runtime.Stop(context.Background()); stopErr != nil {
		t.Fatal(stopErr)
	}
	if _, err := os.Stat(filepath.Join(root, publication)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retained publication remains after Stop: %v", err)
	}
}

func TestCorePackageLostReplyIsReconciledWithoutReplay(t *testing.T) {
	archive := canonicalCoreArchive(t)
	t.Run("confirmed requested package", func(t *testing.T) {
		control := &packageControl{lostReply: true}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 20*time.Millisecond,
			misterruntime.WithCorePackageRoot(t.TempDir()))
		activation, attempted, apiErr := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(archive)), bytes.NewReader(archive))
		if apiErr != nil || !attempted || activation.PackageID == "" || activation.Generation != 1 {
			t.Fatalf("activation=%#v attempted=%t error=%#v", activation, attempted, apiErr)
		}
		if control.loadCalls != 1 {
			t.Fatalf("load calls=%d", control.loadCalls)
		}
		control.lostReply = false
		_, _ = runtime.Stop(context.Background())
	})

	t.Run("confirmed prior package", func(t *testing.T) {
		control := &packageControl{}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 20*time.Millisecond,
			misterruntime.WithCorePackageRoot(t.TempDir()))
		if _, _, apiErr := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(archive)), bytes.NewReader(archive)); apiErr != nil {
			t.Fatal(apiErr)
		}
		priorPath := control.activePath
		control.lostReply, control.preserveOnLost = true, true
		replacement := alternateCoreArchive(t)
		_, attempted, apiErr := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(replacement)), bytes.NewReader(replacement))
		if apiErr == nil || attempted || control.loadCalls != 2 {
			t.Fatalf("attempted=%t calls=%d error=%#v", attempted, control.loadCalls, apiErr)
		}
		if _, err := os.Stat(priorPath); err != nil {
			t.Fatalf("prior package lost: %v", err)
		}
		control.lostReply, control.preserveOnLost = false, false
		_, _ = runtime.Stop(context.Background())
	})

	t.Run("confirmed recovery", func(t *testing.T) {
		control := &packageControl{lostReply: true, preserveOnLost: true}
		recovery := misterruntime.Protocol2Response{Protocol: 2, OK: false, State: "reboot_required", Execution: "none", Version: "test",
			Error: &misterruntime.Protocol2Error{Code: "idle_failed", Message: "reboot", Phase: "recovery"}}
		control.beforeReply = func(string) { control.status2 = &recovery }
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 20*time.Millisecond,
			misterruntime.WithCorePackageRoot(t.TempDir()))
		_, attempted, apiErr := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(archive)), bytes.NewReader(archive))
		if apiErr == nil || !attempted || apiErr.Phase != "recovery" {
			t.Fatalf("attempted=%t error=%#v", attempted, apiErr)
		}
	})

	t.Run("unresolved is retained", func(t *testing.T) {
		root := t.TempDir()
		control := &packageControl{lostReply: true, preserveOnLost: true, status2ErrAfterLoad: io.EOF}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Millisecond,
			misterruntime.WithCorePackageRoot(root))
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer cancel()
		_, attempted, apiErr := runtime.LoadCoreOwned(context.Background(), ctx, context.Background(), int64(len(archive)), bytes.NewReader(archive))
		if apiErr == nil || !attempted {
			t.Fatalf("attempted=%t error=%#v", attempted, apiErr)
		}
		control.lostReply, control.preserveOnLost, control.status2ErrAfterLoad = false, false, nil
		_, _ = runtime.Stop(context.Background())
	})

	t.Run("same package requires a new generation", func(t *testing.T) {
		control := &packageControl{}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 20*time.Millisecond,
			misterruntime.WithCorePackageRoot(t.TempDir()))
		if _, _, apiErr := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(archive)), bytes.NewReader(archive)); apiErr != nil {
			t.Fatal(apiErr)
		}
		control.lostReply = true
		activation, attempted, apiErr := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(archive)), bytes.NewReader(archive))
		if apiErr != nil || !attempted || activation.Generation != 2 {
			t.Fatalf("activation=%#v attempted=%t error=%#v", activation, attempted, apiErr)
		}
		control.lostReply, control.preserveOnLost = true, true
		_, attempted, apiErr = runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(archive)), bytes.NewReader(archive))
		if apiErr == nil || attempted {
			t.Fatalf("same generation attempted=%t error=%#v", attempted, apiErr)
		}
		control.lostReply, control.preserveOnLost = false, false
		_, _ = runtime.Stop(context.Background())
	})

	t.Run("contradictory active package does not preserve prior", func(t *testing.T) {
		control := &packageControl{}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 20*time.Millisecond,
			misterruntime.WithCorePackageRoot(t.TempDir()))
		if _, _, apiErr := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(archive)), bytes.NewReader(archive)); apiErr != nil {
			t.Fatal(apiErr)
		}
		control.lostReply, control.preserveOnLost = true, true
		control.beforeReply = func(string) {
			contradiction := *control.status2
			active := *contradiction.ActivePackage
			active.PackageID = strings.Repeat("c", 64)
			contradiction.ActivePackage = &active
			generation := uint64(99)
			contradiction.Generation = &generation
			control.status2 = &contradiction
		}
		_, attempted, apiErr := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(alternateCoreArchive(t))), bytes.NewReader(alternateCoreArchive(t)))
		if apiErr == nil || !attempted || apiErr.Phase != "recovery" {
			t.Fatalf("attempted=%t error=%#v", attempted, apiErr)
		}
		control.beforeReply = nil
		control.lostReply, control.preserveOnLost = false, false
		_, _ = runtime.Stop(context.Background())
	})

	t.Run("fallback budget begins after observation", func(t *testing.T) {
		control := &packageControl{lostReply: true, blockFirstObservation: true}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 5*time.Millisecond,
			misterruntime.WithCorePackageRoot(t.TempDir()))
		observation, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		activation, attempted, apiErr := runtime.LoadCoreOwned(context.Background(), observation, context.Background(), int64(len(archive)), bytes.NewReader(archive))
		if apiErr != nil || !attempted || activation.Generation != 1 || control.status2Calls < 3 {
			t.Fatalf("activation=%#v attempted=%t calls=%d error=%#v", activation, attempted, control.status2Calls, apiErr)
		}
		control.lostReply = false
		_, _ = runtime.Stop(context.Background())
	})
}

func TestCorePackageReconcilePrefersV2AndAdoptsPrivatePublication(t *testing.T) {
	t.Run("custom package", func(t *testing.T) {
		root := t.TempDir()
		archive := canonicalCoreArchive(t)
		staged, err := corepackage.Stage(context.Background(), root, int64(len(archive)), bytes.NewReader(archive))
		if err != nil {
			t.Fatal(err)
		}
		retiredArchive := alternateCoreArchive(t)
		retired, err := corepackage.Stage(context.Background(), root, int64(len(retiredArchive)), bytes.NewReader(retiredArchive))
		if err != nil {
			t.Fatal(err)
		}
		response := packageStatus(staged, 41, false)
		control := &packageControl{status2: &response}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second, misterruntime.WithCorePackageRoot(root))
		status := runtime.Reconcile(context.Background())
		if status.State != "active" || !status.Development || status.CorePackage == nil ||
			status.CorePackage.PackageID != staged.PackageID || status.CorePackage.Generation != 41 ||
			!status.CorePackage.Gamepad || len(status.CorePackage.ActiveInterfaces) != 2 {
			t.Fatalf("status=%#v", status)
		}
		if _, stopErr := runtime.Stop(context.Background()); stopErr != nil {
			t.Fatal(stopErr)
		}
		if _, err := os.Stat(staged.Directory); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("adopted publication remains: %v", err)
		}
		if _, err := os.Stat(retired.Directory); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("different-ID retired publication remains: %v", err)
		}
	})

	t.Run("mister package", func(t *testing.T) {
		root := t.TempDir()
		archive := misterCoreArchive(t)
		staged, err := corepackage.Stage(context.Background(), root, int64(len(archive)), bytes.NewReader(archive))
		if err != nil {
			t.Fatal(err)
		}
		response := packageStatus(staged, 9, true)
		control := &packageControl{status2: &response}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second, misterruntime.WithCorePackageRoot(root))
		status := runtime.Reconcile(context.Background())
		if status.State != "active" || status.CorePackage == nil || status.CorePackage.Gamepad ||
			status.CorePackage.ABI.ID != "mister" || status.CorePackage.BuildID != staged.Descriptor.Build.ID {
			t.Fatalf("status=%#v", status)
		}
		_, _ = runtime.Stop(context.Background())
	})

	for _, test := range []struct {
		name     string
		response misterruntime.Protocol2Response
		wantIdle bool
	}{
		{name: "raw development", response: misterruntime.Protocol2Response{Protocol: 2, OK: true, State: "running_development", Execution: "development", Version: "test", Generation: uint64Pointer(7)}},
		{name: "idle", response: misterruntime.Protocol2Response{Protocol: 2, OK: true, State: "idle", Execution: "none", Version: "test"}, wantIdle: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			control := &packageControl{status2: &test.response}
			runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second, misterruntime.WithCorePackageRoot(t.TempDir()))
			status := runtime.Reconcile(context.Background())
			if test.wantIdle {
				if status.State != "idle" || status.Development || status.CorePackage != nil {
					t.Fatalf("status=%#v", status)
				}
			} else if status.State != "active" || !status.Development || status.CorePackage != nil {
				t.Fatalf("status=%#v", status)
			}
		})
	}

	t.Run("unavailable v2 does not fall back", func(t *testing.T) {
		control := &packageControl{status2Err: io.EOF}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second, misterruntime.WithCorePackageRoot(t.TempDir()))
		status := runtime.Reconcile(context.Background())
		if status.State != "failed" || status.LastError == nil {
			t.Fatalf("status=%#v", status)
		}
	})

	t.Run("idle retains structured runtime error", func(t *testing.T) {
		expected, observed := "idle", "fabric"
		response := misterruntime.Protocol2Response{Protocol: 2, OK: true, State: "idle", Execution: "none", Version: "test",
			Error: &misterruntime.Protocol2Error{Code: "io_failed", Message: "retained", Phase: "transport", Expected: &expected, Observed: &observed}}
		control := &packageControl{status2: &response}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second, misterruntime.WithCorePackageRoot(t.TempDir()))
		status := runtime.Reconcile(context.Background())
		if status.State != "idle" || status.LastError == nil || status.LastError.Phase != "transport" ||
			status.LastError.Expected != expected || status.LastError.Observed != observed {
			t.Fatalf("status=%#v", status)
		}
	})
}

func packageStatus(staged corepackage.Staged, generation uint64, mister bool) misterruntime.Protocol2Response {
	response := misterruntime.Protocol2Response{Protocol: 2, OK: true, State: "running_development", Execution: "development", Version: "test",
		Core: stringPointer(staged.Descriptor.Core.ID), Generation: &generation,
		ActivePackage: &misterruntime.Protocol2ActivePackage{PackageID: staged.PackageID, Descriptor: staged.Descriptor}}
	if !mister {
		buildID := staged.Descriptor.Build.ID
		response.ActivePackage.Observed = misterruntime.Protocol2Observed{ABI: &misterruntime.Protocol2Contract{ID: staged.Descriptor.ABI.ID, Major: 1}, BuildID: &buildID}
		response.Capabilities.ActiveInterfaces = []misterruntime.Protocol2Interface{{ID: "fes.gamepad", Major: 1}, {ID: "fes.video.fixed-720p60", Major: 1}}
	}
	return response
}

func uint64Pointer(value uint64) *uint64 { return &value }

func TestCorePackageActivationOwnsStagingThroughReplacementAndStop(t *testing.T) {
	root := t.TempDir()
	archive := canonicalCoreArchive(t)
	control := &packageControl{}
	runtime := misterruntime.NewRuntime(control, "", 0, 0, misterruntime.WithCorePackageRoot(root))

	first, attempted, apiErr := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(archive)), bytes.NewReader(archive))
	if apiErr != nil || !attempted || first.PackageID == "" || first.Generation != 1 || !first.Gamepad {
		t.Fatalf("first=%#v attempted=%t err=%v", first, attempted, apiErr)
	}
	firstPath := control.activePath
	if _, err := os.Stat(firstPath); err != nil {
		t.Fatalf("active stage: %v", err)
	}

	second, attempted, apiErr := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(archive)), bytes.NewReader(archive))
	if apiErr != nil || !attempted || second.Generation != 2 || control.activePath == firstPath {
		t.Fatalf("second=%#v attempted=%t err=%v path=%q", second, attempted, apiErr, control.activePath)
	}
	if _, err := os.Stat(firstPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retired stage remains: %v", err)
	}
	secondPath := control.activePath
	if _, apiErr := runtime.Stop(context.Background()); apiErr != nil {
		t.Fatal(apiErr)
	}
	if _, err := os.Stat(secondPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stopped stage remains: %v", err)
	}
}

func TestCorePackagePreMutationFailurePreservesActiveStage(t *testing.T) {
	root := t.TempDir()
	archive := canonicalCoreArchive(t)
	control := &packageControl{}
	runtime := misterruntime.NewRuntime(control, "", 0, 0, misterruntime.WithCorePackageRoot(root))
	t.Cleanup(func() { _, _ = runtime.Stop(context.Background()) })
	if _, _, apiErr := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(archive)), bytes.NewReader(archive)); apiErr != nil {
		t.Fatal(apiErr)
	}
	activePath := control.activePath
	_, attempted, apiErr := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), 3, strings.NewReader("bad"))
	if apiErr == nil || attempted || control.loadCalls != 1 {
		t.Fatalf("attempted=%t calls=%d err=%v", attempted, control.loadCalls, apiErr)
	}
	if _, err := os.Stat(activePath); err != nil {
		t.Fatalf("prior active stage lost: %v", err)
	}
}

func TestCorePackageRemoteErrorPreservesPhaseAndDoesNotReplaceActive(t *testing.T) {
	root := t.TempDir()
	archive := canonicalCoreArchive(t)
	control := &packageControl{}
	runtime := misterruntime.NewRuntime(control, "", 0, 0, misterruntime.WithCorePackageRoot(root))
	t.Cleanup(func() {
		control.remoteErr = nil
		_, _ = runtime.Stop(context.Background())
	})
	if _, _, apiErr := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(archive)), bytes.NewReader(archive)); apiErr != nil {
		t.Fatal(apiErr)
	}
	activePath := control.activePath
	control.remoteErr = &misterruntime.Protocol2Error{Code: "unsupported_interface", Message: "missing input", Phase: "compatibility", Expected: stringPointer("fes.gamepad@1.0")}
	_, attempted, apiErr := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(archive)), bytes.NewReader(archive))
	if apiErr == nil || attempted || apiErr.Phase != "compatibility" || apiErr.Expected != "fes.gamepad@1.0" {
		t.Fatalf("attempted=%t err=%#v", attempted, apiErr)
	}
	if control.activePath != activePath {
		t.Fatalf("active path replaced: %q", control.activePath)
	}
	if _, err := os.Stat(activePath); err != nil {
		t.Fatalf("prior active stage lost: %v", err)
	}
}

func stringPointer(value string) *string { return &value }

func corepackageInspection(path string) (corepackage.Inspection, error) {
	return corepackage.InspectPackage(path)
}

func canonicalCoreArchive(t *testing.T) []byte {
	return coreArchive(t, false)
}

func alternateCoreArchive(t *testing.T) []byte {
	return coreArchive(t, true)
}

func misterCoreArchive(t *testing.T) []byte {
	t.Helper()
	base := filepath.Join("..", "corepackage", "testdata", "core-bundle-v2")
	manifest, err := os.ReadFile(filepath.Join(base, "manifests", "valid-basic.toml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest = bytes.Replace(manifest, []byte(`version = "0.1.0"`), []byte("version = \"0.1.0\"\nsystem = \"pong\""), 1)
	manifest = bytes.Replace(manifest, []byte(`programming_profile = "fes-gp-v1"`), []byte(`programming_profile = "mister-v1"`), 1)
	manifest = bytes.Replace(manifest, []byte(`id = "fes.simple-game"`), []byte(`id = "mister"`), 1)
	start := bytes.Index(manifest, []byte("[[interfaces]]"))
	end := bytes.Index(manifest, []byte("[build]"))
	if start < 0 || end < start {
		t.Fatal("fixture interface section missing")
	}
	manifest = append(append([]byte(nil), manifest[:start]...), manifest[end:]...)
	manifest = bytes.Replace(manifest, []byte("format = 2\n"), []byte("format = 2\ninterfaces = []\n"), 1)
	payload, err := os.ReadFile(filepath.Join(base, "payloads", "fes-fixture.rbf"))
	if err != nil {
		t.Fatal(err)
	}
	return tarCoreArchive(t, manifest, payload)
}

func coreArchive(t *testing.T, alternate bool) []byte {
	t.Helper()
	base := filepath.Join("..", "corepackage", "testdata", "core-bundle-v2")
	manifest, err := os.ReadFile(filepath.Join(base, "manifests", "valid-basic.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if alternate {
		manifest = bytes.Replace(manifest, []byte(`version = "0.1.0"`), []byte(`version = "0.1.1"`), 1)
	}
	payload, err := os.ReadFile(filepath.Join(base, "payloads", "fes-fixture.rbf"))
	if err != nil {
		t.Fatal(err)
	}
	return tarCoreArchive(t, manifest, payload)
}

func tarCoreArchive(t *testing.T, manifest, payload []byte) []byte {
	t.Helper()
	var output bytes.Buffer
	for _, entry := range []struct {
		name string
		data []byte
	}{{"manifest.toml", manifest}, {"core.rbf", payload}} {
		header := make([]byte, 512)
		copy(header, entry.name)
		copy(header[100:108], "0000644\x00")
		copy(header[108:116], "0000000\x00")
		copy(header[116:124], "0000000\x00")
		copy(header[124:136], fmt.Sprintf("%011o\x00", len(entry.data)))
		copy(header[136:148], "00000000000\x00")
		for i := 148; i < 156; i++ {
			header[i] = ' '
		}
		header[156] = '0'
		copy(header[257:263], "ustar\x00")
		copy(header[263:265], "00")
		sum := 0
		for _, value := range header {
			sum += int(value)
		}
		copy(header[148:156], fmt.Sprintf("%06o\x00 ", sum))
		output.Write(header)
		output.Write(entry.data)
		output.Write(make([]byte, (512-len(entry.data)%512)%512))
	}
	output.Write(make([]byte, 1024))
	return output.Bytes()
}
