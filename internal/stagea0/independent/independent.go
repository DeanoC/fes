// Package independent runs the bounded Stage A0 independent-build gate.
//
// The gate deliberately remains a Software-tested/local-only observation. It
// consumes a valid final-lock document, takes two fresh first-build captures
// in distinct output roots, and compares the retained payloads. It does not
// promote the lock, publish artifacts, or make a HIL/reproducibility claim.
package independent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/DeanoC/FogCast-POC/internal/stagea0"
	"github.com/DeanoC/FogCast-POC/internal/stagea0/firstbuild"
	"github.com/DeanoC/FogCast-POC/internal/stagea0/precompare"
)

const (
	FormatV1 = 1
	SchemaV1 = "fogcast.stage-a0.independent-build.v1"

	StatusSoftwareTested    = firstbuild.EvidenceStatusSoftwareTested
	SourceAvailabilityLocal = precompare.SourceAvailabilityLocal
	FreshCaptureCountV1     = 2
)

var expectedBuildEntrypoint = []string{
	"/stage-a0/build-utils/bin/bash",
	"-lc",
	"make clean VDATE=260808; make V=1 VDATE=260808",
}

type Code string

const (
	CodeLockInvalid       Code = "INDEPENDENT_LOCK_INVALID"
	CodeLockMismatch      Code = "INDEPENDENT_LOCK_MISMATCH"
	CodeInputInvalid      Code = "INDEPENDENT_INPUT_INVALID"
	CodeRootReuse         Code = "INDEPENDENT_ROOT_REUSE"
	CodeCaptureFailed     Code = "INDEPENDENT_CAPTURE_FAILED"
	CodeComparisonFailed  Code = "INDEPENDENT_COMPARISON_FAILED"
	CodeReportInvalid     Code = "INDEPENDENT_REPORT_INVALID"
	CodeReportReuse       Code = "INDEPENDENT_REPORT_REUSE"
	CodeReportWriteFailed Code = "INDEPENDENT_REPORT_WRITE_FAILED"
)

// Failure is a stable, path-free gate error. Operational locations are never
// copied into its Detail string, so command output remains safe to retain.
type Failure struct {
	Code   Code
	Detail string
}

func (f *Failure) Error() string { return string(f.Code) + ": " + f.Detail }

// Request contains operational locations and the exact lock bytes consumed by
// a run. Only the lock digest is retained in Report; paths are not evidence.
type Request struct {
	Lock             []byte
	SourceDir        string
	ToolchainArchive string
	LeftOutputDir    string
	RightOutputDir   string
	// ReportPath is optional for library callers. When supplied, Run checks
	// that it cannot alias an input or capture root before doing any build.
	ReportPath string
}

// Report is the deterministic output of two fresh captures and their
// preliminary comparison. The nested comparison keeps the report
// self-contained; no digest is emitted for an unretained sidecar file.
type Report struct {
	Format                 int                   `json:"format"`
	Schema                 string                `json:"schema"`
	Status                 string                `json:"status"`
	SourceAvailability     string                `json:"source_availability"`
	FreshCaptures          int                   `json:"fresh_captures"`
	DistinctCaptureRoots   bool                  `json:"distinct_capture_roots"`
	TwoBuildsByteIdentical bool                  `json:"two_builds_byte_identical"`
	LockSHA256             string                `json:"lock_sha256"`
	Comparison             precompare.Comparison `json:"comparison"`
}

