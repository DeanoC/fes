package independent

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/internal/stagea0"
	"github.com/DeanoC/FogCast-POC/internal/stagea0/firstbuild"
	"github.com/DeanoC/FogCast-POC/internal/stagea0/precompare"
)

func TestEncodeDecodeReportIsCanonical(t *testing.T) {
	report := validReport()
	raw, err := EncodeReport(report)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeReport(raw)
	if err != nil {
		t.Fatalf("DecodeReport(valid) = %v", err)
	}
	reencoded, err := EncodeReport(decoded)
	if err != nil || !bytes.Equal(raw, reencoded) {
		t.Fatalf("decode/re-encode changed canonical bytes: err=%v", err)
	}
	for _, hostile := range [][]byte{
		append([]byte(" "), raw...),
		append(append([]byte(nil), raw...), []byte("{}")...),
		[]byte(`{"format":1,"schema":"fogcast.stage-a0.independent-build.v1","unknown":true}` + "\n"),
	} {
		if _, err := DecodeReport(hostile); !hasCode(err, CodeReportInvalid) {
			t.Fatalf("DecodeReport(%q) = %v, want %s", hostile, err, CodeReportInvalid)
		}
	}
}

func TestValidateReportRemainsSoftwareTestedLocalOnly(t *testing.T) {
	report := validReport()
	report.Status = "Reproducible"
	if err := ValidateReport(report); !hasCode(err, CodeReportInvalid) {
		t.Fatalf("status drift = %v, want %s", err, CodeReportInvalid)
	}
	report = validReport()
	report.Comparison.SourceAvailability = "durably-retrievable"
	if err := ValidateReport(report); !hasCode(err, CodeReportInvalid) {
		t.Fatalf("source availability drift = %v, want %s", err, CodeReportInvalid)
	}
	report = validReport()
	report.FreshCaptures = 1
	if err := ValidateReport(report); !hasCode(err, CodeReportInvalid) {
		t.Fatalf("fresh capture count drift = %v, want %s", err, CodeReportInvalid)
	}
}

func TestRunRejectsInvalidLockBeforeTouchingRoots(t *testing.T) {
	base := t.TempDir()
	left := filepath.Join(base, "left")
	right := filepath.Join(base, "right")
	report := filepath.Join(base, "report.json")
	_, err := Run(t.Context(), Request{
		Lock:             []byte("not a lock\n"),
		SourceDir:        filepath.Join(base, "missing-source"),
		ToolchainArchive: filepath.Join(base, "missing-toolchain.tar.xz"),
		LeftOutputDir:    left,
		RightOutputDir:   right,
		ReportPath:       report,
	})
	if !hasCode(err, CodeLockInvalid) {
		t.Fatalf("Run(invalid lock) = %v, want %s", err, CodeLockInvalid)
	}
	for _, name := range []string{left, right, report} {
		if _, statErr := os.Lstat(name); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("invalid lock touched %q: %v", name, statErr)
		}
	}
}

