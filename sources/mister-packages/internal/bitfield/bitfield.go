package bitfield

import (
	"fmt"
	"strconv"
	"strings"
)

// Range is an inclusive bit range inside a register word.
type Range struct {
	Hi int
	Lo int
}

func Parse(spec string) (Range, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return Range{}, fmt.Errorf("empty bits spec")
	}
	if !strings.Contains(spec, ":") {
		bit, err := strconv.Atoi(spec)
		if err != nil {
			return Range{}, fmt.Errorf("bits %q: %w", spec, err)
		}
		if bit < 0 || bit > 63 {
			return Range{}, fmt.Errorf("bits %q out of range", spec)
		}
		return Range{Hi: bit, Lo: bit}, nil
	}
	parts := strings.Split(spec, ":")
	if len(parts) != 2 {
		return Range{}, fmt.Errorf("bits %q: expected hi:lo", spec)
	}
	hi, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return Range{}, fmt.Errorf("bits %q: %w", spec, err)
	}
	lo, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return Range{}, fmt.Errorf("bits %q: %w", spec, err)
	}
	if hi < lo {
		return Range{}, fmt.Errorf("bits %q: hi < lo", spec)
	}
	if lo < 0 || hi > 63 {
		return Range{}, fmt.Errorf("bits %q out of range", spec)
	}
	return Range{Hi: hi, Lo: lo}, nil
}

func (r Range) Width() int {
	return r.Hi - r.Lo + 1
}

func (r Range) Mask() uint64 {
	if r.Width() >= 64 {
		return ^uint64(0)
	}
	return ((uint64(1) << r.Width()) - 1) << r.Lo
}

func (r Range) Shift() uint64 {
	return uint64(r.Lo)
}

func (r Range) Place(fieldValue uint64) (uint64, error) {
	max := uint64(1)<<r.Width() - 1
	if r.Width() >= 64 {
		max = ^uint64(0)
	}
	if fieldValue > max {
		return 0, fmt.Errorf("value 0x%x does not fit in bits %d:%d", fieldValue, r.Hi, r.Lo)
	}
	return fieldValue << r.Lo, nil
}
