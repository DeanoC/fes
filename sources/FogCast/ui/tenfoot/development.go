package tenfoot

import (
	"context"
	"fmt"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/shared"
	"os"
	"strings"

	"github.com/DeanoC/FogCast/protocol"
)

const (
	diagnosticLabel = "DIAGNOSTIC"
	diagnosticHint  = "not a game session · HDMI/input may be down"
)

func developmentRBFSizeError(size int64) error {
	if size < 1 {
		return fmt.Errorf("development RBF is empty")
	}
	if size > protocol.MaxDevelopmentRBFBytes {
		return fmt.Errorf("development RBF exceeds %d bytes", protocol.MaxDevelopmentRBFBytes)
	}
	return nil
}

func openDevelopmentRBFFile(path string) (*os.File, int64, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, 0, fmt.Errorf("development RBF path is empty")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = file.Close()
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return nil, 0, err
	}
	if !info.Mode().IsRegular() {
		return nil, 0, fmt.Errorf("development RBF path is not a regular file")
	}
	if err := developmentRBFSizeError(info.Size()); err != nil {
		return nil, 0, err
	}
	ok = true
	return file, info.Size(), nil
}

func sessionDevelopmentActive(session hostclient.SessionResult) bool {
	if session.Development {
		return true
	}
	return strings.TrimSpace(session.Execution) == "fpga_development"
}

func sessionDevelopmentState(session hostclient.SessionResult) string {
	if state := strings.TrimSpace(session.DevelopmentSessionState); state != "" {
		return state
	}
	if sessionDevelopmentActive(session) {
		if exec := strings.TrimSpace(session.Execution); exec != "" {
			return exec
		}
		return sessionChromeDevelopment
	}
	return ""
}

func diagnosticNowPlayingLine(s SessionSnapshot) string {
	parts := make([]string, 0, 8)
	parts = append(parts, diagnosticLabel)
	if chrome := strings.TrimSpace(s.Chrome); chrome != "" && chrome != diagnosticLabel {
		parts = append(parts, chrome)
	}
	if state := strings.TrimSpace(s.DevelopmentState); state != "" && state != s.Chrome && state != s.Execution {
		parts = append(parts, state)
	}
	if exec := strings.TrimSpace(s.Execution); exec != "" {
		parts = append(parts, exec)
	}
	parts = append(parts, diagnosticHint)
	if s.Stopping && strings.TrimSpace(s.Chrome) != sessionChromeStopping {
		parts = append(parts, "stopping")
	}
	if s.RetryStop {
		hint := strings.TrimSpace(s.RetryHint)
		if hint == "" {
			hint = "retry Stop"
		}
		parts = append(parts, hint)
	}
	return strings.Join(parts, "  ·  ")
}

func (a *App) developmentLoadingLocked() bool {
	return a.devLoadPhase == "loading"
}

func (a *App) clearStaleDevelopmentLoadLocked() {
	if a.developmentLoadingLocked() {
		return
	}
	if a.devLoadPhase == "error" || a.devLoadPhase == "host" {
		a.clearDevelopmentLoadStatusLocked()
		a.devLoadPhase = "idle"
		a.devLoadMessage = ""
	}
}

func (a *App) clearCompletedDevelopmentLoadLocked() {
	if a.developmentLoadingLocked() {
		return
	}
	if a.devLoadPhase == "ok" {
		a.clearDevelopmentLoadStatusLocked()
		a.devLoadPhase = "idle"
		a.devLoadMessage = ""
	}
}

func (a *App) clearDevelopmentLoadStatusLocked() {
	msg := strings.TrimSpace(a.devLoadMessage)
	if msg != "" && strings.TrimSpace(a.status) == msg {
		a.status = ""
	}
}

func (a *App) kitLeaseBlocksMutationLocked() bool {
	if !a.kitLeaseHave {
		return false
	}
	if a.kitLease.State == "blocked" || strings.EqualFold(strings.TrimSpace(a.kitLease.ErrorCode), "KIT_LEASE_BLOCKED") {
		return true
	}
	return false
}

