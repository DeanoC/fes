package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/internal/fpgadev"
)

func TestCLIPreflightSuccessHasExactFramingAndNoMutation(t *testing.T) {
	runner := &fakeCommandRunner{preflight: nil}
	var stdout, stderr bytes.Buffer
	if got := run([]string{"preflight", "--manifest", "/tmp/fixture/manifest.json", "--artifact", "/tmp/fixture/top.rbf"}, &stdout, &stderr, runner); got != exitOK {
		t.Fatalf("exit = %d, want %d; stdout=%q stderr=%q", got, exitOK, stdout.String(), stderr.String())
	}
	if stdout.String() != "FOGCAST_FPGA_DEV_PREFLIGHT code=ok\n" || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if runner.preflightCalls != 1 || runner.runCalls != 0 {
		t.Fatalf("calls = preflight:%d run:%d", runner.preflightCalls, runner.runCalls)
	}
}

func TestCLIPreflightOwnershipConflictIncludesSafeRejectStageAndCause(t *testing.T) {
	failure := &fpgadev.Failure{Code: fpgadev.CodeOwnershipConflict, Detail: "Main readiness is unavailable"}
	runner := &fakeCommandRunner{preflight: errors.Join(failure, opaqueCauseError{cause: errors.New("compatibility Main is not uniquely present")})}
	var stdout, stderr bytes.Buffer
	if got := run([]string{"preflight", "--manifest", "/tmp/fixture/manifest.json", "--artifact", "/tmp/fixture/top.rbf"}, &stdout, &stderr, runner); got != exitUsage {
		t.Fatalf("exit = %d, want %d", got, exitUsage)
	}
	want := "FOGCAST_FPGA_DEV_PREFLIGHT code=ownership_conflict detail=main_readiness_unavailable cause=compatibility_main_not_unique\n"
	if stdout.Len() != 0 || stderr.String() != want {
		t.Fatalf("stdout=%q stderr=%q, want empty stdout and stderr=%q", stdout.String(), stderr.String(), want)
	}
}

type opaqueCauseError struct {
	cause error
}

func (e opaqueCauseError) Error() string {
	return "ownership conflict cause withheld"
}

func (e opaqueCauseError) Unwrap() error {
	return e.cause
}

func TestCLIPreflightOwnershipConflictClassifiesJournalProofWithoutRawPath(t *testing.T) {
	failure := &fpgadev.Failure{Code: fpgadev.CodeOwnershipConflict, Detail: "quiescence proof is unavailable"}
	runner := &fakeCommandRunner{preflight: errors.Join(failure, errors.New("terminal journal digest does not match maintenance proof: /private/secret"))}
	var stdout, stderr bytes.Buffer
	if got := run([]string{"preflight", "--manifest", "/tmp/fixture/manifest.json", "--artifact", "/tmp/fixture/top.rbf"}, &stdout, &stderr, runner); got != exitUsage {
		t.Fatalf("exit = %d, want %d", got, exitUsage)
	}
	want := "FOGCAST_FPGA_DEV_PREFLIGHT code=ownership_conflict detail=quiescence_proof_unavailable cause=terminal_journal_digest_mismatch\n"
	if stdout.Len() != 0 || stderr.String() != want || strings.Contains(stderr.String(), "/private/secret") {
		t.Fatalf("stdout=%q stderr=%q, want sanitized stderr=%q", stdout.String(), stderr.String(), want)
	}
}

func TestCLIRejectsMalformedSyntaxWithoutCallingRunner(t *testing.T) {
	runner := &fakeCommandRunner{}
	var stdout, stderr bytes.Buffer
	if got := run([]string{"preflight", "--artifact", "x", "--manifest", "y"}, &stdout, &stderr, runner); got != exitUsage {
		t.Fatalf("exit = %d, want %d", got, exitUsage)
	}
	if stdout.Len() != 0 || stderr.Len() == 0 || runner.preflightCalls != 0 {
		t.Fatalf("stdout=%q stderr=%q calls=%d", stdout.String(), stderr.String(), runner.preflightCalls)
	}
}

func TestCLIProductionBuildRejectsFaultCommands(t *testing.T) {
	runner := &fakeCommandRunner{}
	var stdout, stderr bytes.Buffer
	if got := run([]string{"fault-arm", "--run-id", "0123456789abcdef0123456789abcdef"}, &stdout, &stderr, runner); got != exitUsage {
		t.Fatalf("exit = %d, want %d", got, exitUsage)
	}
	if stdout.Len() != 0 || stderr.Len() == 0 || runner.runCalls != 0 || runner.preflightCalls != 0 {
		t.Fatalf("stdout=%q stderr=%q calls=%d/%d", stdout.String(), stderr.String(), runner.runCalls, runner.preflightCalls)
	}
}

