package gfx

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"math"
)

// FC2D v1 command-stream constants. The on-wire layout is documented in
// fpga_protocol.md.
const (
	ProtocolMagic   = "FC2D"
	ProtocolVersion = uint16(1)

	FlagSoftwareReplay uint16 = 1 << 0
	FlagHardware       uint16 = 1 << 1

	FormatRGBA8 uint8 = 0

	cmdLenExt = 0xFFFF
	cmdHasSrc = 1 << 0
)

// Op is one FC2D command opcode.
type Op uint8

const (
	OpBeginFrame     Op = 0x01
	OpClear          Op = 0x02
	OpSetBlend       Op = 0x03
	OpFillRect       Op = 0x04
	OpDraw           Op = 0x05
	OpPresent        Op = 0x06
	OpCreateTexture  Op = 0x10
	OpUpdateTexture  Op = 0x11
	OpDestroyTexture Op = 0x12
	OpDebugText      Op = 0x20
	OpDrawText       Op = 0x21
	OpClose          Op = 0xFF
)

func (op Op) String() string {
	switch op {
	case OpBeginFrame:
		return "BeginFrame"
	case OpClear:
		return "Clear"
	case OpSetBlend:
		return "SetBlend"
	case OpFillRect:
		return "FillRect"
	case OpDraw:
		return "Draw"
	case OpPresent:
		return "Present"
	case OpCreateTexture:
		return "CreateTexture"
	case OpUpdateTexture:
		return "UpdateTexture"
	case OpDestroyTexture:
		return "DestroyTexture"
	case OpDebugText:
		return "DebugText"
	case OpDrawText:
		return "DrawText"
	case OpClose:
		return "Close"
	default:
		return fmt.Sprintf("Op(0x%02x)", uint8(op))
	}
}

// Header is the 16-byte FC2D stream header.
type Header struct {
	Magic    [4]byte
	Version  uint16
	Flags    uint16
	LogicalW uint16
	LogicalH uint16
}

// NewHeader returns a v1 software-replay header for logicalW×logicalH.
func NewHeader(logicalW, logicalH int) (Header, error) {
	if logicalW < 1 {
		logicalW = 1
	}
	if logicalH < 1 {
		logicalH = 1
	}
	if logicalW > 65535 || logicalH > 65535 {
		return Header{}, fmt.Errorf("fc2d: logical size %dx%d exceeds u16", logicalW, logicalH)
	}
	var magic [4]byte
	copy(magic[:], ProtocolMagic)
	return Header{
		Magic:    magic,
		Version:  ProtocolVersion,
		Flags:    FlagSoftwareReplay,
		LogicalW: uint16(logicalW),
		LogicalH: uint16(logicalH),
	}, nil
}

// Command is one decoded FC2D operation.
type Command struct {
	Op     Op
	Color  Color
	Blend  BlendMode
	Dst    Rect
	Src    *Rect
	TexID  uint32
	Width  int
	Height int
	Format uint8
	CRC32  uint32
	Pixels []byte
	Text   string
	X, Y   int
	Scale  int
	SizePx int
	Frame  uint32
}

// EncodeStream writes a v1 header plus commands.
func EncodeStream(h Header, cmds []Command) ([]byte, error) {
	if string(h.Magic[:]) != ProtocolMagic {
		return nil, fmt.Errorf("fc2d: bad magic")
	}
	if h.Version != ProtocolVersion {
		return nil, fmt.Errorf("fc2d: unsupported version %d", h.Version)
	}
	out := make([]byte, 0, 16+len(cmds)*24)
	out = append(out, h.Magic[:]...)
	out = appendU16(out, h.Version)
	out = appendU16(out, h.Flags)
	out = appendU16(out, h.LogicalW)
	out = appendU16(out, h.LogicalH)
	out = appendU32(out, 0)
	for i, c := range cmds {
		payload, flags, err := encodePayload(c)
		if err != nil {
			return nil, fmt.Errorf("fc2d: command %d %s: %w", i, c.Op, err)
		}
		out = append(out, byte(c.Op), flags)
		n := len(payload)
		if n >= cmdLenExt {
			out = appendU16(out, cmdLenExt)
			out = appendU32(out, uint32(n))
		} else {
			out = appendU16(out, uint16(n))
		}
		out = append(out, payload...)
	}
	return out, nil
}

