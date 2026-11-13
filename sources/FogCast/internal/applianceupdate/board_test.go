package applianceupdate

import (
	"os"
	"path/filepath"
	"testing"

	release "github.com/DeanoC/FogCast/appliance"
)

func TestImageBoardRequiresImageAndCPUAgreement(t *testing.T) {
	root := t.TempDir()
	factory, selection := filepath.Join(root, "factory.json"), filepath.Join(root, "fes-board")
	if err := os.WriteFile(factory, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := ImageBoard(factory, selection, "arm"); err != nil || got != release.Board {
		t.Fatalf("legacy ARM image: %q %v", got, err)
	}
	if _, err := ImageBoard(factory, selection, "arm64"); err == nil {
		t.Fatal("AArch64 accepted a missing image selection")
	}
	for _, tc := range []struct{ raw, arch, want string }{
		{"de10-nano\n", "arm", release.Board}, {"de25-nano\n", "arm64", release.BoardDE25},
		{"de10-nano\n", "arm64", ""}, {"de25-nano\n", "arm", ""},
		{"de25-nano", "arm64", ""}, {"de25-nano\nextra", "arm64", ""},
		{"de25-nano\n", "amd64", ""}, {"other\n", "arm64", ""},
	} {
		if err := os.WriteFile(selection, []byte(tc.raw), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := ImageBoard(factory, selection, tc.arch)
		if (err == nil) != (tc.want != "") || (err == nil && got != tc.want) {
			t.Fatalf("selection=%q CPU=%s: %q %v", tc.raw, tc.arch, got, err)
		}
	}
	link := filepath.Join(root, "linked-board")
	if err := os.Symlink(selection, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ImageBoard(factory, link, "arm64"); err == nil {
		t.Fatal("board selection followed a symlink")
	}
	if err := os.Remove(factory); err != nil {
		t.Fatal(err)
	}
	if _, err := ImageBoard(factory, selection, "arm64"); err != nil {
		t.Fatal("direct-root no-update path changed:", err)
	}
}
