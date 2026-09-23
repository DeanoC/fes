package targetimage

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
)

func packageSelectionFixture(t *testing.T) (string, string, CorePackageSelection) {
	return packageSelectionFixtureForCore(t, "fes.pong")
}

func packageSelectionFixtureForCore(t *testing.T, coreID string) (string, string, CorePackageSelection) {
	t.Helper()
	return packageSelectionFixtureWithFormat(t, coreID, 2)
}

func packageSelectionFixtureWithFormat(t *testing.T, coreID string, format int) (string, string, CorePackageSelection) {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "package")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	members := []string{"manifest.toml", "core.rbf"}
	if format == 3 {
		members = append(members, "rom-map.json")
	}
	for _, name := range members {
		data, err := os.ReadFile(filepath.Join("..", "..", "corepackage", "testdata", fmt.Sprintf("core-bundle-v%d", format),
			map[string]string{"manifest.toml": "manifests/valid-basic.toml", "core.rbf": "payloads/fes-fixture.rbf", "rom-map.json": "maps/valid-basic.json"}[name]))
		if err != nil {
			t.Fatal(err)
		}
		if name == "manifest.toml" {
			data = []byte(strings.Replace(string(data), "fes.pong", coreID, 1))
		}
		if err := os.WriteFile(filepath.Join(directory, name), data, 0o444); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(directory, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(directory, 0o755) })
	inspection, err := corepackage.InspectPackage(directory)
	if err != nil {
		t.Fatal(err)
	}
	selection := CorePackageSelection{
		Format: 2, Kind: "core-package", CoreID: coreID, PackageID: inspection.PackageID,
		PayloadSHA256:          inspection.Descriptor.Payload.SHA256,
		MisterossRevision:      inspection.Descriptor.Build.Revision,
		MisterPackagesRevision: strings.Repeat("a", 40),
		InstallPath:            "/usr/share/mister-runtime/core-packages/" + inspection.PackageID,
	}
	record := filepath.Join(t.TempDir(), corePackageSelectionRecordNameForTest(coreID))
	data := fmt.Sprintf("format = 2\nkind = 'core-package'\ncore_id = '%s'\npackage_id = '%s'\npayload_sha256 = '%s'\nmisteross_revision = '%s'\nmister_packages_revision = '%s'\ninstall_path = '%s'\n",
		coreID, selection.PackageID, selection.PayloadSHA256, selection.MisterossRevision,
		selection.MisterPackagesRevision, selection.InstallPath)
	if err := os.WriteFile(record, []byte(data), 0o444); err != nil {
		t.Fatal(err)
	}
	return directory, record, selection
}

func corePackageSelectionRecordNameForTest(coreID string) string {
	switch coreID {
	case "fes.pong":
		return "fes-pong.package-selection.toml"
	case "fes.zx81":
		return "fes-zx81.package-selection.toml"
	case "fes.coleco":
		return "fes-coleco.package-selection.toml"
	default:
		panic("unsupported test core ID")
	}
}

