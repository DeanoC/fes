// SPDX-License-Identifier: GPL-3.0-or-later

package wdt

import (
	"errors"
	"testing"
	"time"
)

func TestParseKeepaliveSpec(t *testing.T) {
	// Empty strings keep the watchdog strictly disabled (opt-in).
	spec, enabled, err := ParseKeepaliveSpec("", "")
	if err != nil || enabled || spec != (KeepaliveSpec{}) {
		t.Fatalf("empty spec=(%+v, %t, %v), want disabled nil err", spec, enabled, err)
	}
	spec, enabled, err = ParseKeepaliveSpec("   ", "\t")
	if err != nil || enabled {
		t.Fatalf("whitespace spec=(%+v, %t, %v), want disabled nil err", spec, enabled, err)
	}

	// Valid opt-in parameters parse cleanly.
	spec, enabled, err = ParseKeepaliveSpec("10", "100000000")
	if err != nil || !enabled {
		t.Fatalf("valid spec=(%+v, %t, %v)", spec, enabled, err)
	}
	if spec.Timeout != 10*time.Second || spec.ClockHz != 100_000_000 {
		t.Fatalf("spec=%+v, want 10s / 100MHz", spec)
	}

	// Partial, non-numeric, or out-of-range inputs fail closed with ErrInvalidConfig.
	for _, tc := range [][2]string{
		{"10", ""},
		{"", "100000000"},
		{"0", "100000000"},
		{"-5", "100000000"},
		{"61", "100000000"},
		{"abc", "100000000"},
		{"10", "999999"},
		{"10", "400000001"},
		{"10", "not-hz"},
	} {
		if _, enabled, err := ParseKeepaliveSpec(tc[0], tc[1]); !errors.Is(err, ErrInvalidConfig) || enabled {
			t.Errorf("ParseKeepaliveSpec(%q, %q) = (enabled=%t, err=%v), want ErrInvalidConfig", tc[0], tc[1], enabled, err)
		}
	}
}
