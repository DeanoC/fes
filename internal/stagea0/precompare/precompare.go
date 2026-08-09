// Package precompare compares two preliminary Stage A0 first-build captures.
//
// This package deliberately stops short of the final reproducibility gate. It
// proves that two captures made from the reviewed local baseline have the same
// locked observations, complete bin inventory, and retained final binary
// bytes. The result remains Software-tested/local-only until the material lock
// and retrieval policy are complete.
package precompare

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"github.com/DeanoC/FogCast-POC/internal/stagea0/firstbuild"
)

const (
	FormatV1                = 1
	SchemaV1                = "fogcast.stage-a0.first-build-precompare.v1"
	SourceAvailabilityLocal = "local-only"

	comparisonReportName = "comparison.json"
)

type Code string

const (
	CodeInputInvalid    Code = "PRECOMPARE_INPUT_INVALID"
	CodeReceiptInvalid  Code = "PRECOMPARE_RECEIPT_INVALID"
	CodeInputMismatch   Code = "PRECOMPARE_INPUT_MISMATCH"
	CodePayloadInvalid  Code = "PRECOMPARE_PAYLOAD_INVALID"
	CodePayloadMismatch Code = "PRECOMPARE_PAYLOAD_MISMATCH"
	CodeReportInvalid   Code = "PRECOMPARE_REPORT_INVALID"
)

type Failure struct {
	Code   Code
	Detail string
}

func (f *Failure) Error() string { return string(f.Code) + ": " + f.Detail }

// Comparison is intentionally deterministic. It contains no capture paths,
// timestamps, host names, or command output.
type Comparison struct {
	Format                 int                  `json:"format"`
	Schema                 string               `json:"schema"`
	Status                 string               `json:"status"`
	SourceAvailability     string               `json:"source_availability"`
	TwoBuildsByteIdentical bool                 `json:"two_builds_byte_identical"`
	LeftReportSHA256       string               `json:"left_report_sha256"`
	RightReportSHA256      string               `json:"right_report_sha256"`
	LeftBuildLogSHA256     string               `json:"left_build_log_sha256"`
	RightBuildLogSHA256    string               `json:"right_build_log_sha256"`
	Artifacts              []ArtifactComparison `json:"artifacts"`
}

type ArtifactComparison struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Compare reads two capture directories and performs the preliminary
// comparison. The directories must be absolute paths and must contain the
// first-build.json receipt plus the two retained final binaries.
func Compare(leftDir, rightDir string) (Comparison, error) {
	if err := validateDirectoryArgument(leftDir); err != nil {
		return Comparison{}, err
	}
	if err := validateDirectoryArgument(rightDir); err != nil {
		return Comparison{}, err
	}
	if leftDir == rightDir {
		return Comparison{}, &Failure{Code: CodeInputInvalid, Detail: "capture roots must be distinct"}
	}
	if same, ok := sameExistingRoot(leftDir, rightDir); ok && same {
		return Comparison{}, &Failure{Code: CodeInputInvalid, Detail: "capture roots must be distinct"}
	}
	left, err := readCapture(leftDir)
	if err != nil {
		return Comparison{}, err
	}
	right, err := readCapture(rightDir)
	if err != nil {
		return Comparison{}, err
	}
	if !sameBuildInputs(left.evidence, right.evidence) {
		return Comparison{}, &Failure{Code: CodeInputMismatch, Detail: "locked build observations differ"}
	}
	if !reflect.DeepEqual(left.evidence.Output.Inventory, right.evidence.Output.Inventory) ||
		!reflect.DeepEqual(left.evidence.Output.Artifacts, right.evidence.Output.Artifacts) {
		return Comparison{}, &Failure{Code: CodePayloadMismatch, Detail: "observed output inventory differs"}
	}

	artifacts := make([]ArtifactComparison, 0, len(left.evidence.Output.Artifacts))
	for _, expected := range left.evidence.Output.Artifacts {
		name, ok := retainedName(expected.Path)
		if !ok {
			return Comparison{}, &Failure{Code: CodePayloadInvalid, Detail: "receipt contains an unsupported retained artifact"}
		}
		leftPayload, err := readRetainedArtifact(leftDir, name, expected)
		if err != nil {
			return Comparison{}, err
		}
		rightPayload, err := readRetainedArtifact(rightDir, name, expected)
		if err != nil {
			return Comparison{}, err
		}
		if !bytes.Equal(leftPayload, rightPayload) {
			return Comparison{}, &Failure{Code: CodePayloadMismatch, Detail: "retained artifact bytes differ"}
		}
		artifacts = append(artifacts, ArtifactComparison{
			Path:   expected.Path,
			Size:   expected.Size,
			SHA256: expected.SHA256,
		})
	}

	comparison := Comparison{
		Format:                 FormatV1,
		Schema:                 SchemaV1,
		Status:                 firstbuild.EvidenceStatusSoftwareTested,
		SourceAvailability:     SourceAvailabilityLocal,
		TwoBuildsByteIdentical: true,
		LeftReportSHA256:       digest(left.raw),
		RightReportSHA256:      digest(right.raw),
		LeftBuildLogSHA256:     digest(left.buildLog),
		RightBuildLogSHA256:    digest(right.buildLog),
		Artifacts:              artifacts,
	}
	if err := ValidateComparison(comparison); err != nil {
		return Comparison{}, err
	}
	return comparison, nil
}

