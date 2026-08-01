package mister_test

import (
	"os"
	"testing"

	"github.com/clawzai2-tech/mister-remote/internal/core"
	"github.com/clawzai2-tech/mister-remote/internal/mister"
	"github.com/clawzai2-tech/mister-remote/protocol"
)

func TestRenderMGLGolden(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		relativePath string
		golden       string
		system       protocol.System
	}{
		{name: "megadrive", system: protocol.SystemMegaDrive, relativePath: "test.md", golden: "testdata/megadrive.mgl"},
		{name: "snes", system: protocol.SystemSNES, relativePath: "RPGs/test & demo.sfc", golden: "testdata/snes.mgl"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spec, _ := core.DefaultRegistry().Lookup(tt.system)
			got, err := mister.RenderMGL(spec, tt.relativePath)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(tt.golden)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want) {
				t.Fatalf("MGL mismatch\nwant:\n%s\ngot:\n%s", want, got)
			}
		})
	}
}
