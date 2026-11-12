//go:build linux

package rooms

import (
	"bytes"
	"io"
	"os"
	"strconv"
	"time"
)

// readHostContention snapshots this machine's /proc. Each call has its own
// reader, so the background baseline and a hard miss on the launcher thread
// never wait on each other.
func readHostContention() (contentionSnapshot, bool) {
	return (&procReader{root: "/proc"}).read()
}

// procReader reads load, system CPU and per-process CPU from a /proc tree.
// It is not safe for concurrent use.
type procReader struct {
	root string
	// now is the clock; nil means time.Now.
	now func() time.Time
	buf []byte
}

func (p *procReader) clock() time.Time {
	if p.now == nil {
		return time.Now()
	}
	return p.now()
}

// read takes a snapshot, stamped when the scan starts and ends. A process
// that exits mid-scan is skipped. ok is false only when loadavg is
// unreadable.
func (p *procReader) read() (contentionSnapshot, bool) {
	at := p.clock()
	load, err := p.file("loadavg")
	if err != nil {
		return contentionSnapshot{}, false
	}
	fields := bytes.Fields(load)
	snap := contentionSnapshot{at: at, loadavg: string(bytes.Join(fields[:min(4, len(fields))], []byte(" ")))}
	if up, err := p.file("uptime"); err == nil {
		snap.uptime, snap.haveUptime = parseUptime(up)
	}
	p.scan(&snap)
	snap.done = p.clock()
	return snap, true
}

// scan fills system CPU and per-process CPU into snap.
func (p *procReader) scan(snap *contentionSnapshot) {
	if stat, err := p.file("stat"); err == nil {
		snap.cpuTotal, snap.cpuIdle, snap.cpus, snap.haveCPU = parseProcStat(stat)
	}
	dir, err := os.Open(p.root)
	if err != nil {
		return
	}
	names, _ := dir.Readdirnames(-1)
	dir.Close()
	snap.procs = make(map[int]procTicks, len(names))
	for _, name := range names {
		pid, err := strconv.Atoi(name)
		if err != nil || pid <= 0 {
			continue
		}
		data, err := p.file(name + "/stat")
		if err != nil {
			continue
		}
		comm, ticks, start, ok := parsePidStat(data)
		if !ok {
			continue
		}
		snap.procs[pid] = procTicks{comm: string(comm), ticks: ticks, start: start}
	}
}

// file reads root/name into the reused buffer. The result is valid until the
// next call.
func (p *procReader) file(name string) ([]byte, error) {
	f, err := os.Open(p.root + "/" + name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if p.buf == nil {
		p.buf = make([]byte, 0, 4096)
	}
	p.buf = p.buf[:0]
	for {
		if len(p.buf) == cap(p.buf) {
			p.buf = append(p.buf, 0)[:len(p.buf)]
		}
		n, err := f.Read(p.buf[len(p.buf):cap(p.buf)])
		p.buf = p.buf[:len(p.buf)+n]
		if err == io.EOF {
			return p.buf, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// parseProcStat sums the aggregate "cpu" line of /proc/stat and counts the
// per-CPU lines. Idle is idle plus iowait.
func parseProcStat(data []byte) (total, idle uint64, cpus int, ok bool) {
	for line := range bytes.Lines(data) {
		f := bytes.Fields(line)
		if len(f) == 0 || !bytes.HasPrefix(f[0], []byte("cpu")) {
			continue
		}
		if len(f[0]) > 3 {
			cpus++
			continue
		}
		if len(f) < 6 {
			return 0, 0, 0, false
		}
		for i, v := range f[1:] {
			n, ok := parseUint(v)
			if !ok {
				return 0, 0, 0, false
			}
			total += n
			if i == 3 || i == 4 { // idle, iowait
				idle += n
			}
		}
		ok = true
	}
	return total, idle, cpus, ok
}

// parsePidStat reads comm, utime+stime and starttime from /proc/<pid>/stat.
// comm may contain spaces and parentheses, so the fields start after the last
// ')'.
func parsePidStat(data []byte) (comm []byte, ticks, start uint64, ok bool) {
	open, end := bytes.IndexByte(data, '('), bytes.LastIndexByte(data, ')')
	if open < 0 || end < open {
		return nil, 0, 0, false
	}
	// After comm come state (field 3) ... utime (14), stime (15) and
	// starttime (22).
	rest := data[end+1:]
	for field := 0; field <= 19; field++ {
		rest = bytes.TrimLeft(rest, " ")
		i := bytes.IndexByte(rest, ' ')
		if i < 0 {
			i = len(rest)
		}
		if i == 0 {
			return nil, 0, 0, false
		}
		if field == 11 || field == 12 || field == 19 {
			n, ok := parseUint(bytes.TrimRight(rest[:i], "\n"))
			if !ok {
				return nil, 0, 0, false
			}
			if field == 19 {
				start = n
			} else {
				ticks += n
			}
		}
		rest = rest[i:]
	}
	return data[open+1 : end], ticks, start, true
}

// parseUptime reads the first field of /proc/uptime, seconds since boot with
// a fraction.
func parseUptime(data []byte) (time.Duration, bool) {
	f := bytes.Fields(data)
	if len(f) == 0 {
		return 0, false
	}
	whole, frac, _ := bytes.Cut(f[0], []byte("."))
	secs, ok := parseUint(whole)
	if !ok {
		return 0, false
	}
	up := time.Duration(secs) * time.Second
	scale := time.Second
	for _, c := range frac {
		if c < '0' || c > '9' {
			return 0, false
		}
		scale /= 10
		up += time.Duration(c-'0') * scale
	}
	return up, true
}

func parseUint(b []byte) (uint64, bool) {
	if len(b) == 0 {
		return 0, false
	}
	var n uint64
	for _, c := range b {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + uint64(c-'0')
	}
	return n, true
}
