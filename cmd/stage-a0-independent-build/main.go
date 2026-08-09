// Command stage-a0-independent-build runs the bounded Stage A0 independent
// build gate. It emits only Software-tested/local-only evidence and refuses
// to reuse an input, capture root, or report path.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/DeanoC/FogCast-POC/internal/stagea0/independent"
)

const (
	exitOK    = 0
	exitGate  = 1
	exitUsage = 2
	usageText = "usage: stage-a0-independent-build --lock FILE --source DIR --toolchain-archive FILE --left DIR --right DIR --report FILE\n"
)

type request struct {
	lock, source, toolchain, left, right, report string
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	req, ok := parseArgs(args)
	if !ok {
		_, _ = io.WriteString(stderr, usageText)
		return exitUsage
	}
	lock, err := os.ReadFile(req.lock)
	if err != nil {
		_, _ = io.WriteString(stderr, "INDEPENDENT_LOCK_INVALID: lock cannot be read\n")
		return exitUsage
	}
	report, err := independent.Run(context.Background(), independent.Request{
		Lock:             lock,
		SourceDir:        req.source,
		ToolchainArchive: req.toolchain,
		LeftOutputDir:    req.left,
		RightOutputDir:   req.right,
		ReportPath:       req.report,
	})
	if err != nil {
		var failure *independent.Failure
		if errors.As(err, &failure) {
			_, _ = fmt.Fprintf(stderr, "%s\n", failure)
		} else {
			_, _ = io.WriteString(stderr, "INDEPENDENT_CAPTURE_FAILED: gate failed\n")
		}
		return exitGate
	}
	if err := independent.WriteReport(req.report, report); err != nil {
		var failure *independent.Failure
		if errors.As(err, &failure) {
			_, _ = fmt.Fprintf(stderr, "%s\n", failure)
		} else {
			_, _ = io.WriteString(stderr, "INDEPENDENT_REPORT_WRITE_FAILED: report cannot be written\n")
		}
		return exitUsage
	}
	_, _ = io.WriteString(stdout, "Software-tested / local-only independent builds: pass\n")
	return exitOK
}

func parseArgs(args []string) (request, bool) {
	var req request
	seen := make(map[string]bool, 6)
	for i := 0; i < len(args); i += 2 {
		if i+1 >= len(args) || args[i+1] == "" || seen[args[i]] {
			return request{}, false
		}
		key, value := args[i], args[i+1]
		seen[key] = true
		switch key {
		case "--lock":
			req.lock = value
		case "--source":
			req.source = value
		case "--toolchain-archive":
			req.toolchain = value
		case "--left":
			req.left = value
		case "--right":
			req.right = value
		case "--report":
			req.report = value
		default:
			return request{}, false
		}
	}
	return req, seen["--lock"] && seen["--source"] && seen["--toolchain-archive"] && seen["--left"] && seen["--right"] && seen["--report"]
}
