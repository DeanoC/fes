package tenfoot

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTenfootDoesNotKeepNeutralCompatibilitySurfaces(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(filename)

	for _, name := range []string{"shared_compat.go", "covercache_compat.go"} {
		_, err := os.Stat(filepath.Join(dir, name))
		if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("compatibility file %s still exists", name)
		}
	}

	sessionPath := filepath.Clean(filepath.Join(dir, "..", "..", "hostclient", "session.go"))
	sessionSource, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read hostclient session source: %v", err)
	}
	if strings.Contains(string(sessionSource), "LaunchResult") {
		t.Errorf("%s retains historical LaunchResult compatibility surface", sessionPath)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || entry.Name() == "compat_migration_test.go" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", entry.Name(), err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.TypeSpec:
				if node.Assign.IsValid() {
					t.Errorf("%s:%d retains type alias %s", entry.Name(), fset.Position(node.Pos()).Line, node.Name.Name)
				}
			case *ast.FuncDecl:
				if legacyNeutralFunction[node.Name.Name] {
					t.Errorf("%s:%d retains neutral wrapper %s", entry.Name(), fset.Position(node.Pos()).Line, node.Name.Name)
				}
			}
			return true
		})
	}
}

func TestTenfootDoesNotDuplicateHostclientLibraryHelpers(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(filename), "client.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	forbidden := map[string]bool{
		"catalogSortParam": true,
		"preferLaunchable": true,
	}
	ast.Inspect(file, func(node ast.Node) bool {
		decl, ok := node.(*ast.FuncDecl)
		if ok && forbidden[decl.Name.Name] {
			t.Errorf("%s:%d duplicates hostclient library helper %s", path, fset.Position(decl.Pos()).Line, decl.Name.Name)
		}
		return true
	})
}

func TestTenfootLaunchBlockReasonMapsHostclientOnly(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(filename), "app.go")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if !strings.Contains(text, "game.LaunchBlock()") {
		t.Error("tenfoot launchBlockReason must map hostclient.Game.LaunchBlock")
	}
	for _, needle := range []string{"game.Launchable", "game.RootOnline", "game.State"} {
		if strings.Contains(text, needle) {
			t.Errorf("tenfoot app.go still inspects %s for launch admission", needle)
		}
	}
}

var legacyNeutralFunction = map[string]bool{
	"CoverHandle":            true,
	"LogoHandle":             true,
	"MarqueeHandle":          true,
	"Box3DHandle":            true,
	"AttractMarqueeHandle":   true,
	"BackdropHandle":         true,
	"VideoHandle":            true,
	"AttractPreviewHandles":  true,
	"DetailPreviewHandles":   true,
	"NewCoverCache":          true,
	"NewStillCache":          true,
	"PageHandles":            true,
	"DecodeCover":            true,
	"DecodeStill":            true,
	"DecodeScreenshot":       true,
	"CoverDestRect":          true,
	"CoverFillRect":          true,
	"PortableSystem":         true,
	"PrefetchOrder":          true,
	"PageIDs":                true,
	"CollectCoverHandles":    true,
	"CollectLogoHandles":     true,
	"CollectBox3DHandles":    true,
	"CollectBackdropHandles": true,
	"SeriesName":             true,
	"RelatedIDs":             true,
	"CollectionID":           true,
	"SeriesMates":            true,
	"GameDetail":             true,
	"normalizeHandle":        true,
	"screenshotHandles":      true,
	"coverDestRect":          true,
	"maskSecret":             true,
	"sanitizeFieldText":      true,
	"oskHint":                true,
}
