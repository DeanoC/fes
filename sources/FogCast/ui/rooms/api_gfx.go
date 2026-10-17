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
		"paint":   r.gfxPaint,
		"reuse":   r.gfxReuse,
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
	r.measures++
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

// gfx.reuse() keeps the frame already presented and ignores anything else
// this draw records. It returns false until a frame has been presented.
func (r *Instance) gfxReuse(L *lua.LState) int {
	if !r.inDraw {
		L.RaiseError("gfx.reuse may only be called inside draw()")
	}
	if !r.frameReady {
		L.Push(lua.LFalse)
		return 1
	}
	r.reuseFrame = true
	L.Push(lua.LTrue)
	return 1
}

// gfx.paint(commands) records a whole frame in one call. Each command is a
// table: {"c", color}, {"r", x, y, w, h, color}, {"t", text, x, y, size,
// color, max_w, bold, align}, {"i", image, x, y, w, h} or {"h", id, x, y, w, h}.
// A finished paint is still presented when the draw misses the soft budget.
func (r *Instance) gfxPaint(L *lua.LState) int {
	f := r.drawing(L, "paint")
	cmds := L.CheckTable(1)
	r.keepSlowFrame = true
	n := cmds.Len()
	for i := 1; i <= n; i++ {
		row, ok := cmds.RawGetInt(i).(*lua.LTable)
		if !ok {
			L.ArgError(1, "paint commands must be tables")
		}
		kind, ok := row.RawGetInt(1).(lua.LString)
		if !ok {
			L.ArgError(1, "paint command missing kind")
		}
		switch string(kind) {
		case "c":
			f.Clear = paintColor(L, row, 2)
			f.HasClear = true
		case "r":
			r.paintOp(L, f, Op{
				Kind: OpRect, X: paintNum(row, 2), Y: paintNum(row, 3), W: paintNum(row, 4), H: paintNum(row, 5),
				Color: paintColor(L, row, 6),
			})
		case "t":
			text, _ := row.RawGetInt(2).(lua.LString)
			if strings.TrimSpace(string(text)) == "" {
				continue
			}
			op := Op{
				Kind: OpText, Text: string(text), X: paintNum(row, 3), Y: paintNum(row, 4),
				Size: int(paintNum(row, 5)), Color: paintColor(L, row, 6), Align: "left",
			}
			if op.Size <= 0 {
				op.Size = 18
			}
			if op.Size > 256 {
				op.Size = 256
			}
			if w, ok := row.RawGetInt(7).(lua.LNumber); ok && w > 0 {
				op.MaxW = int(w)
			}
			if b, ok := row.RawGetInt(8).(lua.LBool); ok {
				op.Bold = bool(b)
			}
			if a, ok := row.RawGetInt(9).(lua.LString); ok {
				switch strings.ToLower(string(a)) {
				case "center", "right", "left":
					op.Align = strings.ToLower(string(a))
				default:
					L.ArgError(1, "align must be left, center or right")
				}
			}
			r.paintOp(L, f, op)
		case "i":
			L.Push(row.RawGetInt(2))
			img := r.checkImage(L, L.GetTop())
			L.Pop(1)
			if !img.Ready || img.Err != "" {
				continue
			}
			w, h := paintNum(row, 5), paintNum(row, 6)
			if w == 0 && h == 0 {
				w, h = float32(img.W), float32(img.H)
			}
			r.paintOp(L, f, Op{Kind: OpImage, Image: img.Key, X: paintNum(row, 3), Y: paintNum(row, 4), W: w, H: h})
		case "h":
			id, _ := row.RawGetInt(2).(lua.LString)
			if id == "" {
				L.ArgError(1, "hit id required")
			}
			if len(f.Hits) >= r.budget.MaxOps {
				L.RaiseError("gfx.paint: frame exceeds %d hit regions", r.budget.MaxOps)
			}
			f.Hits = append(f.Hits, Hit{ID: string(id), X: paintNum(row, 3), Y: paintNum(row, 4), W: paintNum(row, 5), H: paintNum(row, 6)})
		default:
			L.ArgError(1, "unknown paint command")
		}
	}
	return 0
}

func (r *Instance) paintOp(L *lua.LState, f *Frame, op Op) {
	if len(f.Ops) >= r.budget.MaxOps {
		L.RaiseError("gfx.paint: frame exceeds %d draw ops", r.budget.MaxOps)
	}
	f.Ops = append(f.Ops, op)
}

func paintNum(row *lua.LTable, idx int) float32 {
	n, ok := row.RawGetInt(idx).(lua.LNumber)
	if !ok {
		return 0
	}
	return float32(n)
}

func paintColor(L *lua.LState, row *lua.LTable, idx int) gfx.Color {
	L.Push(row.RawGetInt(idx))
	c := checkColor(L, L.GetTop())
	L.Pop(1)
	return c
}
