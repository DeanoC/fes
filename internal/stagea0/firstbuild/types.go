package firstbuild

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
)

const (
	FormatV1                     = 1
	SchemaV1                     = "fogcast.stage-a0.first-build.v1"
	EvidenceStatusSoftwareTested = "Software-tested"

	ExpectedMainBranch       = "fogcast/stage-a-baseline"
	ExpectedMainCommit       = "d1a3a4e65c2dbee1f23eb5a890d8f29e6448c30d"
	ExpectedMainTree         = "efb9c24e8e27945a75d8c497b4b99ec249129075"
	ExpectedMainParent       = "7b5c8de5d3fb16f9cccc1f274a2ff1b481637e42"
	ExpectedMainSubject      = "stage-a0: make VDATE reproducible"
	ExpectedMainTrackedFiles = 420

	ExpectedToolchainArchiveSize   int64 = 104607124
	ExpectedToolchainArchiveSHA256       = "102825ae56c9e00142d06f35d2bdd3299edb6060e84a275a25b095e66fd3fc2a"
	ExpectedToolchainArchiveRoot         = "gcc-arm-10.2-2020.11-x86_64-arm-none-linux-gnueabihf"
	ExpectedCompiler                     = "arm-none-linux-gnueabihf-gcc"
	ExpectedCompilerVersion              = "arm-none-linux-gnueabihf-gcc (GNU Toolchain for the A-profile Architecture 10.2-2020.11 (arm-10.16)) 10.2.1 20201103"

	ExpectedContainerReference    = "stage-a0-firstbuild:debian12-arm102-v1"
	ExpectedContainerImageID      = "sha256:24045e0e800b0ce7df88076ccab628387b149f1bd0786fab46fffae07a859d0c"
	ExpectedContainerOS           = "linux"
	ExpectedContainerArchitecture = "amd64"
	ExpectedNetworkMode           = "none"

	ExpectedSourceDateEpoch int64 = 1786215171
	ExpectedVDate                 = "260808"

	ExpectedMiSTerSize                     int64 = 1157996
	ExpectedMiSTerSHA256                         = "f9e6fd646740449186b74821a3684686d5dbc7b33e28052e9de28b8f4c751f2e"
	ExpectedMiSTerELFSize                  int64 = 1380136
	ExpectedMiSTerELFSHA256                      = "51a9864bb12ebdf8961b30ac0a45d533fd2a2885a96d81fc29f8363d1804706d"
	ExpectedMiSTerELFDescription                 = "ELF 32-bit LSB executable, ARM, EABI5 version 1 (SYSV), dynamically linked, interpreter /lib/ld-linux-armhf.so.3, for GNU/Linux 3.2.0, stripped"
	ExpectedMiSTerELFUnstrippedDescription       = "ELF 32-bit LSB executable, ARM, EABI5 version 1 (SYSV), dynamically linked, interpreter /lib/ld-linux-armhf.so.3, for GNU/Linux 3.2.0, with debug_info, not stripped"
	ExpectedBinInventoryCount                    = 227
	// Derived from the reviewed prepared-image capture's sorted logical
	// inventory. It is intentionally not a hash of the JSON report itself.
	ExpectedBinInventorySHA256 = "7a2e77ffa919e504a4baa638d6b820085a4464a15281449f1abc2f4d66419cd5"
)

type Code string

const (
	CodeSourceDrift    Code = "FIRSTBUILD_SOURCE_DRIFT"
	CodeToolchainDrift Code = "FIRSTBUILD_TOOLCHAIN_DRIFT"
	CodeContainerDrift Code = "FIRSTBUILD_CONTAINER_DRIFT"
	CodeOutputInvalid  Code = "FIRSTBUILD_OUTPUT_INVALID"
	CodeBuildFailed    Code = "FIRSTBUILD_BUILD_FAILED"
	CodeSchemaInvalid  Code = "FIRSTBUILD_SCHEMA_INVALID"
)

type Failure struct {
	Code   Code
	Detail string
}

func (f *Failure) Error() string { return string(f.Code) + ": " + f.Detail }

type Evidence struct {
	Format                 int               `json:"format"`
	Schema                 string            `json:"schema"`
	Status                 string            `json:"status"`
	CanonicalStageA0Report bool              `json:"canonical_stage_a0_report"`
	TwoBuildsByteIdentical bool              `json:"two_builds_byte_identical"`
	Source                 SourceEvidence    `json:"source"`
	Toolchain              ToolchainEvidence `json:"toolchain"`
	Container              ContainerEvidence `json:"container"`
	Build                  BuildEvidence     `json:"build"`
	Output                 OutputEvidence    `json:"output"`
}

