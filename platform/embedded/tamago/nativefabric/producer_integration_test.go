// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol"
	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
)

type producedProbe struct {
	input   io.WriteCloser
	output  *bufio.Reader
	command *exec.Cmd
}

func (p *producedProbe) call(format string, args ...any) ([]uint64, error) {
	if _, err := fmt.Fprintf(p.input, format+"\n", args...); err != nil {
		return nil, err
	}
	line, err := p.output.ReadString('\n')
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "R" || fields[1] != "1" {
		return nil, fmt.Errorf("producer probe response %q", line)
	}
	var values []uint64
	for _, s := range fields[2:] {
		v, err := strconv.ParseUint(s, 16, 64)
		if err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, nil
}
func (p *producedProbe) Read32(o uint32) (uint32, error) {
	v, err := p.call("read %x", o)
	if err != nil {
		return 0, err
	}
	if len(v) != 1 {
		return 0, errors.New("malformed probe read")
	}
	return uint32(v[0]), nil
}
func (p *producedProbe) Write32(o, v uint32) error { _, err := p.call("write %x %x", o, v); return err }

func TestProducedIdentityBindsNativeControl(t *testing.T) {
	dir := os.Getenv("FES_DE25_PRODUCER_DIR")
	if dir == "" {
		t.Skip("run verify_producer.py for the producer integration")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "descriptor.json"))
	if err != nil {
		t.Fatal(err)
	}
	var desc struct {
		Schema            string `json:"schema"`
		Classification    string `json:"classification"`
		BuildID           string `json:"build_id"`
		BuildInputsSHA256 string `json:"build_inputs_sha256"`
		ControlABI        uint32 `json:"control_abi"`
		Artifact          struct {
			File   string `json:"file"`
			SHA256 string `json:"sha256"`
			Type   string `json:"type"`
			Size   int64  `json:"size"`
		} `json:"artifact"`
	}
	if err = json.Unmarshal(raw, &desc); err != nil {
		t.Fatal(err)
	}
	if desc.Schema != "fes.embedded-control-simulation.v1" || desc.Classification != "host-simulation-only" || desc.Artifact.File != "identity-probe" || desc.Artifact.Type != "host-executable" {
		t.Fatal("not a simulation-only producer descriptor")
	}
	inputs, err := os.ReadFile(filepath.Join(dir, "build-inputs.json"))
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(inputs)
	if hex.EncodeToString(hash[:]) != desc.BuildInputsSHA256 {
		t.Fatal("build record digest mismatch")
	}
	artifact := filepath.Join(dir, desc.Artifact.File)
	body, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	hash = sha256.Sum256(body)
	if int64(len(body)) != desc.Artifact.Size || hex.EncodeToString(hash[:]) != desc.Artifact.SHA256 {
		t.Fatal("producer artifact digest mismatch")
	}
	words, err := BuildWords(desc.BuildID)
	if err != nil {
		t.Fatal(err)
	}
	want := embeddedcontrol.Identity{ABI: desc.ControlABI, Build: words}
	// Only this software fixture uses a synthetic package ID and payload. The
	// producer emitted a host executable, not an RBF or an admitted package.
	payload := []byte("synthetic producer integration bytes")
	sum := sha256.Sum256(payload)
	binding := fabricmodel.PackageIdentity{PackageID: strings.Repeat("a", 64), CoreID: "simulation.core", Platform: "simulation", Device: "software", ProgrammingProfile: "test.control", ABI: "test.abi", Major: 1, BuildID: desc.BuildID, ProgrammedSHA256: hex.EncodeToString(sum[:])}
	image, err := fabricmodel.Prepare("model.package", payload, sum, fabricmodel.Identity{ABI: 1, Core: 1, Build: 1, Package: binding}, false)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := New(&fake{}, Options{DRAMBase: 1, ChunkBytes: 32, IdentityProfiles: []IdentityProfile{{Platform: "simulation", Device: "software", ProgrammingProfile: "test.control", ABI: "test.abi", Major: 1, ControlABI: desc.ControlABI}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = endpoint.Preflight(image); err != nil {
		t.Fatal(err)
	}
	expected, err := endpoint.resolve(fabricmodel.Identity{Package: binding})
	if err != nil || expected != want {
		t.Fatalf("admission identity %+v: %v", expected, err)
	}
	p := &producedProbe{command: exec.Command(artifact, "--serve")}
	if p.input, err = p.command.StdinPipe(); err != nil {
		t.Fatal(err)
	}
	stdout, err := p.command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	p.output = bufio.NewReader(stdout)
	p.command.Stderr = os.Stderr
	if err = p.command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		p.input.Close()
		if err := p.command.Wait(); err != nil {
			t.Errorf("producer probe: %v", err)
		}
	})
	control := embeddedcontrol.Control{IO: p}
	if err = control.Verify(expected); err != nil {
		t.Fatalf("live producer identity: %v", err)
	}
	wrong := expected
	wrong.Build[3] ^= 1
	if err = control.Verify(wrong); !errors.Is(err, embeddedcontrol.ErrIdentity) {
		t.Fatalf("wrong build accepted: %v", err)
	}
	wrong = expected
	wrong.ABI ^= 1
	if err = control.Verify(wrong); !errors.Is(err, embeddedcontrol.ErrIdentity) {
		t.Fatalf("wrong control ABI accepted: %v", err)
	}
	if err = control.Enable(expected, 42); err != nil {
		t.Fatal(err)
	}
	if err = control.Disable(expected, 42, 7); err != nil {
		t.Fatal(err)
	}
	if err = control.Drained(expected, 42, 7); err != nil {
		t.Fatal(err)
	}
}
