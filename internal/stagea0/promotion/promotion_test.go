package promotion

import (
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/internal/stagea0"
	"github.com/DeanoC/FogCast-POC/internal/stagea0/policy"
)

func TestValidateRejectsIncompleteLockBeforePromotion(t *testing.T) {
	err := Validate(structuredEmptyLock(), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "main lock is not valid") {
		t.Fatalf("Validate() = %v, want lock rejection", err)
	}
}

func TestValidateForkDeltaRequiresSingleMakefile(t *testing.T) {
	document := policy.Document{ForkDelta: &policy.ForkDeltaPolicy{Completeness: policy.CompletenessComplete, Purpose: "deterministic-vdate-input", ForkSourceSetChange: "none", ChangedPaths: []policy.ChangedPath{{Path: "README.md", Status: "modified", OldMode: "100644", NewMode: "100644", OldBlob: strings.Repeat("a", 40), NewBlob: strings.Repeat("b", 40), OldSHA256: strings.Repeat("c", 64), NewSHA256: strings.Repeat("d", 64)}}}}
	if err := validateForkDelta(document); err == nil {
		t.Fatal("validateForkDelta() accepted a non-Makefile patch")
	}
}

func TestDependencyClosureIncludesInterpreterSymlink(t *testing.T) {
	path := "/stage-a0/sysroot/lib/ld-linux-armhf.so.3"
	dependencies := []policy.DependencyRecord{{SONAME: "ld", LogicalPath: "/stage-a0/sysroot/lib/ld-linux.so.3", RealLogicalPath: "/stage-a0/sysroot/lib/ld-linux-2.so", SymlinkChain: []string{path}, ABI: "ARM", Size: 1, SHA256: strings.Repeat("a", 64), MaterialID: "toolchain", SourcePackage: "libc"}}
	if !dependencyContainsPath(dependencies, path) {
		t.Fatal("dependencyContainsPath() missed interpreter symlink")
	}
}

func TestIntermediateClosureAllowsFullBuildManifest(t *testing.T) {
	lock := stagea0.MainLock{Build: stagea0.MainLockBuild{AllowedFinalArtifacts: []string{"bin/MiSTer", "bin/MiSTer.elf"}}}
	document := policy.Document{IntermediatePath: &policy.IntermediatePathPolicy{
		Completeness: policy.CompletenessComplete,
		Records: []policy.IntermediateRecord{
			{Path: "bin/MiSTer", Class: "final-stripped", ProducerKey: "final-mister", ExpectedMode: "0755"},
			{Path: "bin/MiSTer.elf", Class: "final-unstripped", ProducerKey: "final-mister-elf", ExpectedMode: "0755"},
			{Path: "bin/extra.o", Class: "object", ProducerKey: "extra-object", ExpectedMode: "0644"},
		},
	}}
	if err := validateIntermediate(document, lock); err != nil {
		t.Fatalf("validateIntermediate(full manifest) = %v", err)
	}
}

func TestIntermediateClosureRejectsInvalidLockedFinalRecord(t *testing.T) {
	lock := stagea0.MainLock{Build: stagea0.MainLockBuild{AllowedFinalArtifacts: []string{"bin/MiSTer", "bin/MiSTer.elf"}}}
	document := policy.Document{IntermediatePath: &policy.IntermediatePathPolicy{
		Completeness: policy.CompletenessComplete,
		Records: []policy.IntermediateRecord{
			{Path: "bin/MiSTer", Class: "object", ProducerKey: "final-mister", ExpectedMode: "0755"},
			{Path: "bin/MiSTer.elf", Class: "final-unstripped", ProducerKey: "final-mister-elf", ExpectedMode: "0755"},
		},
	}}
	if err := validateIntermediate(document, lock); err == nil {
		t.Fatal("validateIntermediate() accepted an invalid locked final record")
	}
}

func structuredEmptyLock() (lock stagea0.MainLock) { return lock }
