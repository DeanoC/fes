package fogcast

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/DeanoC/FogCast-POC/catalog"
	"github.com/DeanoC/FogCast-POC/libraryuser"
	"github.com/DeanoC/FogCast-POC/protocol"
)

func TestQueryGamesMapsMalformedCursorToBadRequest(t *testing.T) {
	store := &fakeServiceCatalog{queryErr: catalog.ErrInvalidQuery}
	service := newTestService(store, &fakeServicePreparer{}, &fakeServiceClient{})
	_, err := service.QueryGames(context.Background(), catalog.Query{Cursor: "not-a-cursor", Limit: 10})
	assertServiceErrorCode(t, err, protocol.CodeBadRequest)
}

func TestQueryRecentsAppliesTextFilterAndOpaqueCursor(t *testing.T) {
	ctx := context.Background()
	users, err := libraryuser.Open(filepath.Join(t.TempDir(), "user.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = users.Close() })
	alpha := catalog.Game{
		ID: "snes-alpha-test", Title: "Alpha", System: protocol.SystemSNES,
		Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, RootOnline: true,
	}
	sonic := catalog.Game{
		ID: "snes-sonic-test", Title: "Sonic", System: protocol.SystemSNES,
		Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, RootOnline: true,
	}
	mega := catalog.Game{
		ID: "megadrive-sonic-test", Title: "Sonic MD", System: protocol.SystemMegaDrive,
		Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, RootOnline: true,
	}
	store := &fakeServiceCatalog{games: []catalog.Game{alpha, sonic, mega}}
	service := newService(
		Config{Libraries: []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: "/private/library"}}, RequestTimeout: time.Second, UploadTimeout: 2 * time.Second},
		Paths{Staging: "/private/staging"}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{},
		WithUserLibrary(users),
	)
	if err := users.RecordPlay(ctx, alpha.ID); err != nil {
		t.Fatal(err)
	}
	if err := users.RecordPlay(ctx, sonic.ID); err != nil {
		t.Fatal(err)
	}
	if err := users.RecordPlay(ctx, mega.ID); err != nil {
		t.Fatal(err)
	}
	page, err := service.QueryGames(ctx, catalog.Query{Collection: "recents", Text: "Sonic", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Games) != 1 || page.Games[0].ID != mega.ID || page.NextCursor == "" || page.NextCursor == mega.ID {
		t.Fatalf("recents page = %+v", page)
	}
	next, err := service.QueryGames(ctx, catalog.Query{Collection: "recents", Text: "Sonic", Limit: 1, Cursor: page.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Games) != 1 || next.Games[0].ID != sonic.ID {
		t.Fatalf("recents next = %+v", next)
	}
	_, err = service.QueryGames(ctx, catalog.Query{Collection: "recents", Cursor: "not-a-cursor"})
	assertServiceErrorCode(t, err, protocol.CodeBadRequest)
}
