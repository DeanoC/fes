package rooms

import (
	"fmt"
	"strconv"
	"strings"

	lua "github.com/yuin/gopher-lua"

	"github.com/DeanoC/FogCast/ui/gfx"
)

// ParseHexColor accepts #RGB, #RRGGBB or #RRGGBBAA.
func ParseHexColor(s string) (gfx.Color, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "#") {
		return gfx.Color{}, fmt.Errorf("color %q must start with #", s)
	}
	hex := s[1:]
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	n, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return gfx.Color{}, fmt.Errorf("color %q: %w", s, err)
	}
	switch len(hex) {
	case 6:
		return gfx.RGBA(uint8(n>>16), uint8(n>>8), uint8(n), 255), nil
	case 8:
		return gfx.RGBA(uint8(n>>24), uint8(n>>16), uint8(n>>8), uint8(n)), nil
	default:
		return gfx.Color{}, fmt.Errorf("color %q must be #RGB, #RRGGBB or #RRGGBBAA", s)
	}
}

// FormatHexColor is #rrggbb, or #rrggbbaa when alpha is not opaque.
func FormatHexColor(c gfx.Color) string {
	if c.A == 255 {
		return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	}
	return fmt.Sprintf("#%02x%02x%02x%02x", c.R, c.G, c.B, c.A)
}

func clampByte(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v + 0.5)
}

// checkColor reads a Lua color: "#hex", {r,g,b[,a]} or {r=,g=,b=[,a=]} in 0..255.
func checkColor(L *lua.LState, idx int) gfx.Color {
	switch v := L.Get(idx).(type) {
	case lua.LString:
		c, err := ParseHexColor(string(v))
		if err != nil {
			L.ArgError(idx, err.Error())
		}
		return c
	case *lua.LTable:
		get := func(i int, key string) (float64, bool) {
			lv := v.RawGetInt(i)
			if lv == lua.LNil {
				lv = v.RawGetString(key)
			}
			if n, ok := lv.(lua.LNumber); ok {
				return float64(n), true
			}
			return 0, false
		}
		r, okR := get(1, "r")
		g, okG := get(2, "g")
		b, okB := get(3, "b")
		if !okR || !okG || !okB {
			L.ArgError(idx, "color table needs r, g, b")
		}
		a := 255.0
		if v2, ok := get(4, "a"); ok {
			a = v2
		}
		return gfx.RGBA(clampByte(r), clampByte(g), clampByte(b), clampByte(a))
	default:
		L.ArgError(idx, "color must be a #hex string or {r,g,b[,a]} table")
		return gfx.Color{}
	}
}

func optColor(L *lua.LState, idx int, def gfx.Color) gfx.Color {
	if L.Get(idx) == lua.LNil {
		return def
	}
	return checkColor(L, idx)
}
