package policyobserve

import (
	"debug/elf"
	"testing"
)

func TestELFObserverNormalizesLogicalPathsAndABI(t *testing.T) {
	if got := logicalComponent("libstdc++.so.6"); got != "libstdc_plus_plus.so.6" {
		t.Fatalf("logicalComponent() = %q", got)
	}
	if got := logicalInterpreterPath("/lib/ld-linux-armhf.so.3"); got != "/stage-a0/sysroot/lib/ld-linux-armhf.so.3" {
		t.Fatalf("logicalInterpreterPath() = %q", got)
	}
	if got := elfABI(elf.FileHeader{Machine: elf.EM_ARM}, 0x05000400); got != "EABI5-hard-float" {
		t.Fatalf("hard-float ABI = %q", got)
	}
	if got := elfABI(elf.FileHeader{Machine: elf.EM_ARM}, 0x05000000); got != "EABI5" {
		t.Fatalf("soft-float ABI = %q", got)
	}
}

func TestELFObserverAssignsBundledLibrariesToSeparateMaterials(t *testing.T) {
	root := dependencyRoot{physical: "/capture/main", materialID: "main-fork", sourcePackage: "main-fork"}
	tests := []struct {
		path string
		want string
	}{
		{path: "/capture/main/lib/imlib2/libImlib2.so", want: "main-fork-libimlib2"},
		{path: "/capture/main/lib/imlib2/libfreetype.so", want: "main-fork-libfreetype"},
		{path: "/capture/main/lib/bluetooth/libbluetooth.so", want: "main-fork-libbluetooth"},
		{path: "/capture/main/lib/miniz/miniz.c", want: "main-fork"},
	}
	for _, test := range tests {
		if got := materialIDFor(root, test.path); got != test.want {
			t.Errorf("materialIDFor(%q) = %q, want %q", test.path, got, test.want)
		}
	}
}

func TestELFObserverUsesCanonicalToolchainMaterialID(t *testing.T) {
	if toolchainMaterialID != "toolchain" {
		t.Fatalf("toolchain material ID = %q, want lock material ID toolchain", toolchainMaterialID)
	}
}

func TestELFObserverSymlinkContainment(t *testing.T) {
	for _, test := range []struct {
		root, candidate string
		want            bool
	}{
		{root: "/capture/sysroot", candidate: "/capture/sysroot/lib/libc.so.6", want: true},
		{root: "/capture/sysroot", candidate: "/capture/sysroot/../outside/libc.so.6", want: false},
		{root: "/capture/sysroot", candidate: "/capture/sysrooted/libc.so.6", want: false},
	} {
		if got := pathWithin(test.root, test.candidate); got != test.want {
			t.Errorf("pathWithin(%q, %q) = %v, want %v", test.root, test.candidate, got, test.want)
		}
	}
}

func TestELFObserverRequiresARM32LittleEndian(t *testing.T) {
	want := elf.FileHeader{Class: elf.ELFCLASS32, Data: elf.ELFDATA2LSB, Machine: elf.EM_ARM}
	if err := validateARM32(want); err != nil {
		t.Fatalf("valid ARM32 header rejected: %v", err)
	}
	for _, header := range []elf.FileHeader{
		{Class: elf.ELFCLASS64, Data: elf.ELFDATA2LSB, Machine: elf.EM_ARM},
		{Class: elf.ELFCLASS32, Data: elf.ELFDATA2MSB, Machine: elf.EM_ARM},
		{Class: elf.ELFCLASS32, Data: elf.ELFDATA2LSB, Machine: elf.EM_X86_64},
	} {
		if err := validateARM32(header); err == nil {
			t.Fatalf("unsupported header %#v was accepted", header)
		}
	}
}
