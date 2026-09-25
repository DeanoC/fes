package rooms

import (
	"errors"
	"strings"

	lua "github.com/yuin/gopher-lua"

	"github.com/DeanoC/FogCast/hostclient"
)

var errNoServices = errors.New("library is not available in this launcher")

func (r *Instance) installLibrary() *lua.LTable {
	L := r.L
	t := L.NewTable()
	L.SetFuncs(t, map[string]lua.LGFunction{
		"query":       r.libraryQuery,
		"platforms":   r.libraryPlatforms,
		"collections": r.libraryCollections,
		"game":        r.libraryGame,
	})
	return t
}

func optString(t *lua.LTable, key string) string {
	if t == nil {
		return ""
	}
	if v, ok := t.RawGetString(key).(lua.LString); ok {
		return strings.TrimSpace(string(v))
	}
	return ""
}

func optBool(t *lua.LTable, key string) bool {
	if t == nil {
		return false
	}
	v, ok := t.RawGetString(key).(lua.LBool)
	return ok && bool(v)
}

func optInt(t *lua.LTable, key string, def int) int {
	if t == nil {
		return def
	}
	if v, ok := t.RawGetString(key).(lua.LNumber); ok && v > 0 {
		return int(v)
	}
	return def
}

func optInt64(t *lua.LTable, key string) int64 {
	if t == nil {
		return 0
	}
	if v, ok := t.RawGetString(key).(lua.LNumber); ok {
		return int64(v)
	}
	return 0
}

// library.query({platform, collection, q, sort, genre, year, region,
// hide_prerelease, hide_hacks, limit}, function(games, err) end)
func (r *Instance) libraryQuery(L *lua.LState) int {
	opts := L.OptTable(1, nil)
	cb := L.CheckFunction(2)
	q := hostclient.GameListQuery{
		Platform:       optString(opts, "platform"),
		Collection:     optString(opts, "collection"),
		Q:              optString(opts, "q"),
		Sort:           optString(opts, "sort"),
		Genre:          optString(opts, "genre"),
		Year:           optString(opts, "year"),
		Region:         optString(opts, "region"),
		HidePrerelease: optBool(opts, "hide_prerelease"),
		HideHacks:      optBool(opts, "hide_hacks"),
	}
	max := optInt(opts, "limit", 200)
	if max > r.budget.MaxQueryGames {
		max = r.budget.MaxQueryGames
	}
	if r.opts.Services == nil {
		r.deliver(asyncResult{cb: cb, err: errNoServices})
		return 0
	}
	go func() {
		games, err := r.opts.Services.QueryGames(r.ctx, q, max)
		if games == nil && err == nil {
			games = []hostclient.Game{}
		}
		r.deliver(asyncResult{cb: cb, games: games, err: err})
	}()
	return 0
}

func (r *Instance) libraryPlatforms(L *lua.LState) int {
	cb := L.CheckFunction(1)
	if r.opts.Services == nil {
		r.deliver(asyncResult{cb: cb, err: errNoServices})
		return 0
	}
	go func() {
		platforms, err := r.opts.Services.Platforms(r.ctx)
		if platforms == nil && err == nil {
			platforms = []hostclient.Platform{}
		}
		r.deliver(asyncResult{cb: cb, platforms: platforms, err: err})
	}()
	return 0
}

func (r *Instance) libraryCollections(L *lua.LState) int {
	cb := L.CheckFunction(1)
	if r.opts.Services == nil {
		r.deliver(asyncResult{cb: cb, err: errNoServices})
		return 0
	}
	go func() {
		collections, err := r.opts.Services.Collections(r.ctx)
		if collections == nil && err == nil {
			collections = []hostclient.Collection{}
		}
		r.deliver(asyncResult{cb: cb, collections: collections, err: err})
	}()
	return 0
}

