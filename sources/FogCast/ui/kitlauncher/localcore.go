package kitlauncher

import (
	"context"
	"sync/atomic"
	"time"
)

// localCorePositiveTTL keeps brief runtime probe failures from interrupting
// play, while bounding stale play routing after a stopped or unreachable core.
const localCorePositiveTTL = time.Second

type localCorePresence struct {
	until atomic.Pointer[time.Time]
}

func (p *localCorePresence) observe(bound bool, err error, now time.Time) {
	if err != nil {
		return
	}
	if !bound {
		p.until.Store(nil)
		return
	}
	until := now.Add(localCorePositiveTTL)
	p.until.Store(&until)
}

func (p *localCorePresence) bound(now time.Time) bool {
	until := p.until.Load()
	return until != nil && now.Before(*until)
}

// readLocalCore reports whether the runtime on this kit has a core bound.
// Play input follows this bit. It does not read launcher.json or the host
// session. fogcast-kit installs the probe; without one, play input stays off
// and this package does not open the runtime socket.
func (c *Client) readLocalCore(ctx context.Context) (bool, error) {
	if c == nil || c.localCore == nil {
		return false, nil
	}
	return c.localCore(ctx)
}
