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

const idleResponse = `{"protocol":1,"ok":true,"state":"idle","execution":"none","system":null,"core":null,"error":null,"version":"git-test"}`

type socketFixture struct {
	path string
	done chan fixtureResult
}

type fixtureResult struct {
	request string
	err     error
}

func TestClientStatusUsesOneProtocolRequestAndCloses(t *testing.T) {
	fixture := newSocketFixture(t, idleResponse+"\n", true)

	response, err := NewClient(fixture.path).Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if response != (Response{Protocol: 1, OK: true, State: "idle", Execution: "none", Version: "git-test"}) {
		t.Fatalf("response = %#v", response)
	}
	if request := fixture.wait(t); request != `{"protocol":1,"operation":"status"}` {
		t.Fatalf("request = %q", request)
	}
}

func TestClientStopUsesOnlyTheStopOperation(t *testing.T) {
	fixture := newSocketFixture(t, idleResponse+"\n", true)

	response, err := NewClient(fixture.path).Stop(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if response.State != "idle" || response.Execution != "none" {
		t.Fatalf("response = %#v", response)
	}
	if request := fixture.wait(t); request != `{"protocol":1,"operation":"stop"}` {
		t.Fatalf("request = %q", request)
	}
}

func TestClientLaunchUsesTheExactTypedMegaDriveRequest(t *testing.T) {
	const response = `{"protocol":1,"ok":true,"state":"running_game","execution":"game","system":"megadrive","core":"MegaDrive","error":null,"version":"git-test"}`
	fixture := newSocketFixture(t, response+"\n", true)
	request := LaunchRequest{
		System: "megadrive",
		RBF:    "/usr/share/mister-runtime/cores/megadrive.rbf",
		Media: map[string]string{
			"cartridge": "/media/fat/fogcast/cache/sonic2.bin",
		},
		Settings: map[string]string{},
	}

	result, err := NewClient(fixture.path).Launch(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "running_game" || result.Execution != "game" ||
		result.System == nil || *result.System != "megadrive" ||
		result.Core == nil || *result.Core != "MegaDrive" {
		t.Fatalf("response = %#v", result)
	}
	if got := fixture.wait(t); got != `{"protocol":1,"operation":"launch","system":"megadrive","rbf":"/usr/share/mister-runtime/cores/megadrive.rbf","media":{"cartridge":"/media/fat/fogcast/cache/sonic2.bin"},"settings":{}}` {
		t.Fatalf("request = %q", got)
	}
}

func TestClientLoadDevelopmentRBFUsesTheExactTypedRequest(t *testing.T) {
	const response = `{"protocol":1,"ok":true,"state":"running_development","execution":"development","system":null,"core":"MegaDrive","error":null,"version":"git-test"}`
	fixture := newSocketFixture(t, response+"\n", true)

	result, err := NewClient(fixture.path).LoadDevelopmentRBF(
		context.Background(), "/tmp/fogcast-development/core.rbf",
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "running_development" || result.Execution != "development" ||
		result.System != nil || result.Core == nil || *result.Core != "MegaDrive" {
		t.Fatalf("response = %#v", result)
	}
	if got := fixture.wait(t); got != `{"protocol":1,"operation":"load_development_rbf","rbf":"/tmp/fogcast-development/core.rbf"}` {
		t.Fatalf("request = %q", got)
	}
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
			if _, err := client.LoadDevelopmentRBF(context.Background(), testCase.path); !errors.Is(err, errInvalidRuntimeRequest) {
				t.Fatalf("LoadDevelopmentRBF error = %v, want invalid request", err)
			}
		})
	}
}

