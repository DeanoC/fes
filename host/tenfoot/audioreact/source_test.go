package audioreact

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClampAndQuantize(t *testing.T) {
	t.Parallel()
	if Clamp(-1) != 0 || Clamp(2) != 1 || Clamp(0.25) != 0.25 {
		t.Fatalf("clamp")
	}
	if Quantize(0, 8) != 0 || Quantize(1, 8) != 8 || Quantize(0.5, 8) != 4 {
		t.Fatalf("quantize")
	}
	if Quantize(0.49, 2) != 1 {
		t.Fatalf("quantize round %d", Quantize(0.49, 2))
	}
}

func TestInjectorDrivesMeasuredSample(t *testing.T) {
	t.Parallel()
	var inj Injector
	if inj.Sample(time.Time{}).Visible() {
		t.Fatal("zero injector")
	}
	inj.Set(1.5, KindMeasured)
	s := inj.Sample(time.Time{})
	if s.Kind != KindMeasured || s.Level != 1 {
		t.Fatalf("clamped %+v", s)
	}
	inj.Set(0, KindMeasured)
	if inj.Sample(time.Time{}).Visible() {
		t.Fatal("zero level should hide")
	}
	inj.Set(0.4, KindNone)
	if inj.Sample(time.Time{}).Kind != KindNone {
		t.Fatal("none should clear")
	}
}

func TestFileSourceReadsFloat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "level")
	if err := os.WriteFile(p, []byte("0.75\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := (FileSource{Path: p}).Sample(time.Time{})
	if s.Kind != KindMeasured || s.Level != 0.75 {
		t.Fatalf("file %+v", s)
	}
	if err := os.WriteFile(p, []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if (FileSource{Path: p}).Sample(time.Time{}).Kind != KindNone {
		t.Fatal("invalid file")
	}
	if (FileSource{Path: filepath.Join(dir, "missing")}).Sample(time.Time{}).Kind != KindNone {
		t.Fatal("missing file")
	}
}

func TestIdlePulseIsSyntheticAndVaries(t *testing.T) {
	t.Parallel()
	off := (IdlePulse{}).Sample(time.Unix(0, 0))
	if off.Kind != KindNone || off.Visible() {
		t.Fatalf("disabled %+v", off)
	}
	p := IdlePulse{Enabled: true, Period: 2 * time.Second, Peak: 0.4}
	low := p.Sample(time.Unix(0, 0))
	high := p.Sample(time.Unix(1, 0))
	if low.Kind != KindSynthetic || high.Kind != KindSynthetic {
		t.Fatalf("kind low=%+v high=%+v", low, high)
	}
	if high.Level <= low.Level {
		t.Fatalf("expected mid pulse higher: low=%v high=%v", low.Level, high.Level)
	}
	if high.Level < 0.35 || high.Level > 0.41 {
		t.Fatalf("peak %v", high.Level)
	}
}

func TestCombinedMeasuredWinsAndIdleIsAttractOnly(t *testing.T) {
	t.Parallel()
	var inj Injector
	inj.Set(0.6, KindMeasured)
	c := Combined{Measured: &inj, Idle: IdlePulse{Enabled: true, Period: time.Second, Peak: 0.4, Floor: 0.2}}
	got := c.Sample(time.Unix(0, 0), true)
	if got.Kind != KindMeasured || got.Level != 0.6 {
		t.Fatalf("measured %+v", got)
	}
	inj.Set(0, KindNone)
	idle := c.Sample(time.Unix(0, 0), true)
	if idle.Kind != KindSynthetic {
		t.Fatalf("attract idle %+v", idle)
	}
	browse := c.Sample(time.Unix(0, 0), false)
	if browse.Kind != KindNone {
		t.Fatalf("browse must not use idle pulse %+v", browse)
	}
}

func TestAppendHintLabelsSyntheticOnly(t *testing.T) {
	t.Parallel()
	if got := AppendHint("A play | any back", Sample{Kind: KindMeasured, Level: 1}); got != "A play | any back" {
		t.Fatalf("measured %q", got)
	}
	got := AppendHint("A play | any back", Sample{Kind: KindSynthetic, Level: 0.2})
	if !strings.Contains(got, IdleHint) {
		t.Fatalf("missing idle label %q", got)
	}
	if got2 := AppendHint(got, Sample{Kind: KindSynthetic, Level: 0.2}); got2 != got {
		t.Fatalf("duplicate %q", got2)
	}
}

func TestEnabledGate(t *testing.T) {
	t.Parallel()
	if Enabled(false, "", false, false) {
		t.Fatal("default off")
	}
	if !Enabled(true, "", false, false) || !Enabled(false, "1", false, false) || !Enabled(false, "yes", false, false) {
		t.Fatal("flag/env")
	}
	if !Enabled(false, "", true, false) || !Enabled(false, "", false, true) {
		t.Fatal("config/theme")
	}
	if Enabled(false, "0", false, false) || Enabled(false, "false", false, false) {
		t.Fatal("falsey env")
	}
}
