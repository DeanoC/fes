package policyobserve

import (
	"debug/elf"
	"testing"
)

func TestELFObserverNormalizesLogicalPathsAndABI(t *testing.T) {
	if got := logicalComponent("libstdc++.so.6"); got != "libstdc__.so.6" {
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
