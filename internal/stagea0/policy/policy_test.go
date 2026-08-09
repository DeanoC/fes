package policy

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestAllKindsHaveCanonicalRoundTrip(t *testing.T) {
	for _, kind := range allKinds {
		t.Run(string(kind), func(t *testing.T) {
			document := validDocument(kind)
			raw, err := Encode(document)
			if err != nil {
				t.Fatalf("Encode() = %v", err)
			}
			if !bytes.HasSuffix(raw, []byte("\n")) {
				t.Fatal("encoded policy is not LF terminated")
			}
			decoded, err := Decode(raw)
			if err != nil {
				t.Fatalf("Decode() = %v", err)
			}
			reencoded, err := Encode(decoded)
			if err != nil || !bytes.Equal(raw, reencoded) {
				t.Fatalf("decode/re-encode changed bytes: err=%v", err)
			}
			if got := decoded.Schema; got != SchemaFor(kind) {
				t.Fatalf("schema = %q, want %q", got, SchemaFor(kind))
			}
		})
	}
}

func TestDecodeRejectsNonCanonicalAndUnknownJSON(t *testing.T) {
	raw, err := Encode(validDocument(KindForkDelta))
	if err != nil {
		t.Fatal(err)
	}
	for _, hostile := range [][]byte{
		append([]byte(" "), raw...),
		append(append([]byte(nil), raw...), []byte("{}")...),
		append(bytes.TrimSuffix(raw, []byte("\n")), []byte(`,"purpose":"unexpected"}`)...),
		[]byte(`{"format":1,"schema":"fogcast.stage-a0.policy.upstream-fork-delta.v1","kind":"upstream-fork-delta","authority":{},"normalization_version":1,"upstream_fork_delta":{}}` + "\n"),
	} {
		if _, err := Decode(hostile); !hasCode(err, CodeSchemaInvalid) && !hasCode(err, CodeHashInvalid) {
			t.Fatalf("Decode(%q) = %v, want stable schema/hash failure", hostile, err)
		}
	}
}

func TestValidateRejectsPayloadUnionAndOrdering(t *testing.T) {
	document := validDocument(KindSourceSet)
	document.CompileLink = &CompileLinkPolicy{Completeness: CompletenessObserved, Records: []CompileRecord{validCompileRecord()}}
	if err := Validate(document); !hasCode(err, CodeSchemaInvalid) {
		t.Fatalf("multiple payloads = %v, want %s", err, CodeSchemaInvalid)
	}
	document = validDocument(KindSourceSet)
	document.SourceSet.Records = append(document.SourceSet.Records, SourceRecord{Path: "a.c", GitMode: "100644", BlobOID: strings.Repeat("b", 40), SHA256: strings.Repeat("b", 64), MaterialID: "main-fork", InclusionReason: "c-translation-unit"})
	if err := Validate(document); !hasCode(err, CodeOrderInvalid) {
		t.Fatalf("unsorted source records = %v, want %s", err, CodeOrderInvalid)
	}
	document = validDocument(KindSourceSet)
	document.SourceSet.Records[0].Path = "../escape.c"
	if err := Validate(document); !hasCode(err, CodeSchemaInvalid) {
		t.Fatalf("unsafe source path = %v, want %s", err, CodeSchemaInvalid)
	}
}

func TestValidateRejectsAuthorityDriftAndInvalidHash(t *testing.T) {
	document := validDocument(KindForkDelta)
	document.Authority.ForkParentCommit = strings.Repeat("f", 40)
	if err := Validate(document); !hasCode(err, CodeSchemaInvalid) {
		t.Fatalf("parent drift = %v, want %s", err, CodeSchemaInvalid)
	}
	document = validDocument(KindForkDelta)
	document.ForkDelta.PatchDiffSHA256 = "not-a-digest"
	if err := Validate(document); !hasCode(err, CodeSchemaInvalid) {
		t.Fatalf("digest drift = %v, want %s", err, CodeSchemaInvalid)
	}
	document = validDocument(KindForkDelta)
	document.Authority.VDate = "260809"
	if err := Validate(document); !hasCode(err, CodeSchemaInvalid) {
		t.Fatalf("date drift = %v, want %s", err, CodeSchemaInvalid)
	}
}

