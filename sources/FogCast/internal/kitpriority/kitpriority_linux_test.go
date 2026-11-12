package kitpriority

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

const fixtureEnv = "KITPRIORITY_TEST_FIXTURE"

// TestBackgroundLowersEveryThread runs the fixture in a child process, since
// lowering nice cannot be undone without privilege.
func TestBackgroundLowersEveryThread(t *testing.T) {
	for _, tc := range []struct {
		name    string
		managed string
		nice    int
		procs   string
	}{
		{"managed", "1", Nice, "1"},
		{"unmanaged", "", 0, "skip"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if own := threadNices(t)[os.Getpid()]; own != 0 {
				t.Skipf("test process already runs at nice %d", own)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestBackgroundFixture$", "-test.v")
			cmd.Env = append(os.Environ(), fixtureEnv+"=1", managedEnv+"="+tc.managed, "GOMAXPROCS=")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("fixture: %v\n%s", err, out)
			}
			want := "nice=" + strconv.Itoa(tc.nice) + " procs=" + tc.procs
			if !strings.Contains(string(out), want) {
				t.Fatalf("fixture output lacks %q:\n%s", want, out)
			}
		})
	}
}

// TestBackgroundFixture is the child of TestBackgroundLowersEveryThread.
func TestBackgroundFixture(t *testing.T) {
	if os.Getenv(fixtureEnv) != "1" {
		t.Skip("fixture runs only as a child process")
	}
	var stop atomic.Bool
	defer stop.Store(true)
	// Busy goroutines locked to their own threads keep several threads alive
	// and running while Background walks them.
	started := make(chan struct{})
	for range 4 {
		go func() {
			runtime.LockOSThread()
			started <- struct{}{}
			for !stop.Load() {
			}
		}()
	}
	for range 4 {
		<-started
	}
	before := threadNices(t)

	Background()

	// Threads created after the call must inherit the lowered nice. Each
	// locked goroutine holds its own thread, so more of them than threads
	// that exist now forces the runtime to create new ones.
	release := make(chan struct{})
	defer close(release)
	locked := make(chan struct{})
	for range len(before) + 2 {
		go func() {
			runtime.LockOSThread()
			locked <- struct{}{}
			<-release
		}()
	}
	for range len(before) + 2 {
		<-locked
	}

	nices := threadNices(t)
	created := 0
	for tid := range nices {
		if _, ok := before[tid]; !ok {
			created++
		}
	}
	if created == 0 {
		t.Fatalf("no thread was created after Background: %v", nices)
	}
	nice := nices[os.Getpid()]
	for tid, n := range nices {
		if n != nice {
			t.Fatalf("thread %d at nice %d, main thread at %d: %v", tid, n, nice, nices)
		}
	}
	procs := "skip"
	if managed(os.Getenv) {
		procs = strconv.Itoa(runtime.GOMAXPROCS(0))
	}
	t.Logf("nice=%d procs=%s threads=%d created=%d", nice, procs, len(nices), created)
}

// threadNices returns the nice field of each /proc/self/task/*/stat by
// thread ID.
func threadNices(t *testing.T) map[int]int {
	t.Helper()
	stats, err := filepath.Glob("/proc/self/task/*/stat")
	if err != nil || len(stats) == 0 {
		t.Fatalf("list threads: %v", err)
	}
	nices := make(map[int]int, len(stats))
	for _, path := range stats {
		tid, err := strconv.Atoi(filepath.Base(filepath.Dir(path)))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		// nice is field 19, the 17th after the parenthesised command.
		fields := strings.Fields(string(data[strings.LastIndexByte(string(data), ')')+1:]))
		n, err := strconv.Atoi(fields[16])
		if err != nil {
			t.Fatalf("%s: nice %q: %v", path, fields[16], err)
		}
		nices[tid] = n
	}
	return nices
}
