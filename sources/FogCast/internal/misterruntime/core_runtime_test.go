package misterruntime_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
)

type packageControl struct {
	mu                    sync.Mutex
	generation            uint64
	loadCalls             int
	loadErr               error
	remoteErr             *misterruntime.Protocol2Error
	beforeReply           func(string)
	beforeInspectReply    func(string)
	activePath            string
	activeID              string
	status2               *misterruntime.Protocol2Response
	status2Err            error
	status2Script         []protocol2StatusResult
	status2ErrAfterLoad   error
	status2Calls          int
	legacyStatusCalls     int
	inspectCompatible     *bool
	inspectCompatibility  *misterruntime.Protocol2Error
	inspectCalls          int
	inspectErr            error
	inspectPackageID      string
	inspectDescriptor     *corepackage.Descriptor
	blockFirstObservation bool
	lostReply             bool
	preserveOnLost        bool
}

type protocol2StatusResult struct {
	response misterruntime.Protocol2Response
	err      error
}

type replacementBarrier struct {
	beginErr  error
	finishErr error
	begins    int
	preserve  []bool
}

type unsupportedProtocolError struct{}

func (unsupportedProtocolError) Error() string { return "runtime protocol 2 is unsupported" }
func (unsupportedProtocolError) Is(target error) bool {
	return target != nil && target.Error() == "runtime protocol 2 is unsupported"
}

func (b *replacementBarrier) BeginCoreReplacement(context.Context) (func(context.Context, bool) error, error) {
	b.begins++
	if b.beginErr != nil {
		return nil, b.beginErr
	}
	return func(_ context.Context, preserve bool) error {
		b.preserve = append(b.preserve, preserve)
		return b.finishErr
	}, nil
}

