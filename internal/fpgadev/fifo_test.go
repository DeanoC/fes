package fpgadev

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func makeTestFIFO(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("FIFO dispatch is a Linux adapter")
	}
	path := filepath.Join(t.TempDir(), "MiSTer_cmd")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	return path
}

func openTestFIFOReader(t *testing.T, path string) int {
	t.Helper()
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("open fifo reader: %v", err)
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	return fd
}

func readFIFOAfterDispatch(t *testing.T, fd int) []byte {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		buf := make([]byte, 256)
		n, err := unix.Read(fd, buf)
		if err == nil {
			return buf[:n]
		}
		if err != unix.EAGAIN && err != unix.EWOULDBLOCK {
			t.Fatalf("read fifo: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for FIFO bytes")
	return nil
}

func testFIFO(path string) FIFO {
	return FIFO{Path: path, ExpectedUID: uint32(os.Getuid()), PollInterval: time.Millisecond}
}

func TestFIFODispatchNoReaderCancellationLeavesNoLateWrite(t *testing.T) {
	path := makeTestFIFO(t)
	fifo := testFIFO(path)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(25*time.Millisecond, cancel)

	attempt, err := fifo.Dispatch(ctx, "load_core /tmp/top.rbf\n")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Dispatch error = %v, want context.Canceled", err)
	}
	if attempt != NotInvoked {
		t.Fatalf("Dispatch attempt = %v, want NotInvoked", attempt)
	}

	reader := openTestFIFOReader(t, path)
	// A late reader must not observe a write from a detached or delayed
	// operation after Dispatch has returned.
	deadline := time.Now().Add(75 * time.Millisecond)
	for time.Now().Before(deadline) {
		buf := make([]byte, 128)
		n, readErr := unix.Read(reader, buf)
		if n != 0 {
			t.Fatalf("late FIFO read = n:%d err:%v; want no bytes", n, readErr)
		}
		if readErr != nil && readErr != unix.EAGAIN && readErr != unix.EWOULDBLOCK && readErr != io.EOF {
			t.Fatalf("late FIFO read error = %v", readErr)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestFIFODispatchWritesExactSingleLineAndCompletes(t *testing.T) {
	path := makeTestFIFO(t)
	reader := openTestFIFOReader(t, path)
	fifo := testFIFO(path)

	attempt, err := fifo.Dispatch(context.Background(), "load_core /tmp/top.rbf\n")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if attempt != Completed {
		t.Fatalf("Dispatch attempt = %v, want Completed", attempt)
	}
	if got := string(readFIFOAfterDispatch(t, reader)); got != "load_core /tmp/top.rbf\n" {
		t.Fatalf("FIFO command = %q", got)
	}
}

func TestFIFODispatchRejectsCommandAmbiguityBeforeOpen(t *testing.T) {
	path := makeTestFIFO(t)
	fifo := testFIFO(path)
	var opens atomic.Int32
	fifo.ops = &fifoOperations{open: func(string, int, uint32) (int, error) {
		opens.Add(1)
		return -1, errors.New("open must not run")
	}}

	for _, command := range []string{
		"",
		"load_core /tmp/top.rbf",
		"load_core /tmp/top.rbf\n\n",
		"load_core /tmp/top.rbf\r\n",
		"load_core /tmp/top\x00.rbf\n",
		"load_core /tmp/top\trbf\n",
		"load_core /tmp/top\u0085rbf\n",
		"load_core /tmp/top\xffrbf\n",
	} {
		attempt, err := fifo.Dispatch(context.Background(), command)
		if err == nil {
			t.Fatalf("command %q was accepted", command)
		}
		if attempt != NotInvoked {
			t.Fatalf("command %q attempt = %v, want NotInvoked", command, attempt)
		}
	}
	if opens.Load() != 0 {
		t.Fatalf("open called %d times for invalid commands", opens.Load())
	}
}

func TestFIFODispatchRejectsFIFOTypeModeOwnerAndSymlink(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("FIFO dispatch is a Linux adapter")
	}
	t.Run("regular file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "MiSTer_cmd")
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		attempt, err := testFIFO(path).Dispatch(context.Background(), "load_core /tmp/top.rbf\n")
		if err == nil || attempt != NotInvoked {
			t.Fatalf("regular file Dispatch = %v, %v", attempt, err)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		path := makeTestFIFO(t)
		link := filepath.Join(t.TempDir(), "MiSTer_cmd")
		if err := os.Symlink(path, link); err != nil {
			t.Fatal(err)
		}
		attempt, err := testFIFO(link).Dispatch(context.Background(), "load_core /tmp/top.rbf\n")
		if err == nil || attempt != NotInvoked {
			t.Fatalf("symlink Dispatch = %v, %v", attempt, err)
		}
	})
	t.Run("ancestor symlink", func(t *testing.T) {
		realDir := t.TempDir()
		realPath := filepath.Join(realDir, "MiSTer_cmd")
		if err := unix.Mkfifo(realPath, 0o600); err != nil {
			t.Fatal(err)
		}
		aliasParent := filepath.Join(t.TempDir(), "staging")
		if err := os.Symlink(realDir, aliasParent); err != nil {
			t.Fatal(err)
		}
		attempt, err := testFIFO(filepath.Join(aliasParent, "MiSTer_cmd")).Dispatch(context.Background(), "load_core /tmp/top.rbf\n")
		if err == nil || attempt != NotInvoked {
			t.Fatalf("ancestor symlink Dispatch = %v, %v", attempt, err)
		}
	})
	t.Run("mode", func(t *testing.T) {
		path := makeTestFIFO(t)
		if err := os.Chmod(path, 0o640); err != nil {
			t.Fatal(err)
		}
		attempt, err := testFIFO(path).Dispatch(context.Background(), "load_core /tmp/top.rbf\n")
		if err == nil || attempt != NotInvoked {
			t.Fatalf("wrong mode Dispatch = %v, %v", attempt, err)
		}
	})
	t.Run("owner", func(t *testing.T) {
		path := makeTestFIFO(t)
		wrong := uint32(os.Getuid() + 1)
		attempt, err := (FIFO{Path: path, ExpectedUID: wrong}).Dispatch(context.Background(), "load_core /tmp/top.rbf\n")
		if err == nil || attempt != NotInvoked {
			t.Fatalf("wrong owner Dispatch = %v, %v", attempt, err)
		}
	})
}

func TestFIFODispatchClassifiesOpenPollWriteShortAndCloseErrors(t *testing.T) {
	path := makeTestFIFO(t)
	const command = "load_core /tmp/top.rbf\n"
	openErr := errors.New("open failure")
	pollErr := errors.New("poll failure")
	writeErr := errors.New("write failure")
	closeErr := errors.New("close failure")

	t.Run("open", func(t *testing.T) {
		fifo := testFIFO(path)
		fifo.ops = &fifoOperations{open: func(string, int, uint32) (int, error) { return -1, openErr }}
		attempt, err := fifo.Dispatch(context.Background(), command)
		if !errors.Is(err, openErr) || attempt != NotInvoked {
			t.Fatalf("open result = %v, %v", attempt, err)
		}
	})
	t.Run("poll", func(t *testing.T) {
		fifo := testFIFO(path)
		fifo.ops = &fifoOperations{
			open: func(string, int, uint32) (int, error) { return -1, unix.ENXIO },
			poll: func([]unix.PollFd, int) (int, error) { return 0, pollErr },
		}
		attempt, err := fifo.Dispatch(context.Background(), command)
		if !errors.Is(err, pollErr) || attempt != NotInvoked {
			t.Fatalf("poll result = %v, %v", attempt, err)
		}
	})
	t.Run("write", func(t *testing.T) {
		fifo := testFIFO(path)
		fd := openTestFIFOReader(t, path)
		closed := false
		fifo.ops = &fifoOperations{
			open:  func(string, int, uint32) (int, error) { return fd, nil },
			write: func(int, []byte) (int, error) { return 0, writeErr },
			close: func(int) error { closed = true; return nil },
		}
		attempt, err := fifo.Dispatch(context.Background(), command)
		if !errors.Is(err, writeErr) || attempt != Invoked || !closed {
			t.Fatalf("write result = %v, %v, closed:%v", attempt, err, closed)
		}
	})
	t.Run("short write", func(t *testing.T) {
		fifo := testFIFO(path)
		fd := openTestFIFOReader(t, path)
		fifo.ops = &fifoOperations{
			open:  func(string, int, uint32) (int, error) { return fd, nil },
			write: func(int, []byte) (int, error) { return len(command) - 1, nil },
			close: func(int) error { return nil },
		}
		attempt, err := fifo.Dispatch(context.Background(), command)
		if !errors.Is(err, io.ErrShortWrite) || attempt != Invoked {
			t.Fatalf("short-write result = %v, %v", attempt, err)
		}
	})
	t.Run("close", func(t *testing.T) {
		fifo := testFIFO(path)
		fd := openTestFIFOReader(t, path)
		fifo.ops = &fifoOperations{
			open:  func(string, int, uint32) (int, error) { return fd, nil },
			write: func(int, []byte) (int, error) { return len(command), nil },
			close: func(int) error { return closeErr },
		}
		attempt, err := fifo.Dispatch(context.Background(), command)
		if !errors.Is(err, closeErr) || attempt != Invoked {
			t.Fatalf("close result = %v, %v", attempt, err)
		}
	})
}

func TestFIFODispatchCancellationAfterOpenIsPotentiallyConsumed(t *testing.T) {
	path := makeTestFIFO(t)
	fifo := testFIFO(path)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fd := openTestFIFOReader(t, path)
	closed := false
	fifo.ops = &fifoOperations{
		open: func(string, int, uint32) (int, error) {
			cancel()
			return fd, nil
		},
		close: func(int) error { closed = true; return nil },
	}
	attempt, err := fifo.Dispatch(ctx, "load_core /tmp/top.rbf\n")
	if !errors.Is(err, context.Canceled) || attempt != Invoked || !closed {
		t.Fatalf("post-open cancellation = %v, %v, closed:%v", attempt, err, closed)
	}
}

func TestFIFODispatchRetriesWriteEAGAINAfterPollout(t *testing.T) {
	path := makeTestFIFO(t)
	fd := openTestFIFOReader(t, path)
	const command = "load_core /tmp/top.rbf\n"
	var writes atomic.Int32
	var polls atomic.Int32
	fifo := testFIFO(path)
	fifo.ops = &fifoOperations{
		open: func(string, int, uint32) (int, error) { return fd, nil },
		write: func(int, []byte) (int, error) {
			if writes.Add(1) == 1 {
				return 0, unix.EAGAIN
			}
			return len(command), nil
		},
		poll: func(fds []unix.PollFd, _ int) (int, error) {
			polls.Add(1)
			if len(fds) != 1 || fds[0].Events != unix.POLLOUT {
				return 0, errors.New("poll did not wait for POLLOUT")
			}
			fds[0].Revents = unix.POLLOUT
			return 1, nil
		},
	}
	attempt, err := fifo.Dispatch(context.Background(), command)
	if err != nil || attempt != Completed {
		t.Fatalf("EAGAIN/POLLOUT dispatch = %v, %v", attempt, err)
	}
	if writes.Load() != 2 || polls.Load() != 1 {
		t.Fatalf("write/poll calls = %d/%d, want 2/1", writes.Load(), polls.Load())
	}
}

func TestFIFODispatchClassifiesPollErrorEventsAsInvoked(t *testing.T) {
	for _, event := range []struct {
		name string
		bits int16
	}{
		{name: "error", bits: unix.POLLERR},
		{name: "hangup", bits: unix.POLLHUP},
		{name: "invalid", bits: unix.POLLNVAL},
	} {
		t.Run(event.name, func(t *testing.T) {
			path := makeTestFIFO(t)
			fd := openTestFIFOReader(t, path)
			fifo := testFIFO(path)
			fifo.ops = &fifoOperations{
				open:  func(string, int, uint32) (int, error) { return fd, nil },
				write: func(int, []byte) (int, error) { return 0, unix.EAGAIN },
				poll: func(fds []unix.PollFd, _ int) (int, error) {
					fds[0].Revents = event.bits
					return 1, nil
				},
			}
			attempt, err := fifo.Dispatch(context.Background(), "load_core /tmp/top.rbf\n")
			if err == nil || attempt != Invoked {
				t.Fatalf("poll event dispatch = %v, %v; want Invoked/error", attempt, err)
			}
		})
	}
}

func TestFIFODispatchHandlesEINTRAtOpenWriteAndPoll(t *testing.T) {
	path := makeTestFIFO(t)
	fd := openTestFIFOReader(t, path)
	const command = "load_core /tmp/top.rbf\n"
	var opens, writes, polls atomic.Int32
	fifo := testFIFO(path)
	fifo.ops = &fifoOperations{
		open: func(string, int, uint32) (int, error) {
			if opens.Add(1) == 1 {
				return -1, unix.EINTR
			}
			return fd, nil
		},
		write: func(int, []byte) (int, error) {
			switch writes.Add(1) {
			case 1:
				return 0, unix.EINTR
			case 2:
				return 0, unix.EAGAIN
			}
			return len(command), nil
		},
		poll: func([]unix.PollFd, int) (int, error) {
			if polls.Add(1) == 1 {
				return 0, unix.EINTR
			}
			return 0, nil
		},
	}
	attempt, err := fifo.Dispatch(context.Background(), command)
	if err != nil || attempt != Completed {
		t.Fatalf("EINTR dispatch = %v, %v", attempt, err)
	}
	if opens.Load() != 2 || writes.Load() != 3 {
		t.Fatalf("open/write calls = %d/%d, want 2/3", opens.Load(), writes.Load())
	}
	if polls.Load() != 2 {
		t.Fatalf("poll calls = %d, want two calls including EINTR retry", polls.Load())
	}
}

func TestFIFODispatchCancellationDuringWritePollIsInvoked(t *testing.T) {
	path := makeTestFIFO(t)
	fd := openTestFIFOReader(t, path)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fifo := testFIFO(path)
	fifo.ops = &fifoOperations{
		open:  func(string, int, uint32) (int, error) { return fd, nil },
		write: func(int, []byte) (int, error) { return 0, unix.EAGAIN },
		poll: func([]unix.PollFd, int) (int, error) {
			cancel()
			return 0, nil
		},
	}
	attempt, err := fifo.Dispatch(ctx, "load_core /tmp/top.rbf\n")
	if !errors.Is(err, context.Canceled) || attempt != Invoked {
		t.Fatalf("write-poll cancellation = %v, %v; want Invoked/canceled", attempt, err)
	}
}

func TestFIFODispatchDescriptorReplacementCannotReceiveBytes(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("openat2 descriptor-boundary adapter")
	}
	path := makeTestFIFO(t)
	const command = "load_core /tmp/top.rbf\n"
	var writes atomic.Int32
	var closed atomic.Int32
	var statCalls atomic.Int32
	fifo := testFIFO(path)
	identity := fifoDescriptor{Device: 11, Inode: 22, Mode: unix.S_IFIFO | 0o600, UID: uint32(os.Getuid())}
	fifo.ops = &fifoOperations{
		openRoot:   func() (int, error) { return 100, nil },
		openParent: func(int, string) (int, error) { return 101, nil },
		bindFinal:  func(int, string) (int, error) { return 102, nil },
		openFinal:  func(int, string, int, uint32) (int, error) { return 103, nil },
		fstat: func(fd int) (fifoDescriptor, error) {
			statCalls.Add(1)
			if fd == 103 {
				return fifoDescriptor{Device: 11, Inode: 23, Mode: unix.S_IFIFO | 0o600, UID: uint32(os.Getuid())}, nil
			}
			return identity, nil
		},
		write: func(int, []byte) (int, error) {
			writes.Add(1)
			return len(command), nil
		},
		close: func(int) error {
			closed.Add(1)
			return nil
		},
	}
	attempt, err := fifo.Dispatch(context.Background(), command)
	if err == nil || attempt != Invoked {
		t.Fatalf("replacement dispatch = %v, %v; want Invoked/error", attempt, err)
	}
	if writes.Load() != 0 {
		t.Fatalf("hostile replacement received %d writes", writes.Load())
	}
	if statCalls.Load() != 2 || closed.Load() != 4 {
		t.Fatalf("fstat/close calls = %d/%d, want 2/4", statCalls.Load(), closed.Load())
	}
}

func TestFIFODispatchActualBoundarySymlinkAndQualifyingReplacementReceiveNoBytes(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("openat2 descriptor-boundary adapter")
	}
	t.Run("ancestor symlink", func(t *testing.T) {
		realDir := t.TempDir()
		realPath := filepath.Join(realDir, "MiSTer_cmd")
		if err := unix.Mkfifo(realPath, 0o600); err != nil {
			t.Fatal(err)
		}
		reader := openTestFIFOReader(t, realPath)
		aliasParent := filepath.Join(t.TempDir(), "alias")
		if err := os.Symlink(realDir, aliasParent); err != nil {
			t.Fatal(err)
		}
		attempt, err := testFIFO(filepath.Join(aliasParent, "MiSTer_cmd")).Dispatch(context.Background(), "load_core /tmp/top.rbf\n")
		if err == nil || attempt != NotInvoked {
			t.Fatalf("ancestor symlink dispatch = %v, %v; want NotInvoked/error", attempt, err)
		}
		if buf := readFIFOWithoutWaiting(t, reader); len(buf) != 0 {
			t.Fatalf("ancestor symlink received %q", buf)
		}
	})

	t.Run("qualifying fifo replacement", func(t *testing.T) {
		path := makeTestFIFO(t)
		var hostileReader int = -1
		fifo := testFIFO(path)
		fifo.ops = &fifoOperations{
			openFinal: func(parentFD int, leaf string, flags int, mode uint32) (int, error) {
				if err := os.Remove(path); err != nil {
					return -1, err
				}
				if err := unix.Mkfifo(path, 0o600); err != nil {
					return -1, err
				}
				var err error
				hostileReader, err = unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
				if err != nil {
					return -1, err
				}
				return fifoDefaultOps.openFinal(parentFD, leaf, flags, mode)
			},
		}
		attempt, err := fifo.Dispatch(context.Background(), "load_core /tmp/top.rbf\n")
		if hostileReader >= 0 {
			defer unix.Close(hostileReader)
		}
		if err == nil || attempt != Invoked {
			t.Fatalf("qualifying replacement dispatch = %v, %v; want Invoked/error", attempt, err)
		}
		if hostileReader < 0 {
			t.Fatal("replacement reader was not opened")
		}
		if buf := readFIFOWithoutWaiting(t, hostileReader); len(buf) != 0 {
			t.Fatalf("qualifying replacement received %q", buf)
		}
	})
}

