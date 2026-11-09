package applianceboot

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	release "github.com/DeanoC/FogCast/appliance"
	store "github.com/DeanoC/FogCast/appliance/store"
)

type fixtureStore struct {
	selection store.Selection
	beginErr  error
	corrupt   bool
	fallback  store.Selection
	good      string
	previous  string
	verifyBad map[string]bool
}

func (s fixtureStore) BeginBoot(string) (store.Selection, error) { return s.selection, s.beginErr }
func (s fixtureStore) Status() (store.Status, error) {
	good := s.good
	if good == "" {
		good = "good"
	}
	return store.Status{Good: good, Previous: s.previous, Corrupt: s.corrupt}, nil
}
func (s fixtureStore) LastFallbackSelection(string) (store.Selection, error) {
	if s.fallback.Manifest.ImageSHA256 == "" {
		return store.Selection{}, errors.New("no fallback")
	}
	return s.fallback, nil
}
func (s fixtureStore) Verify(hash string) (release.Manifest, error) {
	if s.verifyBad[hash] {
		return release.Manifest{}, errors.New("invalid image")
	}
	return release.Manifest{ImageSHA256: hash}, nil
}
func (s fixtureStore) ImagePath(hash string) (string, error)                       { return hash, nil }
func (s fixtureStore) RejectTrialContext(context.Context, string, string) error    { return nil }
func (s fixtureStore) RecordFallbackContext(context.Context, string, string) error { return nil }

type fixtureRoot struct {
	p    *fixturePlatform
	hash string
}

func (r *fixtureRoot) Close() error { r.p.events = append(r.p.events, "close:"+r.hash); return nil }
func (r *fixtureRoot) Exec() error {
	r.p.events = append(r.p.events, "exec:"+r.hash)
	return errors.New("exec failed")
}

type fixturePlatform struct {
	events        []string
	bad           map[string]bool
	guardErr      error
	ticket        store.Selection
	ticketPath    string
	beforePrepare func(string)
}

func (p *fixturePlatform) Prepare(path string) (Root, error) {
	p.events = append(p.events, "prepare:"+path)
	if p.beforePrepare != nil {
		p.beforePrepare(path)
	}
	if p.bad[path] {
		return nil, errors.New("invalid init")
	}
	return &fixtureRoot{p, path}, nil
}
func (p *fixturePlatform) Ticket(sel store.Selection) error {
	p.events = append(p.events, "ticket:"+sel.Manifest.ImageSHA256)
	p.ticket = sel
	if p.ticketPath != "" {
		return store.WriteBootTicket(p.ticketPath, sel.BootID, sel.Manifest.ImageSHA256, sel.Trial, sel.FallbackImageSHA256)
	}
	return nil
}
func (p *fixturePlatform) Arm(context.Context, store.Selection) error {
	p.events = append(p.events, "arm")
	return p.guardErr
}

func TestInvalidCandidateFallsBackBeforeArming(t *testing.T) {
	p := &fixturePlatform{bad: map[string]bool{"candidate": true}}
	s := fixtureStore{selection: store.Selection{Manifest: release.Manifest{ImageSHA256: "candidate"}, Path: "candidate", Trial: true}}
	if Run(context.Background(), s, "factory", "boot", p) == nil {
		t.Fatal("exec returning must be fatal")
	}
	want := []string{"prepare:candidate", "prepare:good", "ticket:good", "exec:good", "close:good"}
	if !reflect.DeepEqual(p.events, want) {
		t.Fatalf("events %v want %v", p.events, want)
	}
	if p.ticket.FallbackImageSHA256 != "factory" {
		t.Fatalf("fallback ticket = %q, want factory", p.ticket.FallbackImageSHA256)
	}
}

type recordingTrialStore struct {
	fixtureStore
	rejected  []string
	rejectErr error
	events    *[]string
}

func (s *recordingTrialStore) RejectTrialContext(_ context.Context, bootID, image string) error {
	s.rejected = append(s.rejected, bootID+":"+image)
	*s.events = append(*s.events, "reject:"+bootID+":"+image)
	return s.rejectErr
}

