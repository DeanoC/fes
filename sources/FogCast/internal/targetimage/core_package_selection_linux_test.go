//go:build linux

package targetimage

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestCorePackageSelectionSealsMembersUnderPrivateUmask(t *testing.T) {
	if os.Getenv("FES_PACKAGE_UMASK_CHILD") == "1" {
		previous := unix.Umask(0o077)
		defer unix.Umask(previous)
		cache := os.Getenv("FES_PACKAGE_UMASK_CACHE")
		selection, err := PrepareCorePackageSelectionForCore(
			os.Getenv("FES_PACKAGE_UMASK_SOURCE"), os.Getenv("FES_PACKAGE_UMASK_RECORD"),
			cache, filepath.Join(cache, "fes-atari-st.package-selection.toml"), "fes.atari-st")
		if err != nil {
			t.Fatal(err)
		}
		published := filepath.Join(cache, "core-packages", selection.PackageID)
		defer os.Chmod(published, 0o755)
		for _, name := range []string{"manifest.toml", "core.rbf", "rom-map.json"} {
			info, err := os.Stat(filepath.Join(published, name))
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o444 {
				t.Errorf("published %s mode %04o, want 0444", name, info.Mode().Perm())
			}
		}
		if err := VerifyCorePackageSelectionForCore(published, filepath.Join(cache, "fes-atari-st.package-selection.toml"), "fes.atari-st"); err != nil {
			t.Fatal(err)
		}
		return
	}
	// Isolate the process-wide umask from parallel tests and the Go test runner.
	directory, record, _ := packageSelectionFixtureForCore(t, "fes.atari-st")
	cmd := exec.Command(os.Args[0], "-test.run=^TestCorePackageSelectionSealsMembersUnderPrivateUmask$")
	cmd.Env = append(os.Environ(), "FES_PACKAGE_UMASK_CHILD=1", "FES_PACKAGE_UMASK_SOURCE="+directory,
		"FES_PACKAGE_UMASK_RECORD="+record, "FES_PACKAGE_UMASK_CACHE="+t.TempDir())
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("private-umask package publication: %v\n%s", err, output)
	}
}
