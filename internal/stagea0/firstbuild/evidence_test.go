package firstbuild

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestEncodeEvidenceIsCanonicalAndExcludesRunMetadata(t *testing.T) {
	evidence := validEvidence()
	first, err := EncodeEvidence(evidence)
	if err != nil {
		t.Fatalf("first encode: %v", err)
	}
	second, err := EncodeEvidence(evidence)
	if err != nil {
		t.Fatalf("second encode: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("encoding the same observation produced different bytes")
	}
	if !bytes.HasSuffix(first, []byte("\n")) {
		t.Fatal("canonical JSON is not LF terminated")
	}
	for _, forbidden := range []string{
		"source_dir", "toolchain_archive", "output_dir", "created_at", "timestamp",
		"/Users/", "/tmp/", "hostname", "pid", "build.log",
	} {
		if bytes.Contains(first, []byte(forbidden)) {
			t.Fatalf("report contains forbidden run metadata %q: %s", forbidden, first)
		}
	}
	if !bytes.Contains(first, []byte(`"status":"Software-tested"`)) {
		t.Fatalf("report omitted evidence status: %s", first)
	}
	if !bytes.Contains(first, []byte(`"canonical_stage_a0_report":false`)) {
		t.Fatalf("report did not explicitly mark itself non-canonical: %s", first)
	}
}

func TestDecodeEvidenceRequiresCanonicalJSON(t *testing.T) {
	raw, err := EncodeEvidence(validEvidence())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeEvidence(raw)
	if err != nil {
		t.Fatalf("DecodeEvidence(valid) = %v", err)
	}
	if got, err := EncodeEvidence(decoded); err != nil || string(got) != string(raw) {
		t.Fatalf("decode/re-encode changed canonical bytes: err=%v", err)
	}
	for _, hostile := range [][]byte{
		append([]byte(" "), raw...),
		append(append([]byte(nil), raw...), []byte("{}")...),
		[]byte(`{"format":1,"schema":"fogcast.stage-a0.first-build.v1","unknown":true}` + "\n"),
	} {
		if _, err := DecodeEvidence(hostile); !hasCode(err, CodeSchemaInvalid) {
			t.Fatalf("DecodeEvidence(%q) = %v, want %s", hostile, err, CodeSchemaInvalid)
		}
	}
}

func TestInventoryDigestSortsLogicalEntriesAndRejectsPhysicalPaths(t *testing.T) {
	entries := []InventoryEntry{
		{Path: "bin/z", Mode: "0755", Size: 3, SHA256: strings.Repeat("b", 64)},
		{Path: "bin/a", Mode: "0644", Size: 2, SHA256: strings.Repeat("a", 64)},
	}
	first, err := InventoryDigest(entries)
	if err != nil {
		t.Fatalf("first digest: %v", err)
	}
	second, err := InventoryDigest([]InventoryEntry{entries[1], entries[0]})
	if err != nil {
		t.Fatalf("second digest: %v", err)
	}
	if first != second {
		t.Fatalf("entry order changed digest: %s != %s", first, second)
	}
	_, err = InventoryDigest([]InventoryEntry{{Path: "/Users/operator/Main_MiSTer/bin/MiSTer", Mode: "0755", Size: 1, SHA256: strings.Repeat("c", 64)}})
	if !hasCode(err, CodeOutputInvalid) {
		t.Fatalf("physical path error = %v, want %s", err, CodeOutputInvalid)
	}
	_, err = InventoryDigest([]InventoryEntry{{Path: "bin/a", Mode: "0644", Size: 1, SHA256: strings.Repeat("c", 64)}, {Path: "bin/a", Mode: "0644", Size: 1, SHA256: strings.Repeat("c", 64)}})
	if !hasCode(err, CodeOutputInvalid) {
		t.Fatalf("duplicate path error = %v, want %s", err, CodeOutputInvalid)
	}
	_, err = InventoryDigest([]InventoryEntry{{Path: "bin/a", Mode: "08x4", Size: 1, SHA256: strings.Repeat("c", 64)}})
	if !hasCode(err, CodeOutputInvalid) {
		t.Fatalf("invalid mode error = %v, want %s", err, CodeOutputInvalid)
	}
}

func TestValidateEvidenceRejectsEveryLockedDriftClass(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Evidence)
		code   Code
	}{
		{name: "source", mutate: func(e *Evidence) { e.Source.Commit = strings.Repeat("0", 40) }, code: CodeSourceDrift},
		{name: "toolchain", mutate: func(e *Evidence) { e.Toolchain.ArchiveSHA256 = strings.Repeat("0", 64) }, code: CodeToolchainDrift},
		{name: "container", mutate: func(e *Evidence) { e.Container.ImageID = "sha256:" + strings.Repeat("0", 64) }, code: CodeContainerDrift},
		{name: "output", mutate: func(e *Evidence) { e.Output.Artifacts[0].SHA256 = strings.Repeat("0", 64) }, code: CodeOutputInvalid},
		{name: "build", mutate: func(e *Evidence) { e.Build.Command = []string{"make", "V=1"} }, code: CodeBuildFailed},
		{name: "schema", mutate: func(e *Evidence) { e.Schema = "wrong.schema" }, code: CodeSchemaInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence := validEvidence()
			test.mutate(&evidence)
			if err := ValidateEvidence(evidence); !hasCode(err, test.code) {
				t.Fatalf("ValidateEvidence() = %v, want %s", err, test.code)
			}
		})
	}
}

