// SPDX-License-Identifier: GPL-3.0-or-later

package agentapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targethttp"
	"github.com/DeanoC/fes/platform/embedded/tamago/network"
	gnet "github.com/usbarmory/go-net"
)

type wireDev struct {
	in  <-chan []byte
	out chan<- []byte
}

func (d wireDev) Receive(buf []byte) (int, error) {
	select {
	case f := <-d.in:
		return copy(buf, f), nil
	default:
		return 0, nil
	}
}

func (d wireDev) Transmit(frame []byte) error {
	select {
	case d.out <- bytes.Clone(frame):
		return nil
	default:
		return errors.New("queue full")
	}
}

func (d wireDev) Err() error { return nil }

type tamagoController struct {
	mu     sync.Mutex
	state  protocol.State
	coreID string
}

func (c *tamagoController) Health(agentVersion string) protocol.Health {
	return protocol.Health{
		APIVersion:   "1",
		AgentVersion: agentVersion,
		Ready:        true,
	}
}

func (c *tamagoController) Status() protocol.Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	var core *string
	if c.coreID != "" {
		v := c.coreID
		core = &v
	}
	return protocol.Status{
		State:        c.state,
		ObservedCore: core,
	}
}

func (c *tamagoController) Stop(context.Context) (protocol.Status, *protocol.APIError) {
	c.mu.Lock()
	c.state = protocol.StateIdle
	c.coreID = ""
	c.mu.Unlock()
	return c.Status(), nil
}

type tamagoLauncher struct {
	ctrl *tamagoController
}

func (l *tamagoLauncher) Launch(_ context.Context, req targethttp.LaunchRequest) (protocol.Status, *protocol.APIError) {
	l.ctrl.mu.Lock()
	l.ctrl.state = protocol.StateActive
	l.ctrl.coreID = req.CoreID
	l.ctrl.mu.Unlock()
	return l.ctrl.Status(), nil
}

type noopPackageStore struct{}

func (noopPackageStore) List(context.Context) ([]corepackage.Inspection, error) { return nil, nil }
func (noopPackageStore) Read(context.Context, string) (corepackage.Inspection, error) {
	return corepackage.Inspection{}, corepackage.ErrPackageNotFound
}
func (noopPackageStore) Selections(context.Context) ([]protocol.KitCoreSelection, error) {
	return nil, nil
}
func (noopPackageStore) Install(context.Context, int64, io.Reader) (corepackage.Inspection, bool, error) {
	return corepackage.Inspection{}, false, corepackage.ErrInvalidPackage
}
func (noopPackageStore) Select(context.Context, string, protocol.KitCoreSelectionRequest) *protocol.APIError {
	return &protocol.APIError{Code: protocol.CodeCorePackageNotFound, Message: "not found"}
}

func TestFogCastTargetHTTPOverRealLnetoEthernetFrames(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cToS := make(chan []byte, 256)
	sToC := make(chan []byte, 256)
	serverStack := gnet.NewGVisorStack(1)
	clientStack := gnet.NewLnetoStack(gnet.DefaultLnetoStackConfig())

	serverIface := &gnet.Interface{
		Stack:         serverStack,
		NetworkDevice: wireDev{in: cToS, out: sToC},
	}
	if err := serverIface.Init("192.0.2.10/24", "02:aa:00:00:00:01", ""); err != nil {
		t.Fatal(err)
	}
	linkClient, err := network.NewLink(clientStack, wireDev{in: sToC, out: cToS}, "192.0.2.20/24", "02:aa:00:00:00:02", "")
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = serverIface.Start(ctx) }()
	go func() { defer wg.Done(); _ = linkClient.Start(ctx) }()
	defer func() {
		cancel()
		wg.Wait()
	}()

	sock, err := serverStack.Socket(ctx, "tcp4", syscall.AF_INET, syscall.SOCK_STREAM, &net.TCPAddr{IP: net.IPv4(192, 0, 2, 10), Port: 8080}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ln, ok := sock.(net.Listener)
	if !ok {
		t.Fatalf("expected net.Listener, got %T", sock)
	}

	ctrl := &tamagoController{state: protocol.StateIdle}
	handler, err := targethttp.New(targethttp.Config{
		Controller:           ctrl,
		AllowUnauthenticated: true,
		TargetID:             "11111111-2222-4333-8444-555555555555",
		Packages:             noopPackageStore{},
		Launcher:             &tamagoLauncher{ctrl: ctrl},
	})
	if err != nil {
		t.Fatal(err)
	}

	httpSrv := &http.Server{Handler: handler}
	go func() { _ = httpSrv.Serve(ln) }()
	defer func() { _ = httpSrv.Close() }()

	transport := &http.Transport{
		DialContext: func(dctx context.Context, networkName, address string) (net.Conn, error) {
			raw, err := clientStack.Socket(dctx, "tcp4", syscall.AF_INET, syscall.SOCK_STREAM, nil, &net.TCPAddr{IP: net.IPv4(192, 0, 2, 10), Port: 8080})
			if err != nil {
				return nil, err
			}
			conn, ok := raw.(net.Conn)
			if !ok {
				return nil, errors.New("unexpected client conn type")
			}
			return conn, nil
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}

	resp, err := client.Post("http://192.0.2.10:8080/v1/launch", "application/json", strings.NewReader(`{"core_id":"c64"}`))
	if err != nil {
		t.Fatal(err)
	}
	var st protocol.Status
	err = json.NewDecoder(resp.Body).Decode(&st)
	_ = resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK || st.State != protocol.StateActive {
		t.Fatalf("remote launch over lneto frames failed: status=%d st=%+v err=%v", resp.StatusCode, st, err)
	}

	healthResp, err := client.Get("http://192.0.2.10:8080/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	var health protocol.Health
	_ = json.NewDecoder(healthResp.Body).Decode(&health)
	_ = healthResp.Body.Close()
	if healthResp.StatusCode != http.StatusOK || health.TargetID == "" {
		t.Fatalf("health over lneto frames = %d %+v", healthResp.StatusCode, health)
	}
}
