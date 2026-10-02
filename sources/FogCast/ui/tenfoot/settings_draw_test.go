package tenfoot

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/DeanoC/FogCast/ui/gfx"
)

func TestSettingsBackdropOcclusionMatchesFullDimming(t *testing.T) {
	rng := rand.New(rand.NewSource(318))
	for _, size := range [][2]int{{1, 1}, {24, 17}, {257, 189}, {1280, 720}} {
		g := Grid{Width: size[0], Height: size[1], Safe: insetsFromPct(size[0], size[1], 0.05)}
		plain, _ := gfx.NewSoftware(g.Width, g.Height)
		optimized, _ := gfx.NewSoftware(g.Width, g.Height)
		defer plain.Close()
		defer optimized.Close()
		for i := 0; i < 30; i++ {
			// Include panels entirely outside, crossing each edge, and covering
			// the whole content. Vary all channels, including destination alpha.
			panel := rectI{rng.Intn(g.Width*3) - g.Width, rng.Intn(g.Height*3) - g.Height,
				rng.Intn(g.Width*2) + 1, rng.Intn(g.Height*2) + 1}
			rng.Read(plain.Framebuffer().Pix)
			copy(optimized.Framebuffer().Pix, plain.Framebuffer().Pix)
			plain.SetBlend(gfx.BlendAlpha)
			fillRect(plain, float32(g.contentLeft()), float32(g.contentTop()),
				float32(g.contentWidth()), float32(g.contentHeight()), 8, 8, 12, 180)
			plain.SetBlend(gfx.BlendNone)
			dimOutsidePanel(optimized, g, panel)
			for _, d := range []gfx.Device{plain, optimized} {
				fillRect(d, float32(panel.X), float32(panel.Y), float32(panel.W), float32(panel.H), 255, 184, 48, 255)
			}
			if !bytes.Equal(plain.Framebuffer().Pix, optimized.Framebuffer().Pix) {
				t.Fatalf("size=%v panel=%v differs", size, panel)
			}
		}
	}
}
