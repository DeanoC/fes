// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/network"
)

func TestProgramNetworkStartupBindsBeforeFrontendAndJoinsLifecycle(t *testing.T) {
	var n *programNetwork
	starts := 0
	var startupContext, lifetimeContext context.Context
	f, b, _ := backendFixture(t, func(f *serviceFixture) {
		var control *network.Controller
		calls := f.cpu.calls
		err := f.s.StartNetwork(context.Background(), f.s.Status().Owner, func(ctx, lifetime context.Context, owner uint64) (*network.Controller, error) {
			starts++
			startupContext, lifetimeContext = ctx, lifetime
			n, control = newProgramNetwork(t, lifetime)
			end, ok := ctx.Deadline()
			if !ok || time.Until(end) > 5*time.Second || owner != f.s.Status().Owner {
				t.Fatal("startup lost owner or bound")
			}
			return control, nil
		})
		if err != nil || f.cpu.calls != calls {
			t.Fatal(err, "startup issued fabric IO")
		}
	})
	if startupContext.Err() != context.Canceled || lifetimeContext.Err() != nil {
		t.Fatal("startup cancellation leaked into stack lifetime")
	}
	if starts != 1 || n.stops != 0 || n.starts != 0 {
		t.Fatal("startup repeated or changed controller")
	}
	opener := func(context.Context, context.Context, uint64) (*network.Controller, error) {
		t.Fatal("late opener invoked")
		return nil, nil
	}
	if f.s.StartNetwork(context.Background(), f.s.Status().Owner, opener) == nil {
		t.Fatal("duplicate startup accepted")
	}
	ctx := context.Background()
	session, err := f.s.Launch(ctx, 0, b.entries[0].Request)
	if err != nil {
		t.Fatal(err)
	}
	if n.stops != 1 || n.starts != 1 {
		t.Fatal("launch skipped network boundary")
	}
	if _, err = f.s.Return(ctx, session); err != nil {
		t.Fatal(err)
	}
	if n.stops != 2 || n.starts != 2 {
		t.Fatal("return skipped network boundary")
	}
	session, err = f.s.Launch(ctx, f.s.Status().Generation, b.entries[0].Request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Park(ctx, session); err != nil {
		t.Fatal(err)
	}
	if lifetimeContext.Err() != context.Canceled || n.stops != 4 || n.starts != 3 {
		t.Fatal("Park did not stop workers after network drain")
	}
}

func TestProgramNetworkStartupRejectsBeforeOpener(t *testing.T) {
	for _, mode := range []string{"wrong-owner", "frontend", "canceled", "nil-opener"} {
		t.Run(mode, func(t *testing.T) {
			f := serviceTest(t)
			owner := f.s.Status().Owner
			ctx := context.Background()
			if mode == "wrong-owner" {
				owner++
			}
			if mode == "frontend" {
				if _, err := NewProgramBackend(f.s, nil); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "canceled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			var open func(context.Context, context.Context, uint64) (*network.Controller, error)
			if mode != "nil-opener" {
				open = func(context.Context, context.Context, uint64) (*network.Controller, error) {
					t.Fatal("invalid startup accessed opener")
					return nil, nil
				}
			}
			calls := f.cpu.calls
			if f.s.StartNetwork(ctx, owner, open) == nil || f.cpu.calls != calls || f.s.Status().State != ProgramReady {
				t.Fatal("invalid startup mutated service")
			}
		})
	}
}

func TestProgramNetworkStartupFailureRetainsAndDoesNotCleanFabric(t *testing.T) {
	for _, mode := range []string{"error", "nil-controller", "late-cancellation", "foreign-controller", "panic"} {
		t.Run(mode, func(t *testing.T) {
			f := serviceTest(t)
			n, control := newProgramNetwork(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "foreign-controller" {
				if err := control.Claim(); err != nil {
					t.Fatal(err)
				}
			}
			calls := f.cpu.calls
			opens := 0
			cause := errors.New("partial startup")
			var err error
			panicked := false
			var workerContext context.Context
			func() {
				defer func() {
					if value := recover(); value != nil {
						panicked = true
						if mode != "panic" || value != cause {
							t.Fatal(value)
						}
					}
				}()
				err = f.s.StartNetwork(ctx, f.s.Status().Owner, func(_ context.Context, lifetime context.Context, _ uint64) (*network.Controller, error) {
					workerContext = lifetime
					opens++
					switch mode {
					case "panic":
						panic(cause)
					case "error":
						return control, cause
					case "nil-controller":
						return nil, nil
					case "late-cancellation":
						cancel()
					}
					return control, nil
				})
			}()
			if (mode == "panic") != panicked {
				t.Fatal("platform panic was changed")
			}
			if workerContext.Err() != context.Canceled {
				t.Fatal("failed startup left owned workers live")
			}
			if mode != "panic" && err == nil {
				t.Fatal("failure accepted")
			}
			if mode == "error" && !errors.Is(err, cause) {
				t.Fatal("lost cause", err)
			}
			if mode != "panic" && mode != "nil-controller" && f.s.state.networkStartup != control {
				t.Fatal("partial controller discarded")
			}
			if f.s.Status().State != ProgramRebootRequired || f.cpu.calls != calls || n.stops != 0 || n.starts != 0 {
				t.Fatal("uncertain startup performed cleanup")
			}
			err = f.s.StartNetwork(context.Background(), f.s.Status().Owner, func(context.Context, context.Context, uint64) (*network.Controller, error) {
				opens++
				return control, nil
			})
			if !errors.Is(err, ErrProgramRetired) || opens != 1 || f.cpu.calls != calls {
				t.Fatal("failed startup retried")
			}
		})
	}
}
