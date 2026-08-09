// Package policy contains the closed, deterministic JSON policy documents
// consumed by the Stage A0 lock and build evidence tools.  The policy files
// are observations/candidates until their material provenance and review
// gates are complete; this package never promotes a candidate by itself.
package policy

import (
	"bytes"
	"encoding/json"
	"io"
	"path"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	FormatV1             = 1
	NormalizationV1      = 1
	SchemaPrefixV1       = "fogcast.stage-a0.policy."
	CompletenessComplete = "complete"
	CompletenessObserved = "candidate-observed"
	CompletenessDirect   = "candidate-direct-inputs"
)

type Kind string

const (
	KindSourceSet        Kind = "source-set"
	KindCompileLink      Kind = "compile-link"
	KindELFDependency    Kind = "elf-dependency"
	KindGeneratedInput   Kind = "generated-input"
	KindIntermediatePath Kind = "intermediate-path"
	KindForkDelta        Kind = "upstream-fork-delta"
)

var allKinds = []Kind{
	KindSourceSet,
	KindCompileLink,
	KindELFDependency,
	KindGeneratedInput,
	KindIntermediatePath,
	KindForkDelta,
}

type Code string

const (
	CodeSchemaInvalid Code = "POLICY_SCHEMA_INVALID"
	CodePathInvalid   Code = "POLICY_PATH_INVALID"
	CodeOrderInvalid  Code = "POLICY_ORDER_INVALID"
	CodeHashInvalid   Code = "POLICY_HASH_INVALID"
)

type Failure struct {
	Code   Code
	Detail string
}

func (f *Failure) Error() string { return string(f.Code) + ": " + f.Detail }

// Document is the common envelope plus exactly one kind-specific payload.
// Structs (rather than maps) are intentional: field order is stable and
// unknown/duplicate JSON fields cannot be silently retained.
type Document struct {
	Format               int                     `json:"format"`
	Schema               string                  `json:"schema"`
	Kind                 Kind                    `json:"kind"`
	Authority            Authority               `json:"authority"`
	NormalizationVersion int                     `json:"normalization_version"`
	SourceSet            *SourceSetPolicy        `json:"source_set,omitempty"`
	CompileLink          *CompileLinkPolicy      `json:"compile_link,omitempty"`
	ELFDependency        *ELFDependencyPolicy    `json:"elf_dependency,omitempty"`
	GeneratedInput       *GeneratedInputPolicy   `json:"generated_input,omitempty"`
	IntermediatePath     *IntermediatePathPolicy `json:"intermediate_path,omitempty"`
	ForkDelta            *ForkDeltaPolicy        `json:"upstream_fork_delta,omitempty"`
}

type Authority struct {
	UpstreamCommit   string   `json:"upstream_commit"`
	UpstreamTree     string   `json:"upstream_tree"`
	ForkCommit       string   `json:"fork_commit"`
	ForkTree         string   `json:"fork_tree"`
	ForkParentCommit string   `json:"fork_parent_commit"`
	PatchCommits     []string `json:"patch_commits"`
	SourceDateEpoch  int64    `json:"source_date_epoch"`
	VDate            string   `json:"vdate"`
}

type SourceSetPolicy struct {
	Completeness        string         `json:"completeness"`
	BuildProfile        BuildProfile   `json:"build_profile"`
	Makefile            SourceRecord   `json:"makefile"`
	ForkSourceSetChange string         `json:"fork_source_set_change"`
	Records             []SourceRecord `json:"records"`
}

type BuildProfile struct {
	Debug     int `json:"debug"`
	Profiling int `json:"profiling"`
	Verbose   int `json:"v"`
}

type SourceRecord struct {
	Path            string `json:"path"`
	GitMode         string `json:"git_mode"`
	BlobOID         string `json:"blob_oid"`
	SHA256          string `json:"sha256"`
	MaterialID      string `json:"material_id"`
	InclusionReason string `json:"inclusion_reason"`
}

