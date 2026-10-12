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
// MenuGeneration is the runtime menu generation when MenuIdle is set. Zero
// means this sample has no idle-menu identity. A generation equal to the
// shelf captured when a hold starts is that pre-launch menu.
type LocalCoreSample struct {
	Bound          bool
	MenuIdle       bool
	MenuGeneration uint64
}

type localCorePresence struct {
	until atomic.Pointer[time.Time]
	// verdict is the last successful probe. A probe error withdraws an idle
	// confirmation and leaves a bound core's play-input TTL alone, so a
	// positive sample that has only aged out is not an exit, and a transition
	// sample is not a completed idle menu.
	verdict atomic.Int32
	// sampleGen is the probe generation that stored verdict. The menu hold
	// bumps the generation when it starts, so an idle sample from the shelf
	// before that hold cannot resume it. Store verdict before sampleGen so a
	// reader cannot observe the new generation with the previous verdict.
	sampleGen atomic.Uint64
	// menuGen is the menu generation of the last successful idle sample.
	menuGen atomic.Uint64
	// leftIdleGen is the probe generation that saw the runtime leave the
	// shelf, either bound or in transition. Zero means this hold has not.
	leftIdleGen atomic.Uint64
}

func (p *localCorePresence) observe(sample LocalCoreSample, err error, now time.Time, gen uint64) {
	if err != nil {
		// A failed read is not a current idle menu. Play input keeps the
		// one-second bound TTL; this does not mark the runtime as having left
		// the shelf.
		if p.sampleGen.Load() == gen && p.verdict.Load() == localCoreVerdictMenuIdle {
			p.verdict.Store(localCoreVerdictNone)
		}
		return
	}
	// A bound core owns play input. It is not an idle menu, even if a caller
	// sets both bits.
	if sample.Bound {
		until := now.Add(localCorePositiveTTL)
		p.until.Store(&until)
		p.markLeftIdle(gen)
		p.verdict.Store(localCoreVerdictBound)
		p.sampleGen.Store(gen)
		return
	}
	p.until.Store(nil)
	if sample.MenuIdle {
		p.verdict.Store(localCoreVerdictMenuIdle)
		p.menuGen.Store(sample.MenuGeneration)
		p.sampleGen.Store(gen)
		return
	}
	p.markLeftIdle(gen)
	p.verdict.Store(localCoreVerdictTransition)
	p.sampleGen.Store(gen)
}

func (p *localCorePresence) markLeftIdle(gen uint64) {
	if gen != 0 {
		p.leftIdleGen.Store(gen)
	}
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

// menuGeneration is the last successful idle menu's runtime generation.
func (p *localCorePresence) menuGeneration() uint64 {
	return p.menuGen.Load()
}

func (p *localCorePresence) leftIdle(gen uint64) bool {
	return gen != 0 && p.leftIdleGen.Load() == gen
}

// adoptShelfGeneration records the first idle generation sampled during this
// hold when the hold did not already know the shelf. That sample is the
// pre-launch menu, not proof that startup finished.
func (p *localCorePresence) adoptShelfGeneration(gen, baseline uint64) uint64 {
	if baseline != 0 || gen == 0 || p.leftIdle(gen) || !p.menuIdle() || p.sampleGen.Load() != gen {
		return baseline
	}
	if generation := p.menuGen.Load(); generation != 0 {
		return generation
	}
	return baseline
}

// menuIdleAt reports an idle menu that belongs to this hold. A completed idle
// from an earlier probe generation does not qualify. Neither does the shelf
// generation captured for the hold, unless this hold has since seen the
// runtime leave that menu. A different non-zero generation does, even when the
// transition was not sampled. A probe error withdraws the confirmation.
func (p *localCorePresence) menuIdleAt(gen, baseline uint64) bool {
	if p.sampleGen.Load() != gen || !p.menuIdle() {
		return false
	}
	if p.leftIdle(gen) {
		return true
	}
	generation := p.menuGen.Load()
	return baseline != 0 && generation != 0 && generation != baseline
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
