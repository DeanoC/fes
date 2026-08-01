// Package hil runs the operator-assisted POC 1A hardware acceptance sequence.
package hil

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/clawzai2-tech/mister-remote/host"
	"github.com/clawzai2-tech/mister-remote/protocol"
)

type API interface {
	Health(context.Context) (protocol.Health, error)
	Status(context.Context) (protocol.Status, error)
	Launch(context.Context, protocol.LaunchRequest) (protocol.Status, error)
	Stop(context.Context) (protocol.Status, error)
}

type Prompter interface {
	Confirm(string) (bool, error)
}

type Check struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

type Report struct {
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	Checks     []Check   `json:"checks"`
	Passed     bool      `json:"passed"`
}

type Runner struct {
	API             API
	UnauthorizedAPI API
	Games           []host.Game
	Prompt          Prompter
	Now             func() time.Time
	Sleep           func(context.Context, time.Duration) error
}

func (r Runner) Run(ctx context.Context) (Report, error) {
	r = r.withDefaults()
	report := Report{StartedAt: r.Now()}
	finish := func(err error) (Report, error) {
		report.FinishedAt = r.Now()
		report.Passed = len(report.Checks) > 0
		for _, check := range report.Checks {
			if !check.Passed {
				report.Passed = false
				break
			}
		}
		return report, err
	}
	record := func(name string, passed bool, detail string) {
		report.Checks = append(report.Checks, Check{Name: name, Passed: passed, Detail: detail})
	}

	mega, snes, ok := selectGames(r.Games)
	record("manifest has exactly two supported games", ok, boolDetail(ok, "one Mega Drive and one SNES game", "manifest selection failed"))
	if !ok || r.API == nil || r.UnauthorizedAPI == nil || r.Prompt == nil {
		return finish(errors.New("invalid HIL runner configuration"))
	}

	healthDeadline := r.Now().Add(45 * time.Second)
	for {
		health, err := r.API.Health(ctx)
		if err == nil && health.Ready {
			record("agent health ready", true, "ready before 45 second deadline")
			break
		}
		if !r.Now().Before(healthDeadline) {
			record("agent health ready", false, "not ready before 45 second deadline")
			return finish(nil)
		}
		if err := r.Sleep(ctx, time.Second); err != nil {
			record("agent health ready", false, "health polling interrupted")
			return finish(err)
		}
	}

	games := []host.Game{mega, snes}
	prompted := map[protocol.System]bool{}
	launchNumber := 0
	for cycle := 0; cycle < 5; cycle++ {
		for _, game := range games {
			launchNumber++
			status, err := r.API.Launch(ctx, protocol.LaunchRequest{GameID: game.ID, System: game.System, ROMPath: game.ROMPath})
			passed := err == nil && activeFor(status, game.System, expectedCore(game.System), false)
			record(fmt.Sprintf("launch %02d %s", launchNumber, systemLabel(game.System)), passed, launchDetail(passed, game.System))
			if !passed {
				return finish(nil)
			}
			if !prompted[game.System] {
				for _, checkName := range manualChecks(game.System) {
					confirmed, err := r.Prompt.Confirm(checkName)
					if err != nil {
						record(checkName, false, "operator prompt failed")
						return finish(err)
					}
					record(checkName, confirmed, boolDetail(confirmed, "operator confirmed", "operator declined"))
				}
				prompted[game.System] = true
			}
		}
	}

	negativeCases := []struct {
		name    string
		api     API
		request protocol.LaunchRequest
		code    protocol.ErrorCode
	}{
		{name: "invalid token", api: r.UnauthorizedAPI, request: protocol.LaunchRequest{GameID: "invalid-token", System: protocol.SystemSNES, ROMPath: snes.ROMPath}, code: protocol.CodeUnauthorized},
		{name: "unsupported system", api: r.API, request: protocol.LaunchRequest{GameID: "unsupported-test", System: "nes", ROMPath: snes.ROMPath}, code: protocol.CodeUnsupportedSystem},
		{name: "invalid extension", api: r.API, request: protocol.LaunchRequest{GameID: "invalid-extension", System: protocol.SystemSNES, ROMPath: "/media/fat/games/SNES/.mister-remote-invalid.txt"}, code: protocol.CodeInvalidROMPath},
		{name: "missing ROM", api: r.API, request: protocol.LaunchRequest{GameID: "missing-rom", System: protocol.SystemSNES, ROMPath: "/media/fat/games/SNES/__mister_remote_missing__.sfc"}, code: protocol.CodeROMNotFound},
		{name: "escaped path", api: r.API, request: protocol.LaunchRequest{GameID: "escaped-rom", System: protocol.SystemSNES, ROMPath: "/media/fat/games/SNES/../../MiSTer"}, code: protocol.CodeInvalidROMPath},
	}
	for _, negative := range negativeCases {
		_, err := negative.api.Launch(ctx, negative.request)
		var apiErr *protocol.APIError
		matched := errors.As(err, &apiErr) && apiErr.Code == negative.code
		record(negative.name+" rejected", matched, errorCodeDetail(matched, negative.code, apiErr))
		status, statusErr := r.API.Status(ctx)
		preserved := statusErr == nil && activeFor(status, protocol.SystemSNES, "SNES", false)
		record("state preserved after "+negative.name, preserved, boolDetail(preserved, "SNES remains active", "active SNES state not preserved"))
	}

	restartPrompt := "Restart mister-agent on the target now. From a development SSH shell, run: killall mister-agent"
	confirmed, err := r.Prompt.Confirm(restartPrompt)
	if err != nil {
		record("restart mister-agent", false, "operator prompt failed")
		return finish(err)
	}
	record("restart mister-agent", confirmed, boolDetail(confirmed, "operator confirmed restart", "operator declined restart"))
	if confirmed {
		restartDeadline := r.Now().Add(10 * time.Second)
		for {
			status, statusErr := r.API.Status(ctx)
			if statusErr == nil && activeFor(status, protocol.SystemSNES, "SNES", true) {
				record("restart reconciliation", true, "SNES rediscovered with no game ID")
				break
			}
			if !r.Now().Before(restartDeadline) {
				record("restart reconciliation", false, "SNES was not rediscovered within 10 seconds")
				break
			}
			if err := r.Sleep(ctx, 250*time.Millisecond); err != nil {
				record("restart reconciliation", false, "status polling interrupted")
				return finish(err)
			}
		}
	} else {
		record("restart reconciliation", false, "restart was not performed")
	}

	status, err := r.API.Stop(ctx)
	idle := err == nil && status.State == protocol.StateIdle
	record("stop observes idle", idle, boolDetail(idle, "Menu loaded and state is idle", "idle state not observed"))
	black, err := r.Prompt.Confirm("Confirm black HDMI output after stop")
	if err != nil {
		record("black HDMI after stop", false, "operator prompt failed")
		return finish(err)
	}
	record("black HDMI after stop", black, boolDetail(black, "operator confirmed", "operator declined"))
	return finish(nil)
}

