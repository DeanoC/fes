package imagepoc_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/internal/imagepoc"
)

const validPOC1B = `format = 1

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

func TestLoadPOC1AAcceptedHardwareLock(t *testing.T) {
	t.Parallel()
	lock, err := imagepoc.LoadPOC1A(filepath.Join("..", "..", "build", "sources.poc1a.lock.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if lock.Format != 1 || len(lock.Artifacts) != 7 || len(lock.Libraries) != 14 {
		t.Fatalf("lock summary = format %d, %d artifacts, %d libraries", lock.Format, len(lock.Artifacts), len(lock.Libraries))
	}
	want := map[string]string{
		"main_mister":    "7ca3cd2f224b9264d0889f593a0d77aafa5adda61910baba92c5ae401e26fcce",
		"menu":           "821bcf66181a00ff550e4a4110dc11c9fa8e68d38e9cb5558b3ddb99ca938934",
		"kernel":         "a6c7b1be0da9ba24a91bc1816737915d6a6cfba27c6c3025caded95167dc8dae",
		"megadrive_core": "0cd43ea2c96e726999f04924713ca090ae73829f3ab08109c6b552cebeba0839",
		"snes_core":      "4960eab619aef92596237dff46403a0e5e112c16b486ce659e8ee2e4a9b4bdcd",
	}
	for _, artifact := range lock.Artifacts {
		if digest, ok := want[artifact.Name]; ok {
			if artifact.SHA256 != digest {
				t.Fatalf("%s digest = %s", artifact.Name, artifact.SHA256)
			}
			delete(want, artifact.Name)
		}
	}
	if len(want) != 0 || lock.Runtime.KernelRelease != "5.15.1-MiSTer" {
		t.Fatalf("missing artifacts = %#v, runtime = %#v", want, lock.Runtime)
	}
}

func TestLoadPOC1ARejectsUnsafeInventory(t *testing.T) {
	t.Parallel()
	valid := `format = 1

[[artifacts]]
name = "main_mister"
path = "/media/fat/MiSTer"
sha256 = "7ca3cd2f224b9264d0889f593a0d77aafa5adda61910baba92c5ae401e26fcce"
size = 1059560
source = "official"

[runtime]
kernel_release = "5.15.1-MiSTer"

[[libraries]]
path = "/lib/libc.so.6"
resolved_path = "/usr/lib/libc-2.31.so"
sha256 = "d299728d09db7f41ecc46d3fe3f8865d980b1414bbe842b83fec9486785a935c"
size = 930536
`
	tests := map[string]string{
		"unknown field":        valid + "unknown = true\n",
		"malformed digest":     strings.Replace(valid, "7ca3cd2f224b9264d0889f593a0d77aafa5adda61910baba92c5ae401e26fcce", "not-a-digest", 1),
		"relative target path": strings.Replace(valid, "/media/fat/MiSTer", "media/fat/MiSTer", 1),
		"zero artifact size":   strings.Replace(valid, "size = 1059560", "size = 0", 1),
		"duplicate artifact": valid + `
[[artifacts]]
name = "main_mister"
path = "/media/fat/other"
sha256 = "821bcf66181a00ff550e4a4110dc11c9fa8e68d38e9cb5558b3ddb99ca938934"
size = 1
source = "official"
`,
		"duplicate library": valid + `
[[libraries]]
path = "/lib/libc.so.6"
resolved_path = "/usr/lib/libc-copy.so"
sha256 = "05409218f351018d6a5c94908cc2562dd04685797665af71cfb0dcff7d49b6a2"
size = 1
`,
	}
	for name, content := range tests {
		name, content := name, content
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := writeFixture(t, "poc1a.toml", content)
			if _, err := imagepoc.LoadPOC1A(path); err == nil {
				t.Fatal("unsafe inventory loaded")
			}
		})
	}
}

func TestLoadPOC1BValidatesImmutableInputs(t *testing.T) {
	t.Parallel()
	lock, err := imagepoc.LoadPOC1B(writeFixture(t, "poc1b.toml", validPOC1B))
	if err != nil {
		t.Fatal(err)
	}
	if lock.Container.Platform != "linux/amd64" || lock.Buildroot.Commit != "004a792dcf10e6c474070c9571f7504411e786cc" || lock.Outputs != nil {
		t.Fatalf("lock = %#v", lock)
	}
}

func TestLoadPOC1BRejectsMutableOrMalformedSources(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"missing digest":     strings.Replace(validPOC1B, "digest = 'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'\n", "", 1),
		"tag as commit":      strings.Replace(validPOC1B, "004a792dcf10e6c474070c9571f7504411e786cc", "2021.02.4", 1),
		"short commit":       strings.Replace(validPOC1B, "d7adb20b4ca595838289406c083fff78f004a8c3", "d7adb20", 1),
		"uppercase digest":   strings.Replace(validPOC1B, "sha256:aaaaaaaa", "sha256:AAAAAAAA", 1),
		"unknown field":      validPOC1B + "unexpected = true\n",
		"mutable platform":   strings.Replace(validPOC1B, "linux/amd64", "linux/arm64", 1),
		"empty kernel input": strings.Replace(validPOC1B, "MiSTer_defconfig", "", 1),
	}
	for name, content := range tests {
		name, content := name, content
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := imagepoc.LoadPOC1B(writeFixture(t, "poc1b.toml", content)); err == nil {
				t.Fatal("invalid source lock loaded")
			}
		})
	}
}

func TestWritePOC1BRoundTripsStably(t *testing.T) {
	t.Parallel()
	loaded, err := imagepoc.LoadPOC1B(writeFixture(t, "input.toml", validPOC1B))
	if err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(t.TempDir(), "first.toml")
	second := filepath.Join(t.TempDir(), "second.toml")
	if err := imagepoc.WritePOC1B(first, loaded); err != nil {
		t.Fatal(err)
	}
	roundTrip, err := imagepoc.LoadPOC1B(first)
	if err != nil {
		t.Fatal(err)
	}
	if err := imagepoc.WritePOC1B(second, roundTrip); err != nil {
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
	if err := imagepoc.VerifyFile(path, digest, 14); err != nil {
		t.Fatal(err)
	}
	if err := imagepoc.VerifyFile(path, digest, 13); err == nil {
		t.Fatal("wrong size accepted")
	}
	if err := imagepoc.VerifyFile(path, strings.Repeat("0", 64), 14); err == nil {
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