func (r *Instance) libraryGame(L *lua.LState) int {
	id := strings.TrimSpace(L.CheckString(1))
	cb := L.CheckFunction(2)
	if id == "" {
		L.ArgError(1, "game id required")
	}
	if g, ok := r.games[id]; ok {
		r.deliver(asyncResult{cb: cb, game: &g})
		return 0
	}
	if r.opts.Services == nil {
		r.deliver(asyncResult{cb: cb, err: errNoServices})
		return 0
	}
	go func() {
		g, err := r.opts.Services.Game(r.ctx, id)
		if err != nil {
			r.deliver(asyncResult{cb: cb, err: err})
			return
		}
		r.deliver(asyncResult{cb: cb, game: &g})
	}()
	return 0
}

func (r *Instance) gamesTable(games []hostclient.Game) *lua.LTable {
	t := r.L.NewTable()
	for _, g := range games {
		r.games[g.ID] = g
		t.Append(r.gameTable(g))
	}
	return t
}

func (r *Instance) gameTable(g hostclient.Game) *lua.LTable {
	L := r.L
	t := L.NewTable()
	t.RawSetString("id", lua.LString(g.ID))
	t.RawSetString("title", lua.LString(g.Title))
	t.RawSetString("system", lua.LString(g.System))
	t.RawSetString("genre", lua.LString(g.Genre))
	t.RawSetString("year", lua.LString(g.Year))
	t.RawSetString("region", lua.LString(g.Region))
	t.RawSetString("state", lua.LString(g.State))
	t.RawSetString("series", lua.LString(g.Series))
	t.RawSetString("launchable", lua.LBool(g.LaunchEligible()))
	t.RawSetString("firmware_required", lua.LBool(g.FirmwareRequired))
	t.RawSetString("firmware_ready", lua.LBool(g.FirmwareReady))
	if exec := strings.TrimSpace(g.Execution); exec != "" {
		t.RawSetString("execution", lua.LString(exec))
	}
	t.RawSetString("favorite", lua.LBool(g.Favorite))
	t.RawSetString("play_count", lua.LNumber(g.PlayCount))
	t.RawSetString("last_played_at", lua.LNumber(g.LastPlayedAt))
	h := GameHistory(g)
	t.RawSetString("played", lua.LBool(h.Played))
	t.RawSetString("completed", lua.LBool(h.Completed))
	if block := g.LaunchBlock(); block != "" {
		t.RawSetString("launch_block", lua.LString(string(block)))
	}
	if g.ReadyHere != nil {
		t.RawSetString("ready_here", lua.LBool(*g.ReadyHere))
		t.RawSetString("ready_block", lua.LString(g.ReadyBlock))
		t.RawSetString("next_action", lua.LString(g.NextAction))
	}
	if placement := strings.TrimSpace(g.Placement); placement != "" {
		t.RawSetString("placement", lua.LString(placement))
	}
	cols := L.NewTable()
	for _, c := range g.Collections {
		cols.Append(lua.LString(c))
	}
	t.RawSetString("collections", cols)
	return t
}

func (r *Instance) platformsTable(platforms []hostclient.Platform) *lua.LTable {
	L := r.L
	t := L.NewTable()
	for _, p := range platforms {
		row := L.NewTable()
		row.RawSetString("id", lua.LString(p.ID))
		row.RawSetString("label", lua.LString(p.Label))
		row.RawSetString("game_count", lua.LNumber(p.GameCount))
		row.RawSetString("online", lua.LBool(p.Online))
		row.RawSetString("launchable", lua.LBool(p.Launchable))
		tags := L.NewTable()
		for _, tag := range p.Tags {
			tags.Append(lua.LString(tag))
		}
		row.RawSetString("tags", tags)
		t.Append(row)
	}
	return t
}

func (r *Instance) collectionsTable(collections []hostclient.Collection) *lua.LTable {
	L := r.L
	t := L.NewTable()
	for _, c := range collections {
		row := L.NewTable()
		row.RawSetString("id", lua.LString(c.ID))
		row.RawSetString("name", lua.LString(c.Name))
		t.Append(row)
	}
	return t
}
