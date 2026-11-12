package kitpriority

import (
	"errors"
	"os"
	"strconv"
	"time"

	"golang.org/x/sys/unix"
)

// settle is how long lowerThreads waits before its confirming pass. Linux
// copies nice into a new thread before the thread appears in /proc/self/task,
// so a thread that an unlowered thread was cloning during a pass can be
// missing from that pass and still carry the old nice. The wait lets such a
// clone finish so the confirming pass sees it.
const settle = 10 * time.Millisecond

// lowerThreads sets every thread of this process below nice to nice. It
// repeats until a pass that follows a settle wait changes no thread, so a
// thread created by a thread that was not yet lowered is caught by a later
// pass. It returns the first error.
func lowerThreads(nice int) error {
	for {
		changed, err := lowerPass(nice)
		if err != nil {
			return err
		}
		if changed {
			continue
		}
		time.Sleep(settle)
		changed, err = lowerPass(nice)
		if err != nil {
			return err
		}
		if !changed {
			return nil
		}
	}
}

// lowerPass lowers each thread listed in /proc/self/task that runs below
// nice. It reports whether it changed any thread and returns the first error.
func lowerPass(nice int) (bool, error) {
	entries, err := os.ReadDir("/proc/self/task")
	if err != nil {
		return false, err
	}
	changed := false
	var first error
	for _, entry := range entries {
		tid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		// The raw getpriority syscall returns 20 - nice.
		prio, err := unix.Getpriority(unix.PRIO_PROCESS, tid)
		if errors.Is(err, unix.ESRCH) {
			continue
		}
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		if 20-prio >= nice {
			continue
		}
		if err := unix.Setpriority(unix.PRIO_PROCESS, tid, nice); err != nil {
			if !errors.Is(err, unix.ESRCH) && first == nil {
				first = err
			}
			continue
		}
		changed = true
	}
	return changed, first
}
