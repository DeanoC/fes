package hil

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/DeanoC/FogCast-POC/catalog"
	"github.com/DeanoC/FogCast-POC/fogcast"
	"github.com/DeanoC/FogCast-POC/protocol"
)

// ErrInvalidRunner indicates that a POC 2 run was not given all of its
// operator-facing dependencies. No target operation is attempted in this case.
var ErrInvalidRunner = errors.New("invalid POC 2 HIL runner configuration")

// POC2Service is the host-side operation surface used by the operator-assisted
// acceptance sequence. fogcast.Service implements this interface directly.
// The runner deliberately uses only public service operations; target reboot,
// agent restart, share changes, and upload interruption are delegated to the
// explicit POC2Sabotage implementation below.
type POC2Service interface {
	Scan(context.Context) (catalog.ScanReport, error)
	Games(context.Context) ([]catalog.Game, error)
	Launch(context.Context, string, fogcast.ProgressFunc) (protocol.CachedLaunchResponse, error)
	Health(context.Context) (protocol.Health, error)
	Status(context.Context) (protocol.Status, error)
	Stop(context.Context) (protocol.Status, error)
}

// POC2Sabotage represents actions that must be performed by an operator on
// the dedicated target. Implementations must ask for confirmation and carry
// out no action when confirmation is declined. The runner never invokes SSH,
// mount, umount, reboot, kill, or upload-interruption commands itself.
type POC2Sabotage interface {
	RebootTarget(context.Context) (bool, error)
	RestartAgent(context.Context) (bool, error)
	ToggleNAS(context.Context) (bool, error)
	InterruptUpload(context.Context) (bool, error)
}

// POC2NASRemounter is implemented by sabotage controls that also require an
// explicit confirmation after the offline cache gates to restore the source
// mounts. It is optional for synthetic fakes; the production terminal control
// implements it.
type POC2NASRemounter interface {
	RemountNAS(context.Context) (bool, error)
}

// POC2Runner executes the software-observable portion of POC 2 hardware
// acceptance. SonicID and MarioID are intentionally supplied by the operator;
// no source names or paths are embedded in the binary. UncachedID and
// InterruptedID may be supplied by tests or a manifest with a third fixture;
// when omitted, the corresponding primary game is used.
type POC2Runner struct {
	Service  POC2Service
	Prompt   Prompter
	Sabotage POC2Sabotage
	Now      func() time.Time
	Sleep    func(context.Context, time.Duration) error

	SonicID       string
	MarioID       string
	UncachedID    string
	InterruptedID string
}

