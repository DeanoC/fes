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