type SourceEvidence struct {
	Branch       string `json:"branch"`
	Commit       string `json:"commit"`
	Tree         string `json:"tree"`
	Parent       string `json:"parent"`
	Subject      string `json:"subject"`
	TrackedFiles int    `json:"tracked_files"`
	Clean        bool   `json:"clean"`
}

type ToolchainEvidence struct {
	ArchiveSize     int64  `json:"archive_size"`
	ArchiveSHA256   string `json:"archive_sha256"`
	ArchiveRoot     string `json:"archive_root"`
	Compiler        string `json:"compiler"`
	CompilerVersion string `json:"compiler_version"`
}

type ContainerEvidence struct {
	Reference    string `json:"reference"`
	ImageID      string `json:"image_id"`
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
}

type BuildEvidence struct {
	VDate           string   `json:"vdate"`
	SourceDateEpoch int64    `json:"source_date_epoch"`
	Command         []string `json:"command"`
	NetworkMode     string   `json:"network_mode"`
	ExitCode        int      `json:"exit_code"`
}

type OutputEvidence struct {
	InventoryCount  int                `json:"inventory_count"`
	InventorySHA256 string             `json:"inventory_sha256"`
	Inventory       []InventoryEntry   `json:"inventory"`
	Artifacts       []ArtifactEvidence `json:"artifacts"`
}

type InventoryEntry struct {
	Path   string `json:"path"`
	Mode   string `json:"mode"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type ArtifactEvidence struct {
	Path     string `json:"path"`
	Mode     string `json:"mode"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	ELF      string `json:"elf"`
	Stripped bool   `json:"stripped"`
}

func EncodeEvidence(evidence Evidence) ([]byte, error) {
	if err := ValidateEvidence(evidence); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(evidence)
	if err != nil {
		return nil, &Failure{Code: CodeSchemaInvalid, Detail: "evidence cannot be encoded"}
	}
	return append(raw, '\n'), nil
}

