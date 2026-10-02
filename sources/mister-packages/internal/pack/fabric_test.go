package pack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadTimedVideoFabricContract(t *testing.T) {
	fabric, err := LoadFabric(filepath.Join(repoRoot(t), "packages/fabric/fes_fabric_video_raster_rgb888.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if fabric.ID != "fes.fabric.video.raster-rgb888" || fabric.Major != 1 || fabric.Minor != 0 {
		t.Fatalf("fabric = %#v", fabric)
	}
	for name, want := range map[string]uint32{
		"FesVideoPartMajor": 1, "FesVideoPartMinor": 0,
		"FesVideoPartRequestBits": 32, "FesVideoPartResponseBits": 28,
		"FesVideoPartRgbBits": 24, "FesVideoPartRequestRgbLow": 0,
		"FesVideoPartRequestDeBit": 24, "FesVideoPartRequestHsBit": 25,
		"FesVideoPartRequestVsBit": 26, "FesVideoPartRequestCeBit": 27,
		"FesVideoPartRequestSofBit": 28, "FesVideoPartRequestEolBit": 29,
		"FesVideoPartRequestHoldBit": 30, "FesVideoPartRequestReservedBit": 31,
		"FesVideoPartResponseRgbLow": 0, "FesVideoPartResponseDeBit": 24,
		"FesVideoPartResponseHsBit": 25, "FesVideoPartResponseVsBit": 26,
		"FesVideoPartResponseCeBit": 27, "FesVideoPartBoundaryLatencyCycles": 2,
		"FesVideoPart720pPixelClockHz":     74250000,
		"FesVideoPart720pHorizontalActive": 1280, "FesVideoPart720pHorizontalTotal": 1650,
		"FesVideoPart720pVerticalActive": 720, "FesVideoPart720pVerticalTotal": 750,
	} {
		if got, ok := fabric.Constant(name); !ok || got != want {
			t.Errorf("constant %s = %d, %t; want %d", name, got, ok, want)
		}
	}
}

func TestLoadFabricRejectsMalformedAndHostABIFields(t *testing.T) {
	const valid = "schema: mister-packages.v1\nkind: fabric\nid: fes.fabric.video.raster-rgb888\nmajor: 1\nminor: 0\nconstants:\n  - name: VideoWidth\n    value: 32\n"
	for name, source := range map[string]string{
		"schema":         strings.Replace(valid, "mister-packages.v1", "mister-packages.v2", 1),
		"kind":           strings.Replace(valid, "kind: fabric", "kind: abi", 1),
		"identifier":     strings.Replace(valid, "fes.fabric.video.raster-rgb888", "Bad Id", 1),
		"major":          strings.Replace(valid, "major: 1", "major: 0", 1),
		"constant-name":  strings.Replace(valid, "VideoWidth", "bad_name", 1),
		"constant-bound": strings.Replace(valid, "value: 32", "value: 0x100000000", 1),
		"duplicate":      valid + "  - name: VideoWidth\n    value: 28\n",
		"gp-tag":         valid + "tag: 5\n",
		"capabilities":   valid + "interfaces: []\n",
		"empty":          strings.Replace(valid, "constants:\n  - name: VideoWidth\n    value: 32\n", "constants: []\n", 1),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "fabric.yaml")
			if err := os.WriteFile(path, []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadFabric(path); err == nil {
				t.Fatal("accepted malformed fabric contract")
			}
		})
	}
}
