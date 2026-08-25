package hil_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast-POC/host"
	"github.com/DeanoC/FogCast-POC/internal/hil"
	"github.com/DeanoC/FogCast-POC/protocol"
)

type fakeAPI struct {
	healthCalls  int
	readyAfter   int
	status       protocol.Status
	launches     []protocol.LaunchRequest
	launchResult map[protocol.System]protocol.Status
	errors       map[string]*protocol.APIError
}

func (a *fakeAPI) Health(context.Context) (protocol.Health, error) {
	a.healthCalls++
	return protocol.Health{Ready: a.healthCalls >= a.readyAfter}, nil
}

func (a *fakeAPI) Status(context.Context) (protocol.Status, error) { return a.status, nil }

func (a *fakeAPI) Launch(_ context.Context, request protocol.LaunchRequest) (protocol.Status, error) {
	a.launches = append(a.launches, request)
	if protocol.ValidateSystem(request.System) != nil {
		return protocol.Status{}, &protocol.APIError{Code: protocol.CodeUnsupportedSystem, Message: "unsupported"}
	}
	if apiErr := a.errors[request.GameID]; apiErr != nil {
		return protocol.Status{}, apiErr
	}
	if status, ok := a.launchResult[request.System]; ok {
		return status, nil
	}
	return protocol.Status{}, errors.New("unexpected launch")
}

func (a *fakeAPI) Stop(context.Context) (protocol.Status, error) {
	return protocol.Status{State: protocol.StateIdle}, nil
}

type yesPrompter struct{ prompts []string }

func (p *yesPrompter) Confirm(message string) (bool, error) {
	p.prompts = append(p.prompts, message)
	return true, nil
}

type selectivePrompter struct {
	yesPrompter
	decline string
}

func (p *selectivePrompter) Confirm(message string) (bool, error) {
	p.prompts = append(p.prompts, message)
	return message != p.decline, nil
}

func TestRunnerPassesCompletePOC1ASequence(t *testing.T) {
	t.Parallel()
	api, unauthorized, games := acceptanceFixture()
	prompt := &yesPrompter{}
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	runner := hil.Runner{
		API:             api,
		UnauthorizedAPI: unauthorized,
		Games:           games,
		Prompt:          prompt,
		Now:             func() time.Time { return now },
		Sleep: func(_ context.Context, duration time.Duration) error {
			now = now.Add(duration)
			return nil
		},
	}
	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || len(report.Checks) == 0 {
		t.Fatalf("report = %#v", report)
	}
	duration := report.FinishedAt.Sub(report.StartedAt)
	if api.healthCalls != 2 || duration < 13*time.Second || duration >= 45*time.Second {
		t.Fatalf("health calls = %d, duration = %v", api.healthCalls, report.FinishedAt.Sub(report.StartedAt))
	}
	var gotSystems []protocol.System
	for _, launch := range api.launches {
		if launch.GameID == "megadrive-test" || launch.GameID == "snes-test" {
			gotSystems = append(gotSystems, launch.System)
		}
	}
	wantSystems := []protocol.System{
		protocol.SystemMegaDrive, protocol.SystemSNES,
		protocol.SystemMegaDrive, protocol.SystemSNES,
		protocol.SystemMegaDrive, protocol.SystemSNES,
		protocol.SystemMegaDrive, protocol.SystemSNES,
		protocol.SystemMegaDrive, protocol.SystemSNES,
	}
	if !slices.Equal(gotSystems, wantSystems) {
		t.Fatalf("launch order = %v, want %v", gotSystems, wantSystems)
	}
	for _, fragment := range []string{
		"Mega Drive HDMI video", "Mega Drive HDMI audio", "Mega Drive playable screen", "Mega Drive controller",
		"SNES HDMI video", "SNES HDMI audio", "SNES playable screen", "SNES controller",
		"Restart mister-agent", "black HDMI",
	} {
		if countContaining(prompt.prompts, fragment) != 1 {
			t.Errorf("prompt containing %q count = %d; prompts = %v", fragment, countContaining(prompt.prompts, fragment), prompt.prompts)
		}
	}
	for _, name := range []string{
		"invalid token rejected", "unsupported system rejected", "invalid extension rejected", "missing ROM rejected", "escaped path rejected",
		"state preserved after invalid token", "state preserved after unsupported system", "state preserved after invalid extension", "state preserved after missing ROM", "state preserved after escaped path",
		"restart reconciliation", "stop observes idle", "black HDMI after stop",
	} {
		if !hasPassingCheck(report, name) {
			t.Errorf("missing passing check %q: %#v", name, report.Checks)
		}
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("test-token")) || bytes.Contains(encoded, []byte("/media/fat/games")) {
		t.Fatalf("report leaked a token or ROM path: %s", encoded)
	}
}

func TestRunnerRecordsDeclinedManualCheck(t *testing.T) {
	t.Parallel()
	api, unauthorized, games := acceptanceFixture()
	prompt := &selectivePrompter{decline: "SNES HDMI audio"}
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	report, err := (hil.Runner{
		API: api, UnauthorizedAPI: unauthorized, Games: games, Prompt: prompt,
		Now:   func() time.Time { return now },
		Sleep: func(_ context.Context, duration time.Duration) error { now = now.Add(duration); return nil },
	}).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed {
		t.Fatal("report passed after a declined manual check")
	}
	for _, check := range report.Checks {
		if check.Name == "SNES HDMI audio" && !check.Passed {
			return
		}
	}
	t.Fatalf("declined check not recorded: %#v", report.Checks)
}

func acceptanceFixture() (*fakeAPI, *fakeAPI, []host.Game) {
	megaSystem, snesSystem := protocol.SystemMegaDrive, protocol.SystemSNES
	megaCore, snesCore := "MegaDrive", "SNES"
	api := &fakeAPI{
		readyAfter: 2,
		status: protocol.Status{
			State: protocol.StateActive, System: &snesSystem, ExpectedCore: &snesCore, ObservedCore: &snesCore,
		},
		launchResult: map[protocol.System]protocol.Status{
			protocol.SystemMegaDrive: {State: protocol.StateActive, System: &megaSystem, ExpectedCore: &megaCore, ObservedCore: &megaCore},
			protocol.SystemSNES:      {State: protocol.StateActive, System: &snesSystem, ExpectedCore: &snesCore, ObservedCore: &snesCore},
		},
		errors: map[string]*protocol.APIError{
			"invalid-extension": {Code: protocol.CodeInvalidROMPath, Message: "invalid"},
			"missing-rom":       {Code: protocol.CodeROMNotFound, Message: "missing"},
			"escaped-rom":       {Code: protocol.CodeInvalidROMPath, Message: "escaped"},
		},
	}
	unauthorized := &fakeAPI{
		errors: map[string]*protocol.APIError{
			"invalid-token": {Code: protocol.CodeUnauthorized, Message: "token test-token invalid"},
		},
	}
	games := []host.Game{
		{ID: "megadrive-test", Title: "Mega Drive test game", System: protocol.SystemMegaDrive, ROMPath: "/media/fat/games/MegaDrive/test.md"},
		{ID: "snes-test", Title: "SNES test game", System: protocol.SystemSNES, ROMPath: "/media/fat/games/SNES/test.sfc"},
	}
	return api, unauthorized, games
}

func countContaining(values []string, fragment string) int {
	count := 0
	for _, value := range values {
		if strings.Contains(value, fragment) {
			count++
		}
	}
	return count
}

func hasPassingCheck(report hil.Report, name string) bool {
	for _, check := range report.Checks {
		if check.Name == name && check.Passed {
			return true
		}
	}
	return false
}
