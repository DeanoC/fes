package promotion

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"reflect"
	"sort"

	"github.com/DeanoC/FogCast-POC/internal/stagea0"
	"github.com/DeanoC/FogCast-POC/internal/stagea0/materialobserve"
	"github.com/DeanoC/FogCast-POC/internal/stagea0/policy"
	"github.com/DeanoC/FogCast-POC/internal/stagea0/precompare"
)

const (
	ReportFormat = 1
	ReportSchema = "fogcast.stage-a0.promotion-report.v1"

	// DecisionBlocked is the only decision emitted by this v1 candidate
	// report. A future promoted-evidence schema must add the eligible path;
	// this report must not accidentally turn candidate evidence into a lock.
	DecisionBlocked = "blocked"

	CheckPass    = "pass"
	CheckBlocked = "blocked"
	CheckFailed  = "failed"
)

// Inputs contains only canonical evidence bytes. Physical paths are owned by
// the caller and deliberately never enter the report.
type Inputs struct {
	Lock       []byte
	Policies   map[string][]byte
	Materials  []byte
	Comparison []byte
}

type Check struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// Report is the fail-closed decision at the boundary between candidate
// observations and a promoted Stage A0 input set. It is an evidence report,
// not an authorization to edit or replace the lock.
type Report struct {
	Format             int      `json:"format"`
	Schema             string   `json:"schema"`
	Decision           string   `json:"decision"`
	EvidenceStatus     string   `json:"evidence_status"`
	SourceAvailability string   `json:"source_availability"`
	LockSHA256         string   `json:"lock_sha256"`
	Checks             []Check  `json:"checks"`
	Blockers           []string `json:"blockers"`
}

// Evaluate consumes the current candidate inputs and returns a deterministic
// decision. Malformed or incomplete inputs become blocked checks instead of
// an evaluator error, so the resulting report is itself reviewable.
func Evaluate(inputs Inputs) Report {
	report := Report{
		Format:             ReportFormat,
		Schema:             ReportSchema,
		Decision:           DecisionBlocked,
		EvidenceStatus:     "Software-tested",
		SourceAvailability: "local-only",
		LockSHA256:         digest(inputs.Lock),
	}

	lock, lockErr := stagea0.ParseMainLock(inputs.Lock)
	if lockErr != nil {
		report.addBlocked("lock-schema", "candidate lock is not a valid final-lock document", "LOCK_SCHEMA_INVALID")
	} else {
		report.addPass("lock-schema", "final-lock schema and cross-field invariants parse")
	}

	documents, policyFiles, policyErr := decodePolicies(inputs.Policies)
	if policyErr != "" {
		report.addBlocked("policy-inventory", policyErr, "POLICY_INVENTORY_INCOMPLETE")
	} else if lockErr != nil {
		report.addBlocked("policy-promotion", "policy set cannot be checked against an invalid lock", "POLICY_PROMOTION_BLOCKED")
	} else if err := Validate(lock, documents, policyFiles); err != nil {
		report.addBlocked("policy-promotion", "policy set remains candidate or has unresolved lock references", "POLICY_PROMOTION_BLOCKED")
	} else {
		report.addPass("policy-promotion", "six complete policies match the lock and cross-document closure")
	}

	var materials materialobserve.Manifest
	materialsValid := false
	if len(inputs.Materials) == 0 {
		report.addBlocked("material-catalog", "material catalog is missing", "MATERIAL_CATALOG_MISSING")
	} else {
		decoded, err := materialobserve.Decode(inputs.Materials)
		if err != nil {
			report.addBlocked("material-catalog", "material catalog is not canonical candidate evidence", "MATERIAL_CATALOG_INVALID")
		} else {
			report.addBlocked("material-catalog", "material catalog is candidate-observed or has unresolved review inputs", "MATERIAL_CATALOG_NOT_PROMOTED")
			materials = decoded
			materialsValid = true
		}
	}
	if materialsValid && lockErr == nil && (!authorityMatchesLock(materials.Authority, lock) || !materialsMatchLock(materials, lock)) {
		report.addBlocked("material-lock-binding", "material evidence does not match the candidate lock", "MATERIAL_LOCK_MISMATCH")
	}

	comparisonValid := false
	if len(inputs.Comparison) == 0 {
		report.addBlocked("two-build-comparison", "two-build comparison report is missing", "PRECOMPARE_MISSING")
	} else {
		comparison, err := precompare.DecodeComparison(inputs.Comparison)
		if err != nil {
			report.addBlocked("two-build-comparison", "two-build comparison report is not canonical", "PRECOMPARE_INVALID")
		} else {
			comparisonValid = true
			report.addPass("two-build-comparison", "retained final artifacts are byte-identical in the preliminary comparison")
			if lockErr == nil && !comparisonMatchesLock(comparison, lock) {
				report.addBlocked("comparison-lock-binding", "comparison input binding does not match the candidate lock", "COMPARISON_LOCK_MISMATCH")
			}
			if comparison.SourceAvailability == "durably-retrievable" {
				report.SourceAvailability = comparison.SourceAvailability
			} else {
				report.addBlocked("source-availability", "comparison inputs are local-only", "SOURCE_NOT_DURABLY_RETRIEVABLE")
			}
		}
	}
	if !comparisonValid && len(inputs.Comparison) != 0 {
		report.addBlocked("source-availability", "source availability cannot be established without a valid comparison", "SOURCE_AVAILABILITY_UNKNOWN")
	}

	sort.Slice(report.Checks, func(i, j int) bool { return report.Checks[i].ID < report.Checks[j].ID })
	sort.Strings(report.Blockers)
	return report
}