// DecodeStream parses a v1 stream. Trailing bytes after the last command
// are an error.
func DecodeStream(raw []byte) (Header, []Command, error) {
	if len(raw) < 16 {
		return Header{}, nil, fmt.Errorf("fc2d: short header")
	}
	var h Header
	copy(h.Magic[:], raw[0:4])
	h.Version = binary.LittleEndian.Uint16(raw[4:6])
	h.Flags = binary.LittleEndian.Uint16(raw[6:8])
	h.LogicalW = binary.LittleEndian.Uint16(raw[8:10])
	h.LogicalH = binary.LittleEndian.Uint16(raw[10:12])
	reserved := binary.LittleEndian.Uint32(raw[12:16])
	if string(h.Magic[:]) != ProtocolMagic {
		return Header{}, nil, fmt.Errorf("fc2d: bad magic %q", h.Magic)
	}
	if h.Version != ProtocolVersion {
		return Header{}, nil, fmt.Errorf("fc2d: unsupported version %d", h.Version)
	}
	if reserved != 0 {
		return Header{}, nil, fmt.Errorf("fc2d: reserved header field %d", reserved)
	}
	r := raw[16:]
	var cmds []Command
	for len(r) > 0 {
		if len(r) < 4 {
			return Header{}, nil, fmt.Errorf("fc2d: truncated command header")
		}
		op := Op(r[0])
		flags := r[1]
		n := int(binary.LittleEndian.Uint16(r[2:4]))
		r = r[4:]
		if n == cmdLenExt {
			if len(r) < 4 {
				return Header{}, nil, fmt.Errorf("fc2d: truncated extended length")
			}
			n = int(binary.LittleEndian.Uint32(r[:4]))
			r = r[4:]
		}
		if n < 0 || n > len(r) {
			return Header{}, nil, fmt.Errorf("fc2d: truncated payload for %s", op)
		}
		payload := r[:n]
		r = r[n:]
		c, err := decodePayload(op, flags, payload)
		if err != nil {
			return Header{}, nil, err
		}
		cmds = append(cmds, c)
	}
	return h, cmds, nil
}

// Replay applies decoded commands to d. Stream texture ids are mapped onto
// Device handles created during replay.
func Replay(d Device, cmds []Command) error {
	if d == nil {
		return fmt.Errorf("fc2d: nil device")
	}
	ids := map[uint32]Texture{}
	for i, c := range cmds {
		if err := replayOne(d, ids, c); err != nil {
			return fmt.Errorf("fc2d: replay %d %s: %w", i, c.Op, err)
		}
	}
	return nil
}

// ReplayBytes decodes a stream and Replays it onto d.
func ReplayBytes(d Device, raw []byte) (Header, error) {
	h, cmds, err := DecodeStream(raw)
	if err != nil {
		return Header{}, err
	}
	return h, Replay(d, cmds)
}

func replayOne(d Device, ids map[uint32]Texture, c Command) error {
	switch c.Op {
	case OpBeginFrame:
		d.BeginFrame()
	case OpClear:
		d.Clear(c.Color)
	case OpSetBlend:
		d.SetBlend(c.Blend)
	case OpFillRect:
		d.FillRect(c.Dst, c.Color)
	case OpDraw:
		d.Draw(ids[c.TexID], c.Src, c.Dst)
	case OpPresent:
		d.Present()
	case OpCreateTexture:
		img, err := pixelsToRGBA(c)
		if err != nil {
			return err
		}
		tex, err := d.CreateRGBA(img)
		if err != nil {
			return err
		}
		ids[c.TexID] = tex
	case OpUpdateTexture:
		tex, ok := ids[c.TexID]
		if !ok {
			return fmt.Errorf("unknown texture %d", c.TexID)
		}
		img, err := pixelsToRGBA(c)
		if err != nil {
			return err
		}
		return d.UpdateRGBA(tex, img)
	case OpDestroyTexture:
		if tex, ok := ids[c.TexID]; ok {
			d.Destroy(tex)
			delete(ids, c.TexID)
		}
	case OpDebugText:
		d.DebugText(c.X, c.Y, c.Text, c.Scale)
	case OpDrawText:
		d.DrawText(c.X, c.Y, c.Text, c.SizePx, c.Color)
	case OpClose:
		d.Close()
	default:
		return fmt.Errorf("unknown op 0x%02x", uint8(c.Op))
	}
	return nil
}

