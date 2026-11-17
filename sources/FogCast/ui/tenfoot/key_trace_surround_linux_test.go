//go:build linux

package tenfoot

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/testwait"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/gfx"
	"golang.org/x/sys/unix"
)

func TestDiagnosticHoldUsesRealNonHandoffTransition(t *testing.T) {
	prior := computerTapeDisplaySession()
	prior.ID, prior.GameID, prior.Target, prior.TargetID, prior.FlightID = "session", "game", "kit", "target", "flight"
	prior.CoreKeyboardHID = true
	prior.CorePackage.ActiveInterfaces = append(prior.CorePackage.ActiveInterfaces, hostclient.SessionCoreInterface{ID: "fes.keyboard.hid", Major: 1})
	entered, allow := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(allow) }) }
	t.Cleanup(unblock)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/display" {
			http.NotFound(w, r)
			return
		}
		display := readSessionDisplay(t, r)
		if display == protocol.SessionDisplayShown {
			close(entered)
			select {
			case <-allow:
			case <-r.Context().Done():
				return
			}
		} else if display != protocol.SessionDisplayClosed {
			t.Errorf("unexpected display: %+v", display)
		}
		writeKitDisplaySession(w, prior)
	}))
	t.Cleanup(server.Close)
	app, feed := kitDisplayApp(t, server)
	presenter := &countingSessionMenu{}
	display, err := gfx.NewMenuDisplayWithPresenter(presenter)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(display.Close)
	app.SetMenuDisplay(display)
	app.mu.Lock()
	app.session, app.surround, app.sessionDisplay, app.kitHost = prior, true, protocol.SessionDisplayShown, true
	app.hostAwaitStatus, app.kitHostPreparing = false, false
	app.frameTrace = newFrameTrace()
	now := time.Now()
	app.frameTrace.armKeyRouting(now)
	app.mu.Unlock()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	hold, release := filepath.Join(dir, "hold"), filepath.Join(dir, "release")
	request := fmt.Sprintf("%d start %s\n", os.Getpid(), diagnosticSessionToken(prior))
	write := func(path string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(request), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(hold)
	app.handleDiagnosticSurroundRequest(os.Getpid(), "start", os.Getuid(), now, hold, release)
	app.mu.Lock()
	if app.diagnosticSurroundUntil.IsZero() {
		app.mu.Unlock()
		t.Fatal("diagnostic hold was not accepted")
	}
	app.mu.Unlock()
	app.mu.Lock()
	app.syncSurroundLocked()
	app.mu.Unlock()
	waitFor(t, app, "diagnostic close", func(Snapshot) bool {
		app.mu.Lock()
		defer app.mu.Unlock()
		return !app.sessionDisplayBusy && app.sessionDisplayLocked() == protocol.SessionDisplayClosed
	})
	open := func() (*nativeInput, *os.File) {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
		fd := int(r.Fd())
		if err := unix.SetNonblock(fd, true); err != nil {
			t.Fatal(err)
		}
		return &nativeInput{fd: fd, kind: InputKeyboard}, w
	}
	first, a := open()
	second, b := open()
	inputs := &nativeInputs{devices: []*nativeInput{first, second}}
	send := func(w *os.File, value int32) {
		t.Helper()
		if _, err := w.Write(nativeTestEvent(1, 59, value)); err != nil {
			t.Fatal(err)
		}
		if _, err := inputs.poll(app, time.Now()); err != nil {
			t.Fatal(err)
		}
		drainLocalKeyQueue(t, app)
	}
	send(a, 1)
	if len(feed.snapshot()) == 0 {
		t.Fatal("A was not admitted before the transition")
	}
	write(release)
	app.handleDiagnosticSurroundRequest(os.Getpid(), "start", os.Getuid(), time.Now(), hold, release)
	app.mu.Lock()
	app.syncSurroundLocked()
	app.mu.Unlock()
	select {
	case <-entered:
	case <-time.After(testwait.Timeout):
		t.Fatal("Tenfoot did not start display request")
	}
	send(b, 1)
	send(a, 0)
	code, _ := hidkeys.Code(0x3a)
	var actions []remoteinput.Action
	for _, e := range feed.snapshot() {
		if e.Device == remoteinput.DeviceKeyboard && e.Code == code {
			actions = append(actions, e.Action)
		}
	}
	if len(actions) != 2 || actions[0] != remoteinput.ActionPress || actions[1] != remoteinput.ActionRelease {
		t.Fatalf("input actions during transition = %v", actions)
	}
	unblock()
	waitFor(t, app, "diagnostic open", func(Snapshot) bool {
		app.mu.Lock()
		defer app.mu.Unlock()
		return !app.sessionDisplayBusy && app.sessionDisplayLocked() == protocol.SessionDisplayShown
	})
	send(b, 0)
	if !app.surround {
		t.Fatal("diagnostic changed surround preference")
	}
}