func TestValidateRejectsUnsafeToolAndArguments(t *testing.T) {
	document := validDocument(KindCompileLink)
	document.CompileLink.Records[0].ToolLogicalPath = "/stage-a0/toolchain/bin/../../escape"
	if err := Validate(document); !hasCode(err, CodeSchemaInvalid) {
		t.Fatalf("tool traversal = %v, want %s", err, CodeSchemaInvalid)
	}
	document = validDocument(KindCompileLink)
	document.CompileLink.Records[0].Argv = []string{"arm-none-linux-gnueabihf-gcc", "/Users/example/main.cpp"}
	if err := Validate(document); !hasCode(err, CodeSchemaInvalid) {
		t.Fatalf("host argv path = %v, want %s", err, CodeSchemaInvalid)
	}
	document = validDocument(KindCompileLink)
	document.CompileLink.Records[0].Argv = []string{"arm-none-linux-gnueabihf-gcc", "-I./lib"}
	if err := Validate(document); !hasCode(err, CodeSchemaInvalid) {
		t.Fatalf("relative argv traversal = %v, want %s", err, CodeSchemaInvalid)
	}
}

func TestValidateRejectsNonCanonicalModesAndOpaqueText(t *testing.T) {
	document := validDocument(KindGeneratedInput)
	document.GeneratedInput.Records[0].ExpectedMode = "644"
	if err := Validate(document); !hasCode(err, CodeSchemaInvalid) {
		t.Fatalf("generated mode = %v, want %s", err, CodeSchemaInvalid)
	}
	document = validDocument(KindForkDelta)
	document.ForkDelta.ChangedPaths[0].NewMode = "644"
	if err := Validate(document); !hasCode(err, CodeSchemaInvalid) {
		t.Fatalf("git mode = %v, want %s", err, CodeSchemaInvalid)
	}
	document = validDocument(KindELFDependency)
	document.ELFDependency.ELFs[0].ProgramHeaders[0].Type = "LOAD\n"
	if err := Validate(document); !hasCode(err, CodeSchemaInvalid) {
		t.Fatalf("ELF text = %v, want %s", err, CodeSchemaInvalid)
	}
}

func validDocument(kind Kind) Document {
	document := Document{
		Format: FormatV1,
		Schema: SchemaFor(kind),
		Kind:   kind,
		Authority: Authority{
			UpstreamCommit:   strings.Repeat("1", 40),
			UpstreamTree:     strings.Repeat("2", 40),
			ForkCommit:       strings.Repeat("3", 40),
			ForkTree:         strings.Repeat("4", 40),
			ForkParentCommit: strings.Repeat("1", 40),
			PatchCommits:     []string{strings.Repeat("3", 40)},
			SourceDateEpoch:  1786215171,
			VDate:            "260808",
		},
		NormalizationVersion: NormalizationV1,
	}
	switch kind {
	case KindSourceSet:
		document.SourceSet = &SourceSetPolicy{Completeness: CompletenessDirect, BuildProfile: BuildProfile{Verbose: 1}, Makefile: validSourceRecord("Makefile", "build-config"), ForkSourceSetChange: "none", Records: []SourceRecord{validSourceRecord("bin/MiSTer", "binary-image")}}
	case KindCompileLink:
		document.CompileLink = &CompileLinkPolicy{Completeness: CompletenessObserved, Records: []CompileRecord{validCompileRecord()}}
	case KindELFDependency:
		document.ELFDependency = &ELFDependencyPolicy{Completeness: CompletenessObserved, ELFs: []ELFRecord{validELFRecord()}, Dependencies: []DependencyRecord{validDependencyRecord()}}
	case KindGeneratedInput:
		document.GeneratedInput = &GeneratedInputPolicy{Completeness: CompletenessObserved, Records: []GeneratedRecord{{Path: "bin/main.cpp.d", Kind: "dependency", ProducerKey: "main.cpp", ConsumerKeys: []string{"compile-main"}, SourcePaths: []string{"main.cpp"}, Normalization: "lf-v1", ExpectedMode: "0644"}}}
	case KindIntermediatePath:
		document.IntermediatePath = &IntermediatePathPolicy{Completeness: CompletenessObserved, Records: []IntermediateRecord{{Path: "bin/main.cpp.o", Class: "object", ProducerKey: "compile-main", ExpectedMode: "0644"}}}
	case KindForkDelta:
		document.ForkDelta = &ForkDeltaPolicy{Completeness: CompletenessComplete, Purpose: "deterministic-vdate-input", PatchDiffSHA256: strings.Repeat("a", 64), ForkSourceSetChange: "none", ChangedPaths: []ChangedPath{{Path: "Makefile", Status: "modified", OldMode: "100644", NewMode: "100644", OldBlob: strings.Repeat("5", 40), NewBlob: strings.Repeat("6", 40), OldSHA256: strings.Repeat("7", 64), NewSHA256: strings.Repeat("8", 64)}}}
	}
	return document
}

