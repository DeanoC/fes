package input

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

func grantTestLocal(c *TargetController, b localDelegation) {
	c.localKeyboardGrant = &localKeyboardGrant{executionGeneration: b.LeaseGeneration, binding: KeyboardHIDBinding{PackageID: b.PackageID, Generation: b.CoreGeneration}}
}

func TestDelegatedLocalInputPinsLeaseAndCore(t *testing.T) {
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	t.Cleanup(manager.Close)
	deadline := time.Now().Add(time.Second)
	for manager.Status().State != "free" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	grant, err := manager.Claim(kitlease.ClaimRequest{RequestID: strings.Repeat("a", 32), Owner: "private", Purpose: "test"})
	if err != nil {
		t.Fatal(err)
	}
	packageID := strings.Repeat("c", 64)
	current := uint64(3)
	writes := 0
	c, _ := newLocalHIDFeed(t, func(context.Context, string, string, uint64, hidkeys.Rows) error { writes++; return nil },
		func(context.Context) (CoreObservation, error) {
			return CoreObservation{Active: true, KeyboardHID: &KeyboardHIDBinding{PackageID: packageID, Generation: current}}, nil
		})
	c.SetLocalLeaseManager(manager)
	code, _ := hidkeys.Code(4)
	press := keyboardFrameFor(code, remoteinput.ActionPress)
	binding := localDelegation{LeaseGeneration: grant.Status.Generation, PackageID: packageID, CoreGeneration: current}
	grantTestLocal(c, binding)
	if err := c.deliverDelegatedLocal(context.Background(), press, binding); err != nil || writes != 1 {
		t.Fatalf("valid delivery: writes=%d err=%v", writes, err)
	}
	current++
	if err := c.deliverDelegatedLocal(context.Background(), press, binding); !errors.Is(err, errNoCore) || writes != 1 {
		t.Fatalf("old core binding: writes=%d err=%v", writes, err)
	}
	if _, err := manager.Release(grant.Token); err != nil {
		t.Fatal(err)
	}
	for manager.Status().State != "free" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if _, err := manager.Claim(kitlease.ClaimRequest{RequestID: strings.Repeat("b", 32), Owner: "next", Purpose: "test"}); err != nil {
		t.Fatal(err)
	}
	if err := c.deliverDelegatedLocal(context.Background(), press, binding); !errors.Is(err, kitlease.ErrLease) || writes != 1 {
		t.Fatalf("old lease binding: writes=%d err=%v", writes, err)
	}
}

func TestReadLocalDelegationRejectsInvalidBindings(t *testing.T) {
	valid := `{"operation":"key","lease_generation":"` + strings.Repeat("a", 64) + `","package_id":"` + strings.Repeat("b", 64) + `","core_generation":2,"epoch":4}` + "\n"
	if got, err := readLocalDelegation(bufio.NewReader(strings.NewReader(valid))); err != nil || got == nil || got.CoreGeneration != 2 {
		t.Fatalf("valid header %v %v", got, err)
	}
	duplicate := strings.Replace(valid, `"core_generation":2`, `"core_generation":2,"core_generation":3`, 1)
	trailing := strings.TrimSuffix(valid, "\n") + " {}\n"
	for _, text := range []string{`{"lease_generation":"bad"}` + "\n", valid[:len(valid)-1], `{"unknown":true}` + "\n", duplicate, trailing} {
		if _, err := readLocalDelegation(bufio.NewReader(strings.NewReader(text))); err == nil {
			t.Fatalf("accepted invalid binding %q", text)
		}
	}
}

func TestDelegatedRouteAcceptsOnlyHIDKeyboardEdges(t *testing.T) {
	code, _ := hidkeys.Code(4)
	valid := keyboardFrameFor(code, remoteinput.ActionPress)
	if !delegatedKeyboardFrame(valid) {
		t.Fatal("valid HID key refused")
	}
	for _, alter := range []func(*protocol.InputFrame){
		func(f *protocol.InputFrame) { f.Device = uint8(remoteinput.DeviceGamepad) },
		func(f *protocol.InputFrame) { f.Kind = uint8(remoteinput.KindButton) },
		func(f *protocol.InputFrame) { f.Action = uint8(remoteinput.ActionAbsolute) },
		func(f *protocol.InputFrame) { f.Code = 1 },
	} {
		frame := valid
		alter(&frame)
		if delegatedKeyboardFrame(frame) {
			t.Fatalf("non-HID input admitted: %+v", frame)
		}
	}
}

