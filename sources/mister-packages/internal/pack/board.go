package pack

import (
	"fmt"
	"io"
	"strings"
)

// LoadBoard reads a descriptive board independently of a platform or SoC.
func LoadBoard(path string) (*BoardFile, error) {
	var board BoardFile
	if err := readStrictYAML(path, &board); err != nil {
		return nil, err
	}
	if err := checkHeader(board.Schema, board.Kind, "board", path); err != nil {
		return nil, err
	}
	if err := board.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &board, nil
}

func (b *BoardFile) Validate() error {
	if !validABIIdentifier(b.ID) {
		return fmt.Errorf("invalid board identifier %q", b.ID)
	}
	for _, field := range []struct{ name, value string }{
		{"vendor", b.Vendor}, {"fpga_family", b.FPGAFamily}, {"fpga_device", b.FPGADevice},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("board missing %s", field.name)
		}
	}
	if len(b.Clocks) == 0 {
		return fmt.Errorf("board must declare at least one clock")
	}
	names := make(map[string]bool)
	pins := make(map[string]bool)
	checkPin := func(name, pin, standard string) error {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(pin) == "" || strings.TrimSpace(standard) == "" {
			return fmt.Errorf("name, pin, and standard are required")
		}
		if names[name] {
			return fmt.Errorf("duplicate name %q", name)
		}
		physical := strings.ToUpper(strings.TrimSpace(pin))
		if pins[physical] {
			return fmt.Errorf("duplicate physical pin %q", pin)
		}
		names[name] = true
		pins[physical] = true
		return nil
	}
	for i, clock := range b.Clocks {
		if err := checkPin(clock.Name, clock.Pin, clock.Standard); err != nil {
			return fmt.Errorf("clocks[%d]: %w", i, err)
		}
		if clock.FrequencyHz == 0 {
			return fmt.Errorf("clocks[%d]: frequency_hz must be positive", i)
		}
	}
	for i, pin := range b.Pins {
		if err := checkPin(pin.Name, pin.Pin, pin.Standard); err != nil {
			return fmt.Errorf("pins[%d]: %w", i, err)
		}
		switch pin.Direction {
		case "input", "output", "inout":
		default:
			return fmt.Errorf("pins[%d]: direction must be input, output, or inout", i)
		}
	}
	return nil
}

func (b *BoardFile) Report(w io.Writer) error {
	var report strings.Builder
	fmt.Fprintf(&report, "board %s\nvendor %s\nfamily %s\ndevice %s\n", b.ID, b.Vendor, b.FPGAFamily, b.FPGADevice)
	fmt.Fprintln(&report, "clocks:")
	for _, clock := range b.Clocks {
		fmt.Fprintf(&report, "  %s pin=%s standard=%s frequency_hz=%d\n", clock.Name, clock.Pin, clock.Standard, clock.FrequencyHz)
	}
	fmt.Fprintln(&report, "pins:")
	for _, pin := range b.Pins {
		fmt.Fprintf(&report, "  %s pin=%s standard=%s direction=%s\n", pin.Name, pin.Pin, pin.Standard, pin.Direction)
	}
	_, err := io.WriteString(w, report.String())
	return err
}
