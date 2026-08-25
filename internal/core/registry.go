package core

import (
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
	bySystem map[protocol.System]Spec
}

func DefaultRegistry() Registry {
	var specs []Spec
	for _, row := range systems.Rows() {
		if row.Capability != systems.CapabilityFPGANative || row.Core == nil {
			continue
		}
		specs = append(specs, Spec{
			System: row.PlatformID, ExpectedCore: row.Core.ExpectedCore, RBFSelector: row.Core.RBF,
			ROMRoot: row.Core.KitROMRoot, MGLRoot: row.Core.MGLRoot, Extensions: extensionSet(row.Extensions...),
			FileDelay: row.Core.FileDelay, FileType: row.Core.FileType, FileIndex: row.Core.FileIndex,
		})
	}
	return NewRegistry(specs...)
}

func NewRegistry(specs ...Spec) Registry {
	result := Registry{bySystem: make(map[protocol.System]Spec, len(specs))}
	for _, spec := range specs {
		result.bySystem[spec.System] = cloneSpec(spec)
	}
	return result
}

func (r Registry) Lookup(system protocol.System) (Spec, bool) {
	spec, ok := r.bySystem[system]
	return cloneSpec(spec), ok
}

func (r Registry) LookupObserved(name string) (Spec, bool) {
	for _, spec := range r.bySystem {
		if spec.ExpectedCore == name {
			return cloneSpec(spec), true
		}
	}
	return Spec{}, false
}

func cloneSpec(spec Spec) Spec {
	copy := spec
	copy.Extensions = make(map[string]struct{}, len(spec.Extensions))
	for extension := range spec.Extensions {
		copy.Extensions[extension] = struct{}{}
	}
	return copy
}
