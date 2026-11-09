package pack

import (
	"path/filepath"
	"testing"
)

func TestDE25ScanoutAgreesWithProbeAllocationGeometry(t *testing.T) {
	probe, err := LoadFabric(filepath.Join(repoRoot(t), "packages/fabric/fes_fabric_de25_ddr_probe.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	scanout, err := LoadFabric(filepath.Join(repoRoot(t), "packages/fabric/fes_fabric_de25_scanout.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if scanout.ID != "fes.fabric.de25-scanout" || scanout.Major != 1 || scanout.Minor != 2 {
		t.Fatalf("unexpected scanout contract: %#v", scanout)
	}
	for _, suffix := range []string{"DmaAddressBits", "BeatBytes", "GrantAlignment", "BoundaryBytes",
		"FrameWidth", "FrameHeight", "FrameStride", "FrameBytes", "SlotBytes", "SlotCount", "AllocationBytes"} {
		p, pok := probe.Constant("FesDe25DdrProbe" + suffix)
		s, sok := scanout.Constant("FesDe25Scanout" + suffix)
		if !pok || !sok || p != s {
			t.Errorf("geometry %s differs: probe=%d (%t), scanout=%d (%t)", suffix, p, pok, s, sok)
		}
	}
	get := func(suffix string) uint32 {
		t.Helper()
		value, ok := scanout.Constant("FesDe25Scanout" + suffix)
		if !ok {
			t.Fatalf("missing scanout constant %s", suffix)
		}
		return value
	}
	// The probe and scanout share allocations, while the sustained display
	// reader uses a larger service window than the diagnostic probe.
	probeBurst, ok := probe.Constant("FesDe25DdrProbeMaxBurstBeats")
	if !ok || probeBurst != 16 || get("MaxBurstBeats") != 64 {
		t.Fatal("probe and scanout must preserve their independent burst limits")
	}
	if get("MaxBurstBeats")*get("BeatBytes") > get("BoundaryBytes") {
		t.Fatal("scanout burst exceeds its clipped boundary")
	}
	if get("Version") != 0x10002 || get("StatusDisplayReady") != 64 || get("ErrorDisplay") != 128 {
		t.Fatal("scanout1.2 preserves positive display readiness and its fault bit")
	}
	for index, suffix := range []string{"BuildId0Offset", "BuildId1Offset", "BuildId2Offset", "BuildId3Offset"} {
		if get(suffix) != 0x64+4*uint32(index) || get(suffix)+4 > get("Span") {
			t.Fatalf("selected build identity word %d is outside its exact control layout", index)
		}
	}
	if get("FrameStride") != get("FrameWidth")*4 || get("FrameBytes") != get("FrameStride")*get("FrameHeight") ||
		get("FrameBytes") > get("SlotBytes") || get("AllocationBytes") != get("SlotBytes")*get("SlotCount") {
		t.Fatal("scanout geometry does not describe bounded BGRX8888 slots")
	}
	if get("PeripheralOffset") < 0x10180 || get("PeripheralOffset")+get("Span") > get("ArchitecturalLwSpan") {
		t.Fatal("scanout control overlaps the retained probe or exceeds the aperture")
	}
}
