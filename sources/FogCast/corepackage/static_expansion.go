package corepackage

import (
	"errors"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// StaticExpansion binds the default empty persona to a format5 full base.
// It describes a cold launch, not an operation on a running core. An optional
// expansion must independently bind this package and its frozen root identities.
// These structural fields do not advertise or enable a runtime profile.
type StaticExpansion struct {
	File             string `toml:"file" json:"file"`
	Size             int64  `toml:"size" json:"size"`
	SHA256           string `toml:"sha256" json:"sha256"`
	ControlBuildID   string `toml:"control_build_id" json:"control_build_id"`
	HardwareID       int64  `toml:"hardware_id" json:"hardware_id"`
	RootQDBSHA256    string `toml:"root_qdb_sha256" json:"root_qdb_sha256"`
	InterfaceSHA256  string `toml:"interface_sha256" json:"interface_sha256"`
	ClockResetSHA256 string `toml:"clock_reset_sha256" json:"clock_reset_sha256"`
	POF0Hash         string `toml:"pof0_hash" json:"pof0_hash"`
}

var staticExpansionKeys = []string{"file", "size", "sha256", "control_build_id", "hardware_id", "root_qdb_sha256", "interface_sha256", "clock_reset_sha256", "pof0_hash"}

func validateStaticExpansion(d Descriptor) error {
	if d.Format != 5 {
		if d.StaticExpansion != nil {
			return errors.New("static expansion fields require format 5")
		}
		return nil
	}
	s := d.StaticExpansion
	if s == nil || d.Target.Platform != "de25_nano" || d.Target.Device != "A5EB013BB23BE4SCS" || d.Target.ProgrammingProfile != "de25-linux-static-computer-v1" {
		return errors.New("format 5 requires the explicit DE25 static computer target")
	}
	if s.File != "persona.rbf" || s.Size < 1 || s.Size > MaxPayloadSize || s.HardwareID != 0xc001a001 {
		return errors.New("format 5 requires one bounded default empty persona")
	}
	if !hex32RE.MatchString(s.ControlBuildID) || s.ControlBuildID == strings.Repeat("0", 32) {
		return errors.New("invalid static controller build identity")
	}
	for _, sum := range []string{s.SHA256, s.RootQDBSHA256, s.InterfaceSHA256, s.ClockResetSHA256, s.POF0Hash} {
		if !hex64RE.MatchString(sum) || sum == strings.Repeat("0", 64) {
			return errors.New("invalid static base/persona binding")
		}
	}
	return nil
}

// Call only after manifestHasCompanion has validated the format. Both archive
// and directory paths use this same closed companion name and size limit.
func companionName(manifest []byte) string {
	var header struct {
		Format int64 `toml:"format"`
	}
	if err := toml.Unmarshal(manifest, &header); err == nil && header.Format == 5 {
		return "persona.rbf"
	}
	return "rom-map.json"
}
func companionLimit(manifest []byte) int64 {
	if companionName(manifest) == "persona.rbf" {
		return MaxPayloadSize
	}
	return MaxROMMapSize
}