func (c *packageControl) Status(context.Context) (misterruntime.Response, error) {
	c.mu.Lock()
	c.legacyStatusCalls++
	c.mu.Unlock()
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

func (c *packageControl) InspectCore(ctx context.Context, path, packageID string) (misterruntime.Protocol2Response, error) {
	c.inspectCalls++
	if c.inspectErr != nil {
		return misterruntime.Protocol2Response{}, c.inspectErr
	}
	inspection, err := corepackageInspection(path)
	if err != nil {
		return misterruntime.Protocol2Response{}, err
	}
	compatible := true
	if c.inspectCompatible != nil {
		compatible = *c.inspectCompatible
	}
	returnedID := packageID
	if c.inspectPackageID != "" {
		returnedID = c.inspectPackageID
	}
	descriptor := inspection.Descriptor
	if c.inspectDescriptor != nil {
		descriptor = *c.inspectDescriptor
	}
	if c.beforeInspectReply != nil {
		c.beforeInspectReply(path)
	}
	if err := ctx.Err(); err != nil {
		return misterruntime.Protocol2Response{}, err
	}
	return misterruntime.Protocol2Response{
		Protocol: 2, OK: true, State: "idle", Execution: "none", Version: "test",
		InspectedPackage: &misterruntime.Protocol2Inspection{
			PackageID: returnedID, Descriptor: descriptor,
			Compatible: compatible, CompatibilityError: c.inspectCompatibility,
		},
	}, nil
}

func TestCorePackageInspectionUsesRuntimeAuthorityAndCleansStaging(t *testing.T) {
	root := t.TempDir()
	archive := canonicalCoreArchive(t)
	control := &packageControl{}
	barrier := &replacementBarrier{}
	runtime := misterruntime.NewRuntime(control, "", 0, 0,
		misterruntime.WithCorePackageRoot(root), misterruntime.WithCoreReplacementBarrier(barrier))

	inspection, apiErr := runtime.InspectCore(context.Background(), int64(len(archive)), bytes.NewReader(archive))
	if apiErr != nil || !inspection.Compatible || inspection.CompatibilityError != nil ||
		inspection.PackageID == "" || inspection.Descriptor.Core.ID != "fes.pong" || control.inspectCalls != 1 ||
		control.loadCalls != 0 || barrier.begins != 0 {
		t.Fatalf("inspection=%#v error=%#v inspect=%d loads=%d barriers=%d", inspection, apiErr,
			control.inspectCalls, control.loadCalls, barrier.begins)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("staging root after inspection=%v error=%v", entries, err)
	}
}

func TestCorePackageInspectionReturnsIncompatibilityAsData(t *testing.T) {
	archive := canonicalCoreArchive(t)
	compatible := false
	control := &packageControl{inspectCompatible: &compatible,
		inspectCompatibility: &misterruntime.Protocol2Error{Code: "unsupported_interface", Message: "missing input", Phase: "compatibility", Expected: stringPointer("fes.gamepad@1.0")}}
	runtime := misterruntime.NewRuntime(control, "", 0, 0,
		misterruntime.WithCorePackageRoot(t.TempDir()))

	inspection, apiErr := runtime.InspectCore(context.Background(), int64(len(archive)), bytes.NewReader(archive))
	if apiErr != nil || inspection.Compatible || inspection.CompatibilityError == nil ||
		inspection.CompatibilityError.Code != protocol.CodeUnsupportedOperation ||
		inspection.CompatibilityError.Phase != "compatibility" || inspection.CompatibilityError.Expected != "fes.gamepad@1.0" {
		t.Fatalf("inspection=%#v error=%#v", inspection, apiErr)
	}
}

func TestCorePackageInspectionFailsClosedOnRuntimeIdentityMismatch(t *testing.T) {
	archive := canonicalCoreArchive(t)
	for _, test := range []struct {
		name    string
		control *packageControl
	}{
		{name: "package id", control: &packageControl{inspectPackageID: strings.Repeat("d", 64)}},
		{name: "descriptor", control: &packageControl{inspectDescriptor: &corepackage.Descriptor{}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtime := misterruntime.NewRuntime(test.control, "", 0, 0,
				misterruntime.WithCorePackageRoot(t.TempDir()))
			inspection, apiErr := runtime.InspectCore(context.Background(), int64(len(archive)), bytes.NewReader(archive))
			if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable || inspection.PackageID != "" {
				t.Fatalf("inspection=%#v error=%#v", inspection, apiErr)
			}
		})
	}
}

func TestCorePackageInspectionFailsClosedOnContradictoryCompatibility(t *testing.T) {
	archive := canonicalCoreArchive(t)
	for _, test := range []struct {
		name          string
		compatible    bool
		compatibility *misterruntime.Protocol2Error
	}{
		{name: "compatible with error", compatible: true,
			compatibility: &misterruntime.Protocol2Error{Code: "unsupported_interface", Message: "missing", Phase: "compatibility"}},
		{name: "incompatible without error", compatible: false},
		{name: "incompatible error from wrong phase", compatible: false,
			compatibility: &misterruntime.Protocol2Error{Code: "unsupported_interface", Message: "missing", Phase: "request"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			control := &packageControl{inspectCompatible: &test.compatible, inspectCompatibility: test.compatibility}
			runtime := misterruntime.NewRuntime(control, "", 0, 0,
				misterruntime.WithCorePackageRoot(t.TempDir()))

			inspection, apiErr := runtime.InspectCore(context.Background(), int64(len(archive)), bytes.NewReader(archive))
			if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable || inspection.PackageID != "" {
				t.Fatalf("inspection=%#v error=%#v", inspection, apiErr)
			}
		})
	}
}

func TestCorePackageInspectionCancellationCleansStaging(t *testing.T) {
	root := t.TempDir()
	archive := canonicalCoreArchive(t)
	ctx, cancel := context.WithCancel(context.Background())
	control := &packageControl{beforeInspectReply: func(string) { cancel() }}
	runtime := misterruntime.NewRuntime(control, "", 0, 0,
		misterruntime.WithCorePackageRoot(root))

	inspection, apiErr := runtime.InspectCore(ctx, int64(len(archive)), bytes.NewReader(archive))
	if apiErr == nil || apiErr.Code != protocol.CodeMiSTerUnavailable || inspection.PackageID != "" {
		t.Fatalf("inspection=%#v error=%#v", inspection, apiErr)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("staging root after cancellation=%v error=%v", entries, err)
	}
}

func TestCorePackageInspectionCleanupFailureCannotReportCompatibility(t *testing.T) {
	root := t.TempDir()
	moved := root + "-moved"
	archive := canonicalCoreArchive(t)
	control := &packageControl{beforeInspectReply: func(string) {
		if err := os.Rename(root, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}}
	runtime := misterruntime.NewRuntime(control, "", 0, 0,
		misterruntime.WithCorePackageRoot(root))

	inspection, apiErr := runtime.InspectCore(context.Background(), int64(len(archive)), bytes.NewReader(archive))
	if apiErr == nil || apiErr.Code != protocol.CodeInternal || apiErr.Phase != "recovery" || inspection.Compatible {
		t.Fatalf("inspection=%#v error=%#v", inspection, apiErr)
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(moved, root); err != nil {
		t.Fatal(err)
	}
	if _, stopErr := runtime.Stop(context.Background()); stopErr != nil {
		t.Fatal(stopErr)
	}
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
	var scripted *protocol2StatusResult
	if len(c.status2Script) > 0 {
		index := call - 1
		if index >= len(c.status2Script) {
			index = len(c.status2Script) - 1
		}
		result := c.status2Script[index]
		scripted = &result
	}
	c.mu.Unlock()
	if block {
		<-ctx.Done()
		return misterruntime.Protocol2Response{}, ctx.Err()
	}
	if statusErr != nil {
		return misterruntime.Protocol2Response{}, statusErr
	}
	if scripted != nil {
		return scripted.response, scripted.err
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

func TestCorePackageReplacementBarrierFollowsMutationBoundary(t *testing.T) {
	archive := canonicalCoreArchive(t)
	t.Run("success retires", func(t *testing.T) {
		barrier := &replacementBarrier{}
		runtime := misterruntime.NewRuntime(&packageControl{}, "", 0, 0,
			misterruntime.WithCorePackageRoot(t.TempDir()),
			misterruntime.WithCoreReplacementBarrier(barrier))
		t.Cleanup(func() { _, _ = runtime.Stop(context.Background()) })
		_, attempted, apiErr := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(archive)), bytes.NewReader(archive))
		if apiErr != nil || !attempted || barrier.begins != 1 ||
			!reflect.DeepEqual(barrier.preserve, []bool{false}) {
			t.Fatalf("attempted=%t error=%#v barrier=%#v", attempted, apiErr, barrier)
		}
	})

	t.Run("save failure restores", func(t *testing.T) {
		barrier := &replacementBarrier{}
		control := &packageControl{remoteErr: &misterruntime.Protocol2Error{
			Code: "save_failed", Message: "disk full", Phase: "save"}}
		runtime := misterruntime.NewRuntime(control, "", 0, 0,
			misterruntime.WithCorePackageRoot(t.TempDir()),
			misterruntime.WithCoreReplacementBarrier(barrier))
		_, attempted, apiErr := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(archive)), bytes.NewReader(archive))
		if apiErr == nil || attempted || barrier.begins != 1 ||
			!reflect.DeepEqual(barrier.preserve, []bool{true}) {
			t.Fatalf("attempted=%t error=%#v barrier=%#v", attempted, apiErr, barrier)
		}
	})

	t.Run("unsupported pre-dispatch load restores", func(t *testing.T) {
		barrier := &replacementBarrier{}
		control := &packageControl{loadErr: unsupportedProtocolError{}}
		runtime := misterruntime.NewRuntime(control, "", 0, 0,
			misterruntime.WithCorePackageRoot(t.TempDir()),
			misterruntime.WithCoreReplacementBarrier(barrier))
		_, attempted, apiErr := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(archive)), bytes.NewReader(archive))
		if apiErr == nil || apiErr.Code != "UNSUPPORTED_OPERATION" || attempted ||
			barrier.begins != 1 || !reflect.DeepEqual(barrier.preserve, []bool{true}) {
			t.Fatalf("attempted=%t error=%#v barrier=%#v", attempted, apiErr, barrier)
		}
	})

	t.Run("invalid archive does not pause", func(t *testing.T) {
		barrier := &replacementBarrier{}
		runtime := misterruntime.NewRuntime(&packageControl{}, "", 0, 0,
			misterruntime.WithCorePackageRoot(t.TempDir()),
			misterruntime.WithCoreReplacementBarrier(barrier))
		_, attempted, apiErr := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), 3, strings.NewReader("bad"))
		if apiErr == nil || attempted || barrier.begins != 0 {
			t.Fatalf("attempted=%t error=%#v begins=%d", attempted, apiErr, barrier.begins)
		}
	})

	t.Run("incompatible inspection does not pause", func(t *testing.T) {
		barrier := &replacementBarrier{}
		compatible := false
		control := &packageControl{inspectCompatible: &compatible,
			inspectCompatibility: &misterruntime.Protocol2Error{
				Code: "unsupported_interface", Message: "missing gamepad", Phase: "compatibility"}}
		runtime := misterruntime.NewRuntime(control, "", 0, 0,
			misterruntime.WithCorePackageRoot(t.TempDir()),
			misterruntime.WithCoreReplacementBarrier(barrier))
		_, attempted, apiErr := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(archive)), bytes.NewReader(archive))
		if apiErr == nil || attempted || apiErr.Phase != "compatibility" || barrier.begins != 0 || control.loadCalls != 0 {
			t.Fatalf("attempted=%t error=%#v begins=%d loads=%d", attempted, apiErr, barrier.begins, control.loadCalls)
		}
	})

	t.Run("pause or restore failure requires recovery", func(t *testing.T) {
		for _, test := range []struct {
			name      string
			barrier   *replacementBarrier
			remoteErr *misterruntime.Protocol2Error
		}{
			{name: "pause", barrier: &replacementBarrier{beginErr: errors.New("pause failed")}},
			{name: "restore", barrier: &replacementBarrier{finishErr: errors.New("restore failed")}, remoteErr: &misterruntime.Protocol2Error{Code: "save_failed", Message: "disk full", Phase: "save"}},
		} {
			t.Run(test.name, func(t *testing.T) {
				control := &packageControl{remoteErr: test.remoteErr}
				runtime := misterruntime.NewRuntime(control, "", 0, 0,
					misterruntime.WithCorePackageRoot(t.TempDir()),
					misterruntime.WithCoreReplacementBarrier(test.barrier))
				_, attempted, apiErr := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(archive)), bytes.NewReader(archive))
				if apiErr == nil || !attempted || apiErr.Phase != "recovery" {
					t.Fatalf("attempted=%t error=%#v", attempted, apiErr)
				}
			})
		}
	})
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
		barrier := &replacementBarrier{}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 20*time.Millisecond,
			misterruntime.WithCorePackageRoot(t.TempDir()),
			misterruntime.WithCoreReplacementBarrier(barrier))
		activation, attempted, apiErr := runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(archive)), bytes.NewReader(archive))
		if apiErr != nil || !attempted || activation.PackageID == "" || activation.Generation != 1 {
			t.Fatalf("activation=%#v attempted=%t error=%#v", activation, attempted, apiErr)
		}
		if control.loadCalls != 1 || !reflect.DeepEqual(barrier.preserve, []bool{false}) {
			t.Fatalf("load calls=%d barrier=%#v", control.loadCalls, barrier.preserve)
		}
		control.lostReply = false
		_, _ = runtime.Stop(context.Background())
	})

	t.Run("confirmed prior package", func(t *testing.T) {
		control := &packageControl{}
		barrier := &replacementBarrier{}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 20*time.Millisecond,
			misterruntime.WithCorePackageRoot(t.TempDir()),
			misterruntime.WithCoreReplacementBarrier(barrier))
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
		if !reflect.DeepEqual(barrier.preserve, []bool{false, true}) {
			t.Fatalf("barrier results=%#v", barrier.preserve)
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
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var dispatchedPath string
		control.beforeReply = func(path string) {
			dispatchedPath = path
			// Exhaust observation after dispatch, not while admission is staging
			// the archive on a contended CI filesystem.
			cancel()
		}
		_, attempted, apiErr := runtime.LoadCoreOwned(context.Background(), ctx, context.Background(), int64(len(archive)), bytes.NewReader(archive))
		if apiErr == nil || !attempted || control.loadCalls != 1 {
			t.Fatalf("attempted=%t loads=%d error=%#v", attempted, control.loadCalls, apiErr)
		}
		if _, err := os.Stat(dispatchedPath); err != nil {
			t.Fatalf("unresolved dispatched package was not retained: %v", err)
		}
		control.beforeReply = nil
		control.lostReply, control.preserveOnLost, control.status2ErrAfterLoad = false, false, nil
		_, _ = runtime.Stop(context.Background())
		if _, err := os.Stat(dispatchedPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("resolved idle did not clean retained package: %v", err)
		}
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

	t.Run("transient v2 startup failures are polled without fallback or mutation", func(t *testing.T) {
		control := &packageControl{status2Script: []protocol2StatusResult{
			{err: io.EOF},
			{err: errors.New("runtime socket is not accepting requests")},
			{response: misterruntime.Protocol2Response{Protocol: 2, OK: true, State: "starting", Execution: "none", Version: "test"}},
			{response: misterruntime.Protocol2Response{Protocol: 2, OK: true, State: "idle", Execution: "none", Version: "test"}},
		}}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second, misterruntime.WithCorePackageRoot(t.TempDir()))
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		status := runtime.Reconcile(ctx)
		if status.State != "idle" || status.LastError != nil || control.status2Calls != 4 ||
			control.legacyStatusCalls != 0 || control.loadCalls != 0 {
			t.Fatalf("status=%#v v2 calls=%d v1 calls=%d load calls=%d", status, control.status2Calls, control.legacyStatusCalls, control.loadCalls)
		}
	})

	t.Run("unavailable v2 exhausts the caller deadline without fallback", func(t *testing.T) {
		control := &packageControl{status2Err: io.EOF}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second, misterruntime.WithCorePackageRoot(t.TempDir()))
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
		defer cancel()
		status := runtime.Reconcile(ctx)
		if status.State != "failed" || status.LastError == nil || control.status2Calls < 2 ||
			control.legacyStatusCalls != 0 || control.loadCalls != 0 {
			t.Fatalf("status=%#v v2 calls=%d v1 calls=%d load calls=%d", status, control.status2Calls, control.legacyStatusCalls, control.loadCalls)
		}
	})

	t.Run("explicitly unsupported v2 alone falls back to v1", func(t *testing.T) {
		control := &packageControl{status2Err: unsupportedProtocolError{}}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second, misterruntime.WithCorePackageRoot(t.TempDir()))
		status := runtime.Reconcile(context.Background())
		if status.State != "idle" || status.LastError != nil || control.status2Calls != 1 ||
			control.legacyStatusCalls != 1 || control.loadCalls != 0 {
			t.Fatalf("status=%#v v2 calls=%d v1 calls=%d load calls=%d", status, control.status2Calls, control.legacyStatusCalls, control.loadCalls)
		}
	})

	t.Run("conclusive recovery is returned without polling or fallback", func(t *testing.T) {
		response := misterruntime.Protocol2Response{Protocol: 2, OK: false, State: "reboot_required", Execution: "none", Version: "test",
			Error: &misterruntime.Protocol2Error{Code: "idle_failed", Message: "reboot required", Phase: "recovery"}}
		control := &packageControl{status2: &response}
		runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second, misterruntime.WithCorePackageRoot(t.TempDir()))
		status := runtime.Reconcile(context.Background())
		if status.State != "failed" || status.Recovery != "reboot_required" || status.LastError == nil ||
			status.LastError.Phase != "recovery" || control.status2Calls != 1 ||
			control.legacyStatusCalls != 0 || control.loadCalls != 0 {
			t.Fatalf("status=%#v v2 calls=%d v1 calls=%d load calls=%d", status, control.status2Calls, control.legacyStatusCalls, control.loadCalls)
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
	base := filepath.Join("..", "..", "corepackage", "testdata", "core-bundle-v2")
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

func coreArchive(t *testing.T, alternate bool, extras ...string) []byte {
	t.Helper()
	base := filepath.Join("..", "..", "corepackage", "testdata", "core-bundle-v2")
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
	for _, extra := range extras {
		manifest = append(manifest, []byte(extra)...)
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
