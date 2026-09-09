package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/internal/corepackage"
	"github.com/DeanoC/FogCast/internal/targetimage"
)

func TestResolveWritesImmutableSourceLock(t *testing.T) {
	t.Parallel()
	output := filepath.Join(t.TempDir(), "target-image.sources.lock.toml")
	runner := func(name string, args ...string) ([]byte, error) {
		wantArgs := []string{"image", "inspect", "--format", "{{index .RepoDigests 0}}", "docker.io/library/debian:12.11-slim"}
		if name != "docker" || !reflect.DeepEqual(args, wantArgs) {
			return nil, errors.New("unexpected container command")
		}
		return []byte("docker.io/library/debian@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n"), nil
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"resolve", "--container-runtime", "docker", "--output", output}, &stdout, &stderr, runner); code != 0 {
		t.Fatalf("run returned %d: %s", code, stderr.String())
	}
	lock, err := targetimage.LoadSourceLock(output)
	if err != nil {
		t.Fatal(err)
	}
	if lock.Container.Digest != "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" ||
		lock.Buildroot.Commit != "004a792dcf10e6c474070c9571f7504411e786cc" ||
		lock.ImageCreator.KernelSHA256 != "a6c7b1be0da9ba24a91bc1816737915d6a6cfba27c6c3025caded95167dc8dae" ||
		lock.Kernel.Commit != "d7adb20b4ca595838289406c083fff78f004a8c3" {
		t.Fatalf("lock = %#v", lock)
	}
	if info, err := os.Stat(output); err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("lock mode = %v, err = %v", info.Mode().Perm(), err)
	}
}

func TestResolveRejectsUnpinnedContainerResult(t *testing.T) {
	t.Parallel()
	runner := func(string, ...string) ([]byte, error) {
		return []byte("docker.io/library/debian:12.11-slim\n"), nil
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"resolve", "--container-runtime", "docker", "--output", filepath.Join(t.TempDir(), "lock.toml")}, &stdout, &stderr, runner); code == 0 {
		t.Fatal("mutable container result accepted")
	}
}

func TestVerifyInputsChecksOfficialImageFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cache := filepath.Join(dir, "cache")
	creator := filepath.Join(cache, "image-creator")
	if err := os.MkdirAll(creator, 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"rootfs.tar.bz2": "rootfs",
		"modules.tar.gz": "modules",
		"zImage_dtb":     "kernel",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(creator, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	lockPath := filepath.Join(dir, "lock.toml")
	writeTestLock(t, lockPath, targetimage.ImageCreator{
		Commit:        "8aba321b2162e54b56522aa30758b22d97eec8da",
		RootFSSHA256:  "3c47ef972d531d524daa15fa33dd885dd23de6221bbd10a29eb42ecfcf2ef422",
		ModulesSHA256: "fbc6c1d4c3b6db8fb54278582eb1d965ed644e97509e130346ae130da5406cb3",
		KernelSHA256:  "6923dd1bc0460082c5d55a831908c24a282860b7f1cd6c2b79cf1bc8857c639c",
	})
	var stdout, stderr bytes.Buffer
	if code := run([]string{"verify-inputs", "--lock", lockPath, "--cache", cache}, &stdout, &stderr, nil); code != 0 {
		t.Fatalf("run returned %d: %s", code, stderr.String())
	}
	if err := os.WriteFile(filepath.Join(creator, "modules.tar.gz"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"verify-inputs", "--lock", lockPath, "--cache", cache}, &stdout, &stderr, nil); code == 0 {
		t.Fatal("changed input accepted")
	}
}

func TestCommandRejectsGeneratedOutputRecording(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "lock.toml")
	writeTestLock(t, lockPath, targetimage.ImageCreator{
		Commit:        "8aba321b2162e54b56522aa30758b22d97eec8da",
		RootFSSHA256:  "3c47ef972d531d524daa15fa33dd885dd23de6221bbd10a29eb42ecfcf2ef422",
		ModulesSHA256: "fbc6c1d4c3b6db8fb54278582eb1d965ed644e97509e130346ae130da5406cb3",
		KernelSHA256:  "6923dd1bc0460082c5d55a831908c24a282860b7f1cd6c2b79cf1bc8857c639c",
	})
	prod := filepath.Join(dir, "prod.img")
	dev := filepath.Join(dir, "dev.img")
	kernel := filepath.Join(dir, "zImage_dtb")
	for path, content := range map[string]string{prod: "prod", dev: "dev", kernel: "kernel"} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	args := []string{"record-outputs", "--lock", lockPath, "--prod", prod, "--dev", dev, "--kernel", kernel}
	if code := run(args, &stdout, &stderr, nil); code == 0 {
		t.Fatal("removed generated-output recording command was accepted")
	}
}

