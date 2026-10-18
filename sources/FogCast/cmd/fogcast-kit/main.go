// Command fogcast-kit answers the boot UI probe of base images whose software
// supervisor still runs `fogcast-kit --print-kit-ui` before starting the
// launcher. It always selects tenfoot; the grid launcher was removed (#728).
// Delete this command once every kit runs a base whose supervisor starts
// fogcast-tenfoot directly.
package main

import (
	"fmt"
	"io"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	for _, arg := range args {
		if arg == "--print-kit-ui" || arg == "-print-kit-ui" {
			fmt.Fprintln(stdout, "tenfoot")
			return 0
		}
	}
	fmt.Fprintln(stderr, "fogcast-kit: the grid launcher was removed; the kit UI is fogcast-tenfoot")
	return 2
}
