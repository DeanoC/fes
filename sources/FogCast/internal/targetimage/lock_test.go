package targetimage_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/internal/targetimage"
)

const validSourceLock = `format = 1

[container]
image = 'docker.io/library/debian:12.11-slim'
platform = 'linux/amd64'
digest = 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'

[buildroot]
version = '2021.02.4'
commit = '004a792dcf10e6c474070c9571f7504411e786cc'

[image_creator]
commit = '8aba321b2162e54b56522aa30758b22d97eec8da'
rootfs_sha256 = '65d968c566e5971debd6f822ebf13cc4555ad1bd69fa4a81fa30367f2323c344'
modules_sha256 = '62086a04e09b98162cc5db6d4ac607a2fdb41bbec02035f3318074a85ab9f2fe'
kernel_sha256 = 'a6c7b1be0da9ba24a91bc1816737915d6a6cfba27c6c3025caded95167dc8dae'

[kernel]
commit = 'd7adb20b4ca595838289406c083fff78f004a8c3'
defconfig = 'MiSTer_defconfig'
dtb = 'socfpga_cyclone5_de10_nano.dtb'
release = '5.15.1-MiSTer'
`

func TestLoadSourceLockValidatesImmutableInputs(t *testing.T) {
	t.Parallel()
	lock, err := targetimage.LoadSourceLock(writeFixture(t, "sources.toml", validSourceLock))
	if err != nil {
		t.Fatal(err)
	}
	if lock.Container.Platform != "linux/amd64" || lock.Buildroot.Commit != "004a792dcf10e6c474070c9571f7504411e786cc" {
		t.Fatalf("lock = %#v", lock)
	}
}

func TestLoadSourceLockRejectsMutableOrMalformedSources(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"missing digest":     strings.Replace(validSourceLock, "digest = 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'\n", "", 1),
		"tag as commit":      strings.Replace(validSourceLock, "004a792dcf10e6c474070c9571f7504411e786cc", "2021.02.4", 1),
		"short commit":       strings.Replace(validSourceLock, "d7adb20b4ca595838289406c083fff78f004a8c3", "d7adb20", 1),
		"uppercase digest":   strings.Replace(validSourceLock, "sha256:aaaaaaaa", "sha256:AAAAAAAA", 1),
		"unknown field":      validSourceLock + "unexpected = true\n",
		"generated outputs":  validSourceLock + "\n[outputs]\ndev_rootfs_sha256 = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'\n",
		"mutable platform":   strings.Replace(validSourceLock, "linux/amd64", "linux/arm64", 1),
		"empty kernel input": strings.Replace(validSourceLock, "MiSTer_defconfig", "", 1),
	}
	for name, content := range tests {
		name, content := name, content
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := targetimage.LoadSourceLock(writeFixture(t, "sources.toml", content)); err == nil {
				t.Fatal("invalid source lock loaded")
			}
		})
	}
}

func TestWriteSourceLockRoundTripsStably(t *testing.T) {
	t.Parallel()
	loaded, err := targetimage.LoadSourceLock(writeFixture(t, "input.toml", validSourceLock))
	if err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(t.TempDir(), "first.toml")
	second := filepath.Join(t.TempDir(), "second.toml")
	if err := targetimage.WriteSourceLock(first, loaded); err != nil {
		t.Fatal(err)
	}
	roundTrip, err := targetimage.LoadSourceLock(first)
	if err != nil {
		t.Fatal(err)
	}
	if err := targetimage.WriteSourceLock(second, roundTrip); err != nil {
		t.Fatal(err)
	}
	firstBytes, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	secondBytes, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstBytes) != string(secondBytes) || !strings.HasSuffix(string(firstBytes), "\n") {
		t.Fatalf("unstable output:\nfirst:\n%s\nsecond:\n%s", firstBytes, secondBytes)
	}
}

func TestVerifyFileChecksSizeAndDigest(t *testing.T) {
	t.Parallel()
	path := writeFixture(t, "input.bin", "accepted bytes")
	const digest = "af77f61c6d49263ab0a0e93e4f4245d7cc682159eb52579e04771271b322c7c7"
	if err := targetimage.VerifyFile(path, digest, 14); err != nil {
		t.Fatal(err)
	}
	if err := targetimage.VerifyFile(path, digest, 13); err == nil {
		t.Fatal("wrong size accepted")
	}
	if err := targetimage.VerifyFile(path, strings.Repeat("0", 64), 14); err == nil {
		t.Fatal("wrong digest accepted")
	}
}

func writeFixture(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