func encodePayload(c Command) ([]byte, byte, error) {
	switch c.Op {
	case OpBeginFrame:
		var p [4]byte
		binary.LittleEndian.PutUint32(p[:], c.Frame)
		return p[:], 0, nil
	case OpClear:
		return []byte{c.Color.R, c.Color.G, c.Color.B, c.Color.A}, 0, nil
	case OpSetBlend:
		return []byte{byte(c.Blend)}, 0, nil
	case OpFillRect:
		p := make([]byte, 0, 20)
		p = appendRect(p, c.Dst)
		p = append(p, c.Color.R, c.Color.G, c.Color.B, c.Color.A)
		return p, 0, nil
	case OpDraw:
		var flags byte
		src := Rect{}
		if c.Src != nil {
			flags = cmdHasSrc
			src = *c.Src
		}
		p := make([]byte, 0, 40)
		p = appendU32(p, c.TexID)
		p = append(p, flags, 0, 0, 0)
		p = appendRect(p, src)
		p = appendRect(p, c.Dst)
		return p, flags, nil
	case OpPresent, OpClose:
		return nil, 0, nil
	case OpCreateTexture, OpUpdateTexture:
		if c.Width < 1 || c.Height < 1 {
			return nil, 0, fmt.Errorf("empty texture")
		}
		if c.Width > 65535 || c.Height > 65535 {
			return nil, 0, fmt.Errorf("texture %dx%d exceeds u16", c.Width, c.Height)
		}
		need := c.Width * c.Height * 4
		if len(c.Pixels) != need {
			return nil, 0, fmt.Errorf("pixel length %d want %d", len(c.Pixels), need)
		}
		crc := c.CRC32
		if crc == 0 {
			crc = crc32.ChecksumIEEE(c.Pixels)
		}
		p := make([]byte, 0, 14+len(c.Pixels))
		p = appendU32(p, c.TexID)
		p = appendU16(p, uint16(c.Width))
		p = appendU16(p, uint16(c.Height))
		p = append(p, c.Format, 0)
		p = appendU32(p, crc)
		p = append(p, c.Pixels...)
		return p, 0, nil
	case OpDestroyTexture:
		p := make([]byte, 0, 4)
		return appendU32(p, c.TexID), 0, nil
	case OpDebugText:
		p := make([]byte, 0, 12+len(c.Text))
		p = appendI32(p, int32(c.X))
		p = appendI32(p, int32(c.Y))
		scale := c.Scale
		if scale < 0 {
			scale = 0
		}
		if scale > 255 {
			scale = 255
		}
		p = append(p, byte(scale), 0, 0, 0)
		p = append(p, []byte(c.Text)...)
		return p, 0, nil
	case OpDrawText:
		p := make([]byte, 0, 16+len(c.Text))
		p = appendI32(p, int32(c.X))
		p = appendI32(p, int32(c.Y))
		size := c.SizePx
		if size < 0 {
			size = 0
		}
		if size > 65535 {
			size = 65535
		}
		p = appendU16(p, uint16(size))
		p = append(p, 0, 0)
		p = append(p, c.Color.R, c.Color.G, c.Color.B, c.Color.A)
		p = append(p, []byte(c.Text)...)
		return p, 0, nil
	default:
		return nil, 0, fmt.Errorf("unknown op 0x%02x", uint8(c.Op))
	}
}

