//go:build sdl3

package gfx

/*
#cgo pkg-config: sdl3
#include <SDL3/SDL.h>
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"image"
	"unsafe"
)

// SDLBackend is the SDL3 renderer Device. It wraps an existing SDL_Renderer
// owned by the window shell; Close destroys leftover textures, not the renderer.
type SDLBackend struct {
	r        *C.SDL_Renderer
	next     uint64
	textures map[uint64]*C.SDL_Texture
}

// WrapSDLRenderer attaches a Device to an SDL_Renderer pointer created by the
// window shell. It sets letterbox logical presentation and enables VSync.
func WrapSDLRenderer(renderer unsafe.Pointer, logicalW, logicalH int) (*SDLBackend, error) {
	if renderer == nil {
		return nil, fmt.Errorf("nil SDL renderer")
	}
	r := (*C.SDL_Renderer)(renderer)
	if logicalW < 1 {
		logicalW = 1
	}
	if logicalH < 1 {
		logicalH = 1
	}
	C.SDL_SetRenderLogicalPresentation(r, C.int(logicalW), C.int(logicalH), C.SDL_LOGICAL_PRESENTATION_LETTERBOX)
	C.SDL_SetRenderVSync(r, 1)
	return &SDLBackend{r: r, textures: map[uint64]*C.SDL_Texture{}}, nil
}

func (s *SDLBackend) BeginFrame() {}

func (s *SDLBackend) Clear(c Color) {
	C.SDL_SetRenderDrawColor(s.r, C.Uint8(c.R), C.Uint8(c.G), C.Uint8(c.B), C.Uint8(c.A))
	C.SDL_RenderClear(s.r)
}

func (s *SDLBackend) Present() {
	C.SDL_RenderPresent(s.r)
}

func (s *SDLBackend) CreateRGBA(img *image.RGBA) (Texture, error) {
	if img == nil {
		return Texture{}, fmt.Errorf("empty image")
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 1 || h < 1 || len(img.Pix) == 0 {
		return Texture{}, fmt.Errorf("empty image")
	}
	tex := C.SDL_CreateTexture(s.r, C.SDL_PIXELFORMAT_RGBA32, C.SDL_TEXTUREACCESS_STATIC, C.int(w), C.int(h))
	if tex == nil {
		return Texture{}, fmt.Errorf("%s", sdlError())
	}
	C.SDL_SetTextureScaleMode(tex, C.SDL_SCALEMODE_LINEAR)
	if !bool(C.SDL_UpdateTexture(tex, nil, unsafe.Pointer(&img.Pix[0]), C.int(img.Stride))) {
		C.SDL_DestroyTexture(tex)
		return Texture{}, fmt.Errorf("update texture: %s", sdlError())
	}
	C.SDL_SetTextureBlendMode(tex, C.SDL_BLENDMODE_BLEND)
	s.next++
	id := s.next
	s.textures[id] = tex
	return Texture{id: id, w: w, h: h}, nil
}

func (s *SDLBackend) UpdateRGBA(tex Texture, img *image.RGBA) error {
	sdlTex := s.lookup(tex)
	if sdlTex == nil {
		return fmt.Errorf("invalid texture")
	}
	if img == nil || len(img.Pix) == 0 {
		return fmt.Errorf("empty image")
	}
	b := img.Bounds()
	if b.Dx() != tex.w || b.Dy() != tex.h {
		return fmt.Errorf("size mismatch")
	}
	if !bool(C.SDL_UpdateTexture(sdlTex, nil, unsafe.Pointer(&img.Pix[0]), C.int(img.Stride))) {
		return fmt.Errorf("update texture: %s", sdlError())
	}
	return nil
}

func (s *SDLBackend) Destroy(tex Texture) {
	sdlTex := s.lookup(tex)
	if sdlTex == nil {
		return
	}
	C.SDL_DestroyTexture(sdlTex)
	delete(s.textures, tex.id)
}

func (s *SDLBackend) FillRect(rect Rect, c Color) {
	C.SDL_SetRenderDrawColor(s.r, C.Uint8(c.R), C.Uint8(c.G), C.Uint8(c.B), C.Uint8(c.A))
	r := C.SDL_FRect{x: C.float(rect.X), y: C.float(rect.Y), w: C.float(rect.W), h: C.float(rect.H)}
	C.SDL_RenderFillRect(s.r, &r)
}

func (s *SDLBackend) Draw(tex Texture, src *Rect, dst Rect) {
	sdlTex := s.lookup(tex)
	if sdlTex == nil {
		return
	}
	d := C.SDL_FRect{x: C.float(dst.X), y: C.float(dst.Y), w: C.float(dst.W), h: C.float(dst.H)}
	if src == nil {
		C.SDL_RenderTexture(s.r, sdlTex, nil, &d)
		return
	}
	srect := C.SDL_FRect{x: C.float(src.X), y: C.float(src.Y), w: C.float(src.W), h: C.float(src.H)}
	C.SDL_RenderTexture(s.r, sdlTex, &srect, &d)
}

func (s *SDLBackend) SetBlend(mode BlendMode) {
	if mode == BlendAlpha {
		C.SDL_SetRenderDrawBlendMode(s.r, C.SDL_BLENDMODE_BLEND)
		return
	}
	C.SDL_SetRenderDrawBlendMode(s.r, C.SDL_BLENDMODE_NONE)
}

func (s *SDLBackend) DebugText(x, y int, text string, scale int) {
	if text == "" {
		return
	}
	if scale < 1 {
		scale = 1
	}
	cstr := C.CString(text)
	defer C.free(unsafe.Pointer(cstr))
	C.SDL_SetRenderScale(s.r, C.float(scale), C.float(scale))
	C.SDL_SetRenderDrawColor(s.r, 236, 240, 248, 255)
	C.SDL_RenderDebugText(s.r, C.float(x)/C.float(scale), C.float(y)/C.float(scale), cstr)
	C.SDL_SetRenderScale(s.r, 1, 1)
}

func (s *SDLBackend) DrawText(x, y int, text string, sizePx int, c Color) {
	s.DrawTextWeight(x, y, text, sizePx, WeightRegular, c)
}

func (s *SDLBackend) DrawTextWeight(x, y int, text string, sizePx int, w Weight, c Color) {
	img := RasterizeTextWeight(text, sizePx, w, c, 0)
	if img == nil {
		return
	}
	tex, err := s.CreateRGBA(img)
	if err != nil {
		return
	}
	b := img.Bounds()
	s.Draw(tex, nil, Rect{X: float32(x), Y: float32(y), W: float32(b.Dx()), H: float32(b.Dy())})
	s.Destroy(tex)
}

func (s *SDLBackend) Close() {
	for id, tex := range s.textures {
		C.SDL_DestroyTexture(tex)
		delete(s.textures, id)
	}
}

func (s *SDLBackend) lookup(tex Texture) *C.SDL_Texture {
	if s == nil || tex.id == 0 {
		return nil
	}
	return s.textures[tex.id]
}

func sdlError() string {
	msg := C.GoString(C.SDL_GetError())
	if msg == "" {
		return "unknown SDL error"
	}
	return msg
}

var _ Device = (*SDLBackend)(nil)
