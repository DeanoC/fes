package catalog

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestZX81SaveRoundTrip(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	gameID := GameID(CorePlatform, corePackageLibraryID, "fes.zx81", "ZX81")
	if _, err := store.db.ExecContext(ctx, `INSERT INTO libraries(id,system,root,online) VALUES (?,?,?,1)`, corePackageLibraryID, CorePlatform, corePackageRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO games(game_id,library_id,system,relative_path,title,source_kind,source_state,source_size,modified_ns,seen_generation,search_text,group_key,first_seen_ns) VALUES (?,?,?,?,?,?,?,0,0,0,?,?,1)`,
		gameID, corePackageLibraryID, CorePlatform, "fes.zx81", "ZX81", SourceKindCorePackage, SourceStateAvailable, gameID, gameID); err != nil {
		t.Fatal(err)
	}
	media, _, err := store.ImportCoreMedia(ctx, []byte{0x00, 0x0a, 0x76})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertZX81Save(ctx, gameID, media.MediaID, "Saved program"); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertZX81Save(ctx, gameID, media.MediaID, "Saved program again"); err != nil {
		t.Fatal(err)
	}
	saves, err := store.ListZX81Saves(ctx, gameID)
	if err != nil || len(saves) != 1 || saves[0].MediaID != media.MediaID || saves[0].Name != "Saved program" || saves[0].Size != 3 {
		t.Fatalf("saves = %+v %v", saves, err)
	}
	if _, err := store.ListZX81Saves(ctx, "Bad Game"); err == nil {
		t.Fatal("invalid game id was accepted")
	}
	if err := store.InsertZX81Save(ctx, gameID, strings.Repeat("ab", 32), "Saved program"); err == nil {
		t.Fatal("missing media was stored")
	}
}