// Run validates the final lock and operational roots, performs two fresh
// firstbuild.Capture calls, and compares their retained outputs. A failed
// second capture leaves the first capture available for diagnosis, but no
// Report is returned and no report file is written by this package.
func Run(ctx context.Context, request Request) (Report, error) {
	if len(request.Lock) == 0 {
		return Report{}, &Failure{Code: CodeLockInvalid, Detail: "lock bytes are required"}
	}
	// Own the exact bytes for the duration of the run so a caller cannot
	// mutate the lock while the two builds are executing.
	request.Lock = append([]byte(nil), request.Lock...)
	lock, err := stagea0.ParseMainLock(request.Lock)
	if err != nil {
		return Report{}, &Failure{Code: CodeLockInvalid, Detail: "lock does not satisfy the final-lock schema"}
	}
	if err := ValidateLock(lock); err != nil {
		return Report{}, err
	}
	if err := validateRequest(request); err != nil {
		return Report{}, err
	}

	leftRequest := firstbuild.DefaultRequest()
	leftRequest.SourceDir = request.SourceDir
	leftRequest.ToolchainArchive = request.ToolchainArchive
	leftRequest.OutputDir = request.LeftOutputDir
	if _, err := firstbuild.Capture(ctx, leftRequest); err != nil {
		return Report{}, &Failure{Code: CodeCaptureFailed, Detail: "left fresh capture failed"}
	}

	rightRequest := leftRequest
	rightRequest.OutputDir = request.RightOutputDir
	if _, err := firstbuild.Capture(ctx, rightRequest); err != nil {
		return Report{}, &Failure{Code: CodeCaptureFailed, Detail: "right fresh capture failed"}
	}

	comparison, err := precompare.Compare(request.LeftOutputDir, request.RightOutputDir)
	if err != nil {
		return Report{}, &Failure{Code: CodeComparisonFailed, Detail: "fresh capture comparison failed"}
	}
	report := Report{
		Format:                 FormatV1,
		Schema:                 SchemaV1,
		Status:                 StatusSoftwareTested,
		SourceAvailability:     SourceAvailabilityLocal,
		FreshCaptures:          FreshCaptureCountV1,
		DistinctCaptureRoots:   true,
		TwoBuildsByteIdentical: comparison.TwoBuildsByteIdentical,
		LockSHA256:             digest(request.Lock),
		Comparison:             comparison,
	}
	if err := ValidateReport(report); err != nil {
		return Report{}, err
	}
	return report, nil
}

// ValidateLock checks the subset of final-lock identity consumed by the
// current first-build adapter. ParseMainLock has already checked the complete
// schema and cross-field closure; these checks bind this runner to the
// reviewed Stage A0 baseline rather than silently using a different lock.
func ValidateLock(lock stagea0.MainLock) error {
	if lock.SourceDateEpoch != firstbuild.ExpectedSourceDateEpoch ||
		lock.Main.ForkCommit != firstbuild.ExpectedMainCommit ||
		lock.Main.ForkTree != firstbuild.ExpectedMainTree ||
		lock.Main.ForkParentCommit != firstbuild.ExpectedMainParent {
		return &Failure{Code: CodeLockMismatch, Detail: "lock source identity differs from the reviewed baseline"}
	}
	env := lock.Environment
	if env.JobCount != firstbuild.ExpectedJobCount || env.Network != "disabled-during-build" || !sameStrings(env.PathPolicy, []string{"/stage-a0/build-utils/bin", "/stage-a0/toolchain/bin"}) {
		return &Failure{Code: CodeLockMismatch, Detail: "lock build environment differs from the reviewed baseline"}
	}
	build := lock.Build
	if build.WorkingDirectory != "/stage-a0/src" || build.VDateFormat != "YYMMDD" || build.VDateExpression != "%y%m%d" || !sameStrings(build.AllowedFinalArtifacts, []string{"bin/MiSTer", "bin/MiSTer.elf"}) || !sameStrings(build.Entrypoint, expectedBuildEntrypoint) {
		return &Failure{Code: CodeLockMismatch, Detail: "lock build contract differs from the reviewed baseline"}
	}

	container, ok := materialByID(lock.Materials, env.ContainerMaterialID)
	if !ok || container.OCI == nil || container.OCI.ManifestDigest != firstbuild.ExpectedContainerImageID || container.OCI.OS != firstbuild.ExpectedContainerOS || container.OCI.Architecture != firstbuild.ExpectedContainerArchitecture {
		return &Failure{Code: CodeLockMismatch, Detail: "lock container material differs from the reviewed baseline"}
	}
	archiveCount := 0
	toolchainArchiveIDs := make(map[string]bool)
	for _, material := range lock.Materials {
		if material.ArchiveHTTPS == nil || material.ArchiveHTTPS.SHA256 != firstbuild.ExpectedToolchainArchiveSHA256 {
			continue
		}
		archiveCount++
		if material.Role != "consumed-build-input" {
			return &Failure{Code: CodeLockMismatch, Detail: "matching toolchain archive has an invalid material role"}
		}
		if material.ArchiveHTTPS.Size != firstbuild.ExpectedToolchainArchiveSize {
			return &Failure{Code: CodeLockMismatch, Detail: "lock toolchain archive size differs from the reviewed baseline"}
		}
		toolchainArchiveIDs[material.ID] = true
	}
	if archiveCount != 1 || !toolchainArchiveReferenced(lock.Toolchains, toolchainArchiveIDs) {
		return &Failure{Code: CodeLockMismatch, Detail: "lock does not identify exactly one reviewed toolchain archive"}
	}
	return nil
}

