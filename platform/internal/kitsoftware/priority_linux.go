package kitsoftware

import (
	"log"
	"runtime"
	"sync"

	"golang.org/x/sys/unix"
)

// backgroundNice is the CFS nice value of the host and the agent. The
// launcher's UI thread runs Lua room callbacks against a wall-clock budget, so
// on a two-core kit it must win the CPU from background work during startup.
// Nice 10 has about a tenth of the weight of nice 0. The launcher and the
// runtime keep the supervisor's own priority.
const backgroundNice = 10

// spawnPolicy is what a role adds to the process it starts.
type spawnPolicy struct {
	// background starts the process from backgroundForker, so every thread
	// of it runs at backgroundNice.
	background bool
	// env is appended to the role's environment. GOMAXPROCS=1 keeps a
	// background Go service to one CPU of Go code at a time, so it cannot
	// occupy both cores of a DE10-Nano at once.
	env []string
}

func roleSpawnPolicy(role string) spawnPolicy {
	switch role {
	case roleHost, roleAgent:
		return spawnPolicy{background: true, env: []string{"GOMAXPROCS=1"}}
	}
	return spawnPolicy{}
}

// niceForker runs process starts on one OS thread whose own nice is set.
// Linux keeps nice per thread and a forked child copies the forking thread's
// value. Every thread the child later creates inherits it from the child.
// Changing the child after Start would race its first threads.
//
// The thread stays locked for the life of the supervisor and is never
// returned to the Go scheduler, so no other goroutine runs at its priority.
// It also never exits: Pdeathsig fires when the forking thread exits, not
// the process, so a retired thread would SIGKILL every child it started.
// The Go runtime starts its own new threads from a template thread, not from
// a locked one, so they do not inherit this nice either.
type niceForker struct {
	nice     int
	once     sync.Once
	requests chan func()
}

var backgroundForker = &niceForker{nice: backgroundNice}

// run calls start on the forker's thread and waits for it to return.
func (f *niceForker) run(start func()) {
	f.once.Do(func() {
		f.requests = make(chan func())
		ready := make(chan struct{})
		go f.serve(ready)
		<-ready
	})
	done := make(chan struct{})
	f.requests <- func() {
		defer close(done)
		start()
	}
	<-done
}

func (f *niceForker) serve(ready chan<- struct{}) {
	runtime.LockOSThread()
	// Raising a thread's own nice needs no privilege, so this fails only if
	// the supervisor itself already runs above f.nice and is not root (the
	// image starts it as root from init). Its children then keep the
	// supervisor's priority, which is the log's claim.
	if err := unix.Setpriority(unix.PRIO_PROCESS, unix.Gettid(), f.nice); err != nil {
		log.Printf("software background priority nice %d unavailable, background services keep the supervisor's priority: %v", f.nice, err)
	}
	close(ready)
	for start := range f.requests {
		start()
	}
}
