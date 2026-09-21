package emitcpp

import (
	"fmt"
	"strings"

	"github.com/DeanoC/mister-packages/internal/pack"
)

func Generate(resolved *pack.Resolved) (string, error) {
	symbols, err := resolved.Symbols()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	writeGeneratedPreamble(&b, "packages/platform/"+resolved.Platform.ID+".yaml")
	b.WriteString("#include <cstdint>\n\n")
	b.WriteString("namespace mister {\n")
	b.WriteString("namespace native {\n")
	b.WriteString("namespace generated {\n\n")
	for _, symbol := range symbols {
		fmt.Fprintf(&b, "constexpr std::uint32_t %s = 0x%xu;\n", symbol.Name, symbol.Value)
	}
	b.WriteString("\n} // namespace generated\n")
	b.WriteString("} // namespace native\n")
	b.WriteString("} // namespace mister\n")
	return b.String(), nil
}

// GenerateABI emits C++14 constants for an ABI contract. The output is target
// text; runtime code remains responsible for interpreting the contract.
func GenerateABI(abi *pack.ABIFile) (string, error) {
	if err := abi.Validate(); err != nil {
		return "", err
	}
	if err := validateABISymbols(abi); err != nil {
		return "", err
	}
	prefix := abiSymbolPrefix(abi)
	var b strings.Builder
	writeGeneratedPreamble(&b, "packages/abi/"+packageFileName(abi.ID)+".yaml")
	b.WriteString("#include <cstdint>\n\n")
	b.WriteString("namespace mister {\nnamespace native {\nnamespace generated {\n\n")
	fmt.Fprintf(&b, "constexpr const char* %sABIID = %s;\n", prefix, cstring(abi.ID))
	fmt.Fprintf(&b, "constexpr std::uint16_t %sABIMajor = %du;\n", prefix, abi.Major)
	fmt.Fprintf(&b, "constexpr std::uint16_t %sABIMinor = %du;\n", prefix, abi.Minor)
	fmt.Fprintf(&b, "constexpr std::uint16_t %sABITag = %du;\n", prefix, abi.Tag)
	for _, constant := range abi.Constants {
		fmt.Fprintf(&b, "constexpr std::uint32_t %s = 0x%xu;\n", constant.Name, uint64(constant.Value))
	}
	for _, iface := range abi.Interfaces {
		suffix := abiInterfaceSuffix(iface.ID)
		fmt.Fprintf(&b, "constexpr const char* %sInterface%sID = %s;\n", prefix, suffix, cstring(iface.ID))
		fmt.Fprintf(&b, "constexpr std::uint16_t %sInterface%sMajor = %du;\n", prefix, suffix, iface.Major)
		fmt.Fprintf(&b, "constexpr std::uint16_t %sInterface%sMinor = %du;\n", prefix, suffix, iface.Minor)
		fmt.Fprintf(&b, "constexpr std::uint32_t %sCapability%s = 0x%xu;\n", prefix, suffix, uint32(1)<<iface.CapabilityBit)
	}
	b.WriteString("\n} // namespace generated\n} // namespace native\n} // namespace mister\n")
	return b.String(), nil
}

func validateABISymbols(abi *pack.ABIFile) error {
	prefix := abiSymbolPrefix(abi)
	seen := map[string]bool{}
	add := func(name string) error {
		if seen[name] {
			return fmt.Errorf("ABI %q generates duplicate C++ symbol %q", abi.ID, name)
		}
		seen[name] = true
		return nil
	}
	for _, name := range []string{prefix + "ABIID", prefix + "ABIMajor", prefix + "ABIMinor", prefix + "ABITag"} {
		if err := add(name); err != nil {
			return err
		}
	}
	for _, constant := range abi.Constants {
		if err := add(constant.Name); err != nil {
			return err
		}
	}
	for _, iface := range abi.Interfaces {
		suffix := abiInterfaceSuffix(iface.ID)
		for _, name := range []string{prefix + "Interface" + suffix + "ID", prefix + "Interface" + suffix + "Major", prefix + "Interface" + suffix + "Minor", prefix + "Capability" + suffix} {
			if err := add(name); err != nil {
				return err
			}
		}
	}
	return nil
}

// GenerateProgrammingProfiles emits the approved profile/ABI-major rows. A
// diagnostic-only row has a null ABI and major zero by design.
func GenerateProgrammingProfiles(profiles *pack.ProgrammingProfilesFile) (string, error) {
	if err := profiles.Validate(); err != nil {
		return "", err
	}
	prefix := cppIdent(profiles.Platform)
	var b strings.Builder
	writeGeneratedPreamble(&b, "packages/programming/"+packageFileName(profiles.ID)+".yaml")
	b.WriteString("#include <cstddef>\n#include <cstdint>\n\n")
	b.WriteString("namespace mister {\nnamespace native {\nnamespace generated {\n\n")
	b.WriteString("struct GeneratedProgrammingProfilePair {\n  const char* profile;\n  const char* abi;\n  std::uint16_t major;\n  bool diagnostic_only;\n};\n\n")
	fmt.Fprintf(&b, "constexpr const char* k%sProgrammingPlatform = %s;\n", prefix, cstring(profiles.Platform))
	fmt.Fprintf(&b, "constexpr const char* k%sProgrammingDevice = %s;\n\n", prefix, cstring(profiles.Device))
	fmt.Fprintf(&b, "static constexpr GeneratedProgrammingProfilePair k%sProgrammingProfilePairs[] = {\n", prefix)
	count := 0
	for _, profile := range profiles.Profiles {
		if profile.DiagnosticOnly {
			fmt.Fprintf(&b, "  {%s, nullptr, 0, true},\n", cstring(profile.ID))
			count++
			continue
		}
		for _, abi := range profile.ABIs {
			fmt.Fprintf(&b, "  {%s, %s, %d, false},\n", cstring(profile.ID), cstring(abi.ID), abi.Major)
			count++
		}
	}
	b.WriteString("};\n")
	fmt.Fprintf(&b, "constexpr std::size_t k%sProgrammingProfilePairCount = %d;\n", prefix, count)
	b.WriteString("\n} // namespace generated\n} // namespace native\n} // namespace mister\n")
	return b.String(), nil
}

func writeGeneratedPreamble(b *strings.Builder, source string) {
	b.WriteString("// Copyright 2026 FogCast contributors\n")
	b.WriteString("// SPDX-License-Identifier: GPL-3.0-or-later\n\n")
	b.WriteString("// Code generated by mister-packages from ")
	b.WriteString(source)
	b.WriteString(".\n")
	b.WriteString("// Target C++14 (ARMv7 Linux HPS). Do not edit.\n\n")
	b.WriteString("#pragma once\n\n")
}

func cppIdent(value string) string {
	var b strings.Builder
	start := true
	for _, r := range value {
		ok := (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if !ok {
			start = true
			continue
		}
		if start {
			if r >= 'a' && r <= 'z' {
				r = r - 'a' + 'A'
			}
			start = false
		}
		b.WriteRune(r)
	}
	return b.String()
}

func abiSymbolPrefix(abi *pack.ABIFile) string {
	if abi.ID == "fes.simple-game" {
		return "FesGp"
	}
	return cppIdent(abi.ID)
}

func abiInterfaceSuffix(id string) string {
	if strings.HasPrefix(id, "fes.") {
		id = strings.TrimPrefix(id, "fes.")
	}
	return cppIdent(id)
}

func packageFileName(id string) string {
	return strings.NewReplacer(".", "_", "-", "_", "/", "_").Replace(id)
}

func cstring(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + replacer.Replace(value) + `"`
}

func boolLit(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
