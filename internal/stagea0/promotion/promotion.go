// Package promotion contains the authorization boundary between observed
// Stage A0 policy candidates and a policy set that may be referenced by a
// final lock.  The policy package intentionally validates syntax and local
// invariants; this package validates the cross-document closure and lock
// references that make promotion meaningful.
package promotion

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"time"

	"github.com/DeanoC/FogCast-POC/internal/stagea0"
	"github.com/DeanoC/FogCast-POC/internal/stagea0/policy"
)

type Code string

const CodeInvalid Code = "POLICY_PROMOTION_INVALID"

type Failure struct{ Detail string }

func (f *Failure) Error() string { return string(CodeInvalid) + ": " + f.Detail }

// Validate proves that exactly one complete policy of every V1 kind is
// present, that every policy is the byte sequence named by the lock, and that
// the final-artifact/dependency/source/fork closure is complete.  It does not
// inspect a filesystem: callers must supply the already fetched policy bytes.
func Validate(lock stagea0.MainLock, documents []policy.Document, files map[string][]byte) error {
	if err := stagea0.ValidateMainLock(lock); err != nil {
		return invalid("main lock is not valid: " + err.Error())
	}
	if len(documents) != len(policy.Kinds()) {
		return invalid("exactly six policy documents are required")
	}
	byKind := make(map[policy.Kind]policy.Document, len(documents))
	for _, document := range documents {
		if err := policy.Validate(document); err != nil {
			return invalid("policy is invalid: " + err.Error())
		}
		if policyCompleteness(document) != policy.CompletenessComplete {
			return invalid("candidate policy cannot be promoted")
		}
		if _, exists := byKind[document.Kind]; exists {
			return invalid("policy kind is duplicated")
		}
		byKind[document.Kind] = document
	}
	for _, kind := range policy.Kinds() {
		if _, ok := byKind[kind]; !ok {
			return invalid("policy kind is missing: " + string(kind))
		}
	}
	if err := validateAuthorities(lock, byKind); err != nil {
		return err
	}
	if err := validatePolicyFiles(lock, byKind, files); err != nil {
		return err
	}
	materials := consumedMaterials(lock)
	if err := validateSourceSet(byKind[policy.KindSourceSet], materials); err != nil {
		return err
	}
	if err := validateCompileLink(byKind[policy.KindCompileLink], lock); err != nil {
		return err
	}
	if err := validateELFClosure(byKind[policy.KindELFDependency], lock, materials); err != nil {
		return err
	}
	if err := validateIntermediate(byKind[policy.KindIntermediatePath], lock); err != nil {
		return err
	}
	if err := validateForkDelta(byKind[policy.KindForkDelta]); err != nil {
		return err
	}
	return nil
}

func validateAuthorities(lock stagea0.MainLock, documents map[policy.Kind]policy.Document) error {
	want := policy.Authority{
		UpstreamCommit:   lock.Main.UpstreamCommit,
		UpstreamTree:     lock.Main.UpstreamTree,
		ForkCommit:       lock.Main.ForkCommit,
		ForkTree:         lock.Main.ForkTree,
		ForkParentCommit: lock.Main.ForkParentCommit,
		PatchCommits:     append([]string(nil), lock.Main.PatchCommits...),
		SourceDateEpoch:  lock.SourceDateEpoch,
		VDate:            lockVDate(lock.SourceDateEpoch),
	}
	for _, document := range documents {
		if !reflect.DeepEqual(document.Authority, want) {
			return invalid("policy authority does not match the main lock")
		}
	}
	return nil
}