func TestCLIRunSuccessUsesOnlyExactTwoLineOutput(t *testing.T) {
	payload := []byte("OSS FPGA OK\n")
	digest := sha256.Sum256(payload)
	runner := &fakeCommandRunner{runResult: fpgadev.Result{
		Schema:          fpgadev.ResultSchemaVersion,
		RunID:           strings.Repeat("1", 32),
		Generation:      42,
		Session:         strings.Repeat("2", 32),
		Mode:            fpgadev.ResultModeUpdating,
		Experiment:      fpgadev.ManifestExperiment,
		BuildLane:       fpgadev.BuildLaneOSS,
		ArtifactSHA256:  strings.Repeat("a", 64),
		SourceCommit:    strings.Repeat("b", 40),
		Phase:           fpgadev.ResultPhaseDoneObserved,
		PrimaryCode:     string(fpgadev.CodeOK),
		PayloadHex:      hex.EncodeToString(payload),
		PayloadLength:   uint64(len(payload)),
		PayloadSHA256:   hex.EncodeToString(digest[:]),
		TerminalWord:    "d3130c00",
		RecoveryRequest: "pending",
	}}
	var stdout, stderr bytes.Buffer
	if got := run([]string{"run", "--manifest", "/tmp/fixture/manifest.json", "--artifact", "/tmp/fixture/top.rbf"}, &stdout, &stderr, runner); got != exitOK {
		t.Fatalf("exit = %d, want %d; stdout=%q stderr=%q", got, exitOK, stdout.String(), stderr.String())
	}
	want := "FPGA> OSS FPGA OK\nFOGCAST_FPGA_DEV_RESULT run_id=" + strings.Repeat("1", 32) + " primary=ok\n"
	if stdout.String() != want || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q, want stdout=%q and empty stderr", stdout.String(), stderr.String(), want)
	}
}

func TestCLIRunPostIntentFailureUsesResultFramingOnly(t *testing.T) {
	runID := strings.Repeat("1", 32)
	result := fpgadev.Result{
		Schema:          fpgadev.ResultSchemaVersion,
		RunID:           runID,
		Generation:      42,
		Session:         strings.Repeat("2", 32),
		Mode:            fpgadev.ResultModeUpdating,
		Experiment:      fpgadev.ManifestExperiment,
		BuildLane:       fpgadev.BuildLaneOSS,
		ArtifactSHA256:  strings.Repeat("a", 64),
		SourceCommit:    strings.Repeat("b", 40),
		Phase:           fpgadev.ResultPhaseLoadAttempted,
		PrimaryCode:     string(fpgadev.CodeLoadDispatchFailed),
		PrimaryDetail:   "Main load dispatch failed",
		PayloadSHA256:   fpgadev.EmptyPayloadSHA256,
		TerminalWord:    "00000000",
		RecoveryRequest: "pending",
	}
	runner := &fakeCommandRunner{runResult: result}
	var stdout, stderr bytes.Buffer
	if got := run([]string{"run", "--manifest", "/tmp/fixture/manifest.json", "--artifact", "/tmp/fixture/top.rbf"}, &stdout, &stderr, runner); got != exitFailure {
		t.Fatalf("exit = %d, want %d", got, exitFailure)
	}
	want := "FOGCAST_FPGA_DEV_RESULT run_id=" + runID + " primary=load_dispatch_failed\n"
	if stdout.String() != want || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q, want result line and empty stderr", stdout.String(), stderr.String())
	}
}

func TestCLIRunRecoveryFailureKeepsResultFramingWithoutSecondarySchema(t *testing.T) {
	runID := strings.Repeat("1", 32)
	payload := []byte("OSS FPGA OK\n")
	digest := sha256.Sum256(payload)
	result := fpgadev.Result{
		Schema: fpgadev.ResultSchemaVersion, RunID: runID, Generation: 42,
		Session: strings.Repeat("2", 32), Mode: fpgadev.ResultModeUpdating,
		Experiment: fpgadev.ManifestExperiment, BuildLane: fpgadev.BuildLaneOSS,
		ArtifactSHA256: strings.Repeat("a", 64), SourceCommit: strings.Repeat("b", 40),
		Phase: fpgadev.ResultPhaseDoneObserved, PrimaryCode: string(fpgadev.CodeOK),
		PayloadHex: hex.EncodeToString(payload), PayloadLength: uint64(len(payload)),
		PayloadSHA256: hex.EncodeToString(digest[:]), TerminalWord: "d3130c00",
		RecoveryRequest: "pending",
	}
	runner := &fakeCommandRunner{
		runResult: result,
		runError:  &fpgadev.Failure{Code: fpgadev.CodeStateStoreFailed, IntentCommitted: true},
	}
	var stdout, stderr bytes.Buffer
	if got := run([]string{"run", "--manifest", "/tmp/fixture/manifest.json", "--artifact", "/tmp/fixture/top.rbf"}, &stdout, &stderr, runner); got != exitUsage {
		t.Fatalf("exit = %d, want %d; stdout=%q stderr=%q", got, exitUsage, stdout.String(), stderr.String())
	}
	wantOut := "FOGCAST_FPGA_DEV_RESULT run_id=" + runID + " primary=ok\n"
	if stdout.String() != wantOut || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestCLIRunResultCreateFailureHasNoResultFraming(t *testing.T) {
	failure := &fpgadev.Failure{Code: fpgadev.CodeStateStoreFailed, IntentCommitted: true}
	runner := &fakeCommandRunner{runError: errors.Join(failure, fpgadev.ErrResultUnavailable)}
	var stdout, stderr bytes.Buffer
	if got := run([]string{"run", "--manifest", "/tmp/fixture/manifest.json", "--artifact", "/tmp/fixture/top.rbf"}, &stdout, &stderr, runner); got != exitUsage {
		t.Fatalf("exit = %d, want %d; stdout=%q stderr=%q", got, exitUsage, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 || stderr.String() != "FOGCAST_FPGA_DEV_RESULT_UNAVAILABLE code=state_store_failed\n" {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

type fakeCommandRunner struct {
	preflight      error
	preflightCalls int
	runCalls       int
	runResult      fpgadev.Result
	runError       error
}

func (r *fakeCommandRunner) Preflight(context.Context, fpgadev.Request) error {
	r.preflightCalls++
	return r.preflight
}

func (r *fakeCommandRunner) RunCommand(context.Context, fpgadev.Request) (fpgadev.Result, error) {
	r.runCalls++
	return r.runResult, r.runError
}
