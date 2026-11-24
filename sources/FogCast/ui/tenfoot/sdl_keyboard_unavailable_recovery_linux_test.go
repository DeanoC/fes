//go:build linux

package tenfoot

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/localfeed"
)

func TestSDLPadRecoveryKeepsKeyboardFenced(t *testing.T) {
	a := readyNativeBindingApp(t)
	defer a.Stop()
	client, peer := net.Pipe() // In-memory target; no OS socket or device.
	defer peer.Close()
	_ = peer.SetDeadline(time.Now().Add(time.Second))
	feed := localfeed.New("unused", func(string, string, time.Duration) (net.Conn, error) { return client, nil })
	a.mu.Lock()
	a.kitLocalKeyboardUnavailable = true
	a.localFeed = feed
	a.mu.Unlock()
	service, err := configureSDLPadRecovery(a, true)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := a.padOnlyAuthority(false)
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		handle localfeed.V2Handle
		err    error
	}
	done := make(chan result, 1)
	go func() {
		h, err := service.recover(context.Background(), binding, binding.AgentInstance)
		done <- result{h, err}
	}()
	reader := bufio.NewReader(peer)
	req, err := localhid.ReadRequest(reader)
	if err != nil || req.Op != "recover" {
		t.Fatalf("request=%+v err=%v", req, err)
	}
	pad := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}
	if err := feed.Send(pad, time.Time{}); !errors.Is(err, localfeed.ErrUnavailable) {
		t.Fatalf("pad before recovery: %v", err)
	}
	reply := localhid.Reply{Version: 2, Op: "recover", Request: req.Request, AgentInstance: req.AgentInstance, TargetID: req.TargetID, BootID: req.BootID, Status: "reader-drained"}
	wire, err := localhid.EncodeReply(reply)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = peer.Write(wire); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if got.err != nil {
		t.Fatal(got.err)
	}
	defer feed.CloseV2(got.handle)
	key, _ := hidkeys.Event(4, true)
	if err := feed.Send(key, time.Time{}); !errors.Is(err, localfeed.ErrUnavailable) {
		t.Fatalf("SDL guest keyboard escaped: %v", err)
	}
	padDone := make(chan error, 1)
	go func() { padDone <- feed.Send(pad, time.Time{}) }()
	frame, err := protocol.DecodeInputFrame(reader, 48)
	if err != nil || frame.Device != uint8(remoteinput.DeviceGamepad) {
		t.Fatalf("pad=%+v err=%v", frame, err)
	}
	if err := <-padDone; err != nil {
		t.Fatal(err)
	}
}

func TestNativePadOnlyRequiresCompleteNonKeyboardCapabilities(t *testing.T) {
	a := readyNativeBindingApp(t)
	defer a.Stop()
	a.mu.Lock()
	id := strings.Repeat("a", 64)
	a.session.RuntimeInput = &protocol.RuntimeInputIdentity{Kind: protocol.RuntimeInputDescribed, RuntimeInstance: strings.Repeat("d", 32), PackageID: id, Generation: 9}
	a.session.CorePackage = &hostclient.SessionCorePackage{PackageID: id, Generation: 9, ABI: hostclient.SessionCoreABI{ID: "fes.application", Major: 1}}
	a.session.CoreKeyboard, a.session.CoreKeyboardHID = false, false
	if a.nativePadOnlyCapabilitiesLocked() {
		a.mu.Unlock()
		t.Fatal("missing list accepted")
	}
	a.session.CorePackage.ActiveInterfaces = []hostclient.SessionCoreInterface{{ID: "fes.gamepad.ports", Major: 1}}
	if _, err := a.padOnlyAuthorityLocked(time.Now(), true); err != nil {
		a.mu.Unlock()
		t.Fatalf("valid pad-only session: %v", err)
	}
	a.session.CorePackage.ActiveInterfaces = append(a.session.CorePackage.ActiveInterfaces, hostclient.SessionCoreInterface{ID: "fes.keyboard.hid", Major: 2})
	if a.nativePadOnlyCapabilitiesLocked() {
		a.mu.Unlock()
		t.Fatal("unsupported keyboard inferred absent")
	}
	a.session.CorePackage.ActiveInterfaces = []hostclient.SessionCoreInterface{{ID: "fes.gamepad.ports", Major: 1}, {ID: "fes.gamepad.ports", Major: 1}}
	if a.nativePadOnlyCapabilitiesLocked() {
		a.mu.Unlock()
		t.Fatal("duplicate capability accepted")
	}
	a.mu.Unlock()
}

func TestSDLPadRecoveryRejectsChangedAuthority(t *testing.T) {
	a := readyNativeBindingApp(t)
	defer a.Stop()
	client, peer := net.Pipe()
	defer peer.Close()
	_ = peer.SetDeadline(time.Now().Add(time.Second))
	feed := localfeed.New("unused", func(string, string, time.Duration) (net.Conn, error) { return client, nil })
	a.mu.Lock()
	a.kitLocalKeyboardUnavailable = true
	a.localFeed = feed
	a.mu.Unlock()
	service, err := configureSDLPadRecovery(a, true)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := a.padOnlyAuthority(false)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := service.recover(context.Background(), binding, binding.AgentInstance); done <- err }()
	req, err := localhid.ReadRequest(bufio.NewReader(peer))
	if err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.session.InputAgentInstance = strings.Repeat("f", 32)
	a.mu.Unlock()
	wire, err := localhid.EncodeReply(localhid.Reply{Version: 2, Op: "recover", Request: req.Request, AgentInstance: req.AgentInstance, TargetID: req.TargetID, BootID: req.BootID, Status: "reader-drained"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = peer.Write(wire); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("changed agent accepted")
	}
}
