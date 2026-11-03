package kitpackages

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
)

// factoryPackage writes a fixture package as the image does: an unpacked
// directory named by its package ID. It returns the canonical archive too.
func factoryPackage(t *testing.T, root, version, core string) ([]byte, corepackage.Inspection) {
	t.Helper()
	archive, inspection := fixture(t, version, core)
	directory := filepath.Join(root, inspection.PackageID)
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile("../../corepackage/testdata/core-bundle-v2/manifests/valid-basic.toml")
	if err != nil {
		t.Fatal(err)
	}
	manifest = bytes.ReplaceAll(manifest, []byte(`version = "0.1.0"`), []byte(`version = "`+version+`"`))
	manifest = bytes.ReplaceAll(manifest, []byte(`id = "fes.pong"`), []byte(`id = "`+core+`"`))
	payload, err := os.ReadFile("../../corepackage/testdata/core-bundle-v2/payloads/fes-fixture.rbf")
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"manifest.toml": manifest, "core.rbf": payload} {
		if err := os.WriteFile(filepath.Join(directory, name), data, 0o444); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(directory, 0o555); err != nil {
		t.Fatal(err)
	}
	return archive, inspection
}

func newFactoryRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "core-packages")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err == nil && entry.IsDir() {
				return os.Chmod(path, 0o700)
			}
			return err
		})
	})
	return root
}

func openedView(s *Store, factory *Factory) *View {
	return NewView(func() (*Store, error) { return s, nil }, factory)
}

func TestFactoryReadIsTheCanonicalArchive(t *testing.T) {
	ctx := context.Background()
	root := newFactoryRoot(t)
	archive, want := factoryPackage(t, root, "1.0.0", "fes.pong")
	factory := NewFactory(root)
	inspection, data, err := factory.Read(ctx, want.PackageID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(inspection, want) || !bytes.Equal(data, archive) {
		t.Fatalf("read %+v, archive equal %v", inspection, bytes.Equal(data, archive))
	}
	host, err := corepackage.NewStore(filepath.Join(t.TempDir(), "host"))
	if err != nil {
		t.Fatal(err)
	}
	imported, _, err := host.Import(ctx, int64(len(data)), bytes.NewReader(data))
	if err != nil || imported.PackageID != want.PackageID {
		t.Fatalf("import %+v %v", imported, err)
	}
}

func TestFactoryInspectHashesAnUnchangedPackageOnce(t *testing.T) {
	ctx := context.Background()
	root := newFactoryRoot(t)
	_, want := factoryPackage(t, root, "1.0.0", "fes.pong")
	factory := NewFactory(root)
	factoryVerifications.Store(0)
	for range 3 {
		got, err := factory.Inspect(ctx, want.PackageID)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("inspect %+v %v", got, err)
		}
	}
	if _, err := factory.List(ctx); err != nil {
		t.Fatal(err)
	}
	if got := factoryVerifications.Load(); got != 1 {
		t.Fatalf("verifications = %d, want 1", got)
	}
}

func TestFactoryOmitsAPackageThatDoesNotHashToItsName(t *testing.T) {
	ctx := context.Background()
	root := newFactoryRoot(t)
	_, good := factoryPackage(t, root, "1.0.0", "fes.pong")
	_, moved := factoryPackage(t, root, "2.0.0", "fes.pong")
	wrong := "0000000000000000000000000000000000000000000000000000000000000000"
	if err := os.Rename(filepath.Join(root, moved.PackageID), filepath.Join(root, wrong)); err != nil {
		t.Fatal(err)
	}
	factory := NewFactory(root)
	listed, err := factory.List(ctx)
	if err != nil || len(listed) != 1 || listed[0].PackageID != good.PackageID {
		t.Fatalf("list %+v %v", listed, err)
	}
	if _, err := factory.Inspect(ctx, wrong); !errors.Is(err, ErrInvalidPackage) {
		t.Fatalf("inspect renamed = %v, want ErrInvalidPackage", err)
	}
	if _, _, err := factory.Read(ctx, wrong); !errors.Is(err, ErrInvalidPackage) {
		t.Fatalf("read renamed = %v, want ErrInvalidPackage", err)
	}
	if _, err := NewFactory(filepath.Join(root, "absent")).List(ctx); err != nil {
		t.Fatalf("absent root: %v", err)
	}
}

