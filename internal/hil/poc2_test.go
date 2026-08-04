package hil_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast-POC/catalog"
	"github.com/DeanoC/FogCast-POC/fogcast"
	"github.com/DeanoC/FogCast-POC/internal/hil"
	"github.com/DeanoC/FogCast-POC/protocol"
)

type poc2FakeService struct {
	events             []string
	games              []catalog.Game
	cache              map[string]bool
	offline            bool
	ready              bool
	scan               catalog.ScanReport
	wrongUncachedError bool
	restarted          bool
	uploadEntered      chan struct{}
}

func (s *poc2FakeService) Scan(context.Context) (catalog.ScanReport, error) {
	s.events = append(s.events, "scan")
	return s.scan, nil
}
func (s *poc2FakeService) Games(context.Context) ([]catalog.Game, error) {
	s.events = append(s.events, "games")
	return append([]catalog.Game(nil), s.games...), nil
}
func (s *poc2FakeService) Launch(ctx context.Context, gameID string, progress fogcast.ProgressFunc) (protocol.CachedLaunchResponse, error) {
	s.events = append(s.events, "launch:"+gameID)
	if gameID == "invalid request" {
		return protocol.CachedLaunchResponse{}, &protocol.APIError{Code: protocol.CodeBadRequest, Message: "invalid request"}
	}
	if gameID == "wrong-uncached-error" {
		return protocol.CachedLaunchResponse{}, &protocol.APIError{Code: protocol.CodeInternal, Message: "wrong error"}
	}
	if s.offline && !s.cache[gameID] {
		return protocol.CachedLaunchResponse{}, &protocol.APIError{Code: protocol.CodeSourceUnavailable, Message: "source unavailable"}
	}
	if progress != nil && !s.cache[gameID] {
		progress(fogcast.Progress{Stage: "upload", Message: "content uploaded"})
	}
	if gameID == "interrupted-test" && s.uploadEntered != nil {
		s.events = append(s.events, "upload request entered")
		close(s.uploadEntered)
		<-ctx.Done()
		return protocol.CachedLaunchResponse{}, &protocol.APIError{Code: protocol.CodeTransferFailed, Message: "upload interrupted"}
	}
	if err := ctx.Err(); err != nil {
		return protocol.CachedLaunchResponse{}, &protocol.APIError{Code: protocol.CodeTransferFailed, Message: "upload interrupted"}
	}
	s.cache[gameID] = true
	return protocol.CachedLaunchResponse{Content: protocol.ContentIdentity{SHA256: strings.Repeat("a", 64), Size: 1, Extension: "sfc"}, Status: protocol.Status{State: protocol.StateActive}}, nil
}

func (s *poc2FakeService) Health(context.Context) (protocol.Health, error) {
	s.events = append(s.events, "health")
	return protocol.Health{Ready: s.ready}, nil
}
func (s *poc2FakeService) Status(context.Context) (protocol.Status, error) {
	s.events = append(s.events, "status")
	if s.restarted {
		return protocol.Status{State: protocol.StateIdle}, nil
	}
	return protocol.Status{State: protocol.StateActive}, nil
}
func (s *poc2FakeService) Stop(context.Context) (protocol.Status, error) {
	s.events = append(s.events, "stop")
	return protocol.Status{State: protocol.StateIdle}, nil
}

type poc2FakeSabotage struct {
	events    []string
	interrupt bool
	service   *poc2FakeService
}

func (s *poc2FakeSabotage) RebootTarget(context.Context) (bool, error) {
	s.events = append(s.events, "target reboot")
	return true, nil
}
func (s *poc2FakeSabotage) RestartAgent(context.Context) (bool, error) {
	s.events = append(s.events, "agent restart")
	if s.service != nil {
		s.service.restarted = true
	}
	return true, nil
}
func (s *poc2FakeSabotage) ToggleNAS(context.Context) (bool, error) {
	s.events = append(s.events, "nas offline")
	if s.service != nil {
		s.service.offline = true
	}
	return true, nil
}
func (s *poc2FakeSabotage) RemountNAS(context.Context) (bool, error) {
	s.events = append(s.events, "nas online")
	if s.service != nil {
		s.service.offline = false
	}
	return true, nil
}
func (s *poc2FakeSabotage) InterruptUpload(context.Context) (bool, error) {
	s.events = append(s.events, "upload interruption")
	if s.service != nil {
		if s.service.uploadEntered != nil {
			<-s.service.uploadEntered
		}
		s.service.events = append(s.service.events, "upload interruption")
	}
	return s.interrupt, nil
}

