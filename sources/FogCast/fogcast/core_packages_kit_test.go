package fogcast

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/kitpackages"
	"github.com/DeanoC/FogCast/protocol"
)

// unpackFactoryPackage installs archive as the image does: an unpacked
// directory named by its package ID under root.
func unpackFactoryPackage(t *testing.T, root string, archive []byte) string {
	t.Helper()
	stage := filepath.Join(t.TempDir(), "stage")
	if err := os.Mkdir(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	staged, err := corepackage.StageBytes(context.Background(), stage, archive)
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Cleanup()
	directory := filepath.Join(root, staged.PackageID)
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	members, err := os.ReadDir(staged.Directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range members {
		data, err := os.ReadFile(filepath.Join(staged.Directory, member.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, member.Name()), data, 0o444); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(directory, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(directory, 0o700) })
	return staged.PackageID
}

func kitHostService(t *testing.T) (*Service, *packageLibraryClient, string, *kitpackages.Store) {
	t.Helper()
	root := t.TempDir()
	store, err := catalog.OpenContext(context.Background(), filepath.Join(root, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	durable, err := kitpackages.New(filepath.Join(root, "core-library"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = filepath.WalkDir(filepath.Join(root, "core-library"), func(path string, entry os.DirEntry, err error) error {
			if err == nil && entry.IsDir() {
				return os.Chmod(path, 0o700)
			}
			return err
		})
	})
	factory := filepath.Join(root, "factory")
	if err := os.Mkdir(factory, 0o755); err != nil {
		t.Fatal(err)
	}
	client := &packageLibraryClient{fakeServiceClient: &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateIdle}}}
	s := newService(Config{RequestTimeout: time.Second, UploadTimeout: time.Second}, Paths{}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, client)
	s.corePackages = kitpackages.NewView(func() (*kitpackages.Store, error) { return durable, nil }, kitpackages.NewFactory(factory))
	return s, client, factory, durable
}

// A kit host titles and reads a factory package without uploading it, and
// imports into the durable store.
func TestKitHostServesFactoryPackagesAndImportsDurably(t *testing.T) {
	ctx := context.Background()
	s, client, factory, durable := kitHostService(t)
	archive := libraryPackageFixture(t, "1.0.0")
	id := unpackFactoryPackage(t, factory, archive)

	listed, err := s.CorePackages(ctx)
	if err != nil || len(listed) != 1 || listed[0].PackageID != id {
		t.Fatalf("listed %+v %v", listed, err)
	}
	if inspection, err := s.CorePackage(ctx, id); err != nil || inspection.PackageID != id {
		t.Fatalf("package %+v %v", inspection, err)
	}
	if _, data, err := s.readInstalledCore(ctx, id); err != nil || !bytes.Equal(data, archive) {
		t.Fatalf("read equal=%v %v", bytes.Equal(data, archive), err)
	}
	client.inspection = protocol.CoreInspection{PackageID: id, Descriptor: listed[0].Descriptor, Compatible: true}
	entry, err := s.CreateCoreEntry(ctx, "Factory Pong", id)
	if err != nil || entry.PackageID != id {
		t.Fatalf("entry %+v %v", entry, err)
	}

	upload := libraryPackageFixture(t, "2.0.0")
	imported, created, err := s.ImportCorePackage(ctx, int64(len(upload)), bytes.NewReader(upload))
	if err != nil || !created {
		t.Fatalf("import %+v %v %v", imported, created, err)
	}
	inDurable, err := durable.List(ctx)
	if err != nil || len(inDurable) != 1 || inDurable[0].PackageID != imported.PackageID {
		t.Fatalf("durable %+v %v", inDurable, err)
	}
	if listed, err = s.CorePackages(ctx); err != nil || len(listed) != 2 {
		t.Fatalf("listed after import %+v %v", listed, err)
	}
}

func TestKitHostWithoutDurableStorageReportsItUnavailable(t *testing.T) {
	ctx := context.Background()
	s, _, factory, _ := kitHostService(t)
	id := unpackFactoryPackage(t, factory, libraryPackageFixture(t, "1.0.0"))
	s.corePackages = kitpackages.NewView(func() (*kitpackages.Store, error) {
		return nil, errors.New("FESDATA3 is not mounted")
	}, kitpackages.NewFactory(factory))
	_, listErr := s.CorePackages(ctx)
	_, inspectErr := s.CorePackage(ctx, id)
	archive := libraryPackageFixture(t, "2.0.0")
	_, _, importErr := s.ImportCorePackage(ctx, int64(len(archive)), bytes.NewReader(archive))
	for name, err := range map[string]error{"list": listErr, "inspect": inspectErr, "import": importErr} {
		var apiErr *protocol.APIError
		if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeCorePackageStoreUnavailable || apiErr.Message != coreStoreUnavailableMessage {
			t.Fatalf("%s error = %v, want %s", name, err, protocol.CodeCorePackageStoreUnavailable)
		}
	}
}

func TestCorePackagesKitSurvivesACanonicalConfigRewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `base_url = "http://192.0.2.20:8182"
token = "test-token"
request_timeout_seconds = 12
upload_timeout_seconds = 60

[core_packages]
kit = true
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(path)
	if err != nil || !loaded.CorePackages.Kit {
		t.Fatalf("loaded %#v %v", loaded.CorePackages, err)
	}
	if err := writeCanonicalConfig(path, nil, []TargetConfig{{Name: "kit", Enabled: true, Address: "http://127.0.0.1:8182", Agent: "test-token", AgentSet: true}}, "kit"); err != nil {
		t.Fatal(err)
	}
	if loaded, err = LoadConfig(path); err != nil || !loaded.CorePackages.Kit {
		t.Fatalf("after rewrite %#v %v", loaded.CorePackages, err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(content, "[core_packages]\nkit = true\n", "", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if loaded, err = LoadConfig(path); err != nil || loaded.CorePackages.Kit {
		t.Fatalf("default %#v %v", loaded.CorePackages, err)
	}
}