type CompileLinkPolicy struct {
	Completeness string          `json:"completeness"`
	Records      []CompileRecord `json:"records"`
}

type CompileRecord struct {
	Output          string   `json:"output"`
	Source          string   `json:"source"`
	Phase           string   `json:"phase"`
	ToolRole        string   `json:"tool_role"`
	ToolLogicalPath string   `json:"tool_logical_path"`
	CWD             string   `json:"cwd"`
	Argv            []string `json:"argv"`
	OrderedInputs   []string `json:"ordered_inputs"`
	Outputs         []string `json:"outputs"`
}

type ELFDependencyPolicy struct {
	Completeness string             `json:"completeness"`
	ELFs         []ELFRecord        `json:"elfs"`
	Dependencies []DependencyRecord `json:"dependencies"`
}

type ELFRecord struct {
	Path                    string          `json:"path"`
	Role                    string          `json:"role"`
	Class                   string          `json:"class"`
	Data                    string          `json:"data"`
	Machine                 string          `json:"machine"`
	OSABI                   string          `json:"osabi"`
	ABIFlags                string          `json:"abi_flags"`
	Entry                   string          `json:"entry"`
	Interpreter             string          `json:"interpreter"`
	InterpreterLogicalPath  string          `json:"interpreter_logical_path"`
	ProgramHeaders          []ProgramHeader `json:"program_headers"`
	Sections                []Section       `json:"sections"`
	BuildID                 string          `json:"build_id"`
	Needed                  []string        `json:"needed"`
	RPath                   string          `json:"rpath"`
	RunPath                 string          `json:"runpath"`
	NormalizedReadelfSHA256 string          `json:"normalized_readelf_sha256"`
	NormalizedObjdumpSHA256 string          `json:"normalized_objdump_sha256"`
}

type ProgramHeader struct {
	Type            string `json:"type"`
	Offset          string `json:"offset"`
	VirtualAddress  string `json:"virtual_address"`
	PhysicalAddress string `json:"physical_address"`
	FileSize        string `json:"file_size"`
	MemorySize      string `json:"memory_size"`
	Flags           string `json:"flags"`
	Align           string `json:"align"`
}

type Section struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Address   string `json:"address"`
	Offset    string `json:"offset"`
	Size      string `json:"size"`
	EntrySize string `json:"entry_size"`
	Flags     string `json:"flags"`
	Link      string `json:"link"`
	Info      string `json:"info"`
	Align     string `json:"align"`
}

type DependencyRecord struct {
	SONAME          string   `json:"soname"`
	LogicalPath     string   `json:"logical_path"`
	RealLogicalPath string   `json:"real_logical_path"`
	SymlinkChain    []string `json:"symlink_chain"`
	ABI             string   `json:"abi"`
	Size            int64    `json:"size"`
	SHA256          string   `json:"sha256"`
	MaterialID      string   `json:"material_id"`
	SourcePackage   string   `json:"source_package"`
}

type GeneratedInputPolicy struct {
	Completeness string            `json:"completeness"`
	Records      []GeneratedRecord `json:"records"`
}

type GeneratedRecord struct {
	Path          string   `json:"path"`
	Kind          string   `json:"kind"`
	ProducerKey   string   `json:"producer_key"`
	ConsumerKeys  []string `json:"consumer_keys"`
	SourcePaths   []string `json:"source_paths"`
	Normalization string   `json:"normalization"`
	ExpectedMode  string   `json:"expected_mode"`
}

type IntermediatePathPolicy struct {
	Completeness string               `json:"completeness"`
	Records      []IntermediateRecord `json:"records"`
}

type IntermediateRecord struct {
	Path         string `json:"path"`
	Class        string `json:"class"`
	ProducerKey  string `json:"producer_key"`
	ExpectedMode string `json:"expected_mode"`
}

