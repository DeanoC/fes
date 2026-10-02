package emitverilog

import (
	"strings"
	"testing"

	"github.com/DeanoC/mister-packages/internal/pack"
)

func TestGenerateTimedVideoFabricConstants(t *testing.T) {
	fabric, err := pack.LoadFabric("../../packages/fabric/fes_fabric_video_raster_rgb888.yaml")
	if err != nil {
		t.Fatal(err)
	}
	generated, err := GenerateFabric(fabric)
	if err != nil {
		t.Fatal(err)
	}
	again, err := GenerateFabric(fabric)
	if err != nil || generated != again {
		t.Fatalf("fabric generation is not deterministic: %v", err)
	}
	for _, fragment := range []string{
		"packages/fabric/fes_fabric_video_raster_rgb888.yaml",
		"`ifndef FES_FABRIC_VIDEO_RASTER_RGB888_CONSTANTS_V1",
		"`define FES_VIDEO_PART_REQUEST_BITS 32'h00000020",
		"`define FES_VIDEO_PART_RESPONSE_BITS 32'h0000001c",
		"`define FES_VIDEO_PART_REQUEST_SOF_BIT 32'h0000001c",
		"`define FES_VIDEO_PART_REQUEST_RESERVED_BIT 32'h0000001f",
		"`define FES_VIDEO_PART_BOUNDARY_LATENCY_CYCLES 32'h00000002",
	} {
		if !strings.Contains(generated, fragment) {
			t.Fatalf("missing fabric macro %q", fragment)
		}
	}
	if strings.Contains(generated, "CAPABILITY") || strings.Contains(generated, "GP_") {
		t.Fatal("fabric generated host ABI/capability macros")
	}
	fabric.Constants = append(fabric.Constants,
		pack.ABIConstant{Name: "VideoRGB", Value: 1}, pack.ABIConstant{Name: "VideoRgb", Value: 2})
	if _, err := GenerateFabric(fabric); err == nil {
		t.Fatal("fabric accepted a normalized macro collision")
	}
}
