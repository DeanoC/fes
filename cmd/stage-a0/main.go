package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/DeanoC/FogCast-POC/internal/stagea0"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, stagea0.ExecRunner{})) }

func run(args []string, stdout, stderr io.Writer, runner stagea0.Runner) int {
	if len(args) == 0 {
		return usage(stderr)
	}
	command := args[0]
	bootstrapPath, destination, ok := commandArguments(command, args[1:])
	if !ok {
		return usage(stderr)
	}
	raw, err := os.ReadFile(bootstrapPath)
	if err != nil {
		return report(stderr, stagea0Failure(stagea0.CodeBootstrapSchemaInvalid, "bootstrap file cannot be read"))
	}
	bootstrap, err := stagea0.ParseBootstrap(raw)
	if err != nil {
		return report(stderr, err)
	}
	root, err := os.Getwd()
	if err != nil {
		return report(stderr, stagea0Failure(stagea0.CodeCommandFailed, "working directory cannot be read"))
	}
	if err := stagea0.VerifyFogCastC0(context.Background(), runner, bootstrap, root); err != nil {
		return report(stderr, err)
	}
	if command == "bootstrap-verify" {
		_, _ = io.WriteString(stdout, "valid\n")
		return stagea0.ExitOK
	}
	if _, err := stagea0.InitializeFork(context.Background(), runner, stagea0.InitRequest{Bootstrap: bootstrap, Destination: destination, FogCastRoot: root}); err != nil {
		return report(stderr, err)
	}
	return stagea0.ExitOK
}

func commandArguments(command string, args []string) (bootstrap, destination string, ok bool) {
	if command != "bootstrap-verify" && command != "init-main" {
		return "", "", false
	}
	if command == "bootstrap-verify" {
		if len(args) == 2 && args[0] == "--bootstrap" && args[1] != "" {
			return args[1], "", true
		}
		return "", "", false
	}
	if len(args) != 4 || args[0] != "--bootstrap" || args[2] != "--destination" || args[1] == "" || args[3] == "" {
		return "", "", false
	}
	return args[1], args[3], true
}

func usage(stderr io.Writer) int {
	_, _ = io.WriteString(stderr, "usage: stage-a0 bootstrap-verify --bootstrap FILE | init-main --bootstrap FILE --destination DIR\n")
	return stagea0.ExitUsage
}

func stagea0Failure(code stagea0.Code, detail string) error {
	return &stagea0.Failure{Code: code, Op: "cli", Detail: detail}
}

func report(stderr io.Writer, err error) int {
	var failure *stagea0.Failure
	if !errors.As(err, &failure) {
		failure = &stagea0.Failure{Code: stagea0.CodeCommandFailed, Op: "cli", Detail: "command failed"}
	}
	_, _ = fmt.Fprintf(stderr, "%s: %s\n", failure.Code, failure.Detail)
	return exitCode(err)
}

func exitCode(err error) int {
	var failure *stagea0.Failure
	if !errors.As(err, &failure) {
		return stagea0.ExitCommand
	}
	switch failure.Code {
	case stagea0.CodeBootstrapSchemaInvalid:
		return stagea0.ExitBootstrap
	case stagea0.CodeVDateRecipeMismatch, stagea0.CodeVDateInputInvalid:
		return stagea0.ExitRecipe
	case stagea0.CodeRepositoryPolicyMismatch:
		return stagea0.ExitRepository
	default:
		return stagea0.ExitCommand
	}
}
