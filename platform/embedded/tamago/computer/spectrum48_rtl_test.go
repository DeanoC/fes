// SPDX-License-Identifier: GPL-3.0-or-later
package computer

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	g "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/runtimeadmission"
)

type spectrumSource struct {
	identity Identity
	archive  []byte
	linked   *corepackage.LinkedArchive
	image    []byte
}

func spectrum48Source(t *testing.T, firmware []byte) spectrumSource {
	t.Helper()
	root := "../../../../sources/FogCast/corepackage/testdata/core-bundle-v3"
	dir := t.TempDir()
	for destination, source := range map[string]string{
		"manifest.toml": "manifests/valid-nano-upload-16384.toml",
		"core.rbf":      "payloads/fes-fixture.rbf",
		"rom-map.json":  "maps/valid-nano-upload-16384.json",
	} {
		data, err := os.ReadFile(filepath.Join(root, source))
		if err != nil {
			t.Fatal(err)
		}
		if destination == "manifest.toml" {
			data = []byte(strings.NewReplacer(
				"fes.pong", "fes.spectrum",
				"fes-gp-v1", "de25-native-spectrum48-v1",
				"fes.simple-game", "fes.computer",
				"0123456789abcdef0123456789abcdef", "00112233445566778899aabbccddeeff",
			).Replace(string(data)) + `
[[interfaces]]
id = "fes.media.spectrum-tape"
major = 2
minor = 0
required = true
`)
		}
		if err := os.WriteFile(filepath.Join(dir, destination), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := corepackage.CanonicalArchive(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	archive, err := corepackage.RetainArchive(ctx, int64(len(raw)), bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	info := archive.Inspection()
	d := info.Descriptor
	rawControls, err := os.ReadFile(os.Getenv("FES_SPECTRUM48_CONTROLS"))
	if err != nil {
		t.Fatal(err)
	}
	var controls struct {
		Format        int    `json:"format"`
		Scope         string `json:"scope"`
		Build         string `json:"build_id"`
		Payload       string `json:"payload_sha256"`
		FirmwareBytes int64  `json:"firmware_bytes"`
		Computer      struct {
			Offset       uint32 `json:"peripheral_offset"`
			Span         uint32 `json:"span"`
			FetchCount   uint32 `json:"fetch_count_offset"`
			FetchAddress uint32 `json:"fetch_address_offset"`
		} `json:"computer"`
		Display struct {
			Offset  uint32 `json:"peripheral_offset"`
			Span    uint32 `json:"span"`
			Stop    uint32 `json:"stop_command"`
			Status  uint32 `json:"status_offset"`
			Drained uint32 `json:"drained_mask"`
			Retired uint32 `json:"retired_mask"`
			Base    uint32 `json:"grant_base"`
			Bytes   uint32 `json:"grant_bytes"`
		} `json:"display_reader"`
	}
	if err := json.Unmarshal(rawControls, &controls); err != nil {
		t.Fatal(err)
	}
	if controls.Format != 1 || controls.Scope != "de25-native-spectrum48-controls-v1" ||
		controls.Build != d.Build.ID || controls.Payload != d.Payload.SHA256 ||
		controls.FirmwareBytes != d.ROM.SourceSize ||
		controls.Computer.Offset != g.FesDe25ComputerPeripheralOffset || controls.Computer.Span != g.FesDe25ComputerSpan ||
		controls.Computer.FetchCount != g.FesDe25ComputerFetchCountOffset || controls.Computer.FetchAddress != g.FesDe25ComputerFetchAddressOffset ||
		controls.Display.Offset != g.FesDe25ScanoutPeripheralOffset || controls.Display.Span != g.FesDe25ScanoutSpan ||
		controls.Display.Stop != g.FesDe25ScanoutCommandStop || controls.Display.Status != g.FesDe25ScanoutStatusOffset ||
		controls.Display.Drained != g.FesDe25ScanoutStatusDrained ||
		controls.Display.Retired != 0x80000000 || controls.Display.Base != 0x95000000 ||
		controls.Display.Bytes != g.FesDe25ScanoutAllocationBytes {
		t.Fatal("generated control maps differ from package or driver")
	}
	compat, err := runtimeadmission.NewNanoComputerCompatibility()
	if err != nil || compat.CheckArchive(archive, info.PackageID) == nil {
		t.Fatal("the prototype package must stay refused by normal native admission", err)
	}
	if !corepackage.FirmwareUpload(d) || d.ROM.SourceSize != 16384 {
		t.Fatal("fixture lost the format-3 firmware upload contract")
	}
	if _, err := archive.LinkROMs(ctx, []corepackage.ROMSource{{ID: d.ROM.ID, Role: "firmware", Data: firmware[:8192]}}); err == nil {
		t.Fatal("short ROM accepted")
	}
	linked, err := archive.LinkROMs(ctx, []corepackage.ROMSource{{ID: d.ROM.ID, Role: "firmware", Data: firmware}})
	if err != nil {
		t.Fatal(err)
	}
	receipt := linked.Receipt()
	if receipt.PackageID != info.PackageID || receipt.ROMLink == nil || !receipt.ROMLink.ValidFor(d) ||
		receipt.ROMLink.ProgrammedSHA256 != d.Payload.SHA256 {
		t.Fatal("retained ROM receipt differs from the descriptor")
	}
	reader, err := linked.Source(d.ROM.ID, "firmware")
	if err != nil {
		t.Fatal(err)
	}
	image, err := io.ReadAll(io.LimitReader(reader, d.ROM.SourceSize+1))
	sum := sha256.Sum256(image)
	if err != nil || int64(len(image)) != d.ROM.SourceSize || hex.EncodeToString(sum[:]) != receipt.ROMLink.SourceSHA256 {
		t.Fatal("retained ROM differs from the receipt", err)
	}
	var identity Identity
	identity.Capabilities = g.FesComputerCapabilityVideoFixed720p60 |
		g.FesComputerCapabilityKeyboardHid | g.FesComputerCapabilityGamepadPorts |
		g.FesComputerCapabilityAudioPcmS16Stereo48k | g.FesComputerCapabilityMediaSpectrumTape |
		g.FesComputerCapabilityVideoSessionDisplay
	build, err := hex.DecodeString(d.Build.ID)
	if err != nil || len(build) != len(identity.Build) {
		t.Fatal("invalid build identity", err)
	}
	copy(identity.Build[:], build)
	return spectrumSource{identity: identity, archive: raw, linked: linked, image: image}
}

type spectrumMailbox struct {
	*rtlIO
	finish                          func() error
	begins, data, commits, releases int
	fault                           string
	cancel                          context.CancelFunc
	original, corrupt               uint32
	corruptEcho                     bool
}

type spectrumDisplay struct {
	*rtlIO
	replyAfterStop bool
	stopped        bool
}

func (s *spectrumDisplay) Read32(offset uint32) (uint32, error) {
	reply, err := s.call("display-read %x", offset)
	if err != nil {
		return 0, err
	}
	if len(reply) != 1 {
		return 0, ErrProtocol
	}
	return uint32(reply[0]), nil
}

func (s *spectrumDisplay) Write32(offset, word uint32) error {
	_, err := s.call("display-write %x %x", offset, word)
	if err != nil || offset != g.FesDe25ScanoutCommandOffset || word != g.FesDe25ScanoutCommandStop {
		return err
	}
	s.stopped = true
	if s.replyAfterStop {
		status, err := s.Read32(g.FesDe25ScanoutStatusOffset)
		if err != nil {
			return err
		}
		if status&g.FesDe25ScanoutStatusDrained != 0 {
			return errors.New("scanout reported drained with replies still withheld")
		}
		_, err = s.call("display-reply")
		return err
	}
	return nil
}

func startSpectrumMailbox(t *testing.T, executable string) *spectrumMailbox {
	t.Helper()
	// Keep even a blocked simulator read bounded.
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	cmd := exec.CommandContext(ctx, executable, "--native-mailbox")
	input, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	finish := sync.OnceValue(func() error {
		defer cancel()
		_ = input.Close()
		return cmd.Wait()
	})
	t.Cleanup(func() {
		if err := finish(); err != nil {
			t.Error(err)
		}
	})
	return &spectrumMailbox{rtlIO: &rtlIO{input: input, output: bufio.NewReader(output)}, finish: finish}
}

func (s *spectrumMailbox) Read32(offset uint32) (uint32, error) {
	word, err := s.rtlIO.Read32(offset)
	// Inject corruption downstream of GPO through the simulator boundary:
	// the host-side echo still describes the word it submitted.
	if err == nil && s.corruptEcho && offset == g.FesDe25ComputerGpoOffset && word == s.corrupt {
		return s.original, nil
	}
	return word, err
}

func (s *spectrumMailbox) Write32(offset, word uint32) error {
	s.corruptEcho = false
	switch (word >> 24) & 127 {
	case g.FesFirmwareUploadOpcodeBegin:
		s.begins++
	case g.FesFirmwareUploadOpcodeData:
		s.data++
		if s.fault == "crc" && s.data == 1 {
			s.original, s.corrupt = word, word^1
			s.corruptEcho, word = true, s.corrupt
		}
	case g.FesFirmwareUploadOpcodeCommit:
		s.commits++
	case g.FesComputerOpcodeExecution:
		if uint32(uint16(word)) == g.FesComputerExecutionRelease {
			s.releases++
		}
	}
	err := s.rtlIO.Write32(offset, word)
	if s.fault == "cancel" && s.data == 1 {
		s.cancel()
	}
	if err == nil && s.fault == "return-ack" &&
		word & ^g.FesComputerRequestMask == g.FesComputerOpcodeExecution<<24|g.FesComputerExecutionHoldReset {
		return errors.New("simulated lost Hold acknowledgement")
	}
	return err
}

func (s *spectrumMailbox) state(t *testing.T) (held bool, marker uint64) {
	t.Helper()
	state, err := s.call("spectrum-state")
	if err != nil || len(state) != 2 || state[0] > 1 {
		t.Fatalf("Spectrum state: %v %v", state, err)
	}
	return state[0] != 0, state[1]
}

func TestSpectrum48FirmwareRTL(t *testing.T) {
	executable := os.Getenv("FES_SPECTRUM48_MAILBOX")
	if executable == "" {
		t.Skip("run the Spectrum sim.py with --native-mailbox")
	}
	image, err := os.ReadFile(os.Getenv("FES_SPECTRUM48_FIRMWARE"))
	if err != nil || len(image) != 16384 {
		t.Fatal("expected the 16 KiB open diagnostic ROM", err)
	}
	source := spectrum48Source(t, image)
	want, firmware := source.identity, source.image
	ctx := context.Background()
	limits := Limits{Timeout: 20 * time.Second, MaxPolls: 64, Now: time.Now, Yield: func() {}}

	for _, name := range []string{"launch", "relaunch-after-fresh-configuration", "unarmed-release", "wrong-build", "wrong-size", "cancel", "crc", "return-ack", "already-ready", "fetch-stall"} {
		t.Run(name, func(t *testing.T) {
			s := startSpectrumMailbox(t, executable)
			id := want
			if name == "wrong-build" {
				id.Build[0] ^= 1
			}
			c, err := Open(ctx, s, limits, id)
			if name == "wrong-build" {
				if !errors.Is(err, ErrIdentity) || s.begins != 0 || s.releases != 0 {
					t.Fatal("wrong build got past live identity", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := c.Hold(ctx); err != nil {
				t.Fatal(err)
			}
			if name == "fetch-stall" {
				if _, err := c.Activity(ctx); err != nil {
					t.Fatal("initial fetch snapshot failed", err)
				}
				fault, err := s.call("fetch-stall")
				if err != nil || len(fault) != 1 || fault[0] != uint64(g.FesDe25ComputerFaultBusy) {
					t.Fatal("stalled machine clock did not end the Avalon fetch read with a sticky fault", fault, err)
				}
				if _, err := c.Activity(ctx); !errors.Is(err, ErrProtocol) {
					t.Fatal("faulted fetch endpoint was accepted", err)
				}
				return
			}
			if name == "unarmed-release" {
				if err := c.Run(ctx); !errors.Is(err, ErrProtocol) {
					t.Fatal("RTL accepted release without ROM", err)
				}
				if held, _ := s.state(t); !held {
					t.Fatal("unarmed release took effect")
				}
				return
			}
			transferCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			s.cancel = cancel
			if name == "cancel" || name == "crc" {
				s.fault = name
			}
			data := firmware
			if name == "wrong-size" {
				data = firmware[:8192]
			}
			err = c.LoadInitialFirmware(transferCtx, data)
			switch name {
			case "wrong-size", "cancel", "crc":
				wantErr := ErrProtocol
				if name == "cancel" {
					wantErr = context.Canceled
				}
				if !errors.Is(err, wantErr) {
					t.Fatal("wrong upload verdict", err)
				}
				if name == "wrong-size" && s.begins != 0 ||
					name == "cancel" && (s.data != 1 || s.commits != 0) ||
					name == "crc" && (s.data != 8192 || s.commits != 1) {
					t.Fatalf("unexpected requests before failure: begin=%d data=%d commit=%d", s.begins, s.data, s.commits)
				}
				writes := s.writes
				if !errors.Is(c.Run(ctx), ErrRetired) || !errors.Is(c.LoadInitialFirmware(ctx, firmware), ErrRetired) ||
					s.releases != 0 || writes != s.writes {
					t.Fatal("failed upload permitted release or retry")
				}
				if held, _ := s.state(t); !held {
					t.Fatal("CPU left hold after failure")
				}
				return
			}
			if err != nil || s.begins != 4 || s.data != 8192 || s.commits != 1 {
				t.Fatalf("upload: %v begin=%d data=%d commit=%d", err, s.begins, s.data, s.commits)
			}
			if held, marker := s.state(t); !held || marker != 0 {
				t.Fatal("firmware executed before release", held, marker)
			}
			heldBefore, err := c.Activity(ctx)
			if err != nil || heldBefore.Cycles != 0 {
				t.Fatal("unexpected fetch while held", heldBefore, err)
			}
			if err := c.Run(ctx); err != nil {
				t.Fatal(err)
			}
			advance(t, s.rtlIO, 100000)
			if held, marker := s.state(t); held || marker != 0xa5 {
				t.Fatal("diagnostic ROM did not run", held, marker)
			}
			running, err := c.Activity(ctx)
			if err != nil || running.Cycles <= heldBefore.Cycles || running.Address >= 16384 {
				t.Fatal("no instruction fetch from the diagnostic ROM", running, err)
			}
			if name == "launch" {
				count, err := s.call("check-fetch-snapshots")
				if err != nil || len(count) != 1 || count[0] <= uint64(running.Cycles) {
					t.Fatal("repeated paired fetch snapshots failed", count, err)
				}
			}
			if name == "return-ack" {
				s.fault = name
				if err := c.Hold(ctx); err == nil {
					t.Fatal("uncertain return reported success")
				}
				writes := s.writes
				if !errors.Is(c.Hold(ctx), ErrRetired) || !errors.Is(c.Run(ctx), ErrRetired) ||
					s.writes != writes {
					t.Fatal("uncertain return allowed another mutation")
				}
				return
			}
			if err := c.StopWithDisplay(ctx, &spectrumDisplay{rtlIO: s.rtlIO}); err != nil {
				t.Fatal(err)
			}
			if held, _ := s.state(t); !held || s.releases != 1 {
				t.Fatal("return did not hold the machine")
			}
			first, err := c.Activity(ctx)
			advance(t, s.rtlIO, 1000)
			last, afterErr := c.Activity(ctx)
			if err != nil || afterErr != nil || first != last {
				t.Fatal("fetch did not stop after return", first, last, err, afterErr)
			}
			if name == "already-ready" {
				if err := c.Hold(ctx); err != nil {
					t.Fatal(err)
				}
				err := c.LoadInitialFirmware(ctx, firmware)
				if !errors.Is(err, ErrProtocol) || s.begins != 4 || s.commits != 1 {
					t.Fatal("initial upload adopted or overwrote a ready ROM", err)
				}
			}
		})
	}
}

func TestSpectrum48SecureStopRTL(t *testing.T) {
	executable := os.Getenv("FES_SPECTRUM48_MAILBOX")
	if executable == "" {
		t.Skip("run the Spectrum sim.py with --native-mailbox")
	}
	for _, scenario := range []struct {
		name string
		mode uint32
	}{
		{"idle", 0}, {"active-drains", 1}, {"withheld-response", 2}, {"wrong-generation", 3},
		{"already-retired", 4}, {"fresh-admission", 6}, {"active-admission-rejected", 7},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			mailbox := startSpectrumMailbox(t, executable)
			if _, err := mailbox.call("check-secure-display-stop %x", scenario.mode); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSpectrum48TapeRTL(t *testing.T) {
	executable := os.Getenv("FES_SPECTRUM48_MAILBOX")
	if executable == "" {
		t.Skip("run the Spectrum sim.py with --native-mailbox")
	}
	firmware, err := os.ReadFile(os.Getenv("FES_SPECTRUM48_FIRMWARE"))
	if err != nil {
		t.Fatal(err)
	}
	source := spectrum48Source(t, firmware)
	raw, err := os.ReadFile(os.Getenv("FES_SPECTRUM48_TAPE"))
	if err != nil {
		t.Fatal(err)
	}
	pzx, err := protocol.ConvertSpectrumTape(raw, MaxInitialSpectrumTapeBytes)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	defer cancel()
	s := startSpectrumMailbox(t, executable)
	c, err := Open(ctx, s, Limits{Timeout: 20 * time.Second, MaxPolls: 64, Now: time.Now, Yield: func() {}}, source.identity)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []func() error{
		func() error { return c.Hold(ctx) },
		func() error { return c.LoadInitialFirmware(ctx, source.image) },
		func() error { return c.Hold(ctx) },
		func() error { return c.LoadInitialSpectrumTape(ctx, pzx) },
	} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	if held, _ := s.state(t); !held {
		t.Fatal("Spectrum released execution during tape delivery")
	}
	if err := c.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if err := checkSpectrumTapePlayback(ctx, s); err != nil {
		t.Fatal(err)
	}
}

func checkSpectrumTapePlayback(ctx context.Context, mailbox *spectrumMailbox) error {
	// The diagnostic ROM reads EAR via port FE and decodes all four tape bytes
	// into RAM. This observes playback and CPU loading independently of commit.
	for i := 0; i < 200; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := mailbox.call("advance %x", 100000); err != nil {
			return err
		}
		result, err := mailbox.call("tape-result")
		if err != nil || len(result) != 1 {
			return fmt.Errorf("read Spectrum tape result: %v, %v", result, err)
		}
		if result[0] == 0xa53c817e {
			return nil
		}
	}
	return errors.New("Spectrum diagnostic did not decode the cassette bytes into RAM")
}
