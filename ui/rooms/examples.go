package rooms

import (
	"embed"
	"io/fs"
)

//go:embed examples
var examplesFS embed.FS

// ExamplePrefix namespaces the embedded sample rooms.
const ExamplePrefix = "example."

// Examples returns the sample rooms compiled into the launcher.
func Examples() []Pack {
	sub, err := fs.Sub(examplesFS, "examples")
	if err != nil {
		return nil
	}
	return LoadEmbedded(sub, ExamplePrefix)
}