func TestDisconnectNeutralTraceIdentifiesSourceAndTransition(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(http.NotFound))
	t.Cleanup(server.Close)
	app, feed := kitDisplayApp(t, server)
	app.mu.Lock()
	app.session.CoreKeyboardHID = true
	app.frameTrace = newFrameTrace()
	app.frameTrace.armKeyRouting(time.Now())
	app.mu.Unlock()
	open := func() (*nativeInput, *os.File) {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
		fd := int(r.Fd())
		if err := unix.SetNonblock(fd, true); err != nil {
			t.Fatal(err)
		}
		return &nativeInput{fd: fd, kind: InputKeyboard}, w
	}
	first, a := open()
	second, b := open()
	inputs := &nativeInputs{devices: []*nativeInput{first, second}}
	send := func(w *os.File, value int32) {
		t.Helper()
		if _, err := w.Write(nativeTestEvent(1, 59, value)); err != nil {
			t.Fatal(err)
		}
		if _, err := inputs.poll(app, time.Now()); err != nil {
			t.Fatal(err)
		}
		drainLocalKeyQueue(t, app)
	}
	send(a, 1)
	app.mu.Lock()
	app.sessionDisplayBusy, app.sessionDisplayNonHandoff, app.sessionDisplayEpoch = true, true, 42
	app.mu.Unlock()
	send(b, 1)
	inputs.devices = []*nativeInput{second}
	inputs.releaseLostPlayInput(app, first)
	drainLocalKeyQueue(t, app)
	var found bool
	for _, r := range app.frameTrace.records() {
		if r.Route == "disconnect-neutral" && r.Source == first.fd && r.Epoch == 42 && r.NonHandoff {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing source-bound disconnect trace: %+v", app.frameTrace.records())
	}
	code, _ := hidkeys.Code(0x3a)
	var actions []remoteinput.Action
	for _, e := range feed.snapshot() {
		if e.Device == remoteinput.DeviceKeyboard && e.Code == code {
			actions = append(actions, e.Action)
		}
	}
	if len(actions) != 2 || actions[0] != remoteinput.ActionPress || actions[1] != remoteinput.ActionRelease {
		t.Fatalf("disconnect actions = %v", actions)
	}
}

func TestDiagnosticSurroundIsSessionBoundAndExpires(t *testing.T) {
	s := computerTapeDisplaySession()
	s.ID, s.GameID, s.Target, s.TargetID, s.FlightID = "session", "game", "kit", "target", "flight"
	s.Input = &hostclient.SessionInput{Ready: true}
	a := NewApp(NewClient("http://localhost", nil), 1280, 720, 20)
	a.kitHost, a.surround, a.menuDisplay, a.session = true, true, &gfx.MenuDisplay{}, s
	a.sessionDisplay = protocol.SessionDisplayShown
	a.frameTrace = newFrameTrace()
	now := time.Now()
	a.frameTrace.armKeyRouting(now)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	hold, release := filepath.Join(dir, "hold"), filepath.Join(dir, "release")
	request := fmt.Sprintf("%d %s %s\n", os.Getpid(), "start", diagnosticSessionToken(s))
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(hold, request)
	a.handleDiagnosticSurroundRequest(os.Getpid(), "start", os.Getuid(), now, hold, release)
	if a.diagnosticSurroundUntil.IsZero() || a.sessionReturnDisplayLocked() != protocol.SessionDisplayClosed || !a.surround {
		t.Fatal("valid hold did not close only the diagnostic surround")
	}
	write(release, request)
	a.handleDiagnosticSurroundRequest(os.Getpid(), "start", os.Getuid(), now, hold, release)
	if !a.diagnosticSurroundUntil.IsZero() || a.sessionReturnDisplayLocked() != protocol.SessionDisplayShown || !a.surround {
		t.Fatal("release did not restore the non-persistent preference")
	}
	write(hold, request)
	a.handleDiagnosticSurroundRequest(os.Getpid(), "start", os.Getuid(), now, hold, release)
	a.session.FlightID = "replacement"
	if a.sessionReturnDisplayLocked() != protocol.SessionDisplayShown || !a.diagnosticSurroundUntil.IsZero() {
		t.Fatal("hold crossed a session change")
	}
	a.session = s
	a.diagnosticSurroundSession = s
	a.diagnosticSurroundUntil = time.Now().Add(-time.Second)
	if a.sessionReturnDisplayLocked() != protocol.SessionDisplayShown || !a.diagnosticSurroundUntil.IsZero() {
		t.Fatal("expired hold did not restore surround")
	}
}

func TestDiagnosticSurroundRejectsStaleAndWrongSessionRequests(t *testing.T) {
	s := computerTapeDisplaySession()
	s.ID, s.GameID, s.Target, s.TargetID, s.FlightID = "session", "game", "kit", "target", "flight"
	s.Input = &hostclient.SessionInput{Ready: true}
	a := NewApp(NewClient("http://localhost", nil), 1280, 720, 20)
	a.kitHost, a.surround, a.menuDisplay, a.session = true, true, &gfx.MenuDisplay{}, s
	a.sessionDisplay = protocol.SessionDisplayShown
	a.frameTrace = newFrameTrace()
	now := time.Now()
	a.frameTrace.armKeyRouting(now.Add(-time.Minute))
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	hold, release := filepath.Join(dir, "hold"), filepath.Join(dir, "release")
	if err := os.WriteFile(hold, []byte(fmt.Sprintf("%d start %s\n", os.Getpid(), diagnosticSessionToken(s))), 0600); err != nil {
		t.Fatal(err)
	}
	a.handleDiagnosticSurroundRequest(os.Getpid(), "start", os.Getuid(), now, hold, release)
	if !a.diagnosticSurroundUntil.IsZero() {
		t.Fatal("expired trace accepted")
	}
	a.frameTrace.armKeyRouting(now)
	a.session.FlightID = "replacement"
	a.handleDiagnosticSurroundRequest(os.Getpid(), "start", os.Getuid(), now, hold, release)
	if !a.diagnosticSurroundUntil.IsZero() {
		t.Fatal("request for old session accepted")
	}
}
