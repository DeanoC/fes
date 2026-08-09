package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/DeanoC/FogCast-POC/internal/stagea0/precompare"
)

const (
	exitOK      = 0
	exitUsage   = 2
	exitCompare = 1
	usageText   = "usage: stage-a0-firstbuild-precompare --left DIR --right DIR --report DIR\n"
	successText = "Software-tested (local-only; retained final binaries byte-identical)\n"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

type request struct {
	left   string
	right  string
	report string
}

func run(args []string, stdout, stderr io.Writer) int {
	req, ok := parseArgs(args)
	if !ok {
		_, _ = io.WriteString(stderr, usageText)
		return exitUsage
	}
	comparison, err := precompare.Compare(req.left, req.right)
	if err == nil {
		err = precompare.WriteReport(req.report, comparison)
	}
	if err != nil {
		var failure *precompare.Failure
		if errors.As(err, &failure) {
			_, _ = fmt.Fprintf(stderr, "%s: %s\n", failure.Code, failure.Detail)
		} else {
			_, _ = io.WriteString(stderr, "PRECOMPARE_REPORT_INVALID: comparison failed\n")
		}
		return exitCompare
	}
	_, _ = io.WriteString(stdout, successText)
	return exitOK
}

func parseArgs(args []string) (request, bool) {
	var req request
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		if i+1 >= len(args) || args[i+1] == "" {
			return request{}, false
		}
		value := args[i+1]
		switch args[i] {
		case "--left":
			if seen[args[i]] {
				return request{}, false
			}
			req.left, seen[args[i]] = value, true
		case "--right":
			if seen[args[i]] {
				return request{}, false
			}
			req.right, seen[args[i]] = value, true
		case "--report":
			if seen[args[i]] {
				return request{}, false
			}
			req.report, seen[args[i]] = value, true
		default:
			return request{}, false
		}
		i++
	}
	return req, seen["--left"] && seen["--right"] && seen["--report"]
}