func readFIFOWithoutWaiting(t *testing.T, fd int) []byte {
	t.Helper()
	buf := make([]byte, 256)
	n, err := unix.Read(fd, buf)
	if err != nil && err != unix.EAGAIN && err != unix.EWOULDBLOCK {
		t.Fatalf("read fifo without waiting: %v", err)
	}
	return buf[:n]
}

func TestFIFODispatchRealRootBindingDoesNotLeakDescriptors(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("openat2 descriptor-boundary adapter")
	}
	path := makeTestFIFO(t)
	before := countOpenDescriptors(t)
	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Millisecond)
		attempt, err := testFIFO(path).Dispatch(ctx, "load_core /tmp/top.rbf\n")
		cancel()
		if err == nil || attempt != NotInvoked {
			t.Fatalf("no-reader dispatch %d = %v, %v; want NotInvoked/error", i, attempt, err)
		}
	}
	after := countOpenDescriptors(t)
	if after != before {
		t.Fatalf("open descriptor count changed from %d to %d", before, after)
	}
}

func TestFIFODispatchKeepsBindDescriptorUntilWriterRevalidation(t *testing.T) {
	path := makeTestFIFO(t)
	const command = "load_core /tmp/top.rbf\n"
	identity := fifoDescriptor{Device: 11, Inode: 22, Mode: unix.S_IFIFO | 0o600, UID: uint32(os.Getuid())}
	var order []string
	var writes atomic.Int32
	fifo := testFIFO(path)
	fifo.ops = &fifoOperations{
		openRoot:   func() (int, error) { order = append(order, "root-open"); return 100, nil },
		openParent: func(int, string) (int, error) { order = append(order, "parent-open"); return 101, nil },
		bindFinal:  func(int, string) (int, error) { order = append(order, "bind-open"); return 102, nil },
		openFinal: func(int, string, int, uint32) (int, error) {
			order = append(order, "writer-open")
			return 103, nil
		},
		fstat: func(fd int) (fifoDescriptor, error) {
			if fd == 102 {
				order = append(order, "bind-fstat")
			} else {
				order = append(order, "writer-fstat")
			}
			return identity, nil
		},
		write: func(int, []byte) (int, error) {
			writes.Add(1)
			order = append(order, "write")
			return len(command), nil
		},
		close: func(fd int) error {
			switch fd {
			case 100:
				order = append(order, "root-close")
			case 101:
				order = append(order, "parent-close")
			case 102:
				order = append(order, "bind-close")
			case 103:
				order = append(order, "writer-close")
			}
			return nil
		},
	}
	attempt, err := fifo.Dispatch(context.Background(), command)
	if err != nil || attempt != Completed {
		t.Fatalf("descriptor lifetime dispatch = %v, %v", attempt, err)
	}
	if writes.Load() != 1 {
		t.Fatalf("writes = %d, want one", writes.Load())
	}
	bindClose := indexOf(order, "bind-close")
	writerOpen := indexOf(order, "writer-open")
	writerFstat := indexOf(order, "writer-fstat")
	if bindClose < writerOpen || bindClose < writerFstat {
		t.Fatalf("bind closed before writer verification: %v", order)
	}
	if got := countEvents(order, "root-close", "parent-close", "bind-close", "writer-close"); got != 4 {
		t.Fatalf("close event count = %d, want four: %v", got, order)
	}
}