func toolchainArchiveReferenced(toolchains []stagea0.Toolchain, archiveIDs map[string]bool) bool {
	for _, toolchain := range toolchains {
		for _, component := range toolchain.Components {
			if archiveIDs[component.MaterialID] {
				return true
			}
		}
	}
	return false
}

func validateRequest(request Request) error {
	if err := validateExistingDirectory(request.SourceDir); err != nil {
		return err
	}
	if err := validateExistingRegularFile(request.ToolchainArchive); err != nil {
		return err
	}
	if err := validateFreshRoot(request.LeftOutputDir); err != nil {
		return err
	}
	if err := validateFreshRoot(request.RightOutputDir); err != nil {
		return err
	}
	if request.LeftOutputDir == request.RightOutputDir || pathsOverlap(request.LeftOutputDir, request.RightOutputDir) {
		return &Failure{Code: CodeRootReuse, Detail: "capture roots must be distinct and non-nested"}
	}
	if pathsOverlap(request.SourceDir, request.LeftOutputDir) || pathsOverlap(request.SourceDir, request.RightOutputDir) {
		return &Failure{Code: CodeInputInvalid, Detail: "capture roots cannot overlap the source root"}
	}
	if pathsOverlap(request.ToolchainArchive, request.LeftOutputDir) || pathsOverlap(request.ToolchainArchive, request.RightOutputDir) {
		return &Failure{Code: CodeInputInvalid, Detail: "capture roots cannot overlap the toolchain input"}
	}
	if request.ReportPath != "" {
		if err := validateCleanAbsolute(request.ReportPath); err != nil {
			return err
		}
		if err := validateNoSymlinkAncestors(request.ReportPath); err != nil {
			return err
		}
		if _, err := os.Lstat(request.ReportPath); err == nil {
			return &Failure{Code: CodeReportReuse, Detail: "independent report already exists"}
		} else if !errors.Is(err, os.ErrNotExist) {
			return &Failure{Code: CodeInputInvalid, Detail: "independent report cannot be inspected"}
		}
		if pathsOverlap(request.SourceDir, request.ReportPath) || pathsOverlap(request.LeftOutputDir, request.ReportPath) || pathsOverlap(request.RightOutputDir, request.ReportPath) || pathsOverlap(request.ToolchainArchive, request.ReportPath) {
			return &Failure{Code: CodeInputInvalid, Detail: "independent report cannot overlap an input or capture root"}
		}
	}
	return nil
}

func validateExistingDirectory(directory string) error {
	if err := validateCleanAbsolute(directory); err != nil {
		return err
	}
	if err := validateNoSymlinkAncestors(directory); err != nil {
		return err
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return &Failure{Code: CodeInputInvalid, Detail: "source directory is unavailable or unsafe"}
	}
	return nil
}