func validSourceRecord(path, reason string) SourceRecord {
	return SourceRecord{Path: path, GitMode: "100644", BlobOID: strings.Repeat("a", 40), SHA256: strings.Repeat("b", 64), MaterialID: "main-fork", InclusionReason: reason}
}

func validCompileRecord() CompileRecord {
	return CompileRecord{Output: "bin/main.cpp.o", Source: "main.cpp", Phase: "compile", ToolRole: "compiler", ToolLogicalPath: "/stage-a0/toolchain/bin/arm-none-linux-gnueabihf-gcc", CWD: "/stage-a0/src", Argv: []string{"arm-none-linux-gnueabihf-gcc", "-c", "main.cpp"}, OrderedInputs: []string{"main.cpp"}, Outputs: []string{"bin/main.cpp.o"}}
}

func validELFRecord() ELFRecord {
	return ELFRecord{Path: "bin/MiSTer", Role: "final-stripped", Class: "ELF32", Data: "LSB", Machine: "ARM", OSABI: "SYSV", ABIFlags: "EABI5-hard-float", Entry: "0x18e29", Interpreter: "/lib/ld-linux-armhf.so.3", InterpreterLogicalPath: "/stage-a0/sysroot/lib/ld-linux-armhf.so.3", ProgramHeaders: []ProgramHeader{{Type: "LOAD", Offset: "0x0", VirtualAddress: "0x10000", PhysicalAddress: "0x10000", FileSize: "0x10b21c", MemorySize: "0x10b21c", Flags: "R E", Align: "0x10000"}}, Sections: []Section{{Name: ".text", Type: "PROGBITS", Address: "0x15230", Offset: "0x5230", Size: "0xde594", EntrySize: "0", Flags: "AX", Link: "0", Info: "0", Align: "8"}}, Needed: []string{"libc.so.6"}, NormalizedReadelfSHA256: strings.Repeat("c", 64), NormalizedObjdumpSHA256: strings.Repeat("d", 64)}
}

func validDependencyRecord() DependencyRecord {
	return DependencyRecord{SONAME: "libc.so.6", LogicalPath: "/stage-a0/sysroot/lib/arm-linux-gnueabihf/libc.so.6", RealLogicalPath: "/stage-a0/sysroot/lib/arm-linux-gnueabihf/libc-2.31.so", SymlinkChain: []string{"/stage-a0/sysroot/lib/arm-linux-gnueabihf/libc.so.6"}, ABI: "ARM EABI5", Size: 1, SHA256: strings.Repeat("e", 64), MaterialID: "toolchain", SourcePackage: "libc6"}
}

func hasCode(err error, want Code) bool {
	var failure *Failure
	return errors.As(err, &failure) && failure.Code == want
}
