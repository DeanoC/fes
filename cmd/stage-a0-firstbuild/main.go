package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/DeanoC/FogCast-POC/internal/stagea0/firstbuild"
)

const (
	exitOK      = 0
	exitUsage   = 2
	exitCapture = 1
	usageText   = "usage: stage-a0-firstbuild --source DIR --toolchain-archive FILE --output DIR\n"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	request, ok := parseArgs(args)
	if !ok {
		_, _ = io.WriteString(stderr, usageText)
		return exitUsage
	}
	if _, err := firstbuild.Capture(context.Background(), request); err != nil {
		var failure *firstbuild.Failure
		if errors.As(err, &failure) {
			_, _ = fmt.Fprintf(stderr, "%s: %s\n", failure.Code, failure.Detail)
		} else {
			_, _ = io.WriteString(stderr, "FIRSTBUILD_BUILD_FAILED: capture failed\n")
		}
		return exitCapture
	}
	_, _ = io.WriteString(stdout, "Software-tested\n")
	return exitOK
}

func parseArgs(args []string) (firstbuild.Request, bool) {
	request := firstbuild.DefaultRequest()
	seenSource, seenToolchain, seenOutput := false, false, false
	for i := 0; i < len(args); i++ {
		if i+1 >= len(args) {
			return firstbuild.Request{}, false
		}
		value := args[i+1]
		if value == "" {
			return firstbuild.Request{}, false
		}
		switch args[i] {
		case "--source":
			if seenSource {
				return firstbuild.Request{}, false
			}
			request.SourceDir, seenSource = value, true
		case "--toolchain-archive":
			if seenToolchain {
				return firstbuild.Request{}, false
			}
			request.ToolchainArchive, seenToolchain = value, true
		case "--output":
			if seenOutput {
				return firstbuild.Request{}, false
			}
			request.OutputDir, seenOutput = value, true
		default:
			return firstbuild.Request{}, false
		}
		i++
	}
	return request, seenSource && seenToolchain && seenOutput
}