func validateExistingRegularFile(filename string) error {
	if err := validateCleanAbsolute(filename); err != nil {
		return err
	}
	if err := validateNoSymlinkAncestors(filename); err != nil {
		return err
	}
	info, err := os.Lstat(filename)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return &Failure{Code: CodeInputInvalid, Detail: "toolchain archive is unavailable or unsafe"}
	}
	return nil
}

func validateFreshRoot(directory string) error {
	if err := validateCleanAbsolute(directory); err != nil {
		return err
	}
	if err := validateNoSymlinkAncestors(directory); err != nil {
		return err
	}
	if _, err := os.Lstat(directory); err == nil {
		return &Failure{Code: CodeRootReuse, Detail: "capture root already exists"}
	} else if !errors.Is(err, os.ErrNotExist) {
		return &Failure{Code: CodeInputInvalid, Detail: "capture root cannot be inspected"}
	}
	return nil
}

func validateCleanAbsolute(path string) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return &Failure{Code: CodeInputInvalid, Detail: "operational locations must be clean absolute paths"}
	}
	return nil
}

// validateNoSymlinkAncestors checks every existing parent. System paths such
// as macOS's /var -> /private/var are allowed when their resolved target is a
// directory; symlink leaves are still rejected by the caller that owns them.
func validateNoSymlinkAncestors(path string) error {
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				resolved, resolveErr := filepath.EvalSymlinks(current)
				resolvedInfo, statErr := os.Stat(resolved)
				if resolveErr != nil || statErr != nil || !resolvedInfo.IsDir() {
					return &Failure{Code: CodeInputInvalid, Detail: "operational path contains an unsafe symlink"}
				}
				info = resolvedInfo
			}
			if current != path && !info.IsDir() {
				return &Failure{Code: CodeInputInvalid, Detail: "operational path has a non-directory parent"}
			}
			if current == "/" {
				return nil
			}
			continue
		}
		if !errors.Is(err, os.ErrNotExist) {
			return &Failure{Code: CodeInputInvalid, Detail: "operational path cannot be inspected"}
		}
		next := filepath.Dir(current)
		if next == current {
			return &Failure{Code: CodeInputInvalid, Detail: "operational path has no safe parent"}
		}
		if current == "/" {
			return nil
		}
	}
}

func pathsOverlap(left, right string) bool {
	left = canonicalPath(left)
	right = canonicalPath(right)
	if left == right {
		return true
	}
	return pathContains(left, right) || pathContains(right, left)
}

