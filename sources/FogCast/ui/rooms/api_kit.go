package rooms

import (
	"context"
	"errors"

	"github.com/DeanoC/FogCast/internal/localcores"
	lua "github.com/yuin/gopher-lua"
)

// LocalCores is the kit-local control socket. A nil value leaves the kit
// table unset. Tenfoot does not inject one; a later slice can pass
// *localcores.Client, which implements this.
type LocalCores interface {
	List(ctx context.Context) ([]localcores.Core, error)
	Launch(ctx context.Context, packageID string) error
	Stop(ctx context.Context) error
}

// ErrNoLocalCores is returned when a core destination is activated and no
// client was injected. The room keeps running.
var ErrNoLocalCores = errors.New("local cores are not available")

var _ LocalCores = (*localcores.Client)(nil)

func (r *Instance) installKit() *lua.LTable {
	L := r.L
	t := L.NewTable()
	L.SetFuncs(t, map[string]lua.LGFunction{
		"cores": r.kitCores,
	})
	return t
}

// kit.cores(function(rows, err) end) lists installed cores. rows are
// {package_id, core_id, name, needs, launchable, block}.
func (r *Instance) kitCores(L *lua.LState) int {
	cb := L.CheckFunction(1)
	client := r.opts.Local
	if client == nil {
		r.deliver(asyncResult{cb: cb, err: ErrNoLocalCores})
		return 0
	}
	go func() {
		cores, err := client.List(r.ctx)
		if err != nil {
			r.deliver(asyncResult{cb: cb, err: err})
			return
		}
		if cores == nil {
			cores = []localcores.Core{}
		}
		r.deliver(asyncResult{cb: cb, cores: cores})
	}()
	return 0
}

func (r *Instance) coresTable(cores []localcores.Core) *lua.LTable {
	t := r.L.NewTable()
	for _, c := range cores {
		row := r.L.NewTable()
		row.RawSetString("package_id", lua.LString(c.PackageID))
		row.RawSetString("core_id", lua.LString(c.CoreID))
		row.RawSetString("name", lua.LString(c.Name))
		row.RawSetString("needs", lua.LString(c.Needs))
		row.RawSetString("launchable", lua.LBool(c.Launchable))
		row.RawSetString("block", lua.LString(c.Block))
		t.Append(row)
	}
	return t
}

// ActivateDestination launches the published core destination through the
// injected client. Any other kind, and a core that is not launchable, is a
// no-op. A missing client returns ErrNoLocalCores. in_use, blocked and
// not_found come back as the client sent them and do not fail the room.
func (r *Instance) ActivateDestination(ctx context.Context) error {
	if r == nil || r.dest.Kind != KindCore || !r.dest.CoreLaunchable {
		return nil
	}
	if r.opts.Local == nil {
		return ErrNoLocalCores
	}
	if ctx == nil {
		ctx = context.Background()
	}
	err := r.opts.Local.Launch(ctx, r.dest.PackageID)
	if err != nil {
		r.dest.Availability = AvailUnavailable
		r.dest.Status = err.Error()
		r.dest.Action = err.Error()
		return err
	}
	return nil
}
