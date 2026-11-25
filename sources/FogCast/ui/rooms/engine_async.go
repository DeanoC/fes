package rooms

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

func (r *Instance) drainResults() {
	r.pendingMu.Lock()
	queued := r.pending
	r.pending = nil
	r.pendingMu.Unlock()
	// Conversion runs on the Lua/UI thread. A full Home family response may
	// contain 2,000 rows, so convert at most one batch per Step even when
	// several family responses arrive together. Other results may still run.
	converted := false
	var deferred []asyncResult
	for _, res := range queued {
		if res.homeFamily && res.err == nil && res.games != nil {
			if res.cancelled != nil && res.cancelled.Load() {
				continue
			}
			if converted {
				deferred = append(deferred, res)
				continue
			}
			converted = true
			if res.gameTable == nil {
				res.gameTable = r.L.NewTable()
			}
			r.convertFamilyBatch(&res)
			if res.gameIndex < len(res.games) {
				deferred = append(deferred, res)
				continue
			}
			// A cancelled partial answer never changes the shared game cache.
			accepted := make(map[string]bool)
			for _, g := range res.games {
				if accepted[g.ID] || r.gameVersion[g.ID] <= res.queryEpoch {
					r.cacheGame(g.ID, g)
					accepted[g.ID] = true
				}
			}
		}
		r.applyResult(res)
		if r.fatal != nil {
			return
		}
	}
	if len(deferred) != 0 {
		r.pendingMu.Lock()
		r.pending = append(deferred, r.pending...)
		r.pendingMu.Unlock()
	}
}

const familyResultBatch = 128

// Build at most familyResultBatch rows or nested editions per Step. Keep the
// partially built table private until every row is ready for the callback.
func (r *Instance) convertFamilyBatch(res *asyncResult) {
	for work := 0; work < familyResultBatch && res.gameIndex < len(res.games); work++ {
		game := res.games[res.gameIndex]
		if res.partialGame == nil {
			variants := game.Variants
			if len(variants) == 0 {
				variants = game.Editions
			}
			row := r.gameRow(game, false)
			if len(variants) == 0 {
				res.gameTable.Append(row)
				res.gameIndex++
				continue
			}
			res.partialGame = row
			res.partialList = r.L.NewTable()
			res.variants = variants
			row.RawSetString("variants", res.partialList)
			res.variantIndex = 0
			continue
		}
		res.partialList.Append(r.gameRow(res.variants[res.variantIndex], false))
		res.variantIndex++
		if res.variantIndex == len(res.variants) {
			res.gameTable.Append(res.partialGame)
			res.partialGame = nil
			res.partialList = nil
			res.variants = nil
			res.gameIndex++
		}
	}
}

func (r *Instance) applyResult(res asyncResult) {
	if res.hardwareRead {
		r.hardwareReading = false
	}
	if res.hardwareSave {
		r.hardwareSaving = false
	}
	if res.imageKey != "" {
		r.applyImage(res)
		return
	}
	if res.cb == nil {
		return
	}
	var payload lua.LValue = lua.LNil
	switch {
	case res.err != nil:
	case res.hardware != nil:
		payload = r.hardwareTable(*res.hardware)
	case res.selection != nil:
		t := r.L.NewTable()
		t.RawSetString("game_id", lua.LString(res.selection.GameID))
		t.RawSetString("expansion_id", lua.LString(res.selection.ExpansionID))
		payload = t
	case res.games != nil:
		if res.gameTable != nil {
			payload = res.gameTable
		} else {
			payload = r.gamesTable(res.games)
		}
	case res.platforms != nil:
		payload = r.platformsTable(res.platforms)
	case res.collections != nil:
		payload = r.collectionsTable(res.collections)
	case res.game != nil:
		r.cacheGame(res.game.ID, *res.game)
		payload = r.gameTable(*res.game)
	case res.cores != nil:
		payload = r.coresTable(res.cores)
	}
	var errVal lua.LValue = lua.LNil
	if res.err != nil {
		errVal = lua.LString(res.err.Error())
	}
	r.noteInputBaseline()
	args := []lua.LValue{payload, errVal}
	if res.libraryQuery {
		args = append(args, lua.LBool(res.transient))
		args = append(args, lua.LBool(res.more))
	}
	if err := r.callSoft("async "+res.source(), res.cb, r.budget.Frame, 0, args...); err != nil {
		if errors.Is(err, errSoftOverrun) {
			r.overrunInput(err)
		} else {
			r.fail(err)
		}
	} else {
		r.inputOverruns = 0
	}
}

