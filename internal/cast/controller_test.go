package cast

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

type fakeProcess struct {
	mu                   sync.Mutex
	args                 []string
	killed               bool
	killCalls            int
	keepWaitingAfterKill bool
	waitCh               chan struct{}
	killErr              error
}

func (p *fakeProcess) Wait() error { <-p.waitCh; return nil }
func (p *fakeProcess) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.killCalls++
	if p.killErr != nil {
		return p.killErr
	}
	if !p.killed {
		p.killed = true
		if !p.keepWaitingAfterKill {
			close(p.waitCh)
		}
	}
	return nil
}

func testConfig() Config {
	return Config{Binary: "/media/fat/mister-remote/fbbridge-poc6", RTPAddress: ":5534", Control: ":5535", Framebuffer: "/dev/fb0", NativeCmd: "/dev/MiSTer_cmd", NativeMode: "8888 1 1920 1080", StopTimeout: time.Second}
}

func TestControllerStartsWithAuthenticatedSessionArgumentsAndStops(t *testing.T) {
	process := &fakeProcess{waitCh: make(chan struct{})}
	tokenFile := filepath.Join(t.TempDir(), "cast-token")
	config := testConfig()
	config.TokenFile = tokenFile
	config.Generation = 9
	var name string
	var args []string
	controller, err := New(config, func(_ context.Context, gotName string, gotArgs ...string) (Process, error) {
		name, args = gotName, gotArgs
		return process, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Start(context.Background(), "session-1", "token-1", 9); err != nil {
		t.Fatal(err)
	}
	if name != testConfig().Binary {
		t.Fatalf("binary = %q", name)
	}
	joined := " " + strings.Join(args, " ") + " "
	for _, want := range []string{" -rtp :5534 ", " -control :5535 ", " -session session-1 ", " -generation 9 ", " -token-file "} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args %q missing %q", joined, want)
		}
	}
	if got := controller.Status(context.Background()).State; got != Active {
		t.Fatalf("status = %q", got)
	}
	if err := controller.Stop(context.Background(), "session-1", 9); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(tokenFile)
	if err != nil {
		t.Fatalf("configured token path was removed: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("scrubbed token mode = %04o, want 0600", info.Mode().Perm())
	}
	contents, err := os.ReadFile(tokenFile)
	if err != nil || len(contents) != 0 {
		t.Fatalf("configured token path was not scrubbed: size=%d err=%v", len(contents), err)
	}
	if got := controller.Status(context.Background()).State; got != Idle {
		t.Fatalf("status after stop = %q", got)
	}
}

func TestControllerMediaAdmissionFailsClosedForAudio(t *testing.T) {
	process := &fakeProcess{waitCh: make(chan struct{})}
	controller, err := New(testConfig(), func(context.Context, string, ...string) (Process, error) { return process, nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.StartWithMedia(context.Background(), "session", "token", 9, protocol.CastMediaSet{Version: protocol.CastMediaSetVersion, Video: true, Audio: true}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("audio start = %v, want %v", err, ErrInvalid)
	}
	if got := controller.Status(context.Background()); got.State != Idle || got.Media != nil {
		t.Fatalf("status after rejected audio = %#v", got)
	}
}

func TestControllerReplacesPermissiveTokenFileWithMode0600(t *testing.T) {
	process := &fakeProcess{waitCh: make(chan struct{})}
	tokenFile := filepath.Join(t.TempDir(), "cast-token")
	if err := os.WriteFile(tokenFile, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	config := testConfig()
	config.TokenFile = tokenFile
	controller, err := New(config, func(context.Context, string, ...string) (Process, error) { return process, nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Start(context.Background(), "session", "replacement-token", 9); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = controller.Stop(context.Background(), "session", 9) })

	info, err := os.Stat(tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("token file mode = %04o, want 0600", got)
	}
	contents, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "replacement-token" {
		t.Fatal("token file was not replaced with the dynamic token")
	}
}

func TestControllerReplacesSymlinkWithoutChangingItsTarget(t *testing.T) {
	process := &fakeProcess{waitCh: make(chan struct{})}
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	tokenFile := filepath.Join(dir, "cast-token")
	const targetContents = "must-remain-unchanged"
	if err := os.WriteFile(target, []byte(targetContents), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, tokenFile); err != nil {
		t.Fatal(err)
	}
	config := testConfig()
	config.TokenFile = tokenFile
	controller, err := New(config, func(context.Context, string, ...string) (Process, error) { return process, nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Start(context.Background(), "session", "dynamic-token", 9); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = controller.Stop(context.Background(), "session", 9) })

	info, err := os.Lstat(tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("token path remains a symlink")
	}
	gotTarget, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotTarget) != targetContents {
		t.Fatal("symlink target was changed")
	}
}

func TestControllerRetainsOwnershipAndRetriesFailedTokenScrub(t *testing.T) {
	process := &fakeProcess{waitCh: make(chan struct{})}
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "cast-token")
	config := testConfig()
	config.TokenFile = tokenFile
	controller, err := New(config, func(context.Context, string, ...string) (Process, error) { return process, nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Start(context.Background(), "session", "dynamic-token", 9); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	if err := controller.Stop(context.Background(), "session", 9); !errors.Is(err, ErrStop) {
		t.Fatalf("stop with blocked scrub = %v, want %v", err, ErrStop)
	}
	if got := controller.Status(context.Background()).State; got != Active {
		t.Fatalf("status after failed scrub = %q, want retained %q", got, Active)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := controller.Stop(context.Background(), "session", 9); err != nil {
		t.Fatalf("retry stop = %v", err)
	}
	if got := controller.Status(context.Background()).State; got != Idle {
		t.Fatalf("status after successful scrub = %q, want %q", got, Idle)
	}
	contents, err := os.ReadFile(tokenFile)
	if err != nil || len(contents) != 0 {
		t.Fatalf("token was not scrubbed on retry: size=%d err=%v", len(contents), err)
	}
}

func TestControllerRetainsCleanupOwnershipWhenProcessStartFails(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "cast-token")
	config := testConfig()
	config.TokenFile = tokenFile
	controller, err := New(config, func(context.Context, string, ...string) (Process, error) {
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		return nil, errors.New("start failed")
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Start(context.Background(), "session", "dynamic-token", 9); !errors.Is(err, ErrStart) {
		t.Fatalf("start = %v, want %v", err, ErrStart)
	}
	if got := controller.Status(context.Background()).State; got != Active {
		t.Fatalf("status after failed start cleanup = %q, want retained %q", got, Active)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := controller.Stop(context.Background(), "session", 9); err != nil {
		t.Fatalf("retry cleanup = %v", err)
	}
	contents, err := os.ReadFile(tokenFile)
	if err != nil || len(contents) != 0 {
		t.Fatalf("token was not scrubbed after failed start: size=%d err=%v", len(contents), err)
	}
}

func TestControllerKillsAndReapsPartialProcessBeforeScrubbingToken(t *testing.T) {
	process := &fakeProcess{waitCh: make(chan struct{}), keepWaitingAfterKill: true}
	tokenFile := filepath.Join(t.TempDir(), "cast-token")
	config := testConfig()
	config.TokenFile = tokenFile
	config.StopTimeout = 20 * time.Millisecond
	controller, err := New(config, func(context.Context, string, ...string) (Process, error) {
		return process, errors.New("start failed after acquiring process")
	})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := controller.Start(context.Background(), "session", "dynamic-token", 9); !errors.Is(err, ErrStart) {
		t.Fatalf("start = %v, want %v", err, ErrStart)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("partial-process cleanup was not bounded: %s", elapsed)
	}
	process.mu.Lock()
	killed, killCalls := process.killed, process.killCalls
	process.mu.Unlock()
	if !killed || killCalls != 1 {
		t.Fatalf("partial process killed=%v calls=%d, want true/1", killed, killCalls)
	}
	contents, err := os.ReadFile(tokenFile)
	if err != nil || string(contents) != "dynamic-token" {
		t.Fatalf("token ownership released before process reaped: contents=%q err=%v", contents, err)
	}
	if got := controller.Status(context.Background()).State; got != Active {
		t.Fatalf("status while partial process is unreaped = %q, want %q", got, Active)
	}

	close(process.waitCh)
	if err := controller.Stop(context.Background(), "session", 9); err != nil {
		t.Fatalf("retry cleanup = %v", err)
	}
	contents, err = os.ReadFile(tokenFile)
	if err != nil || len(contents) != 0 {
		t.Fatalf("token was not scrubbed after process reaped: size=%d err=%v", len(contents), err)
	}
	if got := controller.Status(context.Background()).State; got != Idle {
		t.Fatalf("status after retry cleanup = %q, want %q", got, Idle)
	}
}

func TestControllerRejectsImmediateChildExitAndReturnsIdle(t *testing.T) {
	waitCh := make(chan struct{})
	close(waitCh)
	process := &fakeProcess{waitCh: waitCh}
	tokenFile := filepath.Join(t.TempDir(), "cast-token")
	config := testConfig()
	config.TokenFile = tokenFile
	config.StartGrace = 20 * time.Millisecond
	controller, err := New(config, func(context.Context, string, ...string) (Process, error) { return process, nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Start(context.Background(), "session", "token", 9); !errors.Is(err, ErrStart) {
		t.Fatalf("start = %v, want %v", err, ErrStart)
	}
	if got := controller.Status(context.Background()).State; got != Idle {
		t.Fatalf("status = %q, want %q", got, Idle)
	}
	contents, err := os.ReadFile(tokenFile)
	if err != nil || len(contents) != 0 {
		t.Fatalf("configured token path was not scrubbed after child exit: size=%d err=%v", len(contents), err)
	}
}

func TestControllerRejectsReplacementAndInvalidStart(t *testing.T) {
	process := &fakeProcess{waitCh: make(chan struct{})}
	controller, err := New(testConfig(), func(context.Context, string, ...string) (Process, error) { return process, nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Start(context.Background(), "", "token", 9); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid = %v", err)
	}
	if err := controller.Start(context.Background(), "session", "token", 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("zero generation = %v", err)
	}
	if err := controller.Start(context.Background(), "session", "token", 9); err != nil {
		t.Fatal(err)
	}
	if err := controller.Start(context.Background(), "other", "token", 9); !errors.Is(err, ErrBusy) {
		t.Fatalf("replacement = %v", err)
	}
	_ = controller.Stop(context.Background(), "session", 9)
}

func TestControllerRejectsStaleStopIdentity(t *testing.T) {
	process := &fakeProcess{waitCh: make(chan struct{})}
	controller, err := New(testConfig(), func(context.Context, string, ...string) (Process, error) { return process, nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Start(context.Background(), "current", "token", 9); err != nil {
		t.Fatal(err)
	}
	if status := controller.Status(context.Background()); status.Session != "current" || status.Generation != 9 {
		t.Fatalf("status identity = %#v", status)
	}
	if err := controller.Stop(context.Background(), "stale", 8); !errors.Is(err, ErrStale) {
		t.Fatalf("stale stop = %v, want %v", err, ErrStale)
	}
	process.mu.Lock()
	killCalls := process.killCalls
	process.mu.Unlock()
	if killCalls != 0 {
		t.Fatalf("stale stop killed current process %d times", killCalls)
	}
	if err := controller.Stop(context.Background(), "current", 9); err != nil {
		t.Fatal(err)
	}
}

func TestInstallTokenRenameFailureNeverWritesCredentialToTemporaryPath(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token")
	if err := os.Mkdir(tokenPath, 0o700); err != nil {
		t.Fatal(err)
	}
	secret := []byte("credential-must-not-remain")
	if err := installTokenFile(tokenPath, secret); err == nil {
		t.Fatal("install unexpectedly replaced directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() == "token" {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(dir, entry.Name()))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if strings.Contains(string(data), string(secret)) {
			t.Fatalf("temporary path %q retained credential bytes", entry.Name())
		}
	}
}
