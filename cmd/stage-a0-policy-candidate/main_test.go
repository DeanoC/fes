package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/internal/stagea0/firstbuild"
	"github.com/DeanoC/FogCast-POC/internal/stagea0/policy"
)

func TestParseArgsRequiresExactlyOneValueForEachFlag(t *testing.T) {
	valid := []string{
		"--repository", "/fork",
		"--source-material", "main-fork",
		"--authority", "/authority.json",
		"--build-log", "/build.log",
		"--receipt", "/receipt.json",
		"--artifact-dir", "/capture",
		"--toolchain-archive", "/toolchain.tar.xz",
		"--toolchain-root", "/toolchain",
		"--output-dir", "/out",
	}
	req, ok := parseArgs(valid)
	if !ok || req.repository != "/fork" || req.outputDir != "/out" {
		t.Fatalf("parseArgs(valid) = %#v, %v", req, ok)
	}

	cases := [][]string{
		{},
		{"--repository"},
		{"--repository", "/fork", "--repository", "/other"},
		{"--repository", "/fork", "--source-material", "main-fork", "--authority", "/authority.json", "--build-log", "/build.log", "--receipt", "/receipt.json", "--artifact-dir", "/capture", "--toolchain-archive", "/toolchain.tar.xz", "--toolchain-root", "/toolchain", "--output-dir", "/out", "--unknown", "x"},
		{"--repository", "/fork", "--source-material", "main-fork", "--authority", "/authority.json", "--build-log", "/build.log", "--receipt", "", "--artifact-dir", "/capture", "--toolchain-archive", "/toolchain.tar.xz", "--toolchain-root", "/toolchain", "--output-dir", "/out"},
	}
	for _, args := range cases {
		if _, ok := parseArgs(args); ok {
			t.Errorf("parseArgs(%q) unexpectedly accepted", args)
		}
	}
}

func TestRunPrintsUsageForMalformedArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--repository"}, &stdout, &stderr); code != 2 {
		t.Fatalf("run() code = %d, want 2", code)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "usage: stage-a0-policy-candidate") {
		t.Fatalf("run() output = stdout %q stderr %q", stdout.String(), stderr.String())
	}
}

func TestBindEvidenceAuthorityRejectsMismatchedReceipt(t *testing.T) {
	evidence := firstbuild.Evidence{
		Source: firstbuild.SourceEvidence{Commit: "fork", Tree: "tree", Parent: "parent"},
		Build:  firstbuild.BuildEvidence{SourceDateEpoch: 10, VDate: "700101"},
	}
	authority := policy.Authority{ForkCommit: "fork", ForkTree: "tree", ForkParentCommit: "parent", SourceDateEpoch: 10, VDate: "700101"}
	if err := bindEvidenceAuthority(evidence, authority); err != nil {
		t.Fatalf("matching authority rejected: %v", err)
	}
	evidence.Source.Tree = "wrong"
	if err := bindEvidenceAuthority(evidence, authority); err == nil {
		t.Fatal("mismatched tree was accepted")
	}
}

func TestBindBuildLogRejectsMixedOrMissingVDATE(t *testing.T) {
	authority := policy.Authority{VDate: "260808"}
	authority.SourceDateEpoch = 1786215171
	validLog := `STAGE_A0_JOB_COUNT=1
SOURCE_DATE_EPOCH=1786215171 make clean VDATE=260808 make V=1 VDATE=260808 -DVDATE=\"260808\"`
	if err := bindBuildLog(validLog, authority); err != nil {
		t.Fatalf("matching VDATE rejected: %v", err)
	}
	for _, log := range []string{"make", "make VDATE=260807", "make VDATE=260808 VDATE=260807", "SOURCE_DATE_EPOCH=1786215171 make VDATE=260808", "STAGE_A0_JOB_COUNT=2\nSOURCE_DATE_EPOCH=1786215171 make clean VDATE=260808 make V=1 VDATE=260808", "STAGE_A0_JOB_COUNT=1\nSTAGE_A0_JOB_COUNT=1\nSOURCE_DATE_EPOCH=1786215171 make clean VDATE=260808 make V=1 VDATE=260808"} {
		if err := bindBuildLog(log, authority); err == nil {
			t.Errorf("log %q unexpectedly accepted", log)
		}
	}
}
