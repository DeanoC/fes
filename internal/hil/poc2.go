package hil

import (
	"context"
	"errors"
	"strings"
	"sync"
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
	RemountNAS(context.Context) (bool, error)
	InterruptUpload(context.Context) (bool, error)
}

// POC2Runner executes the software-observable portion of POC 2 hardware
// acceptance. SonicID and MarioID are intentionally supplied by the operator;
// no source names or paths are embedded in the binary. UncachedID and
// InterruptedID are explicit operator-supplied fixture IDs so the negative
// paths cannot silently reuse a cached primary game.
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
	if strings.TrimSpace(r.SonicID) == "" || strings.TrimSpace(r.MarioID) == "" || strings.TrimSpace(r.UncachedID) == "" || strings.TrimSpace(r.InterruptedID) == "" {
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

	scanReport, err := r.Service.Scan(ctx)
	if err != nil {
		record("scan", false, "library scan failed")
		return finish(err)
	}
	if !scanRootsReady(scanReport) {
		return fail("scan roots online", "one or more required source roots are offline")
	}
	record("scan", true, "library scan completed")
	games, err := r.Service.Games(ctx)
	if err != nil {
		record("manifest contains requested games", false, "game lookup failed")
		return finish(err)
	}
	if !containsAvailableGame(games, r.SonicID, protocol.SystemMegaDrive) || !containsAvailableGame(games, r.MarioID, protocol.SystemSNES) {
		return fail("manifest contains requested games", "requested game IDs were not found for their systems")
	}
	record("manifest contains requested games", true, "requested Mega Drive and SNES games found")
	if !distinctFixtureIDs(r.SonicID, r.MarioID, r.UncachedID, r.InterruptedID) ||
		!containsAvailableFixture(games, r.UncachedID) || !containsAvailableFixture(games, r.InterruptedID) {
		return fail("manifest contains requested fixtures", "uncached and interrupted IDs must be distinct online ZIP fixtures")
	}
	record("manifest contains requested fixtures", true, "uncached and interrupted ZIP fixtures found")

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

	beforeStatus, err := r.Service.Status(ctx)
	if err != nil {
		return fail("state preserved before uncached offline rejection", "target state could not be checked")
	}
	_, err = r.Service.Launch(ctx, r.UncachedID, nil)
	var apiErr *protocol.APIError
	rejected := errors.As(err, &apiErr) && apiErr.Code == protocol.CodeSourceUnavailable
	if !rejected {
		return fail("uncached offline rejection", "uncached content was unexpectedly accepted")
	}
	record("uncached offline rejection", true, "uncached launch rejected while NAS offline")
	afterStatus, statusErr := r.Service.Status(ctx)
	if statusErr != nil {
		return fail("state preserved after uncached offline rejection", "target state could not be checked")
	}
	if !sameStatusIdentity(beforeStatus, afterStatus) {
		return fail("state preserved after uncached offline rejection", "active target identity changed")
	}
	record("state preserved after uncached offline rejection", true, "active target identity remained unchanged")
	remounted, err := r.Sabotage.RemountNAS(ctx)
	if err != nil {
		record("NAS-root remount confirmation", false, "operator action failed")
		return finish(err)
	}
	if !remounted {
		return fail("NAS-root remount confirmation", "operator declined NAS-root remount")
	}
	record("NAS-root remount confirmation", true, "operator confirmed NAS-root remount")

	interruptedBefore, err := r.Service.Status(ctx)
	if err != nil {
		return fail("interrupted upload state before transfer", "target state could not be checked")
	}
	var uploadStarted bool
	var interruptAttempted bool
	type interruptResult struct {
		confirmed bool
		err       error
	}
	interruptResults := make(chan interruptResult, 1)
	interruptCtx, cancelInterrupt := context.WithCancel(ctx)
	var interruptOnce sync.Once
	_, interruptedErr := r.Service.Launch(interruptCtx, r.InterruptedID, func(progress fogcast.Progress) {
		if progress.Stage == "upload-started" {
			uploadStarted = true
			interruptOnce.Do(func() {
				interruptAttempted = true
				go func() {
					confirmed, err := r.Sabotage.InterruptUpload(interruptCtx)
					interruptResults <- interruptResult{confirmed: confirmed, err: err}
					cancelInterrupt()
				}()
			})
		}
	})
	var interruptConfirmed bool
	var interruptErr error
	if interruptAttempted {
		select {
		case result := <-interruptResults:
			interruptConfirmed, interruptErr = result.confirmed, result.err
		case <-ctx.Done():
			cancelInterrupt()
			return finish(ctx.Err())
		}
	}
	cancelInterrupt()
	if !uploadStarted {
		return fail("interrupted upload started", "service did not report an in-flight upload")
	}
	if interruptErr != nil {
		return fail("interrupted upload confirmation", "operator action failed")
	}
	if !interruptConfirmed {
		return fail("interrupted upload confirmation", "operator declined upload interruption")
	}
	if interruptedErr == nil {
		return fail("interrupted upload transfer failure", "interrupted content unexpectedly completed transfer")
	}
	var interruptedAPIError *protocol.APIError
	if !errors.As(interruptedErr, &interruptedAPIError) || interruptedAPIError.Code != protocol.CodeTransferFailed {
		return fail("interrupted upload transfer failure", "interrupted content did not report transfer failure")
	}
	interruptedAfter, statusErr := r.Service.Status(ctx)
	if statusErr != nil {
		return fail("interrupted upload state preserved", "target state could not be checked")
	}
	if !sameStatusIdentity(interruptedBefore, interruptedAfter) {
		return fail("interrupted upload state preserved", "active target identity changed")
	}
	record("interrupted upload state preserved", true, "interrupted transfer failed and preserved active target state")
	probeOffline, err := r.Sabotage.ToggleNAS(ctx)
	if err != nil {
		record("interrupted content offline probe confirmation", false, "operator action failed")
		return finish(err)
	}
	if !probeOffline {
		return fail("interrupted content offline probe confirmation", "operator declined NAS-root offline probe")
	}
	record("interrupted content offline probe confirmation", true, "operator confirmed NAS-root offline probe")
	probeBefore, err := r.Service.Status(ctx)
	if err != nil {
		return fail("interrupted content probe state before launch", "target state could not be checked")
	}
	_, probeErr := r.Service.Launch(ctx, r.InterruptedID, nil)
	var probeAPIError *protocol.APIError
	if !errors.As(probeErr, &probeAPIError) || probeAPIError.Code != protocol.CodeSourceUnavailable {
		return fail("interrupted upload is not launchable", "interrupted content was launchable while its source was offline")
	}
	probeAfter, statusErr := r.Service.Status(ctx)
	if statusErr != nil {
		return fail("interrupted content probe state preserved", "target state could not be checked")
	}
	if !sameStatusIdentity(probeBefore, probeAfter) {
		return fail("interrupted content probe state preserved", "active target identity changed")
	}
	record("interrupted upload is not launchable", true, "interrupted content was rejected while source was offline")
	remounted, err = r.Sabotage.RemountNAS(ctx)
	if err != nil {
		record("NAS-root remount after interrupted probe", false, "operator action failed")
		return finish(err)
	}
	if !remounted {
		return fail("NAS-root remount after interrupted probe", "operator declined NAS-root remount")
	}
	record("NAS-root remount after interrupted probe", true, "operator confirmed NAS-root remount")

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

	invalidBefore, err := r.Service.Status(ctx)
	if err != nil {
		return fail("state preserved before invalid request", "target state could not be checked")
	}
	_, invalidErr := r.Service.Launch(ctx, "invalid request", nil)
	var invalidAPIError *protocol.APIError
	if !errors.As(invalidErr, &invalidAPIError) || invalidAPIError.Code != protocol.CodeBadRequest {
		return fail("invalid request rejected", "invalid launch request did not return BAD_REQUEST")
	}
	record("invalid request rejected", true, "invalid launch request rejected")
	invalidAfter, statusErr := r.Service.Status(ctx)
	if statusErr != nil {
		return fail("state preserved after invalid request", "target state could not be checked")
	}
	if !sameStatusIdentity(invalidBefore, invalidAfter) {
		return fail("state preserved after invalid request", "active target identity changed")
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
		status, err := r.Service.Status(ctx)
		if err == nil && status.State == protocol.StateIdle && status.GameID == nil && status.System == nil {
			record("agent restart reconciliation", true, "idle status reconciled after restart")
			return true, nil
		}
		if !r.Now().Before(deadline) {
			record("agent restart reconciliation", false, "idle status was not reconciled after restart")
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

func scanRootsReady(report catalog.ScanReport) bool {
	ready := map[protocol.System]bool{}
	for _, root := range report.Roots {
		if root.Offline {
			return false
		}
		if root.System == protocol.SystemMegaDrive || root.System == protocol.SystemSNES {
			ready[root.System] = true
		}
	}
	return ready[protocol.SystemMegaDrive] && ready[protocol.SystemSNES]
}

func containsAvailableGame(games []catalog.Game, id string, system protocol.System) bool {
	for _, game := range games {
		if game.ID == id && game.System == system && game.RootOnline && game.State == catalog.SourceStateAvailable && game.Kind == catalog.SourceKindZIP {
			return true
		}
	}
	return false
}

func containsAvailableFixture(games []catalog.Game, id string) bool {
	for _, game := range games {
		if game.ID == id && game.RootOnline && game.State == catalog.SourceStateAvailable && game.Kind == catalog.SourceKindZIP && (game.System == protocol.SystemMegaDrive || game.System == protocol.SystemSNES) {
			return true
		}
	}
	return false
}

func distinctFixtureIDs(sonicID, marioID, uncachedID, interruptedID string) bool {
	ids := []string{sonicID, marioID, uncachedID, interruptedID}
	for index, id := range ids {
		for _, other := range ids[index+1:] {
			if id == other {
				return false
			}
		}
	}
	return true
}

func sameStatusIdentity(left, right protocol.Status) bool {
	if left.State != right.State || !sameString(left.GameID, right.GameID) || !sameSystem(left.System, right.System) || !sameString(left.ExpectedCore, right.ExpectedCore) || !sameString(left.ObservedCore, right.ObservedCore) {
		return false
	}
	return (left.LastError == nil) == (right.LastError == nil) && (left.LastError == nil || (left.LastError.Code == right.LastError.Code))
}

func sameString(left, right *string) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func sameSystem(left, right *protocol.System) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
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