var errImageBudget = errors.New("room image budget exceeded")

// deliver queues an async result for the next Step. Safe from any
// goroutine and never blocks; results for a closed or failed room are
// dropped. Decoded bitmaps reserve the image budget here, at delivery, so a
// room that is not being stepped (parked, nested, failed) cannot accumulate
// more pixels than MaxImagePixels while its results wait.
func (r *Instance) deliver(res asyncResult) {
	if r.ctx.Err() != nil {
		return
	}
	r.pendingMu.Lock()
	defer r.pendingMu.Unlock()
	if res.img != nil {
		px := res.img.Bounds().Dx() * res.img.Bounds().Dy()
		if r.pixels+px > r.budget.MaxImagePixels {
			res.img = nil
			res.err = errImageBudget
		} else {
			r.pixels += px
		}
	}
	r.pending = append(r.pending, res)
}

// reservedPixels reports the decoded image budget in use (tests/HUD).
func (r *Instance) reservedPixels() int {
	r.pendingMu.Lock()
	defer r.pendingMu.Unlock()
	return r.pixels
}

func (r *Instance) luaLog(L *lua.LState) int {
	n := L.GetTop()
	parts := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		parts = append(parts, L.ToStringMeta(L.Get(i)).String())
	}
	line := strings.Join(parts, " ")
	r.logs = append(r.logs, line)
	if len(r.logs) > 32 {
		r.logs = r.logs[len(r.logs)-32:]
	}
	fmt.Fprintf(r.opts.Stderr, "room[%s]: %s\n", r.pack.ID, line)
	return 0
}

func (r *Instance) luaRequire(L *lua.LState) int {
	name := L.CheckString(1)
	if v, ok := r.modules[name]; ok {
		L.Push(v)
		return 1
	}
	if r.loading[name] {
		L.RaiseError("require %q: circular dependency", name)
	}
	src, chunk, err := r.resolveModule(name)
	if err != nil {
		L.RaiseError("require %q: %s", name, err.Error())
	}
	fn, err := L.Load(bytes.NewReader(src), chunk)
	if err != nil {
		L.RaiseError("require %q: %s", name, err.Error())
	}
	r.loading[name] = true
	L.Push(fn)
	err = L.PCall(0, 1, nil)
	delete(r.loading, name)
	if err != nil {
		L.RaiseError("require %q: %s", name, strings.TrimSpace(err.Error()))
	}
	v := L.Get(-1)
	L.Pop(1)
	if v == lua.LNil {
		v = lua.LTrue
	}
	r.modules[name] = v
	L.Push(v)
	return 1
}

// resolveModule maps "widgets.grid" to the embedded stdlib, else to
// <pack>/widgets/grid.lua. Module names never escape the pack.
func (r *Instance) resolveModule(name string) ([]byte, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
		return nil, "", errors.New("invalid module name")
	}
	rel := strings.ReplaceAll(name, ".", "/") + ".lua"
	if data, err := fs.ReadFile(stdlibFS, "stdlib/"+rel); err == nil {
		return data, "stdlib/" + rel, nil
	}
	data, err := readPackFile(r.pack.FS, rel, maxSourceBytes)
	if err != nil {
		return nil, "", fmt.Errorf("module not found in stdlib or room (%v)", err)
	}
	return data, r.pack.ID + "/" + rel, nil
}