func (r Runner) withDefaults() Runner {
	if r.Now == nil {
		r.Now = time.Now
	}
	if r.Sleep == nil {
		r.Sleep = func(ctx context.Context, duration time.Duration) error {
			timer := time.NewTimer(duration)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		}
	}
	return r
}

func selectGames(games []host.Game) (host.Game, host.Game, bool) {
	var mega, snes host.Game
	megaCount, snesCount := 0, 0
	for _, game := range games {
		switch game.System {
		case protocol.SystemMegaDrive:
			mega, megaCount = game, megaCount+1
		case protocol.SystemSNES:
			snes, snesCount = game, snesCount+1
		}
	}
	return mega, snes, len(games) == 2 && megaCount == 1 && snesCount == 1
}

func activeFor(status protocol.Status, system protocol.System, core string, requireNilGameID bool) bool {
	if status.State != protocol.StateActive || status.System == nil || *status.System != system || status.ObservedCore == nil || *status.ObservedCore != core {
		return false
	}
	return !requireNilGameID || status.GameID == nil
}

func expectedCore(system protocol.System) string {
	if system == protocol.SystemMegaDrive {
		return "MegaDrive"
	}
	return "SNES"
}

func systemLabel(system protocol.System) string {
	if system == protocol.SystemMegaDrive {
		return "Mega Drive"
	}
	return "SNES"
}

func manualChecks(system protocol.System) []string {
	label := systemLabel(system)
	return []string{
		label + " HDMI video",
		label + " HDMI audio",
		label + " playable screen",
		label + " controller operation",
	}
}

func launchDetail(passed bool, system protocol.System) string {
	if passed {
		return expectedCore(system) + " active"
	}
	return systemLabel(system) + " launch did not become active"
}

func boolDetail(passed bool, yes, no string) string {
	if passed {
		return yes
	}
	return no
}

func errorCodeDetail(matched bool, expected protocol.ErrorCode, actual *protocol.APIError) string {
	if matched {
		return "received " + string(expected)
	}
	if actual != nil && actual.Code != "" {
		return "expected " + string(expected) + ", received " + string(actual.Code)
	}
	return "expected " + string(expected) + ", received no API error code"
}
