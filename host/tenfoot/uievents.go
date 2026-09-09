package tenfoot

import (
	"context"
	"strconv"
	"strings"
	"time"
)

func (a *App) rememberFlightLocked(id string) {
	id = strings.TrimSpace(id)
	if !validClientFlightID(id) {
		return
	}
	a.flightID = id
}

func (a *App) rememberFlightsFromEventsLocked(events []SessionEvent) {
	for i := len(events) - 1; i >= 0; i-- {
		if validClientFlightID(events[i].FlightID) {
			a.flightID = events[i].FlightID
			return
		}
	}
}

func (a *App) clientStampLocked() ClientStamp {
	return ClientStampNow().withFlight(a.flightID)
}

func (a *App) postUIEventLocked(kind string, detail map[string]string) {
	if a == nil || a.client == nil {
		return
	}
	stamp := a.clientStampLocked()
	if a.skipDuplicateUIEventLocked(kind, detail) {
		return
	}
	event := UIEvent{
		TSUTC:    stamp.TsUTC,
		MonoMS:   stamp.MonoMS,
		FlightID: stamp.FlightID,
		Layer:    "ui",
		Kind:     kind,
		Severity: "ok",
		Detail:   detail,
	}
	client := a.client
	parent := a.ctx
	go func() {
		ctx := parent
		if ctx == nil {
			ctx = context.Background()
		}
		cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		_ = client.PostUIEvent(cctx, event)
	}()
}

func (a *App) skipDuplicateUIEventLocked(kind string, detail map[string]string) bool {
	key := uiEventKey(kind, detail)
	if key == "" {
		return false
	}
	if kind == "ui.focus" {
		if a.lastFocusKey == key {
			return true
		}
		a.lastFocusKey = key
		return false
	}
	if kind == "ui.nav" {
		if a.lastNavKey == key {
			return true
		}
		a.lastNavKey = key
		return false
	}
	return false
}

func uiEventKey(kind string, detail map[string]string) string {
	if kind == "ui.focus" {
		return strings.TrimSpace(detail["game_id"])
	}
	if kind == "ui.nav" {
		return strings.Join([]string{
			strings.TrimSpace(detail["view"]),
			strings.TrimSpace(detail["layout"]),
			strings.TrimSpace(detail["platform"]),
			strings.TrimSpace(detail["collection"]),
		}, "|")
	}
	return ""
}

func (a *App) focusedGameLocked() (Game, bool) {
	if a.grid.Focus < 0 || a.grid.Focus >= len(a.games) {
		return Game{}, false
	}
	return a.games[a.grid.Focus], true
}

func (a *App) stampFocusLocked() {
	detail := map[string]string{
		"index": strconv.Itoa(a.grid.Focus),
	}
	if game, ok := a.focusedGameLocked(); ok {
		detail["game_id"] = game.ID
		detail["title"] = game.Title
	}
	a.postUIEventLocked("ui.focus", detail)
}

func (a *App) stampNavLocked(reason string) {
	detail := map[string]string{
		"reason":     strings.TrimSpace(reason),
		"layout":     a.grid.Mode.String(),
		"platform":   a.platformID,
		"collection": a.collectionID,
	}
	if game, ok := a.focusedGameLocked(); ok {
		detail["game_id"] = game.ID
	}
	a.postUIEventLocked("ui.nav", detail)
}

func (a *App) debugHUDSnapshotLocked() DebugHUDSnapshot {
	if !a.debugHUD {
		return DebugHUDSnapshot{}
	}
	lines := make([]string, 0, 4)
	if id := strings.TrimSpace(a.flightID); id != "" {
		short := id
		if len(short) > 8 {
			short = short[:8]
		}
		lines = append(lines, "flight "+short)
	} else {
		lines = append(lines, "flight —")
	}
	if a.kitLeaseHave {
		gen := shortLeaseGeneration(a.kitLease.Generation)
		if gen == "" {
			gen = "—"
		}
		ttl := formatLeaseExpiry(a.kitLease)
		if ttl == "" {
			ttl = "—"
		}
		lines = append(lines, "gen "+gen+"  ttl "+ttl)
	} else {
		lines = append(lines, "lease unread")
	}
	errLine := strings.TrimSpace(a.launch.ErrorMessage)
	if errLine == "" {
		errLine = strings.TrimSpace(a.launch.Message)
	}
	if a.launch.Phase != "error" && a.launch.Phase != "host" {
		if a.stopPhase == "error" || a.stopPhase == "host" {
			errLine = strings.TrimSpace(a.stopMessage)
		} else {
			errLine = ""
		}
	}
	if errLine == "" {
		lines = append(lines, "err —")
	} else {
		lines = append(lines, "err "+errLine)
	}
	return DebugHUDSnapshot{Enabled: true, Lines: lines}
}
