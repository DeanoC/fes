// SPDX-License-Identifier: GPL-3.0-or-later
package runtimeadmission

import (
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
)

// These compiled-in fixture drivers are deliberately independent of each
// package's declarations. They are not a physical Nano driver registration.
func fixtureDrivers() []Driver {
	return []Driver{{Profile: "fes-gp-v1", ABI: Contract{"fes.simple-game", 1, 0}, Interfaces: []Contract{
		{"fes.gamepad", 1, 0}, {"fes.video.fixed-720p60", 1, 0},
	}}}
}

func TestComputerMinorSemanticsDoNotInventNativeDriverSupport(t *testing.T) {
	a := fixture(t, func(s string) string {
		s = strings.ReplaceAll(s, "fes.simple-game", "fes.computer")
		s = strings.ReplaceAll(s, "fes.gamepad", "fes.keyboard.hid")
		return strings.Replace(s, "[abi]\nid = \"fes.computer\"\nmajor = 1\nminor = 0",
			"[abi]\nid = \"fes.computer\"\nmajor = 1\nminor = 1", 1)
	})
	semantic, err := NewDE10Compatibility()
	if err != nil {
		t.Fatal(err)
	}
	id := a.Inspection().PackageID
	if err = semantic.CheckArchive(a, id); err != nil {
		t.Fatal("shared ABI 1.1 semantics rejected", err)
	}
	runtime, err := NewRuntimeCompatibility(semantic, []Driver{{Profile: "fes-gp-v1",
		ABI: Contract{"fes.computer", 1, 0}, Interfaces: []Contract{
			{"fes.keyboard.hid", 1, 0}, {"fes.video.fixed-720p60", 1, 0},
		}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := ErrorCode(runtime.CheckArchive(a, id)); got != "unsupported_abi" {
		t.Fatalf("minor-0 driver admitted minor-1 package: %s", got)
	}
}

func runtimeFixture(t *testing.T, target Target, drivers []Driver) (*RuntimeCompatibility, error) {
	t.Helper()
	registry, err := NewCompatibility(target, []ProgrammingProfile{{Profile: "fes-gp-v1", ABI: "fes.simple-game", Major: 1}, {Profile: "development-contained-v1", DiagnosticOnly: true}})
	if err != nil {
		t.Fatal(err)
	}
	return NewRuntimeCompatibility(registry, drivers)
}

func TestRuntimeCompatibilityRequiresActualDriverInterfaces(t *testing.T) {
	a := fixture(t, nil)
	id := a.Inspection().PackageID
	target := Target{"de10_nano", "5CSEBA6U23I7"}
	for _, test := range []struct {
		name, code string
		change     func([]Driver)
	}{
		{"complete", "none", func([]Driver) {}},
		{"missing-video", "unsupported_interface", func(d []Driver) { d[0].Interfaces = d[0].Interfaces[:1] }},
		{"missing-gamepad", "unsupported_interface", func(d []Driver) { d[0].Interfaces = d[0].Interfaces[1:] }},
		{"no-interfaces", "unsupported_interface", func(d []Driver) { d[0].Interfaces = nil }},
		{"wrong-interface-version", "unsupported_interface", func(d []Driver) { d[0].Interfaces[1].Minor = 1 }},
		{"wrong-abi-minor", "unsupported_abi", func(d []Driver) { d[0].ABI.Minor = 1 }},
		{"wrong-profile", "unsupported_programming_profile", func(d []Driver) { d[0].Profile = "another-profile" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			drivers := fixtureDrivers()
			test.change(drivers)
			c, err := runtimeFixture(t, target, drivers)
			if err != nil {
				t.Fatal(err)
			}
			if got := ErrorCode(c.CheckArchive(a, id)); got != test.code {
				t.Fatalf("got %s want %s", got, test.code)
			}
		})
	}
}

func TestRuntimeCompatibilityDoesNotInferDriversOrAcceptDiagnosticSemantics(t *testing.T) {
	// A complete, structurally valid Nano declaration may pass only the explicit
	// fixture inventory. The generated DE10 registry still rejects the target.
	nano := fixture(t, func(s string) string {
		return strings.ReplaceAll(strings.ReplaceAll(s, "de10_nano", "de25_nano"), "5CSEBA6U23I7", "A5EB013BB23BE4SCS")
	})
	id := nano.Inspection().PackageID
	de10, err := NewDE10Compatibility()
	if err != nil {
		t.Fatal(err)
	}
	if ErrorCode(de10.CheckArchive(nano, id)) != "unsupported_target" {
		t.Fatal("DE10 registry admitted a Nano package")
	}
	c, err := runtimeFixture(t, Target{"de25_nano", "A5EB013BB23BE4SCS"}, fixtureDrivers())
	if err != nil || c.CheckArchive(nano, id) != nil {
		t.Fatal("explicit software Nano inventory rejected", err)
	}
	diagnostic := fixture(t, func(s string) string {
		return strings.ReplaceAll(strings.ReplaceAll(s, "fes-gp-v1", "de25-embedded-hps-first-rom-diagnostic-v1"), "fes.simple-game", "fes.de25-embedded-diagnostic")
	})
	drivers := fixtureDrivers()
	drivers[0].Profile = "de25-embedded-hps-first-rom-diagnostic-v1"
	drivers[0].ABI.ID = "fes.de25-embedded-diagnostic"
	registry, err := NewCompatibility(Target{"de10_nano", "5CSEBA6U23I7"}, []ProgrammingProfile{{Profile: drivers[0].Profile, ABI: drivers[0].ABI.ID, Major: 1}})
	if err != nil {
		t.Fatal(err)
	}
	c, err = NewRuntimeCompatibility(registry, drivers)
	if err != nil {
		t.Fatal(err)
	}
	if ErrorCode(c.CheckArchive(diagnostic, diagnostic.Inspection().PackageID)) != "unsupported_abi" {
		t.Fatal("matching diagnostic tuple bypassed normal ABI semantics")
	}
}

func TestRuntimeCompatibilityKeepsSemanticDependenciesAndIgnoresOptionalUnknowns(t *testing.T) {
	for _, test := range []struct {
		name, extra, code string
	}{
		{"optional-unknown", "\n[[interfaces]]\nid = \"test.optional\"\nmajor = 1\nminor = 0\nrequired = false\n", "none"},
		{"required-unknown", "\n[[interfaces]]\nid = \"test.required\"\nmajor = 1\nminor = 0\nrequired = true\n", "unsupported_interface"},
		{"persistence-without-transport", "\n[[interfaces]]\nid = \"fes.persistence.words\"\nmajor = 1\nminor = 0\nrequired = false\n", "unsupported_interface"},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := fixture(t, func(s string) string { return s + test.extra })
			drivers := fixtureDrivers()
			// Claiming support for these IDs must not bypass semantic rejection.
			drivers[0].Interfaces = append(drivers[0].Interfaces, Contract{"test.required", 1, 0}, Contract{"fes.persistence.words", 1, 0})
			c, err := runtimeFixture(t, Target{"de10_nano", "5CSEBA6U23I7"}, drivers)
			if err != nil {
				t.Fatal(err)
			}
			if got := ErrorCode(c.CheckArchive(a, a.Inspection().PackageID)); got != test.code {
				t.Fatalf("got %s want %s", got, test.code)
			}
		})
	}
}

func TestRuntimeCompatibilityRetainsDriverInventoryAndArchiveIdentity(t *testing.T) {
	drivers := fixtureDrivers()
	c, err := runtimeFixture(t, Target{"de10_nano", "5CSEBA6U23I7"}, drivers)
	if err != nil {
		t.Fatal(err)
	}
	drivers[0].Profile = "changed"
	drivers[0].ABI.Minor++
	drivers[0].Interfaces[0].ID = "changed"
	a := fixture(t, nil)
	id := a.Inspection().PackageID
	if err := c.CheckArchive(a, id); err != nil {
		t.Fatal("caller mutation changed private inventory", err)
	}
	for _, invalid := range []struct {
		a  *corepackage.Archive
		id string
	}{{nil, id}, {a, ""}, {a, strings.Repeat("f", 64)}} {
		if ErrorCode(c.CheckArchive(invalid.a, invalid.id)) != "invalid_package" {
			t.Fatal("wrong retained archive identity accepted")
		}
	}
	for _, absent := range []*RuntimeCompatibility{nil, {}} {
		if ErrorCode(absent.CheckArchive(a, id)) != "backend_unavailable" {
			t.Fatal("missing runtime inventory accepted")
		}
	}
}

func TestDriverSupportRejectsAmbiguousOrUnboundedInventory(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*Target, *[]Driver)
	}{
		{"empty", func(_ *Target, d *[]Driver) { *d = nil }},
		{"oversized", func(_ *Target, d *[]Driver) { *d = make([]Driver, 65) }},
		{"target", func(p *Target, _ *[]Driver) { p.Platform = "bad\nplatform" }},
		{"profile", func(_ *Target, d *[]Driver) { (*d)[0].Profile = "" }},
		{"abi-id", func(_ *Target, d *[]Driver) { (*d)[0].ABI.ID = "" }},
		{"abi-major", func(_ *Target, d *[]Driver) { (*d)[0].ABI.Major = 65536 }},
		{"zero-abi-major", func(_ *Target, d *[]Driver) { (*d)[0].ABI.Major = 0 }},
		{"abi-minor", func(_ *Target, d *[]Driver) { (*d)[0].ABI.Minor = -1 }},
		{"duplicate-driver", func(_ *Target, d *[]Driver) { *d = append(*d, (*d)[0]) }},
		{"duplicate-interface", func(_ *Target, d *[]Driver) { (*d)[0].Interfaces = append((*d)[0].Interfaces, (*d)[0].Interfaces[0]) }},
		{"interface-id", func(_ *Target, d *[]Driver) { (*d)[0].Interfaces[0].ID = "bad\tinterface" }},
		{"interface-version", func(_ *Target, d *[]Driver) { (*d)[0].Interfaces[0].Minor = 65536 }},
		{"oversized-interfaces", func(_ *Target, d *[]Driver) { (*d)[0].Interfaces = make([]Contract, 65) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			p, d := Target{"de10_nano", "5CSEBA6U23I7"}, fixtureDrivers()
			test.edit(&p, &d)
			if _, err := NewDriverSupport(p, d); ErrorCode(err) != "backend_unavailable" {
				t.Fatal("bad inventory accepted", err)
			}
		})
	}
}