type poc2FakePrompter struct{ events []string }

func (p *poc2FakePrompter) Confirm(message string) (bool, error) {
	p.events = append(p.events, message)
	return true, nil
}

func TestPOC2RunnerFollowsSafeAcceptanceOrder(t *testing.T) {
	now := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	service := &poc2FakeService{
		cache:         map[string]bool{},
		ready:         true,
		uploadEntered: make(chan struct{}),
		scan:          catalog.ScanReport{Roots: []catalog.RootReport{{RootID: "mega-root", System: protocol.SystemMegaDrive}, {RootID: "snes-root", System: protocol.SystemSNES}}},
		games: []catalog.Game{
			{ID: "sonic-test", System: protocol.SystemMegaDrive, Kind: catalog.SourceKindZIP, State: catalog.SourceStateAvailable, RootOnline: true},
			{ID: "mario-test", System: protocol.SystemSNES, Kind: catalog.SourceKindZIP, State: catalog.SourceStateAvailable, RootOnline: true},
		},
	}
	sabotage := &poc2FakeSabotage{interrupt: true, service: service}
	prompt := &poc2FakePrompter{}
	runner := hil.POC2Runner{
		Service: service, Prompt: prompt, Sabotage: sabotage,
		SonicID: "sonic-test", MarioID: "mario-test",
		UncachedID:    "uncached-test",
		InterruptedID: "interrupted-test",
		Now:           func() time.Time { return now },
		Sleep:         func(context.Context, time.Duration) error { return nil },
	}
	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed {
		t.Fatalf("report failed: %#v", report)
	}
	wantPrefix := []string{"scan", "games", "health", "launch:sonic-test", "launch:mario-test", "launch:sonic-test", "launch:mario-test"}
	if len(service.events) < len(wantPrefix) || !reflect.DeepEqual(service.events[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("service events = %v, want prefix %v", service.events, wantPrefix)
	}
	if len(sabotage.events) == 0 {
		t.Fatal("sabotage actions were not requested")
	}
	for _, name := range []string{"first-transfer", "repeat", "reboot", "offline", "interrupted", "artifact audit"} {
		found := false
		for _, check := range report.Checks {
			if strings.Contains(check.Name, name) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing check containing %q: %#v", name, report.Checks)
		}
	}
}

func TestPOC2RunnerStopsAfterFailedCheck(t *testing.T) {
	service := &poc2FakeService{cache: map[string]bool{}, ready: true, scan: catalog.ScanReport{Roots: []catalog.RootReport{{RootID: "mega-root", System: protocol.SystemMegaDrive}, {RootID: "snes-root", System: protocol.SystemSNES}}}, games: []catalog.Game{{ID: "sonic-test", System: protocol.SystemMegaDrive}}}
	prompt := &poc2FakePrompter{}
	sabotage := &poc2FakeSabotage{}
	runner := hil.POC2Runner{Service: service, Prompt: prompt, Sabotage: sabotage, SonicID: "sonic-test", MarioID: "mario-test", UncachedID: "uncached-test", InterruptedID: "interrupted-test"}
	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed {
		t.Fatal("runner passed with missing game")
	}
	if len(service.events) > 2 {
		t.Fatalf("unsafe actions after failed setup: %v", service.events)
	}
}

func TestPOC2RunnerStopsWhenTargetNeverBecomesReady(t *testing.T) {
	now := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	service := &poc2FakeService{cache: map[string]bool{}, scan: catalog.ScanReport{Roots: []catalog.RootReport{{RootID: "mega-root", System: protocol.SystemMegaDrive}, {RootID: "snes-root", System: protocol.SystemSNES}}}, games: []catalog.Game{
		{ID: "sonic-test", System: protocol.SystemMegaDrive, Kind: catalog.SourceKindZIP, State: catalog.SourceStateAvailable, RootOnline: true},
		{ID: "mario-test", System: protocol.SystemSNES, Kind: catalog.SourceKindZIP, State: catalog.SourceStateAvailable, RootOnline: true},
	}}
	runner := hil.POC2Runner{
		Service: service, Prompt: &poc2FakePrompter{}, Sabotage: &poc2FakeSabotage{}, SonicID: "sonic-test", MarioID: "mario-test", UncachedID: "uncached-test", InterruptedID: "interrupted-test",
		Now: func() time.Time { return now }, Sleep: func(_ context.Context, d time.Duration) error { now = now.Add(d); return nil },
	}
	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed {
		t.Fatal("runner passed without target readiness")
	}
	for _, event := range service.events {
		if strings.HasPrefix(event, "launch:") {
			t.Fatalf("launch occurred after failed health gate: %v", service.events)
		}
	}
}

func TestPOC2RunnerRejectsMissingDependencies(t *testing.T) {
	_, err := (hil.POC2Runner{}).Run(context.Background())
	if err == nil || !errors.Is(err, hil.ErrInvalidRunner) {
		t.Fatalf("error = %v", err)
	}
}

func TestPOC2RunnerRequiresExplicitUncachedAndInterruptedIDs(t *testing.T) {
	service := &poc2FakeService{}
	runner := hil.POC2Runner{Service: service, Prompt: &poc2FakePrompter{}, Sabotage: &poc2FakeSabotage{}, SonicID: "sonic-test", MarioID: "mario-test"}
	_, err := runner.Run(context.Background())
	if !errors.Is(err, hil.ErrInvalidRunner) {
		t.Fatalf("error = %v, want %v", err, hil.ErrInvalidRunner)
	}
}

func TestPOC2RunnerRejectsWrongUncachedErrorAndPreservesStatus(t *testing.T) {
	service := &poc2FakeService{
		cache: map[string]bool{}, ready: true, wrongUncachedError: true,
		scan: catalog.ScanReport{Roots: []catalog.RootReport{{RootID: "mega-root", System: protocol.SystemMegaDrive}, {RootID: "snes-root", System: protocol.SystemSNES}}},
		games: []catalog.Game{
			{ID: "sonic-test", System: protocol.SystemMegaDrive, Kind: catalog.SourceKindZIP, State: catalog.SourceStateAvailable, RootOnline: true},
			{ID: "mario-test", System: protocol.SystemSNES, Kind: catalog.SourceKindZIP, State: catalog.SourceStateAvailable, RootOnline: true},
		},
	}
	runner := hil.POC2Runner{Service: service, Prompt: &poc2FakePrompter{}, Sabotage: &poc2FakeSabotage{service: service}, SonicID: "sonic-test", MarioID: "mario-test", UncachedID: "wrong-uncached-error", InterruptedID: "interrupted-test"}
	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed {
		t.Fatal("runner accepted wrong uncached error")
	}
	for _, event := range service.events {
		if event == "upload interruption" || event == "launch:interrupted-test" {
			t.Fatalf("interruption started after wrong error: %v", service.events)
		}
	}
}

func TestPOC2RunnerRequiresOnlineAvailableScanRoots(t *testing.T) {
	service := &poc2FakeService{
		cache: map[string]bool{}, ready: true,
		scan: catalog.ScanReport{Roots: []catalog.RootReport{{RootID: "mega-root", System: protocol.SystemMegaDrive, Offline: true}, {RootID: "snes-root", System: protocol.SystemSNES}}},
		games: []catalog.Game{
			{ID: "sonic-test", System: protocol.SystemMegaDrive, Kind: catalog.SourceKindZIP, State: catalog.SourceStateAvailable, RootOnline: true},
			{ID: "mario-test", System: protocol.SystemSNES, Kind: catalog.SourceKindZIP, State: catalog.SourceStateAvailable, RootOnline: true},
		},
	}
	runner := hil.POC2Runner{Service: service, Prompt: &poc2FakePrompter{}, Sabotage: &poc2FakeSabotage{}, SonicID: "sonic-test", MarioID: "mario-test", UncachedID: "uncached-test", InterruptedID: "interrupted-test"}
	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed {
		t.Fatal("runner accepted offline scan root")
	}
	if slices.Contains(service.events, "games") {
		t.Fatal("game launches proceeded after offline scan root")
	}
}

func TestPOC2RunnerRejectsRawRequestedGame(t *testing.T) {
	service := &poc2FakeService{
		cache: map[string]bool{}, ready: true,
		scan: catalog.ScanReport{Roots: []catalog.RootReport{{RootID: "mega-root", System: protocol.SystemMegaDrive}, {RootID: "snes-root", System: protocol.SystemSNES}}},
		games: []catalog.Game{
			{ID: "sonic-test", System: protocol.SystemMegaDrive, Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, RootOnline: true},
			{ID: "mario-test", System: protocol.SystemSNES, Kind: catalog.SourceKindZIP, State: catalog.SourceStateAvailable, RootOnline: true},
		},
	}
	runner := hil.POC2Runner{Service: service, Prompt: &poc2FakePrompter{}, Sabotage: &poc2FakeSabotage{}, SonicID: "sonic-test", MarioID: "mario-test", UncachedID: "uncached-test", InterruptedID: "interrupted-test"}
	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed {
		t.Fatal("runner accepted a raw requested game")
	}
	if slices.Contains(service.events, "health") {
		t.Fatalf("health proceeded after raw game rejection: %v", service.events)
	}
}

func TestPOC2RunnerInterruptsOnlyAfterUploadStarts(t *testing.T) {
	service := &poc2FakeService{
		cache: map[string]bool{}, ready: true,
		uploadEntered: make(chan struct{}),
		scan:          catalog.ScanReport{Roots: []catalog.RootReport{{RootID: "mega-root", System: protocol.SystemMegaDrive}, {RootID: "snes-root", System: protocol.SystemSNES}}},
		games: []catalog.Game{
			{ID: "sonic-test", System: protocol.SystemMegaDrive, Kind: catalog.SourceKindZIP, State: catalog.SourceStateAvailable, RootOnline: true},
			{ID: "mario-test", System: protocol.SystemSNES, Kind: catalog.SourceKindZIP, State: catalog.SourceStateAvailable, RootOnline: true},
		},
	}
	sabotage := &poc2FakeSabotage{service: service, interrupt: true}
	runner := hil.POC2Runner{Service: service, Prompt: &poc2FakePrompter{}, Sabotage: sabotage, SonicID: "sonic-test", MarioID: "mario-test", UncachedID: "uncached-test", InterruptedID: "interrupted-test"}
	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed {
		t.Fatalf("report failed: %#v", report)
	}
	start := slices.Index(service.events, "launch:interrupted-test")
	entered := slices.Index(service.events, "upload request entered")
	interrupt := slices.Index(service.events, "upload interruption")
	if start < 0 || entered < 0 || interrupt < 0 {
		t.Fatalf("interruption order = %v", service.events)
	}
	if entered <= start || interrupt <= entered {
		t.Fatalf("interruption happened before upload started: %v", service.events)
	}
}

func TestPOC2ReportContainsNoOperationalSecrets(t *testing.T) {
	report := hil.Report{StartedAt: time.Unix(1, 0).UTC(), FinishedAt: time.Unix(2, 0).UTC(), Checks: []hil.Check{{Name: "scan", Passed: true, Detail: "ok"}}, Passed: true}
	encoded := fmt.Sprintf("%+v", report)
	for _, forbidden := range []string{"Bearer ", "/Volumes/", "sqlite3", ".sfc", ".smc", ".gen", ".md", ".zip"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("report leaked %q: %s", forbidden, encoded)
		}
	}
}
