package btagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultControlSocket is the Unix domain socket exposed by fes-bluetooth
	// so local kit UI processes control pairing and device removal without
	// opening a competing BlueZ D-Bus client.
	DefaultControlSocket = "/run/fes-bluetooth.sock"

	defaultPairingDuration = 30 * time.Second
	minPairingDuration     = time.Second
	maxPairingDuration     = 2 * time.Minute
	controlCallTimeout     = 10 * time.Second
)

// DeviceStatus describes one paired or connected controller managed by fes-bluetooth.
type DeviceStatus struct {
	Address   string `json:"address"`
	Name      string `json:"name"`
	Kind      string `json:"kind,omitempty"`
	Connected bool   `json:"connected"`
}

// Status describes the local Bluetooth adapter, active pairing window, and
// known paired controllers.
type Status struct {
	AdapterConnected   bool           `json:"adapter_connected"`
	AdapterName        string         `json:"adapter_name,omitempty"`
	PairingActive      bool           `json:"pairing_active"`
	PairingRemainingMS int64          `json:"pairing_remaining_ms"`
	ConnectedCount     int            `json:"connected_count"`
	Devices            []DeviceStatus `json:"devices,omitempty"`
}

type pairingStartRequest struct {
	DurationMS int64 `json:"duration_ms"`
}

type forgetDeviceRequest struct {
	Address string `json:"address"`
}

type controlError struct {
	code int
	err  error
}

func (e *controlError) Error() string { return e.err.Error() }
func (e *controlError) Unwrap() error { return e.err }

type controlKind uint8

const (
	controlStartPairing controlKind = iota + 1
	controlStopPairing
	controlForgetDevice
)

type controlResult struct {
	status Status
	err    error
}

type controlRequest struct {
	kind     controlKind
	duration time.Duration
	address  string
	reply    chan controlResult
}

// ControlServer serves fes-bluetooth's local control and status HTTP API over a
// Unix domain socket.
type ControlServer struct {
	socketPath string
	listener   net.Listener
	server     *http.Server

	mu    sync.RWMutex
	agent *agent
}

// ListenControl opens a Unix socket HTTP server at socketPath. Existing socket
// files left by a prior crash are removed before binding.
func ListenControl(socketPath string) (*ControlServer, error) {
	socketPath = strings.TrimSpace(socketPath)
	if socketPath == "" {
		return nil, errors.New("btagent: empty control socket path")
	}
	if info, err := os.Lstat(socketPath); err == nil && info.Mode()&os.ModeSocket != 0 {
		_ = os.Remove(socketPath)
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(socketPath, 0600)

	cs := &ControlServer{
		socketPath: socketPath,
		listener:   ln,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", cs.handleStatus)
	mux.HandleFunc("POST /pairing/start", cs.handleStartPairing)
	mux.HandleFunc("POST /pairing/stop", cs.handleStopPairing)
	mux.HandleFunc("POST /devices/forget", cs.handleForgetDevice)
	cs.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		_ = cs.server.Serve(ln)
	}()
	return cs, nil
}

// Close shuts down the control server and removes its socket file.
func (cs *ControlServer) Close() error {
	if cs == nil {
		return nil
	}
	var err error
	if cs.server != nil {
		err = cs.server.Close()
	} else if cs.listener != nil {
		err = cs.listener.Close()
	}
	if cs.socketPath != "" {
		if info, lstatErr := os.Lstat(cs.socketPath); lstatErr == nil && info.Mode()&os.ModeSocket != 0 {
			_ = os.Remove(cs.socketPath)
		}
	}
	return err
}

func (cs *ControlServer) attach(a *agent) {
	if cs == nil {
		return
	}
	cs.mu.Lock()
	cs.agent = a
	cs.mu.Unlock()
}

func (cs *ControlServer) detach(a *agent) {
	if cs == nil {
		return
	}
	cs.mu.Lock()
	if cs.agent == a {
		cs.agent = nil
	}
	cs.mu.Unlock()
}

func (cs *ControlServer) currentAgent() *agent {
	if cs == nil {
		return nil
	}
	cs.mu.RLock()
	a := cs.agent
	cs.mu.RUnlock()
	return a
}

func (cs *ControlServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	a := cs.currentAgent()
	if a == nil {
		writeJSONStatus(w, http.StatusOK, Status{})
		return
	}
	writeJSONStatus(w, http.StatusOK, a.snapshot())
}

func (cs *ControlServer) handleStartPairing(w http.ResponseWriter, r *http.Request) {
	var body pairingStartRequest
	if err := decodeOptionalJSON(r.Body, &body); err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	duration := defaultPairingDuration
	if body.DurationMS > 0 {
		duration = time.Duration(body.DurationMS) * time.Millisecond
		if duration < minPairingDuration {
			duration = minPairingDuration
		}
		if duration > maxPairingDuration {
			duration = maxPairingDuration
		}
	}
	cs.dispatchControl(w, r.Context(), controlRequest{
		kind:     controlStartPairing,
		duration: duration,
	})
}

func (cs *ControlServer) handleStopPairing(w http.ResponseWriter, r *http.Request) {
	cs.dispatchControl(w, r.Context(), controlRequest{
		kind: controlStopPairing,
	})
}

func (cs *ControlServer) handleForgetDevice(w http.ResponseWriter, r *http.Request) {
	var body forgetDeviceRequest
	if err := decodeOptionalJSON(r.Body, &body); err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	addr := strings.ToUpper(strings.TrimSpace(body.Address))
	if addr == "" {
		writeJSONError(w, http.StatusBadRequest, errors.New("bluetooth address is required"))
		return
	}
	cs.dispatchControl(w, r.Context(), controlRequest{
		kind:    controlForgetDevice,
		address: addr,
	})
}

func (cs *ControlServer) dispatchControl(w http.ResponseWriter, reqCtx context.Context, req controlRequest) {
	a := cs.currentAgent()
	if a == nil {
		writeJSONError(w, http.StatusServiceUnavailable, errors.New("bluetooth agent is not running"))
		return
	}
	ctx, cancel := context.WithTimeout(reqCtx, controlCallTimeout)
	defer cancel()
	st, err := a.submitControl(ctx, req)
	if err != nil {
		code := http.StatusInternalServerError
		var ce *controlError
		if errors.As(err, &ce) {
			code = ce.code
		}
		writeJSONError(w, code, err)
		return
	}
	writeJSONStatus(w, http.StatusOK, st)
}

func decodeOptionalJSON(r io.Reader, dst any) error {
	raw, err := io.ReadAll(io.LimitReader(r, 4096))
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	return nil
}

func writeJSONStatus(w http.ResponseWriter, code int, st Status) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(st)
}

func writeJSONError(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(struct {
		Error string `json:"error"`
	}{Error: err.Error()})
}
