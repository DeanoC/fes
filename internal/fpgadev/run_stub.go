//go:build !linux || !arm || !fpgadev

package fpgadev

// The production lifecycle is intentionally available only on Linux/ARM
// development builds.  Other builds retain the public constructor so callers
// can link normally, but the nil composition fails before designation, lock,
// store, FIFO, mapping, or result access.
func newProductionRunnerDependencies() (runnerDependencies, error) {
	return runnerDependencies{}, ErrRunnerConfiguration
}
