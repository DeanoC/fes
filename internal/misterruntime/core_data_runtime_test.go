package misterruntime_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
)

type leftoverNativeControl struct {
	dataControl
	p1Status       misterruntime.Response
	p1Stop         misterruntime.Response
	p1Launch       misterruntime.Response
	protocol2Stops int
	protocol1Stops int
}

func (c *leftoverNativeControl) Protocol2Stop(context.Context) (misterruntime.Protocol2Response, error) {
	c.protocol2Stops++
	return misterruntime.Protocol2Response{}, errors.New("described-package stop captured native game")
}
func (c *leftoverNativeControl) Status(context.Context) (misterruntime.Response, error) {
	if c.p1Status.State != "" {
		return c.p1Status, nil
	}
	return c.dataControl.Status(context.Background())
}
func (c *leftoverNativeControl) Stop(context.Context) (misterruntime.Response, error) {
	c.protocol1Stops++
	if c.p1Stop.State != "" {
		return c.p1Stop, nil
	}
	return c.dataControl.Stop(context.Background())
}
func (c *leftoverNativeControl) Launch(context.Context, misterruntime.LaunchRequest) (misterruntime.Response, error) {
	if c.p1Launch.State != "" {
		return c.p1Launch, nil
	}
	return misterruntime.Response{}, errors.New("unexpected launch")
}

type dataControl struct {
	packageControl
	dataCalls, libraryCalls int
	root, revision          string
	speed                   protocol.PaddleSpeed
	dataErr                 error
	remoteDataErr           *misterruntime.Protocol2Error
	wrongID                 bool
	afterData               func(string)
	wrongMode               bool
}

