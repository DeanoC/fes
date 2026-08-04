package core

import "github.com/DeanoC/FogCast-POC/protocol"

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

var defaults = []Spec{
	{
		System: protocol.SystemMegaDrive, ExpectedCore: "MegaDrive", RBFSelector: "_Console/MegaDrive",
		ROMRoot: "/media/fat/games/MegaDrive", MGLRoot: "/media/fat/games/MegaDrive", Extensions: extensionSet(".md", ".gen", ".bin"),
		FileDelay: 1, FileType: "f", FileIndex: 1,
	},
	{
		System: protocol.SystemSNES, ExpectedCore: "SNES", RBFSelector: "_Console/SNES",
		ROMRoot: "/media/fat/games/SNES", MGLRoot: "/media/fat/games/SNES", Extensions: extensionSet(".sfc", ".smc", ".bin"),
		FileDelay: 2, FileType: "f", FileIndex: 0,
	},
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
	return NewRegistry(defaults...)
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
