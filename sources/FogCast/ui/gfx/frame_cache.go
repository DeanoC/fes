package gfx

import (
	"image"
	"slices"
)

// FrameCache skips rasterization of identical complete scenes. It compares
// draw commands, not framebuffer bytes, so scripted rooms and asynchronous
// artwork use the same invalidation path as ordinary UI chrome. Input and
// simulation continue at their usual cadence. Only frames that clear their
// background before drawing can be reused; incremental drawing always runs.
// CacheBackdrop can also reuse a CPU framebuffer prefix while overlays change.
// Texture mutations flush pending commands before changing their resources.
// The caller owns the wrapped device's lifecycle and must draw through this
// wrapper exclusively while the cache is in use.
type FrameCache struct {
	device                                       Device
	current, previous                            []frameCommand
	blend                                        BlendMode
	active, direct, dirty, clear, painted, valid bool
	mutatedDuringFrame                           bool
	checkpoint                                   int
	backdropCommands                             []frameCommand
	backdropPixels                               []byte
	backdropBounds                               image.Rectangle
	backdropStride                               int
	backdropBlend                                BlendMode
	backdropValid                                bool
	backdropDamage                               image.Rectangle
	mutatedTextures                              []Texture
	revision                                     uint64
}

type frameCommand struct {
	op         Op
	tex        Texture
	src, dst   Rect
	hasSrc     bool
	color      Color
	blend      BlendMode
	text       string
	x, y, size int
	weight     Weight
}

// NewFrameCache wraps an exclusively owned painter. The caller closes device.
func NewFrameCache(device Device) *FrameCache { return &FrameCache{device: device} }

func (c *FrameCache) BeginFrame() {
	c.current = c.current[:0]
	c.checkpoint = -1
	c.mutatedTextures = c.mutatedTextures[:0]
	c.active, c.direct, c.clear, c.painted, c.mutatedDuringFrame = true, false, false, false, false
	c.record(frameCommand{op: OpSetBlend, blend: c.blend})
}

func (c *FrameCache) record(command frameCommand) {
	c.current = append(c.current, command)
	if c.direct {
		c.execute(command)
	}
}

func (c *FrameCache) Clear(color Color) {
	if !c.painted {
		c.clear = true
	}
	c.record(frameCommand{op: OpClear, color: color})
}
func (c *FrameCache) FillRect(rect Rect, color Color) {
	c.painted = true
	c.record(frameCommand{op: OpFillRect, dst: rect, color: color})
}
func (c *FrameCache) Draw(tex Texture, src *Rect, dst Rect) {
	command := frameCommand{op: OpDraw, tex: tex, dst: dst}
	if src != nil {
		command.src, command.hasSrc = *src, true
	}
	c.painted = true
	c.record(command)
}
func (c *FrameCache) SetBlend(blend BlendMode) {
	c.blend = blend
	if c.active {
		c.record(frameCommand{op: OpSetBlend, blend: blend})
	} else {
		c.device.SetBlend(blend)
	}
}
func (c *FrameCache) DebugText(x, y int, text string, scale int) {
	c.painted = true
	c.record(frameCommand{op: OpDebugText, x: x, y: y, text: text, size: scale})
}
func (c *FrameCache) DrawText(x, y int, text string, size int, color Color) {
	c.DrawTextWeight(x, y, text, size, WeightRegular, color)
}
func (c *FrameCache) DrawTextWeight(x, y int, text string, size int, weight Weight, color Color) {
	c.painted = true
	c.record(frameCommand{op: OpDrawText, x: x, y: y, text: text, size: size, weight: NormalizeWeight(weight), color: color})
}

// CacheBackdrop marks one reusable command prefix in a complete frame. CPU
// painters keep one framebuffer copy; unsupported painters replay normally.
// The checkpoint is deferred so identical whole frames still do no raster work.
func (c *FrameCache) CacheBackdrop() {
	if !c.active || !c.clear || c.checkpoint >= 0 {
		return
	}
	if _, ok := c.device.(interface{ frameSurface() *image.RGBA }); !ok {
		return
	}
	c.checkpoint = len(c.current)
	c.backdropBlend = c.blend
	if c.direct {
		c.captureBackdrop()
	}
}

func (c *FrameCache) captureBackdrop() {
	for _, tex := range c.mutatedTextures {
		if prefixUsesTexture(c.current[:c.checkpoint], tex) {
			c.backdropValid = false
			return
		}
	}
	fb := c.device.(interface{ frameSurface() *image.RGBA }).frameSurface()
	if cap(c.backdropPixels) < len(fb.Pix) {
		c.backdropPixels = make([]byte, len(fb.Pix))
	}
	c.backdropPixels = c.backdropPixels[:len(fb.Pix)]
	copy(c.backdropPixels, fb.Pix)
	c.backdropBounds, c.backdropStride = fb.Rect, fb.Stride
	c.backdropCommands = append(c.backdropCommands[:0], c.current[:c.checkpoint]...)
	c.backdropValid = true
}

