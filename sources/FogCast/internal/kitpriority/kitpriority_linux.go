package kitpriority

import (
	"errors"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

// lowerThreads sets every thread of this process below nice to nice. It
// repeats until a pass changes no thread, so a thread created by a thread that
// was not yet lowered is caught by the next pass. It returns the first error.
func lowerThreads(nice int) error {
	var first error
	for changed := true; changed; {
		changed = false
		entries, err := os.ReadDir("/proc/self/task")
		if err != nil {
			return err
		}
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
		if first != nil {
			return first
		}
	}
	return nil
}
