//go:build linux

package tenfoot

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/menudisplay"
)

type startupDisplayObserver interface {
	StartupStatus(context.Context) (menudisplay.StartupStatus, error)
}

type startupSessionReader func(context.Context) (hostclient.SessionResult, error)
type startupTargetIdentity struct {
	ID        string
	PeerPID   int
	PeerStart string
}

func (i startupTargetIdentity) valid() bool { return i.ID != "" && i.PeerPID > 0 && i.PeerStart != "" }

type startupTargetReader func(context.Context) (startupTargetIdentity, error)

// reconcileDisplayStartupRetry keeps the admission fence while retrying
// transient runtime/host unavailability, until the caller cancels. Every
// observation attempt has its own timeout.
func (a *App) reconcileDisplayStartupRetry(ctx context.Context, observer startupDisplayObserver) error {
	if a == nil || observer == nil || ctx == nil {
		return errors.New("startup display observation is unavailable")
	}
	var read startupSessionReader
	if a.client != nil {
		read = a.client.Session
	}
	return a.reconcileDisplayStartupRetryWith(ctx, observer, read, readLocalStartupTarget)
}

func (a *App) beginStartupDisplayFence() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.menuDisplay == nil || a.sessionDisplayBusy {
		return errors.New("startup display is unavailable")
	}
	// The published socket must reject callbacks until the runtime and host
	// observations agree, including the whole retry interval.
	a.nativePadFenceMappingLocked()
	a.sessionDisplayBusy = true
	return nil
}
func (a *App) reconcileDisplayStartupRetryWith(ctx context.Context, observer startupDisplayObserver, read startupSessionReader, target startupTargetReader) error {
	return a.reconcileDisplayStartupRetryWithTimeout(ctx, observer, read, target, 5*time.Second)
}
func (a *App) reconcileDisplayStartupRetryWithTimeout(ctx context.Context, observer startupDisplayObserver, read startupSessionReader, target startupTargetReader, attemptTimeout time.Duration) error {
	if a == nil || observer == nil || ctx == nil || attemptTimeout <= 0 {
		return errors.New("startup display observation is unavailable")
	}
	if err := a.beginStartupDisplayFence(); err != nil {
		return err
	}
	for {
		attempt, cancel := context.WithTimeout(ctx, attemptTimeout)
		err := a.observeStartupDisplayFenced(attempt, observer, read, target)
		cancel()
		if err == nil {
			return nil
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.Join(err, ctx.Err())
		case <-timer.C:
		}
	}
}
func (a *App) observeStartupDisplayFenced(ctx context.Context, observer startupDisplayObserver, read startupSessionReader, target startupTargetReader) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	first, err := observer.StartupStatus(ctx)
	if err != nil {
		return fmt.Errorf("startup runtime status: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var session hostclient.SessionResult
	var local startupTargetIdentity
	switch first.State {
	case "idle":
		if first.Plane != nil && first.Plane.Session {
			return errors.New("idle runtime reported a session display")
		}
	case "running_development":
		if read == nil || target == nil {
			return errors.New("startup host or local target identity is unavailable")
		}
		local, err = target(ctx)
		if err != nil || !local.valid() {
			return errors.New("local target identity is unavailable")
		}
		session, err = read(ctx)
		if err != nil {
			return fmt.Errorf("startup host session: %w", err)
		}
		if session.ErrorCode != "" || session.State != "active" || session.ID == "" || session.Target == "" || session.TargetID == "" || session.CorePackage == nil || first.PackageID == "" || first.Generation == 0 ||
			session.CorePackage.PackageID != first.PackageID || session.CorePackage.Generation != first.Generation {
			return errors.New("runtime and host session identities differ")
		}
		if session.TargetID != local.ID {
			return errors.New("host session belongs to another local target")
		}
		a.mu.Lock()
		kitID := a.kitID
		a.mu.Unlock()
		if kitID != "" && session.TargetID != kitID {
			return errors.New("host session targets another kit")
		}
		if p := first.Plane; p != nil && p.Session {
			if !hostclient.SessionDisplayCapable(session.CorePackage) || p.Error != nil || p.PackageID != first.PackageID || p.CoreGeneration != first.Generation ||
				(p.Focus != "launcher" && p.Focus != "machine") || (!p.Available && p.Focus != "machine") || (p.Available && p.Generation == 0) {
				return errors.New("runtime session plane identity or focus is uncertain")
			}
		} else if hostclient.SessionDisplayCapable(session.CorePackage) {
			return errors.New("runtime session plane is missing")
		}
	default:
		return fmt.Errorf("runtime is not idle or active: %s", first.State)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	second, err := observer.StartupStatus(ctx)
	if err != nil {
		return fmt.Errorf("startup runtime recheck: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !sameStartupDisplay(first, second) {
		return errors.New("runtime display changed during startup")
	}
	if first.State == "running_development" {
		verified, err := target(ctx)
		if err != nil || !verified.valid() || verified != local {
			return errors.New("local target identity changed during startup")
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.sessionDisplayBusy {
		return errors.New("startup display fence changed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if first.State == "idle" {
		a.applySessionLocked(hostclient.SessionResult{State: "idle"})
		a.menuDisplay.ClearSessionBinding()
		a.displaySurroundOverride = nil
		a.startupFrameVerify = nil
		a.sessionDisplayBusy = false
		return nil
	}
	// Applying the observed session invalidates prior local display state, so
	// restore our fence while holding the lock, before accepting callbacks.
	a.applySessionLocked(session)
	a.sessionDisplayBusy = true
	a.menuDisplay.ClearSessionBinding()
	a.displaySurroundOverride = nil
	a.startupFrameVerify = nil
	a.sessionDisplayBinding = session
	a.sessionDisplay = protocol.SessionDisplayClosed
	if p := first.Plane; p != nil && p.Session && p.Available {
		if p.Focus == "launcher" {
			a.sessionDisplay = protocol.SessionDisplayFocused
		} else {
			a.sessionDisplay = protocol.SessionDisplayShown
			shown := true
			a.displaySurroundOverride = &shown
		}
		a.menuDisplay.BindSession(session.CorePackage.PackageID, session.CorePackage.Generation)
		if a.sessionDisplay == protocol.SessionDisplayFocused {
			// Room admission checks the busy flag. This lock prevents the IPC
			// callback or input from observing the temporary local clear.
			a.sessionDisplayBusy = false
			a.openPlayingHardwareRoomLocked()
			if !a.roomDuringPlay {
				a.sessionDisplayBusy = true
				return errors.New("startup controls could not open synchronously")
			}
		}
	}
	if first.Plane != nil && first.Plane.Session && first.Plane.Available {
		// Keep immutable identities for a fresh observation after the first
		// frame; the launcher remains fenced until the verifier succeeds.
		expected := first
		if first.Plane != nil {
			plane := *first.Plane
			expected.Plane = &plane
		}
		expectedSession := session
		core := *session.CorePackage
		expectedSession.CorePackage = &core
		a.startupFrameVerify = func(verifyCtx context.Context) error {
			return verifyStartupFrame(verifyCtx, observer, read, target, expected, expectedSession, local)
		}
		// Arm the frame fence under the same lock as adoption. The render loop
		// confirms the intended snapshot before it admits input or callbacks.
		a.fenceStartupFrameLocked()
	} else {
		a.sessionDisplayBusy = false
	}
	return nil
}

// verifyStartupFrame rechecks runtime incarnation and host/target identity
// around the first frame. Its caller keeps launcher input fenced throughout.
func verifyStartupFrame(ctx context.Context, observer startupDisplayObserver, read startupSessionReader, target startupTargetReader, expected menudisplay.StartupStatus, session hostclient.SessionResult, local startupTargetIdentity) error {
	if ctx == nil || observer == nil || read == nil || target == nil {
		return errors.New("startup verifier unavailable")
	}
	first, err := observer.StartupStatus(ctx)
	if err != nil || !sameStartupDisplay(expected, first) {
		return errors.New("runtime display changed before startup frame")
	}
	identity, err := target(ctx)
	if err != nil || identity != local || !identity.valid() {
		return errors.New("local target changed before startup frame")
	}
	current, err := read(ctx)
	if err != nil || current.ErrorCode != "" || !samePlayHIDSession(current, session) {
		return errors.New("host session changed before startup frame")
	}
	second, err := observer.StartupStatus(ctx)
	if err != nil || !sameStartupDisplay(expected, second) {
		return errors.New("runtime display changed during startup frame verification")
	}
	identity, err = target(ctx)
	if err != nil || identity != local || !identity.valid() {
		return errors.New("local target changed during startup frame verification")
	}
	return ctx.Err()
}

func sameStartupDisplay(a, b menudisplay.StartupStatus) bool {
	if a.State != b.State || a.PackageID != b.PackageID || a.Generation != b.Generation || a.RuntimePID != b.RuntimePID || a.RuntimeStart != b.RuntimeStart || (a.Plane == nil) != (b.Plane == nil) {
		return false
	}
	if a.Plane == nil {
		return true
	}
	x, y := a.Plane, b.Plane
	return x.Session == y.Session && x.Available == y.Available && x.Focus == y.Focus && x.PackageID == y.PackageID &&
		x.CoreGeneration == y.CoreGeneration && x.Generation == y.Generation && x.Error == nil && y.Error == nil
}

// readLocalStartupTarget uses the agent's root-owned Unix control socket.
// Its client verifies peer credentials and process incarnation; no bearer
// credential is sent to a network listener.
func readLocalStartupTarget(ctx context.Context) (startupTargetIdentity, error) {
	identity, err := localcores.NewClient(localcores.DefaultSocket).Identity(ctx)
	if err != nil {
		return startupTargetIdentity{}, err
	}
	return startupTargetIdentity{ID: identity.TargetID, PeerPID: identity.PeerPID, PeerStart: identity.PeerStart}, nil
}
