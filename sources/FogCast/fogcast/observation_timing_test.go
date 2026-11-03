package fogcast

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestObservationTimingRealHealthAndSession(t *testing.T) {
	f := newReprobeFixture(t)
	if f.service.timing.Load() != nil {
		t.Fatal("timing enabled by default")
	}
	dir := t.TempDir()
	_ = os.Chmod(dir, 0700)
	path := filepath.Join(dir, "timing.jsonl")
	if err := f.service.EnableObservationTiming(path); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.statusTarget(context.Background(), "kit"); err != nil {
		t.Fatal(err)
	}
	if f.mutations.Load() != 0 || f.resets.Load() != 0 || f.resolve.Load() != 0 {
		t.Fatal("observation changed ownership or discovery")
	}
	rec := f.service.timing.Load()
	close(rec.stop)
	select {
	case <-rec.done:
	case <-time.After(time.Second):
		t.Fatal("writer did not finish")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), f.endpoint) || strings.Contains(string(data), reprobeID) || strings.Contains(string(data), "grant-1") {
		t.Fatal("sensitive or target-specific field in output")
	}
	stages := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		if row["summary"] == true {
			if row["truncated"] != false || row["dropped"] != float64(0) || row["active"] != float64(0) || row["write_error"] != false {
				t.Fatalf("incomplete evidence: %v", row)
			}
			continue
		}
		if len(row) != 5 {
			t.Fatalf("unexpected fields %v", row)
		}
		stages[row["stage"].(string)]++
	}
	for _, name := range []string{"health", "session", "lifecycle", "target_lock", "probe", "probe_health", "ownership", "reconcile_status", "final_status"} {
		if stages[name] < 2 {
			t.Fatalf("missing stage %s: %v", name, stages)
		}
	}
}