func (c *dataControl) InspectCoreData(ctx context.Context, path, id, root string) (misterruntime.Protocol2Response, error) {
	return c.data(path, id, root)
}
func (c *dataControl) UpdateCoreSettings(ctx context.Context, path, id, root, revision string, speed protocol.PaddleSpeed) (misterruntime.Protocol2Response, error) {
	c.revision, c.speed = revision, speed
	return c.data(path, id, root)
}
func (c *dataControl) data(path, id, root string) (misterruntime.Protocol2Response, error) {
	c.dataCalls++
	c.root = root
	if c.dataErr != nil {
		return misterruntime.Protocol2Response{}, c.dataErr
	}
	if c.remoteDataErr != nil {
		return misterruntime.Protocol2Response{Error: c.remoteDataErr}, nil
	}
	inspection, err := corepackageInspection(path)
	if err != nil {
		return misterruntime.Protocol2Response{}, err
	}
	if c.afterData != nil {
		c.afterData(path)
	}
	if c.wrongID {
		id = strings.Repeat("d", 64)
	}
	data := protocol.CoreData{PackageID: id, CoreID: "fes.pong", Mode: "volatile", Revision: "absent", PaddleSpeed: 1}
	for _, i := range inspection.Descriptor.Interfaces {
		if i.ID == "fes.pong.progress" {
			data.Mode = "persistent"
			data.Layout = &protocol.RuntimeContract{ID: i.ID, Major: 1}
			data.BestRally = 17
			if c.revision != "" {
				data.Revision = strings.Repeat("d", 64)
				data.PaddleSpeed = c.speed
			}
		}
	}
	return misterruntime.Protocol2Response{OK: true, CoreData: &data}, nil
}
func (c *dataControl) LoadLibraryCore(ctx context.Context, path, id, root string) (misterruntime.Protocol2Response, error) {
	c.libraryCalls++
	c.root = root
	response, err := c.LoadCore(ctx, path, id)
	if response.ActivePackage != nil {
		response.ActivePackage.PersistenceMode = "volatile"
		if c.wrongMode {
			response.ActivePackage.PersistenceMode = "persistent"
		}
	}
	return response, err
}
func TestCoreDataAdapterStagesExactArchiveAndCleansEveryResult(t *testing.T) {
	for _, kind := range []string{"inspect", "update", "invalid identity", "lost reply", "storage refused"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			archive := canonicalCoreArchive(t)
			if kind == "update" {
				archive = coreArchive(t, false, "\n[[interfaces]]\nid = \"fes.pong.progress\"\nmajor = 1\nminor = 0\nrequired = true\n")
			}
			control := &dataControl{}
			barrier := &replacementBarrier{}
			runtime := misterruntime.NewRuntime(control, "", 0, 0, misterruntime.WithCorePackageRoot(root), misterruntime.WithCoreReplacementBarrier(barrier))
			inspection, err := runtime.InspectCore(context.Background(), int64(len(archive)), bytes.NewReader(archive))
			if err != nil {
				t.Fatal(err)
			}
			if kind == "invalid identity" {
				control.wrongID = true
			}
			if kind == "lost reply" {
				control.dataErr = io.ErrUnexpectedEOF
			}
			if kind == "storage refused" {
				control.remoteDataErr = &misterruntime.Protocol2Error{Code: "corrupt_data", Message: "secret/path", Phase: "admission"}
			}
			var apiErr *protocol.APIError
			if kind == "update" || kind == "lost reply" {
				_, apiErr = runtime.UpdateCoreSettings(context.Background(), int64(len(archive)), bytes.NewReader(archive), protocol.CoreSettingsUpdate{ExpectedPackageID: inspection.PackageID, ExpectedRevision: "absent", PaddleSpeed: 2})
			} else {
				_, apiErr = runtime.InspectCoreData(context.Background(), int64(len(archive)), bytes.NewReader(archive), inspection.PackageID)
			}
			if (kind == "invalid identity" || kind == "lost reply" || kind == "storage refused") != (apiErr != nil) {
				t.Fatalf("error=%v", apiErr)
			}
			if control.dataCalls != 1 || control.root != misterruntime.CoreDataRoot || barrier.begins != 0 || control.loadCalls != 0 {
				t.Fatalf("calls=%d root=%q", control.dataCalls, control.root)
			}
			if apiErr != nil && strings.Contains(apiErr.Message, "secret/path") {
				t.Fatal("server path escaped")
			}
			entries, err2 := os.ReadDir(root)
			if err2 != nil || len(entries) != 0 {
				t.Fatalf("staging=%v %v", entries, err2)
			}
		})
	}
}
func TestLibraryCoreLoadIsExplicitAndDevelopmentRemainsVolatile(t *testing.T) {
	archive := canonicalCoreArchive(t)
	control := &dataControl{}
	runtime := misterruntime.NewRuntime(control, "", 0, 0, misterruntime.WithCorePackageRoot(t.TempDir()))
	t.Cleanup(func() { _, _ = runtime.Stop(context.Background()) })
	inspection, err := runtime.InspectCore(context.Background(), int64(len(archive)), bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	_, _, apiErr := runtime.LoadLibraryCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(archive)), bytes.NewReader(archive), inspection.PackageID)
	if apiErr != nil || control.libraryCalls != 1 {
		t.Fatalf("library=%d err=%v", control.libraryCalls, apiErr)
	}
	_, _, apiErr = runtime.LoadCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(archive)), bytes.NewReader(archive))
	if apiErr != nil || control.libraryCalls != 1 || control.loadCalls != 2 {
		t.Fatalf("library=%d loads=%d err=%v", control.libraryCalls, control.loadCalls, apiErr)
	}
}

