// SPDX-License-Identifier: GPL-3.0-or-later
package runtimeadmission

import (
	"strings"

	"github.com/DeanoC/FogCast/corepackage"
)

// DriverSupport is an immutable inventory supplied by platform integration.
// It checks exact driver versions, not general ABI compatibility. An archive,
// attachment or UI request must never supply this inventory. Its result grants
// no boot ownership, storage containment, memory reservation or activation.
type DriverSupport struct {
	target  Target
	drivers []Driver
}

func driverIdentifier(s string) bool {
	return s != "" && len(s) <= 128 && strings.TrimSpace(s) == s && !strings.ContainsAny(s, "\x00\r\n\t")
}

// NewDriverSupport copies the platform's bounded driver and interface inventory.
// Diagnostic loaders may use it, but normal admission must additionally check
// the ABI's semantic contract with RuntimeCompatibility.
func NewDriverSupport(target Target, drivers []Driver) (*DriverSupport, error) {
	if !driverIdentifier(target.Platform) || !driverIdentifier(target.Device) || len(drivers) == 0 || len(drivers) > 64 {
		return nil, reject("backend_unavailable", "invalid platform driver inventory")
	}
	s := &DriverSupport{target: target}
	for _, d := range drivers {
		if !driverIdentifier(d.Profile) || !driverIdentifier(d.ABI.ID) || !valid(d.ABI) || d.ABI.Major == 0 || len(d.Interfaces) > 64 {
			return nil, reject("backend_unavailable", "invalid platform driver")
		}
		for _, previous := range s.drivers {
			if d.Profile == previous.Profile && d.ABI == previous.ABI {
				return nil, reject("backend_unavailable", "duplicate platform driver")
			}
		}
		seen := make(map[string]bool)
		for _, i := range d.Interfaces {
			if !driverIdentifier(i.ID) || !valid(i) || i.Major == 0 || seen[i.ID] {
				return nil, reject("backend_unavailable", "invalid platform interface")
			}
			seen[i.ID] = true
		}
		d.Interfaces = append([]Contract(nil), d.Interfaces...)
		s.drivers = append(s.drivers, d)
	}
	return s, nil
}

func (s *DriverSupport) CheckArchive(a *corepackage.Archive, expectedID string) error {
	if a == nil || !digestID(expectedID) || a.Inspection().PackageID != expectedID {
		return reject("invalid_package", "missing retained archive or wrong package identity")
	}
	d := a.Inspection().Descriptor
	if err := corepackage.ValidateDescriptor(d); err != nil {
		return reject("invalid_package", err.Error())
	}
	if s == nil || len(s.drivers) == 0 {
		return reject("backend_unavailable", "no platform driver inventory")
	}
	if d.Target.Platform != s.target.Platform || d.Target.Device != s.target.Device {
		return reject("unsupported_target", "package target differs from platform drivers")
	}
	profile := false
	for _, driver := range s.drivers {
		if driver.Profile != d.Target.ProgrammingProfile {
			continue
		}
		profile = true
		if driver.ABI != (Contract{d.ABI.ID, d.ABI.Major, d.ABI.Minor}) {
			continue
		}
		for _, required := range d.Interfaces {
			if !required.Required {
				continue
			}
			found := false
			for _, supported := range driver.Interfaces {
				found = found || supported == (Contract{required.ID, required.Major, required.Minor})
			}
			if !found {
				return reject("unsupported_interface", "platform driver lacks required interface "+required.ID)
			}
		}
		return nil
	}
	if !profile {
		return reject("unsupported_programming_profile", "platform driver has no programming profile")
	}
	return reject("unsupported_abi", "platform driver has no exact ABI version")
}

// RuntimeCompatibility requires both the existing FES semantic policy and the
// selected platform's actual driver inventory. It accepts no per-package policy
// decisions. A matching tuple alone cannot admit an unknown/diagnostic ABI.
type RuntimeCompatibility struct {
	semantic *Compatibility
	support  *DriverSupport
}

// The independent profile registry preserves DiagnosticOnly restrictions even
// if a driver happens to advertise an otherwise matching package tuple.
func NewRuntimeCompatibility(compatibility *Compatibility, drivers []Driver) (*RuntimeCompatibility, error) {
	if compatibility == nil {
		return nil, reject("backend_unavailable", "missing platform compatibility registry")
	}
	semantic, err := NewCompatibility(compatibility.target, compatibility.profiles)
	if err != nil {
		return nil, err
	}
	support, err := NewDriverSupport(semantic.target, drivers)
	if err != nil {
		return nil, err
	}
	return &RuntimeCompatibility{semantic: semantic, support: support}, nil
}

func (c *RuntimeCompatibility) CheckArchive(a *corepackage.Archive, expectedID string) error {
	if c == nil || c.semantic == nil || c.support == nil {
		return reject("backend_unavailable", "no runtime compatibility contract")
	}
	if err := c.semantic.CheckArchive(a, expectedID); err != nil {
		return err
	}
	return c.support.CheckArchive(a, expectedID)
}