// canonicalPath resolves existing parents and appends any missing suffix.
// It lets lexical overlap checks catch an output path expressed through a
// system symlink (for example /var on macOS) without requiring output roots
// to exist before the gate starts.
func canonicalPath(path string) string {
	if path == "" {
		return path
	}
	current := path
	var suffix []string
	for {
		if _, err := os.Lstat(current); err == nil {
			resolved, resolveErr := filepath.EvalSymlinks(current)
			if resolveErr != nil {
				return filepath.Clean(path)
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return filepath.Clean(resolved)
		} else if !errors.Is(err, os.ErrNotExist) {
			return filepath.Clean(path)
		}
		next := filepath.Dir(current)
		if next == current {
			return filepath.Clean(path)
		}
		suffix = append(suffix, filepath.Base(current))
		current = next
	}
}

func pathContains(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil || rel == "." || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func materialByID(materials []stagea0.Material, id string) (stagea0.Material, bool) {
	for _, material := range materials {
		if material.ID == id {
			return material, true
		}
	}
	return stagea0.Material{}, false
}

func sameStrings(left, right []string) bool {
	return reflect.DeepEqual(left, right)
}

// ValidateReport checks the stable, non-promoting report contract.
func ValidateReport(report Report) error {
	if report.Format != FormatV1 || report.Schema != SchemaV1 || report.Status != StatusSoftwareTested || report.SourceAvailability != SourceAvailabilityLocal || report.FreshCaptures != FreshCaptureCountV1 || !report.DistinctCaptureRoots || !report.TwoBuildsByteIdentical || !isLowerHexDigest(report.LockSHA256) {
		return &Failure{Code: CodeReportInvalid, Detail: "independent report envelope is invalid"}
	}
	if err := precompare.ValidateComparison(report.Comparison); err != nil {
		return &Failure{Code: CodeReportInvalid, Detail: "independent report comparison is invalid"}
	}
	if report.Comparison.Status != report.Status || report.Comparison.SourceAvailability != report.SourceAvailability || report.Comparison.TwoBuildsByteIdentical != report.TwoBuildsByteIdentical {
		return &Failure{Code: CodeReportInvalid, Detail: "independent report comparison status is inconsistent"}
	}
	return nil
}

// EncodeReport emits canonical JSON with no operational paths or timestamps.
func EncodeReport(report Report) ([]byte, error) {
	if err := ValidateReport(report); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(report)
	if err != nil {
		return nil, &Failure{Code: CodeReportInvalid, Detail: "independent report cannot be encoded"}
	}
	return append(raw, '\n'), nil
}

// DecodeReport accepts only canonical JSON emitted by EncodeReport.
func DecodeReport(raw []byte) (Report, error) {
	var report Report
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&report); err != nil {
		return Report{}, &Failure{Code: CodeReportInvalid, Detail: "independent report JSON cannot be decoded"}
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Report{}, &Failure{Code: CodeReportInvalid, Detail: "independent report JSON has trailing data"}
	}
	canonical, err := EncodeReport(report)
	if err != nil || !bytes.Equal(canonical, raw) {
		return Report{}, &Failure{Code: CodeReportInvalid, Detail: "independent report JSON is not canonical"}
	}
	return report, nil
}

// WriteReport atomically publishes one new report file and never overwrites
// an existing path. The parent is created with private permissions.
func WriteReport(filename string, report Report) error {
	if err := ValidateReport(report); err != nil {
		return err
	}
	if err := validateCleanAbsolute(filename); err != nil {
		return err
	}
	if err := validateNoSymlinkAncestors(filename); err != nil {
		return err
	}
	if _, err := os.Lstat(filename); err == nil {
		return &Failure{Code: CodeReportReuse, Detail: "independent report already exists"}
	} else if !errors.Is(err, os.ErrNotExist) {
		return &Failure{Code: CodeReportWriteFailed, Detail: "independent report cannot be inspected"}
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return &Failure{Code: CodeReportWriteFailed, Detail: "independent report parent cannot be created"}
	}
	if err := validateNoSymlinkAncestors(filename); err != nil {
		return err
	}
	raw, err := EncodeReport(report)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(filename), ".stage-a0-independent-*")
	if err != nil {
		return &Failure{Code: CodeReportWriteFailed, Detail: "independent report staging file cannot be created"}
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return &Failure{Code: CodeReportWriteFailed, Detail: "independent report permissions cannot be set"}
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return &Failure{Code: CodeReportWriteFailed, Detail: "independent report cannot be written"}
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return &Failure{Code: CodeReportWriteFailed, Detail: "independent report cannot be synced"}
	}
	if err := tmp.Close(); err != nil {
		return &Failure{Code: CodeReportWriteFailed, Detail: "independent report staging file cannot be closed"}
	}
	// Link is atomic and fails with EEXIST instead of replacing a report that
	// appeared after the initial Lstat check.
	if err := os.Link(tmpName, filename); err != nil {
		if errors.Is(err, os.ErrExist) {
			return &Failure{Code: CodeReportReuse, Detail: "independent report already exists"}
		}
		return &Failure{Code: CodeReportWriteFailed, Detail: "independent report cannot be published"}
	}
	return nil
}

func digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func isLowerHexDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
