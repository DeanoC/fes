//go:build linux

package tenfoot

import (
	"bufio"
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/ui/localfeed"
)

func nativeTestRecoveredHandle(t *testing.T, b nativeGuestBinding) (*localfeed.Feed, localfeed.V2Handle) {
	t.Helper()
	feed := localfeed.New("memory", func(string, string, time.Duration) (net.Conn, error) {
		client, peer := net.Pipe()
		go func() {
			defer peer.Close()
			req, err := localhid.ReadRequest(bufio.NewReader(peer))
			if err != nil {
				return
			}
			reply := localhid.Reply{Version: 2, Op: "recover", Request: req.Request, Status: "reader-drained", AgentInstance: b.InputAgentInstance, TargetID: b.TargetID, BootID: b.BootID}
			wire, err := localhid.EncodeReply(reply)
			if err != nil {
				return
			}
			_, _ = peer.Write(wire)
			_, _ = io.Copy(io.Discard, peer)
		}()
		return client, nil
	})
	if err := feed.ReserveV2(); err != nil {
		t.Fatal(err)
	}
	_, handle, err := feed.RecoverV2(context.Background(), b.InputAgentInstance, b.TargetID, b.BootID, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(feed.Close)
	return feed, handle
}

func TestNativeConnectionWatcherUsesExactRecoveredHandle(t *testing.T) {
	a := readyNativeBindingApp(t)
	defer a.Stop()
	b, err := a.nativeKeyboardPrepareBinding([16]byte{1})
	if err != nil {
		t.Fatal(err)
	}
	feed, handle := nativeTestRecoveredHandle(t, b)
	done := make(chan bool, 1)
	go func() { done <- a.nativeKeyboardWatchConnection(context.Background(), handle, b) }()
	feed.CloseV2(handle)
	select {
	case matched := <-done:
		if !matched {
			t.Fatal("current connection loss ignored")
		}
	case <-time.After(time.Second):
		t.Fatal("watcher did not observe EOF")
	}
	a.mu.Lock()
	quarantined := a.nativeKeyboardQuarantined
	a.mu.Unlock()
	if !quarantined {
		t.Fatal("lost connection was not quarantined")
	}
}

func TestNativeConnectionWatcherIgnoresStaleHandle(t *testing.T) {
	a := readyNativeBindingApp(t)
	defer a.Stop()
	old, err := a.nativeKeyboardPrepareBinding([16]byte{1})
	if err != nil {
		t.Fatal(err)
	}
	feed, handle := nativeTestRecoveredHandle(t, old)
	next, err := a.nativeKeyboardPrepareBinding([16]byte{1})
	if err != nil {
		t.Fatal(err)
	}
	feed.CloseV2(handle)
	if a.nativeKeyboardWatchConnection(context.Background(), handle, old) {
		t.Fatal("old handle quarantined successor")
	}
	a.mu.Lock()
	current := a.nativeGuestBinding
	a.mu.Unlock()
	if current != next {
		t.Fatal("stale EOF changed successor binding")
	}
}
