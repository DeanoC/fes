package main

import (
	"context"
	"errors"
	"io"
	"os"

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
			return commandFailure(stderr, "preflight", failureCode(err, fpgadev.CodeManifestRejected))
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

func commandFailure(stderr io.Writer, command string, code fpgadev.Code) int {
	if command == "preflight" {
		line := fpgadev.PreflightLine(code)
		if line == "" {
			line = fpgadev.PreflightLine(fpgadev.CodeManifestRejected)
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

func failureCode(err error, fallback fpgadev.Code) fpgadev.Code {
	var failure *fpgadev.Failure
	if errors.As(err, &failure) && failure != nil && failure.Code != "" {
		return failure.Code
	}
	return fallback
}
