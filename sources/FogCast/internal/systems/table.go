// Package systems owns FogCast's declarative system table.
package systems

import (
	"path"
	"strings"

	"github.com/DeanoC/FogCast/protocol"
)

const DefaultSMBShareRoot = "//deano-clawz/Games/Games"

type Capability string

const (
	CapabilityCatalog  Capability = "catalog"
	CapabilityHostOnly Capability = "host_only"
)

const CoverProviderIGDB = "igdb"

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
	CoverSlugs   map[string]CoverSpec
	// Tags classify the hardware ("cpu:z80", "vdp:tms9918-family") so
	// curated views can group systems across vendors.
	Tags []string
}

var table = []Row{
	{
		FolderAlias: "Genesis", PlatformID: protocol.SystemMegaDrive, LaunchSystem: protocol.SystemMegaDrive, Label: "Mega Drive",
		Tags:       []string{"cpu:m68k", "cpu:z80", "vdp:sega-315-5313", "vdp:tms9918-family", "audio:ym2612", "audio:sn76489"},
		Extensions: []string{".md", ".gen", ".bin"}, Capability: CapabilityCatalog,
		CoverSlugs: map[string]CoverSpec{CoverProviderIGDB: {Slug: "genesis-slash-megadrive", Name: "Sega Mega Drive/Genesis"}},
	},
	{
		FolderAlias: "SNES", PlatformID: protocol.SystemSNES, LaunchSystem: protocol.SystemSNES, Label: "SNES",
		Tags:       []string{"cpu:65c816", "ppu:s-ppu", "audio:spc700"},
		Extensions: []string{".sfc", ".smc", ".bin"}, Capability: CapabilityCatalog,
		CoverSlugs: map[string]CoverSpec{CoverProviderIGDB: {Slug: "snes", Name: "Super Nintendo Entertainment System"}},
	},
	{
		FolderAlias: "NES", PlatformID: protocol.SystemNES, LaunchSystem: protocol.SystemNES, Label: "NES",
		Tags:       []string{"cpu:6502", "ppu:rp2c02"},
		Extensions: []string{".nes"}, Capability: CapabilityCatalog,
		CoverSlugs: map[string]CoverSpec{CoverProviderIGDB: {Slug: "nes", Name: "Nintendo Entertainment System"}},
	},
	{
		FolderAlias: "gb", PlatformID: protocol.SystemGameBoy, LaunchSystem: protocol.SystemGameBoy, Label: "Game Boy",
		Tags:       []string{"cpu:sm83", "handheld"},
		Extensions: []string{".gb"}, Capability: CapabilityCatalog,
		CoverSlugs: map[string]CoverSpec{CoverProviderIGDB: {Slug: "gb", Name: "Game Boy"}},
	},
	{
		FolderAlias: "Game Boy Color", PlatformID: protocol.SystemGameBoyColor, LaunchSystem: protocol.SystemGameBoyColor, Label: "Game Boy Color",
		Tags:       []string{"cpu:sm83", "handheld"},
		Extensions: []string{".gbc"}, Capability: CapabilityCatalog,
		CoverSlugs: map[string]CoverSpec{CoverProviderIGDB: {Slug: "gbc", Name: "Game Boy Color"}},
	},
	{
		FolderAlias: "gba", PlatformID: protocol.SystemGBA, LaunchSystem: protocol.SystemGBA, Label: "Game Boy Advance",
		Tags:       []string{"cpu:arm7tdmi", "handheld"},
		Extensions: []string{".gba"}, Capability: CapabilityCatalog,
		CoverSlugs: map[string]CoverSpec{CoverProviderIGDB: {Slug: "gba", Name: "Game Boy Advance"}},
	},
	{
		PlatformID: protocol.SystemPong, LaunchSystem: protocol.SystemPong, Label: "Pong",
		Tags: []string{"discrete-logic"}, Capability: CapabilityCatalog,
	},
	{PlatformID: "n64", Label: "Nintendo 64", Extensions: []string{".n64", ".z64", ".v64"}, Capability: CapabilityCatalog},
	{PlatformID: "psx", Label: "PlayStation", Extensions: []string{".cue", ".chd", ".pbp", ".iso", ".img"}, Capability: CapabilityCatalog},
	{
		FolderAlias: "SMS", PlatformID: protocol.SystemSMS, LaunchSystem: protocol.SystemSMS, Label: "Master System",
		Tags:       []string{"cpu:z80", "vdp:sega-315-5124", "vdp:tms9918-family", "audio:sn76489"},
		Extensions: []string{".sms"}, Capability: CapabilityCatalog,
		CoverSlugs: map[string]CoverSpec{CoverProviderIGDB: {Slug: "sms", Name: "Sega Master System/Mark III"}},
	},
	{
		FolderAlias: "gg", PlatformID: protocol.SystemGameGear, LaunchSystem: protocol.SystemGameGear, Label: "Game Gear",
		Tags:       []string{"cpu:z80", "vdp:sega-315-5124", "vdp:tms9918-family", "audio:sn76489", "handheld"},
		Extensions: []string{".gg"}, Capability: CapabilityCatalog,
		CoverSlugs: map[string]CoverSpec{CoverProviderIGDB: {Slug: "game-gear", Name: "Sega Game Gear"}},
	},
	{
		FolderAlias: "pce", PlatformID: protocol.SystemPCE, LaunchSystem: protocol.SystemPCE, Label: "PC Engine",
		Tags:       []string{"cpu:huc6280", "vdp:huc6270"},
		Extensions: []string{".pce"}, Capability: CapabilityCatalog,
		CoverSlugs: map[string]CoverSpec{CoverProviderIGDB: {Slug: "turbografx16--1", Name: "TurboGrafx-16/PC Engine"}},
	},
	{PlatformID: "32x", Label: "32X", Extensions: []string{".32x"}, Capability: CapabilityCatalog},
	{PlatformID: "saturn", Label: "Saturn", Extensions: []string{".cue", ".chd", ".iso"}, Capability: CapabilityCatalog},
	{PlatformID: "dc", Label: "Dreamcast", Extensions: []string{".gdi", ".cdi", ".chd"}, Capability: CapabilityCatalog},
	{PlatformID: "psp", Label: "PSP", Extensions: []string{".iso", ".cso", ".pbp"}, Capability: CapabilityCatalog},
	{PlatformID: "nds", Label: "Nintendo DS", Extensions: []string{".nds"}, Capability: CapabilityCatalog},
	{PlatformID: "arcade", Label: "Arcade", Extensions: []string{".zip"}, Capability: CapabilityCatalog},
	{
		FolderAlias: "Atari2600", PlatformID: protocol.SystemAtari2600, LaunchSystem: protocol.SystemAtari2600, Label: "Atari 2600",
		Tags:       []string{"cpu:6507", "video:tia"},
		Extensions: []string{".a26", ".bin"}, Capability: CapabilityCatalog,
		CoverSlugs: map[string]CoverSpec{CoverProviderIGDB: {Slug: "atari2600", Name: "Atari 2600"}},
	},
	{
		FolderAlias: "ColecoVision", PlatformID: protocol.SystemColecoVision, LaunchSystem: protocol.SystemColecoVision, Label: "ColecoVision",
		Tags:       []string{"cpu:z80", "vdp:tms9918", "vdp:tms9918-family", "audio:sn76489"},
		Extensions: []string{".col", ".bin", ".rom"}, Capability: CapabilityCatalog,
		CoverSlugs: map[string]CoverSpec{CoverProviderIGDB: {Slug: "colecovision", Name: "ColecoVision"}},
	},
	{
		FolderAlias: "AtariLynx", PlatformID: protocol.SystemAtariLynx, LaunchSystem: protocol.SystemAtariLynx, Label: "Atari Lynx",
		Tags:       []string{"cpu:65c02", "handheld"},
		Extensions: []string{".lnx", ".lyx"}, Capability: CapabilityCatalog,
		CoverSlugs: map[string]CoverSpec{CoverProviderIGDB: {Slug: "lynx", Name: "Atari Lynx"}},
	},
	{PlatformID: "ngp", Label: "Neo Geo Pocket", Extensions: []string{".ngp", ".ngc"}, Capability: CapabilityCatalog},
	{
		FolderAlias: "WonderSwan", PlatformID: protocol.SystemWonderSwan, LaunchSystem: protocol.SystemWonderSwan, Label: "WonderSwan",
		Tags:       []string{"cpu:v30mz", "handheld"},
		Extensions: []string{".ws"}, Capability: CapabilityCatalog,
		CoverSlugs: map[string]CoverSpec{CoverProviderIGDB: {Slug: "wonderswan", Name: "WonderSwan"}},
	},
	{
		FolderAlias: "WonderSwan Color", PlatformID: protocol.SystemWonderSwanColor, LaunchSystem: protocol.SystemWonderSwanColor, Label: "WonderSwan Color",
		Tags:       []string{"cpu:v30mz", "handheld"},
		Extensions: []string{".wsc"}, Capability: CapabilityCatalog,
		CoverSlugs: map[string]CoverSpec{CoverProviderIGDB: {Slug: "wonderswan-color", Name: "WonderSwan Color"}},
	},
	{
		FolderAlias: "Atari7800", PlatformID: protocol.SystemAtari7800, LaunchSystem: protocol.SystemAtari7800, Label: "Atari 7800",
		Tags:       []string{"cpu:6502", "video:maria"},
		Extensions: []string{".a78", ".bin"}, Capability: CapabilityCatalog,
		CoverSlugs: map[string]CoverSpec{CoverProviderIGDB: {Slug: "atari7800", Name: "Atari 7800"}},
	},
	{
		FolderAlias: "Intellivision", PlatformID: protocol.SystemIntellivision, LaunchSystem: protocol.SystemIntellivision, Label: "Intellivision",
		Tags:       []string{"cpu:cp1610", "video:stic"},
		Extensions: []string{".rom", ".int", ".bin"}, Capability: CapabilityCatalog,
		CoverSlugs: map[string]CoverSpec{CoverProviderIGDB: {Slug: "intellivision", Name: "Intellivision"}},
	},
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
	copy.CoverSlugs = make(map[string]CoverSpec, len(row.CoverSlugs))
	for provider, cover := range row.CoverSlugs {
		copy.CoverSlugs[provider] = cover
	}
	copy.Tags = append([]string(nil), row.Tags...)
	return copy
}