func TestRuntimeCompatibilityKeepsMinorSpecificInterfacesSeparate(t *testing.T) {
	// Two supported ABI minors must not pool their interface inventories.
	drivers := fixtureDrivers()
	drivers[0].Interfaces = drivers[0].Interfaces[:1]
	other := fixtureDrivers()[0]
	other.ABI.Minor = 1
	drivers = append(drivers, other)
	c, err := runtimeFixture(t, Target{"de10_nano", "5CSEBA6U23I7"}, drivers)
	if err != nil {
		t.Fatal(err)
	}
	a := fixture(t, nil)
	if ErrorCode(c.CheckArchive(a, a.Inspection().PackageID)) != "unsupported_interface" {
		t.Fatal("another ABI minor supplied missing video capability")
	}
}

func TestRuntimeCompatibilityPreservesDiagnosticOnlyRegistryAndFailsClosed(t *testing.T) {
	a := fixture(t, func(s string) string { return strings.ReplaceAll(s, "fes-gp-v1", "development-contained-v1") })
	drivers := fixtureDrivers()
	drivers[0].Profile = "development-contained-v1"
	c, err := runtimeFixture(t, Target{"de10_nano", "5CSEBA6U23I7"}, drivers)
	if err != nil {
		t.Fatal(err)
	}
	if ErrorCode(c.CheckArchive(a, a.Inspection().PackageID)) != "unsupported_programming_profile" {
		t.Fatal("matching driver overrode diagnostic-only profile registry")
	}
	for _, registry := range []*Compatibility{nil, {}} {
		if _, err := NewRuntimeCompatibility(registry, fixtureDrivers()); ErrorCode(err) != "backend_unavailable" {
			t.Fatal("missing semantic registry accepted", err)
		}
	}
	for _, support := range []*DriverSupport{nil, {}} {
		if ErrorCode(support.CheckArchive(a, a.Inspection().PackageID)) != "backend_unavailable" {
			t.Fatal("missing compiled driver inventory accepted")
		}
	}
}
