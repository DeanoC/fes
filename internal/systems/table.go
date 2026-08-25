// Package systems owns FogCast's declarative system table.
package systems

import (
	"path"
	"strings"

	"github.com/DeanoC/FogCast-POC/protocol"
)

const DefaultSMBShareRoot = "//deano-clawz/Games/Games"

type Capability string

const (
	CapabilityCatalog    Capability = "catalog"
	CapabilityFPGANative Capability = "fpga_native"
	CapabilityHostOnly   Capability = "host_only"
)

const CoverProviderIGDB = "igdb"

type CoreSpec struct {
	ExpectedCore string
	RBF          string
	KitROMRoot   string
	MGLRoot      string
	FileDelay    int
	FileType     string
	FileIndex    int
}

type CoverSpec struct {
	Slug string
	Name string
}

type Row struct {
	FolderAlias  string
	PlatformID   protocol.System
	LaunchSystem protocol.System
	Label        string
	Extensions   []string
	Capability   Capability
	Core         *CoreSpec
	CoverSlugs   map[string]CoverSpec
}

var table = []Row{
	{
		FolderAlias: "Genesis", PlatformID: protocol.SystemMegaDrive, LaunchSystem: protocol.SystemMegaDrive, Label: "Mega Drive",
		Extensions: []string{".md", ".gen", ".bin"}, Capability: CapabilityFPGANative,
		Core:       &CoreSpec{ExpectedCore: "MegaDrive", RBF: "_Console/MegaDrive", KitROMRoot: "/media/fat/games/MegaDrive", MGLRoot: "/media/fat/games/MegaDrive", FileDelay: 1, FileType: "f", FileIndex: 1},
		CoverSlugs: map[string]CoverSpec{CoverProviderIGDB: {Slug: "genesis-slash-megadrive", Name: "Sega Mega Drive/Genesis"}},
	},
	{
		FolderAlias: "SNES", PlatformID: protocol.SystemSNES, LaunchSystem: protocol.SystemSNES, Label: "SNES",
		Extensions: []string{".sfc", ".smc", ".bin"}, Capability: CapabilityFPGANative,
		Core:       &CoreSpec{ExpectedCore: "SNES", RBF: "_Console/SNES", KitROMRoot: "/media/fat/games/SNES", MGLRoot: "/media/fat/games/SNES", FileDelay: 2, FileType: "f", FileIndex: 0},
		CoverSlugs: map[string]CoverSpec{CoverProviderIGDB: {Slug: "snes", Name: "Super Nintendo Entertainment System"}},
	},
	{
		FolderAlias: "NES", PlatformID: protocol.SystemNES, LaunchSystem: protocol.SystemNES, Label: "NES",
		Extensions: []string{".nes", ".unf", ".unif", ".fds"}, Capability: CapabilityFPGANative,
		Core:       &CoreSpec{ExpectedCore: "NES", RBF: "_Console/NES", KitROMRoot: "/media/fat/games/NES", MGLRoot: "/media/fat/games/NES", FileDelay: 1, FileType: "f", FileIndex: 0},
		CoverSlugs: map[string]CoverSpec{CoverProviderIGDB: {Slug: "nes", Name: "Nintendo Entertainment System"}},
	},
	{
		FolderAlias: "gb", PlatformID: protocol.SystemGameBoy, LaunchSystem: protocol.SystemGameBoy, Label: "Game Boy",
		Extensions: []string{".gb"}, Capability: CapabilityFPGANative,
		Core:       &CoreSpec{ExpectedCore: "GAMEBOY", RBF: "_Console/Gameboy", KitROMRoot: "/media/fat/games/Gameboy", MGLRoot: "/media/fat/games/Gameboy", FileDelay: 2, FileType: "f", FileIndex: 1},
		CoverSlugs: map[string]CoverSpec{CoverProviderIGDB: {Slug: "gb", Name: "Game Boy"}},
	},
	{PlatformID: "gbc", Label: "Game Boy Color", Extensions: []string{".gbc"}, Capability: CapabilityCatalog},
	{
		FolderAlias: "gba", PlatformID: protocol.SystemGBA, LaunchSystem: protocol.SystemGBA, Label: "Game Boy Advance",
		Extensions: []string{".gba"}, Capability: CapabilityFPGANative,
		Core:       &CoreSpec{ExpectedCore: "GBA", RBF: "_Console/GBA", KitROMRoot: "/media/fat/games/GBA", MGLRoot: "/media/fat/games/GBA", FileDelay: 2, FileType: "f", FileIndex: 0},
		CoverSlugs: map[string]CoverSpec{CoverProviderIGDB: {Slug: "gba", Name: "Game Boy Advance"}},
	},
	{PlatformID: "n64", Label: "Nintendo 64", Extensions: []string{".n64", ".z64", ".v64"}, Capability: CapabilityCatalog},
	{PlatformID: "psx", Label: "PlayStation", Extensions: []string{".cue", ".chd", ".pbp", ".iso", ".img"}, Capability: CapabilityCatalog},
	{
		FolderAlias: "SMS", PlatformID: protocol.SystemSMS, LaunchSystem: protocol.SystemSMS, Label: "Master System",
		Extensions: []string{".sms"}, Capability: CapabilityFPGANative,
		Core:       &CoreSpec{ExpectedCore: "SMS", RBF: "_Console/SMS", KitROMRoot: "/media/fat/games/SMS", MGLRoot: "/media/fat/games/SMS", FileDelay: 1, FileType: "f", FileIndex: 1},
		CoverSlugs: map[string]CoverSpec{CoverProviderIGDB: {Slug: "sms", Name: "Sega Master System/Mark III"}},
	},
	{
		FolderAlias: "gg", PlatformID: protocol.SystemGameGear, LaunchSystem: protocol.SystemGameGear, Label: "Game Gear",
		Extensions: []string{".gg"}, Capability: CapabilityFPGANative,
		Core:       &CoreSpec{ExpectedCore: "SMS", RBF: "_Console/SMS", KitROMRoot: "/media/fat/games/SMS", MGLRoot: "/media/fat/games/SMS", FileDelay: 1, FileType: "f", FileIndex: 2},
		CoverSlugs: map[string]CoverSpec{CoverProviderIGDB: {Slug: "game-gear", Name: "Sega Game Gear"}},
	},
	{
		FolderAlias: "pce", PlatformID: protocol.SystemPCE, LaunchSystem: protocol.SystemPCE, Label: "PC Engine",
		Extensions: []string{".pce"}, Capability: CapabilityFPGANative,
		Core:       &CoreSpec{ExpectedCore: "TGFX16", RBF: "_Console/TurboGrafx16", KitROMRoot: "/media/fat/games/TGFX16", MGLRoot: "/media/fat/games/TGFX16", FileDelay: 1, FileType: "f", FileIndex: 0},
		CoverSlugs: map[string]CoverSpec{CoverProviderIGDB: {Slug: "turbografx16--1", Name: "TurboGrafx-16/PC Engine"}},
	},
	{PlatformID: "32x", Label: "32X", Extensions: []string{".32x"}, Capability: CapabilityCatalog},
	{PlatformID: "saturn", Label: "Saturn", Extensions: []string{".cue", ".chd", ".iso"}, Capability: CapabilityCatalog},
	{PlatformID: "dc", Label: "Dreamcast", Extensions: []string{".gdi", ".cdi", ".chd"}, Capability: CapabilityCatalog},
	{PlatformID: "psp", Label: "PSP", Extensions: []string{".iso", ".cso", ".pbp"}, Capability: CapabilityCatalog},
	{PlatformID: "nds", Label: "Nintendo DS", Extensions: []string{".nds"}, Capability: CapabilityCatalog},
	{PlatformID: "arcade", Label: "Arcade", Extensions: []string{".zip"}, Capability: CapabilityCatalog},
	{PlatformID: "a2600", Label: "Atari 2600", Extensions: []string{".a26", ".bin"}, Capability: CapabilityCatalog},
	{PlatformID: "lynx", Label: "Lynx", Extensions: []string{".lnx"}, Capability: CapabilityCatalog},
	{PlatformID: "ngp", Label: "Neo Geo Pocket", Extensions: []string{".ngp", ".ngc"}, Capability: CapabilityCatalog},
	{PlatformID: "ws", Label: "WonderSwan", Extensions: []string{".ws", ".wsc"}, Capability: CapabilityCatalog},
}

func Rows() []Row {
	rows := make([]Row, len(table))
	for index, row := range table {
		rows[index] = cloneRow(row)
	}
	return rows
}

func Lookup(id protocol.System) (Row, bool) {
	for _, row := range table {
		if row.PlatformID == id {
			return cloneRow(row), true
		}
	}
	return Row{}, false
}

func Mapped(id protocol.System) bool {
	row, ok := Lookup(id)
	return ok && row.FolderAlias != ""
}

func SMBFolder(root string, id protocol.System) (string, bool) {
	row, ok := Lookup(id)
	if !ok || row.FolderAlias == "" {
		return "", false
	}
	return strings.TrimSuffix(root, "/") + "/" + path.Clean(row.FolderAlias), true
}

func cloneRow(row Row) Row {
	copy := row
	copy.Extensions = append([]string(nil), row.Extensions...)
	if row.Core != nil {
		core := *row.Core
		copy.Core = &core
	}
	copy.CoverSlugs = make(map[string]CoverSpec, len(row.CoverSlugs))
	for provider, cover := range row.CoverSlugs {
		copy.CoverSlugs[provider] = cover
	}
	return copy
}