func TestClientRequiresDevelopmentResponseIdentity(t *testing.T) {
	cases := []struct {
		name     string
		response string
		valid    bool
	}{
		{
			name:     "unobserved core",
			response: `{"protocol":1,"ok":true,"state":"running_development","execution":"development","system":null,"core":null,"error":null,"version":"git-test"}` + "\n",
			valid:    true,
		},
		{
			name:     "observed core",
			response: `{"protocol":1,"ok":true,"state":"running_development","execution":"development","system":null,"core":"MegaDrive","error":null,"version":"git-test"}` + "\n",
			valid:    true,
		},
		{
			name:     "system identity",
			response: `{"protocol":1,"ok":true,"state":"running_development","execution":"development","system":"megadrive","core":"MegaDrive","error":null,"version":"git-test"}` + "\n",
		},
		{
			name:     "empty observed core",
			response: `{"protocol":1,"ok":true,"state":"running_development","execution":"development","system":null,"core":"","error":null,"version":"git-test"}` + "\n",
		},
		{
			name:     "wrong execution",
			response: `{"protocol":1,"ok":true,"state":"running_development","execution":"game","system":null,"core":null,"error":null,"version":"git-test"}` + "\n",
		},
		{
			name:     "failed without error",
			response: `{"protocol":1,"ok":false,"state":"running_development","execution":"development","system":null,"core":null,"error":null,"version":"git-test"}` + "\n",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newSocketFixture(t, testCase.response, true)
			_, err := NewClient(fixture.path).LoadDevelopmentRBF(context.Background(), "/tmp/core.rbf")
			if (err == nil) != testCase.valid {
				t.Fatalf("LoadDevelopmentRBF error = %v, want valid = %t", err, testCase.valid)
			}
			fixture.wait(t)
		})
	}
}

func TestClientRejectsLaunchShapesOutsideTheNativeMegaDriveContractBeforeDial(t *testing.T) {
	valid := LaunchRequest{
		System: "megadrive",
		RBF:    "/usr/share/mister-runtime/cores/megadrive.rbf",
		Media: map[string]string{
			"cartridge": "/media/fat/fogcast/cache/sonic2.bin",
		},
		Settings: map[string]string{},
	}
	cases := []struct {
		name   string
		mutate func(*LaunchRequest)
	}{
		{name: "unsupported system", mutate: func(request *LaunchRequest) { request.System = "nes" }},
		{name: "FAT core", mutate: func(request *LaunchRequest) { request.RBF = "/media/fat/_Console/MegaDrive.rbf" }},
		{name: "relative cartridge", mutate: func(request *LaunchRequest) { request.Media["cartridge"] = "sonic2.bin" }},
		{name: "missing cartridge", mutate: func(request *LaunchRequest) { request.Media = map[string]string{} }},
		{name: "extra media", mutate: func(request *LaunchRequest) { request.Media["save"] = "/tmp/save.sav" }},
		{name: "setting", mutate: func(request *LaunchRequest) { request.Settings["region"] = "auto" }},
		{name: "nil settings", mutate: func(request *LaunchRequest) { request.Settings = nil }},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			request := LaunchRequest{
				System: valid.System,
				RBF:    valid.RBF,
				Media: map[string]string{
					"cartridge": valid.Media["cartridge"],
				},
				Settings: map[string]string{},
			}
			test.mutate(&request)
			client := NewClient(filepath.Join(t.TempDir(), "must-not-be-dialed.sock"))
			if _, err := client.Launch(context.Background(), request); err == nil {
				t.Fatal("invalid native launch request accepted")
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
			response: `{"protocol":1,"ok":true,"state":"idle","execution":"none","system":null,"core":null,"error":null,"version":"git-test","extra":true}` + "\n",
		},
		{
			name:     "unknown error field",
			response: `{"protocol":1,"ok":true,"state":"idle","execution":"none","system":null,"core":null,"error":{"code":"busy","message":"busy","extra":true},"version":"git-test"}` + "\n",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newSocketFixture(t, testCase.response, true)
			if _, err := NewClient(fixture.path).Status(context.Background()); err == nil {
				t.Fatal("invalid runtime response accepted")
			}
			if request := fixture.wait(t); request != `{"protocol":1,"operation":"status"}` {
				t.Fatalf("request = %q", request)
			}
		})
	}
}