func TestObservationTimingPrivateExclusiveOutput(t *testing.T) {
	f := newReprobeFixture(t)
	if err := f.service.EnableObservationTiming("relative"); err == nil {
		t.Fatal("accepted relative path")
	}
	dir := t.TempDir()
	_ = os.Chmod(dir, 0700)
	existing := filepath.Join(dir, "existing")
	if err := os.WriteFile(existing, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.service.EnableObservationTiming(existing); err == nil {
		t.Fatal("accepted existing file")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(existing, link); err != nil {
		t.Fatal(err)
	}
	if err := f.service.EnableObservationTiming(link); err == nil {
		t.Fatal("accepted symlink")
	}
	path := filepath.Join(dir, "new")
	if err := f.service.EnableObservationTiming(path); err != nil {
		t.Fatal(err)
	}
	if err := f.service.EnableObservationTiming(filepath.Join(dir, "other")); err == nil {
		t.Fatal("accepted second recorder")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("mode: %v %v", info, err)
	}
	rec := f.service.timing.Load()
	close(rec.stop)
	<-rec.done
	data, _ := os.ReadFile(existing)
	if string(data) != "unchanged" {
		t.Fatal("existing file changed")
	}
}

func TestObservationTimingOverflowNeverBlocks(t *testing.T) {
	rec := &observationTiming{epoch: time.Now(), events: make(chan timingEvent, 1)}
	rec.emit(1, timingHealth, true)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100000; i++ {
			rec.emit(1, timingHealth, false)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("full diagnostic queue blocked service")
	}
	if rec.dropped.Load() != 100000 {
		t.Fatal("lost-event count incorrect")
	}
}

func BenchmarkObservationStage(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		b.Run(map[bool]string{false: "disabled", true: "enabled_full_queue"}[enabled], func(b *testing.B) {
			ctx := context.Background()
			if enabled {
				rec := &observationTiming{epoch: time.Now(), events: make(chan timingEvent, 1)}
				ctx = context.WithValue(ctx, timingContextKey{}, timingContext{rec, 1})
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				done := observationStage(ctx, timingProbe)
				done()
			}
		})
	}
}

// Delay the real target-client status request; the timing must locate it in
// reconciliation without adding a target request or changing ownership.
type timingDelayTransport struct{ base http.RoundTripper }

func (tr timingDelayTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path == "/v1/status" {
		time.Sleep(15 * time.Millisecond)
	}
	return tr.base.RoundTrip(r)
}
func TestObservationTimingIdentifiesDelayedTargetStatus(t *testing.T) {
	f := newReprobeFixture(t)
	f.transport.base = timingDelayTransport{base: http.DefaultTransport}
	dir := t.TempDir()
	_ = os.Chmod(dir, 0700)
	path := filepath.Join(dir, "trace")
	if err := f.service.EnableObservationTiming(path); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := f.service.timing.Load()
	close(rec.stop)
	<-rec.done
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var start, end int64
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var row struct {
			Stage string `json:"stage"`
			Phase string `json:"phase"`
			Ns    int64  `json:"monotonic_ns"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		if row.Stage == "reconcile_status" && row.Phase == "start" {
			start = row.Ns
		}
		if row.Stage == "reconcile_status" && row.Phase == "end" {
			end = row.Ns
		}
	}
	if start == 0 || end-start < int64(15*time.Millisecond) {
		t.Fatalf("delayed status not attributed: %d ns", end-start)
	}
	if f.mutations.Load() != 0 || f.resets.Load() != 0 {
		t.Fatal("observation changed ownership")
	}
}

func TestObservationTimingEnableCloseRace(t *testing.T) {
	for i := 0; i < 30; i++ {
		s := &Service{}
		dir := t.TempDir()
		_ = os.Chmod(dir, 0700)
		path := filepath.Join(dir, "trace")
		start := make(chan struct{})
		done := make(chan struct{}, 2)
		go func() { <-start; _ = s.EnableObservationTiming(path); done <- struct{}{} }()
		go func() { <-start; s.stopObservationTiming(); done <- struct{}{} }()
		close(start)
		<-done
		<-done
		if err := s.EnableObservationTiming(filepath.Join(filepath.Dir(path), "late")); err == nil {
			t.Fatal("accepted enable after close")
		}
		if rec := s.timing.Load(); rec != nil {
			select {
			case <-rec.done:
			case <-time.After(time.Second):
				t.Fatal("recorder leaked")
			}
		}
	}
}

func TestObservationTimingRejectsUnsafeDirectory(t *testing.T) {
	dir := t.TempDir()
	public := filepath.Join(dir, "public")
	if err := os.Mkdir(public, 0755); err != nil {
		t.Fatal(err)
	}
	s := &Service{}
	if err := s.EnableObservationTiming(filepath.Join(public, "trace")); err == nil {
		t.Fatal("accepted public directory")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableObservationTiming(filepath.Join(link, "trace")); err == nil {
		t.Fatal("accepted symlinked directory")
	}
}

func TestObservationTimingWriteFailureDoesNotBlock(t *testing.T) {
	f, err := os.OpenFile("/dev/full", os.O_WRONLY, 0)
	if err != nil {
		t.Skip("/dev/full unavailable")
	}
	rec := &observationTiming{epoch: time.Now(), events: make(chan timingEvent, 1), stop: make(chan struct{}), done: make(chan struct{})}
	go rec.write(f)
	rec.emit(1, timingHealth, true)
	select {
	case <-rec.done:
	case <-time.After(time.Second):
		t.Fatal("failed writer did not exit")
	}
	rec.emit(1, timingHealth, false)
	rec.emit(1, timingHealth, false)
	if rec.dropped.Load() == 0 {
		t.Fatal("full queue did not report loss")
	}
}

func BenchmarkHealthObservationLocalTarget(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		b.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(b *testing.B) {
			f := newReprobeFixture(b)
			if enabled {
				dir := b.TempDir()
				if err := os.Chmod(dir, 0700); err != nil {
					b.Fatal(err)
				}
				if err := f.service.EnableObservationTiming(filepath.Join(dir, "trace")); err != nil {
					b.Fatal(err)
				}
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := f.service.Health(context.Background()); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			f.service.stopObservationTiming()
		})
	}
}

func TestObservationTimingRetainsOwnershipChecks(t *testing.T) {
	for _, which := range []string{"boot", "lease", "identity"} {
		t.Run(which, func(t *testing.T) {
			f := newReprobeFixture(t)
			dir := t.TempDir()
			if err := os.Chmod(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := f.service.EnableObservationTiming(filepath.Join(dir, "trace")); err != nil {
				t.Fatal(err)
			}
			switch which {
			case "boot":
				f.boot.Store("boot-2")
			case "lease":
				f.leaseGeneration.Store("foreign-grant")
			case "identity":
				f.identity.Store("b54f2c65-cf14-4643-a042-d09825463d44")
			}
			_, err := f.service.Health(context.Background())
			f.service.stopObservationTiming()
			if which == "identity" {
				if err == nil {
					t.Fatal("wrong identity accepted")
				}
			} else if err != nil || f.resets.Load() != 1 {
				t.Fatalf("lost invalidation: err=%v resets=%d", err, f.resets.Load())
			}
			if f.mutations.Load() != 0 {
				t.Fatal("unexpected target mutation")
			}
		})
	}
}

func TestObservationTimingCapIsReported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	rec := &observationTiming{epoch: time.Now(), events: make(chan timingEvent, timingMaxRecords), stop: make(chan struct{}), done: make(chan struct{})}
	for i := 0; i < timingMaxRecords; i++ {
		rec.emit(1, timingHealth, true)
	}
	go rec.write(file)
	select {
	case <-rec.done:
	case <-time.After(5 * time.Second):
		t.Fatal("writer failed to stop at cap")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"truncated":true`) {
		t.Fatal("missing truncation marker")
	}
}

func TestObservationTimingShutdownWithActiveObservationIsIncomplete(t *testing.T) {
	s := &Service{}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "trace")
	if err := s.EnableObservationTiming(path); err != nil {
		t.Fatal(err)
	}
	_, end := s.beginObservation(context.Background(), timingHealth)
	start := time.Now()
	s.stopObservationTiming()
	if time.Since(start) > time.Second {
		t.Fatal("unbounded shutdown")
	}
	rec := s.timing.Load()
	select {
	case <-rec.done:
	case <-time.After(time.Second):
		t.Fatal("writer did not exit")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"active":1`) {
		t.Fatal("active observation missing from summary")
	}
	end()
}

func TestObservationTimingBlockedPreparationDoesNotBlockShutdown(t *testing.T) {
	s := &Service{}
	dir := t.TempDir()
	_ = os.Chmod(dir, 0700)
	entered := make(chan *os.File, 1)
	release := make(chan struct{})
	s.timingOpen = func(path string) (*os.File, error) {
		f, err := openObservationOutput(path)
		if err != nil {
			return nil, err
		}
		entered <- f
		<-release
		return f, nil
	}
	result := make(chan error, 1)
	go func() { result <- s.EnableObservationTiming(filepath.Join(dir, "trace")) }()
	f := <-entered
	finished := make(chan struct{})
	go func() { s.stopObservationTiming(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("shutdown waited for file preparation")
	}
	close(release)
	if err := <-result; err == nil {
		t.Fatal("enabled after shutdown")
	}
	if _, err := f.Stat(); err == nil {
		t.Fatal("losing descriptor not closed")
	}
	if s.timing.Load() != nil {
		t.Fatal("recorder published after shutdown")
	}
}

func TestObservationTimingConcurrentEnableKeepsOneRecorder(t *testing.T) {
	s := &Service{}
	dir := t.TempDir()
	_ = os.Chmod(dir, 0700)
	opened := make(chan *os.File, 2)
	release := make(chan struct{})
	s.timingOpen = func(path string) (*os.File, error) {
		f, err := openObservationOutput(path)
		if err != nil {
			return nil, err
		}
		opened <- f
		<-release
		return f, nil
	}
	results := make(chan error, 2)
	go func() { results <- s.EnableObservationTiming(filepath.Join(dir, "one")) }()
	go func() { results <- s.EnableObservationTiming(filepath.Join(dir, "two")) }()
	first, second := <-opened, <-opened
	close(release)
	a, b := <-results, <-results
	if (a == nil) == (b == nil) {
		t.Fatalf("want exactly one success, got %v %v", a, b)
	}
	s.stopObservationTiming()
	if _, err := first.Stat(); err == nil {
		t.Fatal("first descriptor left open")
	}
	if _, err := second.Stat(); err == nil {
		t.Fatal("second descriptor left open")
	}
}
