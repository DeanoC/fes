package meshpref

import (
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/internal/meshplace"
)

func TestEmptyDefaults(t *testing.T) {
	m := New()
	if m.DisplayPreference() != "" || m.LastDisplaySink() != "" {
		t.Fatalf("preference %q last %q", m.DisplayPreference(), m.LastDisplaySink())
	}
	opts := m.PlaceOptions()
	if opts != (meshplace.Options{}) {
		t.Fatalf("options %+v", opts)
	}
	var zero *Memory
	if zero.DisplayPreference() != "" || zero.LastDisplaySink() != "" || zero.PlaceOptions() != (meshplace.Options{}) {
		t.Fatal("nil memory was not empty")
	}
}

func TestDisplayPreferenceRoundTrip(t *testing.T) {
	m := New()
	m.SetDisplayPreference("  kit-den  ")
	if got := m.DisplayPreference(); got != "kit-den" {
		t.Fatalf("preference = %q", got)
	}
	opts := m.PlaceOptions()
	if opts.DisplayPreference != "kit-den" || opts.LastDisplaySink != "" || opts.OverrideNodeID != "" {
		t.Fatalf("options %+v", opts)
	}
	m.SetDisplayPreference("")
	if m.DisplayPreference() != "" {
		t.Fatalf("cleared preference = %q", m.DisplayPreference())
	}
	m.SetDisplayPreference("   ")
	if m.DisplayPreference() != "" {
		t.Fatalf("blank preference = %q", m.DisplayPreference())
	}
	if m.LastDisplaySink() != "" {
		t.Fatalf("preference write stored last sink %q", m.LastDisplaySink())
	}
}

func TestLastSinkUpdatesOnlyWhenPlayStarts(t *testing.T) {
	m := New()
	m.SetDisplayPreference("kit-den")
	// Preference, reads, and an empty play id are not a play start.
	_ = m.DisplayPreference()
	_ = m.PlaceOptions()
	m.NotePlayStarted("")
	m.NotePlayStarted("   ")
	if m.LastDisplaySink() != "" {
		t.Fatalf("last sink = %q, want empty", m.LastDisplaySink())
	}

	m.NotePlayStarted(" kit-living ")
	if got := m.LastDisplaySink(); got != "kit-living" {
		t.Fatalf("last sink = %q", got)
	}
	if m.DisplayPreference() != "kit-den" {
		t.Fatalf("preference = %q", m.DisplayPreference())
	}

	// A later empty observation does not clear the remembered sink.
	m.NotePlayStarted("")
	m.SetDisplayPreference("kit-other")
	if m.LastDisplaySink() != "kit-living" {
		t.Fatalf("last sink changed to %q", m.LastDisplaySink())
	}

	m.NotePlayStarted("kit-den")
	if m.LastDisplaySink() != "kit-den" || m.DisplayPreference() != "kit-other" {
		t.Fatalf("preference %q last %q", m.DisplayPreference(), m.LastDisplaySink())
	}
}

func TestStoredIDsFeedPlace(t *testing.T) {
	entry := fpgaEntry()
	candidates := []meshplace.Candidate{fpgaCandidate("kit-living"), fpgaCandidate("kit-den")}

	tests := []struct {
		name       string
		preference string
		playSink   string
		want       meshplace.Outcome
		execute    string
	}{
		{name: "empty stays unresolved", want: meshplace.OutcomeUnresolved},
		{name: "preference selects", preference: "kit-den", want: meshplace.OutcomeSelected, execute: "kit-den"},
		{name: "last sink selects when preference is unset", playSink: "kit-living", want: meshplace.OutcomeSelected, execute: "kit-living"},
		{name: "preference wins over last sink", preference: "kit-den", playSink: "kit-living", want: meshplace.OutcomeSelected, execute: "kit-den"},
		{name: "blank preference stays unset", preference: "  ", playSink: "kit-den", want: meshplace.OutcomeSelected, execute: "kit-den"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := New()
			m.SetDisplayPreference(tc.preference)
			if tc.playSink != "" {
				m.NotePlayStarted(tc.playSink)
			}
			opts := m.PlaceOptions()
			if opts.OverrideNodeID != "" || opts.MissingRequiredSlot {
				t.Fatalf("options %+v", opts)
			}
			got := meshplace.Place(entry, candidates, opts)
			if got.Outcome != tc.want {
				t.Fatalf("outcome %q want %q (%+v)", got.Outcome, tc.want, got)
			}
			if tc.want == meshplace.OutcomeSelected && got.Choice.Execute != tc.execute {
				t.Fatalf("execute %q want %q", got.Choice.Execute, tc.execute)
			}
			if tc.want == meshplace.OutcomeUnresolved && got.Choice != (meshplace.Choice{}) {
				t.Fatalf("unresolved named %+v", got.Choice)
			}
		})
	}
}

func fpgaEntry() meshcontent.Entry {
	return meshcontent.Entry{
		TitleID:    "coleco-frogger",
		System:     "coleco",
		Launchable: true,
		Execute:    []meshcontent.Execute{{Kind: meshcontent.ExecuteFPGANative}},
		Slots: []meshcontent.Slot{
			meshcontent.PackageSlot(meshcontent.PackageABI{
				PackageID: strings.Repeat("ab", 32),
				ABI:       "fes.application",
				Major:     1,
			}),
			meshcontent.PrimaryMediaSlot(meshcontent.SumSHA256([]byte("cart"))),
		},
	}
}

func fpgaCandidate(id string) meshplace.Candidate {
	return meshplace.Candidate{
		NodeID:      id,
		MeshMajorOK: true,
		Execute:     []string{meshcontent.ExecuteFPGANative},
		DisplaySink: true,
		InputSource: true,
		ABIs:        []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}
}
