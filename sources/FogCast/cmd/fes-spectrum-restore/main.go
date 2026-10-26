// fes-spectrum-restore writes the restore stream that loads a .z80 or .sna
// snapshot on a Spectrum core, as the host does when the snapshot is inserted.
// The Spectrum simulations use it.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/DeanoC/FogCast/internal/spectrumsnapshots"
)

func run(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("fes-spectrum-restore", flag.ContinueOnError)
	flags.SetOutput(stderr)
	output := flags.String("out", "", "restore stream to write")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *output == "" || flags.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: fes-spectrum-restore -out stream.bin snapshot.{z80,sna}")
		return 2
	}
	data, err := os.ReadFile(flags.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	_, stream, err := spectrumsnapshots.Convert(data)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := os.WriteFile(*output, stream, 0o644); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func main() { os.Exit(run(os.Args[1:], os.Stderr)) }
