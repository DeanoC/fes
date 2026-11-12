package hostapi_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

// legacyReceiptBridge accepts the original version-1 hello but closes on a
// flagged receipt probe, as a pre-receipt bridge does for an unknown flag.
// A later dial models the same legacy bridge accepting a replacement stream.
type legacyReceiptBridge struct {
	ready      chan struct{}
	dials      atomic.Int32
	hellos     chan map[string]json.RawMessage
	firstFrame chan protocol.InputFrame
	errors     chan error
}

func (b *legacyReceiptBridge) Ready() <-chan struct{}     { return b.ready }
func (b *legacyReceiptBridge) Endpoint() string           { return "" }
func (b *legacyReceiptBridge) Stop(context.Context) error { return nil }
func (b *legacyReceiptBridge) Dial(context.Context) (net.Conn, error) {
	client, peer := net.Pipe()
	dial := b.dials.Add(1)
	go func() {
		defer peer.Close()
		reader := bufio.NewReader(peer)
		line, err := reader.ReadBytes('\n')
		if err != nil {
			b.errors <- fmt.Errorf("hello: %w", err)
			return
		}
		var hello map[string]json.RawMessage
		if err := json.Unmarshal(line, &hello); err != nil {
			b.errors <- fmt.Errorf("hello JSON: %w", err)
			return
		}
		b.hellos <- hello
		if _, err := io.WriteString(peer, "{\"ok\":true}\n"); err != nil {
			b.errors <- fmt.Errorf("welcome: %w", err)
			return
		}
		frame, err := protocol.DecodeInputFrame(reader, 4096)
		if dial == 1 {
			if err != nil {
				b.errors <- fmt.Errorf("first frame: %w", err)
				return
			}
			b.firstFrame <- frame
			return
		}
		// Reconnection may replay state. Consume it until the source closes.
		for err == nil {
			_, err = protocol.DecodeInputFrame(reader, 4096)
		}
	}()
	return client, nil
}

func TestLegacyBridgeLaunchSurvivesRejectedReceiptProbe(t *testing.T) {
	gameID, core := "legacy-test", "SNES"
	system := protocol.SystemSNES
	status := protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ObservedCore: &core}
	service := &fakeService{launch: protocol.CachedLaunchResponse{Status: status}, status: status,
		stopped: protocol.Status{State: protocol.StateIdle}, stopCalled: make(chan struct{})}
	bridge := &legacyReceiptBridge{ready: make(chan struct{}), hellos: make(chan map[string]json.RawMessage, 2),
		firstFrame: make(chan protocol.InputFrame, 1), errors: make(chan error, 4)}
	close(bridge.ready)
	input, err := host.NewRemoteInput(host.RemoteInputConfig{
		Starter:           host.BridgeStarterFunc(func(context.Context, host.BridgeSpec) (host.BridgeHandle, error) { return bridge, nil }),
		HeartbeatInterval: time.Hour, DialTimeout: time.Second, ReconnectGrace: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = input.Close() })
	handler := hostapi.New(service, hostapi.WithRemoteInput(input))

	launch := launchSession(t, handler, gameID)
	var launchResult struct {
		Input struct {
			State string `json:"state"`
		} `json:"input"`
	}
	err = json.Unmarshal(launch.Body.Bytes(), &launchResult)
	if launch.Code != http.StatusOK || err != nil || launchResult.Input.State != "attached" {
		t.Fatalf("launch over legacy bridge: %d %s", launch.Code, launch.Body.String())
	}
	hello := <-bridge.hellos
	var helloVersion uint8
	if len(hello) != 4 || json.Unmarshal(hello["version"], &helloVersion) != nil || helloVersion != 1 ||
		hello["session"] == nil || hello["core"] == nil || hello["proof"] == nil {
		t.Fatalf("legacy-compatible version-1 hello has unexpected fields: %+v", hello)
	}
	select {
	case <-service.stopCalled:
		t.Fatal("legacy launch stopped the machine")
	default:
	}
	sessionID := input.Status().SessionID

	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/input/event", strings.NewReader(remotePadEvent))
	request.Host = "127.0.0.1"
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "INPUT_UNAVAILABLE") {
		t.Fatalf("legacy confirmed input: %d %s", response.Code, response.Body.String())
	}
	select {
	case frame := <-bridge.firstFrame:
		if frame.Header.Type != protocol.InputTypePing || frame.Header.Flags != protocol.InputFlagReceiptRequested {
			t.Fatalf("first frame dispatched input before receipt probe: %+v", frame)
		}
	case err := <-bridge.errors:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("receipt probe not observed")
	}
	select {
	case <-service.stopCalled:
		t.Fatal("rejected receipt probe stopped the machine")
	default:
	}
	statusResponse := serve(t, handler, http.MethodGet, "/api/v1/session")
	if statusResponse.Code != http.StatusOK || !strings.Contains(statusResponse.Body.String(), `"state":"active"`) {
		t.Fatalf("machine did not remain active: %d %s", statusResponse.Code, statusResponse.Body.String())
	}

	// A fresh bridge stream can be claimed by the launcher after the failed
	// desktop probe: failed negotiation must not strand desktop ownership.
	beforeReconnect := input.Status().Metrics.FramesSent
	source, err := input.ClaimSource(sessionID)
	if err != nil {
		t.Fatalf("launcher claim after failed probe: %v", err)
	}
	if status := input.Status(); status.Source != "launcher" || status.Metrics.FramesSent != beforeReconnect {
		t.Fatalf("launcher claim replayed rejected input or failed to acquire ownership: %+v", status)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	if bridge.dials.Load() != 2 {
		t.Fatalf("bridge dials = %d, want reconnect", bridge.dials.Load())
	}
}
