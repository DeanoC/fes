package pack_test

import (
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/mister-packages/internal/emitcpp"
	"github.com/DeanoC/mister-packages/internal/emitgo"
	"github.com/DeanoC/mister-packages/internal/emitverilog"
	"github.com/DeanoC/mister-packages/internal/pack"
)

func TestApplicationEmitsAlongsideLegacyABIs(t *testing.T) {
	dir := t.TempDir()
	includes := ""
	for _, name := range []string{"fes_simple_game", "fes_simple_computer", "fes_application"} {
		abi, err := pack.LoadABI("../../packages/abi/" + name + ".yaml")
		if err != nil {
			t.Fatal(err)
		}
		cpp, err := emitcpp.GenerateABI(abi)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, name+".hpp"), []byte(cpp), 0600); err != nil {
			t.Fatal(err)
		}
		includes += "#include \"" + name + ".hpp\"\n"
		goText, err := emitgo.GenerateABI(abi)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = parser.ParseFile(token.NewFileSet(), name+".go", goText, parser.AllErrors); err != nil {
			t.Fatal(err)
		}
		verilog, err := emitverilog.GenerateABI(abi)
		if err != nil {
			t.Fatal(err)
		}
		if name == "fes_application" {
			for _, s := range []string{"FesApplicationABIID", "FesApplicationCapabilityGamepad", "FesApplicationCapabilityMediaBlobStream"} {
				if !strings.Contains(goText, s) {
					t.Fatal(s)
				}
			}
			for _, s := range []string{"FES_APPLICATION_ABI_TAG 32'h00000003", "FES_APPLICATION_OPCODE_BUTTONS 32'h00000003", "FES_APPLICATION_INTERFACE_GAMEPAD_CAPABILITY_MASK 32'h00000001", "FES_APPLICATION_INTERFACE_MEDIA_BLOB_STREAM_CAPABILITY_MASK 32'h00000008"} {
				if !strings.Contains(verilog, s) {
					t.Fatal(s)
				}
			}
			if err = os.WriteFile(filepath.Join(dir, "application.vh"), []byte(verilog), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	source := includes + `
using namespace mister::native::generated;
static_assert(FesApplicationAbiTag == 3, "application tag");
static_assert(FesGpAbiTag == 1 && FesSimpleComputerAbiTag == 2, "legacy tags");
static_assert(FesApplicationSignature == FesGpSignature, "shared framing");
static_assert(FesApplicationOpcodeButtons == 3, "buttons");
static_assert(FesApplicationCapabilityGamepad == 1, "gamepad");
static_assert(FesApplicationCapabilityMediaBlobStream == 8, "stream");
`
	path := filepath.Join(dir, "test.cpp")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	compiler := os.Getenv("CXX")
	if compiler == "" {
		compiler = "c++"
	}
	if out, err := exec.Command(compiler, "-std=c++14", "-pedantic-errors", "-fsyntax-only", path).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	t.Run("VerilogCompile", func(t *testing.T) {
		compiler, err := exec.LookPath("verilator")
		if err != nil {
			t.Skip("verilator unavailable; macros verified above")
		}
		path := filepath.Join(dir, "test.v")
		source := "`include \"application.vh\"\nmodule application_constants; initial begin if (`FES_APPLICATION_ABI_TAG != 3 || `FES_APPLICATION_INTERFACE_MEDIA_BLOB_STREAM_CAPABILITY_MASK != 8) $fatal; end endmodule\n"
		if err = os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(compiler, "--lint-only", "-Wall", "-Wno-DECLFILENAME", "-I"+dir, path).CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	})
}
