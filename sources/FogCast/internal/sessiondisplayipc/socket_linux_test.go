//go:build linux

package sessiondisplayipc

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

func testPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "fesipc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "display.sock")
}
func transition() Transition {
	return Transition{PackageID: strings.Repeat("a", 64), Generation: 7, TargetID: "kit-a", Display: protocol.SessionDisplayFocused}
}
func startLauncher(t *testing.T, path string, uid uint32, prepare func(context.Context, Transition) (func(context.Context, Outcome) error, error)) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	ready := make(chan error, 1)
	go func() { done <- ListenLauncher(ctx, path, uid, prepare, ready) }()
	select {
	case err := <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("listener not ready")
	}
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("listener: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Error("listener did not stop")
		}
	})
	return cancel
}
func TestTransitionRoundTripAndEOFUnknown(t *testing.T) {
	path := testPath(t)
	got := make(chan Outcome, 2)
	startLauncher(t, path, uint32(os.Geteuid()), func(_ context.Context, tr Transition) (func(context.Context, Outcome) error, error) {
		if tr != transition() {
			t.Errorf("transition = %+v", tr)
		}
		return func(_ context.Context, o Outcome) error { got <- o; return nil }, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	prepared, present, err := Prepare(ctx, path, transition())
	if err != nil || !present || prepared == nil {
		t.Fatalf("prepare: %v %v", present, err)
	}
	if err = prepared.Finish(ctx, Applied); err != nil {
		t.Fatal(err)
	}
	if o := <-got; o != Applied {
		t.Fatalf("outcome %v", o)
	}
	if err = prepared.Finish(ctx, Applied); err == nil {
		t.Fatal("duplicate finish accepted")
	}
	prepared, present, err = Prepare(ctx, path, transition())
	if err != nil || !present {
		t.Fatal(present, err)
	}
	_ = prepared.conn.Close()
	select {
	case o := <-got:
		if o != Unknown {
			t.Fatalf("lost peer = %v", o)
		}
	case <-ctx.Done():
		t.Fatal("lost peer did not finish")
	}
}
func TestAbsentStaleAndInvalid(t *testing.T) {
	path := testPath(t)
	ctx := context.Background()
	if p, present, err := Prepare(ctx, path, transition()); p != nil || present || err != nil {
		t.Fatalf("absent = %v %v %v", p, present, err)
	}
	bad := transition()
	bad.Generation = 0
	if _, _, err := Prepare(ctx, path, bad); err == nil {
		t.Fatal("invalid generation accepted")
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = l.Close()
	if _, present, err := Prepare(ctx, path, transition()); err == nil || present {
		t.Fatalf("stale must fail closed: present=%v err=%v", present, err)
	}
	ready := make(chan error, 1)
	if err = ListenLauncher(ctx, path, uint32(os.Geteuid()), func(context.Context, Transition) (func(context.Context, Outcome) error, error) { return nil, nil }, ready); err == nil {
		t.Fatal("replaced stale path")
	}
	if err = <-ready; err == nil {
		t.Fatal("stale path reported ready")
	}
}
func TestListenerRejectsWrongUID(t *testing.T) {
	path := testPath(t)
	startLauncher(t, path, uint32(os.Geteuid())+1, func(context.Context, Transition) (func(context.Context, Outcome) error, error) {
		t.Error("untrusted peer admitted")
		return nil, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, present, err := Prepare(ctx, path, transition()); err == nil || !present {
		t.Fatalf("UID mismatch = %v %v", present, err)
	}
}
func TestGuardSerializesPublicationAndHeadlessMutation(t *testing.T) {
	path := testPath(t)
	ctx := context.Background()
	guard, err := AcquireDisplayGuard(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	listenerCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	ready := make(chan error, 1)
	go func() {
		done <- ListenLauncher(listenerCtx, path, uint32(os.Geteuid()), func(context.Context, Transition) (func(context.Context, Outcome) error, error) {
			return func(context.Context, Outcome) error { return nil }, nil
		}, ready)
	}()
	time.Sleep(50 * time.Millisecond)
	select {
	case <-ready:
		t.Fatal("reported ready during headless mutation")
	default:
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("published during headless mutation: %v", err)
	}
	if _, present, err := Prepare(ctx, path, transition()); err != nil || present {
		t.Fatalf("guarded absent probe = %v %v", present, err)
	}
	if err := guard.Release(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("not ready after guard released")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("listener did not stop")
	}
}

func TestCancelledPreparedConnectionReportsUnknown(t *testing.T) {
	path := testPath(t)
	got := make(chan Outcome, 1)
	startLauncher(t, path, uint32(os.Geteuid()), func(context.Context, Transition) (func(context.Context, Outcome) error, error) {
		return func(_ context.Context, o Outcome) error { got <- o; return nil }, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	_, present, err := Prepare(ctx, path, transition())
	if err != nil || !present {
		t.Fatal(present, err)
	}
	cancel()
	select {
	case o := <-got:
		if o != Unknown {
			t.Fatalf("cancel outcome %v", o)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no cancellation observation")
	}
}

func TestGuardWaitHonorsCancellation(t *testing.T) {
	path := testPath(t)
	guard, err := AcquireDisplayGuard(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Release()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err = AcquireDisplayGuard(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("guard error %v", err)
	}
}

func TestOversizedFramesRejected(t *testing.T) {
	path := testPath(t)
	called := false
	startLauncher(t, path, uint32(os.Geteuid()), func(context.Context, Transition) (func(context.Context, Outcome) error, error) {
		called = true
		return nil, errors.New("unexpected")
	})
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], maxWire+1)
	if _, err = conn.Write(header[:]); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	_, _ = readWire(conn)
	_ = conn.Close()
	if called {
		t.Fatal("oversized frame reached callback")
	}
}

func TestListenerPermissionsAndUnsafeParent(t *testing.T) {
	path := testPath(t)
	startLauncher(t, path, uint32(os.Geteuid()), func(context.Context, Transition) (func(context.Context, Outcome) error, error) {
		return func(context.Context, Outcome) error { return nil }, nil
	})
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 {
		t.Fatalf("socket mode %v", info.Mode())
	}
	unsafePath := testPath(t)
	if err = os.Chmod(filepath.Dir(unsafePath), 0777); err != nil {
		t.Fatal(err)
	}
	if _, err = AcquireDisplayGuard(context.Background(), unsafePath); err == nil {
		t.Fatal("accepted world-writable socket directory")
	}
}

func TestReadinessReportsInitialFailure(t *testing.T) {
	path := testPath(t)
	ready := make(chan error, 1)
	if err := ListenLauncher(context.Background(), path, uint32(os.Geteuid()), nil, ready); err == nil {
		t.Fatal("nil callback accepted")
	}
	if err := <-ready; err == nil {
		t.Fatal("nil callback reported ready")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ready = make(chan error, 1)
	cb := func(context.Context, Transition) (func(context.Context, Outcome) error, error) { return nil, nil }
	if err := ListenLauncher(ctx, path, uint32(os.Geteuid()), cb, ready); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error %v", err)
	}
	select {
	case err := <-ready:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ready error %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled startup did not signal readiness")
	}
}
func TestCrashRestartReclaimsStaleSocket(t *testing.T) {
	if os.Getenv("FES_IPC_CRASH_CHILD") == "1" {
		path := os.Getenv("FES_IPC_CRASH_PATH")
		ready := make(chan error, 1)
		done := make(chan error, 1)
		go func() {
			done <- ListenLauncher(context.Background(), path, uint32(os.Geteuid()), func(context.Context, Transition) (func(context.Context, Outcome) error, error) {
				return func(context.Context, Outcome) error { return nil }, nil
			}, ready)
		}()
		if err := <-ready; err != nil {
			t.Fatal(err)
		}
		fmt.Fprintln(os.Stdout, "ready")
		<-done
		return
	}
	path := testPath(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashRestartReclaimsStaleSocket$")
	cmd.Env = append(os.Environ(), "FES_IPC_CRASH_CHILD=1", "FES_IPC_CRASH_PATH="+path)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "ready" {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("child readiness %q %v", line, err)
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if _, present, err := Prepare(context.Background(), path, transition()); err == nil || present {
		t.Fatalf("stale path was not rejected: present=%v err=%v", present, err)
	}
	startLauncher(t, path, uint32(os.Geteuid()), func(context.Context, Transition) (func(context.Context, Outcome) error, error) {
		return func(context.Context, Outcome) error { return nil }, nil
	})
	prepared, present, err := Prepare(context.Background(), path, transition())
	if err != nil || !present {
		t.Fatalf("restarted prepare: %v %v", present, err)
	}
	if err = prepared.Finish(context.Background(), Applied); err != nil {
		t.Fatal(err)
	}
}

func TestLiveListenerCannotBeReplaced(t *testing.T) {
	path := testPath(t)
	startLauncher(t, path, uint32(os.Geteuid()), func(context.Context, Transition) (func(context.Context, Outcome) error, error) {
		return func(context.Context, Outcome) error { return nil }, nil
	})
	ready := make(chan error, 1)
	err := ListenLauncher(context.Background(), path, uint32(os.Geteuid()), func(context.Context, Transition) (func(context.Context, Outcome) error, error) { return nil, nil }, ready)
	if err == nil {
		t.Fatal("replaced live listener")
	}
	if <-ready == nil {
		t.Fatal("reported duplicate listener ready")
	}
	prepared, present, err := Prepare(context.Background(), path, transition())
	if err != nil || !present {
		t.Fatalf("original listener lost: %v %v", present, err)
	}
	if err = prepared.Finish(context.Background(), Applied); err != nil {
		t.Fatal(err)
	}
}

func TestUnexpectedListenerFailureRemainsFailClosed(t *testing.T) {
	path := testPath(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan error, 1)
	opened := make(chan *net.UnixListener, 1)
	done := make(chan error, 1)
	opener := func(path string) (*net.UnixListener, os.FileInfo, error) {
		l, info, err := openListener(path)
		if err == nil {
			opened <- l
		}
		return l, info, err
	}
	go func() {
		done <- serveWithOpener(ctx, path, uint32(os.Geteuid()), true, ready, func(context.Context, net.Conn) {}, opener)
	}()
	listener := <-opened
	if err := <-ready; err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("unexpected close was reported as normal")
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("failed listener removed fail-closed socket: %v", err)
	}
	if _, present, err := Prepare(context.Background(), path, transition()); err == nil || present {
		t.Fatalf("failed live listener looked headless: present=%v err=%v", present, err)
	}
}
