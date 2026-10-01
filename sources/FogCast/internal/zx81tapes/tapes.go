// Package zx81tapes contains redistributable starter cassettes for the ZX81 room.
// The tapes are separate programs with their own licences; see README.md.
package zx81tapes

import _ "embed"

// Entry describes one immutable bundled .p cassette. Data is an owned copy.
// RAMKB is the recommended installed RAM, not a kit acceptance claim.
type Entry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Filename  string `json:"filename"`
	License   string `json:"license"`
	SourceURL string `json:"source_url"`
	Controls  string `json:"controls"`
	RAMKB     int    `json:"ram_kb"`
	SHA256    string `json:"sha256"`
	Data      []byte `json:"-"`
}

//go:embed assets/aritm.p
var aritm []byte

//go:embed assets/character-display.p
var characterDisplay []byte

//go:embed assets/guess-number.p
var guessNumber []byte

var entries = []Entry{
	{ID: "guess-number", Name: "Guess the Number", Filename: "guess-number.p", License: "MIT", SourceURL: "https://github.com/DeanoC/fes/tree/main/sources/FogCast/internal/zx81tapes/source", Controls: "After LOAD \"\", enter RUN. Guess a number from 1 to 20 and press NEW LINE. Follow HIGHER or LOWER; RUN plays again.", RAMKB: 1, SHA256: "f9c0e11b1b512fd32469c1efbc35974f03db33dd51f2d9d66159ff8daa5cc1e6", Data: guessNumber},
	{ID: "aritm", Name: "Aritm", Filename: "aritm.p", License: "GPL-3.0-or-later", SourceURL: "https://github.com/mobluse/aritmjs/blob/863e12a32830722acc041359a611cf364f97ea04/aritm-zx81.bas", Controls: "After LOAD \"\", enter RUN. Type answers and press NEW LINE; -1 leaves a problem set. Follow the on-screen menus.", RAMKB: 16, SHA256: "1314c4bf0e864d1bc23f7f43ae12ace6f9ec847133e1008ae50ef2872bab5921", Data: aritm},
	{ID: "character-display", Name: "Character Display", Filename: "character-display.p", License: "MIT", SourceURL: "https://github.com/maziac/zx81-sample-program/tree/c0298a7d61f7e221d25cf9eed16f05b1487a7ac5", Controls: "After LOAD \"\", enter RUN if needed. Hold S to start the character display demo.", RAMKB: 16, SHA256: "da2f696ae8fb202021afefa65bd901fbf67767cc521c422537f84672f2c0538e", Data: characterDisplay},
}

// Entries returns the starter shelf in display order, with independent data.
func Entries() []Entry {
	result := make([]Entry, len(entries))
	for i, entry := range entries {
		result[i] = clone(entry)
	}
	return result
}

// Lookup finds a starter cassette by its stable ID. Unknown IDs return false.
func Lookup(id string) (Entry, bool) {
	for _, entry := range entries {
		if entry.ID == id {
			return clone(entry), true
		}
	}
	return Entry{}, false
}

func clone(entry Entry) Entry {
	entry.Data = append([]byte(nil), entry.Data...)
	return entry
}