// EncodeComparison emits the canonical comparison report.
func EncodeComparison(comparison Comparison) ([]byte, error) {
	if err := ValidateComparison(comparison); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(comparison)
	if err != nil {
		return nil, &Failure{Code: CodeReportInvalid, Detail: "comparison cannot be encoded"}
	}
	return append(raw, '\n'), nil
}

// DecodeComparison accepts only canonical JSON emitted by EncodeComparison.
func DecodeComparison(raw []byte) (Comparison, error) {
	var comparison Comparison
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&comparison); err != nil {
		return Comparison{}, &Failure{Code: CodeReportInvalid, Detail: "comparison JSON cannot be decoded"}
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Comparison{}, &Failure{Code: CodeReportInvalid, Detail: "comparison JSON has trailing data"}
	}
	canonical, err := EncodeComparison(comparison)
	if err != nil || !bytes.Equal(canonical, raw) {
		return Comparison{}, &Failure{Code: CodeReportInvalid, Detail: "comparison JSON is not canonical"}
	}
	return comparison, nil
}

// ValidateComparison checks the stable preliminary report contract.
func ValidateComparison(comparison Comparison) error {
	if comparison.Format != FormatV1 || comparison.Schema != SchemaV1 || comparison.Status != firstbuild.EvidenceStatusSoftwareTested || comparison.SourceAvailability != SourceAvailabilityLocal || !comparison.TwoBuildsByteIdentical {
		return &Failure{Code: CodeReportInvalid, Detail: "comparison schema or status is invalid"}
	}
	if !isLowerHexDigest(comparison.LeftReportSHA256) || !isLowerHexDigest(comparison.RightReportSHA256) || !isLowerHexDigest(comparison.LeftBuildLogSHA256) || !isLowerHexDigest(comparison.RightBuildLogSHA256) {
		return &Failure{Code: CodeReportInvalid, Detail: "comparison receipt digest is invalid"}
	}
	if len(comparison.Artifacts) != 2 {
		return &Failure{Code: CodeReportInvalid, Detail: "comparison artifact set is incomplete"}
	}
	want := []ArtifactComparison{
		{Path: "bin/MiSTer", Size: firstbuild.ExpectedMiSTerSize, SHA256: firstbuild.ExpectedMiSTerSHA256},
		{Path: "bin/MiSTer.elf", Size: firstbuild.ExpectedMiSTerELFSize, SHA256: firstbuild.ExpectedMiSTerELFSHA256},
	}
	if !reflect.DeepEqual(comparison.Artifacts, want) {
		return &Failure{Code: CodeReportInvalid, Detail: "comparison artifact metadata differs from reviewed baseline"}
	}
	return nil
}

// WriteReport atomically publishes comparison.json in a new output directory.
// Existing directories are never overwritten.
func WriteReport(outputDir string, comparison Comparison) error {
	if err := validateDirectoryArgument(outputDir); err != nil {
		return err
	}
	if err := ValidateComparison(comparison); err != nil {
		return err
	}
	if _, err := os.Lstat(outputDir); err == nil {
		return &Failure{Code: CodeReportInvalid, Detail: "comparison output already exists"}
	} else if !errors.Is(err, os.ErrNotExist) {
		return &Failure{Code: CodeReportInvalid, Detail: "comparison output cannot be inspected"}
	}
	parent := filepath.Dir(outputDir)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return &Failure{Code: CodeReportInvalid, Detail: "comparison output parent cannot be created"}
	}
	staging, err := os.MkdirTemp(parent, ".stage-a0-precompare-*")
	if err != nil {
		return &Failure{Code: CodeReportInvalid, Detail: "temporary comparison output cannot be created"}
	}
	defer os.RemoveAll(staging)
	raw, err := EncodeComparison(comparison)
	if err != nil {
		return err
	}
	reportPath := filepath.Join(staging, comparisonReportName)
	if err := os.WriteFile(reportPath, raw, 0o600); err != nil {
		return &Failure{Code: CodeReportInvalid, Detail: "comparison report cannot be written"}
	}
	if err := os.Rename(staging, outputDir); err != nil {
		return &Failure{Code: CodeReportInvalid, Detail: "comparison output cannot be published atomically"}
	}
	return nil
}

