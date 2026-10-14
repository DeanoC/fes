package main

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/kitpackages"
	"github.com/DeanoC/FogCast/internal/misterruntime"
)

func durablePackageFixture(t *testing.T, version string) ([]byte, corepackage.Inspection, string) {
	t.Helper()
	root := t.TempDir()
	fixture := filepath.Join("..", "..", "corepackage", "testdata", "core-bundle-v2")
	manifest, err := os.ReadFile(filepath.Join(fixture, "manifests", "valid-basic.toml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest = bytes.Replace(manifest, []byte(`version = "0.1.0"`), []byte(`version = "`+version+`"`), 1)
	payload, err := os.ReadFile(filepath.Join(fixture, "payloads", "fes-fixture.rbf"))
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"manifest.toml": manifest, "core.rbf": payload} {
		if err := os.WriteFile(filepath.Join(root, name), data, 0o400); err != nil {
			t.Fatal(err)
		}
	}
	inspection, err := corepackage.InspectPackage(root)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := corepackage.CanonicalArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	return archive, inspection, root
}

func durablePackageStore(t *testing.T) (*kitpackages.Store, string) {
	t.Helper()
	root := t.TempDir()
	t.Cleanup(func() {
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err == nil && entry.IsDir() {
				return os.Chmod(path, 0o700)
			}
			return err
		})
	})
	path := filepath.Join(root, "core-library")
	store, err := kitpackages.New(path)
	if err != nil {
		t.Fatal(err)
	}
	return store, path
}

func installDurablePackage(t *testing.T, store *kitpackages.Store, archive []byte) corepackage.Inspection {
	t.Helper()
	inspection, created, err := store.Install(context.Background(), int64(len(archive)), bytes.NewReader(archive))
	if err != nil || !created {
		t.Fatalf("install: created=%v err=%v", created, err)
	}
	return inspection
}

func TestDurableInventoriesSeparateInstallationFromSelection(t *testing.T) {
	ctx := context.Background()
	store, _ := durablePackageStore(t)
	archive, first, _ := durablePackageFixture(t, "0.1.0")
	installDurablePackage(t, store, archive)
	local, err := durableLocalInventory(ctx, store)
	if err != nil || len(local) != 0 {
		t.Fatalf("unselected local inventory: %+v %v", local, err)
	}
	abis, ids := durableMeshInventory(ctx, nil, store)
	if len(ids) != 1 || ids[0] != first.PackageID || len(abis) != 1 || abis[0].ID != first.Descriptor.ABI.ID {
		t.Fatalf("installed mesh inventory: %+v %v", abis, ids)
	}
	if _, err := store.Select(ctx, first.Descriptor.Core.ID, "", first.PackageID); err != nil {
		t.Fatal(err)
	}
	archive, second, _ := durablePackageFixture(t, "0.2.0")
	installDurablePackage(t, store, archive)
	local, err = durableLocalInventory(ctx, store)
	if err != nil || len(local) != 1 || local[0].PackageID != first.PackageID || !local[0].Launchable {
		t.Fatalf("install replaced selected local core: %+v %v", local, err)
	}
	_, ids = durableMeshInventory(ctx, nil, store)
	if len(ids) != 2 {
		t.Fatalf("version inventory: %v", ids)
	}
	if _, err := store.Select(ctx, first.Descriptor.Core.ID, first.PackageID, second.PackageID); err != nil {
		t.Fatal(err)
	}
	local, err = durableLocalInventory(ctx, store)
	if err != nil || len(local) != 1 || local[0].PackageID != second.PackageID || local[0].CoreID != second.Descriptor.Core.ID {
		t.Fatalf("selected local upgrade: %+v %v", local, err)
	}
}