func InventoryDigest(entries []InventoryEntry) (string, error) {
	ordered := append([]InventoryEntry(nil), entries...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })
	hash := sha256.New()
	seen := make(map[string]struct{}, len(ordered))
	for _, entry := range ordered {
		if err := validateInventoryEntry(entry); err != nil {
			return "", err
		}
		if _, ok := seen[entry.Path]; ok {
			return "", &Failure{Code: CodeOutputInvalid, Detail: "inventory contains duplicate path"}
		}
		seen[entry.Path] = struct{}{}
		_, _ = fmt.Fprintf(hash, "%s\x00%s\x00%d\x00%s\n", entry.Path, entry.Mode, entry.Size, entry.SHA256)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func ValidateEvidence(evidence Evidence) error {
	if evidence.Format != FormatV1 || evidence.Schema != SchemaV1 || evidence.Status != EvidenceStatusSoftwareTested || evidence.CanonicalStageA0Report || evidence.TwoBuildsByteIdentical {
		return &Failure{Code: CodeSchemaInvalid, Detail: "evidence schema or status is not preliminary v1"}
	}
	if err := validateSource(evidence.Source); err != nil {
		return err
	}
	if err := validateToolchain(evidence.Toolchain); err != nil {
		return err
	}
	if err := validateContainer(evidence.Container); err != nil {
		return err
	}
	if err := validateBuild(evidence.Build); err != nil {
		return err
	}
	if err := validateOutput(evidence.Output); err != nil {
		return err
	}
	return nil
}

func ValidateCapturedEvidence(evidence Evidence) error {
	if err := ValidateEvidence(evidence); err != nil {
		return err
	}
	if evidence.Output.InventoryCount != ExpectedBinInventoryCount {
		return &Failure{Code: CodeOutputInvalid, Detail: "bin inventory count differs from reviewed baseline"}
	}
	if ExpectedBinInventorySHA256 != "" && evidence.Output.InventorySHA256 != ExpectedBinInventorySHA256 {
		return &Failure{Code: CodeOutputInvalid, Detail: "bin inventory digest differs from reviewed baseline"}
	}
	return nil
}

func validateSource(source SourceEvidence) error {
	if source.Branch != ExpectedMainBranch || source.Commit != ExpectedMainCommit || source.Tree != ExpectedMainTree || source.Parent != ExpectedMainParent || source.Subject != ExpectedMainSubject || source.TrackedFiles != ExpectedMainTrackedFiles || !source.Clean {
		return &Failure{Code: CodeSourceDrift, Detail: "source identity differs from reviewed baseline"}
	}
	return nil
}

func validateToolchain(toolchain ToolchainEvidence) error {
	if toolchain.ArchiveSize != ExpectedToolchainArchiveSize || toolchain.ArchiveSHA256 != ExpectedToolchainArchiveSHA256 || toolchain.ArchiveRoot != ExpectedToolchainArchiveRoot || toolchain.Compiler != ExpectedCompiler || toolchain.CompilerVersion != ExpectedCompilerVersion {
		return &Failure{Code: CodeToolchainDrift, Detail: "toolchain identity differs from reviewed baseline"}
	}
	return nil
}

func validateContainer(container ContainerEvidence) error {
	if container.Reference != ExpectedContainerReference || container.ImageID != ExpectedContainerImageID || container.OS != ExpectedContainerOS || container.Architecture != ExpectedContainerArchitecture {
		return &Failure{Code: CodeContainerDrift, Detail: "container identity differs from reviewed baseline"}
	}
	return nil
}

func validateBuild(build BuildEvidence) error {
	if build.VDate != ExpectedVDate || build.SourceDateEpoch != ExpectedSourceDateEpoch || build.NetworkMode != ExpectedNetworkMode || build.ExitCode != 0 || len(build.Command) != 3 || build.Command[0] != "make" || build.Command[1] != "V=1" || build.Command[2] != "VDATE=260808" {
		return &Failure{Code: CodeBuildFailed, Detail: "build observation differs from reviewed recipe"}
	}
	return nil
}

func validateOutput(output OutputEvidence) error {
	if output.InventoryCount != len(output.Inventory) {
		return &Failure{Code: CodeOutputInvalid, Detail: "inventory count does not match entries"}
	}
	for i := 1; i < len(output.Inventory); i++ {
		if output.Inventory[i-1].Path >= output.Inventory[i].Path {
			return &Failure{Code: CodeOutputInvalid, Detail: "inventory is not in logical path order"}
		}
	}
	digest, err := InventoryDigest(output.Inventory)
	if err != nil {
		return err
	}
	if output.InventorySHA256 != digest {
		return &Failure{Code: CodeOutputInvalid, Detail: "inventory digest does not match entries"}
	}
	if len(output.Artifacts) != 2 {
		return &Failure{Code: CodeOutputInvalid, Detail: "required final artifact set is incomplete"}
	}
	for i, want := range []struct {
		path  string
		size  int64
		hash  string
		strip bool
	}{
		{path: "bin/MiSTer", size: ExpectedMiSTerSize, hash: ExpectedMiSTerSHA256, strip: true},
		{path: "bin/MiSTer.elf", size: ExpectedMiSTerELFSize, hash: ExpectedMiSTerELFSHA256, strip: false},
	} {
		artifact := output.Artifacts[i]
		wantELF := ExpectedMiSTerELFUnstrippedDescription
		if want.strip {
			wantELF = ExpectedMiSTerELFDescription
		}
		if artifact.Path != want.path || artifact.Mode != "0755" || artifact.Size != want.size || artifact.SHA256 != want.hash || artifact.ELF != wantELF || artifact.Stripped != want.strip {
			return &Failure{Code: CodeOutputInvalid, Detail: "final artifact differs from reviewed baseline"}
		}
		found := false
		for _, entry := range output.Inventory {
			if entry.Path == artifact.Path && entry.Mode == artifact.Mode && entry.Size == artifact.Size && entry.SHA256 == artifact.SHA256 {
				found = true
				break
			}
		}
		if !found {
			return &Failure{Code: CodeOutputInvalid, Detail: "final artifact is absent from inventory"}
		}
	}
	return nil
}

func validateInventoryEntry(entry InventoryEntry) error {
	if entry.Path == "" || path.IsAbs(entry.Path) || strings.Contains(entry.Path, "\\") || strings.HasPrefix(entry.Path, "../") || strings.Contains(entry.Path, "/../") || path.Clean(entry.Path) != entry.Path || !strings.HasPrefix(entry.Path, "bin/") {
		return &Failure{Code: CodeOutputInvalid, Detail: "inventory contains a non-logical path"}
	}
	if len(entry.SHA256) != sha256.Size*2 || !isLowerHex(entry.SHA256) || !isOctalMode(entry.Mode) || entry.Size < 0 {
		return &Failure{Code: CodeOutputInvalid, Detail: "inventory entry has invalid metadata"}
	}
	return nil
}

func isOctalMode(value string) bool {
	if len(value) != 4 || value[0] != '0' {
		return false
	}
	for _, r := range value[1:] {
		if r < '0' || r > '7' {
			return false
		}
	}
	return true
}

func isLowerHex(value string) bool {
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}
