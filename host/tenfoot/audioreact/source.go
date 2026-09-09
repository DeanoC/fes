// Package audioreact is a CGO-free level source for kit chrome.
//
// A measured sample is a 0..1 peak from an injected file or test double.
// The designated kit exposes only an ALSA Dummy card, which is not FPGA
// HDMI audio; Probe reports that and does not treat Dummy as a meter.
// When chrome is enabled without a measured source, IdlePulse is a quiet
// attract-only animation labeled "idle pulse" so it is never claimed as
// game audio.
package audioreact

import (
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/host/tenfoot/anim"
)

// Kind names how a sample was produced. Empty means no chrome.
type Kind string

const (
	KindNone      Kind = ""
	KindMeasured  Kind = "measured"
	KindSynthetic Kind = "synthetic"
)

const (
	DefaultIdlePeriod = 2400 * time.Millisecond
	DefaultIdlePeak   = 0.28
	IdleHint          = "idle pulse"
)

// Sample is one 0..1 chrome level. Zero Kind skips paint.
type Sample struct {
	Level float64
	Kind  Kind
}

// Visible is true when chrome should paint.
func (s Sample) Visible() bool {
	return s.Kind != KindNone && s.Level > 0
}

// Source yields a sample for one present.
type Source interface {
	Sample(now time.Time) Sample
}

// Clamp maps level onto [0, 1].
func Clamp(level float64) float64 {
	if math.IsNaN(level) || level < 0 {
		return 0
	}
	if level > 1 {
		return 1
	}
	return level
}

// Quantize maps level onto 0..steps inclusive for present-key skip.
func Quantize(level float64, steps int) int {
	level = Clamp(level)
	if steps < 1 {
		steps = 8
	}
	q := int(math.Round(level * float64(steps)))
	if q < 0 {
		q = 0
	}
	if q > steps {
		q = steps
	}
	return q
}

// Injector is a test/demo measured source.
type Injector struct {
	value Sample
}

// Set stores a clamped sample. KindNone clears chrome.
func (i *Injector) Set(level float64, kind Kind) {
	if i == nil {
		return
	}
	if kind == KindNone {
		i.value = Sample{}
		return
	}
	i.value = Sample{Level: Clamp(level), Kind: kind}
}

// Sample returns the stored value.
func (i *Injector) Sample(time.Time) Sample {
	if i == nil {
		return Sample{}
	}
	return i.value
}

// FileSource reads one 0..1 float from Path. Missing or invalid files
// yield KindNone. This is the measured injector for tests and kit demos.
type FileSource struct {
	Path string
}

// Sample reads the file. It does not cache.
func (f FileSource) Sample(time.Time) Sample {
	path := strings.TrimSpace(f.Path)
	if path == "" {
		return Sample{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Sample{}
	}
	s := strings.TrimSpace(string(data))
	if s == "" {
		return Sample{}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return Sample{}
	}
	return Sample{Level: Clamp(v), Kind: KindMeasured}
}

// IdlePulse is a quiet sine-triangle attract animation. It is not audio.
type IdlePulse struct {
	Enabled bool
	Period  time.Duration
	Peak    float64
	Floor   float64
}

// Sample returns KindSynthetic when Enabled, otherwise KindNone.
func (p IdlePulse) Sample(now time.Time) Sample {
	if !p.Enabled {
		return Sample{}
	}
	if now.IsZero() {
		now = time.Now()
	}
	period := p.Period
	if period <= 0 {
		period = DefaultIdlePeriod
	}
	peak := p.Peak
	if peak <= 0 {
		peak = DefaultIdlePeak
	}
	floor := p.Floor
	if floor < 0 {
		floor = 0
	}
	if peak < floor {
		peak = floor
	}
	elapsed := now.UnixNano() % int64(period)
	if elapsed < 0 {
		elapsed = -elapsed
	}
	t := float64(elapsed) / float64(period)
	level := floor + (peak-floor)*anim.Pulse01(t)
	return Sample{Level: Clamp(level), Kind: KindSynthetic}
}

// Combined prefers a measured source. IdlePulse is attract-only.
type Combined struct {
	Measured Source
	Idle     IdlePulse
}

// Sample returns measured chrome when present. Synthetic idle is used
// only while attract is showing and no measured sample exists.
func (c Combined) Sample(now time.Time, attract bool) Sample {
	if c.Measured != nil {
		if s := c.Measured.Sample(now); s.Kind == KindMeasured {
			return s
		}
	}
	if attract {
		return c.Idle.Sample(now)
	}
	return Sample{}
}

// AppendHint adds IdleHint when sample is synthetic so attract copy stays honest.
func AppendHint(hint string, sample Sample) string {
	if sample.Kind != KindSynthetic {
		return hint
	}
	if strings.Contains(hint, IdleHint) {
		return hint
	}
	hint = strings.TrimSpace(hint)
	if hint == "" {
		return IdleHint
	}
	return hint + " | " + IdleHint
}

// Enabled is the operator/theme gate. Built-in packs stay off.
func Enabled(flag bool, env string, config, theme bool) bool {
	return flag || config || theme || envTruthy(env)
}

func envTruthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
