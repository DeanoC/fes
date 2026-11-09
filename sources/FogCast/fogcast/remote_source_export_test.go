package fogcast

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/internal/meshcontent"
)

type sourceCatalogFixture struct {
	*catalog.Store
	legacy, slot, remote string
	remoteError          error
}

func (s *sourceCatalogFixture) CoreEntryExpansion(ctx context.Context, id string) (catalog.CoreEntryExpansion, error) {
	if id == s.legacy {
		return catalog.CoreEntryExpansion{GameID: id, ExpansionID: "selected"}, nil
	}
	return s.Store.CoreEntryExpansion(ctx, id)
}

func (s *sourceCatalogFixture) CoreEntrySlotExpansions(ctx context.Context, id string) ([]catalog.CoreEntrySlotExpansion, error) {
	if id == s.slot {
		return []catalog.CoreEntrySlotExpansion{{Slot: 1, ExpansionID: "selected"}}, nil
	}
	return s.Store.CoreEntrySlotExpansions(ctx, id)
}

func (s *sourceCatalogFixture) IsRemoteSourceEntry(_ context.Context, id string) (bool, error) {
	return id == s.remote, s.remoteError
}

func TestLibrarySourceExportsOnlyLocalZX81Cassettes(t *testing.T) {
	ctx := context.Background()
	store, err := catalog.Open(filepath.Join(t.TempDir(), "library.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	fixture := &sourceCatalogFixture{Store: store}
	// No target client or package store is installed: source reads must not
	// depend on a connected kit, firmware, or the executor inventory.
	service := &Service{catalog: fixture}
	pkg := strings.Repeat("a", 64)
	putMedia := func(data []byte) catalog.CoreMedia {
		t.Helper()
		media, _, err := store.ImportCoreMedia(ctx, data)
		if err != nil {
			t.Fatal(err)
		}
		return media
	}
	create := func(title, core, role string, media catalog.CoreMedia) catalog.CoreEntry {
		t.Helper()
		entry, err := store.CreateCoreMediaEntry(ctx, title, core, pkg, role, media.MediaID)
		if err != nil {
			t.Fatal(err)
		}
		return entry
	}
	good := putMedia([]byte("cassette"))
	entry, err := store.CreateFirmwareRequiredEntry(ctx, "Cassette", "fes.zx81", pkg, "cassette", good.MediaID)
	if err != nil {
		t.Fatal(err)
	}
	other := putMedia([]byte("not exported"))
	create("Not a cassette", "fes.zx81", "blob", other)
	create("Other core", "fes.spectrum", "cassette", other)
	large := putMedia(bytes.Repeat([]byte{1}, 16385))
	create("Too large", "fes.zx81", "cassette", large)
	fixture.legacy = create("Legacy expansion", "fes.zx81", "cassette", other).GameID
	fixture.slot = create("Slot expansion", "fes.zx81", "cassette", other).GameID
	fixture.remote = create("Remote source", "fes.zx81", "cassette", other).GameID
	got, err := service.LibrarySourceEntries(ctx)
	if err != nil || len(got) != 1 || got[0].GameID != entry.GameID || got[0].FirmwareRequired {
		t.Fatalf("source entries=%+v err=%v", got, err)
	}
	id := meshcontent.SumSHA256([]byte("cassette"))
	available, err := service.LibrarySourceContentAdvertises(ctx, id)
	if err != nil || !available {
		t.Fatalf("advertises=%v err=%v", available, err)
	}
	body, err := service.OpenLibrarySourceContent(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(body)
	if closeErr := body.Close(); err != nil || closeErr != nil || string(data) != "cassette" {
		t.Fatalf("content=%q err=%v close=%v", data, err, closeErr)
	}
	notExported := meshcontent.SumSHA256([]byte("not exported"))
	if available, err := service.LibrarySourceContentAdvertises(ctx, notExported); err != nil || available {
		t.Fatalf("unexpected content advertises=%v err=%v", available, err)
	}
	if _, err := service.OpenLibrarySourceContent(ctx, notExported); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("non-exported content: %v", err)
	}
	fixture.remoteError = errors.New("catalog unavailable")
	if _, err := service.LibrarySourceEntries(ctx); err == nil {
		t.Fatal("remote provenance failure was treated as a local entry")
	}
}