type ForkDeltaPolicy struct {
	Completeness        string        `json:"completeness"`
	Purpose             string        `json:"purpose"`
	PatchDiffSHA256     string        `json:"patch_diff_sha256"`
	ForkSourceSetChange string        `json:"fork_source_set_change"`
	ChangedPaths        []ChangedPath `json:"changed_paths"`
}

type ChangedPath struct {
	Path      string `json:"path"`
	Status    string `json:"status"`
	OldMode   string `json:"old_mode"`
	NewMode   string `json:"new_mode"`
	OldBlob   string `json:"old_blob"`
	NewBlob   string `json:"new_blob"`
	OldSHA256 string `json:"old_sha256"`
	NewSHA256 string `json:"new_sha256"`
}

// SchemaFor returns the exact schema string for a kind.
func SchemaFor(kind Kind) string { return SchemaPrefixV1 + string(kind) + ".v1" }

// Kinds returns the six V1 policy kinds in their stable schema order.
func Kinds() []Kind { return append([]Kind(nil), allKinds...) }

func Encode(document Document) ([]byte, error) {
	if err := Validate(document); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(document)
	if err != nil {
		return nil, &Failure{Code: CodeSchemaInvalid, Detail: "policy cannot be encoded"}
	}
	return append(raw, '\n'), nil
}

func Decode(raw []byte) (Document, error) {
	var document Document
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return Document{}, &Failure{Code: CodeSchemaInvalid, Detail: "policy JSON cannot be decoded"}
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Document{}, &Failure{Code: CodeSchemaInvalid, Detail: "policy JSON has trailing data"}
	}
	canonical, err := Encode(document)
	if err != nil || !bytes.Equal(canonical, raw) {
		return Document{}, &Failure{Code: CodeSchemaInvalid, Detail: "policy JSON is not canonical"}
	}
	return document, nil
}

func Validate(document Document) error {
	if document.Format != FormatV1 || !isKind(document.Kind) || document.Schema != SchemaFor(document.Kind) || document.NormalizationVersion != NormalizationV1 {
		return schemaFailure("envelope is invalid")
	}
	if err := validateAuthority(document.Authority); err != nil {
		return err
	}
	count := 0
	if document.SourceSet != nil {
		count++
		if document.Kind != KindSourceSet {
			return schemaFailure("source_set payload does not match kind")
		}
		if err := validateSourceSet(*document.SourceSet); err != nil {
			return err
		}
	}
	if document.CompileLink != nil {
		count++
		if document.Kind != KindCompileLink {
			return schemaFailure("compile_link payload does not match kind")
		}
		if err := validateCompileLink(*document.CompileLink); err != nil {
			return err
		}
	}
	if document.ELFDependency != nil {
		count++
		if document.Kind != KindELFDependency {
			return schemaFailure("elf_dependency payload does not match kind")
		}
		if err := validateELFDependency(*document.ELFDependency); err != nil {
			return err
		}
	}
	if document.GeneratedInput != nil {
		count++
		if document.Kind != KindGeneratedInput {
			return schemaFailure("generated_input payload does not match kind")
		}
		if err := validateGeneratedInput(*document.GeneratedInput); err != nil {
			return err
		}
	}
	if document.IntermediatePath != nil {
		count++
		if document.Kind != KindIntermediatePath {
			return schemaFailure("intermediate_path payload does not match kind")
		}
		if err := validateIntermediatePath(*document.IntermediatePath); err != nil {
			return err
		}
	}
	if document.ForkDelta != nil {
		count++
		if document.Kind != KindForkDelta {
			return schemaFailure("upstream_fork_delta payload does not match kind")
		}
		if err := validateForkDelta(*document.ForkDelta); err != nil {
			return err
		}
	}
	if count != 1 {
		return schemaFailure("exactly one kind payload is required")
	}
	return nil
}

