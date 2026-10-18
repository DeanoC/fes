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
// each callback's elapsed time, the Go GC cycles during the run, and how
// many of the room's images were still loading. Kit reports (#761, #784)
// need that evidence to tell scheduler or GC pauses from cover decoding or
// the script's own work before any budget policy changes.
type overrunStreak struct {
	elapsed       []time.Duration
	gcAtStart     uint64
	imagesLoading int
}

var gcCyclesSample = []metrics.Sample{{Name: "/gc/cycles/total:gc-cycles"}}

func gcCycles() uint64 {
	metrics.Read(gcCyclesSample)
	if gcCyclesSample[0].Value.Kind() != metrics.KindUint64 {
		return 0
	}
	return gcCyclesSample[0].Value.Uint64()
}

func (s *overrunStreak) add(r *Instance, err error, count int) {
	if count == 1 {
		*s = overrunStreak{gcAtStart: gcCycles()}
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
	return fmt.Sprintf("elapsed %s; %d GC cycles; up to %d images loading",
		strings.Join(parts, ", "), gcCycles()-s.gcAtStart, s.imagesLoading)
}

// imagesLoading counts requested room images that have not arrived.
func (r *Instance) imagesLoading() int {
	n := 0
	for _, im := range r.images {
		if im != nil && !im.Ready && im.Err == "" {
			n++
		}
	}
	return n
}
