package misterruntime_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
)

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