func (r POC2Runner) Run(ctx context.Context) (Report, error) {
	r = r.withDefaults()
	report := Report{StartedAt: r.Now()}
	finish := func(runErr error) (Report, error) {
		report.FinishedAt = r.Now()
		report.Passed = len(report.Checks) > 0
		for _, check := range report.Checks {
			if !check.Passed {
				report.Passed = false
				break
			}
		}
		return report, runErr
	}
	record := func(name string, passed bool, detail string) {
		report.Checks = append(report.Checks, Check{Name: name, Passed: passed, Detail: sanitizeDetail(detail)})
	}
	fail := func(name, detail string) (Report, error) {
		record(name, false, detail)
		return finish(nil)
	}

	if err := ctx.Err(); err != nil {
		return finish(err)
	}
	if r.Service == nil || r.Prompt == nil || r.Sabotage == nil {
		return finish(ErrInvalidRunner)
	}
	if strings.TrimSpace(r.SonicID) == "" || strings.TrimSpace(r.MarioID) == "" {
		return finish(ErrInvalidRunner)
	}

	ok, err := r.Prompt.Confirm("Confirm the local FogCast index and target cache are empty before acceptance")
	if err != nil {
		record("empty local index/cache confirmation", false, "operator prompt failed")
		return finish(err)
	}
	if !ok {
		return fail("empty local index/cache confirmation", "operator did not confirm clean starting state")
	}
	record("empty local index/cache confirmation", true, "operator confirmed clean starting state")

	if _, err := r.Service.Scan(ctx); err != nil {
		record("scan", false, "library scan failed")
		return finish(err)
	}
	record("scan", true, "library scan completed")
	games, err := r.Service.Games(ctx)
	if err != nil {
		record("manifest contains requested games", false, "game lookup failed")
		return finish(err)
	}
	if !containsGame(games, r.SonicID, protocol.SystemMegaDrive) || !containsGame(games, r.MarioID, protocol.SystemSNES) {
		return fail("manifest contains requested games", "requested game IDs were not found for their systems")
	}
	record("manifest contains requested games", true, "requested Mega Drive and SNES games found")

	ready, err := r.waitReady(ctx, record)
	if err != nil {
		return finish(err)
	}
	if !ready {
		return finish(nil)
	}
	if done := r.firstLaunch(ctx, record, r.SonicID, "Sonic"); done {
		return finish(nil)
	}
	if done := r.firstLaunch(ctx, record, r.MarioID, "Mario"); done {
		return finish(nil)
	}
	if done := r.repeatLaunch(ctx, record, r.SonicID, "Sonic"); done {
		return finish(nil)
	}
	if done := r.repeatLaunch(ctx, record, r.MarioID, "Mario"); done {
		return finish(nil)
	}

	confirmed, err := r.Sabotage.RebootTarget(ctx)
	if err != nil {
		record("target reboot confirmation", false, "operator action failed")
		return finish(err)
	}
	if !confirmed {
		return fail("target reboot confirmation", "operator declined target reboot")
	}
	record("target reboot confirmation", true, "operator confirmed target reboot")
	ready, err = r.waitReady(ctx, record)
	if err != nil {
		return finish(err)
	}
	if !ready {
		return finish(nil)
	}
	for _, game := range []struct{ id, label string }{{r.SonicID, "Sonic"}, {r.MarioID, "Mario"}} {
		if done := r.repeatLaunch(ctx, record, game.id, game.label+" cached launch after target reboot"); done {
			return finish(nil)
		}
	}

	confirmed, err = r.Sabotage.ToggleNAS(ctx)
	if err != nil {
		record("NAS-offline confirmation", false, "operator action failed")
		return finish(err)
	}
	if !confirmed {
		return fail("NAS-offline confirmation", "operator declined NAS-root offline test")
	}
	record("NAS-offline confirmation", true, "operator confirmed NAS-root offline state")
	for _, game := range []struct{ id, label string }{{r.SonicID, "Sonic"}, {r.MarioID, "Mario"}} {
		if done := r.repeatLaunch(ctx, record, game.id, game.label+" cached launch while NAS offline"); done {
			return finish(nil)
		}
	}

	uncachedID := r.UncachedID
	if uncachedID == "" {
		uncachedID = r.SonicID
	}
	_, err = r.Service.Launch(ctx, uncachedID, nil)
	var apiErr *protocol.APIError
	rejected := err != nil && (errors.As(err, &apiErr) || err != nil)
	if !rejected {
		return fail("uncached offline rejection", "uncached content was unexpectedly accepted")
	}
	record("uncached offline rejection", true, "uncached launch rejected while NAS offline")
	if _, statusErr := r.Service.Status(ctx); statusErr != nil {
		return fail("state preserved after uncached offline rejection", "target state could not be checked")
	}
	record("state preserved after uncached offline rejection", true, "active target state remained queryable")
	if remounter, ok := r.Sabotage.(POC2NASRemounter); ok {
		remounted, err := remounter.RemountNAS(ctx)
		if err != nil {
			record("NAS-root remount confirmation", false, "operator action failed")
			return finish(err)
		}
		if !remounted {
			return fail("NAS-root remount confirmation", "operator declined NAS-root remount")
		}
		record("NAS-root remount confirmation", true, "operator confirmed NAS-root remount")
	}

	confirmed, err = r.Sabotage.InterruptUpload(ctx)
	if err != nil {
		record("interrupted upload confirmation", false, "operator action failed")
		return finish(err)
	}
	if !confirmed {
		return fail("interrupted upload confirmation", "operator declined upload interruption")
	}
	record("interrupted upload confirmation", true, "operator confirmed upload interruption")
	interruptedID := r.InterruptedID
	if interruptedID == "" {
		interruptedID = r.MarioID
	}
	_, interruptedErr := r.Service.Launch(ctx, interruptedID, nil)
	if interruptedErr == nil {
		return fail("interrupted upload is not launchable", "interrupted content unexpectedly launched")
	}
	record("interrupted upload is not launchable", true, "interrupted transfer was rejected")

	for _, game := range []struct{ id, label string }{{r.MarioID, "Mario"}, {r.SonicID, "Sonic"}} {
		if done := r.repeatLaunch(ctx, record, game.id, "alternating "+game.label+" launch"); done {
			return finish(nil)
		}
	}

	status, err := r.Service.Stop(ctx)
	if err != nil || status.State != protocol.StateIdle {
		record("stop observes idle", false, "idle state was not observed")
		if err != nil {
			return finish(err)
		}
		return finish(nil)
	}
	record("stop observes idle", true, "target reported idle state")
	if err := r.Sleep(ctx, 12*time.Second); err != nil {
		record("black HDMI after stop", false, "blanking wait interrupted")
		return finish(err)
	}
	black, err := r.Prompt.Confirm("Confirm black HDMI output after stop")
	if err != nil {
		record("black HDMI after stop", false, "operator prompt failed")
		return finish(err)
	}
	if !black {
		return fail("black HDMI after stop", "operator declined black HDMI confirmation")
	}
	record("black HDMI after stop", true, "operator confirmed black HDMI")

	if _, err := r.Service.Launch(ctx, "invalid-poc2-request", nil); err == nil {
		return fail("invalid request rejected", "invalid launch request was accepted")
	}
	record("invalid request rejected", true, "invalid launch request rejected")
	if _, err := r.Service.Status(ctx); err != nil {
		return fail("state preserved after invalid request", "target state could not be checked")
	}
	record("state preserved after invalid request", true, "active target state remained queryable")

	confirmed, err = r.Sabotage.RestartAgent(ctx)
	if err != nil {
		record("agent restart confirmation", false, "operator action failed")
		return finish(err)
	}
	if !confirmed {
		return fail("agent restart confirmation", "operator declined agent restart")
	}
	record("agent restart confirmation", true, "operator confirmed agent restart")
	reconciled, err := r.waitStatus(ctx, record)
	if err != nil {
		return finish(err)
	}
	if !reconciled {
		return finish(nil)
	}
	for _, prompt := range []string{
		"Confirm target power-cycle acceptance gate",
		"Confirm POC 1 regression suite remains passing",
		"Confirm artifact audit found no private material",
	} {
		confirmed, err := r.Prompt.Confirm(prompt)
		if err != nil {
			record(prompt, false, "operator prompt failed")
			return finish(err)
		}
		if !confirmed {
			return fail(prompt, "operator declined acceptance gate")
		}
		record(prompt, true, "operator confirmed")
	}
	return finish(nil)
}

