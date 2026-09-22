package misterruntime

import (
	"context"
	"errors"
	"fmt"

	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const idleResponse = `{"protocol":2,"ok":true,"state":"idle","execution":"none","system":null,"core":null,"error":null,"version":"git-test"}`

type socketFixture struct {
	path string
	done chan fixtureResult
}

type fixtureResult struct {
	request string
	err     error
}

func TestClientRejectsInvalidDevelopmentRBFPathsBeforeDial(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{name: "empty"},
		{name: "relative", path: "core.rbf"},
		{name: "unclean", path: "/tmp/fogcast-development/../core.rbf"},
		{name: "NUL", path: "/tmp/core\x00.rbf"},
		{name: "overlong", path: "/" + strings.Repeat("a", 4095)},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			client := NewClient(filepath.Join(t.TempDir(), "must-not-be-dialed.sock"))
			if _, err := client.Protocol2LoadDevelopmentRBF(context.Background(), testCase.path); !errors.Is(err, errInvalidRuntimeRequest) {
				t.Fatalf("LoadDevelopmentRBF error = %v, want invalid request", err)
			}
		})
	}
}

func TestClientRejectsWrongProtocolAndUnknownResponseFields(t *testing.T) {
	cases := []struct {
		name     string
		response string
	}{
		{
			name:     "wrong protocol",
			response: `{"protocol":2,"ok":true,"state":"idle","execution":"none","system":null,"core":null,"error":null,"version":"git-test"}` + "\n",
		},
		{
			name:     "unknown top-level field",
			response: `{"protocol":2,"ok":true,"state":"idle","execution":"none","system":null,"core":null,"error":null,"version":"git-test","extra":true}` + "\n",
		},
		{
			name:     "unknown error field",
			response: `{"protocol":2,"ok":true,"state":"idle","execution":"none","system":null,"core":null,"error":{"code":"busy","message":"busy","extra":true},"version":"git-test"}` + "\n",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newSocketFixture(t, testCase.response, true)
			if _, err := NewClient(fixture.path).Protocol2Status(context.Background()); err == nil {
				t.Fatal("invalid runtime response accepted")
			}
			if request := fixture.wait(t); request != `{"protocol":2,"operation":"status"}` {
				t.Fatalf("request = %q", request)
			}
		})
	}
}

func TestClientAcceptsExactly65536BytesAndRejects65537(t *testing.T) {
	fixture := newSocketFixture(t, responseLineAtLength(t, MaximumLineBytes), true)
	response, err := NewClient(fixture.path).Protocol2Status(context.Background())
	if err != nil {
		t.Fatalf("%d-byte response rejected: %v", MaximumLineBytes, err)
	}
	if response.Version == "" {
		t.Fatal("accepted response has an empty version")
	}
	if request := fixture.wait(t); request != `{"protocol":2,"operation":"status"}` {
		t.Fatalf("request = %q", request)
	}

	fixture = newSocketFixture(t, responseLineAtLength(t, MaximumLineBytes+1), true)
	if _, err := NewClient(fixture.path).Protocol2Status(context.Background()); err == nil || err.Error() != "runtime response exceeds 65536 bytes" {
		t.Fatalf("%d-byte response error = %v", MaximumLineBytes+1, err)
	}
	if request := fixture.wait(t); request != `{"protocol":2,"operation":"status"}` {
		t.Fatalf("request = %q", request)
	}
}

func TestClientRejectsMissingNewline(t *testing.T) {
	fixture := newSocketFixture(t, idleResponse, false)

	if _, err := NewClient(fixture.path).Protocol2Status(context.Background()); err == nil {
		t.Fatal("response without a newline accepted")
	}
	if request := fixture.wait(t); request != `{"protocol":2,"operation":"status"}` {
		t.Fatalf("request = %q", request)
	}
}

