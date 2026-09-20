package catalog_test

import (
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

func TestNormalizeRelativePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "already normalized", input: "RPGs/Super Mario World.sfc", want: "RPGs/Super Mario World.sfc"},
		{name: "backslashes become slashes", input: `RPGs\Super Mario World.sfc`, want: "RPGs/Super Mario World.sfc"},
		{name: "clean nested path", input: "RPGs//./Super Mario World.sfc", want: "RPGs/Super Mario World.sfc"},
		{name: "empty", input: "", wantErr: true},
		{name: "absolute", input: "/Volumes/Games/test.sfc", wantErr: true},
		{name: "current directory", input: ".", wantErr: true},
		{name: "parent directory", input: "..", wantErr: true},
		{name: "escapes root", input: "RPGs/../../outside.sfc", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := catalog.NormalizeRelativePath(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NormalizeRelativePath(%q) error = %v, wantErr=%v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("NormalizeRelativePath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestGameIDUsesStableIdentityAndTitleSlug(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		system       protocol.System
		libraryID    string
		relativePath string
		title        string
		want         string
	}{
		{
			name:         "ordinary title",
			system:       protocol.SystemSNES,
			libraryID:    "snes-main",
			relativePath: "RPGs/Super Mario World.sfc",
			title:        "Super Mario World",
			want:         "snes-super-mario-world-638b9691f109",
		},
		{
			name:         "collapsed punctuation",
			system:       protocol.SystemMegaDrive,
			libraryID:    "genesis-main",
			relativePath: "Sonic & Knuckles.md",
			title:        "Sonic!!! & Knuckles",
			want:         "megadrive-sonic-knuckles-67d9940325cf",
		},
		{
			name:         "empty title falls back to game",
			system:       protocol.SystemSNES,
			libraryID:    "snes-main",
			relativePath: "RPGs/Super Mario World.sfc",
			title:        "",
			want:         "snes-game-638b9691f109",
		},
		{
			name:         "title is capped at 48 ascii characters",
			system:       protocol.SystemSNES,
			libraryID:    "snes-main",
			relativePath: "titles/very-long-title.sfc",
			title:        "ABCDEFGHIJKLMNOPQRSTUVWXYZ 0123456789 ABCDEFGHIJKLMNOPQRSTUVWXYZ",
			want:         "snes-abcdefghijklmnopqrstuvwxyz-0123456789-abcdefghij-4f65afc57be8",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := catalog.GameID(tt.system, tt.libraryID, tt.relativePath, tt.title)
			if got != tt.want {
				t.Fatalf("GameID() = %q, want %q", got, tt.want)
			}
			if again := catalog.GameID(tt.system, tt.libraryID, tt.relativePath, tt.title); again != tt.want {
				t.Fatalf("GameID() repeat = %q, want %q", again, tt.want)
			}
		})
	}
}

func TestGameIDChangesWhenPathOrLibraryChanges(t *testing.T) {
	t.Parallel()

	const title = "Super Mario World"
	if got := catalog.GameID(protocol.SystemSNES, "snes-main", "RPGs/Another Game.sfc", title); got != "snes-super-mario-world-f4b2a7977440" {
		t.Fatalf("GameID(path change) = %q", got)
	}
	if got := catalog.GameID(protocol.SystemSNES, "snes-alt", "RPGs/Super Mario World.sfc", title); got != "snes-super-mario-world-ccaf2f0c0837" {
		t.Fatalf("GameID(library change) = %q", got)
	}
}
