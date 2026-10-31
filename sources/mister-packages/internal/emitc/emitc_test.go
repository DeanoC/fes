package emitc

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/DeanoC/mister-packages/internal/pack"
)

func de25Fabric(t *testing.T) *pack.FabricFile {
	t.Helper()
	fabric, err := pack.LoadFabric("../../packages/fabric/fes_fabric_de25_ddr_probe.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return fabric
}

func TestGenerateDE25FabricFreestandingC(t *testing.T) {
	fabric := de25Fabric(t)
	generated, err := GenerateFabric(fabric)
	if err != nil {
		t.Fatal(err)
	}
	again, err := GenerateFabric(fabric)
	if err != nil || generated != again {
		t.Fatalf("fabric generation is not deterministic: %v", err)
	}
	for _, fragment := range []string{
		"packages/fabric/fes_fabric_de25_ddr_probe.yaml",
		"#ifndef FES_FABRIC_DE25_DDR_PROBE_CONSTANTS_V1_H",
		"#define FES_FABRIC_DE25_DDR_PROBE_CONSTANTS_V1_H\n",
		"#define FES_DE25_DDR_PROBE_IDENTITY 0x44323550U\n",
		"#define FES_DE25_DDR_PROBE_ARCHITECTURAL_LW_BASE 0x20000000U\n",
		"#define FES_DE25_DDR_PROBE_GRANT_HI_OFFSET 0x00000014U\n",
		"#define FES_DE25_DDR_PROBE_LAST_DATA3_OFFSET 0x00000054U\n",
	} {
		if !strings.Contains(generated, fragment) {
			t.Fatalf("missing C fabric output %q", fragment)
		}
	}
	macros := regexp.MustCompile(`(?m)^#define [A-Z0-9_]+ 0x[0-9a-f]{8}U$`).FindAllString(generated, -1)
	if len(macros) != len(fabric.Constants) {
		t.Fatalf("emitted %d unsigned 32-bit C macros for %d constants", len(macros), len(fabric.Constants))
	}
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("C compiler unavailable for freestanding header check")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "probe.h"), []byte(generated), 0o600); err != nil {
		t.Fatal(err)
	}
	program := `#include "probe.h"
#include "probe.h"
_Static_assert(FES_DE25_DDR_PROBE_IDENTITY == 0x44323550U, "identity");
_Static_assert(FES_DE25_DDR_PROBE_DMA_ADDRESS_BITS == 32U, "DMA address width");
_Static_assert(FES_DE25_DDR_PROBE_ALLOCATION_BYTES == 8388608U, "allocation");
_Static_assert(FES_DE25_DDR_PROBE_GRANT_HI_OFFSET == 0x14U, "high address register");
_Static_assert(FES_DE25_DDR_PROBE_FRAME_BYTES == 3686400U, "frame geometry");
_Static_assert(sizeof(FES_DE25_DDR_PROBE_IDENTITY) == 4, "32-bit constant");
_Static_assert(_Generic(FES_DE25_DDR_PROBE_IDENTITY, unsigned int: 1, default: 0), "unsigned constant");
`
	command := exec.Command(cc, "-std=c11", "-ffreestanding", "-nostdinc", "-Wall", "-Werror", "-fsyntax-only", "-I", dir, "-x", "c", "-")
	command.Stdin = strings.NewReader(program)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("emitted fabric header is not standalone C: %v\n%s", err, output)
	}
}

func TestGenerateDE25FabricRejectsMacroCollisions(t *testing.T) {
	for name, constants := range map[string][]pack.ABIConstant{
		"normalized": {{Name: "VideoRGB", Value: 1}, {Name: "VideoRgb", Value: 2}},
		"guard":      {{Name: "FesFabricDe25DdrProbeConstantsV1H", Value: 1}},
	} {
		t.Run(name, func(t *testing.T) {
			fabric := de25Fabric(t)
			fabric.Constants = append(fabric.Constants, constants...)
			if _, err := GenerateFabric(fabric); err == nil {
				t.Fatal("accepted a normalized C macro collision")
			}
		})
	}
}
