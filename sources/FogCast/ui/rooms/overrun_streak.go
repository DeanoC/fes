package rooms

import (
	"errors"
	"fmt"
	"runtime/metrics"
	"strings"
	"time"
)

// overrunStreak records a run of consecutive soft overruns so that the
// error that retires a room says what was slow and what else was going on:
// each callback's elapsed time, how many process-wide Go GC cycles completed
// during the run, and how many images the launcher had already been asked to
// fetch. Kit reports (#761, #784) need that evidence before any budget policy
// changes.
//
// The GC figure is process-wide cycles between the sample taken before the
// callback that starts the streak and the sample taken at retirement. It is
// context for the elapsed times, not pause time inside those callbacks.
// Frame and input streaks keep separate baselines. Each read uses its own
// sample slice so two rooms can overrun at once without racing.
type overrunStreak struct {
	elapsed       []time.Duration
	gcAtStart     uint64
	gcBefore      uint64
	haveBefore    bool
	imagesLoading int
	// sample, when set, replaces the process GC read. Tests use it.
	// Production leaves it nil.
	sample func() uint64
}

// gcCycles is the process-wide count of completed GC cycles.
func gcCycles() uint64 {
	sample := []metrics.Sample{{Name: "/gc/cycles/total:gc-cycles"}}
	metrics.Read(sample)
	if sample[0].Value.Kind() != metrics.KindUint64 {
		return 0
	}
	return sample[0].Value.Uint64()
}

func (s *overrunStreak) cycles() uint64 {
	if s.sample != nil {
		return s.sample()
	}
	return gcCycles()
}

// noteBefore samples GC before a callback that may start a streak. Sampling
// after the callback returns misses a cycle that ran inside that callback.
func (s *overrunStreak) noteBefore() {
	s.gcBefore = s.cycles()
	s.haveBefore = true
}

// noteFrameBaseline samples before a frame callback only while no frame
// streak is open, so a healthy room pays for one read per callback and a
// room already overrunning does not sample again until the streak breaks.
func (r *Instance) noteFrameBaseline() {
	if r.frameOverruns == 0 {
		r.frameStreak.noteBefore()
	}
}

// noteInputBaseline is the input-streak counterpart of noteFrameBaseline.
func (r *Instance) noteInputBaseline() {
	if r.inputOverruns == 0 {
		r.inputStreak.noteBefore()
	}
}

func (s *overrunStreak) add(r *Instance, err error, count int) {
	if count == 1 {
		base := s.gcBefore
		if !s.haveBefore {
			base = s.cycles()
		}
		sample := s.sample
		*s = overrunStreak{gcAtStart: base, sample: sample}
	}
	var soft softOverrun
	if errors.As(err, &soft) {
		s.elapsed = append(s.elapsed, soft.elapsed)
	}
	s.imagesLoading = max(s.imagesLoading, r.imagesLoading())
}

func (s *overrunStreak) summary(r *Instance) string {
	parts := make([]string, 0, len(s.elapsed))
	for _, d := range s.elapsed {
		parts = append(parts, d.Round(100*time.Microsecond).String())
	}
	return fmt.Sprintf("elapsed %s; %d process-wide GC cycles; up to %d images loading",
		strings.Join(parts, ", "), s.cycles()-s.gcAtStart, s.imagesLoading)
}

// imagesLoading counts images the launcher has already been asked to fetch
// and that have not arrived. A cover handle exists as soon as the script
// calls image.cover, but it stays unrequested until TakeCoverRequests, which
// the launcher runs only after a successful step. Counting those handles
// would report download contention for a room that retired before any fetch.
func (r *Instance) imagesLoading() int {
	n := 0
	for _, im := range r.images {
		if im != nil && im.requested && !im.Ready && im.Err == "" {
			n++
		}
	}
	return n
}