func TestDelegatedWriteDrainsBeforeLeaseReplacement(t *testing.T) {
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	t.Cleanup(manager.Close)
	deadline := time.Now().Add(time.Second)
	for manager.Status().State != "free" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	grant, err := manager.Claim(kitlease.ClaimRequest{RequestID: strings.Repeat("a", 32), Owner: "private", Purpose: "test"})
	if err != nil {
		t.Fatal(err)
	}
	packageID := strings.Repeat("c", 64)
	started, allow := make(chan struct{}), make(chan struct{})
	c, _ := newLocalHIDFeed(t, func(context.Context, string, string, uint64, hidkeys.Rows) error { close(started); <-allow; return nil },
		func(context.Context) (CoreObservation, error) {
			return CoreObservation{Active: true, KeyboardHID: &KeyboardHIDBinding{PackageID: packageID, Generation: 3}}, nil
		})
	c.SetLocalLeaseManager(manager)
	code, _ := hidkeys.Code(4)
	done := make(chan error, 1)
	grantTestLocal(c, localDelegation{LeaseGeneration: grant.Status.Generation, PackageID: packageID, CoreGeneration: 3})
	go func() {
		done <- c.deliverDelegatedLocal(context.Background(), keyboardFrameFor(code, remoteinput.ActionPress), localDelegation{LeaseGeneration: grant.Status.Generation, PackageID: packageID, CoreGeneration: 3})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("write did not start")
	}
	if _, err := manager.Release(grant.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Claim(kitlease.ClaimRequest{RequestID: strings.Repeat("b", 32), Owner: "next", Purpose: "test"}); !errors.Is(err, kitlease.ErrBusy) {
		t.Fatalf("replacement did not wait: %v", err)
	}
	close(allow)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("write did not drain")
	}
	deadline = time.Now().Add(time.Second)
	for manager.Status().State != "free" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if _, err := manager.Claim(kitlease.ClaimRequest{RequestID: strings.Repeat("b", 32), Owner: "next", Purpose: "test"}); err != nil {
		t.Fatal(err)
	}
}

// silentLocalConn exercises the first-read deadline without a device or socket.
type silentLocalConn struct{ deadline time.Time }

func (c *silentLocalConn) Read([]byte) (int, error) {
	time.Sleep(time.Until(c.deadline))
	return 0, os.ErrDeadlineExceeded
}
func (c *silentLocalConn) Write(p []byte) (int, error)       { return len(p), nil }
func (c *silentLocalConn) Close() error                      { return nil }
func (c *silentLocalConn) LocalAddr() net.Addr               { return nil }
func (c *silentLocalConn) RemoteAddr() net.Addr              { return nil }
func (c *silentLocalConn) SetDeadline(t time.Time) error     { c.deadline = t; return nil }
func (c *silentLocalConn) SetReadDeadline(t time.Time) error { c.deadline = t; return nil }
func (c *silentLocalConn) SetWriteDeadline(time.Time) error  { return nil }

func TestSilentLocalClientCannotMonopolizeFeed(t *testing.T) {
	c := newTargetControllerWithSink("127.0.0.1:0", nil)
	done := make(chan struct{})
	go func() { c.readLocal(context.Background(), &silentLocalConn{}, false); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("silent input client retained feed")
	}
}