func authorityMatchesLock(authority policy.Authority, lock stagea0.MainLock) bool {
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
	return reflect.DeepEqual(authority, want)
}

func comparisonMatchesLock(comparison precompare.Comparison, lock stagea0.MainLock) bool {
	if comparison.SourceCommit != lock.Main.ForkCommit || comparison.SourceTree != lock.Main.ForkTree || comparison.ForkParentCommit != lock.Main.ForkParentCommit || comparison.SourceDateEpoch != lock.SourceDateEpoch || comparison.VDate != lockVDate(lock.SourceDateEpoch) {
		return false
	}
	containerSeen, toolchainSeen := false, false
	for _, material := range lock.Materials {
		if material.ID == lock.Environment.ContainerMaterialID {
			if material.OCI == nil || comparison.ContainerImageID != material.OCI.ManifestDigest {
				return false
			}
			containerSeen = true
		}
		if material.ID == "toolchain" {
			if material.ArchiveHTTPS == nil || comparison.ToolchainArchiveSHA256 != material.ArchiveHTTPS.SHA256 {
				return false
			}
			toolchainSeen = true
		}
	}
	return containerSeen && toolchainSeen
}

func materialsMatchLock(manifest materialobserve.Manifest, lock stagea0.MainLock) bool {
	seen := map[string]bool{"fork": false, "upstream": false, "toolchain": false, "container": false}
	for _, record := range manifest.Records {
		switch record.ID {
		case lock.Main.ForkMaterialID:
			if record.Commit != lock.Main.ForkCommit || record.Tree != lock.Main.ForkTree {
				return false
			}
			seen["fork"] = true
		case lock.Main.UpstreamMaterialID:
			if record.Commit != lock.Main.UpstreamCommit || record.Tree != lock.Main.UpstreamTree {
				return false
			}
			seen["upstream"] = true
		case "toolchain":
			for _, material := range lock.Materials {
				if material.ID == record.ID && material.ArchiveHTTPS != nil {
					if record.SHA256 != material.ArchiveHTTPS.SHA256 || record.Size != material.ArchiveHTTPS.Size {
						return false
					}
					seen["toolchain"] = true
				}
			}
		case "container":
			for _, material := range lock.Materials {
				if material.ID == record.ID && material.OCI != nil {
					if record.ManifestDigest != material.OCI.ManifestDigest || record.ConfigDigest != material.OCI.ConfigDigest {
						return false
					}
					seen["container"] = true
				}
			}
		}
	}
	return seen["fork"] && seen["upstream"] && seen["toolchain"] && seen["container"]
}