func TestCorePackageSelectionSupportsSelectedFESPackageCores(t *testing.T) {
	for _, coreID := range []string{"fes.pong", "fes.zx81", "fes.coleco"} {
		t.Run(coreID, func(t *testing.T) {
			directory, record, selection := packageSelectionFixtureForCore(t, coreID)
			cache := t.TempDir()
			output := filepath.Join(cache, corePackageSelectionRecordNameForTest(coreID))
			got, err := PrepareCorePackageSelectionForCore(directory, record, cache, output, coreID)
			if err != nil {
				t.Fatal(err)
			}
			if got != selection {
				t.Fatalf("selection=%+v want=%+v", got, selection)
			}
			published := filepath.Join(cache, "core-packages", selection.PackageID)
			t.Cleanup(func() { _ = os.Chmod(published, 0o755) })
			if err := VerifyCorePackageSelectionForCore(published, output, coreID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCorePackageSelectionRejectsExpectedCoreMismatch(t *testing.T) {
	directory, record, _ := packageSelectionFixtureForCore(t, "fes.zx81")
	cache := t.TempDir()
	output := filepath.Join(cache, corePackageSelectionRecordNameForTest("fes.zx81"))
	if _, err := PrepareCorePackageSelectionForCore(directory, record, cache, output, "fes.pong"); err == nil {
		t.Fatal("selection accepted with a mismatched expected core ID")
	}
}

func TestCorePackageSelectionPreservesOtherCorePackages(t *testing.T) {
	pongDirectory, pongRecord, pong := packageSelectionFixtureForCore(t, "fes.pong")
	zx81Directory, zx81Record, zx81 := packageSelectionFixtureForCore(t, "fes.zx81")
	cache := t.TempDir()
	t.Cleanup(func() {
		_ = filepath.Walk(cache, func(path string, _ os.FileInfo, _ error) error { _ = os.Chmod(path, 0o755); return nil })
	})

	pongOutput := filepath.Join(cache, corePackageSelectionRecordNameForTest("fes.pong"))
	if _, err := PrepareCorePackageSelectionForCore(pongDirectory, pongRecord, cache, pongOutput, "fes.pong"); err != nil {
		t.Fatal(err)
	}
	zx81Output := filepath.Join(cache, corePackageSelectionRecordNameForTest("fes.zx81"))
	if _, err := PrepareCorePackageSelectionForCore(zx81Directory, zx81Record, cache, zx81Output, "fes.zx81"); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(filepath.Join(cache, "core-packages"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("package cache entries=%v", entries)
	}
	if err := VerifyCorePackageSelectionForCore(filepath.Join(cache, "core-packages", pong.PackageID), pongOutput, "fes.pong"); err != nil {
		t.Fatalf("Pong package was not preserved: %v", err)
	}
	if err := VerifyCorePackageSelectionForCore(filepath.Join(cache, "core-packages", zx81.PackageID), zx81Output, "fes.zx81"); err != nil {
		t.Fatalf("ZX81 package was not published: %v", err)
	}
}

func TestCorePackageSelectionFailedReplacementResealsCurrentPackage(t *testing.T) {
	directory, record, selection := packageSelectionFixtureForCore(t, "fes.pong")
	cache := t.TempDir()
	output := filepath.Join(cache, corePackageSelectionRecordNameForTest("fes.pong"))
	if _, err := PrepareCorePackageSelectionForCore(directory, record, cache, output, "fes.pong"); err != nil {
		t.Fatal(err)
	}
	packageRoot := filepath.Join(cache, "core-packages")
	t.Cleanup(func() {
		_ = filepath.Walk(cache, func(path string, _ os.FileInfo, _ error) error { _ = os.Chmod(path, 0o755); return nil })
	})
	injectedRename := func(oldPath, newPath string) error {
		if strings.HasSuffix(newPath, filepath.Join(".previous", "current-package")) {
			return fmt.Errorf("injected package backup rename failure")
		}
		return os.Rename(oldPath, newPath)
	}
	if _, err := prepareCorePackageSelectionForCoreWithRename(directory, record, cache, output, "fes.pong", injectedRename); err == nil {
		t.Fatal("replacement succeeded with a non-writable package root")
	}
	info, err := os.Stat(filepath.Join(packageRoot, selection.PackageID))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o555 {
		t.Fatalf("failed replacement left package mode %o, want 555", info.Mode().Perm())
	}
}

func TestCorePackageSelectionPublishesAndVerifiesExactClosedPair(t *testing.T) {
	directory, record, selection := packageSelectionFixture(t)
	cache := t.TempDir()
	t.Cleanup(func() {
		_ = filepath.Walk(cache, func(path string, _ os.FileInfo, _ error) error { _ = os.Chmod(path, 0o755); return nil })
	})
	output := filepath.Join(cache, "fes-pong.package-selection.toml")
	got, err := PrepareCorePackageSelection(directory, record, cache, output)
	if err != nil {
		t.Fatal(err)
	}
	if got != selection {
		t.Fatalf("selection=%+v want=%+v", got, selection)
	}
	published := filepath.Join(cache, "core-packages", selection.PackageID)
	t.Cleanup(func() { _ = os.Chmod(published, 0o755) })
	if err := VerifyCorePackageSelection(published, output); err != nil {
		t.Fatal(err)
	}
	verified, recordSHA256, err := InspectCorePackageSelection(published, output)
	if err != nil {
		t.Fatal(err)
	}
	if verified != selection {
		t.Fatalf("verified selection=%+v want=%+v", verified, selection)
	}
	for _, path := range []string{published, filepath.Join(published, "manifest.toml"), filepath.Join(published, "core.rbf"), output} {
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o222 != 0 {
			t.Fatalf("unsealed publication %s: mode=%v err=%v", path, info.Mode(), err)
		}
	}
	wantRecord, _ := os.ReadFile(record)
	gotRecord, _ := os.ReadFile(output)
	if string(gotRecord) != string(wantRecord) {
		t.Fatal("normalized selection bytes changed")
	}
	wantRecordSHA256 := fmt.Sprintf("%x", sha256.Sum256(gotRecord))
	if recordSHA256 != wantRecordSHA256 {
		t.Fatalf("record SHA-256=%s want=%s", recordSHA256, wantRecordSHA256)
	}
}

func TestCorePackageSelectionRejectsMalformedRecordAndPackage(t *testing.T) {
	for name, mutate := range map[string]func(string, string){
		"payload": func(_, record string) {
			_ = os.Chmod(record, 0o644)
			data, _ := os.ReadFile(record)
			_ = os.WriteFile(record, []byte(strings.Replace(string(data), "payload_sha256 = '", "payload_sha256 = 'ff", 1)), 0o444)
		},
		"unknown field": func(_, record string) {
			_ = os.Chmod(record, 0o644)
			file, _ := os.OpenFile(record, os.O_APPEND|os.O_WRONLY, 0)
			_, _ = file.WriteString("unexpected = true\n")
			_ = file.Close()
			_ = os.Chmod(record, 0o444)
		},
		"package stray": func(directory, _ string) {
			_ = os.Chmod(directory, 0o755)
			_ = os.WriteFile(filepath.Join(directory, "stray"), []byte("x"), 0o444)
			_ = os.Chmod(directory, 0o555)
		},
	} {
		t.Run(name, func(t *testing.T) {
			packageCopy, recordCopy, _ := packageSelectionFixture(t)
			mutate(packageCopy, recordCopy)
			cache := t.TempDir()
			if _, err := PrepareCorePackageSelection(packageCopy, recordCopy, cache,
				filepath.Join(cache, "fes-pong.package-selection.toml")); err == nil {
				t.Fatal("invalid package selection accepted")
			}
		})
	}
}

func TestCorePackageSelectionReplacementPreservesOldPairAndClosesChangedID(t *testing.T) {
	firstPackage, firstRecord, first := packageSelectionFixture(t)
	cache := t.TempDir()
	t.Cleanup(func() {
		_ = filepath.Walk(cache, func(path string, _ os.FileInfo, _ error) error { _ = os.Chmod(path, 0o755); return nil })
	})
	output := filepath.Join(cache, "fes-pong.package-selection.toml")
	if _, err := PrepareCorePackageSelection(firstPackage, firstRecord, cache, output); err != nil {
		t.Fatal(err)
	}

	invalidRecord := filepath.Join(t.TempDir(), "fes-pong.package-selection.toml")
	data, err := os.ReadFile(firstRecord)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), first.PayloadSHA256, strings.Repeat("f", 64), 1))
	if err := os.WriteFile(invalidRecord, data, 0o444); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareCorePackageSelection(firstPackage, invalidRecord, cache, output); err == nil {
		t.Fatal("invalid replacement was accepted")
	}
	if err := VerifyCorePackageSelection(filepath.Join(cache, "core-packages", first.PackageID), output); err != nil {
		t.Fatalf("failed replacement damaged old pair: %v", err)
	}

	secondPackage, secondRecord, _ := packageSelectionFixture(t)
	manifestPath := filepath.Join(secondPackage, "manifest.toml")
	if err := os.Chmod(secondPackage, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(manifestPath, 0o644); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest = []byte(strings.Replace(string(manifest), "Synthetic test-only core bundle fixture", "Changed synthetic core bundle fixture", 1))
	if err := os.WriteFile(manifestPath, manifest, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(manifestPath, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(secondPackage, 0o555); err != nil {
		t.Fatal(err)
	}
	secondInspection, err := corepackage.InspectPackage(secondPackage)
	if err != nil {
		t.Fatal(err)
	}
	secondData, err := os.ReadFile(secondRecord)
	if err != nil {
		t.Fatal(err)
	}
	secondData = []byte(strings.ReplaceAll(string(secondData), first.PackageID, secondInspection.PackageID))
	if err := os.Chmod(secondRecord, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondRecord, secondData, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(secondRecord, 0o444); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareCorePackageSelection(secondPackage, secondRecord, cache, output); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(cache, "core-packages"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != secondInspection.PackageID {
		t.Fatalf("package cache entries=%v", entries)
	}
	if err := VerifyCorePackageSelection(filepath.Join(cache, "core-packages", secondInspection.PackageID), output); err != nil {
		t.Fatal(err)
	}
}

func TestCorePackageSelectionPreservesSealedROMMap(t *testing.T) {
	directory, record, selection := packageSelectionFixtureWithFormat(t, "fes.pong", 3)
	cache := t.TempDir()
	t.Cleanup(func() { _ = removePath(cache) })
	output := filepath.Join(cache, corePackageSelectionName)
	if _, err := PrepareCorePackageSelection(directory, record, cache, output); err != nil {
		t.Fatal(err)
	}
	published := filepath.Join(cache, "core-packages", selection.PackageID)
	if err := VerifyCorePackageSelection(published, output); err != nil {
		t.Fatal(err)
	}
	want, _ := os.ReadFile(filepath.Join(directory, "rom-map.json"))
	path := filepath.Join(published, "rom-map.json")
	got, err := os.ReadFile(path)
	if err != nil || string(want) != string(got) {
		t.Fatalf("ROM map changed or missing: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0444 {
		t.Fatalf("ROM map not sealed: %v", err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyCorePackageSelection(published, output); err == nil {
		t.Fatal("writable ROM map accepted")
	}
	if err := os.WriteFile(path, append(got, ' '), 0444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0444); err != nil {
		t.Fatal(err)
	}
	if err := VerifyCorePackageSelection(published, output); err == nil {
		t.Fatal("tampered ROM map accepted")
	}
}

func TestCorePackageSelectionRejectsInvalidROMMembers(t *testing.T) {
	for _, tc := range []struct {
		name   string
		format int
		mutate func(string) error
	}{
		{"format2 with map", 2, func(dir string) error { return os.WriteFile(filepath.Join(dir, "rom-map.json"), []byte("{}"), 0444) }},
		{"format3 missing map", 3, func(dir string) error { return os.Remove(filepath.Join(dir, "rom-map.json")) }},
		{"format3 stray", 3, func(dir string) error { return os.WriteFile(filepath.Join(dir, "stray"), []byte("x"), 0444) }},
		{"format3 symlink map", 3, func(dir string) error {
			path := filepath.Join(dir, "rom-map.json")
			if err := os.Remove(path); err != nil {
				return err
			}
			return os.Symlink("manifest.toml", path)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directory, record, _ := packageSelectionFixtureWithFormat(t, "fes.pong", tc.format)
			if err := os.Chmod(directory, 0755); err != nil {
				t.Fatal(err)
			}
			if err := tc.mutate(directory); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(directory, 0555); err != nil {
				t.Fatal(err)
			}
			cache := t.TempDir()
			if _, err := PrepareCorePackageSelection(directory, record, cache, filepath.Join(cache, corePackageSelectionName)); err == nil {
				t.Fatal("invalid package members accepted")
			}
		})
	}
}
