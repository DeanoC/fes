package precompare

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/DeanoC/FogCast-POC/internal/stagea0/firstbuild"
)

func TestEncodeDecodeComparisonIsCanonical(t *testing.T) {
	report := validComparison()
	raw, err := EncodeComparison(report)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeComparison(raw)
	if err != nil {
		t.Fatalf("DecodeComparison(valid) = %v", err)
	}
	reencoded, err := EncodeComparison(decoded)
	if err != nil || !bytes.Equal(raw, reencoded) {
		t.Fatalf("decode/re-encode changed canonical bytes: err=%v", err)
	}
	for _, hostile := range [][]byte{
		append([]byte(" "), raw...),
		append(append([]byte(nil), raw...), []byte("{}")...),
		[]byte(`{"format":1,"schema":"fogcast.stage-a0.first-build-precompare.v1","unknown":true}` + "\n"),
	} {
		if _, err := DecodeComparison(hostile); !hasCode(err, CodeReportInvalid) {
			t.Fatalf("DecodeComparison(%q) = %v, want %s", hostile, err, CodeReportInvalid)
		}
	}
}

func TestWriteReportPublishesOnlyToNewDirectory(t *testing.T) {
	parent := t.TempDir()
	output := filepath.Join(parent, "comparison")
	report := validComparison()
	if err := WriteReport(output, report); err != nil {
		t.Fatalf("WriteReport() = %v", err)
	}
	path := filepath.Join(output, comparisonReportName)
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("comparison mode = %04o, want 0600 regular file", info.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeComparison(raw); err != nil {
		t.Fatalf("published report is invalid: %v", err)
	}
	if err := WriteReport(output, report); !hasCode(err, CodeReportInvalid) {
		t.Fatalf("second WriteReport() = %v, want %s", err, CodeReportInvalid)
	}
}

func TestCompareRejectsUnsafeOrMissingInputs(t *testing.T) {
	if _, err := Compare("relative", "/absolute/right"); !hasCode(err, CodeInputInvalid) {
		t.Fatalf("relative left = %v, want %s", err, CodeInputInvalid)
	}
	if _, err := Compare("/absolute/left", "relative"); !hasCode(err, CodeInputInvalid) {
		t.Fatalf("relative right = %v, want %s", err, CodeInputInvalid)
	}
	left := t.TempDir()
	right := t.TempDir()
	if _, err := Compare(left, left); !hasCode(err, CodeInputInvalid) {
		t.Fatalf("same capture root = %v, want %s", err, CodeInputInvalid)
	}
	if _, err := Compare(left, right); !hasCode(err, CodeReceiptInvalid) {
		t.Fatalf("missing receipts = %v, want %s", err, CodeReceiptInvalid)
	}
}

func TestValidateComparisonRejectsStatusAndArtifactDrift(t *testing.T) {
	report := validComparison()
	report.Status = "Reproducible"
	if err := ValidateComparison(report); !hasCode(err, CodeReportInvalid) {
		t.Fatalf("status drift = %v, want %s", err, CodeReportInvalid)
	}
	report = validComparison()
	report.Artifacts[0].SHA256 = "0" + report.Artifacts[0].SHA256[1:]
	if err := ValidateComparison(report); !hasCode(err, CodeReportInvalid) {
		t.Fatalf("artifact drift = %v, want %s", err, CodeReportInvalid)
	}
}

func validComparison() Comparison {
	return Comparison{
		Format:                  FormatV1,
		Schema:                  SchemaV1,
		Status:                  firstbuild.EvidenceStatusSoftwareTested,
		SourceAvailability:      SourceAvailabilityLocal,
		TwoBuildsByteIdentical:  true,
		SourceCommit:            firstbuild.ExpectedMainCommit,
		SourceTree:              firstbuild.ExpectedMainTree,
		ForkParentCommit:        firstbuild.ExpectedMainParent,
		ToolchainArchiveSHA256:  firstbuild.ExpectedToolchainArchiveSHA256,
		ContainerImageID:        firstbuild.ExpectedContainerImageID,
		ContainerManifestDigest: firstbuild.ExpectedContainerManifestDigest,
		ContainerConfigDigest:   firstbuild.ExpectedContainerConfigDigest,
		SourceDateEpoch:         firstbuild.ExpectedSourceDateEpoch,
		VDate:                   firstbuild.ExpectedVDate,
		LeftReportSHA256:        "1111111111111111111111111111111111111111111111111111111111111111",
		RightReportSHA256:       "2222222222222222222222222222222222222222222222222222222222222222",
		LeftBuildLogSHA256:      "3333333333333333333333333333333333333333333333333333333333333333",
		RightBuildLogSHA256:     "4444444444444444444444444444444444444444444444444444444444444444",
		Artifacts: []ArtifactComparison{
			{Path: "bin/MiSTer", Size: firstbuild.ExpectedMiSTerSize, SHA256: firstbuild.ExpectedMiSTerSHA256},
			{Path: "bin/MiSTer.elf", Size: firstbuild.ExpectedMiSTerELFSize, SHA256: firstbuild.ExpectedMiSTerELFSHA256},
		},
	}
}

func hasCode(err error, want Code) bool {
	var failure *Failure
	return errors.As(err, &failure) && failure.Code == want
}
