package corepackage

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestValidateDescriptorSharesPayloadIndependentManifestRules(t *testing.T) {
	base := filepath.Join("testdata", "core-bundle-v2")
	directory := writeDirectoryFromPaths(t,
		filepath.Join(base, "manifests", "valid-basic.toml"),
		filepath.Join(base, "payloads", "fes-fixture.rbf"))
	descriptor, err := Inspect(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDescriptor(descriptor); err != nil {
		t.Fatalf("valid descriptor: %v", err)
	}
	for name, mutate := range map[string]func(*Descriptor){
		"empty HTTPS authority":   func(value *Descriptor) { value.Build.Repository = "https://" },
		"credentialed repository": func(value *Descriptor) { value.Build.Repository = "https://user@example.invalid/repo" },
		"name control":            func(value *Descriptor) { value.Core.Name = "FES\x01Pong" },
		"toolchain control":       func(value *Descriptor) { value.Build.Toolchain = "tool\x01chain" },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := descriptor
			mutate(&invalid)
			if ValidateDescriptor(invalid) == nil {
				t.Fatal("invalid descriptor accepted")
			}
		})
	}
}

func TestKnownBoardTargetsThroughArchiveStore(t *testing.T) {
	manifest, payload := fixtureBytes(t, loadCases(t)[0])
	for _, tc := range []struct {
		name, platform, device, profile string
		valid                           bool
	}{
		{"de10", "de10_nano", "5CSEBA6U23I7", "fes-gp-v1", true},
		{"de25-computer", "de25_nano", "A5EB013BB23BE4SCS", "de25-hps-first-v1", true},
		{"de25-menu", "de25_nano", "A5EB013BB23BE4SCS", "de25-linux-menu-v1", true},
		{"de10-with-agilex", "de10_nano", "A5EB013BB23BE4SCS", "fes-gp-v1", false},
		{"de25-with-cyclone", "de25_nano", "5CSEBA6U23I7", "de25-hps-first-v1", false},
		{"unknown-agilex-part", "de25_nano", "A5EB013BB23BE4R1", "de25-hps-first-v1", false},
		{"unknown-board", "other_nano", "A5EB013BB23BE4SCS", "de25-hps-first-v1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			selected := bytes.ReplaceAll(manifest, []byte("de10_nano"), []byte(tc.platform))
			selected = bytes.ReplaceAll(selected, []byte("5CSEBA6U23I7"), []byte(tc.device))
			selected = bytes.ReplaceAll(selected, []byte("fes-gp-v1"), []byte(tc.profile))
			archive := canonicalArchive(selected, payload)
			root := t.TempDir()
			if err := os.Chmod(root, 0o700); err != nil {
				t.Fatal(err)
			}
			store, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			value, created, err := store.Import(context.Background(), int64(len(archive)), bytes.NewReader(archive))
			if !tc.valid {
				if err == nil || created {
					t.Fatalf("unsupported target imported: created=%v err=%v", created, err)
				}
				files, err := os.ReadDir(root)
				if err != nil || len(files) != 0 {
					t.Fatalf("rejected import left files: %v, %v", files, err)
				}
				return
			}
			if err != nil || !created {
				t.Fatalf("known target import: created=%v err=%v", created, err)
			}
			got, err := store.Inspect(context.Background(), value.PackageID)
			if err != nil || !reflect.DeepEqual(got, value) ||
				got.Descriptor.Target != (Target{Platform: tc.platform, Device: tc.device, ProgrammingProfile: tc.profile}) {
				t.Fatalf("known target readback: %#v, %v", got, err)
			}
		})
	}
}

func TestAdoptRetainsOnlyMatchingRootedPublicationsForCleanup(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join("testdata", "core-bundle-v2")
	manifest, err := os.ReadFile(filepath.Join(base, "manifests", "valid-basic.toml"))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(base, "payloads", "fes-fixture.rbf"))
	if err != nil {
		t.Fatal(err)
	}
	archive := canonicalArchive(manifest, payload)
	first, err := Stage(context.Background(), root, int64(len(archive)), bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Stage(context.Background(), root, int64(len(archive)), bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	alternateManifest := bytes.Replace(manifest, []byte(`version = "0.1.0"`), []byte(`version = "0.1.1"`), 1)
	thirdArchive := canonicalArchive(alternateManifest, payload)
	third, err := Stage(context.Background(), root, int64(len(thirdArchive)), bytes.NewReader(thirdArchive))
	if err != nil {
		t.Fatal(err)
	}
	owned, err := Adopt(root)
	if err != nil || len(owned) != 3 {
		t.Fatalf("owned=%d err=%v", len(owned), err)
	}
	seen := map[string]int{}
	for _, publication := range owned {
		seen[publication.PackageID]++
		if err := publication.Cleanup(); err != nil {
			t.Fatal(err)
		}
	}
	if seen[first.PackageID] != 2 || seen[third.PackageID] != 1 {
		t.Fatalf("adopted package IDs=%v", seen)
	}
	if _, err := os.Stat(first.Directory); !os.IsNotExist(err) {
		t.Fatalf("first remains: %v", err)
	}
	if _, err := os.Stat(second.Directory); !os.IsNotExist(err) {
		t.Fatalf("second remains: %v", err)
	}
	if _, err := os.Stat(third.Directory); !os.IsNotExist(err) {
		t.Fatalf("third remains: %v", err)
	}
}

func TestAdoptUsesTheVerifiedOpenedRootDuringInspection(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join("testdata", "core-bundle-v2")
	manifest, err := os.ReadFile(filepath.Join(base, "manifests", "valid-basic.toml"))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(base, "payloads", "fes-fixture.rbf"))
	if err != nil {
		t.Fatal(err)
	}
	archive := canonicalArchive(manifest, payload)
	original, err := Stage(context.Background(), root, int64(len(archive)), bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	moved := root + "-moved"
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	replacementManifest := bytes.Replace(manifest, []byte(`version = "0.1.0"`), []byte(`version = "0.1.1"`), 1)
	replacementArchive := canonicalArchive(replacementManifest, payload)
	replacement, err := Stage(context.Background(), root, int64(len(replacementArchive)), bytes.NewReader(replacementArchive))
	if err != nil {
		t.Fatal(err)
	}
	owned, err := adoptOpenedRoot(root, opened, rootInfo)
	if err != nil || len(owned) != 1 {
		t.Fatalf("owned=%d err=%v", len(owned), err)
	}
	for _, publication := range owned {
		if publication.PackageID != original.PackageID || !reflect.DeepEqual(publication.Descriptor, original.Descriptor) {
			t.Fatalf("publication=%#v", publication)
		}
	}
	if err := replacement.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(moved, root); err != nil {
		t.Fatal(err)
	}
	for _, publication := range owned {
		if err := publication.Cleanup(); err != nil {
			t.Fatal(err)
		}
	}
}

func writeDirectoryFromPaths(t *testing.T, manifestPath, payloadPath string) string {
	t.Helper()
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(payloadPath)
	if err != nil {
		t.Fatal(err)
	}
	return writeDirectory(t, manifest, payload)
}