func TestClientAcceptsExactly65536BytesAndRejects65537(t *testing.T) {
	fixture := newSocketFixture(t, responseLineAtLength(t, MaximumLineBytes), true)
	response, err := NewClient(fixture.path).Status(context.Background())
	if err != nil {
		t.Fatalf("%d-byte response rejected: %v", MaximumLineBytes, err)
	}
	if response.Version == "" {
		t.Fatal("accepted response has an empty version")
	}
	if request := fixture.wait(t); request != `{"protocol":1,"operation":"status"}` {
		t.Fatalf("request = %q", request)
	}

	fixture = newSocketFixture(t, responseLineAtLength(t, MaximumLineBytes+1), true)
	if _, err := NewClient(fixture.path).Status(context.Background()); err == nil || err.Error() != "runtime response exceeds 65536 bytes" {
		t.Fatalf("%d-byte response error = %v", MaximumLineBytes+1, err)
	}
	if request := fixture.wait(t); request != `{"protocol":1,"operation":"status"}` {
		t.Fatalf("request = %q", request)
	}
}

func TestClientRejectsMissingNewline(t *testing.T) {
	fixture := newSocketFixture(t, idleResponse, false)

	if _, err := NewClient(fixture.path).Status(context.Background()); err == nil {
		t.Fatal("response without a newline accepted")
	}
	if request := fixture.wait(t); request != `{"protocol":1,"operation":"status"}` {
		t.Fatalf("request = %q", request)
	}
}

func TestClientRejectsInvalidStateExecutionAndErrorShapes(t *testing.T) {
	cases := []struct {
		name     string
		response string
	}{
		{
			name:     "unknown state",
			response: `{"protocol":1,"ok":true,"state":"unknown","execution":"none","system":null,"core":null,"error":null,"version":"git-test"}` + "\n",
		},
		{
			name:     "idle game execution",
			response: `{"protocol":1,"ok":true,"state":"idle","execution":"game","system":"snes","core":"SNES","error":null,"version":"git-test"}` + "\n",
		},
		{
			name:     "running game without core",
			response: `{"protocol":1,"ok":true,"state":"running_game","execution":"game","system":"snes","core":null,"error":null,"version":"git-test"}` + "\n",
		},
		{
			name:     "running development identity",
			response: `{"protocol":1,"ok":true,"state":"running_development","execution":"development","system":"snes","core":"SNES","error":null,"version":"git-test"}` + "\n",
		},
		{
			name:     "reboot required without idle failed error",
			response: `{"protocol":1,"ok":false,"state":"reboot_required","execution":"none","system":null,"core":null,"error":null,"version":"git-test"}` + "\n",
		},
		{
			name:     "failed response without error",
			response: `{"protocol":1,"ok":false,"state":"idle","execution":"none","system":null,"core":null,"error":null,"version":"git-test"}` + "\n",
		},
		{
			name:     "unknown error code",
			response: `{"protocol":1,"ok":false,"state":"idle","execution":"none","system":null,"core":null,"error":{"code":"unknown","message":"bad"},"version":"git-test"}` + "\n",
		},
		{
			name:     "error missing message",
			response: `{"protocol":1,"ok":false,"state":"idle","execution":"none","system":null,"core":null,"error":{"code":"busy"},"version":"git-test"}` + "\n",
		},
		{
			name:     "missing version",
			response: `{"protocol":1,"ok":true,"state":"idle","execution":"none","system":null,"core":null,"error":null,"version":""}` + "\n",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newSocketFixture(t, testCase.response, true)
			if _, err := NewClient(fixture.path).Status(context.Background()); err == nil {
				t.Fatal("invalid runtime response accepted")
			}
			if request := fixture.wait(t); request != `{"protocol":1,"operation":"status"}` {
				t.Fatalf("request = %q", request)
			}
		})
	}
}

func TestClientAcceptsOperationalIdleStatusWithRetainedPriorError(t *testing.T) {
	fixture := newSocketFixture(t, `{"protocol":1,"ok":true,"state":"idle","execution":"none","system":null,"core":null,"error":{"code":"io_failed","message":"previous cleanup completed"},"version":"git-test"}`+"\n", true)
	response, err := NewClient(fixture.path).Status(context.Background())
	if err != nil {
		t.Fatalf("successful status retaining a runtime error rejected: %v", err)
	}
	if response.Protocol != 1 || !response.OK || response.State != "idle" || response.Execution != "none" ||
		response.System != nil || response.Core != nil || response.Version != "git-test" || response.Error == nil ||
		response.Error.Code != "io_failed" || response.Error.Message != "previous cleanup completed" {
		t.Fatalf("response = %#v", response)
	}
	if request := fixture.wait(t); request != `{"protocol":1,"operation":"status"}` {
		t.Fatalf("request = %q", request)
	}
}

