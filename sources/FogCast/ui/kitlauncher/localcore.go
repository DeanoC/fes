package kitlauncher

import (
	"context"
	"sync/atomic"
	"time"
)

// localCorePositiveTTL keeps brief runtime probe failures from interrupting
// play, while bounding stale play routing after a stopped or unreachable core.
const localCorePositiveTTL = time.Second

const (
	localCoreVerdictNone int32 = iota
	localCoreVerdictBound
	localCoreVerdictTransition
	localCoreVerdictMenuIdle
)

// LocalCoreSample is one runtime status read.
// Bound is the play-input condition: a development core is running.
// MenuIdle is a completed idle menu. starting, reboot_required, and an idle
// status whose menu display is not ready leave both false.
type LocalCoreSample struct {
	Bound    bool
	MenuIdle bool
}

type localCorePresence struct {
	until atomic.Pointer[time.Time]
	// verdict is the last successful probe. A probe error leaves it alone, so
	// a positive sample that has only aged out is not an exit, and a
	// transition sample is not a completed idle menu.
	verdict atomic.Int32
	// sampleGen is the probe generation that stored verdict. The menu hold
	// bumps the generation when it starts, so an idle sample from the shelf
	// before that hold cannot resume it. Store verdict before sampleGen so a
	// reader cannot observe the new generation with the previous verdict.
	sampleGen atomic.Uint64
}

func (p *localCorePresence) observe(sample LocalCoreSample, err error, now time.Time, gen uint64) {
	if err != nil {
		return
	}
	// A bound core owns play input. It is not an idle menu, even if a caller
	// sets both bits.
	if sample.Bound {
		until := now.Add(localCorePositiveTTL)
		p.until.Store(&until)
		p.verdict.Store(localCoreVerdictBound)
		p.sampleGen.Store(gen)
		return
	}
	p.until.Store(nil)
	if sample.MenuIdle {
		p.verdict.Store(localCoreVerdictMenuIdle)
		p.sampleGen.Store(gen)
		return
	}
	p.verdict.Store(localCoreVerdictTransition)
	p.sampleGen.Store(gen)
}

func (p *localCorePresence) bound(now time.Time) bool {
	until := p.until.Load()
	return until != nil && now.Before(*until)
}

// menuIdle reports that the last successful probe confirmed a completed idle
// menu. A missing probe, a probe error, an aged bound sample, and a
// transition such as starting or reboot_required are not that confirmation.
func (p *localCorePresence) menuIdle() bool {
	return p.verdict.Load() == localCoreVerdictMenuIdle
}

// menuIdleAt reports that confirmation for one probe generation. A completed
// idle menu from an earlier generation, including the shelf before a hold
// started, is not a resume.
func (p *localCorePresence) menuIdleAt(gen uint64) bool {
	return p.sampleGen.Load() == gen && p.menuIdle()
}

// readLocalCore reads one runtime status sample. Play input follows Bound.
// The host-absent menu handoff follows MenuIdle. It does not read
// launcher.json or the host session. fogcast-kit installs the probe; without
// one, play input stays off and this package does not open the runtime socket.
func (c *Client) readLocalCore(ctx context.Context) (LocalCoreSample, error) {
	if c == nil || c.localCore == nil {
		return LocalCoreSample{}, nil
	}
	return c.localCore(ctx)
}
