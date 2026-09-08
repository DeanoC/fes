// Package gfx is the tenfoot 2D bitmap device.
//
// UI draw helpers talk only to Device. Production sofa runs use the SDL3
// backend (WrapSDLRenderer, build tag sdl3). Software is a pure-Go
// rasterizer for tests and CI. FPGA records the versioned FC2D command
// stream and rasters through Software until a programmed 2D core exists
// (IsStub stays true; see fpga_protocol.md). FPGAStub is the older thin
// Software wrapper without a stream. LinuxFB rasters with Software and
// Present-blits onto a 32bpp Linux framebuffer. Recorder is a call-order
// test double and does not draw pixels.
//
// Window creation, events, gamepad, and text input stay in the SDL shell
// (host/tenfoot/sdl.go) until a later slice.
package gfx

import "image"

// Texture is an opaque GPU bitmap handle. The zero value is invalid.
type Texture struct {
	id uint64
	w  int
	h  int
}

// Valid reports whether t was created by a Device and has not been zeroed.
func (t Texture) Valid() bool { return t.id != 0 }

// Width returns the texture width in pixels.
func (t Texture) Width() int { return t.w }

// Height returns the texture height in pixels.
func (t Texture) Height() int { return t.h }

// Rect is a destination or source rectangle in logical pixels.
type Rect struct {
	X, Y, W, H float32
}

// Color is an 8-bit RGBA value. A is 255 for opaque fills.
type Color struct {
	R, G, B, A uint8
}

// RGB returns an opaque color.
func RGB(r, g, b uint8) Color { return Color{R: r, G: g, B: b, A: 255} }

// RGBA returns a color with explicit alpha.
func RGBA(r, g, b, a uint8) Color { return Color{R: r, G: g, B: b, A: a} }

// BlendMode selects how FillRect composites.
type BlendMode int

const (
	// BlendNone replaces destination pixels.
	BlendNone BlendMode = iota
	// BlendAlpha uses source alpha (SDL_BLENDMODE_BLEND on the SDL path).
	BlendAlpha
)

// Device is a high-performance 2D bitmap device.
//
// Lifecycle is BeginFrame, Clear, draws, Present. Textures are created from
// RGBA8 CPU bitmaps and destroyed individually so GPU park can drop covers
// while keeping a preview handle. Backends must not assume a 3D pipeline.
type Device interface {
	BeginFrame()
	Clear(c Color)
	Present()

	CreateRGBA(img *image.RGBA) (Texture, error)
	UpdateRGBA(tex Texture, img *image.RGBA) error
	Destroy(tex Texture)

	FillRect(r Rect, c Color)
	Draw(tex Texture, src *Rect, dst Rect)
	SetBlend(mode BlendMode)

	DebugText(x, y int, text string, scale int)

	Close()
}