func (r POC2Runner) firstLaunch(ctx context.Context, record func(string, bool, string), id, label string) bool {
	var upload bool
	_, err := r.Service.Launch(ctx, id, func(progress fogcast.Progress) {
		if progress.Stage == "upload" {
			upload = true
		}
	})
	passed := err == nil && upload
	record(label+" first-transfer launch", passed, boolDetail(passed, "launch completed with an upload", "first transfer did not complete"))
	if !passed {
		return true
	}
	for _, check := range []string{label + " HDMI video", label + " HDMI audio", label + " controller operation", label + " playable screen"} {
		confirmed, promptErr := r.Prompt.Confirm(check)
		if promptErr != nil {
			record(check, false, "operator prompt failed")
			return true
		}
		record(check, confirmed, boolDetail(confirmed, "operator confirmed", "operator declined"))
		if !confirmed {
			return true
		}
	}
	return false
}

func (r POC2Runner) repeatLaunch(ctx context.Context, record func(string, bool, string), id, label string) bool {
	var upload bool
	_, err := r.Service.Launch(ctx, id, func(progress fogcast.Progress) {
		if progress.Stage == "upload" {
			upload = true
		}
	})
	passed := err == nil && !upload
	record(label+" repeat cache hit", passed, boolDetail(passed, "cached launch completed with zero upload", "repeat launch uploaded or failed"))
	if !passed {
		return true
	}
	return false
}

func (r POC2Runner) waitReady(ctx context.Context, record func(string, bool, string)) (bool, error) {
	deadline := r.Now().Add(45 * time.Second)
	for {
		health, err := r.Service.Health(ctx)
		if err == nil && health.Ready {
			record("agent health ready", true, "target reported ready")
			return true, nil
		}
		if !r.Now().Before(deadline) {
			record("agent health ready", false, "target did not report ready before deadline")
			return false, nil
		}
		if err := r.Sleep(ctx, time.Second); err != nil {
			record("agent health ready", false, "health polling interrupted")
			return false, err
		}
	}
}

func (r POC2Runner) waitStatus(ctx context.Context, record func(string, bool, string)) (bool, error) {
	deadline := r.Now().Add(10 * time.Second)
	for {
		if _, err := r.Service.Status(ctx); err == nil {
			record("agent restart reconciliation", true, "status available after restart")
			return true, nil
		}
		if !r.Now().Before(deadline) {
			record("agent restart reconciliation", false, "status unavailable after restart")
			return false, nil
		}
		if err := r.Sleep(ctx, 250*time.Millisecond); err != nil {
			record("agent restart reconciliation", false, "status polling interrupted")
			return false, err
		}
	}
}

func (r POC2Runner) withDefaults() POC2Runner {
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

func containsGame(games []catalog.Game, id string, system protocol.System) bool {
	for _, game := range games {
		if game.ID == id && game.System == system {
			return true
		}
	}
	return false
}

func sanitizeDetail(detail string) string {
	for _, forbidden := range []string{"Bearer ", "/Volumes/", ".sqlite3", ".sfc", ".smc", ".gen", ".md", ".zip"} {
		if strings.Contains(detail, forbidden) {
			return "operator check detail unavailable"
		}
	}
	if len(detail) > 256 {
		return detail[:256]
	}
	return detail
}