func decodePayload(op Op, flags byte, p []byte) (Command, error) {
	c := Command{Op: op}
	need := func(n int) error {
		if len(p) < n {
			return fmt.Errorf("fc2d: short %s payload", op)
		}
		return nil
	}
	switch op {
	case OpBeginFrame:
		if err := need(4); err != nil {
			return Command{}, err
		}
		c.Frame = binary.LittleEndian.Uint32(p[:4])
	case OpClear:
		if err := need(4); err != nil {
			return Command{}, err
		}
		c.Color = Color{R: p[0], G: p[1], B: p[2], A: p[3]}
	case OpSetBlend:
		if err := need(1); err != nil {
			return Command{}, err
		}
		c.Blend = BlendMode(p[0])
	case OpFillRect:
		if err := need(20); err != nil {
			return Command{}, err
		}
		c.Dst = readRect(p[:16])
		c.Color = Color{R: p[16], G: p[17], B: p[18], A: p[19]}
	case OpDraw:
		if err := need(40); err != nil {
			return Command{}, err
		}
		c.TexID = binary.LittleEndian.Uint32(p[0:4])
		src := readRect(p[8:24])
		c.Dst = readRect(p[24:40])
		if flags&cmdHasSrc != 0 || p[4]&cmdHasSrc != 0 {
			cp := src
			c.Src = &cp
		}
	case OpPresent, OpClose:
		// empty
	case OpCreateTexture, OpUpdateTexture:
		if err := need(14); err != nil {
			return Command{}, err
		}
		c.TexID = binary.LittleEndian.Uint32(p[0:4])
		c.Width = int(binary.LittleEndian.Uint16(p[4:6]))
		c.Height = int(binary.LittleEndian.Uint16(p[6:8]))
		c.Format = p[8]
		c.CRC32 = binary.LittleEndian.Uint32(p[10:14])
		c.Pixels = append([]byte(nil), p[14:]...)
		needPix := c.Width * c.Height * 4
		if len(c.Pixels) != needPix {
			return Command{}, fmt.Errorf("fc2d: %s pixels %d want %d", op, len(c.Pixels), needPix)
		}
		if c.CRC32 != 0 && crc32.ChecksumIEEE(c.Pixels) != c.CRC32 {
			return Command{}, fmt.Errorf("fc2d: %s crc mismatch", op)
		}
	case OpDestroyTexture:
		if err := need(4); err != nil {
			return Command{}, err
		}
		c.TexID = binary.LittleEndian.Uint32(p[:4])
	case OpDebugText:
		if err := need(12); err != nil {
			return Command{}, err
		}
		c.X = int(int32(binary.LittleEndian.Uint32(p[0:4])))
		c.Y = int(int32(binary.LittleEndian.Uint32(p[4:8])))
		c.Scale = int(p[8])
		c.Text = string(p[12:])
	case OpDrawText:
		if err := need(16); err != nil {
			return Command{}, err
		}
		c.X = int(int32(binary.LittleEndian.Uint32(p[0:4])))
		c.Y = int(int32(binary.LittleEndian.Uint32(p[4:8])))
		c.SizePx = int(binary.LittleEndian.Uint16(p[8:10]))
		c.Color = Color{R: p[12], G: p[13], B: p[14], A: p[15]}
		c.Text = string(p[16:])
	default:
		return Command{}, fmt.Errorf("fc2d: unknown op 0x%02x", uint8(op))
	}
	return c, nil
}

func pixelsToRGBA(c Command) (*image.RGBA, error) {
	if c.Format != FormatRGBA8 {
		return nil, fmt.Errorf("unsupported format %d", c.Format)
	}
	if c.Width < 1 || c.Height < 1 {
		return nil, fmt.Errorf("empty image")
	}
	need := c.Width * c.Height * 4
	if len(c.Pixels) != need {
		return nil, fmt.Errorf("pixel length %d want %d", len(c.Pixels), need)
	}
	img := image.NewRGBA(image.Rect(0, 0, c.Width, c.Height))
	copy(img.Pix, c.Pixels)
	return img, nil
}

func tightRGBA(img *image.RGBA) (w, h int, pix []byte, err error) {
	if img == nil {
		return 0, 0, nil, fmt.Errorf("empty image")
	}
	b := img.Bounds()
	w, h = b.Dx(), b.Dy()
	if w < 1 || h < 1 || len(img.Pix) == 0 {
		return 0, 0, nil, fmt.Errorf("empty image")
	}
	stride := w * 4
	pix = make([]byte, stride*h)
	for y := 0; y < h; y++ {
		off := img.PixOffset(b.Min.X, b.Min.Y+y)
		copy(pix[y*stride:(y+1)*stride], img.Pix[off:off+stride])
	}
	return w, h, pix, nil
}

func appendU16(b []byte, v uint16) []byte {
	return append(b, byte(v), byte(v>>8))
}

func appendU32(b []byte, v uint32) []byte {
	return append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

func appendI32(b []byte, v int32) []byte {
	return appendU32(b, uint32(v))
}

func appendRect(b []byte, r Rect) []byte {
	b = appendU32(b, math.Float32bits(r.X))
	b = appendU32(b, math.Float32bits(r.Y))
	b = appendU32(b, math.Float32bits(r.W))
	b = appendU32(b, math.Float32bits(r.H))
	return b
}

func readRect(p []byte) Rect {
	return Rect{
		X: math.Float32frombits(binary.LittleEndian.Uint32(p[0:4])),
		Y: math.Float32frombits(binary.LittleEndian.Uint32(p[4:8])),
		W: math.Float32frombits(binary.LittleEndian.Uint32(p[8:12])),
		H: math.Float32frombits(binary.LittleEndian.Uint32(p[12:16])),
	}
}
