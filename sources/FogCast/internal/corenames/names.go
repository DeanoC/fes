// Package corenames supplies presentation names for first-party machines.
// It does not change package metadata, identities or admission.
package corenames

import (
	"strings"

	"github.com/DeanoC/FogCast/corepackage"
)

type model struct {
	name    string
	aliases []string
}

var models = map[string]model{
	"fes.zx81":           {"Sinclair ZX81", []string{"ZX81", "Zx81"}},
	"fes.zx80":           {"Sinclair ZX80", []string{"ZX80"}},
	"fes.ts1000":         {"Timex Sinclair 1000", []string{"TS1000", "Timex Sinclair TS1000"}},
	"fes.ts1500":         {"Timex Sinclair 1500", []string{"TS1500", "Timex Sinclair TS1500"}},
	"fes.c64":            {"Commodore 64", []string{"C64"}},
	"fes.spectrum":       {"ZX Spectrum 48K", []string{"ZX Spectrum", "Spectrum 48K"}},
	"fes.spectrum128":    {"ZX Spectrum 128K", []string{"Spectrum 128K"}},
	"fes.spectrum-plus3": {"ZX Spectrum +3", []string{"Spectrum +3", "ZX Spectrum +2A/+3"}},
	"fes.pentagon":       {"Pentagon 128", []string{"Pentagon"}},
	"fes.apple2":         {"Apple II", nil},
	"fes.atari-st":       {"Atari ST", []string{"Atari 520ST"}},
	"fes.coleco":         {"ColecoVision", []string{"Coleco"}},
	"fes.sms":            {"Sega Master System", []string{"Master System", "SMS"}},
	"fes.sg1000":         {"Sega SG-1000", []string{"SG-1000"}},
	"fes.pong":           {"Pong", nil},
	"fes.catch":          {"Catch", nil},
	"fes.ramtest":        {"RAM Tester", nil},
	"fes.riscv":          {"RISC-V", nil},
}

// Name uses an exact registered core ID, preserving unknown package names.
func Name(coreID, fallback string) string {
	if m, ok := models[coreID]; ok {
		return m.name
	}
	if fallback != "" {
		return fallback
	}
	return coreID
}

// EntryTitle recognizes only machine defaults previously saved by setup or
// diagnostic tools. Genuine game/custom titles and unknown cores stay verbatim.
// The caller projects this label; it never updates the stored library entry.
func EntryTitle(coreID, title string) string {
	m, ok := models[coreID]
	if !ok {
		return title
	}
	candidate := strings.TrimPrefix(title, "FES ")
	if generatedTitle(candidate, m.name) {
		return m.name
	}
	for _, alias := range m.aliases {
		if generatedTitle(candidate, alias) {
			return m.name
		}
	}
	return title
}

func generatedTitle(title, base string) bool {
	if title == base {
		return true
	}
	suffix, ok := strings.CutPrefix(title, base+" ")
	if !ok {
		return false
	}
	fields := strings.Fields(suffix)
	if len(fields) >= 1 && len(fields) <= 3 {
		issue := strings.TrimPrefix(strings.TrimPrefix(fields[0], "HIL"), "-")
		if fields[0] == "HIL" && len(fields) >= 2 {
			issue, fields = fields[1], fields[1:]
		}
		if strings.HasPrefix(suffix, "HIL") && decimal(issue) && (len(fields) == 1 || len(fields) == 2 && decimal(fields[1])) {
			return true
		}
	}
	version, digest, ok := strings.Cut(suffix, " · ")
	if !ok || len(digest) != 8 {
		return false
	}
	if !corepackage.ValidVersion(version) {
		return false
	}
	for _, c := range digest {
		if c < '0' || c > '9' {
			if c < 'a' || c > 'f' {
				return false
			}
		}
	}
	return true
}

func decimal(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
