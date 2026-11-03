package main

import (
	"context"
	"fmt"
	"io"

	"github.com/DeanoC/FogCast/internal/kitmemory"
)

// kitBinaryName is the kit bundle's name for this binary. The bundle builds
// cmd/fogcast-api as bin/fogcast-kit, a multi-call binary: `fogcast-kit host`
// is the kit's FogCast host, which the base supervisor starts (#863 slice 5).
const kitBinaryName = "fogcast-kit"

// kitMain is fogcast-kit's entry point.
//   - `host [flags]` runs this API in kit mode: -kit -headless plus flags.
//   - `--print-kit-ui` answers the boot UI probe of base images whose
//     supervisor still asks before starting the launcher. It always selects
//     tenfoot; the grid launcher was removed (#728).
func kitMain(ctx context.Context, args []string, stdout, stderr io.Writer, open openService) int {
	if len(args) > 0 && args[0] == "host" {
		kitmemory.SetLimit(kitmemory.Host)
		return run(ctx, append([]string{"-kit", "-headless"}, args[1:]...), stdout, stderr, open)
	}
	for _, arg := range args {
		if arg == "--print-kit-ui" || arg == "-print-kit-ui" {
			fmt.Fprintln(stdout, "tenfoot")
			return 0
		}
	}
	fmt.Fprintln(stderr, "fogcast-kit: usage: fogcast-kit host [fogcast-api flags]; the kit UI is fogcast-tenfoot")
	return 2
}
