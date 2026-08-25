package protocol_test

import (
	"testing"

	"github.com/DeanoC/FogCast-POC/protocol"
)

func TestValidateGameID(t *testing.T) {
	t.Parallel()
	tests := map[string]bool{
		"megadrive-test": true,
		"snes-test":      true,
		"MegaDrive":      false,
		"two words":      false,
		"../escape":      false,
		"":               false,
	}
	for input, wantValid := range tests {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			err := protocol.ValidateGameID(input)
			if (err == nil) != wantValid {
				t.Fatalf("ValidateGameID(%q) error = %v, want valid=%v", input, err, wantValid)
			}
		})
	}
}

func TestValidateSystem(t *testing.T) {
	t.Parallel()
	for _, system := range []protocol.System{
		protocol.SystemMegaDrive, protocol.SystemSNES, protocol.SystemNES, protocol.SystemSMS,
		protocol.SystemGameBoy, protocol.SystemGameBoyColor, protocol.SystemGBA, protocol.SystemPCE,
		protocol.SystemGameGear, protocol.SystemAtari2600, protocol.SystemAtari7800, protocol.SystemColecoVision,
		protocol.SystemAtariLynx, protocol.SystemWonderSwan, protocol.SystemWonderSwanColor, protocol.SystemIntellivision,
	} {
		if err := protocol.ValidateSystem(system); err != nil {
			t.Fatalf("ValidateSystem(%q): %v", system, err)
		}
	}
	for _, system := range []protocol.System{"mystery", "n64"} {
		if err := protocol.ValidateSystem(system); err == nil {
			t.Fatalf("ValidateSystem(%q) succeeded", system)
		}
	}
}

func TestValidateDigest(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		testDigest: true,
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcde":   false,
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0": false,
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdeg":  false,
		"0123456789ABCDEF0123456789abcdef0123456789abcdef0123456789abcdef":  false,
	}
	for digest, wantValid := range tests {
		t.Run(digest, func(t *testing.T) {
			t.Parallel()
			if err := protocol.ValidateDigest(digest); (err == nil) != wantValid {
				t.Fatalf("ValidateDigest(%q) error = %v, want valid=%v", digest, err, wantValid)
			}
		})
	}
}

func TestValidateExtension(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"sfc":  true,
		"md32": true,
		"SFC":  false,
		".sfc": false,
		"s/fc": false,
		"s fc": false,
		"":     false,
	}
	for extension, wantValid := range tests {
		t.Run(extension, func(t *testing.T) {
			t.Parallel()
			if err := protocol.ValidateExtension(extension); (err == nil) != wantValid {
				t.Fatalf("ValidateExtension(%q) error = %v, want valid=%v", extension, err, wantValid)
			}
		})
	}
}

func TestValidateContentKey(t *testing.T) {
	t.Parallel()

	if err := protocol.ValidateContentKey(protocol.ContentKey{SHA256: testDigest, Extension: "sfc"}); err != nil {
		t.Fatalf("ValidateContentKey(valid): %v", err)
	}
	if err := protocol.ValidateContentKey(protocol.ContentKey{SHA256: testDigest, Extension: ".sfc"}); err == nil {
		t.Fatal("ValidateContentKey(invalid extension) succeeded")
	}
}

func TestValidateContentIdentitySizeBoundaries(t *testing.T) {
	t.Parallel()

	tests := map[int64]bool{
		1:                            true,
		protocol.MaxContentBytes:     true,
		0:                            false,
		-1:                           false,
		protocol.MaxContentBytes + 1: false,
	}
	for size, wantValid := range tests {
		t.Run("size", func(t *testing.T) {
			if err := protocol.ValidateContentIdentity(protocol.ContentIdentity{SHA256: testDigest, Size: size, Extension: "sfc"}); (err == nil) != wantValid {
				t.Fatalf("ValidateContentIdentity(size=%d) error = %v, want valid=%v", size, err, wantValid)
			}
		})
	}
}

func TestCompleteCachedLaunchRequestRejectsUnknownSystem(t *testing.T) {
	t.Parallel()

	request := protocol.CachedLaunchRequest{
		GameID:  "snes-test",
		System:  "mystery",
		Content: protocol.ContentIdentity{SHA256: testDigest, Size: 1, Extension: "sfc"},
	}
	if err := protocol.ValidateGameID(request.GameID); err != nil {
		t.Fatalf("ValidateGameID(%q): %v", request.GameID, err)
	}
	if err := protocol.ValidateContentIdentity(request.Content); err != nil {
		t.Fatalf("ValidateContentIdentity(%#v): %v", request.Content, err)
	}
	if err := protocol.ValidateSystem(request.System); err == nil {
		t.Fatal("ValidateSystem(unknown cached launch system) succeeded")
	}
}
