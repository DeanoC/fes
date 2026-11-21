// SPDX-License-Identifier: GPL-3.0-or-later
package computer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/tenfoot"
)

// The test host models reconfiguration and keeps each RTL process private to
// one play. Tenfoot uses its normal HTTP client; all machine operations reach
// the real Go driver and Spectrum Avalon slaves. SD and TF-A are outside this
// test host.
type spectrumTestHost struct {
	mu            sync.Mutex
	mailboxes     []*spectrumMailbox
	identity      Identity
	firmware      []byte
	computer      *Computer
	active        map[string]any
	launches      int
	returns       int
	retired       bool
	dropResponses bool
}

func (h *spectrumTestHost) launch(ctx context.Context, flight string) error {
	if h.retired || h.computer != nil || h.launches == len(h.mailboxes) {
		return errors.New("Spectrum launch unavailable")
	}
	h.retired = true
	mailbox := h.mailboxes[h.launches]
	limits := Limits{Timeout: 15 * time.Second, MaxPolls: 512, Now: time.Now, Yield: func() {}}
	c, err := Open(ctx, mailbox, limits, h.identity)
	if err != nil {
		return err
	}
	if err := c.Hold(ctx); err != nil {
		return err
	}
	before, err := c.Activity(ctx)
	if err != nil {
		return err
	}
	if err := c.LoadInitialFirmware(ctx, h.firmware); err != nil {
		return err
	}
	held, err := c.Activity(ctx)
	if err != nil || held != before {
		return fmt.Errorf("fetch changed while uploading: %v, %v", held, err)
	}
	if err := c.Run(ctx); err != nil {
		return err
	}
	if _, err := mailbox.call("advance %x", 100000); err != nil {
		return err
	}
	running, err := c.Activity(ctx)
	if err != nil || running.Cycles <= held.Cycles || running.Address >= 16384 {
		return fmt.Errorf("Spectrum fetch proof failed: %v, %v", running, err)
	}
	beats, err := mailbox.call("display-prime")
	if err != nil || len(beats) != 1 || beats[0] == 0 {
		return fmt.Errorf("display reader did not issue a burst: %v, %v", beats, err)
	}
	h.launches++
	h.active = map[string]any{
		"state": "active", "execution": "fpga_native", "media": "active",
		"id": fmt.Sprintf("spectrum-proof-%d", h.launches), "game_id": "spectrum48",
		"target": "de25-nano", "target_id": "offboard", "flight_id": flight,
		"core_package": map[string]any{
			"core_id": "fes.spectrum", "generation": h.launches,
			"abi": map[string]any{"id": "fes.computer", "major": 1, "minor": 0},
		},
	}
	h.computer, h.retired = c, false
	return nil
}

func (h *spectrumTestHost) stop(ctx context.Context) error {
	if h.retired {
		return ErrRetired
	}
	if h.computer == nil {
		return nil
	}
	mailbox := h.mailboxes[h.launches-1]
	display := &spectrumDisplay{rtlIO: mailbox.rtlIO, replyAfterStop: !h.dropResponses}
	if err := h.computer.StopWithDisplay(ctx, display); err != nil {
		h.retired = true
		h.active = nil
		return err
	}
	if !display.stopped {
		return errors.New("display STOP missing")
	}
	h.computer, h.active = nil, nil
	h.returns++
	return nil
}

func (h *spectrumTestHost) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	encode := json.NewEncoder(w)
	w.Header().Set("Content-Type", "application/json")
	game := hostclient.Game{ID: "spectrum48", Title: "ZX Spectrum 48K diagnostic",
		System: "native", State: "available", RootOnline: true, Launchable: true}
	var err error
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
		_ = encode.Encode(map[string]any{"games": []hostclient.Game{game}})
		return
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/launch":
		var request struct {
			ID string `json:"game_id"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil || request.ID != game.ID {
			err = ErrArgument
		} else {
			err = h.launch(r.Context(), r.Header.Get("X-FogCast-Flight-Id"))
		}
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/stop":
		err = h.stop(r.Context())
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		w.WriteHeader(http.StatusConflict)
		_ = encode.Encode(map[string]any{"error": map[string]string{"code": "SPECTRUM_PROOF_FAILED", "message": err.Error()}})
	} else if h.active != nil {
		_ = encode.Encode(h.active)
	} else if h.retired {
		_ = encode.Encode(map[string]string{"state": "failed"})
	} else {
		_ = encode.Encode(map[string]string{"state": "idle", "execution": "fpga_native"})
	}
}

func TestSpectrum48TenfootRTL(t *testing.T) {
	executable := os.Getenv("FES_SPECTRUM48_MAILBOX")
	if executable == "" {
		t.Skip("run the Spectrum sim.py with --native-mailbox")
	}
	image, err := os.ReadFile(os.Getenv("FES_SPECTRUM48_FIRMWARE"))
	if err != nil || len(image) != 16384 {
		t.Fatal("expected the 16 KiB open diagnostic ROM", err)
	}
	identity, firmware := spectrum48Source(t, image)
	host := &spectrumTestHost{identity: identity, firmware: firmware}
	for range 3 {
		host.mailboxes = append(host.mailboxes, startSpectrumMailbox(t, executable))
	}
	server := httptest.NewServer(host)
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	app := tenfoot.NewApp(tenfoot.NewClient(server.URL, server.Client()), 640, 480, 100)
	app.Start(ctx)
	defer app.Stop()
	wait := func(name string, ready func(tenfoot.Snapshot) bool) {
		t.Helper()
		for ctx.Err() == nil {
			app.Tick(time.Now())
			snapshot := app.Snapshot()
			if ready(snapshot) {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("%s: session=%+v launch=%+v", name, app.Snapshot().Session, app.Snapshot().Launch)
	}
	wait("catalogue", func(s tenfoot.Snapshot) bool { return len(s.Games) == 1 && !s.Loading })
	for i := 1; i <= 3; i++ {
		app.Press(tenfoot.CmdSelect, time.Now())
		wait("launch", func(s tenfoot.Snapshot) bool { return s.Session.State == "active" && s.GPUParked })
		host.mu.Lock()
		if i == 3 {
			host.dropResponses = true
		}
		host.mu.Unlock()
		app.Press(tenfoot.CmdStop, time.Now())
		if i != 3 {
			wait("return to library", func(s tenfoot.Snapshot) bool { return s.Session.State == "idle" && !s.GPUParked })
		}
	}
	wait("missing reader replies", func(_ tenfoot.Snapshot) bool {
		host.mu.Lock()
		defer host.mu.Unlock()
		return host.retired
	})
	host.mu.Lock()
	defer host.mu.Unlock()
	if host.launches != 3 || host.returns != 2 || !host.retired {
		t.Fatalf("launches=%d returns=%d retired=%t", host.launches, host.returns, host.retired)
	}
	writes := host.mailboxes[2].writes
	if !errors.Is(host.computer.Run(ctx), ErrRetired) || host.mailboxes[2].writes != writes {
		t.Fatal("uncertain drain allowed another GP mutation")
	}
}