func validateAuthority(authority Authority) error {
	for _, value := range []string{authority.UpstreamCommit, authority.ForkCommit, authority.ForkParentCommit} {
		if !lowerHex(value, 40) {
			return &Failure{Code: CodeHashInvalid, Detail: "authority commit is invalid"}
		}
	}
	for _, value := range []string{authority.UpstreamTree, authority.ForkTree} {
		if !lowerHex(value, 40) {
			return &Failure{Code: CodeHashInvalid, Detail: "authority tree is invalid"}
		}
	}
	if authority.ForkParentCommit != authority.UpstreamCommit || len(authority.PatchCommits) == 0 || authority.PatchCommits[len(authority.PatchCommits)-1] != authority.ForkCommit {
		return schemaFailure("authority ancestry is invalid")
	}
	seen := make(map[string]struct{}, len(authority.PatchCommits))
	for _, commit := range authority.PatchCommits {
		if !lowerHex(commit, 40) {
			return &Failure{Code: CodeHashInvalid, Detail: "patch commit is invalid"}
		}
		if _, ok := seen[commit]; ok {
			return schemaFailure("patch commits are duplicated")
		}
		seen[commit] = struct{}{}
	}
	if authority.SourceDateEpoch < 0 || authority.SourceDateEpoch > 253402300799 || len(authority.VDate) != 6 || !allASCIIDigits(authority.VDate) || time.Unix(authority.SourceDateEpoch, 0).UTC().Format("060102") != authority.VDate {
		return schemaFailure("authority date is invalid")
	}
	return nil
}

func validateSourceSet(value SourceSetPolicy) error {
	if err := validateCompleteness(value.Completeness); err != nil {
		return err
	}
	if value.BuildProfile != (BuildProfile{Debug: 0, Profiling: 0, Verbose: 1}) {
		return schemaFailure("source-set build profile is invalid")
	}
	if value.ForkSourceSetChange != "none" {
		return schemaFailure("source-set fork change is invalid")
	}
	if err := validateSourceRecord(value.Makefile); err != nil {
		return err
	}
	if value.Makefile.Path != "Makefile" || value.Makefile.InclusionReason != "build-config" {
		return schemaFailure("source-set Makefile record is invalid")
	}
	if len(value.Records) == 0 {
		return schemaFailure("source-set records are empty")
	}
	previous := ""
	for _, record := range value.Records {
		if err := validateSourceRecord(record); err != nil {
			return err
		}
		if record.Path == "Makefile" {
			return schemaFailure("source-set Makefile is duplicated in records")
		}
		if previous >= record.Path {
			return &Failure{Code: CodeOrderInvalid, Detail: "source-set records are not strictly sorted"}
		}
		previous = record.Path
	}
	return nil
}

func validateSourceRecord(record SourceRecord) error {
	if !safeRelative(record.Path) || record.GitMode != "100644" || !lowerHex(record.BlobOID, 40) || !lowerHex(record.SHA256, 64) || !safeID(record.MaterialID) || !validInclusionReason(record.InclusionReason) {
		return schemaFailure("source-set record is invalid")
	}
	return nil
}

func validateCompileLink(value CompileLinkPolicy) error {
	if err := validateCompleteness(value.Completeness); err != nil {
		return err
	}
	if len(value.Records) == 0 {
		return schemaFailure("compile-link records are empty")
	}
	previous := ""
	for _, record := range value.Records {
		if err := validateCompileRecord(record); err != nil {
			return err
		}
		key := record.Output + "\x00" + record.Source + "\x00" + record.ToolRole
		if previous >= key {
			return &Failure{Code: CodeOrderInvalid, Detail: "compile-link records are not strictly sorted"}
		}
		previous = key
	}
	return nil
}

