package core

import (
	"sort"

	"github.com/DeanoC/FogCast-POC/internal/systems"
	"github.com/DeanoC/FogCast-POC/protocol"
)

type Spec struct {
	System       protocol.System
	ExpectedCore string
	RBFSelector  string
	ROMRoot      string
	MGLRoot      string
	Extensions   map[string]struct{}
	FileDelay    int
	FileType     string
	FileIndex    int
}

func extensionSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

type Registry struct {
	bySystem           map[protocol.System]Spec
	byObserved         map[string]protocol.System
	observedCandidates map[string]map[protocol.System]struct{}
}

func DefaultRegistry() Registry {
	var specs []Spec
	for _, row := range systems.Rows() {
		if row.Capability != systems.CapabilityFPGANative || row.LaunchSystem == "" || row.Core == nil {
			continue
		}
		specs = append(specs, Spec{
			System: row.LaunchSystem, ExpectedCore: row.Core.ExpectedCore, RBFSelector: row.Core.RBF,
			ROMRoot: row.Core.KitROMRoot, MGLRoot: row.Core.MGLRoot, Extensions: extensionSet(row.Extensions...),
			FileDelay: row.Core.FileDelay, FileType: row.Core.FileType, FileIndex: row.Core.FileIndex,
		})
	}
	return NewRegistry(specs...)
}

func NewRegistry(specs ...Spec) Registry {
	result := Registry{
		bySystem:           make(map[protocol.System]Spec, len(specs)),
		byObserved:         make(map[string]protocol.System, len(specs)),
		observedCandidates: make(map[string]map[protocol.System]struct{}, len(specs)),
	}
	for _, spec := range specs {
		result.bySystem[spec.System] = cloneSpec(spec)
		if spec.ExpectedCore != "" {
			candidates := result.observedCandidates[spec.ExpectedCore]
			if candidates == nil {
				candidates = make(map[protocol.System]struct{})
				result.observedCandidates[spec.ExpectedCore] = candidates
			}
			candidates[spec.System] = struct{}{}
			if len(candidates) == 1 {
				result.byObserved[spec.ExpectedCore] = spec.System
			} else if _, sms := candidates[protocol.SystemSMS]; sms {
				// SMS and Game Gear intentionally share the MiSTer core name.
				// Keep SMS as the explicit no-intent fallback; Coordinator uses
				// the validated pending launch system to select Game Gear.
				result.byObserved[spec.ExpectedCore] = protocol.SystemSMS
			} else {
				delete(result.byObserved, spec.ExpectedCore)
			}
		}
	}
	return result
}

func (r Registry) Lookup(system protocol.System) (Spec, bool) {
	spec, ok := r.bySystem[system]
	return cloneSpec(spec), ok
}

func (r Registry) Specs() []Spec {
	specs := make([]Spec, 0, len(r.bySystem))
	for _, spec := range r.bySystem {
		specs = append(specs, cloneSpec(spec))
	}
	sort.Slice(specs, func(left, right int) bool {
		return specs[left].System < specs[right].System
	})
	return specs
}

func (r Registry) LookupObserved(name string) (Spec, bool) {
	system, ok := r.byObserved[name]
	if !ok {
		return Spec{}, false
	}
	return r.Lookup(system)
}

// LookupObservedForSystem resolves name only when it is the expected core for
// the requested system. It is used when durable launch intent disambiguates a
// shared observed core name such as SMS.
func (r Registry) LookupObservedForSystem(name string, system protocol.System) (Spec, bool) {
	spec, ok := r.Lookup(system)
	if !ok || spec.ExpectedCore != name {
		return Spec{}, false
	}
	return spec, true
}

// RecognizesObserved reports whether name belongs to any registered core.
// It deliberately does not require a unique system mapping; SMS is shared by
// Master System and Game Gear and is resolved by launch intent when available.
func (r Registry) RecognizesObserved(name string) bool {
	_, ok := r.observedCandidates[name]
	return ok
}

func cloneSpec(spec Spec) Spec {
	copy := spec
	copy.Extensions = make(map[string]struct{}, len(spec.Extensions))
	for extension := range spec.Extensions {
		copy.Extensions[extension] = struct{}{}
	}
	return copy
}
