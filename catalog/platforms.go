package catalog

import (
	"fmt"
	"strings"

	"github.com/DeanoC/FogCast-POC/protocol"
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
	return NewPlatformRegistry(defaultPlatforms...)
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

var defaultPlatforms = []Platform{
	{ID: protocol.SystemMegaDrive, Label: "Mega Drive", Extensions: platformExtensions(".md", ".gen", ".bin"), LaunchSystem: protocol.SystemMegaDrive},
	{ID: protocol.SystemSNES, Label: "SNES", Extensions: platformExtensions(".sfc", ".smc", ".bin"), LaunchSystem: protocol.SystemSNES},
	{ID: "nes", Label: "NES", Extensions: platformExtensions(".nes", ".unf", ".unif", ".fds")},
	{ID: "gb", Label: "Game Boy", Extensions: platformExtensions(".gb")},
	{ID: "gbc", Label: "Game Boy Color", Extensions: platformExtensions(".gbc")},
	{ID: "gba", Label: "Game Boy Advance", Extensions: platformExtensions(".gba")},
	{ID: "n64", Label: "Nintendo 64", Extensions: platformExtensions(".n64", ".z64", ".v64")},
	{ID: "psx", Label: "PlayStation", Extensions: platformExtensions(".cue", ".chd", ".pbp", ".iso", ".img")},
	{ID: "sms", Label: "Master System", Extensions: platformExtensions(".sms")},
	{ID: "gg", Label: "Game Gear", Extensions: platformExtensions(".gg")},
	{ID: "pce", Label: "PC Engine", Extensions: platformExtensions(".pce", ".sgx")},
	{ID: "32x", Label: "32X", Extensions: platformExtensions(".32x")},
	{ID: "saturn", Label: "Saturn", Extensions: platformExtensions(".cue", ".chd", ".iso")},
	{ID: "dc", Label: "Dreamcast", Extensions: platformExtensions(".gdi", ".cdi", ".chd")},
	{ID: "psp", Label: "PSP", Extensions: platformExtensions(".iso", ".cso", ".pbp")},
	{ID: "nds", Label: "Nintendo DS", Extensions: platformExtensions(".nds")},
	{ID: "arcade", Label: "Arcade", Extensions: platformExtensions(".zip")},
	{ID: "a2600", Label: "Atari 2600", Extensions: platformExtensions(".a26", ".bin")},
	{ID: "lynx", Label: "Lynx", Extensions: platformExtensions(".lnx")},
	{ID: "ngp", Label: "Neo Geo Pocket", Extensions: platformExtensions(".ngp", ".ngc")},
	{ID: "ws", Label: "WonderSwan", Extensions: platformExtensions(".ws", ".wsc")},
}
