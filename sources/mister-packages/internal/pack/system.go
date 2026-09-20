package pack

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/DeanoC/mister-packages/internal/hexnum"
)

const (
	FileWireLittleEndianBytePairs = "little_endian_byte_pairs"
	FileWireLittleEndianBytes     = "little_endian_bytes"
	maxMediaRules                 = 8
	maxSettingRules               = 16
	maxMediaSize                  = 32 * 1024 * 1024
)

func (s *SystemFile) Validate() error {
	if !validIdentifier(s.ID) {
		return fmt.Errorf("invalid system identifier %q", s.ID)
	}
	if !validCore(s.ExpectedCore) {
		return fmt.Errorf("invalid expected core %q", s.ExpectedCore)
	}
	if !validIdentifier(s.RBF.Role) {
		return fmt.Errorf("invalid rbf role %q", s.RBF.Role)
	}
	if !validArtifact(s.RBF.Artifact) {
		return fmt.Errorf("invalid rbf artifact %q", s.RBF.Artifact)
	}
	if len(s.Media) > maxMediaRules {
		return fmt.Errorf("too many media rules")
	}
	if len(s.Settings) > maxSettingRules {
		return fmt.Errorf("too many setting rules")
	}
	if err := s.Core.validate(); err != nil {
		return err
	}
	if err := s.Input.validate(); err != nil {
		return err
	}
	roles := map[string]bool{}
	indices := map[int]bool{}
	for i, rule := range s.Media {
		if err := rule.validate(); err != nil {
			return fmt.Errorf("media[%d]: %w", i, err)
		}
		if roles[rule.Role] {
			return fmt.Errorf("duplicate media role %q", rule.Role)
		}
		roles[rule.Role] = true
		if indices[rule.Index] {
			return fmt.Errorf("duplicate media index %d", rule.Index)
		}
		indices[rule.Index] = true
	}
	names := map[string]bool{}
	for i, rule := range s.Settings {
		if err := rule.validate(); err != nil {
			return fmt.Errorf("settings[%d]: %w", i, err)
		}
		if names[rule.Name] {
			return fmt.Errorf("duplicate setting %q", rule.Name)
		}
		names[rule.Name] = true
	}
	return nil
}

// MediaTransform returns the explicit transfer recipe, including the raw default.
func (r MediaRule) MediaTransform() string {
	if r.Transform == "" {
		return "raw"
	}
	return r.Transform
}

func (r MediaRule) validate() error {
	if r.MediaTransform() != "raw" && r.MediaTransform() != "snes_cartridge" && r.MediaTransform() != "nes_cartridge" {
		return fmt.Errorf("unsupported media transform %q", r.Transform)
	}
	if !validIdentifier(r.Role) {
		return fmt.Errorf("invalid role %q", r.Role)
	}
	if r.Index < 0 || r.Index > 255 {
		return fmt.Errorf("index %d out of range", r.Index)
	}
	if len(r.Extensions) == 0 {
		return fmt.Errorf("missing extensions")
	}
	size := uint64(r.MaximumSize)
	if size == 0 || size > maxMediaSize {
		return fmt.Errorf("invalid size limit 0x%x", size)
	}
	seen := map[string]bool{}
	for _, ext := range r.Extensions {
		if !validExtension(ext) {
			return fmt.Errorf("invalid extension %q", ext)
		}
		if seen[ext] {
			return fmt.Errorf("duplicate extension %q", ext)
		}
		seen[ext] = true
	}
	return nil
}

func (r SettingRule) validate() error {
	if !validIdentifier(r.Name) {
		return fmt.Errorf("invalid name %q", r.Name)
	}
	seen := map[string]bool{}
	for _, value := range r.AllowedValues {
		if value == "" || len(value) > 64 || !utf8.ValidString(value) {
			return fmt.Errorf("invalid allowed value %q", value)
		}
		if seen[value] {
			return fmt.Errorf("duplicate allowed value %q", value)
		}
		seen[value] = true
	}
	return nil
}

func (c CoreRecipe) validate() error {
	if err := requireUint16("reset_assert_word", c.ResetAssertWord); err != nil {
		return err
	}
	if err := requireUint16("initial_status_word", c.InitialStatusWord); err != nil {
		return err
	}
	if err := requireUint16("reset_release_word", c.ResetReleaseWord); err != nil {
		return err
	}
	if uint64(c.ResetAssertWord) == 0 || uint64(c.InitialStatusWord) == 0 {
		return fmt.Errorf("invalid core recipe")
	}
	if c.FileWire != FileWireLittleEndianBytePairs &&
		c.FileWire != FileWireLittleEndianBytes {
		return fmt.Errorf("unsupported file_wire %q", c.FileWire)
	}
	return nil
}