func TestClientHonorsContextDeadlineWithoutRetry(t *testing.T) {
	path := temporarySocketPath(t)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	done := make(chan fixtureResult, 1)
	go func() {
		result := fixtureResult{}
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			result.err = acceptErr
			done <- result
			return
		}
		defer connection.Close()

		result.request, result.err = readNewline(connection)
		if result.err != nil {
			done <- result
			return
		}
		time.Sleep(50 * time.Millisecond)
		_, _ = writeAll(connection, idleResponse+"\n")

		if err := listener.SetDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
			result.err = err
			done <- result
			return
		}
		second, acceptErr := listener.Accept()
		if acceptErr == nil {
			_ = second.Close()
			result.err = fmt.Errorf("accepted more than one connection")
		}
		done <- result
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := NewClient(path).Protocol2Status(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Status error = %v, want context deadline exceeded", err)
	}

	result := waitFixtureResult(t, done)
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.request != `{"protocol":2,"operation":"status"}` {
		t.Fatalf("request = %q", result.request)
	}
}

func TestClientHonorsContextCancellationAfterConnectWithoutRetry(t *testing.T) {
	path := temporarySocketPath(t)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	requestReceived := make(chan fixtureResult, 1)
	fixtureDone := make(chan fixtureResult, 1)
	go func() {
		result := fixtureResult{}
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			result.err = acceptErr
			requestReceived <- result
			fixtureDone <- result
			return
		}
		defer connection.Close()

		result.request, result.err = readNewline(connection)
		requestReceived <- result
		if result.err == nil {
			result.err = requireClientClose(connection)
		}
		if err := listener.SetDeadline(time.Now().Add(100 * time.Millisecond)); err != nil && result.err == nil {
			result.err = err
		}
		second, acceptErr := listener.Accept()
		if acceptErr == nil {
			_ = second.Close()
			if result.err == nil {
				result.err = fmt.Errorf("accepted more than one connection")
			}
		}
		fixtureDone <- result
	}()

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Hour))
	defer cancel()
	callDone := make(chan error, 1)
	go func() {
		_, err := NewClient(path).Protocol2Status(ctx)
		callDone <- err
	}()

	request := waitFixtureResult(t, requestReceived)
	if request.err != nil {
		t.Fatal(request.err)
	}
	if request.request != `{"protocol":2,"operation":"status"}` {
		t.Fatalf("request = %q", request.request)
	}

	cancelled := time.Now()
	cancel()
	select {
	case err := <-callDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Status error = %v, want context canceled", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("Status did not return promptly after context cancellation")
	}
	if elapsed := time.Since(cancelled); elapsed > 250*time.Millisecond {
		t.Fatalf("Status returned after %s", elapsed)
	}

	result := waitFixtureResult(t, fixtureDone)
	if result.err != nil {
		t.Fatal(result.err)
	}
}

func TestClientDoesNotReplaceCancellationDeadlineWithFutureContextDeadline(t *testing.T) {
	previousProcs := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previousProcs)

	path := temporarySocketPath(t)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	accepted := make(chan struct{}, 1)
	fixtureDone := make(chan fixtureResult, 1)
	go func() {
		result := fixtureResult{}
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			result.err = acceptErr
			fixtureDone <- result
			return
		}
		accepted <- struct{}{}
		defer connection.Close()

		result.request, result.err = readNewline(connection)
		if errors.Is(result.err, io.EOF) {
			result.err = nil
		} else if result.err == nil {
			if result.request != `{"protocol":2,"operation":"status"}` {
				result.err = fmt.Errorf("request = %q", result.request)
			} else {
				result.err = requireClientClose(connection)
			}
		}
		if err := listener.SetDeadline(time.Now().Add(100 * time.Millisecond)); err != nil && result.err == nil {
			result.err = err
		}
		second, acceptErr := listener.Accept()
		if acceptErr == nil {
			_ = second.Close()
			if result.err == nil {
				result.err = fmt.Errorf("accepted more than one connection")
			}
		}
		fixtureDone <- result
	}()

	base, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Hour))
	defer cancel()
	deadlineEntered := make(chan struct{}, 1)
	releaseDeadline := make(chan struct{})
	ctx := &deadlineGateContext{
		Context:         base,
		gateCall:        4,
		deadlineEntered: deadlineEntered,
		releaseDeadline: releaseDeadline,
	}
	callDone := make(chan error, 1)
	go func() {
		_, err := NewClient(path).Protocol2Status(ctx)
		callDone <- err
	}()

	waitSignal(t, accepted)
	waitSignal(t, deadlineEntered)
	cancel()
	for range 10 {
		runtime.Gosched()
	}
	close(releaseDeadline)

	select {
	case err := <-callDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Status error = %v, want context canceled", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("Status did not return promptly after cancellation with a future deadline")
	}

	result := waitFixtureResult(t, fixtureDone)
	if result.err != nil {
		t.Fatal(result.err)
	}
}

