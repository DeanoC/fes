package rooms

import (
	"strings"

	lua "github.com/yuin/gopher-lua"

	"github.com/DeanoC/FogCast/hostclient"
)

func (r *Instance) installDestination() *lua.LTable {
	L := r.L
	t := L.NewTable()
	L.SetFuncs(t, map[string]lua.LGFunction{
		"set":          r.destinationSet,
		"clear":        r.destinationClear,
		"get":          r.destinationGet,
		"classify":     r.destinationClassify,
		"play_history": r.destinationPlayHistory,
	})
	return t
}

// Destination is the location the script last published for the compact panel.
func (r *Instance) Destination() Destination {
	if r == nil {
		return Destination{}
	}
	return r.dest
}

func (r *Instance) destinationSet(L *lua.LState) int {
	opts := L.CheckTable(1)
	d := Destination{
		Kind:     parseKind(optString(opts, "kind")),
		Label:    optString(opts, "label"),
		System:   optString(opts, "system"),
		GameID:   optString(opts, "game_id"),
		RoomID:   optString(opts, "room_id"),
		Note:     optString(opts, "note"),
		NoteBy:   optString(opts, "note_by"),
		Query:    optString(opts, "query"),
		Platform: optString(opts, "platform"),
	}
	if d.NoteBy == "" {
		d.NoteBy = strings.TrimSpace(r.pack.Author)
	}
	d.Matches = r.gamesFromLua(opts.RawGetString("matches"))
	if len(d.Matches) == 0 && d.GameID != "" {
		if g, ok := r.games[d.GameID]; ok {
			d.Matches = []hostclient.Game{g}
		}
	}
	switch {
	case optBool(opts, "resolving"):
		d.Availability = AvailChecking
		if d.Kind == "" {
			d.Kind = KindUnresolved
		}
	case optBool(opts, "missing"):
		d.Availability = AvailMissing
		if d.Kind == "" {
			d.Kind = KindGame
		}
	case optString(opts, "availability") != "":
		d.Availability = parseAvailability(optString(opts, "availability"))
	case d.Kind == KindRoom, d.Kind == KindLibrary:
		// Confirm is enter / open library; availability is unused.
	case len(d.Matches) > 0:
		state, matches := ClassifyGames(d.Matches, d.Query)
		d.Availability = state
		d.Matches = matches
		if d.Kind == "" {
			d.Kind = KindGame
		}
		if d.GameID == "" && state == AvailReady && len(matches) == 1 {
			d.GameID = matches[0].ID
		}
		if d.System == "" && len(matches) == 1 {
			d.System = matches[0].System
		}
	case d.GameID != "":
		if g, ok := r.games[d.GameID]; ok {
			if g.LaunchEligible() {
				d.Availability = AvailReady
			} else {
				d.Availability = AvailUnavailable
				d.Matches = []hostclient.Game{g}
			}
		} else {
			d.Availability = AvailChecking
		}
		if d.Kind == "" {
			d.Kind = KindGame
		}
	default:
		if d.Kind == "" {
			d.Kind = KindUnresolved
		}
		if d.Availability == "" {
			d.Availability = AvailChecking
		}
	}
	if d.Kind == KindGame && d.System == "" && d.Platform != "" {
		d.System = d.Platform
	}
	d.FillCopy()
	d.FillHistory()
	r.dest = d
	return 0
}

func (r *Instance) destinationClear(L *lua.LState) int {
	r.dest = Destination{}
	return 0
}

func (r *Instance) destinationGet(L *lua.LState) int {
	L.Push(r.destinationTable(r.dest))
	return 1
}

func (r *Instance) destinationClassify(L *lua.LState) int {
	games := r.gamesFromLua(L.CheckTable(1))
	opts := L.OptTable(2, nil)
	query := optString(opts, "q")
	if query == "" {
		query = optString(opts, "query")
	}
	state, matches := ClassifyGames(games, query)
	d := Destination{Kind: KindGame, Availability: state, Matches: matches, Query: query}
	if state == AvailReady && len(matches) == 1 {
		d.GameID = matches[0].ID
		d.System = matches[0].System
		d.Label = matches[0].Title
	}
	d.FillCopy()
	d.FillHistory()
	L.Push(r.destinationTable(d))
	return 1
}

