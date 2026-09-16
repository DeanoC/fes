package fogcast

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

func TestInstalledPackageReadFailureIsInternal(t *testing.T) {
	root := filepath.Join(t.TempDir(), "packages")
	store, err := corepackage.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	raw := libraryPackageFixture(t, "0.1.0")
	inspection, _, err := store.Import(context.Background(), int64(len(raw)), bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	s := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, &fakeServiceClient{})
	s.corePackages = store
	if err := os.Rename(root, root+"-moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	_, err = s.CorePackage(context.Background(), inspection.PackageID)
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeInternal {
		t.Fatalf("replaced store root should be an internal availability failure: %v", err)
	}
}
