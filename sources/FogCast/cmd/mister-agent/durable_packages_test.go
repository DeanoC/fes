package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/kitpackages"
	"github.com/DeanoC/FogCast/internal/localcores"
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

func TestAbsentPartitionWithholdsLocalFactoryInventory(t *testing.T) {
	read := localCoreInventory(context.Background(), nil, errDataPartitionAbsent)
	if read == nil {
		t.Fatal("absent FESDATA3 left kit-local cores on the factory catalog")
	}
	cores, err := read()
	if !errors.Is(err, errDataPartitionAbsent) || len(cores) != 0 {
		t.Fatalf("absent inventory = %+v %v", cores, err)
	}
	if localCoreInventory(context.Background(), nil, nil) != nil {
		t.Fatal("no library error installed a kit-local inventory callback")
	}
}

type durableZeroReader struct{}

func (durableZeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func writeDurableZeros(t *testing.T, path string, n int64) string {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	if _, err := io.CopyN(io.MultiWriter(file, hash), durableZeroReader{}, n); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func largePackageDir(t *testing.T, coreID, name string, n int64) (string, corepackage.Inspection) {
	t.Helper()
	dir := t.TempDir()
	manifest, err := os.ReadFile(filepath.Join("..", "..", "corepackage", "testdata", "core-bundle-v2", "manifests", "valid-basic.toml"))
	if err != nil {
		t.Fatal(err)
	}
	sum := writeDurableZeros(t, filepath.Join(dir, "core.rbf"), n)
	manifest = bytes.ReplaceAll(manifest, []byte(`id = "fes.pong"`), []byte(`id = "`+coreID+`"`))
	manifest = bytes.ReplaceAll(manifest, []byte(`name = "FES Pong"`), []byte(`name = "`+name+`"`))
	manifest = bytes.ReplaceAll(manifest, []byte("size = 12\n"), []byte(fmt.Sprintf("size = %d\n", n)))
	manifest = bytes.ReplaceAll(manifest, []byte(`sha256 = "e7bbf8fe5ebdebeef7f2e70638a0a3494f22ab977e1506386010705a3d43adf1"`), []byte(fmt.Sprintf("sha256 = %q", sum)))
	if err := os.WriteFile(filepath.Join(dir, "manifest.toml"), manifest, 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, "core.rbf"), 0o400); err != nil {
		t.Fatal(err)
	}
	inspection, err := corepackage.InspectPackage(dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, inspection
}

func installFactoryPackage(t *testing.T, packages, selections, coreID, name string, n int64) string {
	t.Helper()
	dir, inspection := largePackageDir(t, coreID, name, n)
	dest := filepath.Join(packages, inspection.PackageID)
	if err := os.Rename(dir, dest); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("core_id = %q\npackage_id = %q\npayload_sha256 = %q\ninstall_path = %q\n",
		coreID, inspection.PackageID, inspection.Descriptor.Payload.SHA256, dest)
	stem := strings.TrimPrefix(coreID, "fes.")
	if err := os.WriteFile(filepath.Join(selections, "fes-"+stem+".package.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return inspection.PackageID
}

func processReadBytes(t *testing.T) uint64 {
	t.Helper()
	data, err := os.ReadFile("/proc/self/io")
	if err != nil {
		t.Fatalf("process io: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		var n uint64
		if _, err := fmt.Sscanf(line, "read_bytes: %d", &n); err == nil && strings.HasPrefix(line, "read_bytes:") {
			return n
		}
	}
	t.Fatal("read_bytes missing")
	return 0
}

func listLocalCores(t *testing.T, handler http.Handler) string {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/local/cores", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("list %d %s", response.Code, response.Body.String())
	}
	return response.Body.String()
}

func TestRepeatedLocalCoreListDoesNotRereadSelectedPackage(t *testing.T) {
	const payloadBytes = 1 << 20
	root := t.TempDir()
	selections := filepath.Join(root, "selections")
	packages := filepath.Join(root, "packages")
	if err := os.Mkdir(selections, 0o755); err != nil || os.Mkdir(packages, 0o755) != nil {
		t.Fatal(err)
	}
	factoryID := installFactoryPackage(t, packages, selections, "fes.pong", "FES Pong", payloadBytes)
	store, _ := durablePackageStore(t)
	dir, inspection := largePackageDir(t, "fes.coleco", "FES Coleco", payloadBytes)
	archive, err := corepackage.CanonicalArchive(dir)
	if err != nil {
		t.Fatal(err)
	}
	installDurablePackage(t, store, archive)
	if _, err := store.Select(context.Background(), "fes.coleco", "", inspection.PackageID); err != nil {
		t.Fatal(err)
	}
	service := localcores.New(nil, nil, localcores.Roots{Selections: selections, Packages: packages})
	t.Cleanup(service.Close)
	service.SetInventory(localCoreInventory(context.Background(), store, nil))
	handler := localcores.Handler(service)

	first := listLocalCores(t, handler)
	if !strings.Contains(first, inspection.PackageID) || !strings.Contains(first, factoryID) || !strings.Contains(first, "fes.coleco") {
		t.Fatalf("list %s", first)
	}
	before := processReadBytes(t)
	for range 2 {
		again := listLocalCores(t, handler)
		if again != first {
			t.Fatalf("list changed\n%s\n%s", first, again)
		}
	}
	if delta := processReadBytes(t) - before; delta > 256<<10 {
		t.Fatalf("repeated lists read %d payload bytes", delta)
	}
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

func rewriteSameSizeRestoreMtime(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(original) == 0 {
		t.Fatal("empty package member")
	}
	rewritten := bytes.Clone(original)
	rewritten[len(rewritten)/2] ^= 0xff
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, rewritten, info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
	// Linux inode timestamps follow a coarse clock. Re-apply metadata until
	// ctime moves so the inventory check is not a race against that tick.
	settleLinuxCtime(t, path, info)
	after, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) || after.Mode().Perm() != info.Mode().Perm() {
		t.Fatalf("rewrite changed size, mtime, or mode: before=%d %v %v after=%d %v %v", info.Size(), info.ModTime(), info.Mode().Perm(), after.Size(), after.ModTime(), after.Mode().Perm())
	}
	body, err := os.ReadFile(path)
	if err != nil || bytes.Equal(body, original) {
		t.Fatal("same-size rewrite left the original bytes in place")
	}
}

func TestSameSizeRewriteWithRestoredMtimeIsNotAdvertised(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("listing ctime comes from Linux stat")
	}
	for _, member := range []string{"archive", "member"} {
		t.Run(member, func(t *testing.T) {
			ctx := context.Background()
			store, root := durablePackageStore(t)
			archive, inspection, _ := durablePackageFixture(t, "0.1.0")
			installDurablePackage(t, store, archive)
			if _, err := store.Select(ctx, inspection.Descriptor.Core.ID, "", inspection.PackageID); err != nil {
				t.Fatal(err)
			}
			listed, err := durableLocalInventory(ctx, store)
			if err != nil || len(listed) != 1 || listed[0].PackageID != inspection.PackageID {
				t.Fatalf("selected inventory before rewrite: %+v %v", listed, err)
			}
			abis, ids := durableMeshInventory(ctx, nil, store)
			if len(ids) != 1 || ids[0] != inspection.PackageID || len(abis) != 1 {
				t.Fatalf("mesh inventory before rewrite: %+v %v", abis, ids)
			}
			path := filepath.Join(root, "archives", inspection.PackageID+".fcore")
			if member == "member" {
				path = filepath.Join(root, "packages", inspection.PackageID, "core.rbf")
			}
			rewriteSameSizeRestoreMtime(t, path)
			if local, err := durableLocalInventory(ctx, store); err == nil || len(local) != 0 {
				t.Fatalf("rewritten local core stayed advertised: %+v %v", local, err)
			}
			abis, ids = durableMeshInventory(ctx, nil, store)
			if len(abis) != 0 || len(ids) != 0 {
				t.Fatalf("rewritten package stayed in mesh inventory: %+v %v", abis, ids)
			}
			_, factory, factoryPath := durablePackageFixture(t, "0.0.1")
			factoryRoot := t.TempDir()
			if err := os.Rename(factoryPath, filepath.Join(factoryRoot, factory.PackageID)); err != nil {
				t.Fatal(err)
			}
			abis, ids = durableMeshInventory(ctx, []string{factoryRoot}, store)
			if len(ids) != 1 || ids[0] != factory.PackageID || len(abis) != 1 {
				t.Fatalf("rewritten durable package hid unrelated factory inventory: %+v %v", abis, ids)
			}
		})
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
