//go:build linux

package tenfoot

import (
	"bytes"
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
	defaultBluetoothControlSocket = "/run/fes-bluetooth.sock"
	defaultBluetoothPollInterval  = time.Second
	defaultBluetoothCallTimeout   = 2 * time.Second
)

type linuxBluetoothConfig struct {
	SocketPath   string
	PollInterval time.Duration
	CallTimeout  time.Duration
}

type btAgentDeviceJSON struct {
	Address   string `json:"address"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Connected bool   `json:"connected"`
}

type btAgentStatusJSON struct {
	AdapterConnected   bool                `json:"adapter_connected"`
	AdapterName        string              `json:"adapter_name"`
	PairingActive      bool                `json:"pairing_active"`
	PairingRemainingMS int64               `json:"pairing_remaining_ms"`
	ConnectedCount     int                 `json:"connected_count"`
	Devices            []btAgentDeviceJSON `json:"devices"`
}

type linuxBluetoothController struct {
	transport   *http.Transport
	client      *http.Client
	callTimeout time.Duration
	cancel      context.CancelFunc
	done        chan struct{}

	mu           sync.RWMutex
	status       BluetoothStatus
	pairingUntil time.Time
}

func startLinuxBluetooth(ctx context.Context, opts Options) *linuxBluetoothController {
	if opts.Smoke {
		return nil
	}
	sock := strings.TrimSpace(os.Getenv("FES_BLUETOOTH_SOCKET"))
	if sock == "" {
		sock = defaultBluetoothControlSocket
	}
	if !opts.KitHost {
		if _, err := os.Stat(sock); err != nil {
			return nil
		}
	}
	return newLinuxBluetoothController(ctx, linuxBluetoothConfig{SocketPath: sock})
}

func newLinuxBluetoothController(parent context.Context, cfg linuxBluetoothConfig) *linuxBluetoothController {
	sock := strings.TrimSpace(cfg.SocketPath)
	if sock == "" {
		sock = defaultBluetoothControlSocket
	}
	poll := cfg.PollInterval
	if poll <= 0 {
		poll = defaultBluetoothPollInterval
	}
	timeout := cfg.CallTimeout
	if timeout <= 0 {
		timeout = defaultBluetoothCallTimeout
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		},
	}
	ctx, cancel := context.WithCancel(parent)
	c := &linuxBluetoothController{
		transport:   transport,
		client:      &http.Client{Transport: transport},
		callTimeout: timeout,
		cancel:      cancel,
		done:        make(chan struct{}),
	}
	go c.pollLoop(ctx, poll)
	return c
}

func (c *linuxBluetoothController) pollLoop(ctx context.Context, interval time.Duration) {
	defer close(c.done)
	c.refresh(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.refresh(ctx)
		}
	}
}

func (c *linuxBluetoothController) refresh(ctx context.Context) {
	reqCtx, cancel := context.WithTimeout(ctx, c.callTimeout)
	defer cancel()
	wire, err := c.doJSON(reqCtx, http.MethodGet, "/status", nil)
	if err != nil {
		if ctx.Err() == nil {
			c.clearStatus()
		}
		return
	}
	c.applyWireStatus(wire)
}

func (c *linuxBluetoothController) clearStatus() {
	c.mu.Lock()
	c.status = BluetoothStatus{}
	c.pairingUntil = time.Time{}
	c.mu.Unlock()
}

func (c *linuxBluetoothController) applyWireStatus(wire btAgentStatusJSON) {
	next := BluetoothStatus{
		DongleConnected: wire.AdapterConnected,
		DongleName:      wire.AdapterName,
		PairingActive:   wire.PairingActive,
		ConnectedCount:  wire.ConnectedCount,
	}
	var until time.Time
	if wire.PairingActive && wire.PairingRemainingMS > 0 {
		rem := time.Duration(wire.PairingRemainingMS) * time.Millisecond
		next.PairingRemaining = rem
		until = time.Now().Add(rem)
	}
	if len(wire.Devices) > 0 {
		next.Devices = make([]BluetoothDevice, len(wire.Devices))
		for i, d := range wire.Devices {
			next.Devices[i] = BluetoothDevice{
				Address:   d.Address,
				Name:      d.Name,
				Kind:      d.Kind,
				Connected: d.Connected,
			}
		}
	}
	c.mu.Lock()
	c.status = next
	c.pairingUntil = until
	c.mu.Unlock()
}

// BluetoothStatus returns the latest cached snapshot without performing I/O.
func (c *linuxBluetoothController) BluetoothStatus() BluetoothStatus {
	if c == nil {
		return BluetoothStatus{}
	}
	c.mu.RLock()
	st := c.status
	until := c.pairingUntil
	if len(c.status.Devices) > 0 {
		st.Devices = append([]BluetoothDevice(nil), c.status.Devices...)
	}
	c.mu.RUnlock()

	if st.PairingActive && !until.IsZero() {
		rem := time.Until(until)
		if rem <= 0 {
			st.PairingActive = false
			st.PairingRemaining = 0
		} else {
			st.PairingRemaining = rem
		}
	}
	return st
}

func (c *linuxBluetoothController) StartBluetoothPairing(duration time.Duration) error {
	if c == nil {
		return errors.New("bluetooth controller is nil")
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.callTimeout)
	defer cancel()
	wire, err := c.doJSON(ctx, http.MethodPost, "/pairing/start", struct {
		DurationMS int64 `json:"duration_ms"`
	}{DurationMS: duration.Milliseconds()})
	if err != nil {
		return err
	}
	c.applyWireStatus(wire)
	return nil
}

func (c *linuxBluetoothController) StopBluetoothPairing() error {
	if c == nil {
		return errors.New("bluetooth controller is nil")
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.callTimeout)
	defer cancel()
	wire, err := c.doJSON(ctx, http.MethodPost, "/pairing/stop", nil)
	if err != nil {
		return err
	}
	c.applyWireStatus(wire)
	return nil
}

func (c *linuxBluetoothController) ForgetBluetoothDevice(address string) error {
	if c == nil {
		return errors.New("bluetooth controller is nil")
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.callTimeout)
	defer cancel()
	wire, err := c.doJSON(ctx, http.MethodPost, "/devices/forget", struct {
		Address string `json:"address"`
	}{Address: strings.TrimSpace(address)})
	if err != nil {
		return err
	}
	c.applyWireStatus(wire)
	return nil
}

func (c *linuxBluetoothController) doJSON(ctx context.Context, method, path string, reqBody any) (btAgentStatusJSON, error) {
	var bodyReader io.Reader
	if reqBody != nil {
		raw, err := json.Marshal(reqBody)
		if err != nil {
			return btAgentStatusJSON{}, err
		}
		bodyReader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://fes-bluetooth"+path, bodyReader)
	if err != nil {
		return btAgentStatusJSON{}, err
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return btAgentStatusJSON{}, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return btAgentStatusJSON{}, err
	}
	if resp.StatusCode != http.StatusOK {
		var errPayload struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &errPayload) == nil && errPayload.Error != "" {
			return btAgentStatusJSON{}, errors.New(errPayload.Error)
		}
		return btAgentStatusJSON{}, fmt.Errorf("bluetooth agent %s: %s", path, resp.Status)
	}
	var out btAgentStatusJSON
	if err := json.Unmarshal(raw, &out); err != nil {
		return btAgentStatusJSON{}, fmt.Errorf("decode bluetooth agent %s response: %w", path, err)
	}
	return out, nil
}

func (c *linuxBluetoothController) Close() {
	if c == nil {
		return
	}
	if c.cancel != nil {
		c.cancel()
	}
	if c.done != nil {
		<-c.done
	}
	if c.transport != nil {
		c.transport.CloseIdleConnections()
	}
}
