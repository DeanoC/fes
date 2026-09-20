package fogcast

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/libraryuser"
	"github.com/DeanoC/FogCast/protocol"
)

func TestServiceEditionPreferencePersistsAcrossOpen(t *testing.T) {
	ctx := context.Background()
	users, err := libraryuser.Open(filepath.Join(t.TempDir(), "user.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = users.Close() })
	service := newService(
		Config{Libraries: []catalog.Root{{ID: "nes-main", System: protocol.SystemNES, Path: "/private/library"}}, RequestTimeout: time.Second, UploadTimeout: 2 * time.Second},
		Paths{Staging: "/private/staging"}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{},
		WithUserLibrary(users),
	)
	saved, err := service.SetEditionPreference(ctx, "Super Mario Bros.", "nes", "nes-smb-usa")
	if err != nil {
		t.Fatal(err)
	}
	if saved.GameID != "nes-smb-usa" || saved.Query != "super mario bros" {
		t.Fatalf("saved = %+v", saved)
	}
	got, err := service.EditionPreference(ctx, "Super Mario Bros", "NES")
	if err != nil || got.GameID != "nes-smb-usa" {
		t.Fatalf("get = %+v, %v", got, err)
	}
	listed, err := service.EditionPreferences(ctx)
	if err != nil || len(listed) != 1 || listed[0].GameID != "nes-smb-usa" {
		t.Fatalf("list = %+v, %v", listed, err)
	}
	if _, err := service.SetEditionPreference(ctx, "", "nes", "nes-smb-usa"); err == nil {
		t.Fatal("empty query should fail")
	} else {
		assertServiceErrorCode(t, err, protocol.CodeBadRequest)
	}
	if _, err := service.EditionPreference(ctx, "Zelda", "nes"); !errors.Is(err, libraryuser.ErrNotFound) {
		t.Fatalf("missing = %v", err)
	}
}

func TestServiceEditionPreferenceWithoutUserLibrary(t *testing.T) {
	ctx := context.Background()
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, &fakeServiceClient{})
	if _, err := service.SetEditionPreference(ctx, "Mario", "nes", "nes-smb-usa"); err == nil {
		t.Fatal("set without store should fail")
	}
	listed, err := service.EditionPreferences(ctx)
	if err != nil || len(listed) != 0 {
		t.Fatalf("list = %+v, %v", listed, err)
	}
}