func TestValidateEvidenceRequiresPreliminaryStatus(t *testing.T) {
	evidence := validEvidence()
	evidence.Status = "Reproducible"
	if err := ValidateEvidence(evidence); !hasCode(err, CodeSchemaInvalid) {
		t.Fatalf("status validation = %v, want %s", err, CodeSchemaInvalid)
	}
	evidence = validEvidence()
	evidence.CanonicalStageA0Report = true
	if err := ValidateEvidence(evidence); !hasCode(err, CodeSchemaInvalid) {
		t.Fatalf("canonical flag validation = %v, want %s", err, CodeSchemaInvalid)
	}
}

func validEvidence() Evidence {
	entries := []InventoryEntry{
		{Path: "bin/MiSTer", Mode: "0755", Size: ExpectedMiSTerSize, SHA256: ExpectedMiSTerSHA256},
		{Path: "bin/MiSTer.elf", Mode: "0755", Size: ExpectedMiSTerELFSize, SHA256: ExpectedMiSTerELFSHA256},
	}
	digest, _ := InventoryDigest(entries)
	return Evidence{
		Format:                 FormatV1,
		Schema:                 SchemaV1,
		Status:                 EvidenceStatusSoftwareTested,
		CanonicalStageA0Report: false,
		TwoBuildsByteIdentical: false,
		Source: SourceEvidence{
			Branch:       ExpectedMainBranch,
			Commit:       ExpectedMainCommit,
			Tree:         ExpectedMainTree,
			Parent:       ExpectedMainParent,
			Subject:      ExpectedMainSubject,
			TrackedFiles: ExpectedMainTrackedFiles,
			Clean:        true,
		},
		Toolchain: ToolchainEvidence{
			ArchiveSize:     ExpectedToolchainArchiveSize,
			ArchiveSHA256:   ExpectedToolchainArchiveSHA256,
			ArchiveRoot:     ExpectedToolchainArchiveRoot,
			Compiler:        ExpectedCompiler,
			CompilerVersion: ExpectedCompilerVersion,
		},
		Container: ContainerEvidence{
			Reference:    ExpectedContainerReference,
			ImageID:      ExpectedContainerImageID,
			OS:           ExpectedContainerOS,
			Architecture: ExpectedContainerArchitecture,
		},
		Build: BuildEvidence{
			VDate:           ExpectedVDate,
			SourceDateEpoch: ExpectedSourceDateEpoch,
			Command:         []string{"make", "V=1", "VDATE=260808"},
			NetworkMode:     ExpectedNetworkMode,
			ExitCode:        0,
		},
		Output: OutputEvidence{
			InventoryCount:  len(entries),
			InventorySHA256: digest,
			Inventory:       entries,
			Artifacts: []ArtifactEvidence{
				{Path: "bin/MiSTer", Mode: "0755", Size: ExpectedMiSTerSize, SHA256: ExpectedMiSTerSHA256, ELF: ExpectedMiSTerELFDescription, Stripped: true},
				{Path: "bin/MiSTer.elf", Mode: "0755", Size: ExpectedMiSTerELFSize, SHA256: ExpectedMiSTerELFSHA256, ELF: ExpectedMiSTerELFUnstrippedDescription, Stripped: false},
			},
		},
	}
}

func hasCode(err error, want Code) bool {
	var failure *Failure
	return errors.As(err, &failure) && failure.Code == want
}