func (in InputRecipe) validate() error {
	if in.PlayerCount != 1 {
		return fmt.Errorf("invalid input recipe")
	}
	if err := requireUint16("player_command", in.PlayerCommand); err != nil {
		return err
	}
	if uint64(in.PlayerCommand) == 0 {
		return fmt.Errorf("invalid input recipe")
	}
	masks := []struct {
		name  string
		value hexnum.Uint64
	}{
		{"up", in.Up},
		{"down", in.Down},
		{"left", in.Left},
		{"right", in.Right},
		{"a", in.A},
		{"b", in.B},
		{"start", in.Start},
		{"c", in.C},
		{"x", in.X},
		{"y", in.Y},
		{"l", in.L},
		{"r", in.R},
		{"select", in.Select},
	}
	var seen uint16
	for i, mask := range masks {
		if err := requireUint16(mask.name, mask.value); err != nil {
			return err
		}
		v := uint16(mask.value)
		if (v == 0 && i < 7) || v&(v-1) != 0 || seen&v != 0 {
			return fmt.Errorf("invalid input recipe")
		}
		seen |= v
	}
	return nil
}

func (s *SystemFile) Report(w io.Writer) error {
	fmt.Fprintf(w, "system   %s\n", s.ID)
	fmt.Fprintf(w, "core     %s\n", s.ExpectedCore)
	fmt.Fprintf(w, "rbf      role=%s artifact=%s\n", s.RBF.Role, s.RBF.Artifact)
	if s.CoreSource != nil {
		fmt.Fprintf(w, "source   %s\n", s.CoreSource.ID)
		fmt.Fprintf(w, "  repo     %s\n", s.CoreSource.Repository)
		fmt.Fprintf(w, "  commit   %s\n", s.CoreSource.Commit)
		fmt.Fprintf(w, "  rbf_path %s\n", s.CoreSource.RBFPath)
		fmt.Fprintf(w, "  sha256   %s\n", s.CoreSource.RBFSHA256)
		fmt.Fprintf(w, "  size     %d\n", s.CoreSource.RBFSize)
		fmt.Fprintf(w, "  project  %s\n", s.CoreSource.Project)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "media:")
	for _, rule := range s.Media {
		fmt.Fprintf(w, "  %-12s index=%d required=%t %s max=%s transform=%s\n",
			rule.Role, rule.Index, rule.Required,
			strings.Join(rule.Extensions, ","),
			hexnum.Format(uint64(rule.MaximumSize)), rule.MediaTransform())
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "core recipe:\n")
	fmt.Fprintf(w, "  reset_assert=%s initial_status=%s reset_release=%s file_wire=%s\n",
		hexnum.Format(uint64(s.Core.ResetAssertWord)),
		hexnum.Format(uint64(s.Core.InitialStatusWord)),
		hexnum.Format(uint64(s.Core.ResetReleaseWord)),
		s.Core.FileWire)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "input:\n")
	fmt.Fprintf(w, "  players=%d command=%s\n", s.Input.PlayerCount, hexnum.Format(uint64(s.Input.PlayerCommand)))
	fmt.Fprintf(w, "  up=%s down=%s left=%s right=%s a=%s b=%s c=%s start=%s\n",
		hexnum.Format(uint64(s.Input.Up)),
		hexnum.Format(uint64(s.Input.Down)),
		hexnum.Format(uint64(s.Input.Left)),
		hexnum.Format(uint64(s.Input.Right)),
		hexnum.Format(uint64(s.Input.A)),
		hexnum.Format(uint64(s.Input.B)),
		hexnum.Format(uint64(s.Input.C)),
		hexnum.Format(uint64(s.Input.Start)))
	fmt.Fprintf(w, "  x=%s y=%s l=%s r=%s select=%s\n", hexnum.Format(uint64(s.Input.X)), hexnum.Format(uint64(s.Input.Y)), hexnum.Format(uint64(s.Input.L)), hexnum.Format(uint64(s.Input.R)), hexnum.Format(uint64(s.Input.Select)))
	return nil
}