func (a *App) developmentLoadBlockedLocked() string {
	if a.retryStopLock {
		hint := strings.TrimSpace(a.retryStopHint)
		if hint == "" {
			return retryStopHint
		}
		return hint
	}
	if a.kitLeaseBlocksMutationLocked() {
		if line := strings.TrimSpace(formatKitLeaseLine(a.kitLease)); line != "" {
			return line
		}
		return "kit lease blocked"
	}
	if a.launch.Phase == "launching" || a.developmentLoadingLocked() || a.stopPhase == "stopping" {
		return "session busy"
	}
	if a.sessionStopOfferedLocked() {
		return "stop the current session first"
	}
	return ""
}

func (a *App) openDevelopmentPathOSKLocked() {
	if reason := a.developmentLoadBlockedLocked(); reason != "" {
		a.settingsStatus = reason
		a.status = reason
		return
	}
	a.settingsOSKKind = settingsOSKDevelopmentPath
	a.settingsOSKIndex = 0
	a.settingsOSKIsAdd = false
	a.settingsOSKField = shared.TextField{Buffer: a.developmentRBFPath}
	a.settingsOSKField.OSK.Reset()
	a.settingsOSKField.OSK.CyclePage(1)
}

func (a *App) submitDevelopmentPathOSKLocked() {
	if a.settingsOSKKind != settingsOSKDevelopmentPath {
		a.closeSettingsOSKLocked()
		return
	}
	if reason := a.developmentLoadBlockedLocked(); reason != "" {
		a.settingsStatus = reason
		a.status = reason
		a.closeSettingsOSKLocked()
		return
	}
	path := strings.TrimSpace(a.settingsOSKField.Buffer)
	if path == "" {
		a.settingsStatus = "development RBF path is empty"
		a.status = a.settingsStatus
		return
	}
	file, size, err := openDevelopmentRBFFile(path)
	if err != nil {
		a.settingsStatus = err.Error()
		a.status = a.settingsStatus
		return
	}
	_ = file.Close()
	a.developmentRBFPath = path
	a.closeSettingsOSKLocked()
	a.closeSettingsLocked()
	a.startDevelopmentLoadLocked(path, size)
}

func (a *App) startDevelopmentLoadLocked(path string, size int64) {
	if reason := a.developmentLoadBlockedLocked(); reason != "" {
		a.status = reason
		return
	}
	if err := developmentRBFSizeError(size); err != nil {
		a.status = err.Error()
		return
	}
	a.devLoadPhase = "loading"
	a.devLoadMessage = "loading diagnostic RBF"
	a.status = a.devLoadMessage
	a.sessionTitle = ""
	a.hold.Clear()
	a.bumpSessionGenLocked()
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go a.doDevelopmentLoad(ctx, path)
}

func (a *App) doDevelopmentLoad(ctx context.Context, path string) {
	file, size, err := openDevelopmentRBFFile(path)
	var result hostclient.SessionResult
	if err == nil {
		result, err = a.client.LoadDevelopmentRBF(ctx, size, file)
		_ = file.Close()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.devLoadPhase != "loading" {
		return
	}
	a.bumpSessionGenLocked()
	if err != nil {
		a.devLoadPhase = "error"
		a.devLoadMessage = "diagnostic RBF failed: " + err.Error()
		a.status = a.devLoadMessage
		return
	}
	a.devLoadMessage = ""
	if result.ErrorCode != "" {
		a.devLoadPhase = "host"
		a.devLoadMessage = fmt.Sprintf("host diagnostic RBF %d %s: %s", result.HTTPStatus, result.ErrorCode, result.ErrorMessage)
		a.status = a.devLoadMessage
		if saveFailedOrLeaseRetained(result.ErrorCode, result.ErrorMessage) {
			a.lockRetryStopLocked(result.ErrorCode, result.ErrorMessage)
			a.syncGPUParkLocked()
		}
		return
	}
	a.devLoadPhase = "ok"
	a.devLoadMessage = "DIAGNOSTIC RBF loaded · not a game session"
	a.status = a.devLoadMessage
	a.launch.Phase = "idle"
	a.launch.Message = ""
	a.launch.GameID = ""
	a.applySessionLocked(result)
	a.kickSessionPollLocked()
}