func TestFIFODispatchReplacementDuringENXIORetryCannotWriteOrReuseBinding(t *testing.T) {
	path := makeTestFIFO(t)
	const command = "load_core /tmp/top.rbf\n"
	bound := fifoDescriptor{Device: 11, Inode: 22, Mode: unix.S_IFIFO | 0o600, UID: uint32(os.Getuid())}
	replaced := fifoDescriptor{Device: 11, Inode: 23, Mode: unix.S_IFIFO | 0o600, UID: uint32(os.Getuid())}
	var openCalls, writes atomic.Int32
	var closeCounts = map[int]int{}
	fifo := testFIFO(path)
	fifo.ops = &fifoOperations{
		openRoot:   func() (int, error) { return 100, nil },
		openParent: func(int, string) (int, error) { return 101, nil },
		bindFinal:  func(int, string) (int, error) { return 102, nil },
		openFinal: func(int, string, int, uint32) (int, error) {
			if openCalls.Add(1) == 1 {
				return -1, unix.ENXIO
			}
			return 103, nil
		},
		poll: func([]unix.PollFd, int) (int, error) { return 0, nil },
		fstat: func(fd int) (fifoDescriptor, error) {
			if fd == 102 {
				return bound, nil
			}
			return replaced, nil
		},
		write: func(int, []byte) (int, error) {
			writes.Add(1)
			return len(command), nil
		},
		close: func(fd int) error {
			closeCounts[fd]++
			return nil
		},
	}
	attempt, err := fifo.Dispatch(context.Background(), command)
	if err == nil || attempt != Invoked {
		t.Fatalf("replacement-after-ENXIO dispatch = %v, %v; want Invoked/error", attempt, err)
	}
	if writes.Load() != 0 {
		t.Fatalf("replacement received %d writes", writes.Load())
	}
	if openCalls.Load() != 2 {
		t.Fatalf("writer opens = %d, want retry", openCalls.Load())
	}
	for _, fd := range []int{100, 101, 102, 103} {
		if closeCounts[fd] != 1 {
			t.Fatalf("fd %d close count = %d, want one (%v)", fd, closeCounts[fd], closeCounts)
		}
	}
}

func indexOf(values []string, want string) int {
	for index, value := range values {
		if value == want {
			return index
		}
	}
	return -1
}

func countEvents(values []string, wants ...string) int {
	count := 0
	for _, value := range values {
		for _, want := range wants {
			if value == want {
				count++
				break
			}
		}
	}
	return count
}

func countOpenDescriptors(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}
