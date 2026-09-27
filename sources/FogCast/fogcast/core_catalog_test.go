package fogcast

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func catalogServiceFixture(t *testing.T) (*Service, string) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	store, err := catalog.OpenContext(ctx, filepath.Join(root, "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	packages, err := corepackage.NewStore(filepath.Join(root, "installed"))
	if err != nil {
		t.Fatal(err)
	}
	s := newService(Config{RequestTimeout: time.Second, UploadTimeout: time.Second}, Paths{}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, &packageLibraryClient{fakeServiceClient: &fakeServiceClient{}})
	s.corePackages = packages
	raw := libraryPackageFixture(t, "0.1.0")
	inspection, _, err := packages.Import(ctx, int64(len(raw)), bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	os.WriteFile(filepath.Join(root, "core.fcore"), raw, 0600)
	value := map[string]any{"version": 1, "source_id": "fes-first-party", "catalog_sha256": hex.EncodeToString(sum[:]), "entries": []any{map[string]any{"core_id": "fes.pong", "label": "Pong", "system": "pong", "standing": "supported", "package_id": inspection.PackageID, "archive_path": "core.fcore", "archive_sha256": hex.EncodeToString(sum[:]), "archive_size": len(raw)}}}
	delete(value, "catalog_sha256")
	body, _ := json.Marshal(value)
	catalogSum := sha256.Sum256(body)
	value["catalog_sha256"] = hex.EncodeToString(catalogSum[:])
	b, _ := json.Marshal(value)
	path := filepath.Join(root, "catalog.json")
	os.WriteFile(path, b, 0600)
	s.coreCatalogPath = path
	s.coreLibrarySourceID = "test-library"
	return s, inspection.PackageID
}
func TestCoreCatalogInstallPreservesEntry(t *testing.T) {
	s, pid := catalogServiceFixture(t)
	ctx := context.Background()
	e, err := s.CreateCoreEntry(ctx, "My Pong", pid)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err = s.InstallAvailableCore(ctx, "fes-first-party", "fes.pong", pid); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.AvailableCores(ctx)
	if err != nil || len(rows) != 1 || rows[0].ArtifactState != "installed" {
		t.Fatalf("%+v %v", rows, err)
	}
	got, err := s.CoreEntry(ctx, e.GameID)
	if err != nil || got != e {
		t.Fatalf("entry changed %+v %v", got, err)
	}
	if _, err = s.InstallAvailableCore(ctx, "other-source", "fes.pong", pid); err == nil {
		t.Fatal("accepted wrong source")
	}
}
func TestCoreCatalogRejectsCanonicalIdentityBeforeImport(t *testing.T) {
	s, pid := catalogServiceFixture(t)
	empty, err := corepackage.NewStore(filepath.Join(t.TempDir(), "empty"))
	if err != nil {
		t.Fatal(err)
	}
	s.corePackages = empty
	var value map[string]any
	b, err := os.ReadFile(s.coreCatalogPath)
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(b, &value)
	row := value["entries"].([]any)[0].(map[string]any)
	row["core_id"] = "fes.sms"
	delete(value, "catalog_sha256")
	b, _ = json.Marshal(value)
	sum := sha256.Sum256(b)
	value["catalog_sha256"] = hex.EncodeToString(sum[:])
	b, _ = json.Marshal(value)
	os.WriteFile(s.coreCatalogPath, b, 0600)
	if _, err := s.InstallAvailableCore(context.Background(), "fes-first-party", "fes.sms", pid); err == nil {
		t.Fatal("imported another core")
	}
	values, err := empty.List(context.Background())
	if err != nil || len(values) != 0 {
		t.Fatal(values, err)
	}
}
func TestCoreCatalogDisabledAndUnavailableStates(t *testing.T) {
	s, _ := catalogServiceFixture(t)
	path := s.coreCatalogPath
	s.coreCatalogPath = ""
	if _, err := s.AvailableCores(context.Background()); err == nil {
		t.Fatal("disabled catalog accepted")
	}
	s.coreCatalogPath = path
	s.coreLibrarySourceID = "test-library"
	os.Remove(filepath.Join(filepath.Dir(path), "core.fcore"))
	rows, err := s.AvailableCores(context.Background())
	if err != nil || rows[0].ArtifactState != "installed" {
		t.Fatal(rows, err)
	}
	empty, err := corepackage.NewStore(filepath.Join(t.TempDir(), "empty"))
	if err != nil {
		t.Fatal(err)
	}
	s.corePackages = empty
	rows, err = s.AvailableCores(context.Background())
	if err != nil || rows[0].ArtifactState != "unavailable" {
		t.Fatal(rows, err)
	}
}
