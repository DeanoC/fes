package rooms

import (
	"strconv"
	"strings"

	"github.com/DeanoC/FogCast/protocol"

	lua "github.com/yuin/gopher-lua"
)

func (r *Instance) installAPI() {
	L := r.L
	L.SetGlobal("gfx", r.installGfx())
	L.SetGlobal("image", r.installImage())
	L.SetGlobal("library", r.installLibrary())
	L.SetGlobal("hardware", r.installHardware())
	L.SetGlobal("rooms", r.installRooms())
	L.SetGlobal("session", r.installSession())
	L.SetGlobal("store", r.installStore())
	L.SetGlobal("destination", r.installDestination())
	L.SetGlobal("room", r.installRoom())
	// kit exists only when a local-control client is injected. On the host
	// the global stays nil, so scripts can tell a kit screen from a host one.
	if r.opts.Local != nil {
		L.SetGlobal("kit", r.installKit())
	}
}

func (r *Instance) installRoom() *lua.LTable {
	L := r.L
	t := L.NewTable()
	t.RawSetString("id", lua.LString(r.pack.ID))
	t.RawSetString("title", lua.LString(r.pack.Title))
	t.RawSetString("width", lua.LNumber(r.opts.Width))
	t.RawSetString("height", lua.LNumber(r.opts.Height))
	t.RawSetString("time", lua.LNumber(0))
	t.RawSetString("reduced_motion", lua.LBool(r.opts.ReducedMotion))
	t.RawSetString("live_controls", lua.LBool(!r.opts.SessionDisplayRequired))
	th := r.opts.Theme
	colors := L.NewTable()
	background := FormatHexColor(th.SofaBackground)
	if r.pack.Theme.Background != "" {
		background = r.pack.Theme.Background
	}
	accent := FormatHexColor(th.Highlight)
	if r.pack.Theme.Accent != "" {
		accent = r.pack.Theme.Accent
	}
	colors.RawSetString("background", lua.LString(background))
	colors.RawSetString("accent", lua.LString(accent))
	colors.RawSetString("highlight", lua.LString(FormatHexColor(th.Highlight)))
	colors.RawSetString("flash", lua.LString(FormatHexColor(th.Flash)))
	colors.RawSetString("label", lua.LString(FormatHexColor(th.Label)))
	colors.RawSetString("label_bar", lua.LString(FormatHexColor(th.LabelBar)))
	colors.RawSetString("status", lua.LString(FormatHexColor(th.Status)))
	colors.RawSetString("header", lua.LString(FormatHexColor(th.Header)))
	colors.RawSetString("header_bar", lua.LString(FormatHexColor(th.HeaderBar)))
	colors.RawSetString("footer_bar", lua.LString(FormatHexColor(th.FooterBar)))
	colors.RawSetString("cover_frame", lua.LString(FormatHexColor(th.CoverFrame)))
	t.RawSetString("theme", colors)
	r.roomTable = t
	return t
}

func (r *Instance) installRooms() *lua.LTable {
	L := r.L
	t := L.NewTable()
	L.SetFuncs(t, map[string]lua.LGFunction{
		"home": func(L *lua.LState) int {
			r.actions = append(r.actions, Action{Kind: ActionHome})
			return 0
		},
		"open": func(L *lua.LState) int {
			id := strings.TrimSpace(L.CheckString(1))
			if id == "" {
				L.ArgError(1, "room id required")
			}
			if r.opts.Index != nil {
				if p, ok := r.opts.Index.Find(id); !ok || !p.Valid() {
					L.RaiseError("rooms.open: unknown room %q", id)
				}
			}
			r.actions = append(r.actions, Action{Kind: ActionOpenRoom, RoomID: id})
			return 0
		},
		"back": func(L *lua.LState) int {
			r.actions = append(r.actions, Action{Kind: ActionBack})
			return 0
		},
		"open_library": func(L *lua.LState) int {
			opts := L.OptTable(1, nil)
			r.actions = append(r.actions, Action{
				Kind:       ActionOpenLibrary,
				Platform:   optString(opts, "platform"),
				Collection: optString(opts, "collection"),
				Layout:     optString(opts, "layout"),
			})
			return 0
		},
		"list": func(L *lua.LState) int {
			out := L.NewTable()
			if r.opts.Index != nil {
				for _, p := range r.opts.Index.Packs {
					if !p.Valid() {
						continue
					}
					row := L.NewTable()
					row.RawSetString("id", lua.LString(p.ID))
					row.RawSetString("title", lua.LString(p.Title))
					row.RawSetString("author", lua.LString(p.Author))
					row.RawSetString("description", lua.LString(p.Description))
					out.Append(row)
				}
			}
			L.Push(out)
			return 1
		},
	})
	return t
}

func (r *Instance) installSession() *lua.LTable {
	L := r.L
	t := L.NewTable()
	L.SetFuncs(t, map[string]lua.LGFunction{
		"stop": func(L *lua.LState) int {
			r.actions = append(r.actions, roomSessionAction(L, ActionStop))
			return 0
		},
		"open_tape": func(L *lua.LState) int {
			r.actions = append(r.actions, roomSessionAction(L, ActionOpenTape))
			return 0
		},
		"resume": func(L *lua.LState) int {
			r.actions = append(r.actions, Action{Kind: ActionResumeSession})
			return 0
		},
		"launch": func(L *lua.LState) int {
			id := strings.TrimSpace(L.CheckString(1))
			if id == "" {
				L.ArgError(1, "game id required")
			}
			r.actions = append(r.actions, Action{Kind: ActionLaunch, GameID: id})
			return 0
		},
		"state": func(L *lua.LState) int {
			L.Push(lua.LString(r.sessionState))
			return 1
		},
	})
	return t
}

// The host process ID alone is not a play identity. Carry the exact observed
// target/package/generation and flight through Stop and live-media controls.
func roomSessionAction(L *lua.LState, kind ActionKind) Action {
	s := L.CheckTable(1)
	id := optString(s, "id")
	generation, err := strconv.ParseUint(optString(s, "generation"), 10, 64)
	binding := protocol.DevelopmentMediaBinding{PackageID: optString(s, "package_id"), Generation: generation, Target: optString(s, "target"), TargetID: optString(s, "target_id")}
	if id == "" || err != nil || !binding.Valid() || binding.Target == "" {
		L.ArgError(1, "expected live session binding required")
	}
	return Action{Kind: kind, SessionID: id, GameID: optString(s, "game_id"), FlightID: optString(s, "flight_id"), MediaBinding: binding}
}

func (r *Instance) installStore() *lua.LTable {
	L := r.L
	t := L.NewTable()
	L.SetFuncs(t, map[string]lua.LGFunction{
		"get": func(L *lua.LState) int {
			key := L.CheckString(1)
			v, ok := r.store.values[key]
			if !ok {
				L.Push(L.Get(2))
				return 1
			}
			L.Push(goToLua(L, v))
			return 1
		},
		"set": func(L *lua.LState) int {
			key := L.CheckString(1)
			v, ok := luaToGo(L.Get(2), 0)
			if !ok {
				L.ArgError(2, "store values must be strings, numbers, booleans or plain tables")
			}
			r.store.set(key, v)
			return 0
		},
	})
	return t
}