func (r *Instance) destinationTable(d Destination) *lua.LTable {
	L := r.L
	t := L.NewTable()
	t.RawSetString("kind", lua.LString(d.Kind))
	t.RawSetString("label", lua.LString(d.Label))
	t.RawSetString("system", lua.LString(d.System))
	t.RawSetString("game_id", lua.LString(d.GameID))
	t.RawSetString("room_id", lua.LString(d.RoomID))
	t.RawSetString("availability", lua.LString(d.Availability))
	t.RawSetString("state", lua.LString(d.Availability))
	t.RawSetString("status", lua.LString(d.Status))
	t.RawSetString("action", lua.LString(d.Action))
	t.RawSetString("note", lua.LString(d.Note))
	t.RawSetString("note_by", lua.LString(d.NoteBy))
	t.RawSetString("query", lua.LString(d.Query))
	t.RawSetString("platform", lua.LString(d.Platform))
	t.RawSetString("played", lua.LBool(d.History.Played))
	t.RawSetString("completed", lua.LBool(d.History.Completed))
	t.RawSetString("history", lua.LString(d.History.Line()))
	matches := L.NewTable()
	for _, g := range d.Matches {
		matches.Append(r.gameTable(g))
	}
	t.RawSetString("matches", matches)
	if g, ok := d.Game(); ok {
		t.RawSetString("game", r.gameTable(g))
	}
	return t
}

func (r *Instance) gamesFromLua(v lua.LValue) []hostclient.Game {
	t, ok := v.(*lua.LTable)
	if !ok || t == nil {
		return nil
	}
	var out []hostclient.Game
	t.ForEach(func(_, val lua.LValue) {
		row, ok := val.(*lua.LTable)
		if !ok {
			return
		}
		id := optString(row, "id")
		if id != "" {
			if g, cached := r.games[id]; cached {
				// Host catalog play facts stay authoritative. gameTable always
				// emits play_count and last_played_at (including zeros), so
				// copying them from a republished match table would freeze the
				// script snapshot and discard later host values.
				out = append(out, g)
				return
			}
		}
		g := hostclient.Game{
			ID:         id,
			Title:      optString(row, "title"),
			System:     optString(row, "system"),
			Genre:      optString(row, "genre"),
			Year:       optString(row, "year"),
			Region:     optString(row, "region"),
			State:      optString(row, "state"),
			Launchable: optBool(row, "launchable"),
			RootOnline: true,
		}
		g = applyPlayFacts(g, row)
		if g.State == "" && g.Launchable {
			g.State = "available"
		}
		if id != "" || g.Title != "" {
			if id != "" {
				r.games[id] = g
			}
			out = append(out, g)
		}
	})
	return out
}

func (r *Instance) destinationPlayHistory(L *lua.LState) int {
	opts := L.OptTable(1, nil)
	playCount := optInt64(opts, "play_count")
	lastPlayed := optInt64(opts, "last_played_at")
	completed := optBool(opts, "completed")
	if id := optString(opts, "id"); id != "" {
		if g, ok := r.games[id]; ok {
			if opts == nil || opts.RawGetString("play_count") == lua.LNil {
				playCount = g.PlayCount
			}
			if opts == nil || opts.RawGetString("last_played_at") == lua.LNil {
				lastPlayed = g.LastPlayedAt
			}
		}
	}
	L.Push(r.historyTable(ClassifyHistory(playCount, lastPlayed, completed)))
	return 1
}

func (r *Instance) historyTable(h History) *lua.LTable {
	t := r.L.NewTable()
	t.RawSetString("played", lua.LBool(h.Played))
	t.RawSetString("completed", lua.LBool(h.Completed))
	t.RawSetString("line", lua.LString(h.Line()))
	return t
}

// applyPlayFacts copies play_count / last_played_at from a Lua row onto a
// game that is not already in the host catalog cache. Cached rows keep the
// host values; Lua is not the household play store.
func applyPlayFacts(g hostclient.Game, row *lua.LTable) hostclient.Game {
	if row == nil {
		return g
	}
	if row.RawGetString("play_count") != lua.LNil {
		g.PlayCount = optInt64(row, "play_count")
	}
	if row.RawGetString("last_played_at") != lua.LNil {
		g.LastPlayedAt = optInt64(row, "last_played_at")
	}
	return g
}

func parseKind(s string) Kind {
	switch Kind(strings.ToLower(strings.TrimSpace(s))) {
	case KindGame, KindRoom, KindLibrary, KindUnresolved:
		return Kind(strings.ToLower(strings.TrimSpace(s)))
	default:
		return ""
	}
}

func parseAvailability(s string) Availability {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "-", "_")
	switch Availability(s) {
	case AvailChecking, AvailMissing, AvailNeedsChoice, AvailUnavailable, AvailReady:
		return Availability(s)
	case "needs_a_choice":
		return AvailNeedsChoice
	default:
		return ""
	}
}
