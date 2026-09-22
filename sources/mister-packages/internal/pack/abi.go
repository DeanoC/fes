package pack

import (
	"fmt"
	"math"
	"strings"
)

// LoadABI reads and validates one declarative ABI contract.
func LoadABI(path string) (*ABIFile, error) {
	var abi ABIFile
	if err := readStrictYAML(path, &abi); err != nil {
		return nil, err
	}
	if err := checkHeader(abi.Schema, abi.Kind, "abi", path); err != nil {
		return nil, err
	}
	if err := abi.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &abi, nil
}

// LoadProgrammingProfiles reads the supported platform profile/ABI-major
// registry. Diagnostic-only profiles intentionally have no ABI pairings.
func LoadProgrammingProfiles(path string) (*ProgrammingProfilesFile, error) {
	var profiles ProgrammingProfilesFile
	if err := readStrictYAML(path, &profiles); err != nil {
		return nil, err
	}
	if err := checkHeader(profiles.Schema, profiles.Kind, "programming_profiles", path); err != nil {
		return nil, err
	}
	if err := profiles.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &profiles, nil
}

func (a *ABIFile) Validate() error {
	if !validABIIdentifier(a.ID) {
		return fmt.Errorf("invalid ABI identifier %q", a.ID)
	}
	if a.Major == 0 {
		return fmt.Errorf("ABI major must be at least 1")
	}
	constantNames := make(map[string]bool, len(a.Constants))
	for i, constant := range a.Constants {
		if !validABIConstantName(constant.Name) {
			return fmt.Errorf("constants[%d]: invalid name %q", i, constant.Name)
		}
		if constantNames[constant.Name] {
			return fmt.Errorf("constants[%d]: duplicate name %q", i, constant.Name)
		}
		constantNames[constant.Name] = true
		if uint64(constant.Value) > math.MaxUint32 {
			return fmt.Errorf("constants[%d]: value 0x%x exceeds uint32", i, uint64(constant.Value))
		}
	}
	interfaceIDs := make(map[string]bool, len(a.Interfaces))
	capabilityBits := make(map[uint8]bool, len(a.Interfaces))
	for i, iface := range a.Interfaces {
		if !validABIIdentifier(iface.ID) {
			return fmt.Errorf("interfaces[%d]: invalid identifier %q", i, iface.ID)
		}
		if iface.Major == 0 {
			return fmt.Errorf("interfaces[%d]: major must be at least 1", i)
		}
		if iface.CapabilityBit > 31 {
			return fmt.Errorf("interfaces[%d]: capability bit %d out of range", i, iface.CapabilityBit)
		}
		if interfaceIDs[iface.ID] {
			return fmt.Errorf("interfaces[%d]: duplicate identifier %q", i, iface.ID)
		}
		if capabilityBits[iface.CapabilityBit] {
			return fmt.Errorf("interfaces[%d]: duplicate capability bit %d", i, iface.CapabilityBit)
		}
		interfaceIDs[iface.ID] = true
		capabilityBits[iface.CapabilityBit] = true
	}
	return nil
}

func (a *ABIFile) Constant(name string) (uint32, bool) {
	for _, constant := range a.Constants {
		if constant.Name == name {
			return uint32(constant.Value), true
		}
	}
	return 0, false
}

func (p *ProgrammingProfilesFile) Validate() error {
	if !validIdentifier(p.ID) {
		return fmt.Errorf("invalid programming registry identifier %q", p.ID)
	}
	if !validIdentifier(p.Platform) {
		return fmt.Errorf("invalid platform identifier %q", p.Platform)
	}
	if p.Device == "" || len(p.Device) > 64 || strings.ContainsAny(p.Device, " \t\r\n") {
		return fmt.Errorf("invalid device %q", p.Device)
	}
	if len(p.Profiles) == 0 {
		return fmt.Errorf("no programming profiles")
	}
	profileIDs := make(map[string]bool, len(p.Profiles))
	for i, profile := range p.Profiles {
		if !validABIIdentifier(profile.ID) {
			return fmt.Errorf("profiles[%d]: invalid identifier %q", i, profile.ID)
		}
		if profileIDs[profile.ID] {
			return fmt.Errorf("profiles[%d]: duplicate identifier %q", i, profile.ID)
		}
		profileIDs[profile.ID] = true
		if profile.DiagnosticOnly && len(profile.ABIs) != 0 {
			return fmt.Errorf("profiles[%d]: diagnostic-only profile has ABI pairings", i)
		}
		if !profile.DiagnosticOnly && len(profile.ABIs) == 0 {
			return fmt.Errorf("profiles[%d]: missing ABI pairings", i)
		}
		pairs := make(map[string]bool, len(profile.ABIs))
		for j, abi := range profile.ABIs {
			if !validABIIdentifier(abi.ID) || abi.Major == 0 {
				return fmt.Errorf("profiles[%d].abis[%d]: invalid ABI-major pairing", i, j)
			}
			key := fmt.Sprintf("%s/%d", abi.ID, abi.Major)
			if pairs[key] {
				return fmt.Errorf("profiles[%d].abis[%d]: duplicate ABI-major pairing %q", i, j, key)
			}
			pairs[key] = true
		}
	}
	return nil
}

func validABIIdentifier(value string) bool {
	if len(value) == 0 || len(value) > 96 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for i := 1; i < len(value); i++ {
		b := value[i]
		if (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '_' || b == '-' || b == '.' {
			continue
		}
		return false
	}
	return true
}

func validABIConstantName(value string) bool {
	if len(value) == 0 || len(value) > 64 || !((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) {
		return false
	}
	for i := 1; i < len(value); i++ {
		b := value[i]
		if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') {
			continue
		}
		return false
	}
	return true
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