func validatePolicyFiles(lock stagea0.MainLock, documents map[policy.Kind]policy.Document, files map[string][]byte) error {
	if len(lock.Policies) != len(policy.Kinds()) || len(files) != len(lock.Policies) {
		return invalid("policy file inventory is not exactly six entries")
	}
	byKind := make(map[policy.Kind]stagea0.Policy, len(lock.Policies))
	paths := make(map[string]struct{}, len(lock.Policies))
	for _, record := range lock.Policies {
		kind := policy.Kind(record.Kind)
		if _, ok := documents[kind]; !ok {
			return invalid("lock policy kind is not present in the document set")
		}
		if _, ok := byKind[kind]; ok {
			return invalid("lock policy kind is duplicated")
		}
		if _, ok := paths[record.Path]; ok {
			return invalid("lock policy path is duplicated")
		}
		paths[record.Path] = struct{}{}
		raw, ok := files[record.Path]
		if !ok || sha256Hex(raw) != record.SHA256 {
			return invalid("policy bytes do not match the lock hash")
		}
		decoded, err := policy.Decode(raw)
		if err != nil || decoded.Kind != kind || !reflect.DeepEqual(decoded, documents[kind]) {
			return invalid("policy bytes do not match the supplied canonical document")
		}
		material, ok := materialByID(lock.Materials, record.MaterialID)
		if !ok || material.MaterialFile == nil || material.MaterialFile.Path != record.Path || material.MaterialFile.Size != int64(len(raw)) || material.MaterialFile.SHA256 != record.SHA256 {
			return invalid("policy material-file reference is unresolved")
		}
		byKind[kind] = record
	}
	for kind := range documents {
		if _, ok := byKind[kind]; !ok {
			return invalid("lock policy inventory is missing a document kind")
		}
	}
	return nil
}

func validateSourceSet(document policy.Document, materials map[string]struct{}) error {
	value := document.SourceSet
	if value == nil || value.ForkSourceSetChange != "none" || value.Makefile.Path != "Makefile" {
		return invalid("source-set closure is invalid")
	}
	for _, record := range append([]policy.SourceRecord{value.Makefile}, value.Records...) {
		if _, ok := materials[record.MaterialID]; !ok {
			return invalid("source-set material is unresolved")
		}
	}
	return nil
}

func validateCompileLink(document policy.Document, lock stagea0.MainLock) error {
	if document.CompileLink == nil || len(document.CompileLink.Records) == 0 {
		return invalid("compile-link closure is empty")
	}
	tools := make(map[string]struct{})
	for _, toolchain := range lock.Toolchains {
		for _, component := range toolchain.Components {
			tools[component.LogicalPath] = struct{}{}
		}
	}
	for _, utility := range lock.BuildUtilities {
		tools[utility.LogicalPath] = struct{}{}
	}
	for _, record := range document.CompileLink.Records {
		if record.CWD != lock.Build.WorkingDirectory {
			return invalid("compile-link working directory differs from the lock")
		}
		if _, ok := tools[record.ToolLogicalPath]; !ok {
			return invalid("compile-link tool is not declared by the lock")
		}
	}
	return nil
}

func validateELFClosure(document policy.Document, lock stagea0.MainLock, materials map[string]struct{}) error {
	value := document.ELFDependency
	if value == nil || len(value.ELFs) != len(lock.Build.AllowedFinalArtifacts) {
		return invalid("ELF closure does not contain exactly the locked final artifacts")
	}
	allowed := make(map[string]struct{}, len(lock.Build.AllowedFinalArtifacts))
	for _, path := range lock.Build.AllowedFinalArtifacts {
		allowed[path] = struct{}{}
	}
	needed := make(map[string]struct{})
	seenELFs := make(map[string]struct{}, len(value.ELFs))
	for _, elf := range value.ELFs {
		if _, ok := allowed[elf.Path]; !ok {
			return invalid("ELF path is not a locked final artifact")
		}
		if _, exists := seenELFs[elf.Path]; exists {
			return invalid("ELF final-artifact path is duplicated")
		}
		seenELFs[elf.Path] = struct{}{}
		wantRole := "final-unstripped"
		if elf.Path == "bin/MiSTer" {
			wantRole = "final-stripped"
		}
		if elf.Role != wantRole {
			return invalid("ELF final-artifact role is invalid")
		}
		for _, name := range elf.Needed {
			needed[name] = struct{}{}
		}
	}
	if len(seenELFs) != len(allowed) {
		return invalid("a locked final artifact is missing from ELF closure")
	}
	dependencies := make(map[string]policy.DependencyRecord, len(value.Dependencies))
	for _, dependency := range value.Dependencies {
		if _, ok := materials[dependency.MaterialID]; !ok {
			return invalid("ELF dependency material is unresolved")
		}
		if _, ok := dependencies[dependency.SONAME]; ok {
			return invalid("ELF dependency SONAME is duplicated")
		}
		dependencies[dependency.SONAME] = dependency
	}
	for name := range needed {
		if _, ok := dependencies[name]; !ok {
			return invalid("ELF DT_NEEDED entry is unresolved")
		}
	}
	for _, elf := range value.ELFs {
		if !dependencyContainsPath(value.Dependencies, elf.InterpreterLogicalPath) {
			return invalid("ELF interpreter is outside the dependency closure")
		}
	}
	return nil
}

