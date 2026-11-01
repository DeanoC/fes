package rooms

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/DeanoC/FogCast/internal/corenames"
	"github.com/DeanoC/FogCast/internal/localcores"
	lua "github.com/yuin/gopher-lua"
)

// localCoresWarmRetry is how often kit.cores asks again while the agent is
// still filling the catalog caches. The list call itself returns immediately.
const localCoresWarmRetry = 200 * time.Millisecond

const localCoresStartupRetry = 60 * time.Second

// LocalCores is the kit-local control socket. A nil value leaves the kit
// table unset. Tenfoot passes *localcores.Client only for -gfx menu-display.
// A host session leaves it nil. Status is GET /v1/local/status.
type LocalCores interface {
	List(ctx context.Context) ([]localcores.Core, error)
	Launch(ctx context.Context, packageID string) (localcores.LaunchResult, error)
	Stop(ctx context.Context, expectedGeneration string) error
	Status(ctx context.Context) (localcores.RunStatus, error)
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
// {package_id, core_id, name, needs, launchable, block, inputs}.
func (r *Instance) kitCores(L *lua.LState) int {
	cb := L.CheckFunction(1)
	client := r.opts.Local
	if client == nil {
		r.deliver(asyncResult{cb: cb, err: ErrNoLocalCores})
		return 0
	}
	go func() {
		cores, err := listLocalCores(r.ctx, client, localCoresStartupRetry)
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

// Listing is read-only. A socket that is not listening yet, or a timed-out
// catalog read, may recover during startup. Permanent errors stay visible;
// closing the room cancels both the read and the retry wait.
func listLocalCores(ctx context.Context, client LocalCores, budget time.Duration) ([]localcores.Core, error) {
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		requestCtx, requestCancel := context.WithTimeout(ctx, 3*time.Second)
		cores, err := client.List(requestCtx)
		requestCancel()
		var timeout net.Error
		transient := errors.Is(err, localcores.ErrUnavailable) || errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout())
		if err == nil || (!errors.Is(err, localcores.ErrWarming) && !transient) {
			return cores, err
		}
		timer := time.NewTimer(localCoresWarmRetry)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (r *Instance) coresTable(cores []localcores.Core) *lua.LTable {
	t := r.L.NewTable()
	for _, c := range cores {
		if !r.coreShown(c.CoreID) {
			continue
		}
		row := r.L.NewTable()
		row.RawSetString("package_id", lua.LString(c.PackageID))
		row.RawSetString("core_id", lua.LString(c.CoreID))
		row.RawSetString("name", lua.LString(corenames.Name(c.CoreID, c.Name)))
		row.RawSetString("needs", lua.LString(c.Needs))
		row.RawSetString("launchable", lua.LBool(c.Launchable))
		row.RawSetString("block", lua.LString(c.Block))
		inputs := r.L.NewTable()
		for _, name := range c.Inputs {
			inputs.Append(lua.LString(name))
		}
		row.RawSetString("inputs", inputs)
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
	_, err := r.opts.Local.Launch(ctx, r.dest.PackageID)
	if err != nil {
		r.dest.Availability = AvailUnavailable
		r.dest.Status = err.Error()
		r.dest.Action = err.Error()
		return err
	}
	return nil
}
