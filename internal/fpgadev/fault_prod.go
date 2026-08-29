//go:build !linux || !fpgadev

package fpgadev

import (
	"context"

	"github.com/DeanoC/FogCast-POC/internal/hardwareowner"
)

func readProductionManifest(context.Context, string, uint32, func()) ([]byte, *productionStagingDirectory, error) {
	return nil, nil, ErrUnsupported
}

// Untagged and non-Linux builds contain no fault-control implementation or
// exported fault methods. The lifecycle hook is a compile-time no-op only;
// the production runner itself is still disabled by run_stub.go.
func (r *Runner) afterDispatch(context.Context, runnerDependencies, *preparedRun, hardwareowner.Record) error {
	return nil
}
