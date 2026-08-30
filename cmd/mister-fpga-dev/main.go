package main

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/DeanoC/FogCast-POC/internal/fpgadev"
)

const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

type commandRunner interface {
	Preflight(context.Context, fpgadev.Request) error
	RunCommand(context.Context, fpgadev.Request) (fpgadev.Result, error)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, fpgadev.NewProductionRunner()))
}

func run(args []string, stdout, stderr io.Writer, runner commandRunner) int {
	if len(args) == 0 {
		return usage(stderr)
	}
	if faultCommandName(args[0]) {
		return runFaultCommand(args, stdout, stderr, runner)
	}
	if installCommandName(args[0]) {
		return runInstallCommand(args, stdout, stderr, runner)
	}
	if args[0] != "preflight" && args[0] != "run" {
		return usage(stderr)
	}
	request, ok := parseRequest(args[1:])
	if !ok {
		return usage(stderr)
	}
	if runner == nil {
		return commandFailure(stderr, args[0], fpgadev.CodeProfileDisabled)
	}
	ctx := context.Background()
	if args[0] == "preflight" {
		if err := runner.Preflight(ctx, request); err != nil {
			return commandFailure(stderr, "preflight", failureCode(err, fpgadev.CodeManifestRejected), err)
		}
		_, _ = io.WriteString(stdout, fpgadev.PreflightLine(fpgadev.CodeOK))
		return exitOK
	}
	result, err := runner.RunCommand(ctx, request)
	if err != nil {
		var failure *fpgadev.Failure
		if !errors.As(err, &failure) || failure == nil || !failure.IntentCommitted {
			return commandFailure(stderr, "run", failureCode(err, fpgadev.CodeManifestRejected))
		}
		if errors.Is(err, fpgadev.ErrResultUnavailable) {
			_, _ = io.WriteString(stderr, "FOGCAST_FPGA_DEV_RESULT_UNAVAILABLE code=state_store_failed\n")
			return exitUsage
		}
		if result.Validate() != nil {
			return commandFailure(stderr, "run", fpgadev.CodeStateStoreFailed)
		}
		if line := fpgadev.ResultLine(result); line != "" {
			_, _ = io.WriteString(stdout, line)
		}
		return exitUsage
	}
	if err := result.Validate(); err != nil {
		return commandFailure(stderr, "run", fpgadev.CodeStateStoreFailed)
	}
	if result.PrimaryCode == string(fpgadev.CodeOK) {
		_, _ = io.WriteString(stdout, fpgadev.SuccessOutput(result))
		return exitOK
	}
	_, _ = io.WriteString(stdout, fpgadev.ResultLine(result))
	return exitFailure
}

func parseRequest(args []string) (fpgadev.Request, bool) {
	if len(args) != 4 || args[0] != "--manifest" || args[2] != "--artifact" || args[1] == "" || args[3] == "" {
		return fpgadev.Request{}, false
	}
	return fpgadev.Request{ManifestPath: args[1], ArtifactPath: args[3]}, true
}

func usage(stderr io.Writer) int {
	_, _ = io.WriteString(stderr, "usage: mister-fpga-dev preflight|run --manifest PATH --artifact PATH\n")
	return exitUsage
}

func commandFailure(stderr io.Writer, command string, code fpgadev.Code, commandErr ...error) int {
	if command == "preflight" {
		line := fpgadev.PreflightLine(code)
		if line == "" {
			line = fpgadev.PreflightLine(fpgadev.CodeManifestRejected)
		}
		if code == fpgadev.CodeOwnershipConflict && len(commandErr) != 0 && commandErr[0] != nil {
			line = ownershipConflictPreflightLine(commandErr[0])
		}
		_, _ = io.WriteString(stderr, line)
	} else {
		line := fpgadev.RunLine(code)
		if line == "" {
			line = fpgadev.RunLine(fpgadev.CodeManifestRejected)
		}
		_, _ = io.WriteString(stderr, line)
	}
	return exitUsage
}

func ownershipConflictPreflightLine(err error) string {
	detail := "unclassified"
	var failure *fpgadev.Failure
	if errors.As(err, &failure) && failure != nil {
		details := map[string]string{
			"maintenance gate is unavailable":          "maintenance_gate_unavailable",
			"maintenance status is unavailable":        "maintenance_status_unavailable",
			"owner admission is unavailable":           "owner_admission_unavailable",
			"owner record is unavailable":              "owner_record_unavailable",
			"owner record is invalid":                  "owner_record_invalid",
			"owner admission is fenced":                "owner_admission_fenced",
			"quiescence proof is unavailable":          "quiescence_proof_unavailable",
			"Main readiness is unavailable":            "main_readiness_unavailable",
			"development tool identity is unavailable": "tool_identity_unavailable",
			"Main process observer is unavailable":     "main_observer_unavailable",
			"Main process baseline is unavailable":     "main_baseline_unavailable",
			"Main process baseline is invalid":         "main_baseline_invalid",
			"preflight cleanup failed":                 "preflight_cleanup_failed",
			"operation canceled":                       "operation_canceled",
		}
		if classified, ok := details[failure.Detail]; ok {
			detail = classified
		}
	}
	cause := classifyOwnershipConflictCause(err)
	return "FOGCAST_FPGA_DEV_PREFLIGHT code=ownership_conflict detail=" + detail + " cause=" + cause + "\n"
}

func classifyOwnershipConflictCause(err error) string {
	for _, candidate := range []struct {
		contains string
		token    string
	}{
		{"terminal journal digest does not match maintenance proof", "terminal_journal_digest_mismatch"},
		{"terminal journal inventory does not match maintenance proof", "terminal_journal_inventory_mismatch"},
		{"terminal journal is not terminal", "terminal_journal_not_terminal"},
		{"terminal journal is unavailable", "terminal_journal_unavailable"},
		{"owner is not a reclaimable compatibility Main", "owner_not_reclaimable"},
		{"owner record is invalid", "owner_record_invalid"},
		{"current boot changed during prior-boot admission", "current_boot_changed"},
		{"owner does not belong to a prior boot", "owner_not_prior_boot"},
		{"compatibility Main is not uniquely present", "compatibility_main_not_unique"},
		{"process identity changed", "process_identity_changed"},
		{"process scanner unsupported", "process_scanner_unsupported"},
		{"Main command FIFO", "main_fifo_not_ready"},
		{"FPGA manager is not operating", "fpga_manager_not_operating"},
		{"CORENAME", "menu_not_ready"},
		{"deadline exceeded", "deadline_exceeded"},
		{"operation canceled", "operation_canceled"},
	} {
		if errorTreeContains(err, candidate.contains, 0) {
			return candidate.token
		}
	}
	return "unclassified"
}

func errorTreeContains(err error, fragment string, depth int) bool {
	if err == nil || depth >= 64 {
		return false
	}
	if strings.Contains(err.Error(), fragment) {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			if errorTreeContains(child, fragment, depth+1) {
				return true
			}
		}
		return false
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return errorTreeContains(wrapped.Unwrap(), fragment, depth+1)
	}
	return false
}

func failureCode(err error, fallback fpgadev.Code) fpgadev.Code {
	var failure *fpgadev.Failure
	if errors.As(err, &failure) && failure != nil && failure.Code != "" {
		return failure.Code
	}
	return fallback
}
