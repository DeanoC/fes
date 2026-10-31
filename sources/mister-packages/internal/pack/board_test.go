package pack

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadBoard(t *testing.T) {
	board, err := LoadBoard(filepath.Join(repoRoot(t), "packages", "board", "de10_nano.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if board.ID != "de10_nano" || board.FPGADevice != "5CSEBA6U23I7" || board.Clocks[0].FrequencyHz != 50000000 {
		t.Fatalf("unexpected board: %+v", board)
	}
	var output bytes.Buffer
	if err := board.Report(&output); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"board de10_nano", "family Cyclone V", "device 5CSEBA6U23I7", "fpga_clk1_50 pin=V11", "frequency_hz=50000000", "LED0 pin=W15", "direction=output"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("report missing %q: %s", want, output.String())
		}
	}
}

func TestLoadDE25Board(t *testing.T) {
	board, err := LoadBoard(filepath.Join(repoRoot(t), "packages", "board", "de25_nano.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if board.ID != "de25_nano" || board.FPGAFamily != "Agilex 5" || board.FPGADevice != "A5EB013BB23BE4SCS" || len(board.Clocks) != 3 || len(board.Pins) != 8 {
		t.Fatalf("unexpected board: %+v", board)
	}
	for _, clock := range board.Clocks {
		if clock.FrequencyHz != 50000000 {
			t.Errorf("clock %s: frequency_hz=%d", clock.Name, clock.FrequencyHz)
		}
	}
	var output bytes.Buffer
	if err := board.Report(&output); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"board de25_nano", "device A5EB013BB23BE4SCS", "CLOCK0_50 pin=DJ35 standard=1.1-V", "LEDR[0] pin=DF35 standard=1.1-V direction=output", "LEDR[7] pin=DP30"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("report missing %q: %s", want, output.String())
		}
	}
}

func TestLoadBoardRejectsMalformedDefinitions(t *testing.T) {
	source, err := os.ReadFile(filepath.Join(repoRoot(t), "packages", "board", "de10_nano.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, old, replacement, want string }{
		{"wrong schema", "schema: mister-packages.v1", "schema: unknown", "schema"},
		{"wrong kind", "kind: board", "kind: soc", "kind"},
		{"invalid identity", "id: de10_nano", "id: DE10 Nano", "invalid board identifier"},
		{"missing vendor", "vendor: altera", "vendor: ''", "missing vendor"},
		{"missing family", "fpga_family: Cyclone V", "fpga_family: ''", "missing fpga_family"},
		{"missing device", "fpga_device: 5CSEBA6U23I7", "fpga_device: ''", "missing fpga_device"},
		{"missing clock name", "name: fpga_clk1_50", "name: ''", "name, pin, and standard are required"},
		{"missing clock pin", "pin: V11", "pin: ''", "name, pin, and standard are required"},
		{"missing standard", "standard: 3.3-V LVTTL", "standard: ''", "name, pin, and standard are required"},
		{"zero frequency", "frequency_hz: 50000000", "frequency_hz: 0", "frequency_hz must be positive"},
		{"negative frequency", "frequency_hz: 50000000", "frequency_hz: -1", "invalid syntax"},
		{"duplicate name", "name: LED0", "name: fpga_clk1_50", "duplicate name"},
		{"duplicate physical pin", "pin: W15", "pin: v11", "duplicate physical pin"},
		{"invalid direction", "direction: output", "direction: out", "direction must be"},
		{"unknown field", "id: de10_nano", "id: de10_nano\nunknown: true", "field unknown not found"},
		{"multiple documents", "id: de10_nano", "id: de10_nano", "multiple YAML documents"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text := strings.Replace(string(source), tc.old, tc.replacement, 1)
			if tc.name == "multiple documents" {
				text += "---\nkind: board\n"
			}
			path := filepath.Join(t.TempDir(), "board.yaml")
			if err := os.WriteFile(path, []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadBoard(path); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("LoadBoard = %v; want %q", err, tc.want)
			}
		})
	}
}
