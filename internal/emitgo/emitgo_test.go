package emitgo

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/DeanoC/mister-packages/internal/pack"
)

func TestGenerateSystemMegaDriveLaunchFields(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	sys, err := pack.LoadSystem(filepath.Join(root, "packages", "system", "megadrive.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text, err := GenerateSystem(sys)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		"package generated",
		`MegaDriveSystem`,
		`"megadrive"`,
		`MegaDriveExpectedCore`,
		`"MegaDrive"`,
		`MegaDriveCartridgeIndex = 1`,
	} {
		if !strings.Contains(text, fragment) {
			t.Errorf("generated Go missing %s\n%s", fragment, text)
		}
	}
}

func TestGenerateSystemWithoutCartridge(t *testing.T) {
	sys, err := pack.LoadSystem("../../packages/system/megadrive.yaml")
	if err != nil {
		t.Fatal(err)
	}
	sys.Media = nil // Emitter fixture; does not declare hardware support.
	text, err := GenerateSystem(sys)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"MegaDriveSystem", "MegaDriveExpectedCore", `"megadrive"`, `"MegaDrive"`} {
		if !strings.Contains(text, fragment) {
			t.Errorf("missing %s", fragment)
		}
	}
	if strings.Contains(text, "CartridgeIndex") {
		t.Fatal("fabricated cartridge index")
	}
}
