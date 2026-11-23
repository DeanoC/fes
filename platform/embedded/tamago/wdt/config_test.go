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

	// Valid opt-in parameters (including exact 2^31-cycle boundaries) parse cleanly.
	for _, okCase := range []struct {
		t, hz   string
		wantSec int
		wantHz  uint64
	}{
		{"10", "100000000", 10, 100_000_000},
		{"21", "100000000", 21, 100_000_000},
		{"5", "400000000", 5, 400_000_000},
		{"60", "25000000", 60, 25_000_000},
	} {
		spec, enabled, err := ParseKeepaliveSpec(okCase.t, okCase.hz)
		if err != nil || !enabled || spec.Timeout != time.Duration(okCase.wantSec)*time.Second || spec.ClockHz != okCase.wantHz {
			t.Fatalf("ParseKeepaliveSpec(%q, %q) = (%+v, %t, %v)", okCase.t, okCase.hz, spec, enabled, err)
		}
	}

	// Partial, non-numeric, out-of-range, or >2^31-cycle inputs fail closed with ErrInvalidConfig.
	for _, tc := range [][2]string{
		{"10", ""},
		{"", "100000000"},
		{"0", "100000000"},
		{"-5", "100000000"},
		{"61", "25000000"},
		{"abc", "100000000"},
		{"10", "999999"},
		{"10", "400000001"},
		{"10", "not-hz"},
		{"22", "100000000"},
		{"6", "400000000"},
		{"60", "400000000"},
	} {
		if _, enabled, err := ParseKeepaliveSpec(tc[0], tc[1]); !errors.Is(err, ErrInvalidConfig) || enabled {
			t.Errorf("ParseKeepaliveSpec(%q, %q) = (enabled=%t, err=%v), want ErrInvalidConfig", tc[0], tc[1], enabled, err)
		}
	}
}
