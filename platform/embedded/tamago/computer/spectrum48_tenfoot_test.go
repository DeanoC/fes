// SPDX-License-Identifier: GPL-3.0-or-later
package computer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdcontrol"
)

// The test host models reconfiguration and keeps each RTL process private to
// one play. Tenfoot uses its normal HTTP client; all machine operations reach
// the real Go driver and Spectrum Avalon slaves. The SD filesystem and
// package store are real. SD ownership and TF-A programming are modeled.
type spectrumTestHost struct {
	mu            sync.Mutex
	mailboxes     []*spectrumMailbox
	identity      Identity
	store         *packagestore.Store
	request       packagestore.Request
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
	selection, err := h.store.Retain(ctx, h.request)
	if err != nil {
		return err
	}
	var tape []byte
	if raw := selection.Tape(); len(raw) != 0 {
		if selection.TapeFormat() != packagestore.SpectrumTapeFormat ||
			!protocol.DeclaresSpectrumTape(selection.Inspection().Descriptor) {
			return ErrArgument
		}
		tape, err = protocol.ConvertSpectrumTape(raw, MaxInitialSpectrumTapeBytes)
		if err != nil {
			return err
		}
	}
	h.retired = true
	handoff, err := h.store.Contain(selection)
	if err != nil {
		return err
	}
	retained, err := handoff.Selection()
	if err != nil {
		return err
	}
	linked := retained.Linked()
	rom := retained.Inspection().Descriptor.ROM
	if linked == nil || rom == nil || rom.SourceSize != 16<<10 {
		return ErrArgument
	}
	reader, err := linked.Source(rom.ID, rom.Role)
	if err != nil {
		return err
	}
	firmware, err := io.ReadAll(io.LimitReader(reader, rom.SourceSize+1))
	if err != nil {
		return err
	}
	if int64(len(firmware)) != rom.SourceSize {
		return ErrArgument
	}
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
	if err := c.LoadInitialFirmware(ctx, firmware); err != nil {
		return fmt.Errorf("upload retained Spectrum ROM: %w", err)
	}
	held, err := c.Activity(ctx)
	if err != nil || held != before {
		return fmt.Errorf("fetch changed while uploading ROM: %v, %v", held, err)
	}
	if len(tape) != 0 {
		if err := c.Hold(ctx); err != nil {
			return fmt.Errorf("hold Spectrum for retained tape: %w", err)
		}
		if err := c.LoadInitialSpectrumTape(ctx, tape); err != nil {
			return fmt.Errorf("upload retained Spectrum tape: %w", err)
		}
		held, err = c.Activity(ctx)
		if err != nil || held != before {
			return fmt.Errorf("fetch changed while uploading tape: %v, %v", held, err)
		}
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
	if len(tape) != 0 {
		if err := checkSpectrumTapePlayback(ctx, mailbox); err != nil {
			return err
		}
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
	// TF-A re-observes the retired reader after the nonsecure display and
	// execution hold. The test host resumes the modeled lease only thereafter.
	if _, err := mailbox.call("check-secure-display-stop 5"); err != nil {
		h.retired, h.active = true, nil
		return err
	}
	if err := h.store.Resume(); err != nil {
		h.retired = true
		h.active = nil
		return err
	}
	// The next FPGA configuration uses a fresh simulator process.
	if err := mailbox.finish(); err != nil {
		h.retired, h.active = true, nil
		return fmt.Errorf("finish retired Spectrum simulation: %w", err)
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
	tape, err := os.ReadFile(os.Getenv("FES_SPECTRUM48_TAPE"))
	if err != nil || len(tape) == 0 {
		t.Fatal("expected the open Spectrum tape fixture", err)
	}
	source := spectrum48Source(t, image)
	t.Run("return failure", func(t *testing.T) {
		testSpectrum48Tenfoot(t, executable, source, nil, true)
	})
	t.Run("SD tape relaunch", func(t *testing.T) {
		testSpectrum48Tenfoot(t, executable, source, tape, false)
	})
}

func testSpectrum48Tenfoot(t *testing.T, executable string, source spectrumSource, tape []byte, failReturn bool) {
	t.Helper()
	store, lease, request := spectrumFATStore(t, source, tape)
	badRequest := request
	badRequest.Sources = append([]packagestore.Source(nil), request.Sources...)
	badRequest.Sources[0].Path = "BAD.ROM"
	if _, err := store.Retain(t.Context(), badRequest); err == nil || lease.stops != 0 {
		t.Fatal("wrong FAT ROM reached containment", err)
	}
	t.Run("display grants", func(t *testing.T) {
		for _, address := range []uint32{0x00100000, 0x95000020, 0x95800000} {
			mailbox := startSpectrumMailbox(t, executable)
			if issued, err := mailbox.call("check-display-grant %x", address); err != nil || len(issued) != 1 || issued[0] != 0 {
				t.Fatalf("scanout accepted foreign DMA grant %#x: %v, %v", address, issued, err)
			}
		}
		mailbox := startSpectrumMailbox(t, executable)
		if issued, err := mailbox.call("check-display-retirement"); err != nil || len(issued) != 1 || issued[0] != 0 {
			t.Fatalf("retired scanout became reusable: %v, %v", issued, err)
		}
	})
	host := &spectrumTestHost{identity: source.identity, store: store, request: request}
	server := httptest.NewServer(host)
	defer server.Close()
	// Bound each complete off-board scenario independently of the operations.
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
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
	launches := 2
	if failReturn {
		launches++
	}
	for i := 1; i <= launches; i++ {
		mailbox := startSpectrumMailbox(t, executable)
		host.mu.Lock()
		host.mailboxes = append(host.mailboxes, mailbox)
		host.mu.Unlock()
		app.Press(tenfoot.CmdSelect, time.Now())
		wait("launch", func(s tenfoot.Snapshot) bool { return s.Session.State == "active" && s.GPUParked })
		injectFailure := failReturn && i == launches
		host.mu.Lock()
		host.dropResponses = injectFailure
		host.mu.Unlock()
		app.Press(tenfoot.CmdStop, time.Now())
		if !injectFailure {
			wait("return to library", func(s tenfoot.Snapshot) bool { return s.Session.State == "idle" && !s.GPUParked })
		}
	}
	if failReturn {
		wait("missing reader replies", func(_ tenfoot.Snapshot) bool {
			host.mu.Lock()
			defer host.mu.Unlock()
			return host.retired
		})
	}
	host.mu.Lock()
	defer host.mu.Unlock()
	if host.launches != launches || host.returns != 2 || host.retired != failReturn {
		t.Fatalf("launches=%d returns=%d retired=%t", host.launches, host.returns, host.retired)
	}
	if lease.stops != launches || lease.resumes != 2 || lease.receipt.Active == failReturn || lease.containedReads != 0 {
		t.Fatalf("unexpected storage lifecycle: stops=%d resumes=%d active=%t contained reads=%d",
			lease.stops, lease.resumes, lease.receipt.Active, lease.containedReads)
	}
	if failReturn {
		mailbox := host.mailboxes[launches-1]
		writes := mailbox.writes
		if !errors.Is(host.computer.Run(ctx), ErrRetired) || mailbox.writes != writes {
			t.Fatal("uncertain drain allowed another GP mutation")
		}
	}
}

// The file-backed disk records post-containment IO independently of the package
// store's bookkeeping. The lease models transitions only, not the firmware proof.
type spectrumFATLease struct {
	block.Device
	receipt                        sdcontrol.Receipt
	stops, resumes, containedReads int
}

func (d *spectrumFATLease) ReadSectors(lba uint64, dst []byte) error {
	if !d.receipt.Active {
		d.containedReads++
		return packagestore.ErrState
	}
	return d.Device.ReadSectors(lba, dst)
}

func (d *spectrumFATLease) LastReceipt() (sdcontrol.Receipt, bool) { return d.receipt, true }

func (d *spectrumFATLease) Quiesce() error {
	d.stops++
	d.receipt.Generation++
	d.receipt.Active, d.receipt.Prepared = false, false
	return nil
}

func (d *spectrumFATLease) Resume() error {
	d.resumes++
	d.receipt.Generation += 2
	d.receipt.Active, d.receipt.Prepared = true, true
	return nil
}

func spectrumFATStore(t *testing.T, source spectrumSource, tape []byte) (*packagestore.Store, *spectrumFATLease, packagestore.Request) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "native48.img")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(file.Truncate(64<<20), file.Close()); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("mformat", "-i", path, "-F", "::").CombinedOutput(); err != nil {
		t.Fatalf("mformat: %v: %s", err, output)
	}
	disk, err := block.OpenFile(path, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = disk.Close() })
	lease := &spectrumFATLease{Device: disk, receipt: sdcontrol.Receipt{Owner: 1, Generation: 1, Active: true, Prepared: true}}
	volume, err := fat.Mount(lease)
	if err != nil {
		t.Fatal(err)
	}
	corrupt := bytes.Clone(source.image)
	corrupt[0] ^= 1
	const tapePath = "SPEC48.PZX"
	files := map[string][]byte{"SPEC48.FCP": source.archive, "SPEC48.ROM": source.image, "BAD.ROM": corrupt}
	var media packagestore.TapeRequest
	if len(tape) != 0 {
		files[tapePath] = tape
		media = packagestore.TapeRequest{Path: tapePath, Format: packagestore.SpectrumTapeFormat,
			Size: int64(len(tape)), SHA256: fmt.Sprintf("%x", sha256.Sum256(tape))}
	}
	for path, data := range files {
		if err := volume.CreateFile(t.Context(), path, data); err != nil {
			t.Fatal(err)
		}
	}
	if err := disk.Close(); err != nil {
		t.Fatal(err)
	}
	readOnly, err := block.OpenFile(path, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = readOnly.Close() })
	lease.Device = readOnly
	volume, err = fat.Mount(lease)
	if err != nil {
		t.Fatal(err)
	}
	store, err := packagestore.New(volume, lease)
	if err != nil {
		t.Fatal(err)
	}
	receipt := source.linked.Receipt()
	return store, lease, packagestore.Request{
		Path: "SPEC48.FCP", PackageID: receipt.PackageID, ReceiptSHA256: source.linked.ReceiptSHA256(),
		Tape: media,
		Sources: []packagestore.Source{{
			ID: receipt.ROMLink.ROMID, Role: "firmware", Path: "SPEC48.ROM", SHA256: receipt.ROMLink.SourceSHA256,
		}},
	}
}