func TestClientAcceptsStartingNoneWithNullOrRetainedIdentity(t *testing.T) {
	cases := []struct {
		name     string
		response string
		valid    bool
	}{
		{
			name:     "null identity",
			response: `{"protocol":1,"ok":true,"state":"starting","execution":"none","system":null,"core":null,"error":null,"version":"git-test"}` + "\n",
			valid:    true,
		},
		{
			name:     "retained complete identity",
			response: `{"protocol":1,"ok":true,"state":"starting","execution":"none","system":"snes","core":"SNES","error":null,"version":"git-test"}` + "\n",
			valid:    true,
		},
		{
			name:     "partial retained identity",
			response: `{"protocol":1,"ok":true,"state":"starting","execution":"none","system":"snes","core":null,"error":null,"version":"git-test"}` + "\n",
			valid:    false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newSocketFixture(t, testCase.response, true)
			_, err := NewClient(fixture.path).Status(context.Background())
			if (err == nil) != testCase.valid {
				t.Fatalf("Status error = %v, want valid = %t", err, testCase.valid)
			}
			fixture.wait(t)
		})
	}
}

func TestClientRequiresCompleteIdentityForStartingGame(t *testing.T) {
	cases := []struct {
		name     string
		response string
		valid    bool
	}{
		{
			name:     "complete identity",
			response: `{"protocol":1,"ok":true,"state":"starting","execution":"game","system":"snes","core":"SNES","error":null,"version":"git-test"}` + "\n",
			valid:    true,
		},
		{
			name:     "missing system",
			response: `{"protocol":1,"ok":true,"state":"starting","execution":"game","system":null,"core":"SNES","error":null,"version":"git-test"}` + "\n",
			valid:    false,
		},
		{
			name:     "empty core",
			response: `{"protocol":1,"ok":true,"state":"starting","execution":"game","system":"snes","core":"","error":null,"version":"git-test"}` + "\n",
			valid:    false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newSocketFixture(t, testCase.response, true)
			_, err := NewClient(fixture.path).Status(context.Background())
			if (err == nil) != testCase.valid {
				t.Fatalf("Status error = %v, want valid = %t", err, testCase.valid)
			}
			fixture.wait(t)
		})
	}
}

func TestClientRequiresNullIdentityForStartingDevelopment(t *testing.T) {
	cases := []struct {
		name     string
		response string
		valid    bool
	}{
		{
			name:     "null identity",
			response: `{"protocol":1,"ok":true,"state":"starting","execution":"development","system":null,"core":null,"error":null,"version":"git-test"}` + "\n",
			valid:    true,
		},
		{
			name:     "identity present",
			response: `{"protocol":1,"ok":true,"state":"starting","execution":"development","system":"snes","core":"SNES","error":null,"version":"git-test"}` + "\n",
			valid:    false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newSocketFixture(t, testCase.response, true)
			_, err := NewClient(fixture.path).Status(context.Background())
			if (err == nil) != testCase.valid {
				t.Fatalf("Status error = %v, want valid = %t", err, testCase.valid)
			}
			fixture.wait(t)
		})
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
	if _, err := NewClient(path).Status(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Status error = %v, want context deadline exceeded", err)
	}

	result := waitFixtureResult(t, done)
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.request != `{"protocol":1,"operation":"status"}` {
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
		_, err := NewClient(path).Status(ctx)
		callDone <- err
	}()

	request := waitFixtureResult(t, requestReceived)
	if request.err != nil {
		t.Fatal(request.err)
	}
	if request.request != `{"protocol":1,"operation":"status"}` {
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
			if result.request != `{"protocol":1,"operation":"status"}` {
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
		_, err := NewClient(path).Status(ctx)
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
	const prefix = `{"protocol":1,"ok":true,"state":"idle","execution":"none","system":null,"core":null,"error":null,"version":"`
	const suffix = `"}` + "\n"
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
