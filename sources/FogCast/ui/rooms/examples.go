package rooms

import (
	"embed"
	"io/fs"
)

//go:embed examples
var examplesFS embed.FS

// ExamplePrefix is the stable namespace of the built-in room IDs.
const ExamplePrefix = "example."

// Examples returns Home, Systems, setup packs and the Coleco Arcade (example.coleco-arcade) browser.
func Examples() []Pack {
	sub, err := fs.Sub(examplesFS, "examples")
	if err != nil {
		return nil
	}
	return LoadEmbedded(sub, ExamplePrefix)
}
