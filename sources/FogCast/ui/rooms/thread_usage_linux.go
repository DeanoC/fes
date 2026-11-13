//go:build linux

package rooms

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// readThreadUsage samples the calling OS thread. The caller keeps its
// goroutine locked to that thread between samples.
func readThreadUsage(sched *schedstatReader) (threadUsage, bool) {
	var ts unix.Timespec
	if unix.ClockGettime(unix.CLOCK_THREAD_CPUTIME_ID, &ts) != nil {
		return threadUsage{}, false
	}
	var ru unix.Rusage
	if unix.Getrusage(unix.RUSAGE_THREAD, &ru) != nil {
		return threadUsage{}, false
	}
	u := threadUsage{
		cpu:         time.Duration(ts.Nano()),
		voluntary:   int64(ru.Nvcsw),
		involuntary: int64(ru.Nivcsw),
		majorFaults: int64(ru.Majflt),
		minorFaults: int64(ru.Minflt),
		blocksRead:  int64(ru.Inblock),
	}
	u.runDelay, u.haveRunDelay = sched.runDelay()
	return u, true
}

// schedstatReader reads the calling thread's run-queue wait from
// /proc/thread-self/schedstat ("<cpu ns> <run delay ns> <timeslices>"). The
// file needs CONFIG_SCHED_INFO; the locked DE10-Nano kernel (5.15.1-MiSTer)
// has it. Where it is missing or unreadable the field is omitted.
//
// Opening the file costs several times the rest of a sample, so the reader
// keeps it open for the thread that opened it and reopens only when a callback
// runs on another thread. A cached file whose thread has exited fails its read
// (even if the thread id is reused), and the reader opens the current one.
// os.File closes a file the Instance drops without Close.
type schedstatReader struct {
	tid int
	f   *os.File
	buf [96]byte
}

func (s *schedstatReader) runDelay() (time.Duration, bool) {
	tid := unix.Gettid()
	for attempt := 0; attempt < 2; attempt++ {
		if s.f == nil || s.tid != tid || attempt > 0 {
			s.close()
			f, err := os.Open("/proc/thread-self/schedstat")
			if err != nil {
				return 0, false
			}
			s.f, s.tid = f, tid
		}
		if n, _ := s.f.ReadAt(s.buf[:], 0); n > 0 {
			return parseRunDelay(s.buf[:n])
		}
	}
	return 0, false
}

func (s *schedstatReader) close() {
	if s.f != nil {
		s.f.Close()
		s.f = nil
	}
}

// parseRunDelay returns the second space-separated decimal field.
func parseRunDelay(b []byte) (time.Duration, bool) {
	i := 0
	for i < len(b) && b[i] != ' ' {
		i++
	}
	i++
	var v int64
	digits := 0
	for ; i < len(b) && b[i] >= '0' && b[i] <= '9'; i++ {
		v = v*10 + int64(b[i]-'0')
		digits++
	}
	if digits == 0 {
		return 0, false
	}
	return time.Duration(v), true
}