func TestValidateRequestRejectsRootReuseAndNesting(t *testing.T) {
	base := t.TempDir()
	source := filepath.Join(base, "source")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	toolchain := filepath.Join(base, "toolchain.tar.xz")
	if err := os.WriteFile(toolchain, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	left := filepath.Join(base, "left")
	if err := os.Mkdir(left, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := validateRequest(Request{SourceDir: source, ToolchainArchive: toolchain, LeftOutputDir: left, RightOutputDir: filepath.Join(base, "right")}); !hasCode(err, CodeRootReuse) {
		t.Fatalf("existing left root = %v, want %s", err, CodeRootReuse)
	}

	nestedLeft := filepath.Join(base, "nested-left")
	nestedRight := filepath.Join(nestedLeft, "right")
	if err := validateRequest(Request{SourceDir: source, ToolchainArchive: toolchain, LeftOutputDir: nestedLeft, RightOutputDir: nestedRight}); !hasCode(err, CodeRootReuse) {
		t.Fatalf("nested roots = %v, want %s", err, CodeRootReuse)
	}
}

func TestValidateRequestRejectsReportAlias(t *testing.T) {
	base := t.TempDir()
	source := filepath.Join(base, "source")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	toolchain := filepath.Join(base, "toolchain.tar.xz")
	if err := os.WriteFile(toolchain, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	left := filepath.Join(base, "left")
	right := filepath.Join(base, "right")
	if err := validateRequest(Request{SourceDir: source, ToolchainArchive: toolchain, LeftOutputDir: left, RightOutputDir: right, ReportPath: filepath.Join(left, "report.json")}); !hasCode(err, CodeInputInvalid) {
		t.Fatalf("report alias = %v, want %s", err, CodeInputInvalid)
	}
}

func TestWriteReportPublishesOnlyToNewPath(t *testing.T) {
	report := validReport()
	filename := filepath.Join(t.TempDir(), "nested", "independent.json")
	if err := WriteReport(filename, report); err != nil {
		t.Fatalf("WriteReport() = %v", err)
	}
	info, err := os.Lstat(filename)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("report mode = %04o, want 0600 regular file", info.Mode().Perm())
	}
	raw, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeReport(raw); err != nil {
		t.Fatalf("published report is invalid: %v", err)
	}
	if err := WriteReport(filename, report); !hasCode(err, CodeReportReuse) {
		t.Fatalf("second WriteReport() = %v, want %s", err, CodeReportReuse)
	}
}

func TestValidateLockRejectsUnboundLock(t *testing.T) {
	if err := ValidateLock(stagea0MainLockFixture()); !hasCode(err, CodeLockMismatch) {
		t.Fatalf("ValidateLock(unbound) = %v, want %s", err, CodeLockMismatch)
	}
}

func TestValidateLockBindsFullBuildEntrypoint(t *testing.T) {
	lock := reviewedLockFixture()
	lock.Build.Entrypoint = append([]string(nil), expectedBuildEntrypoint...)
	lock.Build.Entrypoint[2] = "make clean VDATE=260808; make V=1 VDATE=260808; make extra"
	if err := ValidateLock(lock); !hasCode(err, CodeLockMismatch) {
		t.Fatalf("ValidateLock(mutated entrypoint) = %v, want %s", err, CodeLockMismatch)
	}
}

func TestValidateLockRejectsDuplicateToolchainArchiveRole(t *testing.T) {
	lock := reviewedLockFixture()
	lock.Materials = append(lock.Materials, stagea0.Material{
		ID:   "toolchain-context",
		Role: "context-only",
		ArchiveHTTPS: &stagea0.ArchiveHTTPSMaterial{
			Size:   firstbuild.ExpectedToolchainArchiveSize,
			SHA256: firstbuild.ExpectedToolchainArchiveSHA256,
		},
	})
	if err := ValidateLock(lock); !hasCode(err, CodeLockMismatch) {
		t.Fatalf("ValidateLock(duplicate archive role) = %v, want %s", err, CodeLockMismatch)
	}
}

func TestValidateLockAcceptsReviewedBindingShape(t *testing.T) {
	if err := ValidateLock(reviewedLockFixture()); err != nil {
		t.Fatalf("ValidateLock(reviewed shape) = %v", err)
	}
}

func reviewedLockFixture() stagea0.MainLock {
	return stagea0.MainLock{
		SourceDateEpoch: firstbuild.ExpectedSourceDateEpoch,
		Environment: stagea0.MainLockEnvironment{
			JobCount:            firstbuild.ExpectedJobCount,
			Network:             "disabled-during-build",
			PathPolicy:          []string{"/stage-a0/build-utils/bin", "/stage-a0/toolchain/bin"},
			ContainerMaterialID: "oci",
		},
		Main: stagea0.MainLockMain{
			ForkCommit:       firstbuild.ExpectedMainCommit,
			ForkTree:         firstbuild.ExpectedMainTree,
			ForkParentCommit: firstbuild.ExpectedMainParent,
		},
		Build: stagea0.MainLockBuild{
			Entrypoint:            append([]string(nil), expectedBuildEntrypoint...),
			WorkingDirectory:      "/stage-a0/src",
			VDateFormat:           "YYMMDD",
			VDateExpression:       "%y%m%d",
			AllowedFinalArtifacts: []string{"bin/MiSTer", "bin/MiSTer.elf"},
		},
		Materials: []stagea0.Material{
			{ID: "oci", Role: "consumed-build-input", OCI: &stagea0.OCIMaterial{ManifestDigest: firstbuild.ExpectedContainerImageID, OS: firstbuild.ExpectedContainerOS, Architecture: firstbuild.ExpectedContainerArchitecture}},
			{ID: "toolchain", Role: "consumed-build-input", ArchiveHTTPS: &stagea0.ArchiveHTTPSMaterial{Size: firstbuild.ExpectedToolchainArchiveSize, SHA256: firstbuild.ExpectedToolchainArchiveSHA256}},
		},
		Toolchains: []stagea0.Toolchain{{ID: "arm-none-linux-gnueabihf", TargetTriple: "arm-none-linux-gnueabihf", Components: []stagea0.ToolchainComponent{{Role: "compiler", MaterialID: "toolchain", LogicalPath: "/stage-a0/toolchain/bin/arm-none-linux-gnueabihf-gcc"}}}},
	}
}

func validReport() Report {
	comparison := precompare.Comparison{
		Format:                 precompare.FormatV1,
		Schema:                 precompare.SchemaV1,
		Status:                 firstbuild.EvidenceStatusSoftwareTested,
		SourceAvailability:     precompare.SourceAvailabilityLocal,
		TwoBuildsByteIdentical: true,
		SourceCommit:           firstbuild.ExpectedMainCommit,
		SourceTree:             firstbuild.ExpectedMainTree,
		ForkParentCommit:       firstbuild.ExpectedMainParent,
		ToolchainArchiveSHA256: firstbuild.ExpectedToolchainArchiveSHA256,
		ContainerImageID:       firstbuild.ExpectedContainerImageID,
		SourceDateEpoch:        firstbuild.ExpectedSourceDateEpoch,
		VDate:                  firstbuild.ExpectedVDate,
		LeftReportSHA256:       strings.Repeat("1", 64),
		RightReportSHA256:      strings.Repeat("2", 64),
		LeftBuildLogSHA256:     strings.Repeat("3", 64),
		RightBuildLogSHA256:    strings.Repeat("4", 64),
		Artifacts: []precompare.ArtifactComparison{
			{Path: "bin/MiSTer", Size: firstbuild.ExpectedMiSTerSize, SHA256: firstbuild.ExpectedMiSTerSHA256},
			{Path: "bin/MiSTer.elf", Size: firstbuild.ExpectedMiSTerELFSize, SHA256: firstbuild.ExpectedMiSTerELFSHA256},
		},
	}
	return Report{
		Format:                 FormatV1,
		Schema:                 SchemaV1,
		Status:                 StatusSoftwareTested,
		SourceAvailability:     SourceAvailabilityLocal,
		FreshCaptures:          FreshCaptureCountV1,
		DistinctCaptureRoots:   true,
		TwoBuildsByteIdentical: true,
		LockSHA256:             strings.Repeat("a", 64),
		Comparison:             comparison,
	}
}

// This fixture is intentionally only a shape check: ValidateLock should not
// accept an unbound zero-value lock even if a future parser changes defaults.
func stagea0MainLockFixture() stagea0.MainLock { return stagea0.MainLock{} }

func hasCode(err error, want Code) bool {
	var failure *Failure
	return errors.As(err, &failure) && failure.Code == want
}