func TestViewListsDurableThenFactoryPackagesOnce(t *testing.T) {
	ctx := context.Background()
	durable := newStore(t)
	root := newFactoryRoot(t)
	shared, sharedInspection := factoryPackage(t, root, "1.0.0", "fes.pong")
	_, factoryOnly := factoryPackage(t, root, "2.0.0", "fes.pong")
	install(t, durable, shared)
	durableArchive, durableOnly := fixture(t, "3.0.0", "fes.sms")
	install(t, durable, durableArchive)
	view := openedView(durable, NewFactory(root))
	listed, err := view.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, inspection := range listed {
		ids = append(ids, inspection.PackageID)
	}
	want := []string{sharedInspection.PackageID, factoryOnly.PackageID, durableOnly.PackageID}
	if len(ids) != 3 || !sameSet(ids, want) || !sortedStrings(ids) {
		t.Fatalf("list %v, want %v sorted", ids, want)
	}
	for _, id := range want {
		inspection, err := view.Inspect(ctx, id)
		if err != nil || inspection.PackageID != id {
			t.Fatalf("inspect %s: %+v %v", id, inspection, err)
		}
		inspection, data, err := view.Read(ctx, id)
		if err != nil || inspection.PackageID != id || len(data) == 0 {
			t.Fatalf("read %s: %+v %v", id, inspection, err)
		}
	}
	missing := "1111111111111111111111111111111111111111111111111111111111111111"
	if _, err := view.Inspect(ctx, missing); !errors.Is(err, ErrPackageNotFound) {
		t.Fatalf("inspect missing = %v", err)
	}
	if _, _, err := view.Read(ctx, missing); !errors.Is(err, ErrPackageNotFound) {
		t.Fatalf("read missing = %v", err)
	}
}

func TestViewImportsIntoTheDurableStore(t *testing.T) {
	ctx := context.Background()
	durable := newStore(t)
	view := openedView(durable, NewFactory(newFactoryRoot(t)))
	archive, want := fixture(t, "1.0.0", "fes.pong")
	got, created, err := view.Import(ctx, int64(len(archive)), bytes.NewReader(archive))
	if err != nil || !created || got.PackageID != want.PackageID {
		t.Fatalf("import %+v %v %v", got, created, err)
	}
	listed, err := durable.List(ctx)
	if err != nil || len(listed) != 1 || listed[0].PackageID != want.PackageID {
		t.Fatalf("durable list %+v %v", listed, err)
	}
}

func TestDurableInspectHashesAnUnchangedPackageOnce(t *testing.T) {
	ctx := context.Background()
	durable := newStore(t)
	archive, want := fixture(t, "1.0.0", "fes.pong")
	install(t, durable, archive)
	reopened, err := New(durable.root)
	if err != nil {
		t.Fatal(err)
	}
	fullVerificationReads.Store(0)
	for range 3 {
		got, err := reopened.Inspect(ctx, want.PackageID)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("inspect %+v %v", got, err)
		}
	}
	// Opening may already have verified the package; three calls never
	// hash it more than once.
	if got := fullVerificationReads.Load(); got > 1 {
		t.Fatalf("verifications = %d, want at most 1", got)
	}
	if _, err := reopened.Inspect(ctx, "not-an-id"); !errors.Is(err, ErrInvalidPackageID) {
		t.Fatalf("invalid id = %v", err)
	}
}

