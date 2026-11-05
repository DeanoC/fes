package expansion

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

const (
	StaticDevice           = "A5EB013BB23BE4SCS"
	StaticMap              = "fes.de25-zx81-bus.partial/1"
	EmptyPersonaID  uint32 = 0xc001a001
	RAM16KPersonaID uint32 = 0xc002b002
)

// StaticPersona binds an opaque vendor partial image to the frozen base. This
// is metadata equality, not decoding or qualifying a vendor bitstream.
type StaticPersona struct {
	ControlBuildID   string `json:"control_build_id"`
	HardwareID       uint32 `json:"hardware_id"`
	RootQDBSHA256    string `json:"root_qdb_sha256"`
	InterfaceSHA256  string `json:"interface_sha256"`
	ClockResetSHA256 string `json:"clock_reset_sha256"`
	POF0Hash         string `json:"pof0_hash"`
}

func (p StaticPersona) validate() error {
	if !hex32.MatchString(p.ControlBuildID) || p.ControlBuildID == strings.Repeat("0", 32) || (p.HardwareID != EmptyPersonaID && p.HardwareID != RAM16KPersonaID) {
		return errors.New("invalid static controller or persona identity")
	}
	for _, sum := range []string{p.RootQDBSHA256, p.InterfaceSHA256, p.ClockResetSHA256, p.POF0Hash} {
		if !hex64.MatchString(sum) || sum == strings.Repeat("0", 64) {
			return errors.New("invalid frozen static root identity")
		}
	}
	return nil
}
func sameStaticRoot(a, b StaticPersona) bool {
	a.HardwareID = 0
	b.HardwareID = 0
	return a == b
}
func (m Manifest) validateStaticPersona() error {
	if m.Device != StaticDevice || m.Slot != Slot || m.Map != StaticMap || m.SlotMajor != 2 || m.SlotMinor != 0 || m.SlotIndex != 0 || m.BoundaryPatch != nil || m.Partial == nil {
		return errors.New("unsupported static partial target or slot")
	}
	if err := m.Partial.validate(); err != nil {
		return err
	}
	if !hex64.MatchString(m.CartSHA256) || !hex64.MatchString(m.ShellPackageID) || !hex64.MatchString(m.ShellSHA256) || !hex64.MatchString(m.RecipeSHA256) || !hex40.MatchString(m.Revision) || !hex32.MatchString(m.ShellBuildID) {
		return errors.New("invalid static expansion identity")
	}
	if m.CartSize < 1 || m.CartSize > maxRBFBytes {
		return errors.New("invalid opaque partial payload size")
	}
	return nil
}
func assetIdentity(format int, canonical []byte) string {
	domain := "fes-expansion-v1\x00"
	if format == 2 {
		domain = "fes-expansion-v2\x00"
	}
	return hash(append([]byte(domain), canonical...))
}

// StaticShell comes from an independently verified format5 package. Its empty
// persona is part of that package, so an empty selection still loads two images.
type StaticShell struct {
	BaseSHA256, DefaultPersonaSHA256 string
	PackageID, BuildID               string
	Base, DefaultPersona             []byte
	Default                          StaticPersona
}

// StaticComposition is a next-launch identity over two unchanged images. It is
// never a monolithic RBF and contains no operation on a running machine.
type StaticComposition struct {
	ID             string `json:"composition_id"`
	PackageID      string `json:"package_id"`
	ExpansionID    string `json:"expansion_id,omitempty"`
	BaseSHA256     string `json:"base_sha256"`
	BaseSize       int64  `json:"base_size"`
	PersonaSHA256  string `json:"persona_sha256"`
	PersonaSize    int64  `json:"persona_size"`
	ControlBuildID string `json:"control_build_id"`
	HardwareID     uint32 `json:"hardware_id"`
}

func (s StaticShell) validate() error {
	if !hex64.MatchString(s.PackageID) || !hex32.MatchString(s.BuildID) || len(s.Base) < 1 || len(s.Base) > maxRBFBytes || len(s.DefaultPersona) < 1 || len(s.DefaultPersona) > maxRBFBytes || s.Default.HardwareID != EmptyPersonaID {
		return errors.New("invalid sealed static base or default persona")
	}
	if !hex64.MatchString(s.BaseSHA256) || !hex64.MatchString(s.DefaultPersonaSHA256) || hash(s.Base) != s.BaseSHA256 || hash(s.DefaultPersona) != s.DefaultPersonaSHA256 {
		return errors.New("static package payload differs from its sealed descriptor")
	}
	return s.Default.validate()
}

// ComposeStatic validates the selected asset against the exact package, base,
// build and frozen root. A nil asset chooses the sealed default empty persona.
// No device is opened and neither image is edited or concatenated.
func ComposeStatic(shell StaticShell, selected *Asset) (StaticComposition, error) {
	return ComposeStaticContext(context.Background(), shell, selected)
}

func ComposeStaticContext(ctx context.Context, shell StaticShell, selected *Asset) (StaticComposition, error) {
	if err := ctx.Err(); err != nil {
		return StaticComposition{}, err
	}
	if err := shell.validate(); err != nil {
		return StaticComposition{}, err
	}
	plan := StaticComposition{PackageID: shell.PackageID, BaseSHA256: shell.BaseSHA256, BaseSize: int64(len(shell.Base)), PersonaSHA256: shell.DefaultPersonaSHA256, PersonaSize: int64(len(shell.DefaultPersona)), ControlBuildID: shell.Default.ControlBuildID, HardwareID: shell.Default.HardwareID}
	if selected != nil {
		if err := selected.Validate(); err != nil {
			return StaticComposition{}, err
		}
		m := selected.Manifest
		if m.Format != 2 || m.Partial == nil {
			return StaticComposition{}, errors.New("static base requires an opaque partial expansion")
		}
		if m.ShellPackageID != shell.PackageID || m.ShellBuildID != shell.BuildID || m.ShellSHA256 != plan.BaseSHA256 || !sameStaticRoot(shell.Default, *m.Partial) {
			return StaticComposition{}, errors.New("partial does not match selected package/base/frozen root")
		}
		plan.ExpansionID = selected.ID
		plan.PersonaSHA256 = m.CartSHA256
		plan.PersonaSize = m.CartSize
		plan.HardwareID = m.Partial.HardwareID
	}
	if err := ctx.Err(); err != nil {
		return StaticComposition{}, err
	}
	material := []string{plan.PackageID, plan.ExpansionID, plan.BaseSHA256, strconv.FormatInt(plan.BaseSize, 10), plan.PersonaSHA256, strconv.FormatInt(plan.PersonaSize, 10), plan.ControlBuildID, strconv.FormatUint(uint64(plan.HardwareID), 10)}
	plan.ID = hash([]byte("fes-de25-static-composition-v1\x00" + strings.Join(material, "\x00")))
	return plan, nil
}
