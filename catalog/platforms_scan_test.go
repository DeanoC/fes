package catalog

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/DeanoC/FogCast-POC/internal/core"
)

func TestScannerUsesHostPlatformRegistryForBrowseOnlySystems(t *testing.T) {
	ctx := context.Background()
	rootPath := t.TempDir()
	mustWriteScannerFile(t, filepath.Join(rootPath, "mario.nes"), []byte("mario"))
	store := openScannerStore(t)
	root := Root{ID: "nes-main", System: "nes", Path: rootPath}
	scanner := Scanner{Store: store, Registry: core.DefaultRegistry(), Platforms: DefaultPlatforms()}
	report, err := scanner.Scan(ctx, []Root{root})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Roots) != 1 || report.Roots[0].Added != 1 {
		t.Fatalf("report = %+v", report)
	}
	games, err := store.Games(ctx)
	if err != nil || len(games) != 1 || games[0].System != "nes" {
		t.Fatalf("games = %+v, %v", games, err)
	}
}

func TestScannerRejectsUnknownPlatforms(t *testing.T) {
	ctx := context.Background()
	store := openScannerStore(t)
	root := Root{ID: "mystery-main", System: "mystery", Path: t.TempDir()}
	scanner := Scanner{Store: store, Registry: core.DefaultRegistry(), Platforms: DefaultPlatforms()}
	if _, err := scanner.Scan(ctx, []Root{root}); err == nil {
		t.Fatal("Scan(unknown platform) succeeded")
	}
}
