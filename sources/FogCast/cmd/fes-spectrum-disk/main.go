// fes-spectrum-disk writes the raw track image that a Spectrum core's disk
// unit reads for a TR-DOS .trd or .scl image, as the host does when the disk
// is inserted. The Spectrum simulations use it.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/DeanoC/FogCast/internal/spectrumdisks"
)

func run(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("fes-spectrum-disk", flag.ContinueOnError)
	flags.SetOutput(stderr)
	output := flags.String("out", "", "raw track image to write")
	boot := flags.Bool("boot", false, "flag the image as a launch disk, which the core boots")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *output == "" || flags.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: fes-spectrum-disk [-boot] -out disk.bin disk.trd")
		return 2
	}
	data, err := os.ReadFile(flags.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	image, err := spectrumdisks.Convert(data, *boot)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := os.WriteFile(*output, image, 0o644); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func main() { os.Exit(run(os.Args[1:], os.Stderr)) }
