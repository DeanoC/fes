package coremedia

import (
	_ "embed"
	"encoding/hex"
	"strings"
)

//go:embed assets/coleco-graphics-i.hex
var colecoGraphicsIHex string

var defaultMediaHex = map[string]string{
	"fes.coleco": colecoGraphicsIHex,
}

// Lookup returns a defensive copy of the registered default media for coreID.
func Lookup(coreID string) ([]byte, bool) {
	encoded, ok := defaultMediaHex[coreID]
	if !ok {
		return nil, false
	}
	data, err := hex.DecodeString(strings.Join(strings.Fields(encoded), ""))
	if err != nil {
		panic("invalid embedded default core media: " + err.Error())
	}
	return append([]byte(nil), data...), true
}
