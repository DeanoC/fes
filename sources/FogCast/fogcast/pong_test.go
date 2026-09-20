package fogcast

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

func TestOpenDiscoversROMlessPongWithoutLibrary(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	config := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(config, []byte("selected_target='local'\nrequest_timeout_seconds=12\nupload_timeout_seconds=60\n[[targets]]\nname='local'\nenabled=false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	service, err := Open(ctx, Paths{Config: config, Index: filepath.Join(dir, "index.sqlite3"), Staging: filepath.Join(dir, "staging")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	for _, grouped := range []bool{false, true} {
		page, err := service.QueryGames(ctx, catalog.Query{Text: "Pong", Platform: "pong", Grouped: grouped, Availability: catalog.AvailabilityReady})
		if err != nil || len(page.Games) != 1 {
			t.Fatalf("discover grouped=%v: %#v %v", grouped, page, err)
		}
		game := page.Games[0]
		if game.ID != "pong" || game.Kind != "builtin" || game.Content != nil || game.RelativePath != "" {
			t.Fatalf("not a ROMless built-in: %#v", game)
		}
	}
	if err := service.retireSupersededLibraries(ctx); err != nil {
		t.Fatal(err)
	}
	if game, err := service.Game(ctx, "pong"); err != nil || game.State != catalog.SourceStateAvailable {
		t.Fatalf("scan retirement removed Pong: %#v %v", game, err)
	}
}

func TestServicePongUsesExistingDirectLaunchWithoutContent(t *testing.T) {
	game := catalog.Game{ID: "pong", Title: "Pong", LibraryID: "builtin-pong", System: "pong", Kind: "builtin", State: catalog.SourceStateAvailable, RootOnline: true}
	store := &fakeServiceCatalog{games: []catalog.Game{game}}
	preparer := &fakeServicePreparer{}
	client := &fakeServiceClient{nativeLaunch: func(_ context.Context, request protocol.LaunchRequest) (protocol.Status, error) {
		if request.GameID != "pong" || request.System != "pong" || request.ROMPath != "" {
			t.Fatalf("request = %#v", request)
		}
		expected := "Pong"
		return protocol.Status{State: protocol.StateActive, GameID: &request.GameID, System: &request.System, ExpectedCore: &expected, ObservedCore: &expected}, nil
	}}
	service := newTestService(store, preparer, client)
	response, err := service.Launch(context.Background(), "pong", nil)
	if err != nil || response.Status.State != protocol.StateActive {
		t.Fatalf("launch=%#v %v", response, err)
	}
	if client.nativeLaunchCalls != 1 || client.probeCalls != 0 || client.uploadCalls != 0 || client.launchCalls != 0 || preparer.calls != 0 {
		t.Fatal("built-in touched content path")
	}
}

func TestPongCannotBypassMediaChecksByClaimingBuiltinKind(t *testing.T) {
	for _, game := range []catalog.Game{
		{ID: "pong", System: "pong", Kind: "raw", LibraryID: "builtin-pong"},
		{ID: "pong-other", System: "pong", Kind: "builtin", LibraryID: "builtin-pong"},
		{ID: "pong", System: "pong", Kind: "builtin", LibraryID: "builtin-pong", RelativePath: "game.bin"},
		{ID: "pong", System: "pong", Kind: "builtin", LibraryID: "builtin-pong", Content: &catalog.Content{SHA256: serviceDigest, Size: 1, Extension: "bin"}},
	} {
		client := &fakeServiceClient{}
		service := newTestService(&fakeServiceCatalog{games: []catalog.Game{game}}, &fakeServicePreparer{}, client)
		if _, err := service.Launch(context.Background(), game.ID, nil); err == nil {
			t.Fatalf("accepted forged builtin: %#v", game)
		}
		if client.nativeLaunchCalls != 0 || client.probeCalls != 0 {
			t.Fatal("forged builtin reached target")
		}
	}
}