func validateCompileRecord(record CompileRecord) error {
	if !safeRelative(record.Output) || !safeRelative(record.Source) || !validPhase(record.Phase) || !safeID(record.ToolRole) || !safeToolPath(record.ToolLogicalPath) || !safeLogicalPath(record.CWD) || len(record.Argv) == 0 || len(record.OrderedInputs) == 0 || len(record.Outputs) == 0 {
		return schemaFailure("compile-link record is invalid")
	}
	for _, arg := range record.Argv {
		if !safeArgument(arg) {
			return schemaFailure("compile-link argv is invalid")
		}
	}
	for _, input := range record.OrderedInputs {
		if !safeRelativeOrLogical(input) {
			return schemaFailure("compile-link input is invalid")
		}
	}
	for _, output := range record.Outputs {
		if !safeRelativeOrLogical(output) {
			return schemaFailure("compile-link output is invalid")
		}
	}
	return nil
}

func validateELFDependency(value ELFDependencyPolicy) error {
	if err := validateCompleteness(value.Completeness); err != nil {
		return err
	}
	if len(value.ELFs) == 0 || len(value.Dependencies) == 0 {
		return schemaFailure("ELF/dependency closure is empty")
	}
	previous := ""
	for _, elf := range value.ELFs {
		if err := validateELF(elf); err != nil {
			return err
		}
		if previous >= elf.Path {
			return &Failure{Code: CodeOrderInvalid, Detail: "ELF records are not strictly sorted"}
		}
		previous = elf.Path
	}
	previous = ""
	for _, dependency := range value.Dependencies {
		if err := validateDependency(dependency); err != nil {
			return err
		}
		if previous >= dependency.LogicalPath {
			return &Failure{Code: CodeOrderInvalid, Detail: "dependency records are not strictly sorted"}
		}
		previous = dependency.LogicalPath
	}
	return nil
}

func validateELF(elf ELFRecord) error {
	if !safeRelative(elf.Path) || !safeID(elf.Role) || !safeText(elf.Class) || !safeText(elf.Data) || !safeText(elf.Machine) || !safeText(elf.OSABI) || !safeText(elf.ABIFlags) || !safeText(elf.Entry) || !safeText(elf.Interpreter) || !safeLogicalPath(elf.InterpreterLogicalPath) || len(elf.ProgramHeaders) == 0 || len(elf.Sections) == 0 || (elf.BuildID != "" && !lowerHex(elf.BuildID, 64)) || len(elf.Needed) == 0 || !lowerHex(elf.NormalizedReadelfSHA256, 64) || !lowerHex(elf.NormalizedObjdumpSHA256, 64) {
		return schemaFailure("ELF record is invalid")
	}
	for _, needed := range elf.Needed {
		if !safeText(needed) {
			return schemaFailure("ELF dependency name is invalid")
		}
	}
	for _, header := range elf.ProgramHeaders {
		if !validProgramHeader(header) {
			return schemaFailure("ELF program header is invalid")
		}
	}
	for _, section := range elf.Sections {
		if !validSection(section) {
			return schemaFailure("ELF section is invalid")
		}
	}
	return nil
}

func validateDependency(dependency DependencyRecord) error {
	if !safeText(dependency.SONAME) || !safeLogicalPath(dependency.LogicalPath) || !safeLogicalPath(dependency.RealLogicalPath) || dependency.Size < 0 || !lowerHex(dependency.SHA256, 64) || !safeID(dependency.MaterialID) || !safeText(dependency.ABI) || !safeText(dependency.SourcePackage) || len(dependency.SymlinkChain) == 0 {
		return schemaFailure("dependency record is invalid")
	}
	for _, link := range dependency.SymlinkChain {
		if !safeLogicalPath(link) {
			return schemaFailure("dependency symlink chain is invalid")
		}
	}
	return nil
}

func validProgramHeader(value ProgramHeader) bool {
	return safeText(value.Type) && safeText(value.Offset) && safeText(value.VirtualAddress) && safeText(value.PhysicalAddress) && safeText(value.FileSize) && safeText(value.MemorySize) && safeText(value.Flags) && safeText(value.Align)
}

