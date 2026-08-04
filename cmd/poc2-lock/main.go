package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/DeanoC/FogCast-POC/internal/imagepoc"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "poc2-lock: command is required: record or verify")
		return 2
	}
	switch args[0] {
	case "record":
		return runRecord(args[1:], stdout, stderr)
	case "verify":
		return runVerify(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "poc2-lock: unknown command %q\n", args[0])
		return 2
	}
}

type lockPaths struct {
	lock  *string
	poc1a *string
	poc1b *string
	prod  *string
	dev   *string
}

func bindPaths(flags *flag.FlagSet, lockName string) lockPaths {
	return lockPaths{
		lock:  flags.String(lockName, "", "POC 2 output lock"),
		poc1a: flags.String("poc1a-lock", "", "accepted POC 1A lock"),
		poc1b: flags.String("poc1b-lock", "", "accepted POC 1B lock"),
		prod:  flags.String("prod", "", "POC 2 production root image"),
		dev:   flags.String("dev", "", "POC 2 development root image"),
	}
}

func (paths lockPaths) complete() bool {
	return *paths.lock != "" && *paths.poc1a != "" && *paths.poc1b != "" &&
		*paths.prod != "" && *paths.dev != ""
}

func runRecord(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("record", flag.ContinueOnError)
	flags.SetOutput(stderr)
	paths := bindPaths(flags, "output")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || !paths.complete() {
		fmt.Fprintln(stderr, "poc2-lock record: --poc1a-lock, --poc1b-lock, --prod, --dev, and --output are required")
		return 2
	}
	if err := imagepoc.RecordPOC2(*paths.poc1a, *paths.poc1b, *paths.prod, *paths.dev, *paths.lock); err != nil {
		fmt.Fprintf(stderr, "poc2-lock record: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "recorded POC 2 outputs in %s\n", *paths.lock)
	return 0
}

func runVerify(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(stderr)
	paths := bindPaths(flags, "lock")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || !paths.complete() {
		fmt.Fprintln(stderr, "poc2-lock verify: --lock, --poc1a-lock, --poc1b-lock, --prod, and --dev are required")
		return 2
	}
	if err := imagepoc.VerifyPOC2(*paths.lock, *paths.poc1a, *paths.poc1b, *paths.prod, *paths.dev); err != nil {
		fmt.Fprintf(stderr, "poc2-lock verify: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "POC 2 outputs verified")
	return 0
}
