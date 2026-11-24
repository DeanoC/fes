package input

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
)

// focusAdapterControl models the runtime's successful focus/neutral result.
// It does not model hardware or the local runtime socket.
type focusAdapterControl struct {
	mu       sync.Mutex
	response misterruntime.Protocol2Response
	focus    protocol.SessionFocus
	posts    []misterruntime.KeyboardHIDRows
	entered  chan struct{}
	proceed  chan struct{}
}

func (c *focusAdapterControl) Protocol2Status(context.Context) (misterruntime.Protocol2Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.response, nil
}
func (c *focusAdapterControl) Protocol2Stop(context.Context) (misterruntime.Protocol2Response, error) {
	return c.Protocol2Status(context.Background())
}
func (c *focusAdapterControl) Protocol2LoadDevelopmentRBF(context.Context, string) (misterruntime.Protocol2Response, error) {
	return c.Protocol2Status(context.Background())
}
func (c *focusAdapterControl) SetSessionDisplay(_ context.Context, _ string, _ uint64, display protocol.SessionDisplay) (misterruntime.Protocol2Response, error) {
	c.mu.Lock()
	c.focus = display.Focus
	menu := *c.response.MenuDisplay
	menu.Focus, menu.Available = display.Focus, display.Visible
	c.response.MenuDisplay = &menu
	response := c.response
	c.mu.Unlock()
	if display.Focus == protocol.SessionFocusLauncher && c.entered != nil {
		close(c.entered)
		<-c.proceed
	}
	return response, nil
}
func (c *focusAdapterControl) SetKeyboardHID(_ context.Context, _, _ string, _ uint64, rows misterruntime.KeyboardHIDRows) (misterruntime.Protocol2Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.posts = append(c.posts, rows)
	return c.response, nil
}

func TestV2HIDRespectsRuntimeAdapterDisplayFocus(t *testing.T) {
	f := newV2BackendFixture(t)
	instance, id, generation := f.req.RuntimeInstance, f.req.PackageID, f.req.CoreGeneration
	response := misterruntime.Protocol2Response{
		Protocol: 2, OK: true, State: "running_development", Execution: "development", RuntimeInstance: &instance, Generation: &generation,
		ActivePackage: &misterruntime.Protocol2ActivePackage{PackageID: id, Descriptor: corepackage.Descriptor{ABI: corepackage.Contract{ID: "fes.computer", Major: 1}}},
		Capabilities: misterruntime.Protocol2Capabilities{ActiveInterfaces: []misterruntime.Protocol2Interface{
			{ID: "fes.keyboard.hid", Major: 1}, {ID: "fes.memory.hps-ddr", Major: 1},
			{ID: "fes.video.session-display", Major: 1}, {ID: "fes.media.zx81-tape", Major: 1},
		}},
		MenuDisplay: &misterruntime.Protocol2MenuDisplay{Session: true, Available: true, PackageID: &id, CoreGeneration: generation, Generation: 1, Focus: protocol.SessionFocusMachine},
	}
	control := &focusAdapterControl{response: response, focus: protocol.SessionFocusMachine, entered: make(chan struct{}), proceed: make(chan struct{})}
	runtime := misterruntime.NewRuntime(control, "", 0, 0)
	f.c.keyboard = NewKeyboardSink()
	f.c.ports.keys = f.c.keyboard
	f.c.ports.hid.setPoster(func(ctx context.Context, instance, id string, generation uint64, rows hidkeys.Rows) error {
		return runtime.SetKeyboardHID(ctx, instance, id, generation, misterruntime.KeyboardHIDRows(rows))
	})
	runtime.ConfigureSessionDisplayFocus(f.c)
	backend := delegatedV2Backend{f.c}
	witness, err := backend.Open(context.Background(), f.req)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Edge(context.Background(), witness, &localhid.Delta{Usage: 0xe1, Down: true}); err != nil {
		t.Fatal(err)
	}
	control.mu.Lock()
	beforeFocus := len(control.posts)
	control.mu.Unlock()
	if beforeFocus != 1 {
		t.Fatalf("initial press posts = %d", beforeFocus)
	}
	binding := protocol.DevelopmentMediaBinding{PackageID: id, Generation: generation}
	focusDone := make(chan *protocol.APIError, 1)
	var release sync.Once
	unblock := func() { release.Do(func() { close(control.proceed) }) }
	defer unblock()
	go func() {
		focusDone <- runtime.SetSessionDisplay(context.Background(), protocol.SessionDisplayFocused, binding)
	}()
	select {
	case <-control.entered:
	case err := <-focusDone:
		t.Fatalf("focus did not reach runtime: %v", err)
	case <-time.After(time.Second):
		t.Fatal("focus did not reach runtime")
	}
	transitionEdge := make(chan error, 1)
	go func() {
		transitionEdge <- backend.Edge(context.Background(), witness, &localhid.Delta{Usage: 4, Down: true})
	}()
	select {
	case err := <-transitionEdge:
		if err == nil {
			t.Fatal("v2 edge admitted during the focus transition")
		}
	case <-time.After(time.Second):
		t.Fatal("v2 edge did not reject while focus was in progress")
	}
	control.mu.Lock()
	duringTransition := len(control.posts)
	control.mu.Unlock()
	if duringTransition != beforeFocus {
		t.Fatal("keyboard wrote during the focus transition")
	}
	unblock()
	if err := <-focusDone; err != nil {
		t.Fatalf("launcher focus: %v", err)
	}
	if err := backend.Edge(context.Background(), witness, &localhid.Delta{Usage: 4, Down: true}); err == nil {
		t.Fatal("v2 edge admitted during launcher focus")
	}
	newRequest := f.req
	newRequest.QueueEpoch++
	if _, err := backend.Open(context.Background(), newRequest); err == nil {
		t.Fatal("new v2 source admitted during launcher focus")
	}
	control.mu.Lock()
	focusedPosts := len(control.posts)
	actualFocus := control.focus
	control.mu.Unlock()
	if actualFocus != protocol.SessionFocusLauncher || focusedPosts != beforeFocus {
		t.Fatalf("focus=%s; keyboard posts changed %d→%d", actualFocus, beforeFocus, focusedPosts)
	}
	if err := runtime.SetSessionDisplay(context.Background(), protocol.SessionDisplayShown, binding); err != nil {
		t.Fatalf("machine focus: %v", err)
	}
	control.mu.Lock()
	afterRestore := len(control.posts)
	control.mu.Unlock()
	// The held Shift was fenced at focus handoff. A repeat must remain
	// suppressed until the real release, after which a fresh press may post.
	if err := backend.Edge(context.Background(), witness, &localhid.Delta{Usage: 0xe1, Down: true}); err != nil {
		t.Fatal(err)
	}
	control.mu.Lock()
	afterRepeat := len(control.posts)
	control.mu.Unlock()
	if afterRepeat != afterRestore {
		t.Fatal("focus-suppressed held Shift replayed")
	}
	if err := backend.Edge(context.Background(), witness, &localhid.Delta{Usage: 0xe1, Down: false}); err != nil {
		t.Fatal(err)
	}
	if err := backend.Edge(context.Background(), witness, &localhid.Delta{Usage: 0xe1, Down: true}); err != nil {
		t.Fatal(err)
	}
	control.mu.Lock()
	last := control.posts[len(control.posts)-1]
	control.mu.Unlock()
	if last[8]&2 == 0 {
		t.Fatal("fresh Shift press was not posted after release")
	}
}
