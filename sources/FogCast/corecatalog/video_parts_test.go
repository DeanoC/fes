package corecatalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
)

func rewriteVideoCatalog(t *testing.T, edit func(map[string]any)) string {
	t.Helper()
	file := fixture(t, "packages/a.fcore")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	entry := doc["entries"].([]any)[0].(map[string]any)
	entry["video_parts"] = []any{map[string]any{
		"profile": "direct", "part_id": strings.Repeat("c", 64),
		"archive_path": "core-video-parts/direct.tar", "archive_sha256": strings.Repeat("d", 64), "archive_size": 2048,
	}}
	edit(entry)
	delete(doc, "catalog_sha256")
	data, err = json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	doc["catalog_sha256"] = fmt.Sprintf("%x", sha256.Sum256(data))
	data, err = json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestCatalogVideoCompanionsRetainExactReferences(t *testing.T) {
	file := rewriteVideoCatalog(t, func(map[string]any) {})
	c, err := Load(file)
	if err != nil {
		t.Fatal(err)
	}
	parts := c.Entries[0].VideoParts
	if len(parts) != 1 || parts[0].Profile != "direct" || parts[0].PartID != strings.Repeat("c", 64) || parts[0].ArchiveSize != 2048 {
		t.Fatalf("companions=%+v", parts)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte(`"profile":"direct"`), []byte(`"profile":"scanlines"`), 1)
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(file); err == nil {
		t.Fatal("companion metadata escaped the catalog digest")
	}
}

func TestCatalogRejectsMalformedVideoCompanions(t *testing.T) {
	for name, edit := range map[string]func(map[string]any){
		"unknown profile": func(p map[string]any) { p["profile"] = "crt" },
		"missing id":      func(p map[string]any) { delete(p, "part_id") },
		"escape":          func(p map[string]any) { p["archive_path"] = "../direct.tar" },
		"control path":    func(p map[string]any) { p["archive_path"] = "parts/dir\nect.tar" },
		"oversize":        func(p map[string]any) { p["archive_size"] = corepackage.MaxPayloadSize + 1 },
		"unknown field":   func(p map[string]any) { p["resource_budget"] = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			file := rewriteVideoCatalog(t, func(entry map[string]any) { edit(entry["video_parts"].([]any)[0].(map[string]any)) })
			if _, err := Load(file); err == nil {
				t.Fatal("accepted malformed companion")
			}
		})
	}
	for _, duplicate := range []string{"profile", "part_id", "archive_path"} {
		t.Run("duplicate "+duplicate, func(t *testing.T) {
			file := rewriteVideoCatalog(t, func(entry map[string]any) {
				first := entry["video_parts"].([]any)[0].(map[string]any)
				second := map[string]any{"profile": "scanlines", "part_id": strings.Repeat("e", 64),
					"archive_path": "parts/scanlines.tar", "archive_sha256": strings.Repeat("f", 64), "archive_size": 2048}
				second[duplicate] = first[duplicate]
				entry["video_parts"] = []any{first, second}
			})
			if _, err := Load(file); err == nil {
				t.Fatal("accepted duplicate companion mapping")
			}
		})
	}
	t.Run("unpublished shell", func(t *testing.T) {
		file := rewriteVideoCatalog(t, func(entry map[string]any) {
			for _, field := range []string{"package_id", "archive_path", "archive_sha256", "archive_size"} {
				delete(entry, field)
			}
		})
		if _, err := Load(file); err == nil {
			t.Fatal("accepted companions without a published exact shell")
		}
	})
}