// The bootstrap must record a rejected trial before booting the old image.
// An uncertain state write must not let it silently proceed with fallback.
func TestRejectedTrialIsRecordedBeforeFallback(t *testing.T) {
	for _, tc := range []struct {
		name       string
		writeErr   error
		wantEvents []string
	}{
		{"recorded", nil, []string{"prepare:candidate", "reject:boot-1:candidate", "prepare:good", "ticket:good", "exec:good", "close:good"}},
		{"record failure", errors.New("record failed"), []string{"prepare:candidate", "reject:boot-1:candidate"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &fixturePlatform{bad: map[string]bool{"candidate": true}}
			s := &recordingTrialStore{fixtureStore: fixtureStore{selection: store.Selection{Manifest: release.Manifest{ImageSHA256: "candidate"}, Path: "candidate", Trial: true}}, rejectErr: tc.writeErr, events: &p.events}
			err := Run(context.Background(), s, "factory", "boot-1", p)
			if err == nil {
				t.Fatal("a returned init or failed rejection must be fatal")
			}
			if !reflect.DeepEqual(s.rejected, []string{"boot-1:candidate"}) || !reflect.DeepEqual(p.events, tc.wantEvents) {
				t.Fatalf("rejected=%v events=%v, want %v", s.rejected, p.events, tc.wantEvents)
			}
			if tc.writeErr != nil && !errors.Is(err, tc.writeErr) {
				t.Fatalf("rejection failure lost: %v", err)
			}
		})
	}
}
func TestStableSelectionFallsBackToPreviousBeforeFactory(t *testing.T) {
	p := &fixturePlatform{bad: map[string]bool{"good": true}}
	s := fixtureStore{selection: store.Selection{
		Manifest:            release.Manifest{ImageSHA256: "good"},
		Path:                "good",
		FallbackImageSHA256: "previous",
	}}
	_ = Run(context.Background(), s, "factory", "boot", p)
	want := []string{"prepare:good", "prepare:previous", "ticket:previous", "exec:previous", "close:previous"}
	if !reflect.DeepEqual(p.events, want) {
		t.Fatalf("events %v want %v", p.events, want)
	}
	if p.ticket.FallbackImageSHA256 != "factory" {
		t.Fatalf("fallback ticket = %q, want factory", p.ticket.FallbackImageSHA256)
	}
}

func TestTrialWithUnverifiableGoodFallsBackToPreviousBeforeFactory(t *testing.T) {
	good := strings.Repeat("a", 64)
	previous := strings.Repeat("b", 64)
	trial := strings.Repeat("c", 64)
	factory := strings.Repeat("d", 64)
	p := &fixturePlatform{bad: map[string]bool{trial: true}}
	s := fixtureStore{
		selection: store.Selection{
			Manifest:            release.Manifest{ImageSHA256: trial},
			Path:                trial,
			Trial:               true,
			FallbackImageSHA256: good,
		},
		good:      good,
		previous:  previous,
		verifyBad: map[string]bool{good: true},
	}
	_ = Run(context.Background(), s, factory, "boot", p)
	want := []string{"prepare:" + trial, "prepare:" + previous, "ticket:" + previous, "exec:" + previous, "close:" + previous}
	if !reflect.DeepEqual(p.events, want) {
		t.Fatalf("events %v want %v", p.events, want)
	}
	if p.ticket.Manifest.ImageSHA256 != previous || p.ticket.FallbackImageSHA256 != factory {
		t.Fatalf("fallback ticket = %+v, want previous with factory fallback", p.ticket)
	}
}

func TestTrialArmsBeforeExecAndNeverExecutesSecondRootAfterExecError(t *testing.T) {
	p := &fixturePlatform{}
	s := fixtureStore{selection: store.Selection{Manifest: release.Manifest{ImageSHA256: "candidate"}, Path: "candidate", Trial: true}}
	_ = Run(context.Background(), s, "factory", "boot", p)
	want := []string{"prepare:candidate", "ticket:candidate", "arm", "exec:candidate", "close:candidate"}
	if !reflect.DeepEqual(p.events, want) {
		t.Fatalf("events %v want %v", p.events, want)
	}
}
func TestFailedGuardDoesNotExecuteCandidate(t *testing.T) {
	p := &fixturePlatform{guardErr: errors.New("no watchdog")}
	s := fixtureStore{selection: store.Selection{Manifest: release.Manifest{ImageSHA256: "candidate"}, Path: "candidate", Trial: true}}
	_ = Run(context.Background(), s, "factory", "boot", p)
	want := []string{"prepare:candidate", "ticket:candidate", "arm", "close:candidate"}
	if !reflect.DeepEqual(p.events, want) {
		t.Fatalf("events %v want %v", p.events, want)
	}
}
func TestBrokenSelectionStateAndGoodImageStillTryFactory(t *testing.T) {
	p := &fixturePlatform{bad: map[string]bool{"good": true}}
	_ = Run(context.Background(), fixtureStore{beginErr: errors.New("write failed")}, "factory", "boot", p)
	want := []string{"prepare:good", "prepare:factory", "ticket:factory", "exec:factory", "close:factory"}
	if !reflect.DeepEqual(p.events, want) {
		t.Fatalf("events %v want %v", p.events, want)
	}
}

func TestCorruptSelectionUsesTheFallbackRecordedWithTheLastBoot(t *testing.T) {
	p := &fixturePlatform{}
	s := fixtureStore{
		selection: store.Selection{Manifest: release.Manifest{ImageSHA256: "factory"}, Path: "factory"},
		corrupt:   true,
		fallback:  store.Selection{Manifest: release.Manifest{ImageSHA256: "last-good"}, Path: "last-good"},
	}
	_ = Run(context.Background(), s, "factory", "boot", p)
	want := []string{"prepare:last-good", "ticket:last-good", "exec:last-good", "close:last-good"}
	if !reflect.DeepEqual(p.events, want) {
		t.Fatalf("events %v want %v", p.events, want)
	}
}
