package tenfoot

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	maxSessionEventRows = 8
	retryStopHint       = "retry Stop before leaving the game"
)

const (
	sessionChromeIdle        = "idle"
	sessionChromeActive      = "active"
	sessionChromeStopping    = "stopping"
	sessionChromeFailed      = "failed"
	sessionChromeDevelopment = "development"
)

// sessionChromeState maps host session fields onto sofa chrome kinds.
func sessionChromeState(state, execution string, stopping, retryLock bool) string {
	if stopping {
		return sessionChromeStopping
	}
	if retryLock || strings.TrimSpace(state) == sessionChromeFailed {
		return sessionChromeFailed
	}
	if strings.TrimSpace(execution) == "fpga_development" {
		switch strings.TrimSpace(state) {
		case sessionChromeActive, "launching", "":
			return sessionChromeDevelopment
		}
	}
	switch strings.TrimSpace(state) {
	case "", sessionChromeIdle:
		return sessionChromeIdle
	case "launching":
		return sessionChromeActive
	case sessionChromeActive, sessionChromeStopping, sessionChromeFailed:
		return strings.TrimSpace(state)
	default:
		return strings.TrimSpace(state)
	}
}

func formatSessionEvent(ev SessionEvent) string {
	parts := make([]string, 0, 6)
	if event := readableSessionEventName(ev.Event); event != "" {
		parts = append(parts, event)
	}
	if state := strings.TrimSpace(ev.State); state != "" {
		parts = append(parts, state)
	}
	if title := strings.TrimSpace(ev.GameID); title != "" {
		parts = append(parts, title)
	}
	if system := strings.TrimSpace(ev.System); system != "" {
		parts = append(parts, system)
	}
	if media := strings.TrimSpace(ev.Media); media != "" {
		parts = append(parts, "media "+media)
	}
	if ev.Progress != nil {
		if msg := strings.TrimSpace(ev.Progress.Message); msg != "" {
			parts = append(parts, msg)
		} else if stage := strings.TrimSpace(ev.Progress.Stage); stage != "" {
			parts = append(parts, stage)
		}
	}
	return strings.Join(parts, "  ·  ")
}

func readableSessionEventName(event string) string {
	event = strings.TrimSpace(event)
	if event == "" {
		return ""
	}
	event = strings.TrimPrefix(event, "session.")
	event = strings.ReplaceAll(event, ".", " ")
	event = strings.ReplaceAll(event, "_", " ")
	return event
}

func formatKitLeaseLine(status KitLeaseStatus) string {
	if status.Unavailable {
		if msg := strings.TrimSpace(status.ErrorMessage); msg != "" {
			return msg
		}
		return "kit unreachable"
	}
	if status.HTTPStatus == 0 && status.State == "" && status.ErrorCode == "" {
		return ""
	}
	if status.ErrorCode != "" && status.State == "" {
		line := "kit lease " + strings.ToLower(status.ErrorCode)
		if msg := strings.TrimSpace(status.ErrorMessage); msg != "" {
			return line + "  ·  " + msg
		}
		return line
	}
	parts := make([]string, 0, 6)
	if state := strings.TrimSpace(status.State); state != "" {
		parts = append(parts, "lease "+state)
	} else {
		parts = append(parts, "lease")
	}
	if owner := strings.TrimSpace(status.Owner); owner != "" {
		parts = append(parts, owner)
	}
	if purpose := strings.TrimSpace(status.Purpose); purpose != "" {
		parts = append(parts, purpose)
	}
	if gen := shortLeaseGeneration(status.Generation); gen != "" {
		parts = append(parts, "gen "+gen)
	}
	if exp := formatLeaseExpiry(status); exp != "" {
		parts = append(parts, exp)
	}
	if status.State == "blocked" || strings.TrimSpace(status.Reason) != "" {
		reason := strings.TrimSpace(status.Reason)
		if reason == "" {
			reason = "blocked"
		}
		parts = append(parts, reason)
	}
	return strings.Join(parts, "  ·  ")
}

func shortLeaseGeneration(generation string) string {
	generation = strings.TrimSpace(generation)
	if len(generation) <= 8 {
		return generation
	}
	return generation[:8]
}

func formatLeaseExpiry(status KitLeaseStatus) string {
	if status.ExpiresInMS > 0 {
		sec := (status.ExpiresInMS + 999) / 1000
		if sec < 1 {
			sec = 1
		}
		return strconv.FormatInt(sec, 10) + "s"
	}
	if exp := strings.TrimSpace(status.ExpiresAt); exp != "" {
		if i := strings.IndexByte(exp, 'T'); i > 0 && len(exp) >= i+6 {
			return exp[i+1 : i+6]
		}
		return exp
	}
	return ""
}

func saveFailedOrLeaseRetained(code, message string) bool {
	blob := strings.ToLower(strings.TrimSpace(code) + " " + strings.TrimSpace(message))
	if blob == "" || blob == " " {
		return false
	}
	return strings.Contains(blob, "save_failed") ||
		strings.Contains(blob, "retry stop") ||
		strings.Contains(blob, "kit lease") ||
		strings.Contains(blob, "kit_lease")
}

func sessionEventRequestsRetryStop(ev SessionEvent) bool {
	if saveFailedOrLeaseRetained(ev.Event, "") {
		return true
	}
	if ev.Progress != nil && saveFailedOrLeaseRetained(ev.Progress.Stage, ev.Progress.Message) {
		return true
	}
	return false
}

// eventsRetainRetryStop is true when a save_failed / lease-retained event is
// still the latest unresolved event. A later successful session.stop to idle
// clears it so historical rows from after=0 cannot lock a live idle sofa.
func eventsRetainRetryStop(rows []SessionEvent) (bool, SessionEvent) {
	lock := false
	var hit SessionEvent
	for _, ev := range rows {
		if strings.TrimSpace(ev.Event) == "session.stop" && strings.TrimSpace(ev.State) == sessionChromeIdle {
			lock = false
			hit = SessionEvent{}
			continue
		}
		if sessionEventRequestsRetryStop(ev) {
			lock = true
			hit = ev
		}
	}
	return lock, hit
}

func retryStopStatus(code, message string) string {
	if saveFailedOrLeaseRetained(code, message) {
		return retryStopHint
	}
	msg := strings.TrimSpace(message)
	if msg == "" {
		return "retry Stop"
	}
	if strings.Contains(strings.ToLower(msg), "retry stop") {
		return msg
	}
	return fmt.Sprintf("retry Stop  ·  %s", msg)
}

func mergeSessionEvents(existing []SessionEvent, incoming []SessionEvent, after uint64) (rows []SessionEvent, next uint64) {
	rows = append([]SessionEvent(nil), existing...)
	next = after
	seen := make(map[uint64]bool, len(rows))
	for _, ev := range rows {
		seen[ev.Sequence] = true
		if ev.Sequence > next {
			next = ev.Sequence
		}
	}
	for _, ev := range incoming {
		if ev.Sequence == 0 || ev.Sequence <= after || seen[ev.Sequence] {
			if ev.Sequence > next {
				next = ev.Sequence
			}
			continue
		}
		seen[ev.Sequence] = true
		rows = append(rows, ev)
		if ev.Sequence > next {
			next = ev.Sequence
		}
	}
	if len(rows) > maxSessionEventRows {
		rows = append([]SessionEvent(nil), rows[len(rows)-maxSessionEventRows:]...)
	}
	return rows, next
}
