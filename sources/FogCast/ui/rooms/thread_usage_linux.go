//go:build linux

package rooms

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// readThreadUsage reads the calling OS thread's CPU time, switches, faults
// and block reads into u. The caller keeps its goroutine locked to that
// thread between samples.
func readThreadUsage(u *threadUsage) bool {
	var ts unix.Timespec
	if unix.ClockGettime(unix.CLOCK_THREAD_CPUTIME_ID, &ts) != nil {
		return false
	}
	var ru unix.Rusage
	if unix.Getrusage(unix.RUSAGE_THREAD, &ru) != nil {
		return false
	}
	u.cpu = time.Duration(ts.Nano())
	u.voluntary = int64(ru.Nvcsw)
	u.involuntary = int64(ru.Nivcsw)
	u.majorFaults = int64(ru.Majflt)
	u.minorFaults = int64(ru.Minflt)
	u.blocksRead = int64(ru.Inblock)
	return true
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
// os.File closes a file the Instance drops without Close. A read is one
// pread on its descriptor: os.File.ReadAt would pread again at the end of the
// short line to fill the buffer, which costs a second syscall for EOF.
type schedstatReader struct {
	tid int
	f   *os.File
	fd  int
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
			s.f, s.fd, s.tid = f, int(f.Fd()), tid
		}
		if n, err := unix.Pread(s.fd, s.buf[:], 0); err == nil && n > 0 {
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