// Another process installs through its own Store on the same root, as
// mister-agent does beside the kit host.
func TestViewSeesAnotherStoreInstanceAndBoundsItsLockWait(t *testing.T) {
	ctx := context.Background()
	agent := newStore(t)
	host, err := New(agent.root)
	if err != nil {
		t.Fatal(err)
	}
	view := openedView(host, NewFactory(newFactoryRoot(t)))
	archive, want := fixture(t, "1.0.0", "fes.pong")
	install(t, agent, archive)
	listed, err := view.List(ctx)
	if err != nil || len(listed) != 1 || listed[0].PackageID != want.PackageID {
		t.Fatalf("list %+v %v", listed, err)
	}

	_, unlock, err := agent.lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	previous := viewLockWait
	viewLockWait = 100 * time.Millisecond
	defer func() { viewLockWait = previous }()
	started := time.Now()
	if _, err := view.Inspect(context.Background(), want.PackageID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("inspect while locked = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("lock wait took %v", elapsed)
	}
}

func TestViewWithholdsFactoryPackagesWhileTheDurableStoreIsUnavailable(t *testing.T) {
	ctx := context.Background()
	root := newFactoryRoot(t)
	_, factoryOnly := factoryPackage(t, root, "1.0.0", "fes.pong")
	durable := newStore(t)
	opens := 0
	available := false
	now := time.Unix(1000, 0)
	view := &View{factory: NewFactory(root), now: func() time.Time { return now }}
	view.open = func() (*Store, error) {
		opens++
		if !available {
			return nil, errors.New("FESDATA3 is not mounted")
		}
		return durable, nil
	}
	archive := []byte("ignored")
	if _, err := view.List(ctx); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("list = %v", err)
	}
	if _, err := view.Inspect(ctx, factoryOnly.PackageID); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("inspect = %v", err)
	}
	if _, _, err := view.Read(ctx, factoryOnly.PackageID); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("read = %v", err)
	}
	if _, _, err := view.Import(ctx, int64(len(archive)), bytes.NewReader(archive)); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("import = %v", err)
	}
	if opens != 1 {
		t.Fatalf("opens before the retry delay = %d, want 1", opens)
	}
	available = true
	if _, err := view.List(ctx); !errors.Is(err, ErrStoreUnavailable) || opens != 1 {
		t.Fatalf("list inside the retry delay = %v opens=%d", err, opens)
	}
	now = now.Add(viewReopenDelay)
	listed, err := view.List(ctx)
	if err != nil || len(listed) != 1 || listed[0].PackageID != factoryOnly.PackageID || opens != 2 {
		t.Fatalf("list after the delay = %+v %v opens=%d", listed, err, opens)
	}
}

func TestCachedInspectionsAreNotShared(t *testing.T) {
	ctx := context.Background()
	root := newFactoryRoot(t)
	_, factoryWant := factoryPackage(t, root, "1.0.0", "fes.pong")
	durable := newStore(t)
	archive, durableWant := fixture(t, "2.0.0", "fes.pong")
	install(t, durable, archive)
	view := openedView(durable, NewFactory(root))
	for _, want := range []corepackage.Inspection{factoryWant, durableWant} {
		if len(want.Descriptor.Interfaces) == 0 {
			t.Fatal("fixture declares no interfaces")
		}
		for range 2 {
			first, err := view.Inspect(ctx, want.PackageID)
			if err != nil {
				t.Fatal(err)
			}
			first.Descriptor.Interfaces[0] = corepackage.Interface{}
		}
		second, err := view.Inspect(ctx, want.PackageID)
		if err != nil || !reflect.DeepEqual(second, want) {
			t.Fatalf("cache kept a caller's change: %+v %v", second.Descriptor.Interfaces, err)
		}
	}
}

func sameSet(got, want []string) bool {
	seen := map[string]int{}
	for _, value := range got {
		seen[value]++
	}
	for _, value := range want {
		seen[value]--
	}
	for _, count := range seen {
		if count != 0 {
			return false
		}
	}
	return true
}

func sortedStrings(values []string) bool {
	for i := 1; i < len(values); i++ {
		if values[i-1] > values[i] {
			return false
		}
	}
	return true
}
