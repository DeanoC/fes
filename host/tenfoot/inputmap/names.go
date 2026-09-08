package inputmap

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/DeanoC/FogCast/remoteinput"
)

const (
	prefixEvdev = "evdev:"
	prefixJS    = "js:"
)

var logicalNames = map[string]remoteinput.Code{
	"dpad-up":    remoteinput.ButtonDPadUp,
	"dpad-down":  remoteinput.ButtonDPadDown,
	"dpad-left":  remoteinput.ButtonDPadLeft,
	"dpad-right": remoteinput.ButtonDPadRight,
	"a":          remoteinput.ButtonA,
	"b":          remoteinput.ButtonB,
	"c":          remoteinput.ButtonC,
	"x":          remoteinput.ButtonX,
	"y":          remoteinput.ButtonY,
	"l":          remoteinput.ButtonL,
	"r":          remoteinput.ButtonR,
	"start":      remoteinput.ButtonStart,
	"select":     remoteinput.ButtonSelect,
	"left-x":     remoteinput.AxisLeftX,
	"left-y":     remoteinput.AxisLeftY,
}

func parseLogical(name string) (remoteinput.Code, error) {
	code, ok := logicalNames[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return 0, fmt.Errorf("inputmap: unknown control %q", name)
	}
	return code, nil
}

func isPhysical(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return strings.HasPrefix(n, prefixEvdev) || strings.HasPrefix(n, prefixJS)
}

func parsePhysical(name string) (kind string, code uint16, err error) {
	n := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.HasPrefix(n, prefixEvdev):
		v, convErr := strconv.ParseUint(strings.TrimPrefix(n, prefixEvdev), 10, 16)
		if convErr != nil {
			return "", 0, fmt.Errorf("inputmap: invalid evdev code %q", name)
		}
		return "evdev", uint16(v), nil
	case strings.HasPrefix(n, prefixJS):
		v, convErr := strconv.ParseUint(strings.TrimPrefix(n, prefixJS), 10, 8)
		if convErr != nil {
			return "", 0, fmt.Errorf("inputmap: invalid js button %q", name)
		}
		return "js", uint16(v), nil
	default:
		return "", 0, fmt.Errorf("inputmap: unknown control %q", name)
	}
}

func isAxisCode(code remoteinput.Code) bool {
	return code == remoteinput.AxisLeftX || code == remoteinput.AxisLeftY
}

func validateBindings(bindings map[string]string) error {
	for from, to := range bindings {
		dst, err := parseLogical(to)
		if err != nil {
			return err
		}
		if isPhysical(from) {
			if _, _, err := parsePhysical(from); err != nil {
				return err
			}
			if isAxisCode(dst) {
				return fmt.Errorf("inputmap: physical control %q must map to a button", from)
			}
			continue
		}
		src, err := parseLogical(from)
		if err != nil {
			return err
		}
		if isAxisCode(src) != isAxisCode(dst) {
			return fmt.Errorf("inputmap: cannot bind %q to %q", from, to)
		}
	}
	return nil
}
