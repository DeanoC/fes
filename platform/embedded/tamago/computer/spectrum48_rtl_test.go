// SPDX-License-Identifier: GPL-3.0-or-later
package computer

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	g "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/runtimeadmission"
)

func spectrum48Source(t *testing.T, firmware []byte) (Identity, []byte) {
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
				"fes-gp-v1", "de25-linux-computer-v1",
				"fes.simple-game", "fes.computer",
				"0123456789abcdef0123456789abcdef", "00112233445566778899aabbccddeeff",
			).Replace(string(data)))
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
	compat, err := runtimeadmission.NewNanoComputerCompatibility()
	if err != nil || compat.CheckArchive(archive, info.PackageID) == nil {
		t.Fatal("the Linux package must stay refused by native admission", err)
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
	return identity, image
}

type spectrumMailbox struct {
	*rtlIO
	begins, data, commits, releases int
	fault                           string
	cancel                          context.CancelFunc
	original, corrupt               uint32
	corruptEcho                     bool
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
	t.Cleanup(func() {
		defer cancel()
		input.Close()
		if err := cmd.Wait(); err != nil {
			t.Error(err)
		}
	})
	return &spectrumMailbox{rtlIO: &rtlIO{input: input, output: bufio.NewReader(output)}}
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
	want, firmware := spectrum48Source(t, image)
	ctx := context.Background()
	limits := Limits{Timeout: 20 * time.Second, MaxPolls: 64, Now: time.Now, Yield: func() {}}

	for _, name := range []string{"launch", "relaunch-after-fresh-configuration", "unarmed-release", "wrong-build", "wrong-size", "cancel", "crc", "return-ack", "already-ready"} {
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
			if err := c.Run(ctx); err != nil {
				t.Fatal(err)
			}
			advance(t, s.rtlIO, 100000)
			if held, marker := s.state(t); held || marker != 0xa5 {
				t.Fatal("diagnostic ROM did not run", held, marker)
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
			if err := c.Hold(ctx); err != nil {
				t.Fatal(err)
			}
			if held, _ := s.state(t); !held || s.releases != 1 {
				t.Fatal("return did not hold the machine")
			}
			if name == "already-ready" {
				err := c.LoadInitialFirmware(ctx, firmware)
				if !errors.Is(err, ErrProtocol) || s.begins != 4 || s.commits != 1 {
					t.Fatal("initial upload adopted or overwrote a ready ROM", err)
				}
			}
		})
	}
}