// Only the preceding overlay can have altered a matching backdrop. Commands
// with uncertain pixel bounds conservatively damage the whole framebuffer.
func backdropDamage(commands []frameCommand, bounds image.Rectangle) image.Rectangle {
	var damage image.Rectangle
	for _, command := range commands {
		switch command.op {
		case OpSetBlend:
		case OpFillRect, OpDraw:
			x0, y0, x1, y1, ok := clipRect(command.dst, bounds.Dx(), bounds.Dy())
			if ok {
				damage = damage.Union(image.Rect(x0, y0, x1, y1).Add(bounds.Min))
			}
		default:
			return bounds
		}
	}
	return damage
}

func (c *FrameCache) restoreBackdrop(fb *image.RGBA) {
	r := c.backdropDamage.Intersect(fb.Rect)
	if r.Empty() {
		return
	}
	if r == fb.Rect {
		copy(fb.Pix, c.backdropPixels)
		return
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		start := fb.PixOffset(r.Min.X, y)
		end := start + r.Dx()*4
		copy(fb.Pix[start:end], c.backdropPixels[start:end])
	}
}

func (c *FrameCache) flush() {
	if !c.active || c.direct {
		return
	}
	c.device.BeginFrame()
	start := 0
	if c.checkpoint >= 0 {
		fb := c.device.(interface{ frameSurface() *image.RGBA }).frameSurface()
		if c.backdropValid && fb.Rect == c.backdropBounds && fb.Stride == c.backdropStride && len(fb.Pix) == len(c.backdropPixels) && slices.Equal(c.current[:c.checkpoint], c.backdropCommands) {
			c.restoreBackdrop(fb)
			c.device.SetBlend(c.backdropBlend)
		} else {
			for _, command := range c.current[:c.checkpoint] {
				c.execute(command)
			}
			c.captureBackdrop()
		}
		start = c.checkpoint
	}
	for _, command := range c.current[start:] {
		c.execute(command)
	}
	c.direct = true
}
func (c *FrameCache) execute(command frameCommand) {
	switch command.op {
	case OpClear:
		c.device.Clear(command.color)
	case OpSetBlend:
		c.device.SetBlend(command.blend)
	case OpFillRect:
		c.device.FillRect(command.dst, command.color)
	case OpDraw:
		var src *Rect
		if command.hasSrc {
			src = &command.src
		}
		c.device.Draw(command.tex, src, command.dst)
	case OpDebugText:
		c.device.DebugText(command.x, command.y, command.text, command.size)
	case OpDrawText:
		c.device.DrawTextWeight(command.x, command.y, command.text, command.size, command.weight, command.color)
	}
}

func (c *FrameCache) Present() {
	changed := c.direct || c.dirty || !c.valid || !c.clear || !slices.Equal(c.current, c.previous)
	if changed {
		c.flush()
		c.revision++
		c.previous = append(c.previous[:0], c.current...)
	}
	if c.checkpoint >= 0 {
		c.backdropDamage = backdropDamage(c.current[c.checkpoint:], c.backdropBounds)
	} else {
		// A frame drawn without this checkpoint may touch any backdrop pixel.
		c.backdropValid = false
	}
	// A mutation after an earlier draw can make the next identical command
	// list produce different pixels. Do not reuse that frame.
	c.active, c.valid, c.dirty = false, !c.mutatedDuringFrame, false
	if device, ok := c.device.(interface{ PresentRevision(uint64) }); ok {
		// Also services generation probes and retries when nothing was rasterized.
		device.PresentRevision(c.revision)
	} else if changed {
		c.device.Present()
	}
}

func (c *FrameCache) CreateRGBA(img *image.RGBA) (Texture, error) {
	c.flush()
	return c.device.CreateRGBA(img)
}
func (c *FrameCache) UpdateRGBA(tex Texture, img *image.RGBA) error {
	c.flush()
	err := c.device.UpdateRGBA(tex, img)
	if err == nil {
		c.invalidateBackdrop(tex)
		c.dirty = true
		c.mutatedDuringFrame = c.active
	}
	return err
}
func (c *FrameCache) Destroy(tex Texture) {
	c.flush()
	c.invalidateBackdrop(tex)
	c.dirty = true
	c.mutatedDuringFrame = c.active
	c.device.Destroy(tex)
}

// Overlay-only resources do not affect the saved prefix. Mutations of a
// texture already drawn in that prefix prevent capturing its old pixels.
func (c *FrameCache) invalidateBackdrop(tex Texture) {
	if prefixUsesTexture(c.backdropCommands, tex) {
		c.backdropValid = false
	}
	if c.active {
		c.mutatedTextures = append(c.mutatedTextures, tex)
	}
}
func prefixUsesTexture(commands []frameCommand, tex Texture) bool {
	for _, command := range commands {
		if command.op == OpDraw && command.tex == tex {
			return true
		}
	}
	return false
}

func (c *FrameCache) Close() { c.device.Close() }

var _ Device = (*FrameCache)(nil)