func TestCoreDataCleanupFailureRemainsOwnedAndVisible(t *testing.T) {
	root := t.TempDir()
	archive := canonicalCoreArchive(t)
	control := &dataControl{}
	runtime := misterruntime.NewRuntime(control, "", 0, 0, misterruntime.WithCorePackageRoot(root))
	inspection, apiErr := runtime.InspectCore(context.Background(), int64(len(archive)), bytes.NewReader(archive))
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	moved := root + "-moved"
	control.afterData = func(string) {
		if err := os.Rename(root, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(root, []byte("block cleanup"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	result, apiErr := runtime.InspectCoreData(context.Background(), int64(len(archive)), bytes.NewReader(archive), inspection.PackageID)
	if apiErr == nil || apiErr.Phase != "recovery" || result.PackageID != "" {
		t.Fatalf("result=%+v err=%v", result, apiErr)
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(moved, root); err != nil {
		t.Fatal(err)
	}
	control.afterData = nil
	if _, apiErr = runtime.Stop(context.Background()); apiErr != nil {
		t.Fatal(apiErr)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("retained cleanup=%v err=%v", entries, err)
	}
}
func TestRetiredCoreStagingDoesNotCaptureSNESStop(t *testing.T) {
	root := t.TempDir()
	archive := canonicalCoreArchive(t)
	control := &leftoverNativeControl{}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second,
		misterruntime.WithCorePackageRoot(root),
		misterruntime.WithSaveRoot(filepath.Join(t.TempDir(), "saves", "snes")))
	inspection, apiErr := runtime.InspectCore(context.Background(), int64(len(archive)), bytes.NewReader(archive))
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	moved := root + "-moved"
	control.afterData = func(string) {
		if err := os.Rename(root, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(root, []byte("block cleanup"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, apiErr = runtime.InspectCoreData(context.Background(), int64(len(archive)), bytes.NewReader(archive), inspection.PackageID); apiErr == nil || apiErr.Phase != "recovery" {
		t.Fatalf("cleanup ownership err=%v", apiErr)
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(moved, root); err != nil {
		t.Fatal(err)
	}
	control.afterData = nil
	control.status2 = &misterruntime.Protocol2Response{Protocol: 2, OK: true, State: "running_game", Execution: "game", Version: "test"}
	control.p1Status = runtimeResponse("idle", "none")
	control.p1Launch = snesResponse("running_game")
	spec, _ := core.DefaultRegistry().Lookup(protocol.SystemSNES)
	prepared, err := runtime.Prepare(spec, snesROM(t))
	if err != nil {
		t.Fatal(err)
	}
	prepared.GameID = "super-mario-world"
	if _, _, err := runtime.Launch(context.Background(), prepared); err != nil {
		t.Fatal(err)
	}
	control.p1Status = snesResponse("running_game")
	control.p1Stop = runtimeResponse("idle", "none")
	if !runtime.StopReady() {
		t.Fatal("leftover staging blocked SNES Stop")
	}
	if _, err := runtime.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if control.protocol2Stops != 0 || control.protocol1Stops != 1 {
		t.Fatalf("protocol2=%d protocol1=%d", control.protocol2Stops, control.protocol1Stops)
	}
	failed := snesResponse("running_game")
	failed.Error = &misterruntime.RemoteError{Code: "save_failed", Message: "cannot save"}
	control.p1Status = failed
	control.p1Stop = failed
	if !runtime.StopReady() {
		t.Fatal("SNES save failure must remain Stop-retryable")
	}
	if _, err := runtime.Stop(context.Background()); err == nil || err.Code != protocol.CodeInternal {
		t.Fatalf("SNES save isolation err=%v", err)
	}
	if control.protocol2Stops != 0 {
		t.Fatalf("protocol-2 stop captured SNES save failure: %d", control.protocol2Stops)
	}
}

func TestLibraryLoadRejectsReturnedPersistenceModeMismatch(t *testing.T) {
	archive := canonicalCoreArchive(t)
	control := &dataControl{wrongMode: true}
	runtime := misterruntime.NewRuntime(control, "", 0, 0, misterruntime.WithCorePackageRoot(t.TempDir()))
	t.Cleanup(func() { _, _ = runtime.Stop(context.Background()) })
	inspection, err := runtime.InspectCore(context.Background(), int64(len(archive)), bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	_, attempted, apiErr := runtime.LoadLibraryCoreOwned(context.Background(), context.Background(), context.Background(), int64(len(archive)), bytes.NewReader(archive), inspection.PackageID)
	if apiErr == nil || !attempted {
		t.Fatalf("mode mismatch attempted=%t err=%v", attempted, apiErr)
	}
}
