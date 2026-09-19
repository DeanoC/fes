package rooms

import (
	"strings"

	lua "github.com/yuin/gopher-lua"

	"github.com/DeanoC/FogCast/ui/gfx"
)

func (r *Instance) installGfx() *lua.LTable {
	L := r.L
	t := L.NewTable()
	L.SetFuncs(t, map[string]lua.LGFunction{
		"clear":   r.gfxClear,
		"rect":    r.gfxRect,
		"image":   r.gfxImage,
		"text":    r.gfxText,
		"measure": r.gfxMeasure,
		"clip":    r.gfxClip,
		"unclip":  r.gfxUnclip,
		"hit":     r.gfxHit,
		"blend":   r.gfxBlend,
	})
	return t
}

func (r *Instance) drawing(L *lua.LState, fn string) *Frame {
	if !r.inDraw || r.building == nil {
		L.RaiseError("gfx.%s may only be called inside draw()", fn)
	}
	if len(r.building.Ops) >= r.budget.MaxOps {
		L.RaiseError("gfx.%s: frame exceeds %d draw ops", fn, r.budget.MaxOps)
	}
	return r.building
}

func num(L *lua.LState, idx int) float32 { return float32(L.CheckNumber(idx)) }

func (r *Instance) gfxClear(L *lua.LState) int {
	f := r.drawing(L, "clear")
	f.Clear = checkColor(L, 1)
	f.HasClear = true
	return 0
}

func (r *Instance) gfxRect(L *lua.LState) int {
	f := r.drawing(L, "rect")
	f.Ops = append(f.Ops, Op{
		Kind: OpRect, X: num(L, 1), Y: num(L, 2), W: num(L, 3), H: num(L, 4),
		Color: checkColor(L, 5),
	})
	return 0
}

// gfx.image(img, x, y [, w, h [, sx, sy, sw, sh]])
func (r *Instance) gfxImage(L *lua.LState) int {
	f := r.drawing(L, "image")
	img := r.checkImage(L, 1)
	op := Op{Kind: OpImage, Image: img.Key, X: num(L, 2), Y: num(L, 3)}
	if L.GetTop() >= 5 {
		op.W, op.H = num(L, 4), num(L, 5)
	} else {
		op.W, op.H = float32(img.W), float32(img.H)
	}
	if L.GetTop() >= 9 {
		op.Src = &gfx.Rect{X: num(L, 6), Y: num(L, 7), W: num(L, 8), H: num(L, 9)}
	}
	if !img.Ready || img.Err != "" {
		return 0
	}
	f.Ops = append(f.Ops, op)
	return 0
}

// gfx.text(str, x, y [, opts{size, bold|weight, color, max_w, align}])
func (r *Instance) gfxText(L *lua.LState) int {
	f := r.drawing(L, "text")
	text := L.CheckString(1)
	op := Op{Kind: OpText, Text: text, X: num(L, 2), Y: num(L, 3), Size: 18, Color: r.opts.Theme.Label, Align: "left"}
	if opts := L.OptTable(4, nil); opts != nil {
		if v, ok := opts.RawGetString("size").(lua.LNumber); ok && v > 0 {
			op.Size = int(v)
		}
		if v, ok := opts.RawGetString("bold").(lua.LBool); ok {
			op.Bold = bool(v)
		}
		if v, ok := opts.RawGetString("weight").(lua.LString); ok {
			op.Bold = strings.EqualFold(string(v), "bold")
		}
		if c := opts.RawGetString("color"); c != lua.LNil {
			L.Push(c)
			op.Color = checkColor(L, L.GetTop())
			L.Pop(1)
		}
		if v, ok := opts.RawGetString("max_w").(lua.LNumber); ok && v > 0 {
			op.MaxW = int(v)
		}
		if v, ok := opts.RawGetString("align").(lua.LString); ok {
			switch strings.ToLower(string(v)) {
			case "center", "right", "left":
				op.Align = strings.ToLower(string(v))
			default:
				L.ArgError(4, "align must be left, center or right")
			}
		}
	}
	if op.Size > 256 {
		op.Size = 256
	}
	if strings.TrimSpace(text) == "" {
		return 0
	}
	f.Ops = append(f.Ops, op)
	return 0
}

// gfx.measure(str, size [, bold]) -> width, height
func (r *Instance) gfxMeasure(L *lua.LState) int {
	text := L.CheckString(1)
	size := int(L.OptNumber(2, 18))
	if size <= 0 {
		size = 18
	}
	if size > 256 {
		size = 256
	}
	w := gfx.WeightRegular
	if L.OptBool(3, false) {
		w = gfx.WeightBold
	}
	L.Push(lua.LNumber(gfx.MeasureTextWeight(text, size, w)))
	L.Push(lua.LNumber(gfx.TextHeightWeight(size, w)))
	return 2
}

func (r *Instance) gfxClip(L *lua.LState) int {
	f := r.drawing(L, "clip")
	f.Ops = append(f.Ops, Op{Kind: OpClipPush, X: num(L, 1), Y: num(L, 2), W: num(L, 3), H: num(L, 4)})
	return 0
}

func (r *Instance) gfxUnclip(L *lua.LState) int {
	f := r.drawing(L, "unclip")
	f.Ops = append(f.Ops, Op{Kind: OpClipPop})
	return 0
}

func (r *Instance) gfxHit(L *lua.LState) int {
	f := r.drawing(L, "hit")
	id := L.CheckString(1)
	if len(f.Hits) >= r.budget.MaxOps {
		L.RaiseError("gfx.hit: frame exceeds %d hit regions", r.budget.MaxOps)
	}
	f.Hits = append(f.Hits, Hit{ID: id, X: num(L, 2), Y: num(L, 3), W: num(L, 4), H: num(L, 5)})
	return 0
}

func (r *Instance) gfxBlend(L *lua.LState) int {
	f := r.drawing(L, "blend")
	mode := gfx.BlendAlpha
	switch strings.ToLower(L.CheckString(1)) {
	case "alpha":
	case "none":
		mode = gfx.BlendNone
	default:
		L.ArgError(1, "blend must be alpha or none")
	}
	f.Ops = append(f.Ops, Op{Kind: OpBlend, Blend: mode})
	return 0
}
