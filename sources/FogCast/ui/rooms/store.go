package rooms

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	lua "github.com/yuin/gopher-lua"
)

const storeFlushEvery = time.Second

// store is the per-room key/value persistence behind store.get/set.
type store struct {
	path   string
	values map[string]any
	dirty  bool
	last   time.Time
	stderr io.Writer
	logged bool
}

func newStore(path string, stderr io.Writer) *store {
	s := &store{path: path, values: map[string]any{}, stderr: stderr}
	if path == "" {
		return s
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	var values map[string]any
	if json.Unmarshal(data, &values) == nil && values != nil {
		s.values = values
	}
	return s
}

func (s *store) set(key string, v any) {
	if v == nil {
		delete(s.values, key)
	} else {
		s.values[key] = v
	}
	s.dirty = true
}

func (s *store) maybeFlush(now time.Time) {
	if !s.dirty || now.Sub(s.last) < storeFlushEvery {
		return
	}
	s.last = now
	s.flush()
}

func (s *store) flush() {
	if !s.dirty || s.path == "" {
		s.dirty = false
		return
	}
	data, err := json.MarshalIndent(s.values, "", "  ")
	if err != nil {
		s.dirty = false
		s.noteWriteError(err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
		s.noteWriteError(err)
		return
	}
	if err := os.WriteFile(s.path, append(data, '\n'), 0o644); err != nil {
		s.noteWriteError(err)
		return
	}
	s.dirty = false
}

// noteWriteError logs the first failed save for this room and leaves the
// value in memory. Later flushes stay quiet and the script keeps running.
func (s *store) noteWriteError(err error) {
	if s.logged || err == nil {
		return
	}
	s.logged = true
	w := s.stderr
	if w == nil {
		w = os.Stderr
	}
	fmt.Fprintf(w, "tenfoot: could not save room state %s: %v\n", s.path, err)
}

const maxStoreDepth = 8

// luaToGo converts strings, numbers, booleans and tables (array or map).
func luaToGo(v lua.LValue, depth int) (any, bool) {
	switch x := v.(type) {
	case lua.LString:
		return string(x), true
	case lua.LNumber:
		return float64(x), true
	case lua.LBool:
		return bool(x), true
	case *lua.LNilType:
		return nil, true
	case *lua.LTable:
		if depth >= maxStoreDepth {
			return nil, false
		}
		n := x.Len()
		if n > 0 && x.RawGetInt(1) != lua.LNil {
			arr := make([]any, 0, n)
			for i := 1; i <= n; i++ {
				e, ok := luaToGo(x.RawGetInt(i), depth+1)
				if !ok {
					return nil, false
				}
				arr = append(arr, e)
			}
			return arr, true
		}
		m := map[string]any{}
		ok := true
		x.ForEach(func(k, val lua.LValue) {
			ks, isStr := k.(lua.LString)
			if !isStr {
				ok = false
				return
			}
			e, good := luaToGo(val, depth+1)
			if !good {
				ok = false
				return
			}
			m[string(ks)] = e
		})
		if !ok {
			return nil, false
		}
		return m, true
	default:
		return nil, false
	}
}

func goToLua(L *lua.LState, v any) lua.LValue {
	switch x := v.(type) {
	case nil:
		return lua.LNil
	case string:
		return lua.LString(x)
	case float64:
		return lua.LNumber(x)
	case bool:
		return lua.LBool(x)
	case []any:
		t := L.NewTable()
		for _, e := range x {
			t.Append(goToLua(L, e))
		}
		return t
	case map[string]any:
		t := L.NewTable()
		for k, e := range x {
			t.RawSetString(k, goToLua(L, e))
		}
		return t
	default:
		return lua.LNil
	}
}