func validSection(value Section) bool {
	return safeText(value.Name) && safeText(value.Type) && safeText(value.Address) && safeText(value.Offset) && safeText(value.Size) && safeText(value.EntrySize) && safeText(value.Flags) && safeText(value.Link) && safeText(value.Info) && safeText(value.Align)
}

func validateGeneratedInput(value GeneratedInputPolicy) error {
	if err := validateCompleteness(value.Completeness); err != nil {
		return err
	}
	if len(value.Records) == 0 {
		return schemaFailure("generated-input records are empty")
	}
	previous := ""
	for _, record := range value.Records {
		if !safeRelative(record.Path) || !validGeneratedKind(record.Kind) || !safeID(record.ProducerKey) || !validGeneratedNormalization(record.Normalization) || !validFileMode(record.ExpectedMode) || len(record.ConsumerKeys) == 0 || len(record.SourcePaths) == 0 {
			return schemaFailure("generated-input record is invalid")
		}
		if previous >= record.Path {
			return &Failure{Code: CodeOrderInvalid, Detail: "generated-input records are not strictly sorted"}
		}
		previous = record.Path
		if !sortedUnique(record.ConsumerKeys) || !sortedUnique(record.SourcePaths) {
			return &Failure{Code: CodeOrderInvalid, Detail: "generated-input references are not sorted"}
		}
		for _, source := range record.SourcePaths {
			if !safeRelative(source) {
				return schemaFailure("generated-input source path is invalid")
			}
		}
	}
	return nil
}

func validateIntermediatePath(value IntermediatePathPolicy) error {
	if err := validateCompleteness(value.Completeness); err != nil {
		return err
	}
	if len(value.Records) == 0 {
		return schemaFailure("intermediate-path records are empty")
	}
	previous := ""
	for _, record := range value.Records {
		if !safeRelative(record.Path) || !validIntermediateClass(record.Class) || !safeID(record.ProducerKey) || !validFileMode(record.ExpectedMode) {
			return schemaFailure("intermediate-path record is invalid")
		}
		if previous >= record.Path {
			return &Failure{Code: CodeOrderInvalid, Detail: "intermediate-path records are not strictly sorted"}
		}
		previous = record.Path
	}
	return nil
}

func validateForkDelta(value ForkDeltaPolicy) error {
	if err := validateCompleteness(value.Completeness); err != nil {
		return err
	}
	if value.Purpose != "deterministic-vdate-input" || !lowerHex(value.PatchDiffSHA256, 64) || value.ForkSourceSetChange != "none" || len(value.ChangedPaths) == 0 {
		return schemaFailure("upstream-fork-delta header is invalid")
	}
	previous := ""
	for _, record := range value.ChangedPaths {
		if !safeRelative(record.Path) || !validChangeStatus(record.Status) || !validGitMode(record.OldMode) || !validGitMode(record.NewMode) || !lowerHex(record.OldBlob, 40) || !lowerHex(record.NewBlob, 40) || !lowerHex(record.OldSHA256, 64) || !lowerHex(record.NewSHA256, 64) {
			return schemaFailure("upstream-fork-delta path record is invalid")
		}
		if previous >= record.Path {
			return &Failure{Code: CodeOrderInvalid, Detail: "upstream-fork-delta records are not strictly sorted"}
		}
		previous = record.Path
	}
	return nil
}

func validateCompleteness(value string) error {
	if value != CompletenessComplete && value != CompletenessObserved && value != CompletenessDirect {
		return schemaFailure("policy completeness is invalid")
	}
	return nil
}

func isKind(value Kind) bool {
	for _, kind := range allKinds {
		if value == kind {
			return true
		}
	}
	return false
}
func validInclusionReason(value string) bool {
	switch value {
	case "build-config", "c-translation-unit", "cpp-translation-unit", "binary-image", "compiler-header", "link-input":
		return true
	}
	return false
}
func validPhase(value string) bool {
	switch value {
	case "compile", "link", "dependency", "generated-input":
		return true
	}
	return false
}
func validIntermediateClass(value string) bool {
	switch value {
	case "dependency", "object", "final-stripped", "final-unstripped":
		return true
	}
	return false
}
func validChangeStatus(value string) bool {
	switch value {
	case "modified", "added", "deleted":
		return true
	}
	return false
}

