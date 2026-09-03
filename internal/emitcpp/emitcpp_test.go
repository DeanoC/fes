package emitcpp

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/DeanoC/mister-packages/internal/pack"
)

func TestGenerateContainsOracleNames(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	resolved, err := pack.LoadPlatform(filepath.Join(root, "packages", "platform", "de10_nano.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text, err := Generate(resolved)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"kFpgaStatusAddress",
		"kFpgaDataAddress",
		"kSpiGpiAddress",
		"kFpgaCoreReset",
		"HPS_REG_APERTURE_BASE_ADDR",
	} {
		if !strings.Contains(text, name) {
			t.Errorf("generated C++ missing %s", name)
		}
	}
	if !strings.Contains(text, "namespace generated") {
		t.Error("missing generated namespace")
	}
	if strings.Contains(text, "inline constexpr") {
		t.Error("generated C++ is not C++14 (inline constexpr)")
	}
}

func TestGenerateSystemContainsProfileFields(t *testing.T) {
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
		`"megadrive"`,
		`"MegaDrive"`,
		`"megadrive.rbf"`,
		`"cartridge"`,
		`".md"`,
		`"little_endian_byte_pairs"`,
		"kMegaDrive",
		"namespace native",
		"static constexpr",
	} {
		if !strings.Contains(text, fragment) {
			t.Errorf("generated C++ missing %s", fragment)
		}
	}
	if strings.Contains(text, "inline constexpr") {
		t.Error("generated C++ is not C++14 (inline constexpr)")
	}
}
