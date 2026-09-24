package kitcontent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/internal/meshcontent"
)

func TestReadInstalledPackages(t *testing.T) {
	root := t.TempDir()
	pkg := strings.Repeat("ab", 32)
	other := strings.Repeat("cd", 32)
	writeManifest(t, filepath.Join(root, pkg), `
format = 2
[abi]
id = "fes.simple-game"
major = 1
minor = 0
`)
	writeManifest(t, filepath.Join(root, other), `
[abi]
id = "fes.simple-game"
major = 1
`)
	link := filepath.Join(root, strings.Repeat("ee", 32))
	if err := os.Symlink(pkg, link); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "not-a-package"), 0o700); err != nil {
		t.Fatal(err)
	}
	abis, packages := ReadInstalledPackages([]string{root, filepath.Join(root, "missing")})
	if len(packages) != 2 || packages[0] != pkg || packages[1] != other {
		t.Fatalf("packages %v", packages)
	}
	if len(abis) != 1 || abis[0] != (meshcontent.EligibleABI{ID: "fes.simple-game", Major: 1}) {
		t.Fatalf("abis %+v", abis)
	}
	if got, ids := ReadInstalledPackages(nil); got != nil || ids != nil {
		t.Fatalf("empty roots %v %v", got, ids)
	}
}

func TestNormalizePackageIDsDropsInvalid(t *testing.T) {
	good := strings.Repeat("a", 64)
	got := normalizePackageIDs([]string{good, "nope", strings.ToUpper(good), good})
	if len(got) != 1 || got[0] != good {
		t.Fatalf("ids %v", got)
	}
}

func writeManifest(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