func TestSelectMegaDriveCommandWritesSourceBuiltSelection(t *testing.T) {
	t.Parallel()
	payload := []byte("cli source-built")
	digest := sha256.Sum256(payload)
	bundle := t.TempDir()
	manifest := fmt.Sprintf(`format = 1
abi = 'mister'
system = 'megadrive'
artifact = 'megadrive.rbf'
sha256 = '%s'
size = %d
repository = 'https://github.com/MiSTer-devel/MegaDrive_MiSTer'
revision = '7365a137cfd8fa6f041e964d8b953159c0ec42d9'
recipe = 'scripts/rebuild_core.py'
recipe_sha256 = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
toolchain = 'Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition'
`, hex.EncodeToString(digest[:]), len(payload))
	if err := os.WriteFile(filepath.Join(bundle, "megadrive.rbf"), payload, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "megadrive-rbf.toml"), []byte(manifest), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(bundle, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bundle, 0o755) })
	cache := filepath.Join(t.TempDir(), "cache")
	output := filepath.Join(cache, "megadrive.selection.toml")
	var stdout, stderr bytes.Buffer
	if code := run([]string{
		"select-megadrive", "--source", "source-built", "--bundle", bundle,
		"--cache", cache, "--output", output,
	}, &stdout, &stderr, nil); code != 0 {
		t.Fatalf("run returned %d: %s", code, stderr.String())
	}
	selectionBytes, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(selectionBytes), "origin = 'source-built'") || !strings.Contains(string(selectionBytes), hex.EncodeToString(digest[:])) {
		t.Fatalf("selection = %s", selectionBytes)
	}
}

func TestSelectMegaDriveCommandRequiresMatchingSourceFlags(t *testing.T) {
	t.Parallel()
	for name, args := range map[string][]string{
		"missing source":  {"select-megadrive", "--cache", t.TempDir(), "--output", filepath.Join(t.TempDir(), "selection.toml")},
		"source artifact": {"select-megadrive", "--source", "source-built", "--artifact", "/tmp/rbf", "--cache", t.TempDir(), "--output", filepath.Join(t.TempDir(), "selection.toml")},
		"upstream bundle": {"select-megadrive", "--source", "upstream", "--bundle", "/tmp/bundle", "--cache", t.TempDir(), "--output", filepath.Join(t.TempDir(), "selection.toml")},
		"unknown flag":    {"select-megadrive", "--source", "source-built", "--wat", "x"},
	} {
		name, args := name, args
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			if code := run(args, &stdout, &stderr, nil); code != 2 {
				t.Fatalf("run returned %d, stderr=%s", code, stderr.String())
			}
		})
	}
}

func TestVerifyPackagePrintsCanonicalBuildInputs(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "package")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	for source, destination := range map[string]string{
		filepath.Join("..", "..", "internal", "corepackage", "testdata", "core-bundle-v2", "manifests", "valid-basic.toml"): filepath.Join(directory, "manifest.toml"),
		filepath.Join("..", "..", "internal", "corepackage", "testdata", "core-bundle-v2", "payloads", "fes-fixture.rbf"):   filepath.Join(directory, "core.rbf"),
	} {
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, data, 0o444); err != nil {
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
	directoryWithID := filepath.Join(filepath.Dir(directory), inspection.PackageID)
	if err := os.Rename(directory, directoryWithID); err != nil {
		t.Fatal(err)
	}
	directory = directoryWithID
	record := filepath.Join(t.TempDir(), "fes-pong.package-selection.toml")
	recordBytes := []byte(fmt.Sprintf("format = 2\nkind = 'core-package'\ncore_id = 'fes.pong'\npackage_id = '%s'\npayload_sha256 = '%s'\nmisteross_revision = '%s'\nmister_packages_revision = '%s'\ninstall_path = '%s'\n",
		inspection.PackageID, inspection.Descriptor.Payload.SHA256, inspection.Descriptor.Build.Revision,
		strings.Repeat("a", 40), "/usr/share/mister-runtime/core-packages/"+inspection.PackageID))
	if err := os.WriteFile(record, recordBytes, 0o444); err != nil {
		t.Fatal(err)
	}
	wantRecordSHA := fmt.Sprintf("%x", sha256.Sum256(recordBytes))
	var stdout, stderr bytes.Buffer
	if code := run([]string{"verify-package", "--package", directory, "--selection", record, "--print-inputs"}, &stdout, &stderr, nil); code != 0 {
		t.Fatalf("run returned %d: %s", code, stderr.String())
	}
	want := fmt.Sprintf("fes_pong_package_selection_sha256=%s\nfes_pong_package_id=%s\nfes_pong_payload_sha256=%s\nfes_pong_misteross_revision=%s\nfes_pong_mister_packages_revision=%s\nfes_pong_install_path=%s\n",
		wantRecordSHA, inspection.PackageID, inspection.Descriptor.Payload.SHA256,
		inspection.Descriptor.Build.Revision, strings.Repeat("a", 40),
		"/usr/share/mister-runtime/core-packages/"+inspection.PackageID)
	if stdout.String() != want {
		t.Fatalf("stdout=%q want=%q", stdout.String(), want)
	}
}

func writeTestLock(t *testing.T, path string, creator targetimage.ImageCreator) {
	t.Helper()
	lock := targetimage.Sources{
		Format: 1,
		Container: targetimage.Container{
			Image:    "docker.io/library/debian:12.11-slim",
			Platform: "linux/amd64",
			Digest:   "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		Buildroot:    targetimage.Buildroot{Version: "2021.02.4", Commit: "004a792dcf10e6c474070c9571f7504411e786cc"},
		ImageCreator: creator,
		Kernel: targetimage.Kernel{
			Commit:    "d7adb20b4ca595838289406c083fff78f004a8c3",
			Defconfig: "MiSTer_defconfig",
			DTB:       "socfpga_cyclone5_de10_nano.dtb",
			Release:   "5.15.1-MiSTer",
		},
	}
	if err := targetimage.WriteSourceLock(path, lock); err != nil {
		t.Fatal(err)
	}
}
