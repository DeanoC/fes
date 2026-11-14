package fogcast

import (
	"bytes"
	"context"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/misteross/expansion"
)

type countingVideoReadinessCatalog struct {
	*catalog.Store
	lists, reads int
	damaged      string
}

func (c *countingVideoReadinessCatalog) CoreVideoParts(ctx context.Context) ([]catalog.CoreVideoPart, error) {
	c.lists++
	return c.Store.CoreVideoParts(ctx)
}

func (c *countingVideoReadinessCatalog) ReadCoreVideoPart(ctx context.Context, id string) (expansion.Asset, error) {
	c.reads++
	if id == c.damaged {
		return expansion.Asset{}, catalog.ErrInvalidCoreVideoPart
	}
	return c.Store.ReadCoreVideoPart(ctx, id)
}

func TestVideoReadinessMatchesExactShellAndCurrentProfile(t *testing.T) {
	ctx := context.Background()
	raw, assets := libraryVideoFixtureLayout(t, expansion.ColecoNativeVideoLayout)
	s, _, entry, _ := newCoreEntryLaunchFixture(t, raw, "Native video")
	second, err := s.CreateCoreEntry(ctx, "Second title", entry.PackageID)
	if err != nil {
		t.Fatal(err)
	}
	store := &countingVideoReadinessCatalog{Store: s.catalog.(*catalog.Store)}
	s.catalog = store
	check := func(want bool) {
		t.Helper()
		store.lists, store.reads = 0, 0
		got, err := s.CoreCompositions(ctx, []string{entry.GameID, second.GameID})
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{entry.GameID, second.GameID} {
			if comp := got[id]; !comp.VideoRequired || comp.VideoReady != want {
				t.Fatalf("%s video readiness = %+v, want %v", id, comp, want)
			}
		}
		if store.lists != 1 || store.reads > 1 {
			t.Fatalf("listing reread shared video part: lists=%d reads=%d", store.lists, store.reads)
		}
	}
	check(false)
	// A part installed for a different shell cannot make this title ready.
	raster, foreign := libraryVideoFixture(t)
	if _, _, err := s.ImportCorePackage(ctx, int64(len(raster)), bytes.NewReader(raster)); err != nil {
		t.Fatal(err)
	}
	importVideoFixture(t, s, foreign[0], "direct")
	check(false)
	scanlines := importVideoFixture(t, s, assets[1], "scanlines")
	// A Scanlines part does not replace explicitly selected Direct.
	check(false)
	profile := "scanlines"
	if err := s.PatchLibrarySettings(ctx, LibraryConfigPatch{VideoProfile: &profile}); err != nil {
		t.Fatal(err)
	}
	check(true)
	importVideoFixture(t, s, assets[0], "direct")
	store.damaged = scanlines.PartID
	// An installed but damaged preferred part must not silently fall back.
	check(false)
	profile = "direct"
	if err := s.PatchLibrarySettings(ctx, LibraryConfigPatch{VideoProfile: &profile}); err != nil {
		t.Fatal(err)
	}
	check(true)
}

func TestVideoReadinessBuiltinAndDirectFallback(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		layout  string
		builtin bool
	}{
		{expansion.ColecoVideoLayout, true},
		{expansion.ColecoNativeVideoLayout, false},
	} {
		t.Run(tc.layout, func(t *testing.T) {
			raw, assets := libraryVideoFixtureLayout(t, tc.layout)
			s, _, entry, _ := newCoreEntryLaunchFixture(t, raw, "Fallback")
			profile := "scanlines"
			if err := s.PatchLibrarySettings(ctx, LibraryConfigPatch{VideoProfile: &profile}); err != nil {
				t.Fatal(err)
			}
			check := func(want bool) {
				t.Helper()
				got, err := s.CoreCompositions(ctx, []string{entry.GameID})
				if err != nil || got[entry.GameID].VideoRequired != true || got[entry.GameID].VideoReady != want {
					t.Fatalf("composition=%+v err=%v, want video=%v", got[entry.GameID], err, want)
				}
			}
			check(tc.builtin)
			importVideoFixture(t, s, assets[0], "direct")
			check(true)
		})
	}
}
