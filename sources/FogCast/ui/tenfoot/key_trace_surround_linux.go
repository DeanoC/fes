//go:build linux

package tenfoot

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
)

const diagnosticSurroundMaxHold = 15 * time.Second

// diagnosticSessionToken binds a request to the complete live play identity.
// No session identifiers or keyboard contents are written to the frame trace.
func diagnosticSessionToken(s hostclient.SessionResult) string {
	if s.State != "active" || s.Target != "kit" || s.CorePackage == nil || s.CorePackage.Generation == 0 {
		return ""
	}
	parts := []string{s.ID, s.GameID, s.Target, s.TargetID, s.FlightID, s.CorePackage.PackageID, strconv.FormatUint(s.CorePackage.Generation, 10)}
	for _, part := range parts {
		if part == "" || strings.ContainsAny(part, "\r\n") {
			return ""
		}
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "\n") + "\n"))
	return fmt.Sprintf("%x", digest)
}

// handleDiagnosticSurroundRequest is invoked only for managed root kit-host
// tracing. A request is bound to this process and the observed session. The
// hold never changes the preference and automatically expires after 15s or a
// change of play identity. Normal display transitions still use the usual path.
func (a *App) handleDiagnosticSurroundRequest(pid int, start string, uid int, now time.Time, holdPath, releasePath string) {
	if start == "" {
		return
	}
	a.mu.Lock()
	session := a.session
	token := diagnosticSessionToken(session)
	eligible := a.kitHost && a.frameTrace.keyRoutingActive(now) && token != "" &&
		!a.playHIDFailClosedLocked() && !a.playHIDInputBlockedLocked()
	a.mu.Unlock()
	if !eligible {
		return
	}
	expected := fmt.Sprintf("%d %s %s\n", pid, start, token)
	hold := consumeKeyTraceExpected(holdPath, expected, uid, now)
	release := consumeKeyTraceExpected(releasePath, expected, uid, now)
	if !hold && !release {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !samePlayHIDSession(a.session, session) || a.playHIDFailClosedLocked() || a.playHIDInputBlockedLocked() {
		return
	}
	if hold && !release && a.diagnosticSurroundUntil.IsZero() && a.surroundOfferedLocked() &&
		a.sessionDisplayLocked() == protocol.SessionDisplayShown && !a.sessionDisplayBusy && !a.sessionDisplayUncertain &&
		!a.roomDuringPlay && a.playHIDRouteReadyLocked() {
		a.diagnosticSurroundSession = session
		a.diagnosticSurroundUntil = now.Add(diagnosticSurroundMaxHold)
		a.traceKeyRouteLocked("diagnostic-surround-held", 0)
	}
	if release && !hold && !a.diagnosticSurroundUntil.IsZero() && samePlayHIDSession(a.session, a.diagnosticSurroundSession) {
		a.diagnosticSurroundUntil = time.Time{}
		a.diagnosticSurroundSession = hostclient.SessionResult{}
		a.traceKeyRouteLocked("diagnostic-surround-released", 0)
	}
}
