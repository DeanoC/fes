package fogcast

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type unavailableExpansionCatalog struct {
	*catalog.Store
	asset *expansion.Asset
}

func (s *unavailableExpansionCatalog) CoreEntryExpansion(_ context.Context, id string) (catalog.CoreEntryExpansion, error) {
	return catalog.CoreEntryExpansion{GameID: id, ExpansionID: strings.Repeat("e", 64)}, nil
}
func (s *unavailableExpansionCatalog) ReadCoreExpansion(context.Context, string) (expansion.Asset, error) {
	if s.asset == nil {
		return expansion.Asset{}, catalog.ErrCoreExpansionNotFound
	}
	return *s.asset, nil
}
func TestMissingOrMismatchedExpansionRejectsBeforeRecoveryStop(t *testing.T) {
	for _, missing := range []bool{true, false} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			database, err := catalog.OpenContext(ctx, filepath.Join(root, "catalog.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			store := &unavailableExpansionCatalog{Store: database}
			if !missing {
				cart := bytes.Repeat([]byte{1}, 45000)
				asset, err := expansion.NewAsset(expansion.Manifest{CartSHA256: fmt.Sprintf("%x", sha256.Sum256(cart)), CartSize: int64(len(cart)), Device: expansion.Device, Format: 1, Map: expansion.Map, RecipeSHA256: strings.Repeat("a", 64), Revision: strings.Repeat("b", 40), ShellBuildID: strings.Repeat("c", 32), ShellPackageID: strings.Repeat("d", 64), ShellSHA256: strings.Repeat("e", 64), Slot: expansion.Slot, SlotMajor: 1}, cart)
				if err != nil {
					t.Fatal(err)
				}
				store.asset = &asset
			}
			packages, err := corepackage.NewStore(filepath.Join(root, "packages"))
			if err != nil {
				t.Fatal(err)
			}
			client := &packageLibraryClient{fakeServiceClient: &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateIdle}}}
			service := newService(Config{RequestTimeout: time.Second, UploadTimeout: time.Second}, Paths{}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, client)
			service.corePackages = packages
			archive := libraryPackageFixture(t, "0.1.0")
			base, _, err := service.ImportCorePackage(ctx, int64(len(archive)), bytes.NewReader(archive))
			if err != nil {
				t.Fatal(err)
			}
			entry, err := service.CreateCoreEntry(ctx, "ZX81 selected expansion", base.PackageID)
			if err != nil {
				t.Fatal(err)
			}
			service.packageRejection = &protocol.APIError{Code: protocol.CodeInternal, Message: "pending recovery"}
			if _, err = service.Launch(ctx, entry.GameID, nil); err == nil {
				t.Fatal("accepted unavailable expansion")
			}
			if client.stopCalls != 0 || client.coreCalls != 0 {
				t.Fatalf("admission mutated target stops=%d loads=%d", client.stopCalls, client.coreCalls)
			}
		})
	}
}
