package emitcpp

import (
	"os"
	"os/exec"
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

// These altered identities are emitter fixtures, not hardware system packages.
func TestSystemHeadersCompileTogether(t *testing.T) {
	for _, empty := range []bool{false, true} {
		name := "with-media"
		if empty {
			name = "without-media"
		}
		t.Run(name, func(t *testing.T) {
			sys, err := pack.LoadSystem("../../packages/system/megadrive.yaml")
			if err != nil {
				t.Fatal(err)
			}
			first, err := GenerateSystem(sys)
			if err != nil {
				t.Fatal(err)
			}
			sys.ID, sys.ExpectedCore, sys.RBF.Artifact = "emitter_fixture", "EmitterFixture", "emitter_fixture.rbf"
			if empty {
				sys.Media = nil
			}
			second, err := GenerateSystem(sys)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			source := "#include \"first.hpp\"\n#include \"second.hpp\"\nusing namespace mister::native::generated;\nstatic_assert(kMegaDrive.media_count == 1, \"existing media\");\n"
			if empty {
				source += "static_assert(kEmitterFixture.media_count == 0 && kEmitterFixture.media == nullptr, \"no media\");\n"
			}
			for file, content := range map[string]string{"first.hpp": first, "second.hpp": second, "test.cpp": source} {
				if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			compiler := os.Getenv("CXX")
			if compiler == "" {
				compiler = "c++"
			}
			// Syntax-check generated text only; no runtime or target objects are built.
			output, err := exec.Command(compiler, "-std=c++14", "-pedantic-errors", "-fsyntax-only", filepath.Join(dir, "test.cpp")).CombinedOutput()
			if err != nil {
				t.Fatalf("strict C++14: %v\n%s", err, output)
			}
		})
	}
}

func TestRealSystemHeadersV2CompileTogether(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"megadrive", "pong", "snes"} {
		sys, err := pack.LoadSystem("../../packages/system/" + name + ".yaml")
		if err != nil {
			t.Fatal(err)
		}
		generated, err := GenerateSystem(sys)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(generated, "MISTER_PACKAGES_GENERATED_SYSTEM_TYPES_V2") {
			t.Fatal("missing V2 layout guard")
		}
		if err := os.WriteFile(filepath.Join(dir, name+".hpp"), []byte(generated), 0600); err != nil {
			t.Fatal(err)
		}
	}
	source := `#include "megadrive.hpp"
#include "pong.hpp"
#include "snes.hpp"
using namespace mister::native::generated;
static_assert(kMegaDrive.input.c == 0x40 && kMegaDrive.input.x == 0 && kMegaDrive.input.select == 0, "MD unchanged");
static_assert(kMegaDrive.media[0].transform[0] == 'r', "raw default");
static_assert(kPong.media == nullptr && kPong.media_count == 0 && kPong.input.x == 0, "Pong unchanged");
static_assert(kSNES.media[0].index == 1 && kSNES.media[0].maximum_size == 0x400200, "SNES native cartridge");
static_assert(kSNES.media[0].transform[0] == 's', "SNES transform");
static_assert(kSNES.input.c == 0 && kSNES.input.x == 0x40 && kSNES.input.y == 0x80 && kSNES.input.l == 0x100 && kSNES.input.r == 0x200 && kSNES.input.select == 0x400 && kSNES.input.start == 0x800, "SNES buttons");
`
	if err := os.WriteFile(filepath.Join(dir, "test.cpp"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	compiler := os.Getenv("CXX")
	if compiler == "" {
		compiler = "c++"
	}
	if out, err := exec.Command(compiler, "-std=c++14", "-pedantic-errors", "-fsyntax-only", filepath.Join(dir, "test.cpp")).CombinedOutput(); err != nil {
		t.Fatalf("V2 headers: %v\n%s", err, out)
	}
}