func DiffSystemOracle(sys *SystemFile, oracle *SystemOracleFile) []string {
	var problems []string
	want := oracle.Profile
	eq := func(field, got, expected string) {
		if got != expected {
			problems = append(problems, fmt.Sprintf("%s got %q want %q", field, got, expected))
		}
	}
	eqU := func(field string, got, expected uint64) {
		if got != expected {
			problems = append(problems, fmt.Sprintf("%s got 0x%x want 0x%x", field, got, expected))
		}
	}
	eq("system", sys.ID, want.System)
	eq("expected_core", sys.ExpectedCore, want.ExpectedCore)
	eq("rbf.role", sys.RBF.Role, want.RBF.Role)
	eq("rbf.artifact", sys.RBF.Artifact, want.RBF.Artifact)
	if len(sys.Media) != len(want.Media) {
		problems = append(problems, fmt.Sprintf("media count got %d want %d", len(sys.Media), len(want.Media)))
	} else {
		for i := range want.Media {
			prefix := fmt.Sprintf("media[%d]", i)
			got, expected := sys.Media[i], want.Media[i]
			eq(prefix+".role", got.Role, expected.Role)
			eq(prefix+".transform", got.MediaTransform(), expected.MediaTransform())
			if got.Index != expected.Index {
				problems = append(problems, fmt.Sprintf("%s.index got %d want %d", prefix, got.Index, expected.Index))
			}
			if got.Required != expected.Required {
				problems = append(problems, fmt.Sprintf("%s.required got %t want %t", prefix, got.Required, expected.Required))
			}
			eqU(prefix+".maximum_size", uint64(got.MaximumSize), uint64(expected.MaximumSize))
			if strings.Join(got.Extensions, ",") != strings.Join(expected.Extensions, ",") {
				problems = append(problems, fmt.Sprintf("%s.extensions got %q want %q",
					prefix, strings.Join(got.Extensions, ","), strings.Join(expected.Extensions, ",")))
			}
		}
	}
	eqU("core.reset_assert_word", uint64(sys.Core.ResetAssertWord), uint64(want.Core.ResetAssertWord))
	eqU("core.initial_status_word", uint64(sys.Core.InitialStatusWord), uint64(want.Core.InitialStatusWord))
	eqU("core.reset_release_word", uint64(sys.Core.ResetReleaseWord), uint64(want.Core.ResetReleaseWord))
	eq("core.file_wire", sys.Core.FileWire, want.Core.FileWire)
	if sys.Input.PlayerCount != want.Input.PlayerCount {
		problems = append(problems, fmt.Sprintf("input.player_count got %d want %d",
			sys.Input.PlayerCount, want.Input.PlayerCount))
	}
	eqU("input.player_command", uint64(sys.Input.PlayerCommand), uint64(want.Input.PlayerCommand))
	eqU("input.up", uint64(sys.Input.Up), uint64(want.Input.Up))
	eqU("input.down", uint64(sys.Input.Down), uint64(want.Input.Down))
	eqU("input.left", uint64(sys.Input.Left), uint64(want.Input.Left))
	eqU("input.right", uint64(sys.Input.Right), uint64(want.Input.Right))
	eqU("input.a", uint64(sys.Input.A), uint64(want.Input.A))
	eqU("input.b", uint64(sys.Input.B), uint64(want.Input.B))
	eqU("input.c", uint64(sys.Input.C), uint64(want.Input.C))
	eqU("input.start", uint64(sys.Input.Start), uint64(want.Input.Start))
	eqU("input.x", uint64(sys.Input.X), uint64(want.Input.X))
	eqU("input.y", uint64(sys.Input.Y), uint64(want.Input.Y))
	eqU("input.l", uint64(sys.Input.L), uint64(want.Input.L))
	eqU("input.r", uint64(sys.Input.R), uint64(want.Input.R))
	eqU("input.select", uint64(sys.Input.Select), uint64(want.Input.Select))
	return problems
}

func validIdentifier(value string) bool {
	if value == "" || len(value) > 32 {
		return false
	}
	for i := 0; i < len(value); i++ {
		b := value[i]
		if (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '_' || b == '-' {
			continue
		}
		return false
	}
	return true
}

func validCore(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 0x20 || value[i] > 0x7e {
			return false
		}
	}
	return true
}

func validExtension(value string) bool {
	if len(value) < 2 || len(value) > 16 || value[0] != '.' {
		return false
	}
	for i := 1; i < len(value); i++ {
		b := value[i]
		if (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') {
			continue
		}
		return false
	}
	return true
}

func validArtifact(name string) bool {
	if name == "" || strings.ContainsAny(name, `/\`) || strings.ContainsRune(name, 0) {
		return false
	}
	return strings.HasSuffix(name, ".rbf")
}

func requireUint16(name string, value hexnum.Uint64) error {
	if uint64(value) > 0xffff {
		return fmt.Errorf("%s 0x%x exceeds uint16", name, uint64(value))
	}
	return nil
}