func TestDelegatedDisconnectRetriesNeutralAndPreservesRemoteHold(t *testing.T) {
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	t.Cleanup(manager.Close)
	deadline := time.Now().Add(time.Second)
	for manager.Status().State != "free" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	grant, err := manager.Claim(kitlease.ClaimRequest{RequestID: strings.Repeat("a", 32), Owner: "private", Purpose: "test"})
	if err != nil {
		t.Fatal(err)
	}
	pkg := strings.Repeat("c", 64)
	var mu sync.Mutex
	var posts []hidkeys.Rows
	var failed int
	c, _ := newLocalHIDFeed(t, func(_ context.Context, _ string, _ string, _ uint64, rows hidkeys.Rows) error {
		mu.Lock()
		defer mu.Unlock()
		posts = append(posts, rows)
		if rows == (hidkeys.Rows{1 << 5}) && failed < 2 {
			failed++
			return errors.New("temporary neutral failure")
		}
		return nil
	}, func(context.Context) (CoreObservation, error) {
		return CoreObservation{Active: true, KeyboardHID: &KeyboardHIDBinding{PackageID: pkg, Generation: 3}}, nil
	})
	c.SetLocalLeaseManager(manager)
	binding := localDelegation{LeaseGeneration: grant.Status.Generation, PackageID: pkg, CoreGeneration: 3}
	grantTestLocal(c, binding)
	pad := remoteinput.Event{Player: 0, Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}
	if err := c.ports.sources[sourceLocal].Apply(pad); err != nil {
		t.Fatal(err)
	}
	padBefore := c.ports.sources[sourceLocal].SnapshotForPlayer(0)
	localCode, _ := hidkeys.Code(4)
	remoteCode, _ := hidkeys.Code(5)
	if err := c.deliverDelegatedLocal(context.Background(), keyboardFrameFor(localCode, remoteinput.ActionPress), binding); err != nil {
		t.Fatal(err)
	}
	if err := c.ports.hid.applyFrom(context.Background(), sourceRemote, keyboardFrameFor(remoteCode, remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { c.retireDelegatedLocal(context.Background(), binding); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("neutral was not retried")
	}
	if got := c.ports.sources[sourceLocal].SnapshotForPlayer(0); !reflect.DeepEqual(got, padBefore) {
		t.Fatalf("keyboard retirement changed local pad: before=%+v after=%+v", padBefore, got)
	}
	mu.Lock()
	defer mu.Unlock()
	if failed != 2 || len(posts) < 5 || posts[len(posts)-1] != (hidkeys.Rows{1 << 5}) {
		t.Fatalf("remote hold or retries lost: failures=%d posts=%+v", failed, posts)
	}
}

func TestDelegatedNeutralRetryStopsForLeaseRevocation(t *testing.T) {
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	t.Cleanup(manager.Close)
	deadline := time.Now().Add(time.Second)
	for manager.Status().State != "free" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	grant, err := manager.Claim(kitlease.ClaimRequest{RequestID: strings.Repeat("a", 32), Owner: "private", Purpose: "test"})
	if err != nil {
		t.Fatal(err)
	}
	pkg := strings.Repeat("c", 64)
	var fail atomic.Bool
	failed := make(chan struct{}, 1)
	c, _ := newLocalHIDFeed(t, func(context.Context, string, string, uint64, hidkeys.Rows) error {
		if fail.Load() {
			select {
			case failed <- struct{}{}:
			default:
			}
			return errors.New("unavailable")
		}
		return nil
	}, func(context.Context) (CoreObservation, error) {
		return CoreObservation{Active: true, KeyboardHID: &KeyboardHIDBinding{PackageID: pkg, Generation: 3}}, nil
	})
	c.SetLocalLeaseManager(manager)
	binding := localDelegation{LeaseGeneration: grant.Status.Generation, PackageID: pkg, CoreGeneration: 3}
	grantTestLocal(c, binding)
	code, _ := hidkeys.Code(4)
	if err := c.deliverDelegatedLocal(context.Background(), keyboardFrameFor(code, remoteinput.ActionPress), binding); err != nil {
		t.Fatal(err)
	}
	fail.Store(true)
	done := make(chan struct{})
	go func() { c.retireDelegatedLocal(context.Background(), binding); close(done) }()
	select {
	case <-failed:
	case <-time.After(time.Second):
		t.Fatal("neutral failure not observed")
	}
	if _, err := manager.Release(grant.Token); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("retry did not stop for revoke")
	}
	deadline = time.Now().Add(time.Second)
	for manager.Status().State != "free" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if manager.Status().State != "free" {
		t.Fatal("revocation failed to drain retry")
	}
}

func TestDelegatedNeutralRetryStopsAtObservedCoreReplacement(t *testing.T) {
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	t.Cleanup(manager.Close)
	deadline := time.Now().Add(time.Second)
	for manager.Status().State != "free" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	grant, err := manager.Claim(kitlease.ClaimRequest{RequestID: strings.Repeat("a", 32), Owner: "private", Purpose: "test"})
	if err != nil {
		t.Fatal(err)
	}
	pkg := strings.Repeat("c", 64)
	var generation atomic.Uint64
	generation.Store(3)
	var fail atomic.Bool
	var writes atomic.Int32
	failed := make(chan struct{}, 1)
	c, _ := newLocalHIDFeed(t, func(context.Context, string, string, uint64, hidkeys.Rows) error {
		writes.Add(1)
		if fail.Load() {
			select {
			case failed <- struct{}{}:
			default:
			}
			return errors.New("unavailable")
		}
		return nil
	}, func(context.Context) (CoreObservation, error) {
		return CoreObservation{Active: true, KeyboardHID: &KeyboardHIDBinding{PackageID: pkg, Generation: generation.Load()}}, nil
	})
	c.SetLocalLeaseManager(manager)
	binding := localDelegation{LeaseGeneration: grant.Status.Generation, PackageID: pkg, CoreGeneration: 3}
	grantTestLocal(c, binding)
	code, _ := hidkeys.Code(4)
	if err := c.deliverDelegatedLocal(context.Background(), keyboardFrameFor(code, remoteinput.ActionPress), binding); err != nil {
		t.Fatal(err)
	}
	fail.Store(true)
	done := make(chan struct{})
	go func() { c.retireDelegatedLocal(context.Background(), binding); close(done) }()
	select {
	case <-failed:
	case <-time.After(time.Second):
		t.Fatal("neutral failure not observed")
	}
	generation.Store(4)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("retry did not stop for replacement")
	}
	if got := writes.Load(); got != 2 {
		t.Fatalf("old neutral posted into replacement: writes=%d", got)
	}
}