func validGeneratedKind(value string) bool {
	switch value {
	case "dependency", "compiler-dependency", "register-map", "memory-map", "generated-source":
		return true
	}
	return false
}

func validGeneratedNormalization(value string) bool {
	switch value {
	case "lf-v1", "compiler-dependency-v1", "register-map-v1", "memory-map-v1":
		return true
	}
	return false
}

func validFileMode(value string) bool {
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

func validGitMode(value string) bool {
	switch value {
	case "040000", "100644", "100755", "120000", "160000":
		return true
	}
	return false
}

func safeID(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		if !(r == '-' || r == '_' || r == '.' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) || (i == 0 && !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

func safeRelative(value string) bool {
	if value == "" || path.IsAbs(value) || path.Clean(value) != value || strings.ContainsAny(value, `\\`) || strings.HasPrefix(value, "../") || strings.Contains(value, "/../") || strings.HasPrefix(value, "./") || strings.Contains(value, "//") {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
		for _, r := range part {
			if !(r == '.' || r == '_' || r == '-' || r == '~' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
				return false
			}
		}
	}
	return true
}

func safeLogicalPath(value string) bool {
	for _, root := range []string{"/stage-a0/src", "/stage-a0/build", "/stage-a0/build-utils", "/stage-a0/toolchain", "/stage-a0/sysroot"} {
		if value == root {
			return true
		}
		prefix := root + "/"
		if strings.HasPrefix(value, prefix) {
			return safeRelative(strings.TrimPrefix(value, prefix))
		}
	}
	return false
}

func safeToolPath(value string) bool {
	for _, root := range []string{"/stage-a0/toolchain/bin/", "/stage-a0/build-utils/bin/"} {
		if strings.HasPrefix(value, root) {
			return safeRelative(strings.TrimPrefix(value, root))
		}
	}
	return false
}

func safeRelativeOrLogical(value string) bool { return safeRelative(value) || safeLogicalPath(value) }

func safeArgument(value string) bool {
	if !safeText(value) || strings.ContainsRune(value, '\\') {
		return false
	}
	if strings.HasPrefix(value, "/") {
		return safeLogicalPath(value)
	}
	if strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../") || strings.Contains(value, "/../") || strings.Contains(value, "//") {
		return false
	}
	if strings.Contains(value, "/") {
		for _, prefix := range []string{"-I", "-L", "-isystem", "-include", "-imacros", "-o", "-MF", "-MT", "-MQ", "--sysroot="} {
			if strings.HasPrefix(value, prefix) {
				return safeRelativeOrLogical(strings.TrimPrefix(value, prefix))
			}
		}
		return safeRelative(value)
	}
	for _, r := range value {
		if !(r == '-' || r == '_' || r == '.' || r == '=' || r == '+' || r == ',' || r == ':' || r == '@' || r == '%' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

func safeText(value string) bool {
	if value == "" || !utf8.ValidString(value) || strings.TrimSpace(value) == "" || strings.HasPrefix(value, "\ufeff") {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f || r == '\ufeff' {
			return false
		}
	}
	return true
}

func sortedUnique(values []string) bool {
	if len(values) == 0 {
		return false
	}
	copyValues := append([]string(nil), values...)
	sort.Strings(copyValues)
	for i := range values {
		if values[i] != copyValues[i] || (i > 0 && values[i-1] == values[i]) {
			return false
		}
	}
	return true
}

func lowerHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

func allASCIIDigits(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
func schemaFailure(detail string) error { return &Failure{Code: CodeSchemaInvalid, Detail: detail} }
