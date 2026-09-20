package catalog

import (
	"context"
	"path/filepath"
	"testing"
)

func TestScannerUsesHostPlatformRegistryForBrowseOnlySystems(t *testing.T) {
	ctx := context.Background()
	rootPath := t.TempDir()
	mustWriteScannerFile(t, filepath.Join(rootPath, "mario.gba"), []byte("mario"))
	store := openScannerStore(t)
	root := Root{ID: "gba-main", System: "gba", Path: rootPath}
	scanner := Scanner{Store: store, Platforms: DefaultPlatforms()}
	report, err := scanner.Scan(ctx, []Root{root})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Roots) != 1 || report.Roots[0].Added != 1 {
		t.Fatalf("report = %+v", report)
	}
	games, err := store.Games(ctx)
	if err != nil || len(games) != 1 || games[0].System != "gba" {
		t.Fatalf("games = %+v, %v", games, err)
	}
}

func TestScannerRejectsUnknownPlatforms(t *testing.T) {
	ctx := context.Background()
	store := openScannerStore(t)
	root := Root{ID: "mystery-main", System: "mystery", Path: t.TempDir()}
	scanner := Scanner{Store: store, Platforms: DefaultPlatforms()}
	if _, err := scanner.Scan(ctx, []Root{root}); err == nil {
		t.Fatal("Scan(unknown platform) succeeded")
	}
}

func TestPlatformTagsFlowFromSystemsTable(t *testing.T) {
	tags := PlatformTags("sms")
	found := false
	for _, tag := range tags {
		if tag == "vdp:tms9918-family" {
			found = true
		}
	}
	if !found {
		t.Fatalf("sms tags %v should include the TMS9918 family", tags)
	}
	if PlatformTags(CorePlatform) != nil {
		t.Fatal("the synthetic FPGA platform has no hardware tags")
	}
	tags[0] = "mutated"
	if PlatformTags("sms")[0] == "mutated" {
		t.Fatal("PlatformTags must not expose registry storage")
	}
}
