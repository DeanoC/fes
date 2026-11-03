// Offline, source-matched latency reproduction; all endpoints are local fakes.
package httpapi_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/appliance/softwarecontrol"
	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/internal/testsock"
	"github.com/DeanoC/FogCast/targetclient"
)

type measuredRuntimeControl struct {
	delay      time.Duration
	mu         sync.Mutex
	start, end time.Time
	completed  chan struct{}
	contextErr error
}

func (c *measuredRuntimeControl) Protocol2Status(ctx context.Context) (misterruntime.Protocol2Response, error) {
	c.mu.Lock()
	c.start = time.Now()
	c.mu.Unlock()
	defer func() { c.mu.Lock(); c.end = time.Now(); c.mu.Unlock(); close(c.completed) }()
	timer := time.NewTimer(c.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		c.mu.Lock()
		c.contextErr = ctx.Err()
		c.mu.Unlock()
		return misterruntime.Protocol2Response{}, ctx.Err()
	case <-timer.C:
	}
	return misterruntime.Protocol2Response{Protocol: 2, OK: true, State: "idle", Execution: "none", Version: "repro", Capabilities: misterruntime.Protocol2Capabilities{ProgrammingProfiles: []string{}, ABIs: []misterruntime.Protocol2ABI{}, ActiveInterfaces: []misterruntime.Protocol2Interface{}}}, nil
}
func (*measuredRuntimeControl) Protocol2Stop(context.Context) (misterruntime.Protocol2Response, error) {
	return misterruntime.Protocol2Response{}, errors.New("unexpected stop")
}
func (*measuredRuntimeControl) Protocol2LoadDevelopmentRBF(context.Context, string) (misterruntime.Protocol2Response, error) {
	return misterruntime.Protocol2Response{}, errors.New("unexpected load")
}

func TestOfflineAgentHealthSerialDelay(t *testing.T) {
	control := &measuredRuntimeControl{delay: 200 * time.Millisecond, completed: make(chan struct{})}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 250*time.Millisecond)
	coordinator := agent.New(runtime, time.Second, time.Second)
	var supervisorStart, supervisorEnd time.Time
	selected := strings.Repeat("a", 64)
	socket := softwareSocket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		supervisorStart = time.Now()
		time.Sleep(200 * time.Millisecond)
		supervisorEnd = time.Now()
		_ = json.NewEncoder(w).Encode(softwarecontrol.Status{State: softwarecontrol.State{Version: 1, Selected: selected, Receipts: map[string]softwarecontrol.Receipt{}}, ActiveBundle: selected, BaseIdentity: "base-contract"})
	}))
	handler := httpapi.New(coordinator, "secret", "repro", nil, httpapi.WithKitSoftware(socket), httpapi.WithTargetID("01234567-89ab-cdef-0123-456789abcdef"))
	server := httptest.NewServer(handler)
	defer server.Close()
	base, _ := url.Parse(server.URL)
	client := targetclient.NewClient(base, "secret", server.Client())
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()
	start := time.Now()
	health, err := client.Health(ctx)
	elapsed := time.Since(start)
	<-control.completed
	if err != nil || health.TargetID != "01234567-89ab-cdef-0123-456789abcdef" || health.SoftwareIdentityV1 == nil || !health.Ready || health.SoftwareIdentityV1.Selected != selected || health.SoftwareIdentityV1.ActiveBundle != selected || health.SoftwareIdentityV1.BaseIdentity != "base-contract" {
		t.Fatalf("health response err=%v", err)
	}
	control.mu.Lock()
	runtimeStart, runtimeEnd := control.start, control.end
	control.mu.Unlock()
	if supervisorStart.Before(runtimeEnd) {
		t.Fatal("supervisor was sampled before runtime health completed")
	}
	if elapsed < 380*time.Millisecond {
		t.Fatalf("expected sequential work, elapsed %s", elapsed)
	}
	t.Logf("runtime=%v supervisor=%v total=%v (controlled local delays; not kit attribution)", runtimeEnd.Sub(runtimeStart), supervisorEnd.Sub(supervisorStart), elapsed)
}

func TestOfflineCanceledHealthStopsRuntimeWorkPromptly(t *testing.T) {
	control := &measuredRuntimeControl{delay: 5 * time.Second, completed: make(chan struct{})}
	runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 250*time.Millisecond)
	coordinator := agent.New(runtime, time.Second, time.Second)
	var supervisorCalls atomic.Int32
	socket := softwareSocket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { supervisorCalls.Add(1); w.WriteHeader(http.StatusOK) }))
	handler := httpapi.New(coordinator, "secret", "repro", nil, httpapi.WithKitSoftware(socket), httpapi.WithTargetID("01234567-89ab-cdef-0123-456789abcdef"))
	handlerDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { defer close(handlerDone); handler.ServeHTTP(w, r) }))
	defer server.Close()
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/v1/health", nil)
	req.Header.Set("Authorization", "Bearer secret")
	ctx, cancel := context.WithCancel(context.Background())
	req = req.WithContext(ctx)
	result := make(chan error, 1)
	go func() {
		resp, err := server.Client().Do(req)
		if resp != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		result <- err
	}()
	deadline := time.Now().Add(time.Second)
	for {
		control.mu.Lock()
		started := control.start
		control.mu.Unlock()
		if !started.IsZero() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("runtime status never started")
		}
		time.Sleep(time.Millisecond)
	}
	canceled := time.Now()
	cancel()
	if err := <-result; err == nil {
		t.Fatal("client unexpectedly succeeded")
	}
	select {
	case <-control.completed:
	case <-time.After(time.Second):
		t.Fatal("runtime work did not stop promptly after the HTTP request was canceled")
	}
	control.mu.Lock()
	ended := control.end
	statusErr := control.contextErr
	control.mu.Unlock()
	if !errors.Is(statusErr, context.Canceled) {
		t.Fatalf("runtime status did not receive HTTP cancellation: %v", statusErr)
	}
	remaining := ended.Sub(canceled)
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("health handler did not stop")
	}
	if supervisorCalls.Load() != 0 {
		t.Fatal("supervisor was queried after cancellation")
	}
	t.Logf("runtime status stopped %v after client cancellation", remaining)
}

// Exercise cancellation across the real runtime Unix-socket client as well as
// the HTTP/coordinator path. The synthetic local server never responds.
func TestCanceledHealthClosesRealRuntimeSocket(t *testing.T) {
	socket := testsock.Path(t, "runtime.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	received := make(chan struct{})
	closed := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			closed <- err
			return
		}
		defer conn.Close()
		if _, err := bufio.NewReader(conn).ReadBytes('\n'); err != nil {
			closed <- err
			return
		}
		close(received)
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		var one [1]byte
		_, err = conn.Read(one[:])
		closed <- err
	}()
	runtime := misterruntime.NewRuntime(misterruntime.NewClient(socket), "", time.Millisecond, 2*time.Second)
	coordinator := agent.New(runtime, time.Second, time.Second)
	server := httptest.NewServer(httpapi.New(coordinator, "secret", "repro", nil))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/health", nil)
	result := make(chan error, 1)
	go func() {
		resp, err := server.Client().Do(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
		result <- err
	}()
	select {
	case <-received:
	case <-time.After(time.Second):
		t.Fatal("runtime request not received")
	}
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("canceled client succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("HTTP client did not stop")
	}
	select {
	case err := <-closed:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("runtime connection did not close: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime socket remained open")
	}
}