func decodePolicies(files map[string][]byte) ([]policy.Document, map[string][]byte, string) {
	want := []string{
		"compile-link.json",
		"elf-dependency.json",
		"generated-input.json",
		"intermediate-path.json",
		"source-set.json",
		"upstream-fork-delta.json",
	}
	if len(files) != len(want) {
		return nil, nil, "policy inventory is not exactly six files"
	}
	documents := make([]policy.Document, 0, len(want))
	canonical := make(map[string][]byte, len(want))
	for _, name := range want {
		raw, ok := files[name]
		if !ok || len(raw) == 0 {
			return nil, nil, "policy inventory is missing a required file"
		}
		document, err := policy.Decode(raw)
		if err != nil {
			return nil, nil, "policy inventory contains non-canonical JSON"
		}
		documents = append(documents, document)
		canonical[name] = raw
	}
	return documents, canonical, ""
}

func (report *Report) addPass(id, detail string) {
	report.Checks = append(report.Checks, Check{ID: id, Status: CheckPass, Detail: detail})
}

func (report *Report) addBlocked(id, detail, blocker string) {
	report.Checks = append(report.Checks, Check{ID: id, Status: CheckBlocked, Detail: detail})
	report.Blockers = append(report.Blockers, blocker)
}

func EncodeReport(report Report) ([]byte, error) {
	if err := ValidateReport(report); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(report)
	if err != nil {
		return nil, &Failure{Detail: "promotion report cannot be encoded"}
	}
	return append(raw, '\n'), nil
}

func DecodeReport(raw []byte) (Report, error) {
	var report Report
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&report); err != nil {
		return Report{}, &Failure{Detail: "promotion report JSON cannot be decoded"}
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Report{}, &Failure{Detail: "promotion report JSON has trailing data"}
	}
	canonical, err := EncodeReport(report)
	if err != nil || !bytes.Equal(canonical, raw) {
		return Report{}, &Failure{Detail: "promotion report JSON is not canonical"}
	}
	return report, nil
}

func ValidateReport(report Report) error {
	if report.Format != ReportFormat || report.Schema != ReportSchema || report.Decision != DecisionBlocked || report.EvidenceStatus != "Software-tested" || report.SourceAvailability != "local-only" || !lowerHex(report.LockSHA256) || len(report.Checks) == 0 {
		return &Failure{Detail: "promotion report envelope is invalid"}
	}
	knownChecks := map[string]struct{}{
		"comparison-lock-binding": {}, "lock-schema": {}, "material-catalog": {},
		"material-lock-binding": {}, "policy-inventory": {}, "policy-promotion": {},
		"source-availability": {}, "two-build-comparison": {},
	}
	seenChecks := make(map[string]struct{}, len(report.Checks))
	hasFailure := false
	previous := ""
	for _, check := range report.Checks {
		if check.ID == "" || check.ID <= previous || check.Status != CheckPass && check.Status != CheckBlocked && check.Status != CheckFailed || check.Detail == "" {
			return &Failure{Detail: "promotion report checks are not canonical"}
		}
		if _, ok := knownChecks[check.ID]; !ok {
			return &Failure{Detail: "promotion report check ID is unknown"}
		}
		if check.Status == CheckBlocked || check.Status == CheckFailed {
			hasFailure = true
		}
		if _, ok := seenChecks[check.ID]; ok {
			return &Failure{Detail: "promotion report checks are duplicated"}
		}
		seenChecks[check.ID] = struct{}{}
		previous = check.ID
	}
	previous = ""
	for _, blocker := range report.Blockers {
		if blocker == "" || blocker <= previous {
			return &Failure{Detail: "promotion report blockers are not sorted"}
		}
		previous = blocker
	}
	if !hasFailure || len(report.Blockers) == 0 {
		return &Failure{Detail: "blocked promotion report has no blockers"}
	}
	return nil
}

func digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func lowerHex(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}