func TestOwnerGrantSurvivesRemoteDetachAndEndsAtCoreReplacement(t *testing.T) {
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	t.Cleanup(manager.Close)
	deadline := time.Now().Add(time.Second)
	for manager.Status().State != "free" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	grant, err := manager.Claim(kitlease.ClaimRequest{RequestID: strings.Repeat("a", 32), Owner: "private", Purpose: "test"})
	if err != nil {
		t.Fatal(err)
	}
	pkg := strings.Repeat("c", 64)
	var writes []hidkeys.Rows
	c, _ := newLocalHIDFeed(t, func(_ context.Context, _ string, _ string, _ uint64, rows hidkeys.Rows) error {
		writes = append(writes, rows)
		return nil
	}, func(context.Context) (CoreObservation, error) {
		return CoreObservation{Active: true, KeyboardHID: &KeyboardHIDBinding{PackageID: pkg, Generation: 3}}, nil
	})
	c.SetLocalLeaseManager(manager)
	expected := KeyboardHIDBinding{PackageID: pkg, Generation: 3}
	c.active = &lease{session: 7, executionGeneration: grant.Status.Generation, keyboardBinding: &expected}
	ctx := kitlease.WithAdmission(kitlease.WithGeneration(context.Background(), grant.Status.Generation))
	if err := c.GrantLocalKeyboard(context.Background(), 7, expected); err == nil {
		t.Fatal("unadmitted caller granted local input")
	}
	if err := c.GrantLocalKeyboard(ctx, 8, expected); err == nil {
		t.Fatal("wrong input session granted")
	}
	if err := c.GrantLocalKeyboard(kitlease.WithAdmission(kitlease.WithGeneration(context.Background(), strings.Repeat("d", 64))), 7, expected); err == nil {
		t.Fatal("foreign lease granted")
	}
	if err := c.GrantLocalKeyboard(ctx, 7, KeyboardHIDBinding{PackageID: pkg, Generation: 4}); err == nil {
		t.Fatal("wrong core generation granted")
	}
	if err := c.GrantLocalKeyboard(ctx, 7, expected); err != nil {
		t.Fatal(err)
	}
	binding := localDelegation{LeaseGeneration: grant.Status.Generation, PackageID: pkg, CoreGeneration: 3}
	code, _ := hidkeys.Code(0xe1)
	if err := c.deliverDelegatedLocal(context.Background(), keyboardFrameFor(code, remoteinput.ActionPress), binding); err != nil {
		t.Fatal(err)
	}
	if err := c.Detach(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if !c.localKeyboardAdmitted(binding) {
		t.Fatal("remote detach withdrew local hold")
	}
	if len(writes) != 1 || writes[0][8] != (1<<1) {
		t.Fatalf("remote detach changed local hold: %+v", writes)
	}
	if err := c.deliverDelegatedLocal(context.Background(), keyboardFrameFor(code, remoteinput.ActionRelease), binding); err != nil {
		t.Fatal(err)
	}
	finish, err := c.BeginCoreReplacement(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := finish(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if c.localKeyboardAdmitted(binding) {
		t.Fatal("core replacement retained grant")
	}
}