func TestDurableCorruptionIsNotAdvertised(t *testing.T) {
	for _, member := range []string{"archive", "member"} {
		t.Run(member, func(t *testing.T) {
			ctx := context.Background()
			store, root := durablePackageStore(t)
			archive, inspection, _ := durablePackageFixture(t, "0.1.0")
			installDurablePackage(t, store, archive)
			if _, err := store.Select(ctx, inspection.Descriptor.Core.ID, "", inspection.PackageID); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "archives", inspection.PackageID+".fcore")
			if member == "member" {
				path = filepath.Join(root, "packages", inspection.PackageID, "core.rbf")
			}
			if err := os.Chmod(path, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("corrupt retained bytes"), 0o400); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0o400); err != nil {
				t.Fatal(err)
			}
			if local, err := durableLocalInventory(ctx, store); err == nil || len(local) != 0 {
				t.Fatalf("corrupt local core advertised: %+v %v", local, err)
			}
			abis, ids := durableMeshInventory(ctx, nil, store)
			if len(abis) != 0 || len(ids) != 0 {
				t.Fatalf("corrupt durable mesh inventory: %+v %v", abis, ids)
			}
			_, factory, factoryPath := durablePackageFixture(t, "0.0.1")
			factoryRoot := t.TempDir()
			if err := os.Rename(factoryPath, filepath.Join(factoryRoot, factory.PackageID)); err != nil {
				t.Fatal(err)
			}
			abis, ids = durableMeshInventory(ctx, []string{factoryRoot}, store)
			if len(ids) != 1 || ids[0] != factory.PackageID || len(abis) != 1 {
				t.Fatalf("durable corruption hid unrelated factory inventory: %+v %v", abis, ids)
			}
			if _, _, err := store.Read(ctx, inspection.PackageID); err == nil {
				t.Fatal("corrupt installed object read successfully")
			}
		})
	}
}

type durableStagedControl struct {
	scriptedControl
	t           *testing.T
	stagingRoot string
	archive     []byte
	packageID   string
	path        string
}

func (c *durableStagedControl) LoadCore(ctx context.Context, path, packageID string) (misterruntime.Protocol2Response, error) {
	if !strings.HasPrefix(path, c.stagingRoot+string(filepath.Separator)) || filepath.Dir(path) != c.stagingRoot {
		c.t.Fatalf("installed core bypassed existing staging root: %q", path)
	}
	if packageID != c.packageID {
		c.t.Fatalf("load identity: %s want %s", packageID, c.packageID)
	}
	archive, err := corepackage.CanonicalArchive(path)
	if err != nil {
		return misterruntime.Protocol2Response{}, err
	}
	if !bytes.Equal(archive, c.archive) {
		c.t.Fatal("staged bytes differ from installed canonical package")
	}
	c.path = path
	return c.scriptedControl.LoadCore(ctx, path, packageID)
}

type durableFileSnapshot struct {
	Mode os.FileMode
	Data string
}

func durableTreeSnapshot(t *testing.T, root string) map[string]durableFileSnapshot {
	t.Helper()
	result := map[string]durableFileSnapshot{}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		value := durableFileSnapshot{Mode: info.Mode()}
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			value.Data = string(data)
		}
		result[relative] = value
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestInstalledCoreActivationStagesWithoutChangingRetainedBytes(t *testing.T) {
	for _, source := range []string{"factory", "durable"} {
		t.Run(source, func(t *testing.T) {
			archive, inspection, path := durablePackageFixture(t, "0.1.0")
			retainedRoot := path
			if source == "durable" {
				store, root := durablePackageStore(t)
				installDurablePackage(t, store, archive)
				var err error
				path, err = store.Path(context.Background(), inspection.PackageID)
				if err != nil {
					t.Fatal(err)
				}
				retainedRoot = root
			}
			before := durableTreeSnapshot(t, retainedRoot)
			stagingRoot := leasePackageStagingRoot(t)
			control := &durableStagedControl{t: t, stagingRoot: stagingRoot, archive: archive, packageID: inspection.PackageID}
			runtime := misterruntime.NewRuntime(control, "", 0, 0, misterruntime.WithCorePackageRoot(stagingRoot))
			program := &kitLocalProgram{}
			native := nativeLocalRuntime{runtime: runtime, program: program}
			t.Cleanup(func() { _ = native.Stop(context.Background(), context.Background()) })
			if err := native.LoadCore(context.Background(), context.Background(), path, inspection.PackageID); err != nil {
				t.Fatal(err)
			}
			loads, _, _ := control.counts()
			if loads != 1 || !program.active() || control.path == path {
				t.Fatalf("activation: loads=%d programmed=%v stage=%q", loads, program.active(), control.path)
			}
			if after := durableTreeSnapshot(t, retainedRoot); !reflect.DeepEqual(before, after) {
				t.Fatal("installed directory or archive changed during activation")
			}
			if err := native.Stop(context.Background(), context.Background()); err != nil {
				t.Fatal(err)
			}
			if after := durableTreeSnapshot(t, retainedRoot); !reflect.DeepEqual(before, after) {
				t.Fatal("installed directory or archive changed during Stop")
			}
		})
	}
}
