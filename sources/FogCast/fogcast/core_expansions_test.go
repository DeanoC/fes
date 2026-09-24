package fogcast

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/misteross/expansion"
)

type colecoCompositionClient struct {
	*defaultMediaPackageClient
	root   string
	active protocol.Status
	linked []byte
}

func (c *colecoCompositionClient) LoadComposedCore(ctx context.Context, size int64, body io.Reader, packageID string) (protocol.Status, error) {
	staged, err := corepackage.StageComposition(ctx, c.root, size, body)
	if err != nil {
		return protocol.Status{}, err
	}
	defer staged.Cleanup()
	if staged.PackageID != packageID || staged.Composition == nil {
		return protocol.Status{}, errors.New("target received a different Coleco composition")
	}
	c.linked, err = os.ReadFile(staged.PayloadPath)
	if err != nil {
		return protocol.Status{}, err
	}
	status := c.active
	status.CorePackage.Composition = staged.Composition
	c.statusResult = status
	c.mediaStatus = status
	return status, nil
}

func TestColecoExpansionLibraryBinding(t *testing.T) {
	golden := os.Getenv("FES_COLECO_GOLDEN_ROOT")
	if golden == "" {
		t.Skip("set FES_COLECO_GOLDEN_ROOT to a sealed diagnostic build")
	}
	ctx := context.Background()
	root := t.TempDir()
	store, err := catalog.OpenContext(ctx, filepath.Join(root, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	packages, err := corepackage.NewStore(filepath.Join(root, "packages"))
	if err != nil {
		t.Fatal(err)
	}
	client := &colecoCompositionClient{defaultMediaPackageClient: &defaultMediaPackageClient{
		packageLibraryClient: &packageLibraryClient{fakeServiceClient: &fakeServiceClient{
			statusResult: protocol.Status{State: protocol.StateIdle},
		}},
	}, root: filepath.Join(root, "staged")}
	if err := os.Mkdir(client.root, 0700); err != nil {
		t.Fatal(err)
	}
	service := newService(Config{RequestTimeout: time.Second, UploadTimeout: time.Second}, Paths{}, store,
		&fakeServiceScanner{}, &fakeServicePreparer{}, client)
	service.corePackages = packages
	shell := filepath.Join(golden, "..", "..", "fes-coleco-socket-dev")
	var archive bytes.Buffer
	for _, name := range []string{"manifest.toml", "core.rbf"} {
		data, err := os.ReadFile(filepath.Join(shell, name))
		if err != nil {
			t.Fatal(err)
		}
		header := make([]byte, 512)
		copy(header, name)
		copy(header[100:], "0000644\x00")
		copy(header[108:], "0000000\x00")
		copy(header[116:], "0000000\x00")
		copy(header[124:], fmt.Sprintf("%011o\x00", len(data)))
		copy(header[136:], "00000000000\x00")
		copy(header[148:], "        ")
		header[156] = '0'
		copy(header[257:], "ustar\x00")
		copy(header[263:], "00")
		sum := 0
		for _, value := range header {
			sum += int(value)
		}
		copy(header[148:], fmt.Sprintf("%06o\x00 ", sum))
		archive.Write(header)
		archive.Write(data)
		archive.Write(make([]byte, (512-len(data)%512)%512))
	}
	archive.Write(make([]byte, 1024))
	installed, _, err := service.ImportCorePackage(ctx, int64(archive.Len()), bytes.NewReader(archive.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	client.inspection = protocol.CoreInspection{PackageID: installed.PackageID,
		Descriptor: installed.Descriptor, Compatible: true}
	entry, err := store.CreateCoreEntry(ctx, "Coleco bus diagnostic", installed.Descriptor.Core.ID, installed.PackageID)
	if err != nil {
		t.Fatal(err)
	}
	archives, err := filepath.Glob(filepath.Join(golden, "*.tar"))
	if err != nil || len(archives) != 1 {
		t.Fatalf("diagnostic archive: %v %v", archives, err)
	}
	f, err := os.Open(archives[0])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	imported, err := service.ImportCoreExpansion(ctx, info.Size(), f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SelectCoreEntryExpansion(ctx, entry.GameID, installed.PackageID, "", imported.ExpansionID); err != nil {
		t.Fatal(err)
	}
	selected, err := store.CoreEntryExpansion(ctx, entry.GameID)
	if err != nil || selected.ExpansionID != imported.ExpansionID {
		t.Fatalf("selection %v: %+v", err, selected)
	}
	readiness, err := service.CoreCompositions(ctx, []string{entry.GameID})
	if err != nil || !readiness[entry.GameID].ExpansionReady {
		t.Fatalf("Coleco expansion not ready: %+v %v", readiness, err)
	}
	bundle, err := service.composeCoreEntry(ctx, entry, archive.Bytes())
	if err != nil || bundle == nil || bundle.Composition.ExpansionID != imported.ExpansionID {
		t.Fatalf("Coleco library composition: %+v %v", bundle, err)
	}
	oracle, err := os.ReadFile(filepath.Join(golden, "linked.rbf"))
	if err != nil || !bytes.Equal(bundle.Payload, oracle) {
		t.Fatalf("selected Coleco expansion differs from routed linker oracle: %v", err)
	}
	attachLibraryMedia(t, service, entry.GameID, installed.PackageID, "", []byte("Coleco diagnostic media"))
	client.active = coreEntryActiveStatus(installed, 7, true)
	client.active.CorePackage.ActiveInterfaces = append(client.active.CorePackage.ActiveInterfaces,
		protocol.RuntimeInterface{ID: "fes.media.blob-stream", Major: 1})
	client.active.CorePackage.MediaStream = &protocol.MediaStreamCapability{
		Interface: protocol.MediaStreamInterface(), MinBytes: 1, MaxBytes: 32768, ChunkBytes: 512,
	}
	if _, err := service.Launch(ctx, entry.GameID, nil); err != nil {
		t.Fatalf("Coleco composed library launch: %v", err)
	}
	if !bytes.Equal(client.linked, oracle) || client.mediaCalls != 1 {
		t.Fatalf("Coleco launch sent linked RBF=%t media calls=%d", bytes.Equal(client.linked, oracle), client.mediaCalls)
	}
	reopened, err := catalog.OpenContext(ctx, filepath.Join(root, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	restored, err := reopened.CoreEntryExpansion(ctx, entry.GameID)
	if closeErr := reopened.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil || restored.ExpansionID != imported.ExpansionID {
		t.Fatalf("Coleco selection did not survive catalog reopen: %+v %v", restored, err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	asset, err := expansion.ReadAsset(f)
	if err != nil {
		t.Fatal(err)
	}
	wrong := asset.Manifest
	wrong.Slot, wrong.Map = expansion.Slot, expansion.Map
	wrongAsset, err := expansion.NewAsset(wrong, asset.Cart)
	if err != nil {
		t.Fatal(err)
	}
	wrongStored, err := store.ImportCoreExpansion(ctx, wrongAsset)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SelectCoreEntryExpansion(ctx, entry.GameID, installed.PackageID, imported.ExpansionID, wrongStored.ExpansionID); err != nil {
		t.Fatal(err)
	}
	readiness, err = service.CoreCompositions(ctx, []string{entry.GameID})
	if err != nil || readiness[entry.GameID].ExpansionReady {
		t.Fatalf("wrong-bus selection reported ready: %+v %v", readiness, err)
	}
	if _, err := service.composeCoreEntry(ctx, entry, archive.Bytes()); err == nil {
		t.Fatal("wrong-bus library selection composed for launch")
	}
	if _, err := service.SelectCoreEntryExpansion(ctx, entry.GameID, installed.PackageID, imported.ExpansionID, ""); err == nil {
		t.Fatal("stale expansion selection cleared current choice")
	}
	if _, err := service.SelectCoreEntryExpansion(ctx, entry.GameID, installed.PackageID, wrongStored.ExpansionID, ""); err != nil {
		t.Fatal(err)
	}
	readiness, err = service.CoreCompositions(ctx, []string{entry.GameID})
	if err != nil || readiness[entry.GameID].ExpansionID != "" || readiness[entry.GameID].ExpansionReady {
		t.Fatalf("cleared Coleco socket still selected: %+v %v", readiness, err)
	}
	if bundle, err := service.composeCoreEntry(ctx, entry, archive.Bytes()); err != nil || bundle != nil {
		t.Fatalf("cleared Coleco expansion still composed: %+v %v", bundle, err)
	}
}

func TestExpansionErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code protocol.ErrorCode
	}{
		{"missing title", catalog.ErrCoreEntryNotFound, protocol.CodeROMNotFound},
		{"selection conflict", catalog.ErrCoreEntryConflict, protocol.CodeStaleRevision},
		{"invalid entry", catalog.ErrInvalidCoreEntry, protocol.CodeBadRequest},
		{"missing pack", catalog.ErrCoreExpansionNotFound, protocol.CodeBadRequest},
		{"invalid pack", catalog.ErrInvalidCoreExpansion, protocol.CodeBadRequest},
		{"store failure", errors.New("private database details"), protocol.CodeInternal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var api *protocol.APIError
			if err := expansionError(fmt.Errorf("wrapped: %w", tc.err)); !errors.As(err, &api) || api.Code != tc.code {
				t.Fatalf("expected %s, got %v", tc.code, err)
			}
			if strings.Contains(api.Message, "private database details") {
				t.Fatal("exposed storage error details")
			}
		})
	}
	if expansionError(nil) != nil {
		t.Fatal("nil error changed")
	}
}

func TestClearExpansionPreservesCatalogErrors(t *testing.T) {
	for _, closed := range []bool{false, true} {
		t.Run(fmt.Sprint(closed), func(t *testing.T) {
			ctx := context.Background()
			store, err := catalog.OpenContext(ctx, filepath.Join(t.TempDir(), "catalog.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			want := protocol.CodeROMNotFound
			if closed {
				store.Close()
				want = protocol.CodeInternal
			}
			service := newService(Config{RequestTimeout: time.Second}, Paths{}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{})
			_, err = service.SelectCoreEntryExpansion(ctx, "missing-title", strings.Repeat("a", 64), "", "")
			var api *protocol.APIError
			if !errors.As(err, &api) || api.Code != want {
				t.Fatalf("expected %s, got %v", want, err)
			}
		})
	}
}

func TestMalformedExpansionImportRemainsAdmissionError(t *testing.T) {
	ctx := context.Background()
	store, err := catalog.OpenContext(ctx, filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := newService(Config{}, Paths{}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{})
	_, err = service.ImportCoreExpansion(ctx, 3, strings.NewReader("bad"))
	var api *protocol.APIError
	if !errors.As(err, &api) || api.Code != protocol.CodeBadRequest || api.Phase != "admission" {
		t.Fatalf("expected admission error, got %v", err)
	}
}

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
