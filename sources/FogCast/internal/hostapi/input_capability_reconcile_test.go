package hostapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

// Status reconciliation, not an explicit attach, must replace the live stream
// when fes.gamepad.ports, fes.keypad.ports, or fes.mouse.relative changes, and
// the replacement must install those flags on the real input gates.
func TestStatusReconciliationReplacesStreamWhenCapabilitiesChange(t *testing.T) {
	const (
		player2 = `{"event":{"Player":1,"Device":1,"Kind":1,"Action":1,"Code":103,"Value":0}}`
		keypad  = `{"event":{"Device":1,"Kind":1,"Action":1,"Code":120,"Value":0}}`
		mouse   = `{"event":{"Device":2,"Kind":4,"Action":3,"Code":0,"Value":1}}`
	)
	cases := []struct {
		name    string
		drop    string
		gated   string
		remains string
		code    uint16
		device  uint8
	}{
		{name: "gamepad ports", drop: "fes.gamepad.ports", gated: player2, remains: mouse, code: 0, device: 2},
		{name: "keypad ports", drop: "fes.keypad.ports", gated: keypad, remains: player2, code: 103, device: 1},
		{name: "relative mouse", drop: "fes.mouse.relative", gated: mouse, remains: keypad, code: 120, device: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			core := "fes.c64"
			service := &fakeService{status: protocol.Status{
				State: protocol.StateActive, Development: true, ObservedCore: &core,
				CorePackage: &protocol.CorePackageStatus{
					PackageID: strings.Repeat("a", 64), Generation: 7, Gamepad: true,
					ABI: protocol.RuntimeContract{ID: "fes.computer", Major: 1},
					ActiveInterfaces: []protocol.RuntimeInterface{
						{ID: "fes.keyboard.hid", Major: 1},
						{ID: "fes.gamepad.ports", Major: 1},
						{ID: "fes.keypad.ports", Major: 1},
						{ID: "fes.mouse.relative", Major: 1},
					},
				},
			}}
			rig := newStreamRig(t)
			handler := hostapi.New(service, hostapi.WithRemoteInput(rig.input))

			first := sessionStreamID(t, serve(t, handler, http.MethodGet, "/api/v1/session"))
			if again := sessionStreamID(t, serve(t, handler, http.MethodGet, "/api/v1/session")); again != first {
				t.Fatalf("unchanged status replaced stream %s with %s", first, again)
			}
			if rec := postSessionInput(handler, tc.gated); rec.Code != http.StatusOK {
				t.Fatalf("initial gate rejected %s: %d %s", tc.drop, rec.Code, rec.Body.String())
			}
			awaitSinkFrames(t, rig, 1)

			service.status.CorePackage.ActiveInterfaces = dropInterface(t, service.status.CorePackage.ActiveInterfaces, tc.drop)
			replaced := sessionStreamID(t, serve(t, handler, http.MethodGet, "/api/v1/session"))
			if replaced == "" || replaced == first {
				t.Fatalf("capability change kept stream %s", first)
			}
			held := rig.sink.count()
			if rec := postSessionInput(handler, tc.gated); rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "TARGET_UNAVAILABLE") {
				t.Fatalf("updated %s gate still accepted the event: %d %s", tc.drop, rec.Code, rec.Body.String())
			}
			if rig.sink.count() != held {
				t.Fatalf("rejected %s event reached the replacement stream", tc.drop)
			}
			if rec := postSessionInput(handler, tc.remains); rec.Code != http.StatusOK {
				t.Fatalf("replacement stream rejected a remaining capability: %d %s", rec.Code, rec.Body.String())
			}
			frame := awaitSinkFrame(t, rig, held)
			if frame.Code != tc.code || frame.Device != tc.device {
				t.Fatalf("replacement stream delivered %+v, want code %d device %d", frame, tc.code, tc.device)
			}
		})
	}
}

func sessionStreamID(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("session=%d %s", response.Code, response.Body.String())
	}
	var body struct {
		Input *struct {
			SessionID string `json:"session_id"`
			State     string `json:"state"`
			Ready     bool   `json:"ready"`
		} `json:"input"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Input == nil || !body.Input.Ready || body.Input.State != "attached" || body.Input.SessionID == "" {
		t.Fatalf("input stream=%s", response.Body.String())
	}
	return body.Input.SessionID
}

func dropInterface(t *testing.T, interfaces []protocol.RuntimeInterface, id string) []protocol.RuntimeInterface {
	t.Helper()
	kept := make([]protocol.RuntimeInterface, 0, len(interfaces)-1)
	for _, contract := range interfaces {
		if contract.ID != id {
			kept = append(kept, contract)
		}
	}
	if len(kept) != len(interfaces)-1 {
		t.Fatalf("interface %s was not active", id)
	}
	return kept
}

func awaitSinkFrames(t *testing.T, rig *streamRig, n int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for rig.sink.count() < n && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if rig.sink.count() < n {
		t.Fatalf("bridge applied %d frames, want %d", rig.sink.count(), n)
	}
}

func awaitSinkFrame(t *testing.T, rig *streamRig, before int) protocol.InputFrame {
	t.Helper()
	awaitSinkFrames(t, rig, before+1)
	rig.sink.mu.Lock()
	defer rig.sink.mu.Unlock()
	return rig.sink.events[len(rig.sink.events)-1]
}
