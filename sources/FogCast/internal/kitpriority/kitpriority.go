// Package kitpriority lowers a background kit service below the launcher.
//
// On a two-core DE10-Nano the launcher's room callbacks run against a
// wall-clock budget, so the host and the agent must not take both CPUs from
// it. The kit software supervisor ships in the base image, which bundles do
// not update, so each background service applies this policy to itself.
package kitpriority

import (
	"log"
	"os"
	"runtime"
)

// Nice is the CFS nice value of a background kit service. Nice 10 has about a
// tenth of the weight of nice 0.
const Nice = 10

// managedEnv is set by the kit software supervisor on every process it starts.
const managedEnv = "FES_SOFTWARE_MANAGED"

// managed reports whether the kit software supervisor started this process.
// The shared host and dev hosts run these binaries without it.
func managed(getenv func(string) string) bool {
	return getenv(managedEnv) == "1"
}

// Background runs this process at Nice with GOMAXPROCS=1 when the kit
// software supervisor started it, and does nothing otherwise. Call it first
// in main. The launcher and the runtime never call it.
//
// Linux keeps nice per thread, and a new thread copies the nice of the thread
// that created it. Background lowers every existing thread, so threads the Go
// runtime starts later inherit Nice. A thread already at or above Nice is
// left alone. A failure is logged once and the process keeps its priority.
func Background() {
	if !managed(os.Getenv) {
		return
	}
	runtime.GOMAXPROCS(1)
	if err := lowerThreads(Nice); err != nil {
		log.Printf("kit background priority nice %d unavailable, keeping current priority: %v", Nice, err)
	}
}
