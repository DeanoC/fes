package shared

import (
	"os"
	"runtime/debug"
)

// ConfigureKitMemoryLimit gives the CPU kit launchers a 96 MiB soft Go memory
// budget on the 492 MiB board shared with the runtime. Artwork decoding can
// temporarily exceed it; the runtime collects and scavenges sooner instead of
// retaining a startup-sized heap during idle. An explicit GOMEMLIMIT wins.
// Call only from a kit launcher entry point, before starting its workers.
func ConfigureKitMemoryLimit() {
	if _, configured := os.LookupEnv("GOMEMLIMIT"); !configured {
		debug.SetMemoryLimit(96 << 20)
	}
}