func validateIntermediate(document policy.Document, lock stagea0.MainLock) error {
	value := document.IntermediatePath
	if value == nil || len(value.Records) != len(lock.Build.AllowedFinalArtifacts) {
		return invalid("intermediate-path closure does not contain exactly the locked finals")
	}
	byPath := make(map[string]policy.IntermediateRecord, len(value.Records))
	for _, record := range value.Records {
		if _, exists := byPath[record.Path]; exists {
			return invalid("intermediate path is duplicated")
		}
		byPath[record.Path] = record
	}
	if byPath["bin/MiSTer"].Class != "final-stripped" || byPath["bin/MiSTer.elf"].Class != "final-unstripped" {
		return invalid("intermediate final-artifact classes are invalid")
	}
	for _, path := range lock.Build.AllowedFinalArtifacts {
		if _, ok := byPath[path]; !ok {
			return invalid("locked final artifact is missing from intermediate paths")
		}
	}
	return nil
}

func validateForkDelta(document policy.Document) error {
	value := document.ForkDelta
	if value == nil || value.Purpose != "deterministic-vdate-input" || value.ForkSourceSetChange != "none" || len(value.ChangedPaths) != 1 {
		return invalid("fork delta is not the single deterministic VDATE patch")
	}
	change := value.ChangedPaths[0]
	if change.Path != "Makefile" || change.Status != "modified" || change.OldMode != "100644" || change.NewMode != "100644" {
		return invalid("fork delta changes more than Makefile")
	}
	return nil
}

func policyCompleteness(document policy.Document) string {
	switch document.Kind {
	case policy.KindSourceSet:
		return document.SourceSet.Completeness
	case policy.KindCompileLink:
		return document.CompileLink.Completeness
	case policy.KindELFDependency:
		return document.ELFDependency.Completeness
	case policy.KindGeneratedInput:
		return document.GeneratedInput.Completeness
	case policy.KindIntermediatePath:
		return document.IntermediatePath.Completeness
	case policy.KindForkDelta:
		return document.ForkDelta.Completeness
	}
	return ""
}

func consumedMaterials(lock stagea0.MainLock) map[string]struct{} {
	result := make(map[string]struct{})
	for _, material := range lock.Materials {
		if material.Role == "consumed-build-input" {
			result[material.ID] = struct{}{}
		}
	}
	return result
}

func materialByID(materials []stagea0.Material, id string) (stagea0.Material, bool) {
	for _, material := range materials {
		if material.ID == id {
			return material, true
		}
	}
	return stagea0.Material{}, false
}

func dependencyContainsPath(dependencies []policy.DependencyRecord, logicalPath string) bool {
	for _, dependency := range dependencies {
		if dependency.LogicalPath == logicalPath || dependency.RealLogicalPath == logicalPath {
			return true
		}
		for _, link := range dependency.SymlinkChain {
			if link == logicalPath {
				return true
			}
		}
	}
	return false
}

func sha256Hex(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func lockVDate(epoch int64) string {
	// Keep date derivation in one place in the promotion package; policy's
	// validator applies the same UTC YYMMDD rule to every document.
	return timeFromUnix(epoch).Format("060102")
}

func timeFromUnix(epoch int64) (value time.Time) { return time.Unix(epoch, 0).UTC() }

func invalid(detail string) error { return &Failure{Detail: detail} }
