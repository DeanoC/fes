package applianceboot

import (
	"context"
	"errors"
	"reflect"
	"testing"

	release "github.com/DeanoC/FogCast/appliance"
	store "github.com/DeanoC/FogCast/internal/appliance"
)

type fixtureStore struct {
	selection store.Selection
	beginErr  error
}

func (s fixtureStore) BeginBoot(string) (store.Selection, error) { return s.selection, s.beginErr }
func (s fixtureStore) Status() (store.Status, error)             { return store.Status{Good: "good"}, nil }
func (s fixtureStore) Verify(hash string) (release.Manifest, error) {
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
	events   []string
	bad      map[string]bool
	guardErr error
}

func (p *fixturePlatform) Prepare(path string) (Root, error) {
	p.events = append(p.events, "prepare:"+path)
	if p.bad[path] {
		return nil, errors.New("invalid init")
	}
	return &fixtureRoot{p, path}, nil
}
func (p *fixturePlatform) Ticket(sel store.Selection) error {
	p.events = append(p.events, "ticket:"+sel.Manifest.ImageSHA256)
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