type capture struct {
	evidence firstbuild.Evidence
	raw      []byte
	buildLog []byte
}

func readCapture(directory string) (capture, error) {
	rootInfo, err := os.Lstat(directory)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return capture{}, &Failure{Code: CodeReceiptInvalid, Detail: "capture root is missing or unsafe"}
	}
	buildLogPath := filepath.Join(directory, "build.log")
	buildLogInfo, err := os.Lstat(buildLogPath)
	if err != nil || !buildLogInfo.Mode().IsRegular() || buildLogInfo.Mode().Perm() != 0o600 {
		return capture{}, &Failure{Code: CodeReceiptInvalid, Detail: "build log is missing or unsafe"}
	}
	buildLog, err := os.ReadFile(buildLogPath)
	if err != nil {
		return capture{}, &Failure{Code: CodeReceiptInvalid, Detail: "build log cannot be read"}
	}
	if err := firstbuild.ValidateBuildAdapterLog(buildLog); err != nil {
		return capture{}, &Failure{Code: CodeReceiptInvalid, Detail: "build log is not adapter-bound"}
	}
	reportPath := filepath.Join(directory, "first-build.json")
	info, err := os.Lstat(reportPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return capture{}, &Failure{Code: CodeReceiptInvalid, Detail: "first-build receipt is missing or unsafe"}
	}
	raw, err := os.ReadFile(reportPath)
	if err != nil {
		return capture{}, &Failure{Code: CodeReceiptInvalid, Detail: "first-build receipt cannot be read"}
	}
	evidence, err := firstbuild.DecodeEvidence(raw)
	if err != nil {
		return capture{}, &Failure{Code: CodeReceiptInvalid, Detail: "first-build receipt is not canonical"}
	}
	if err := firstbuild.ValidateCapturedEvidence(evidence); err != nil {
		return capture{}, &Failure{Code: CodeReceiptInvalid, Detail: "first-build receipt is not a reviewed capture"}
	}
	return capture{evidence: evidence, raw: raw, buildLog: buildLog}, nil
}

func sameExistingRoot(leftDir, rightDir string) (bool, bool) {
	leftInfo, leftErr := os.Stat(leftDir)
	rightInfo, rightErr := os.Stat(rightDir)
	if leftErr != nil || rightErr != nil {
		return false, false
	}
	return os.SameFile(leftInfo, rightInfo), true
}

func sameBuildInputs(left, right firstbuild.Evidence) bool {
	return reflect.DeepEqual(left.Source, right.Source) &&
		reflect.DeepEqual(left.Toolchain, right.Toolchain) &&
		reflect.DeepEqual(left.Container, right.Container) &&
		reflect.DeepEqual(left.Build, right.Build)
}

func retainedName(logical string) (string, bool) {
	switch logical {
	case "bin/MiSTer":
		return "MiSTer", true
	case "bin/MiSTer.elf":
		return "MiSTer.elf", true
	default:
		return "", false
	}
}

func readRetainedArtifact(directory, name string, expected firstbuild.ArtifactEvidence) ([]byte, error) {
	filename := filepath.Join(directory, name)
	info, err := os.Lstat(filename)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o755 {
		return nil, &Failure{Code: CodePayloadInvalid, Detail: "retained artifact is missing or unsafe"}
	}
	raw, err := os.ReadFile(filename)
	if err != nil {
		return nil, &Failure{Code: CodePayloadInvalid, Detail: "retained artifact cannot be read"}
	}
	if int64(len(raw)) != expected.Size || digest(raw) != expected.SHA256 {
		return nil, &Failure{Code: CodePayloadInvalid, Detail: "retained artifact differs from its receipt"}
	}
	return raw, nil
}

func validateDirectoryArgument(directory string) error {
	if directory == "" || !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return &Failure{Code: CodeInputInvalid, Detail: "capture and report locations must be clean absolute paths"}
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
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}
