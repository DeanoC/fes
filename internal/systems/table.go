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
	FolderAlias string
	PlatformID  protocol.System
	Label       string
	Extensions  []string
	Capability  Capability
	Core        *CoreSpec
	CoverSlugs  map[string]CoverSpec
}

var table = []Row{
	{
		FolderAlias: "Genesis", PlatformID: protocol.SystemMegaDrive, Label: "Mega Drive",
		Extensions: []string{".md", ".gen", ".bin"}, Capability: CapabilityFPGANative,
		Core:       &CoreSpec{ExpectedCore: "MegaDrive", RBF: "_Console/MegaDrive", KitROMRoot: "/media/fat/games/MegaDrive", MGLRoot: "/media/fat/games/MegaDrive", FileDelay: 1, FileType: "f", FileIndex: 1},
		CoverSlugs: map[string]CoverSpec{CoverProviderIGDB: {Slug: "genesis-slash-megadrive", Name: "Sega Mega Drive/Genesis"}},
	},
	{
		FolderAlias: "SNES", PlatformID: protocol.SystemSNES, Label: "SNES",
		Extensions: []string{".sfc", ".smc", ".bin"}, Capability: CapabilityFPGANative,
		Core:       &CoreSpec{ExpectedCore: "SNES", RBF: "_Console/SNES", KitROMRoot: "/media/fat/games/SNES", MGLRoot: "/media/fat/games/SNES", FileDelay: 2, FileType: "f", FileIndex: 0},
		CoverSlugs: map[string]CoverSpec{CoverProviderIGDB: {Slug: "snes", Name: "Super Nintendo Entertainment System"}},
	},
	{PlatformID: "nes", Label: "NES", Extensions: []string{".nes", ".unf", ".unif", ".fds"}, Capability: CapabilityCatalog},
	{PlatformID: "gb", Label: "Game Boy", Extensions: []string{".gb"}, Capability: CapabilityCatalog},
	{PlatformID: "gbc", Label: "Game Boy Color", Extensions: []string{".gbc"}, Capability: CapabilityCatalog},
	{PlatformID: "gba", Label: "Game Boy Advance", Extensions: []string{".gba"}, Capability: CapabilityCatalog},
	{PlatformID: "n64", Label: "Nintendo 64", Extensions: []string{".n64", ".z64", ".v64"}, Capability: CapabilityCatalog},
	{PlatformID: "psx", Label: "PlayStation", Extensions: []string{".cue", ".chd", ".pbp", ".iso", ".img"}, Capability: CapabilityCatalog},
	{PlatformID: "sms", Label: "Master System", Extensions: []string{".sms"}, Capability: CapabilityCatalog},
	{PlatformID: "gg", Label: "Game Gear", Extensions: []string{".gg"}, Capability: CapabilityCatalog},
	{PlatformID: "pce", Label: "PC Engine", Extensions: []string{".pce", ".sgx"}, Capability: CapabilityCatalog},
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
