package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/DeanoC/FogCast/internal/packagepoc"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("package-poc1a", flag.ContinueOnError)
	flags.SetOutput(stderr)
	source := flags.String("source", "", "directory containing the four deployment files")
	output := flags.String("output", "", "output .tar.gz path")
	timestamp := flags.String("time", "", "fixed archive timestamp in RFC3339 format")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *source == "" || *output == "" || *timestamp == "" {
		fmt.Fprintln(stderr, "package-poc1a: --source, --output, and --time are required")
		return 2
	}
	fixedTime, err := time.Parse(time.RFC3339, *timestamp)
	if err != nil {
		fmt.Fprintln(stderr, "package-poc1a: --time must be RFC3339")
		return 2
	}
	if err := packagepoc.Create(*source, *output, fixedTime); err != nil {
		fmt.Fprintf(stderr, "package-poc1a: %v\n", err)
		return 1
	}
	return 0
}
