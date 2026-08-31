package catalog

import (
	"fmt"
	"strings"

	"github.com/DeanoC/FogCast/internal/systems"
	"github.com/DeanoC/FogCast/protocol"
)

// Platform is a host-owned browse/scan identity. It is not a host/target
// protocol system unless LaunchSystem is set.
type Platform struct {
	ID           protocol.System
	Label        string
	Extensions   map[string]struct{}
	LaunchSystem protocol.System
}

type PlatformRegistry struct {
	byID map[protocol.System]Platform
}

func DefaultPlatforms() PlatformRegistry {
	rows := systems.Rows()
	platforms := make([]Platform, 0, len(rows))
	for _, row := range rows {
		platform := Platform{ID: row.PlatformID, Label: row.Label, Extensions: platformExtensions(row.Extensions...)}
		platform.LaunchSystem = row.LaunchSystem
		platforms = append(platforms, platform)
	}
	return NewPlatformRegistry(platforms...)
}

func NewPlatformRegistry(platforms ...Platform) PlatformRegistry {
	result := PlatformRegistry{byID: make(map[protocol.System]Platform, len(platforms))}
	for _, platform := range platforms {
		result.byID[platform.ID] = clonePlatform(platform)
	}
	return result
}

func (r PlatformRegistry) Lookup(id protocol.System) (Platform, bool) {
	platform, ok := r.byID[id]
	return clonePlatform(platform), ok
}

func (r PlatformRegistry) LookupID(id string) (Platform, bool) {
	return r.Lookup(protocol.System(id))
}

func ValidatePlatform(id protocol.System) error {
	if _, ok := DefaultPlatforms().Lookup(id); !ok {
		return fmt.Errorf("unsupported catalog platform %q", id)
	}
	return nil
}

// Launchable reports whether the platform maps to a public protocol launch
// system. Host-emulator launchability is a service policy on top of this.
func Launchable(id protocol.System) bool {
	platform, ok := DefaultPlatforms().Lookup(id)
	if !ok {
		return false
	}
	if platform.LaunchSystem == "" {
		return false
	}
	return protocol.ValidateSystem(platform.LaunchSystem) == nil
}

func PlatformLabel(id protocol.System) string {
	if platform, ok := DefaultPlatforms().Lookup(id); ok {
		return platform.Label
	}
	return strings.TrimSpace(string(id))
}

func clonePlatform(platform Platform) Platform {
	copy := platform
	copy.Extensions = make(map[string]struct{}, len(platform.Extensions))
	for extension := range platform.Extensions {
		copy.Extensions[extension] = struct{}{}
	}
	return copy
}

func platformExtensions(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
