package corenames

import "testing"

func TestNamesUseExactFirstPartyIdentity(t *testing.T) {
	if len(models) != 18 {
		t.Fatalf("registered models: %d", len(models))
	}
	for id, m := range models {
		if got := Name(id, "Diagnostic package HIL790"); got != m.name {
			t.Fatalf("%s: %q", id, got)
		}
	}
	for _, id := range []string{"other.pong", "fes.Pong", "fes.future"} {
		if got := Name(id, "My custom core HIL790"); got != "My custom core HIL790" {
			t.Fatalf("unknown identity %s renamed to %q", id, got)
		}
		if got := Name(id, ""); got != id {
			t.Fatalf("empty unknown fallback %q", got)
		}
	}
}

func TestEntryTitleRecognizesOnlyGeneratedMachineDefaults(t *testing.T) {
	for _, tc := range []struct{ core, input, want string }{
		{"fes.coleco", "Coleco HIL 1120 123509", "ColecoVision"},
		{"fes.coleco", "ColecoVision HIL790", "ColecoVision"},
		{"fes.coleco", "Coleco HIL-790 123509", "ColecoVision"},
		{"fes.zx81", "FES ZX81 1.7.0-hil790+build.12 · deadbeef", "Sinclair ZX81"},
		{"fes.zx81", "FES ZX81 01.7.0-hil790 · deadbeef", "FES ZX81 01.7.0-hil790 · deadbeef"},
		{"fes.zx81", "ZX81 HIL790 123509", "Sinclair ZX81"},
		{"fes.spectrum-plus3", "FES ZX Spectrum +3 0.5.0 · 88766ab1", "ZX Spectrum +3"},
		{"fes.spectrum-plus3", "FES ZX Spectrum +2A/+3 1.7.0-hil790 · deadbeef", "ZX Spectrum +3"},
		{"fes.zx80", "Sinclair ZX80 1.2.3 · 01234567", "Sinclair ZX80"},
		{"fes.pong", "FES Pong", "Pong"},
		{"fes.ts1000", "FES Timex Sinclair 1000 1.7.0-hil790 · deadbeef", "Timex Sinclair 1000"},
		{"fes.sms", "FES Sega Master System HIL 790 120000", "Sega Master System"},
		{"fes.spectrum", "FES Manic Miner", "FES Manic Miner"},
		{"fes.catch", "FES Catch", "Catch"},
		{"fes.spectrum", "Manic Miner", "Manic Miner"},
		{"fes.zx81", "My ZX81 HIL790", "My ZX81 HIL790"},
		{"fes.coleco", "HIL Adventures", "HIL Adventures"},
		{"fes.coleco", "Coleco HIL Adventures", "Coleco HIL Adventures"},
		{"fes.coleco", "Coleco HIL790 Adventure", "Coleco HIL790 Adventure"},
		{"fes.zx81", "Sinclair ZX81 1.2.3 · my setup", "Sinclair ZX81 1.2.3 · my setup"},
		{"fes.zx81", "Sinclair ZX81 1.2.3 · deadbeef extra", "Sinclair ZX81 1.2.3 · deadbeef extra"},
		{"other.coleco", "Coleco HIL 1120 123509", "Coleco HIL 1120 123509"},
	} {
		if got := EntryTitle(tc.core, tc.input); got != tc.want {
			t.Errorf("%s %q: got %q want %q", tc.core, tc.input, got, tc.want)
		}
	}
}