func newSocketFixture(t *testing.T, response string, waitForClientClose bool) *socketFixture {
	t.Helper()
	path := temporarySocketPath(t)
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	fixture := &socketFixture{path: path, done: make(chan fixtureResult, 1)}
	go func() {
		result := fixtureResult{}
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			result.err = acceptErr
			fixture.done <- result
			return
		}
		_ = listener.Close()
		defer connection.Close()

		result.request, result.err = readNewline(connection)
		if result.err == nil {
			_, result.err = writeAll(connection, response)
		}
		if result.err == nil && waitForClientClose {
			result.err = requireClientClose(connection)
		}
		fixture.done <- result
	}()
	return fixture
}

func (fixture *socketFixture) wait(t *testing.T) string {
	t.Helper()
	result := waitFixtureResult(t, fixture.done)
	if result.err != nil {
		t.Fatal(result.err)
	}
	return result.request
}

func waitFixtureResult(t *testing.T, done <-chan fixtureResult) fixtureResult {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(time.Second):
		t.Fatal("socket fixture did not finish")
		return fixtureResult{}
	}
}

func waitSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("socket fixture did not reach synchronization point")
	}
}

func temporarySocketPath(t *testing.T) string {
	t.Helper()
	directory, err := os.MkdirTemp(os.TempDir(), "fc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	return filepath.Join(directory, "runtime.sock")
}

func readNewline(connection net.Conn) (string, error) {
	line := make([]byte, 0, 256)
	var byteBuffer [1]byte
	for {
		count, err := connection.Read(byteBuffer[:])
		if count > 0 {
			if byteBuffer[0] == '\n' {
				return string(line), nil
			}
			line = append(line, byteBuffer[0])
		}
		if err != nil {
			return "", err
		}
	}
}

func writeAll(connection net.Conn, response string) (int, error) {
	bytes := []byte(response)
	written := 0
	for len(bytes) > 0 {
		count, err := connection.Write(bytes)
		written += count
		bytes = bytes[count:]
		if err != nil {
			return written, err
		}
		if count == 0 {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}

func requireClientClose(connection net.Conn) error {
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		return err
	}
	var byteBuffer [1]byte
	count, err := connection.Read(byteBuffer[:])
	if count != 0 {
		return fmt.Errorf("client wrote extra request data")
	}
	if !errors.Is(err, io.EOF) {
		return fmt.Errorf("client did not close connection: %w", err)
	}
	return nil
}

func responseLineAtLength(t *testing.T, length int) string {
	t.Helper()
	const prefix = `{"protocol":2,"ok":true,"state":"idle","execution":"none","system":null,"core":null,"error":null,"version":"`
	const suffix = `","capabilities":{"programming_profiles":[],"abis":[],"active_interfaces":[]},"active_package":null,"generation":null,"inspected_package":null}` + "\n"
	padding := length - len(prefix) - len(suffix)
	if padding < 1 {
		t.Fatalf("requested response line is too short: %d", length)
	}
	response := prefix + strings.Repeat("v", padding) + suffix
	if len(response) != length {
		t.Fatalf("response length = %d, want %d", len(response), length)
	}
	return response
}

type deadlineGateContext struct {
	context.Context
	mu              sync.Mutex
	calls           int
	gateCall        int
	deadlineEntered chan<- struct{}
	releaseDeadline <-chan struct{}
}

func (ctx *deadlineGateContext) Deadline() (time.Time, bool) {
	ctx.mu.Lock()
	ctx.calls++
	gate := ctx.calls == ctx.gateCall
	ctx.mu.Unlock()
	if gate {
		ctx.deadlineEntered <- struct{}{}
		<-ctx.releaseDeadline
	}
	return ctx.Context.Deadline()
}
