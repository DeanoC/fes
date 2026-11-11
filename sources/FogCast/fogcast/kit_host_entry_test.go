package fogcast

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/libraryuser"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/romsource"
)

func TestKitHostEntryPreparesScannedCartridgeAndPreservesSelections(t *testing.T) {
	ctx := context.Background()
	s, pong := catalogServiceFixture(t)
	s.kitHost = true
	s.uploadTimeout = time.Minute
	root := catalog.Root{ID: "sms", System: protocol.SystemSMS, Path: t.TempDir()}
	path := filepath.Join(root.Path, "Cartridge.sms")
	if err := os.WriteFile(path, bytes.Repeat([]byte{1}, 1024), 0600); err != nil {
		t.Fatal(err)
	}
	store := s.catalog.(*catalog.Store)
	scanner := catalog.Scanner{Store: store}
	if _, err := scanner.Scan(ctx, []catalog.Root{root}); err != nil {
		t.Fatal(err)
	}
	s.rootsByID = map[string]catalog.Root{root.ID: root}
	s.preparer = &romsource.Preparer{StagingRoot: t.TempDir()}
	games, err := store.Games(ctx)
	if err != nil || len(games) != 1 {
		t.Fatalf("games=%+v err=%v", games, err)
	}
	gameID := games[0].ID
	raw := sizedSetupArchive(t, "fes.sms", 1024)
	pkg, _, err := s.corePackages.Import(ctx, int64(len(raw)), bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	s.kitHostInventory = func(context.Context) ([]localcores.Core, error) {
		return []localcores.Core{{CoreID: "fes.sms", PackageID: pkg.PackageID}, {CoreID: "fes.pong", PackageID: pong}}, nil
	}
	type preparedResult struct {
		entry catalog.CoreEntry
		err   error
	}
	results := make(chan preparedResult, 6)
	for i := 0; i < cap(results); i++ {
		go func() {
			entry, err := s.KitHostEntry(ctx, pkg.PackageID, gameID)
			results <- preparedResult{entry, err}
		}()
	}
	var first catalog.CoreEntry
	for i := 0; i < cap(results); i++ {
		got := <-results
		if got.err != nil {
			t.Fatal(got.err)
		}
		if i == 0 {
			first = got.entry
		} else if got.entry.GameID != first.GameID {
			t.Fatalf("concurrent requests created different entries: %+v %+v", first, got.entry)
		}
	}
	if first.Title != "Cartridge" {
		t.Fatalf("title %q", first.Title)
	}
	if got, err := store.Game(ctx, first.GameID); err != nil || got.ID != first.GameID {
		t.Fatalf("direct helper lookup=%+v err=%v", got, err)
	}
	for _, grouped := range []bool{false, true} {
		page, err := s.QueryGames(ctx, catalog.Query{Text: "Cartridge", Grouped: grouped, Limit: 20})
		if err != nil || len(page.Games) != 1 || page.Games[0].ID != gameID {
			t.Fatalf("library grouped=%v page=%+v err=%v", grouped, page, err)
		}
	}
	users, err := libraryuser.Open(filepath.Join(t.TempDir(), "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = users.Close() })
	s.users = users
	if err := s.RecordPlay(ctx, first.GameID); err != nil {
		t.Fatal(err)
	}
	recent, err := s.QueryGames(ctx, catalog.Query{Collection: "recents", Limit: 20})
	if err != nil || len(recent.Games) != 1 || recent.Games[0].ID != gameID {
		t.Fatalf("recent=%+v err=%v", recent, err)
	}

	binding, err := s.CoreEntryROM(ctx, first.GameID)
	if err != nil || binding.MediaID == "" {
		t.Fatalf("binding=%+v err=%v", binding, err)
	}
	again, err := s.KitHostEntry(ctx, pkg.PackageID, gameID)
	if err != nil || again.GameID != first.GameID {
		t.Fatalf("repeat=%+v err=%v", again, err)
	}
	if _, err := s.KitHostEntry(ctx, pong, gameID); err == nil {
		t.Fatal("accepted non-SMS package")
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte{2}, 1024), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.KitHostEntry(ctx, pkg.PackageID, gameID); err == nil {
		t.Fatal("accepted file changed since scan")
	}
	if _, err := scanner.Scan(ctx, []catalog.Root{root}); err != nil {
		t.Fatal(err)
	}
	changed, err := s.KitHostEntry(ctx, pkg.PackageID, gameID)
	if err != nil || changed.GameID == first.GameID || changed.Title != "Cartridge (2)" {
		t.Fatalf("changed=%+v err=%v", changed, err)
	}
	prior, err := s.CoreEntryROM(ctx, first.GameID)
	if err != nil || prior.MediaID != binding.MediaID {
		t.Fatalf("prior=%+v err=%v", prior, err)
	}
	changedBinding, err := s.CoreEntryROM(ctx, changed.GameID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectCoreEntryROM(ctx, first.GameID, pkg.PackageID, binding.ROMID, binding.MediaID, changedBinding.MediaID); err != nil {
		t.Fatal(err)
	}
	visible, err := store.Games(ctx)
	if err != nil {
		t.Fatal(err)
	}
	foundEdited := false
	for _, game := range visible {
		if game.ID == first.GameID {
			foundEdited = true
		}
		if game.ID == changed.GameID {
			t.Fatal("unchanged helper visible")
		}
	}
	if !foundEdited {
		t.Fatal("edited helper did not become visible")
	}

	if err := os.WriteFile(path, bytes.Repeat([]byte{1}, 1024), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := scanner.Scan(ctx, []catalog.Root{root}); err != nil {
		t.Fatal(err)
	}
	replacement, err := s.KitHostEntry(ctx, pkg.PackageID, gameID)
	if err != nil || replacement.GameID == first.GameID {
		t.Fatalf("replacement=%+v err=%v", replacement, err)
	}
	customized, err := s.CoreEntryROM(ctx, first.GameID)
	if err != nil || customized.MediaID != changedBinding.MediaID {
		t.Fatalf("user edit overwritten: %+v %v", customized, err)
	}
	// A room selection that has changed must not start an old retained package.
	s.kitHostInventory = func(context.Context) ([]localcores.Core, error) {
		return []localcores.Core{{CoreID: "fes.sms", PackageID: strings.Repeat("e", 64)}}, nil
	}
	if _, err := s.KitHostEntry(ctx, pkg.PackageID, gameID); err == nil {
		t.Fatal("accepted stale selected package")
	}
}

func TestKitHostEntryReusesBlankCore(t *testing.T) {
	s, pkg := catalogServiceFixture(t)
	s.kitHost = true
	s.kitHostInventory = func(context.Context) ([]localcores.Core, error) {
		return []localcores.Core{{CoreID: "fes.pong", PackageID: pkg}}, nil
	}
	inspection, err := s.installedCoreInspection(context.Background(), pkg)
	if err != nil {
		t.Fatal(err)
	}
	userEntry, err := s.CreateCoreEntry(context.Background(), inspection.Descriptor.Core.Name, pkg)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.KitHostEntry(context.Background(), pkg, "")
	if err != nil {
		t.Fatal(err)
	}
	if first.GameID == userEntry.GameID {
		t.Fatal("adopted independently configured user entry")
	}
	again, err := s.KitHostEntry(context.Background(), pkg, "")
	if err != nil || first.GameID != again.GameID {
		t.Fatalf("first=%+v again=%+v err=%v", first, again, err)
	}
	s.kitHost = false
	if _, err := s.KitHostEntry(context.Background(), pkg, ""); err == nil {
		t.Fatal("accepted request outside kit mode")
	}
}

func TestSelectedKitSMSPackage(t *testing.T) {
	id := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name  string
		cores []localcores.Core
		want  string
	}{
		{"selected", []localcores.Core{{CoreID: "fes.pong", PackageID: strings.Repeat("b", 64)}, {CoreID: "fes.sms", PackageID: id}}, id},
		{"absent", nil, ""},
		{"unavailable", []localcores.Core{{CoreID: "fes.sms", PackageID: id, Block: "Hardware unavailable"}}, ""},
		{"ambiguous", []localcores.Core{{CoreID: "fes.sms", PackageID: id}, {CoreID: "fes.sms", PackageID: strings.Repeat("b", 64)}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := selectedKitSMSPackage(tc.cores)
			if got != tc.want || (err != nil) != (tc.want == "") {
				t.Fatalf("selected=%q err=%v", got, err)
			}
		})
	}
}

func TestKitHostEntrySkipsRetainedUserTitle(t *testing.T) {
	ctx := context.Background()
	s, pkg := catalogServiceFixture(t)
	s.kitHost = true
	s.kitHostInventory = func(context.Context) ([]localcores.Core, error) {
		return []localcores.Core{{CoreID: "fes.pong", PackageID: pkg}}, nil
	}
	inspection, err := s.installedCoreInspection(ctx, pkg)
	if err != nil {
		t.Fatal(err)
	}
	title := kitHostEntryTitle(inspection.Descriptor.Core.Name, "fes.pong")
	store := s.catalog.(*catalog.Store)
	user, err := store.CreateCoreEntry(ctx, title, "fes.pong", pkg)
	if err != nil {
		t.Fatal(err)
	}
	media, _, err := store.ImportCoreMedia(ctx, []byte{0, 10, 0x76})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertZX81Save(ctx, user.GameID, media.MediaID, "Retained save"); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveCoreEntry(ctx, user.GameID); err != nil {
		t.Fatal(err)
	}
	helper, err := s.KitHostEntry(ctx, pkg, "")
	if err != nil {
		t.Fatal(err)
	}
	if helper.GameID == user.GameID || helper.Title != title+" (2)" {
		t.Fatalf("helper claimed retained title: %+v", helper)
	}
	saves, err := store.ListZX81Saves(ctx, user.GameID)
	if err != nil || len(saves) != 1 || saves[0].Name != "Retained save" {
		t.Fatalf("retained saves=%+v err=%v", saves, err)
	}
}
